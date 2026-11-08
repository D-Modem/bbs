package timesync

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	ptp "github.com/facebook/time/ptp/protocol"

	"github.com/D-Modem/bbs/internal/sockets"
)

// PTP's ports and its IPv4 multicast group (IEEE 1588 annex C).
const (
	EventPort   = 319
	GeneralPort = 320
	ptpGroup    = "224.0.1.129"
)

// PTPSettings are the [ptp] section's.
type PTPSettings struct {
	Address   string // the address bound ("": all), on ports 319 and 320
	Interface string // multicast's, and hardware timestamps' ("": no multicast)
	Master    bool   // a grandmaster ([ptp]); else only a client of ptp servers
	Multicast bool   // sends announcements and syncs to the group on Interface
	Unicast   bool   // grants unicast transmission to clients that ask
	Domain    uint8
	Priority1 uint8
	Priority2 uint8
	SyncLog     int8 // multicast syncs every 2^SyncLog seconds
	AnnounceLog int8
	MinLog      int8 // the shortest interval granted unicast clients (2^MinLog s)
	MaxClients  int  // unicast clients at once
	Hardware    bool // the network card's timestamps (Interface's)
	UTCOffset   int  // TAI - UTC, in seconds

	basePort int // tests': the event port (the general one past it); 0: 319
}

// Ports are the addresses PTP binds (both: the event and general ports).
func (s PTPSettings) Ports() []string {
	e, g := s.ports()
	return []string{net.JoinHostPort(s.Address, strconv.Itoa(e)), net.JoinHostPort(s.Address, strconv.Itoa(g))}
}

func (s PTPSettings) ports() (event, general int) {
	if s.basePort != 0 {
		return s.basePort, s.basePort + 1
	}
	return EventPort, GeneralPort
}

// PTP is a PTP port: a grandmaster's, and a client's of the ptp servers
// the clock follows, on the same two sockets.
type PTP struct {
	S      PTPSettings
	Clock  *Clock
	Refuse func(net.IP) bool    // nil: none refused
	Log    func(string, ...any) // nil: none

	event, general *net.UDPConn
	stamped        bool // the kernel stamps the event socket's packets
	id             ptp.ClockIdentity

	wmu sync.Mutex // an event packet's write and the reading of its stamp

	mu      sync.Mutex
	grants  map[string]*grant      // unicast clients, by address
	clients map[string]*ptpUpstream // servers followed, by address
	seq     [2]uint16               // announce and sync sequence numbers: one count for all
	// (multicast and every unicast client), so one that gets both never
	// matches one's Sync with the other's Follow_Up
}

func (p *PTP) logf(f string, a ...any) {
	if p.Log != nil {
		p.Log(f, a...)
	}
}

// Open binds (or takes the sockets handed down for) the ports, and turns
// on timestamps.
func (p *PTP) Open() error {
	ports := p.S.Ports()
	var err error
	if p.event, err = sockets.ListenUDP(ports[0]); err != nil {
		return fmt.Errorf("ptp: %w", err)
	}
	if p.general, err = sockets.ListenUDP(ports[1]); err != nil {
		p.event.Close()
		return fmt.Errorf("ptp: %w", err)
	}
	if err := EnableTimestamps(p.event, p.S.Hardware, p.S.Interface); err != nil {
		if p.S.Hardware {
			p.Close()
			return fmt.Errorf("ptp: hardware timestamps on %s: %w", p.S.Interface, err)
		}
		p.logf("ptp: no kernel timestamps (%v): stamping as packets are read and written", err)
	} else {
		p.stamped = true
	}
	p.id = clockIdentity(p.S.Interface)
	p.grants = map[string]*grant{}
	if p.clients == nil {
		p.clients = map[string]*ptpUpstream{}
	}
	if p.S.Master && p.S.Multicast && p.S.Interface != "" {
		ifc, err := net.InterfaceByName(p.S.Interface)
		if err != nil {
			p.Close()
			return fmt.Errorf("ptp: interface: %w", err)
		}
		for _, c := range []*net.UDPConn{p.event, p.general} {
			if err := joinGroup(c, ifc, net.ParseIP(ptpGroup)); err != nil {
				p.Close()
				return fmt.Errorf("ptp: multicast on %s: %w", p.S.Interface, err)
			}
		}
	}
	return nil
}

// Close closes the sockets.
func (p *PTP) Close() {
	if p.event != nil {
		p.event.Close()
	}
	if p.general != nil {
		p.general.Close()
	}
}

// clockIdentity is the clock's EUI-64, from iface's MAC address (or the
// first interface's that has one; or random).
func clockIdentity(iface string) ptp.ClockIdentity {
	var macs []net.HardwareAddr
	if ifc, err := net.InterfaceByName(iface); err == nil {
		macs = append(macs, ifc.HardwareAddr)
	}
	if ifs, err := net.Interfaces(); err == nil {
		for _, i := range ifs {
			if i.Flags&net.FlagLoopback == 0 {
				macs = append(macs, i.HardwareAddr)
			}
		}
	}
	for _, m := range macs {
		if len(m) == 6 {
			if id, err := ptp.NewClockIdentity(m); err == nil {
				return id
			}
		}
	}
	var b [6]byte
	rand.Read(b[:])
	b[0] |= 2 // locally administered
	id, _ := ptp.NewClockIdentity(b[:])
	return id
}

// Run answers until ctx is done.
func (p *PTP) Run(ctx context.Context) error {
	context.AfterFunc(ctx, p.Close)
	go p.read(ctx, p.general, false)
	if p.S.Master {
		go p.schedule(ctx)
	}
	for _, u := range p.upstreams() {
		go u.run(ctx)
	}
	p.read(ctx, p.event, true)
	return nil
}

func (p *PTP) upstreams() []*ptpUpstream {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []*ptpUpstream
	for _, u := range p.clients {
		out = append(out, u)
	}
	return out
}

// read reads a socket's packets until it is closed.
func (p *PTP) read(ctx context.Context, c *net.UDPConn, event bool) {
	buf, oob := make([]byte, 1500), make([]byte, 512)
	for {
		n, oobn, _, addr, err := c.ReadMsgUDP(buf, oob)
		rx := time.Now()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if t, ok := rxStamp(oob[:oobn]); ok && event && p.stamped {
			rx = t
		}
		p.handle(buf[:n], addr, rx)
	}
}

// tai is a system clock reading as PTP has it: corrected, TAI.
func (p *PTP) tai(t time.Time) ptp.Timestamp {
	return ptp.NewTimestamp(p.Clock.Correct(t).Add(time.Duration(p.S.UTCOffset) * time.Second))
}

// handle takes one packet: a server's followed, to its client; else the
// grandmaster's.
func (p *PTP) handle(b []byte, from *net.UDPAddr, rx time.Time) {
	if len(b) < headerLen || b[1]&0x0f != 2 {
		return
	}
	if p.Refuse != nil && p.Refuse(from.IP) {
		return
	}
	p.mu.Lock()
	u := p.clients[from.IP.String()]
	p.mu.Unlock()
	if u != nil {
		u.handle(b, rx)
		return
	}
	if !p.S.Master || b[4] != p.S.Domain {
		return
	}
	switch ptp.MessageType(b[0] & 0x0f) {
	case ptp.MessageDelayReq:
		var req ptp.SyncDelayReq
		if req.UnmarshalBinary(b) != nil {
			return
		}
		p.delayResp(&req, from, rx)
	case ptp.MessageSignaling:
		var sig ptp.Signaling
		if sig.UnmarshalBinary(b) != nil {
			return
		}
		p.negotiate(&sig, from)
	}
}

// Message lengths (without the two bytes UDPv6 would have).
const (
	headerLen    = 34
	announceLen  = headerLen + 30
	syncLen      = headerLen + 10
	delayRespLen = headerLen + 20
	sigLen       = headerLen + 10
)

// header is a message's header from this clock.
func (p *PTP) header(t ptp.MessageType, length int, seq uint16, flags uint16, logIntv int8) ptp.Header {
	control := map[ptp.MessageType]uint8{ptp.MessageSync: 0, ptp.MessageDelayReq: 1, ptp.MessageFollowUp: 2, ptp.MessageDelayResp: 3}
	c, ok := control[t]
	if !ok {
		c = 5
	}
	return ptp.Header{SdoIDAndMsgType: ptp.NewSdoIDAndMsgType(t, 0), Version: ptp.Version,
		MessageLength: uint16(length), DomainNumber: p.S.Domain, FlagField: flags,
		SourcePortIdentity: ptp.PortIdentity{ClockIdentity: p.id, PortNumber: 1}, SequenceID: seq,
		ControlField: c, LogMessageInterval: ptp.LogInterval(logIntv)}
}

// sendEvent sends an event message (to port 319), and is when it went.
func (p *PTP) sendEvent(b []byte, to net.IP) (time.Time, error) {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	before := time.Now()
	port, _ := p.S.ports()
	if _, err := p.event.WriteToUDP(b, &net.UDPAddr{IP: to, Port: port}); err != nil {
		return time.Time{}, err
	}
	if p.stamped {
		if t, err := txStamp(p.event); err == nil {
			return t, nil
		}
	}
	return before, nil
}

func (p *PTP) sendGeneral(b []byte, to net.IP) error {
	_, port := p.S.ports()
	_, err := p.general.WriteToUDP(b, &net.UDPAddr{IP: to, Port: port})
	return err
}

// quality is the grandmaster's, from what the clock follows.
func (p *PTP) quality() (ptp.ClockQuality, uint16, ptp.TimeSource) {
	r := p.Clock.Ref()
	src := ptp.TimeSource(r.TimeSrc)
	if !r.Synced {
		return ptp.ClockQuality{ClockClass: 248, ClockAccuracy: ptp.ClockAccuracyUnknown, OffsetScaledLogVariance: 0xffff}, 0, ptp.TimeSourceInternalOscillator
	}
	flags := ptp.FlagPTPTimescale | ptp.FlagCurrentUtcOffsetValid | ptp.FlagTimeTraceable | ptp.FlagFrequencyTraceable
	switch r.Leap {
	case 1:
		flags |= ptp.FlagLeap61
	case 2:
		flags |= ptp.FlagLeap59
	}
	return ptp.ClockQuality{ClockClass: ptp.ClockClass6, ClockAccuracy: ptp.ClockAccuracyFromOffset(r.RootDelay/2 + r.RootDisp),
		OffsetScaledLogVariance: 0xffff}, flags, src
}

// announce sends an Announce to an address (the group's, or a client's).
func (p *PTP) announce(to net.IP, seq uint16, unicast bool, logIntv int8) {
	q, flags, src := p.quality()
	if flags == 0 {
		flags = ptp.FlagPTPTimescale
	}
	if unicast {
		flags |= ptp.FlagUnicast
	}
	a := ptp.Announce{Header: p.header(ptp.MessageAnnounce, announceLen, seq, flags, logIntv),
		AnnounceBody: ptp.AnnounceBody{OriginTimestamp: p.tai(time.Now()), CurrentUTCOffset: int16(p.S.UTCOffset),
			GrandmasterPriority1: p.S.Priority1, GrandmasterClockQuality: q, GrandmasterPriority2: p.S.Priority2,
			GrandmasterIdentity: p.id, TimeSource: src}}
	if b, err := ptp.Bytes(&a); err == nil {
		p.sendGeneral(b, to)
	}
}

// sync sends a two-step Sync and its Follow_Up, with when the Sync went.
func (p *PTP) sync(to net.IP, seq uint16, unicast bool, logIntv int8) {
	flags := ptp.FlagTwoStep
	if unicast {
		flags |= ptp.FlagUnicast
	}
	s := ptp.SyncDelayReq{Header: p.header(ptp.MessageSync, syncLen, seq, flags, logIntv),
		SyncDelayReqBody: ptp.SyncDelayReqBody{OriginTimestamp: p.tai(time.Now())}}
	b, err := ptp.Bytes(&s)
	if err != nil {
		return
	}
	sent, err := p.sendEvent(b, to)
	if err != nil {
		return
	}
	fflags := uint16(0)
	if unicast {
		fflags = ptp.FlagUnicast
	}
	f := ptp.FollowUp{Header: p.header(ptp.MessageFollowUp, syncLen, seq, fflags, logIntv),
		FollowUpBody: ptp.FollowUpBody{PreciseOriginTimestamp: p.tai(sent)}}
	if b, err := ptp.Bytes(&f); err == nil {
		p.sendGeneral(b, to)
	}
}

// delayResp answers a Delay_Req, received at rx: to the client itself
// (multicast clients' too: the hybrid mode linuxptp and others follow).
func (p *PTP) delayResp(req *ptp.SyncDelayReq, from *net.UDPAddr, rx time.Time) {
	flags := req.FlagField & ptp.FlagUnicast
	d := ptp.DelayResp{Header: p.header(ptp.MessageDelayResp, delayRespLen, req.SequenceID, flags, 0),
		DelayRespBody: ptp.DelayRespBody{ReceiveTimestamp: p.tai(rx), RequestingPortIdentity: req.SourcePortIdentity}}
	d.CorrectionField = req.CorrectionField
	if b, err := ptp.Bytes(&d); err == nil {
		p.sendGeneral(b, from.IP)
	}
}

// grant is a unicast client's: what it is sent, how often, until when.
type grant struct {
	ip    net.IP
	logs  [3]int8      // announce, sync, delay_resp
	until [3]time.Time // zero: not granted
	next  [3]time.Time
}

// kinds are the messages granted, in grant's order.
var kinds = [3]ptp.MessageType{ptp.MessageAnnounce, ptp.MessageSync, ptp.MessageDelayResp}

func kindIndex(t ptp.MessageType) int {
	for i, k := range kinds {
		if k == t {
			return i
		}
	}
	return -1
}

// maxGrant is the longest a grant runs before the client must ask again.
const maxGrant = 1000 * time.Second

// negotiate answers a client's Signaling: unicast transmission granted
// (or refused: a grant of no time), or cancelled.
func (p *PTP) negotiate(sig *ptp.Signaling, from *net.UDPAddr) {
	var out []ptp.TLV
	now := time.Now()
	key := from.IP.String()
	p.mu.Lock()
	for _, t := range sig.TLVs {
		switch r := t.(type) {
		case *ptp.RequestUnicastTransmissionTLV:
			k := r.MsgTypeAndReserved.MsgType()
			i := kindIndex(k)
			dur := time.Duration(r.DurationField) * time.Second
			g := p.grants[key]
			ok := p.S.Unicast && i >= 0 && int8(r.LogInterMessagePeriod) >= p.S.MinLog && dur > 0
			if ok && g == nil && len(p.grants) >= p.S.MaxClients {
				ok = false
			}
			gr := &ptp.GrantUnicastTransmissionTLV{TLVHead: ptp.TLVHead{TLVType: ptp.TLVGrantUnicastTransmission, LengthField: 8},
				MsgTypeAndReserved: r.MsgTypeAndReserved, LogInterMessagePeriod: r.LogInterMessagePeriod}
			if ok {
				dur = min(dur, maxGrant)
				if g == nil {
					g = &grant{ip: from.IP}
					p.grants[key] = g
				}
				g.logs[i] = int8(r.LogInterMessagePeriod)
				g.until[i] = now.Add(dur)
				if g.next[i].IsZero() || g.next[i].Before(now) {
					g.next[i] = now
				}
				gr.DurationField = uint32(dur / time.Second)
				gr.Renewal = 1
			}
			out = append(out, gr)
		case *ptp.CancelUnicastTransmissionTLV:
			if g := p.grants[key]; g != nil {
				if i := kindIndex(r.MsgTypeAndFlags.MsgType()); i >= 0 {
					g.until[i] = time.Time{}
				}
			}
			out = append(out, &ptp.AcknowledgeCancelUnicastTransmissionTLV{
				TLVHead:         ptp.TLVHead{TLVType: ptp.TLVAcknowledgeCancelUnicastTransmission, LengthField: 2},
				MsgTypeAndFlags: r.MsgTypeAndFlags})
		}
	}
	p.mu.Unlock()
	if len(out) == 0 {
		return
	}
	n := sigLen
	for _, t := range out {
		switch t.(type) {
		case *ptp.GrantUnicastTransmissionTLV:
			n += 12
		default:
			n += 6
		}
	}
	resp := ptp.Signaling{Header: p.header(ptp.MessageSignaling, n, sig.SequenceID, ptp.FlagUnicast, 0x7f),
		TargetPortIdentity: sig.SourcePortIdentity, TLVs: out}
	if b, err := ptp.Bytes(&resp); err == nil {
		p.sendGeneral(b, from.IP)
	}
}

// schedule sends what is due: multicast announcements and syncs, and the
// unicast clients' as granted.
func (p *PTP) schedule(ctx context.Context) {
	t := time.NewTicker(time.Second / 32)
	defer t.Stop()
	var nextA, nextS time.Time
	group := net.ParseIP(ptpGroup)
	multicast := p.S.Multicast && p.S.Interface != ""
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if multicast {
				if !now.Before(nextA) {
					p.announce(group, p.nextSeq(0), false, p.S.AnnounceLog)
					nextA = now.Add(ptp.LogInterval(p.S.AnnounceLog).Duration())
				}
				if !now.Before(nextS) {
					p.sync(group, p.nextSeq(1), false, p.S.SyncLog)
					nextS = now.Add(ptp.LogInterval(p.S.SyncLog).Duration())
				}
			}
			type due struct {
				ip   net.IP
				kind int
				log  int8
			}
			var send []due
			p.mu.Lock()
			for k, g := range p.grants {
				live := false
				for i := range 2 { // announce and sync are sent; delay_resp answered
					if g.until[i].After(now) {
						live = true
						if !now.Before(g.next[i]) {
							send = append(send, due{g.ip, i, g.logs[i]})
							g.next[i] = g.next[i].Add(ptp.LogInterval(g.logs[i]).Duration())
							if g.next[i].Before(now) {
								g.next[i] = now
							}
						}
					}
				}
				if !live && !g.until[2].After(now) {
					delete(p.grants, k)
				}
			}
			p.mu.Unlock()
			// syncs first: an announcement going out at once delays them
			// in the kernel, and the stamps with them
			sort.SliceStable(send, func(i, j int) bool { return send[i].kind > send[j].kind })
			for _, d := range send {
				if d.kind == 0 {
					p.announce(d.ip, p.nextSeq(0), true, d.log)
				} else {
					p.sync(d.ip, p.nextSeq(1), true, d.log)
				}
			}
		}
	}
}

// nextSeq is the next sequence number of announcements (0) or syncs (1).
func (p *PTP) nextSeq(i int) uint16 {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.seq[i]
	p.seq[i]++
	return n
}

// Clients is how many unicast clients have grants.
func (p *PTP) Clients() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.grants)
}

// follow makes a client of the PTP server at host (unicast negotiation),
// before Run.
func (p *PTP) follow(host string) (*ptpUpstream, error) {
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("ptp %s: %v", host, err)
	}
	ip := ips[0]
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	u := &ptpUpstream{p: p, host: host, ip: ip, syncs: map[uint16]time.Time{}, reqs: map[uint16]time.Time{}}
	p.mu.Lock()
	if p.clients == nil {
		p.clients = map[string]*ptpUpstream{}
	}
	p.clients[ip.String()] = u
	p.mu.Unlock()
	return u, nil
}

// ptpUpstream is a client's of one PTP server.
type ptpUpstream struct {
	p    *PTP
	host string
	ip   net.IP

	mu       sync.Mutex
	ann      *ptp.Announce
	granted  time.Time // the sync grant's end
	syncs    map[uint16]time.Time // a Sync's arrival, by sequence, till its Follow_Up
	t1, t2   time.Time // the last sync's: when it went (TAI) and came
	reqSeq   uint16
	reqs     map[uint16]time.Time // a Delay_Req's sending, by sequence
	sigSeq   uint16
	samples  []Sample
}

// run asks for unicast transmission and keeps asking before it runs out.
func (u *ptpUpstream) run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var asked time.Time
	for {
		u.mu.Lock()
		g := u.granted
		u.mu.Unlock()
		// asked for 60 seconds: again when there are 20 left, or if no
		// answer came in 5
		if time.Until(g) < 20*time.Second && time.Since(asked) > 5*time.Second {
			u.request(60 * time.Second)
			asked = time.Now()
		}
		select {
		case <-ctx.Done():
			u.cancel()
			return
		case <-t.C:
		}
	}
}

func (u *ptpUpstream) request(d time.Duration) {
	var tlvs []ptp.TLV
	for _, k := range []struct {
		t   ptp.MessageType
		log int8
	}{{ptp.MessageAnnounce, 1}, {ptp.MessageSync, 0}, {ptp.MessageDelayResp, 0}} {
		tlvs = append(tlvs, &ptp.RequestUnicastTransmissionTLV{
			TLVHead:            ptp.TLVHead{TLVType: ptp.TLVRequestUnicastTransmission, LengthField: 6},
			MsgTypeAndReserved: ptp.NewUnicastMsgTypeAndFlags(k.t, 0), LogInterMessagePeriod: ptp.LogInterval(k.log),
			DurationField: uint32(d / time.Second)})
	}
	u.signal(tlvs, sigLen+3*10)
}

func (u *ptpUpstream) cancel() {
	var tlvs []ptp.TLV
	for _, k := range kinds {
		tlvs = append(tlvs, &ptp.CancelUnicastTransmissionTLV{
			TLVHead:         ptp.TLVHead{TLVType: ptp.TLVCancelUnicastTransmission, LengthField: 2},
			MsgTypeAndFlags: ptp.NewUnicastMsgTypeAndFlags(k, 0)})
	}
	u.signal(tlvs, sigLen+3*6)
}

func (u *ptpUpstream) signal(tlvs []ptp.TLV, n int) {
	u.mu.Lock()
	seq := u.sigSeq
	u.sigSeq++
	u.mu.Unlock()
	s := ptp.Signaling{Header: u.p.header(ptp.MessageSignaling, n, seq, ptp.FlagUnicast, 0x7f),
		TargetPortIdentity: ptp.DefaultTargetPortIdentity, TLVs: tlvs}
	if b, err := ptp.Bytes(&s); err == nil {
		u.p.sendGeneral(b, u.ip)
	}
}

// handle takes a packet from the server.
func (u *ptpUpstream) handle(b []byte, rx time.Time) {
	if b[4] != u.p.S.Domain {
		return
	}
	switch ptp.MessageType(b[0] & 0x0f) {
	case ptp.MessageAnnounce:
		a := &ptp.Announce{}
		if len(b) < announceLen {
			return
		}
		a.UnmarshalBinary(b) // a TLV not known is no matter: the body is read
		u.mu.Lock()
		u.ann = a
		u.mu.Unlock()
	case ptp.MessageSignaling:
		var s ptp.Signaling
		if s.UnmarshalBinary(b) != nil {
			return
		}
		for _, t := range s.TLVs {
			if g, ok := t.(*ptp.GrantUnicastTransmissionTLV); ok && g.MsgTypeAndReserved.MsgType() == ptp.MessageSync {
				u.mu.Lock()
				if g.DurationField == 0 {
					u.granted = time.Time{}
					u.p.logf("ptp %s: unicast refused", u.host)
				} else {
					u.granted = time.Now().Add(time.Duration(g.DurationField) * time.Second)
				}
				u.mu.Unlock()
			}
		}
	case ptp.MessageSync:
		var s ptp.SyncDelayReq
		if s.UnmarshalBinary(b) != nil {
			return
		}
		u.mu.Lock()
		defer u.mu.Unlock()
		if s.FlagField&ptp.FlagTwoStep == 0 {
			u.t1, u.t2 = s.OriginTimestamp.Time().Add(s.CorrectionField.Duration()), rx
			go u.delayReq()
			return
		}
		u.syncs[s.SequenceID] = rx
		for k := range u.syncs {
			if k != s.SequenceID && s.SequenceID-k < 0x8000 && s.SequenceID-k > 8 {
				delete(u.syncs, k)
			}
		}
	case ptp.MessageFollowUp:
		var f ptp.FollowUp
		if f.UnmarshalBinary(b) != nil {
			return
		}
		u.mu.Lock()
		defer u.mu.Unlock()
		t2, ok := u.syncs[f.SequenceID]
		if !ok {
			return
		}
		delete(u.syncs, f.SequenceID)
		u.t1, u.t2 = f.PreciseOriginTimestamp.Time().Add(f.CorrectionField.Duration()), t2
		go u.delayReq()
	case ptp.MessageDelayResp:
		var d ptp.DelayResp
		if d.UnmarshalBinary(b) != nil {
			return
		}
		if d.RequestingPortIdentity.ClockIdentity != u.p.id {
			return
		}
		u.mu.Lock()
		defer u.mu.Unlock()
		t3, ok := u.reqs[d.SequenceID]
		if !ok || u.t1.IsZero() {
			return
		}
		delete(u.reqs, d.SequenceID)
		t4 := d.ReceiveTimestamp.Time().Add(-d.CorrectionField.Duration())
		u.measure(t3, t4)
	}
}

// delayReq sends a Delay_Req, after a sync.
func (u *ptpUpstream) delayReq() {
	u.mu.Lock()
	seq := u.reqSeq
	u.reqSeq++
	u.mu.Unlock()
	r := ptp.SyncDelayReq{Header: u.p.header(ptp.MessageDelayReq, syncLen, seq, ptp.FlagUnicast, 0x7f)}
	b, err := ptp.Bytes(&r)
	if err != nil {
		return
	}
	t3, err := u.p.sendEvent(b, u.ip)
	if err != nil {
		return
	}
	u.mu.Lock()
	u.reqs[seq] = t3
	for k := range u.reqs {
		if seq-k > 8 {
			delete(u.reqs, k)
		}
	}
	u.mu.Unlock()
}

// measure makes a sample of the last sync and a delay request's answer
// (t1, t4 the server's, TAI; t2, t3 the system clock's), held.
func (u *ptpUpstream) measure(t3, t4 time.Time) {
	utc := time.Duration(u.p.S.UTCOffset) * time.Second
	stratum, refid, leap := uint8(1), refIP(u.ip), uint8(0)
	src := SourcePTP
	if a := u.ann; a != nil {
		if a.FlagField&ptp.FlagCurrentUtcOffsetValid != 0 {
			utc = time.Duration(a.CurrentUTCOffset) * time.Second
		}
		switch a.TimeSource {
		case ptp.TimeSourceAtomicClock, ptp.TimeSourceGNSS, ptp.TimeSourceTerrestrialRadio, ptp.TimeSourceSerialTimeCode:
			if a.GrandmasterClockQuality.ClockClass <= 7 {
				stratum, refid = 0, refASCII("PTP")
			}
		}
		if a.GrandmasterClockQuality.ClockClass >= 248 && a.FlagField&ptp.FlagTimeTraceable == 0 {
			stratum = 2
		}
		switch {
		case a.FlagField&ptp.FlagLeap61 != 0:
			leap = 1
		case a.FlagField&ptp.FlagLeap59 != 0:
			leap = 2
		}
	}
	ms := u.t2.Add(utc).Sub(u.t1) // master to slave: delay + our clock's error
	sm := t4.Sub(t3.Add(utc))     // slave to master: delay - the error
	s := Sample{Source: "ptp " + u.host, Offset: -(ms - sm) / 2, Delay: ms + sm, Stratum: stratum,
		RefID: refid, Leap: leap, TimeSrc: src, When: time.Now()}
	if s.Delay < 0 {
		s.Delay = 0
	}
	u.samples = append(u.samples, s)
	if len(u.samples) > 16 {
		u.samples = u.samples[len(u.samples)-16:]
	}
}

// best is the recent sample the shortest round trip had.
func (u *ptpUpstream) best(within time.Duration) (Sample, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	var recent []Sample
	for _, s := range u.samples {
		if time.Since(s.When) < within {
			recent = append(recent, s)
		}
	}
	if len(recent) == 0 {
		if u.granted.IsZero() {
			return Sample{}, errors.New("no unicast grant (yet)")
		}
		return Sample{}, errors.New("no measurements")
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].Delay < recent[j].Delay })
	return recent[0], nil
}
