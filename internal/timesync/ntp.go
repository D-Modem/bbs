package timesync

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/facebook/time/ntp/ntske"
	ntp "github.com/facebook/time/ntp/protocol"
	"github.com/facebook/time/ntp/protocol/nts"
)

// NTP's modes, and the header's first byte for each.
const (
	modeClient = 3
	modeServer = 4
	version    = 4
)

func settings(leap, vn, mode uint8) uint8 { return leap<<6 | vn<<3 | mode }

// ntpShort is a duration in NTP's short format (16.16 seconds).
func ntpShort(d time.Duration) uint32 {
	if d < 0 {
		d = 0
	}
	v := d.Seconds() * 65536
	if v > 0xffffffff {
		return 0xffffffff
	}
	return uint32(v)
}

func fromShort(v uint32) time.Duration {
	return time.Duration(float64(v) / 65536 * float64(time.Second))
}

// precision is what the server tells of its clock's (2^-20 s: a
// microsecond, a Go time.Now's).
const precision = -20

// NTPServer answers NTP on UDP, NTS-secured requests too when it has the
// NTS-KE server's keys.
type NTPServer struct {
	Clock  *Clock
	Keys   ntske.Keystore       // nil: NTS-secured requests answered as plain ones
	Refuse func(net.IP) bool    // nil: none refused
	Rate   float64              // requests a second an address may make (0: no limit)
	Burst  int                  // and at once
	Log    func(string, ...any) // nil: none

	lim limiter
}

// Serve answers on c until ctx is done.
func (s *NTPServer) Serve(ctx context.Context, c *net.UDPConn) error {
	context.AfterFunc(ctx, func() { c.Close() })
	s.lim = limiter{rate: s.Rate, burst: float64(max(s.Burst, 1))}
	buf := make([]byte, 1500)
	for {
		n, addr, err := c.ReadFromUDPAddrPort(buf)
		rx := s.Clock.Now()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			continue
		}
		ip := net.IP(addr.Addr().AsSlice())
		if s.Refuse != nil && s.Refuse(ip) {
			continue
		}
		if s.Rate > 0 && !s.lim.allow(addr.Addr().String(), rx) {
			continue
		}
		out := s.answer(buf[:n], rx)
		if out != nil {
			c.WriteToUDPAddrPort(out, addr)
		}
	}
}

// answer is the reply to a request received at rx (nil: none).
func (s *NTPServer) answer(b []byte, rx time.Time) []byte {
	var req ntp.Packet
	if len(b) < ntp.PacketSizeBytes || req.UnmarshalBinary(b) != nil {
		return nil
	}
	vn, mode := req.Settings>>3&7, req.Settings&7
	if mode != modeClient || vn < 1 || vn > 4 {
		return nil // not a client's (control and private modes are not answered)
	}
	r := s.Clock.Ref()
	resp := &ntp.Packet{Settings: settings(r.Leap, vn, modeServer), Stratum: r.Stratum, Poll: req.Poll,
		Precision: precision, RootDelay: ntpShort(r.RootDelay), RootDispersion: ntpShort(r.RootDisp),
		ReferenceID: r.RefID, OrigTimeSec: req.TxTimeSec, OrigTimeFrac: req.TxTimeFrac}
	resp.RefTimeSec, resp.RefTimeFrac = ntp.Time(r.Updated)
	resp.RxTimeSec, resp.RxTimeFrac = ntp.Time(rx)
	// the transmit time is set before an NTS authenticator is sealed: it
	// is what it vouches for
	resp.TxTimeSec, resp.TxTimeFrac = ntp.Time(s.Clock.Now())
	if ntp.HasNTSField(req.ExtensionFields) && s.Keys != nil && vn == version {
		if err := secure(s.Keys, &req, resp); err != nil {
			// an NTS NAK (RFC 8915 5.7): a kiss, the request's unique ID back
			nak := &ntp.Packet{Settings: settings(3, vn, modeServer), Stratum: 0, Poll: req.Poll,
				Precision: precision, ReferenceID: refASCII("NTSN"),
				OrigTimeSec: req.TxTimeSec, OrigTimeFrac: req.TxTimeFrac}
			for _, ef := range req.ExtensionFields {
				if ef.Type == ntp.UniqueIdentifier {
					nak.ExtensionFields = []ntp.ExtensionField{ef}
					break
				}
			}
			out, _ := nak.Bytes()
			return out
		}
	}
	out, err := resp.Bytes()
	if err != nil {
		return nil
	}
	return out
}

// maxCookies is the most fresh cookies one response carries.
const maxCookies = 8

// secure checks an NTS-secured request and puts the unique ID and the
// authenticator, with fresh cookies sealed in it, on the response (its
// header complete).
func secure(ks ntske.Keystore, req, resp *ntp.Packet) error {
	var uid, cookie []byte
	auth, placeholders := -1, 0
	for i, ef := range req.ExtensionFields {
		switch ef.Type {
		case ntp.UniqueIdentifier:
			if uid != nil || cookie != nil || len(ef.Body) < nts.MinUniqueIdentifierLen {
				return errors.New("nts: unique identifier")
			}
			uid = ef.Body
		case ntp.NTSCookie:
			if uid == nil || cookie != nil {
				return errors.New("nts: cookie out of order")
			}
			cookie = ef.Body
		case ntp.NTSCookiePlaceholder:
			// as long as a cookie: so a response is never the larger
			if cookie != nil && len(ef.Body) == len(cookie) {
				placeholders++
			}
		case ntp.NTSAuthenticator:
			auth = i
		}
		if auth >= 0 {
			break
		}
	}
	if uid == nil || cookie == nil || auth < 0 {
		return errors.New("nts: incomplete")
	}
	aeadID, c2s, s2c, err := ks.OpenCookie(cookie)
	if err != nil {
		return err
	}
	ad, err := req.AssociatedData(auth)
	if err != nil {
		return err
	}
	in, err := nts.NewAEAD(aeadID, c2s)
	if err != nil {
		return err
	}
	if _, err := nts.OpenAuthenticator(in, ad, req.ExtensionFields[auth]); err != nil {
		return err
	}
	var fresh []ntp.ExtensionField
	for range min(1+placeholders, maxCookies) {
		c, err := ks.SealCookie(aeadID, c2s, s2c)
		if err != nil {
			return err
		}
		fresh = append(fresh, nts.NewCookie(c))
	}
	sealed, err := ntp.MarshalExtensionFields(fresh)
	if err != nil {
		return err
	}
	resp.ExtensionFields = []ntp.ExtensionField{nts.NewUniqueIdentifier(uid)}
	rad, err := resp.AssociatedData(1)
	if err != nil {
		return err
	}
	outAEAD, err := nts.NewAEAD(aeadID, s2c)
	if err != nil {
		return err
	}
	a, err := nts.SealAuthenticator(outAEAD, rad, sealed)
	if err != nil {
		return err
	}
	resp.ExtensionFields = append(resp.ExtensionFields, a)
	return nil
}

// limiter limits each address to rate requests a second, burst at once
// (a token bucket each); forgotten when full again.
type limiter struct {
	rate, burst float64
	mu          sync.Mutex
	b           map[string]*bucket
	swept       time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

// maxAddresses bounds what the limiter remembers: past it, newcomers are
// let through unlimited until the next sweep.
const maxAddresses = 1 << 16

func (l *limiter) allow(addr string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.b == nil {
		l.b = map[string]*bucket{}
	}
	if now.Sub(l.swept) > time.Minute {
		full := time.Duration(l.burst / l.rate * float64(time.Second))
		for k, v := range l.b {
			if now.Sub(v.at) > full {
				delete(l.b, k)
			}
		}
		l.swept = now
	}
	bk := l.b[addr]
	if bk == nil {
		if len(l.b) >= maxAddresses {
			return true
		}
		bk = &bucket{tokens: l.burst, at: now}
		l.b[addr] = bk
	}
	bk.tokens = min(l.burst, bk.tokens+now.Sub(bk.at).Seconds()*l.rate)
	bk.at = now
	if bk.tokens < 1 {
		return false
	}
	bk.tokens--
	return true
}

// query asks an NTP server the time once (NTS-secured, given a session):
// the sample, and the server's response.
func query(ctx context.Context, addr string, sess *ntsSession) (Sample, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return Sample{}, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	// the transmit time is random, not the clock's: it is what an answer
	// is matched by, and it tells nobody the time here (RFC 9109)
	var txs [8]byte
	rand.Read(txs[:])
	req := ntp.Packet{Settings: settings(0, version, modeClient), Poll: 6, Precision: precision}
	req.TxTimeSec = uint32(txs[0])<<24 | uint32(txs[1])<<16 | uint32(txs[2])<<8 | uint32(txs[3])
	req.TxTimeFrac = uint32(txs[4])<<24 | uint32(txs[5])<<16 | uint32(txs[6])<<8 | uint32(txs[7])
	var out, uid []byte
	if sess != nil {
		cookie, ok := sess.take()
		if !ok {
			return Sample{}, errNoCookies
		}
		uid = make([]byte, 32)
		rand.Read(uid)
		out, err = nts.BuildNTSRequest(req, nts.RequestParams{AEAD: sess.aead, C2S: sess.c2s, Cookie: cookie,
			UniqueID: uid, Placeholders: sess.wanted()})
	} else {
		out, err = req.Bytes()
	}
	if err != nil {
		return Sample{}, err
	}
	t1 := time.Now()
	if _, err := conn.Write(out); err != nil {
		return Sample{}, err
	}
	buf := make([]byte, 1500)
	for {
		n, err := conn.Read(buf)
		t4 := time.Now()
		if err != nil {
			return Sample{}, err
		}
		var resp *ntp.Packet
		if sess != nil {
			p, cookies, err := nts.VerifyNTSResponse(buf[:n], sess.aead, sess.s2c, uid)
			if err != nil {
				if k, kerr := ntp.BytesToPacket(buf[:n]); kerr == nil && k.Stratum == 0 && k.ReferenceID == refASCII("NTSN") {
					sess.lost()
					return Sample{}, errors.New("the server refused the cookie (NTS NAK)")
				}
				continue // not the answer, or forged
			}
			sess.give(cookies)
			resp = p
		} else {
			p, err := ntp.BytesToPacket(buf[:n])
			if err != nil {
				continue
			}
			resp = p
		}
		if resp.OrigTimeSec != req.TxTimeSec || resp.OrigTimeFrac != req.TxTimeFrac {
			continue
		}
		if resp.Settings&7 != modeServer {
			continue
		}
		if resp.Stratum == 0 {
			return Sample{}, fmt.Errorf("kissed off: %s", RefIDString(resp.ReferenceID, 0))
		}
		leap := resp.Settings >> 6
		if leap == 3 || resp.Stratum >= unsynced {
			return Sample{}, errors.New("the server is not synchronised")
		}
		if resp.TxTimeSec == 0 && resp.TxTimeFrac == 0 {
			return Sample{}, errors.New("no transmit time")
		}
		t2 := ntp.Unix(resp.RxTimeSec, resp.RxTimeFrac)
		t3 := ntp.Unix(resp.TxTimeSec, resp.TxTimeFrac)
		delay := time.Duration(ntp.RoundTripDelay(t1, t2, t3, t4))
		if delay < 0 {
			delay = 0
		}
		ip := conn.RemoteAddr().(*net.UDPAddr).IP
		return Sample{Offset: time.Duration(ntp.Offset(t1, t2, t3, t4)), Delay: delay, Stratum: resp.Stratum,
			RefID: refIP(ip), Leap: leap, RootDelay: fromShort(resp.RootDelay),
			RootDisp: fromShort(resp.RootDispersion), TimeSrc: SourceNTP, When: t4}, nil
	}
}

var errNoCookies = errors.New("nts: no cookies left")
