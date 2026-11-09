package oscar

import (
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// families served on the BOS connection
var bosFamilies = []uint16{0x0001, 0x0002, 0x0003, 0x0004, 0x0006, 0x0008, 0x0009, 0x000a, 0x000b, 0x0013, 0x0015}

// default family versions, for clients that do not say theirs
var familyVersions = map[uint16]uint16{0x0001: 4, 0x0002: 1, 0x0003: 1, 0x0004: 1, 0x0006: 1, 0x0008: 1,
	0x0009: 1, 0x000a: 1, 0x000b: 1, 0x000d: 1, 0x000e: 1, 0x0013: 4, 0x0015: 1}

// session is a BOS connection: a client signed on, on a node of its own.
type session struct {
	s        *Server
	fc       *flapConn
	host     string
	u        *store.User
	sn       string // as signed on: the name, or the ICQ number
	icq      bool
	clientID string
	n        *bbs.Node
	signon   time.Time

	todo chan func()
	done chan struct{}
	once sync.Once

	mu       sync.Mutex
	isReady  bool              // the client said it is online
	versions map[uint16]uint16 // the families' versions it told
	listed   map[string]string // client-side buddies: ident → as named
	bagged   map[string]string // feedbag buddies
	watch    map[int64]string  // the buddies' accounts → as the client names them
	profile  string
	profMIME string
	away     string
	awayMIME string
	caps     [][]byte
	idle     time.Time // since when (zero: not idle)
	status   uint32    // ICQ's status
	pdMode   byte      // permit/deny mode (1 all ... 5 buddies only)
	permit   map[string]bool
	deny     map[string]bool
	bagOn    bool                // the feedbag is in use
	items    []store.FeedbagItem // the feedbag as the client last had it
	bagSent  bool                // the client asked for the feedbag
	noted    map[int64]bool      // told that these are away (their IMs kept)
	chats    map[*chatConn]bool
	nav      []*flapConn // service connections without rooms (chat nav)
}

// permit/deny modes
const (
	pdPermitAll  = 1
	pdDenyAll    = 2
	pdPermitSome = 3
	pdDenySome   = 4
	pdBuddies    = 5
)

// ICQ status bits
const (
	icqAway      = 0x0001
	icqDND       = 0x0002
	icqNA        = 0x0004
	icqOccupied  = 0x0010
	icqFree      = 0x0020
	icqInvisible = 0x0100
)

// bos serves a BOS connection, cookie k's.
func (s *Server) bos(fc *flapConn, host string, k *cookie) {
	u, err := s.B.Store.UserByID(k.user.ID)
	if err != nil || u.Locked {
		fc.signoff(nil)
		return
	}
	via := "aim"
	if k.icq {
		via = "icq"
	}
	ss := &session{s: s, fc: fc, host: host, u: u, sn: k.sn, icq: k.icq, clientID: k.clientID,
		signon: time.Now(), todo: make(chan func(), queueLen), done: make(chan struct{}),
		versions: map[uint16]uint16{}, listed: map[string]string{}, bagged: map[string]string{},
		watch: map[int64]string{}, permit: map[string]bool{}, deny: map[string]bool{},
		pdMode: pdPermitAll, chats: map[*chatConn]bool{}, noted: map[int64]bool{}}
	n, err := s.B.Enter(&bbs.Caller{Via: via, Listener: "oscar", Remote: host, Source: "ip:" + host,
		Terminal: k.clientID}, ss.close)
	if err != nil {
		var o tlvs
		o.addU16(0x0009, 0x0001) // the system is busy
		fc.signoff(o)
		return
	}
	ss.n = n
	n.SetUser(u)
	n.Doing(strings.ToUpper(via))
	s.register(ss)
	s.B.Log.Printf("node %d: %s on %s (%s, %s)", n.Num, u.Name, strings.ToUpper(via), host, k.clientID)
	defer func() {
		ss.close()
		s.unregister(ss)
		n.Leave()
		s.B.Log.Printf("node %d: gone (%s)", n.Num, via)
		s.changed(u)
	}()
	go ss.events()
	var b buf
	for _, f := range bosFamilies {
		b.u16(f)
	}
	ss.snac(0x0001, 0x0003, 0, b)
	ss.serve()
}

// closed reports that the session is over.
func (ss *session) closed() bool {
	select {
	case <-ss.done:
		return true
	default:
		return false
	}
}

// close ends the session and its service connections.
func (ss *session) close() {
	ss.once.Do(func() {
		close(ss.done)
		ss.fc.c.Close()
		ss.mu.Lock()
		for c := range ss.chats {
			c.fc.c.Close()
		}
		for _, f := range ss.nav {
			f.c.Close()
		}
		ss.mu.Unlock()
	})
}

// ready: the client has said it is online.
func (ss *session) ready() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.isReady
}

// do has the session's goroutine do f (dropped if too much waits).
func (ss *session) do(f func()) {
	select {
	case ss.todo <- f:
	case <-ss.done:
	default:
		ss.s.B.Log.Printf("node %d: oscar: too much waiting; dropped", ss.n.Num)
	}
}

// snac sends a SNAC on the BOS connection.
func (ss *session) snac(fam, sub uint16, req uint32, body []byte) {
	if err := ss.fc.snac(fam, sub, 0, req, body); err != nil {
		ss.close()
	}
}

// fail answers a request with its family's error.
func (ss *session) fail(fam uint16, req uint32, code uint16) {
	var b buf
	b.u16(code)
	ss.snac(fam, 0x0001, req, b)
}

// serve reads the client's frames until it goes.
func (ss *session) serve() {
	for {
		ss.fc.c.SetReadDeadline(time.Now().Add(idleTimeout))
		f, err := ss.fc.read()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !ss.closed() {
				ss.s.B.Log.Printf("node %d: oscar: %v", ss.n.Num, err)
			}
			return
		}
		switch f.ch {
		case chanSignoff:
			return
		case chanData:
			h, body, err := parseSNAC(f.data)
			if err != nil {
				continue
			}
			ss.dispatch(h, body)
		}
	}
}

// events gives the client what its node is told, and does what the
// server asks of the session, until it goes.
func (ss *session) events() {
	for {
		select {
		case <-ss.done:
			return
		case f := <-ss.todo:
			f()
		case <-ss.n.Wake():
			if !ss.ready() {
				continue // held until the client is ready (online drains them)
			}
			ss.drain()
		}
	}
}

// dispatch handles a SNAC on the BOS connection.
func (ss *session) dispatch(h snacHeader, body []byte) {
	r := &rd{p: body}
	switch h.fam {
	case 0x0001:
		ss.oservice(h, r, ss.fc, 0)
	case 0x0002:
		ss.locate(h, r)
	case 0x0003:
		ss.buddy(h, r)
	case 0x0004:
		ss.icbm(h, r)
	case 0x0009:
		ss.permitDeny(h, r)
	case 0x000a: // user lookup by e-mail: there are no addresses
		ss.fail(0x000a, h.req, 0x0014)
	case 0x000b:
		if h.sub == 0x0003 { // a stats report
			ss.snac(0x000b, 0x0004, h.req, nil)
		}
	case 0x0013:
		ss.feedbag(h, r)
	case 0x0015:
		ss.icqMeta(h, r)
	default:
		ss.fail(h.fam, h.req, 0x0008)
	}
}

// version is the version of a family the client told (or the default).
func (ss *session) version(fam uint16) uint16 {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if v, ok := ss.versions[fam]; ok {
		return v
	}
	return 0
}

// oservice is family 1 on a connection (the BOS's, or a service's: then
// svc is its family).
func (ss *session) oservice(h snacHeader, r *rd, fc *flapConn, svc uint16) {
	send := func(sub uint16, body []byte) {
		if err := fc.snac(0x0001, sub, 0, h.req, body); err != nil {
			fc.c.Close()
		}
	}
	switch h.sub {
	case 0x0002: // the client is ready
		if svc == 0 {
			ss.online()
		}
	case 0x0004: // a service, elsewhere
		if svc == 0 {
			ss.serviceRequest(h, r)
		} else {
			send(0x0001, []byte{0, 0x05})
		}
	case 0x0006: // rate classes
		send(0x0007, ss.rates())
	case 0x0008, 0x0016, 0x0014, 0x000c: // rates acked, no-op, privacy flags, pause acked
	case 0x000e: // who am I
		send(0x000f, ss.selfInfo())
	case 0x0011: // idle
		secs := r.u32()
		ss.mu.Lock()
		if secs == 0 {
			ss.idle = time.Time{}
		} else {
			ss.idle = time.Now().Add(-time.Duration(secs) * time.Second)
		}
		ss.mu.Unlock()
		ss.s.changed(ss.u)
	case 0x0017: // the client's versions: ours are the same
		var b buf
		for r.left() >= 4 {
			fam, v := r.u16(), r.u16()
			ss.mu.Lock()
			ss.versions[fam] = v
			ss.mu.Unlock()
			b.u16(fam)
			b.u16(v)
		}
		send(0x0018, b)
		var m tlvs
		m.addStr(0x000b, ss.s.B.Conf().Name)
		var mb buf
		mb.u16(0x0004)
		mb.raw(m.bytes())
		fc.snac(0x0001, 0x0013, 0, 0, mb)
	case 0x001e: // status (ICQ), status message
		t := parseTLVs(r.rest())
		if st, ok := t.u32(0x0006); ok {
			ss.mu.Lock()
			ss.status = st & 0xffff
			ss.mu.Unlock()
			ss.n.Doing(ss.doing())
		}
		send(0x000f, ss.selfInfo())
		ss.s.changed(ss.u)
	default:
		send(0x0001, []byte{0, 0x08})
	}
}

// rates are the rate classes: one, holding every SNAC, generous.
func (ss *session) rates() []byte {
	var b buf
	b.u16(1)
	b.u16(1)    // class 1
	b.u32(80)   // window
	b.u32(2500) // clear
	b.u32(2000) // alert
	b.u32(1500) // limit
	b.u32(800)  // disconnect
	b.u32(6000) // current
	b.u32(6000) // max
	if ss.version(0x0001) > 1 {
		b.u32(0)
		b.u8(0)
	}
	var pairs [][2]uint16
	for fam, top := range map[uint16]uint16{0x01: 0x21, 0x02: 0x1c, 0x03: 0x0c, 0x04: 0x17, 0x06: 0x08, 0x08: 0x03,
		0x09: 0x0b, 0x0a: 0x03, 0x0b: 0x05, 0x0d: 0x0a, 0x0e: 0x0a, 0x13: 0x1c, 0x15: 0x03, 0x17: 0x07} {
		for sub := uint16(1); sub <= top; sub++ {
			pairs = append(pairs, [2]uint16{fam, sub})
		}
	}
	b.u16(1)
	b.u16(uint16(len(pairs)))
	for _, p := range pairs {
		b.u16(p[0])
		b.u16(p[1])
	}
	return b
}

// online is the client ready: its buddies online told, and it to those
// watching.
func (ss *session) online() {
	ss.mu.Lock()
	ss.isReady = true
	ss.mu.Unlock()
	ss.n.Doing(ss.doing())
	ss.do(ss.allArrived)
	ss.do(ss.drain)
	if !ss.icq { // ICQ asks for its offline messages itself
		ss.do(func() {
			for _, e := range ss.n.Kept() {
				ss.event(e)
			}
		})
	}
	ss.s.changed(ss.u)
}

// drain gives the client the node's events waiting.
func (ss *session) drain() {
	for _, e := range ss.n.Pending() {
		ss.event(e)
	}
}

// doing is what who's online says of the session.
func (ss *session) doing() string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	what := "AIM"
	if ss.icq {
		what = "ICQ"
	}
	if ss.away != "" || ss.status&(icqAway|icqNA|icqDND|icqOccupied) != 0 {
		what += " (away)"
	}
	return what
}

// selfInfo is the client's own user info (SNAC 1/F).
func (ss *session) selfInfo() []byte {
	b, _ := ss.s.userInfo(ss, ss.u, true)
	return b
}

// serviceRequest is SNAC 1/4: a service's connection (chat navigation, a
// chat room), here, with a cookie.
func (ss *session) serviceRequest(h snacHeader, r *rd) {
	fam := r.u16()
	t := parseTLVs(r.rest())
	k := &cookie{user: ss.u, sn: ss.sn, icq: ss.icq, service: fam, sess: ss}
	switch fam {
	case 0x000d:
	case 0x000e:
		v, ok := t.get(0x0001)
		if !ok {
			ss.fail(0x0001, h.req, 0x0005)
			return
		}
		rr := &rd{p: v}
		k.exchange = rr.u16()
		k.room = roomOfCookie(rr.str8())
		if rr.bad || k.room == "" {
			ss.fail(0x0001, h.req, 0x0005)
			return
		}
	default:
		ss.fail(0x0001, h.req, 0x0005)
		return
	}
	var o tlvs
	o.addU16(0x000d, fam)
	o.addStr(0x0005, ss.s.reconnectHost(ss.fc.c))
	o.add(0x0006, ss.s.newCookie(k))
	if t.has(0x008c) {
		o.addU8(0x008e, 0) // no SSL
	}
	ss.snac(0x0001, 0x0005, h.req, o.bytes())
}

// newICBMCookie is a message's cookie.
func newICBMCookie() []byte {
	b := make([]byte, 8)
	rand.Read(b)
	return b
}

// nameFor is how the session names an account: its ICQ number to an ICQ
// client, else its name (as the client has it on its list, if it does).
func (ss *session) nameFor(u *store.User) string {
	if ss.icq {
		return ss.s.uinOf(u.ID)
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if sn, ok := ss.watch[u.ID]; ok && ident(sn) == ident(u.Name) {
		return sn
	}
	return u.Name
}

// systemName is the BBS's own, to the session.
func (ss *session) systemName() string {
	if ss.icq {
		return itoa(int(ss.s.S.UINBase))
	}
	return ss.s.S.System
}
