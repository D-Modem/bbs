// Package oldtls is a small TLS 1.0 server for the clients Go's crypto/tls
// can't take: those that open with an SSL 2.0-format hello, as AIM 6.2 to
// 7.5 (their NSS) do.  Only that first message is SSL 2.0's; the rest is
// TLS 1.0, which crypto/tls still speaks, but TLS's Finished messages sign
// the hello's very bytes, so it can't be rewritten for crypto/tls.
//
// It serves TLS 1.0 only, with RSA key exchange (so an RSA certificate)
// and AES-CBC, 3DES-CBC or RC4: no resumption, renegotiation, client
// certificates or extensions beyond secure renegotiation's.  These are
// old ciphers for old clients; anything newer belongs on crypto/tls.
package oldtls

import (
	"bufio"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/subtle"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const (
	version10 = 0x0301

	recCCS       = 20
	recAlert     = 21
	recHandshake = 22
	recData      = 23

	hsClientHello       = 1
	hsServerHello       = 2
	hsCertificate       = 11
	hsServerDone        = 14
	hsClientKeyExchange = 16
	hsFinished          = 20

	maxPlain  = 16384
	maxRecord = maxPlain + 2048
	maxHSMsg  = 1 << 16

	alertCloseNotify         = 0
	alertUnexpected          = 10
	alertBadMAC              = 20
	alertOverflow            = 22
	alertHandshakeFailure    = 40
	alertDecode              = 50
	alertDecrypt             = 51
	alertProtocolVersion     = 70
	alertInternal            = 80
	alertNoRenegotiation     = 100
	levelWarning, levelFatal = 1, 2
)

// Config is a server's.
type Config struct {
	// GetCertificate gives the certificate (its key must be RSA).
	GetCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)
}

// IsSSLv2Hello reports whether a connection's first bytes (three or
// more) are an SSL 2.0-format ClientHello.
func IsSSLv2Hello(b []byte) bool { return len(b) >= 3 && b[0]&0x80 != 0 && b[2] == 1 }

// Conn is a TLS 1.0 connection, server side.
type Conn struct {
	net.Conn
	cfg *Config
	br  *bufio.Reader

	hsMu   sync.Mutex
	hsDone bool
	hsErr  error
	up     atomic.Bool
	suite  uint16

	hs          []byte // handshake bytes read, not yet taken
	md5h, sha1h hash.Hash

	inMu    sync.Mutex
	in      half
	data    []byte
	readErr error

	outMu sync.Mutex
	out   half
}

// Server is c served as TLS 1.0; the handshake comes with the first Read
// or Write, or Handshake.
func Server(c net.Conn, cfg *Config) *Conn {
	return &Conn{Conn: c, cfg: cfg, br: bufio.NewReader(c)}
}

// CipherSuite is the suite agreed (0 before the handshake).
func (c *Conn) CipherSuite() uint16 {
	c.hsMu.Lock()
	defer c.hsMu.Unlock()
	return c.suite
}

// tlsError is an error that sends an alert.
type tlsError struct {
	alert uint8
	msg   string
}

func (e *tlsError) Error() string { return "oldtls: " + e.msg }

func fail(alert uint8, format string, a ...any) error {
	return &tlsError{alert, fmt.Sprintf(format, a...)}
}

// alertFor sends err's alert, if it has one.
func (c *Conn) alertFor(err error) {
	var te *tlsError
	if errors.As(err, &te) && te.alert != 0 {
		c.sendAlert(levelFatal, te.alert)
	}
}

func (c *Conn) sendAlert(level, desc uint8) {
	c.writeRecord(recAlert, []byte{level, desc})
}

// alertIn is the error for an alert the client sent.
func alertIn(data []byte) error {
	if len(data) != 2 {
		return fail(alertDecode, "bad alert")
	}
	if data[1] == alertCloseNotify {
		return io.EOF
	}
	return fmt.Errorf("oldtls: alert %d from the client", data[1])
}

func (c *Conn) writeRecord(typ uint8, data []byte) error {
	c.outMu.Lock()
	defer c.outMu.Unlock()
	var buf []byte
	for len(data) > 0 || buf == nil {
		n := min(len(data), maxPlain)
		buf = append(buf, c.out.seal(typ, data[:n])...)
		data = data[n:]
	}
	_, err := c.Conn.Write(buf)
	return err
}

func (c *Conn) readRecord() (uint8, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
		return 0, nil, err
	}
	if hdr[1] != 3 {
		return 0, nil, fail(alertProtocolVersion, "record version %x", hdr[1:3])
	}
	n := int(binary.BigEndian.Uint16(hdr[3:]))
	if n > maxRecord {
		return 0, nil, fail(alertOverflow, "record of %d bytes", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c.br, body); err != nil {
		return 0, nil, err
	}
	plain, ok := c.in.open(hdr[0], body)
	if !ok {
		return 0, nil, fail(alertBadMAC, "bad record MAC")
	}
	if len(plain) > maxPlain {
		return 0, nil, fail(alertOverflow, "record of %d bytes", len(plain))
	}
	return hdr[0], plain, nil
}

// readHandshake is the next handshake message, whole (with its header).
func (c *Conn) readHandshake() ([]byte, error) {
	for {
		if len(c.hs) >= 4 {
			n := int(c.hs[1])<<16 | int(c.hs[2])<<8 | int(c.hs[3])
			if n > maxHSMsg {
				return nil, fail(alertDecode, "handshake message of %d bytes", n)
			}
			if len(c.hs) >= 4+n {
				m := slices.Clone(c.hs[:4+n])
				c.hs = c.hs[4+n:]
				return m, nil
			}
		}
		typ, data, err := c.readRecord()
		if err != nil {
			return nil, err
		}
		switch typ {
		case recHandshake:
			c.hs = append(c.hs, data...)
		case recAlert:
			return nil, alertIn(data)
		default:
			return nil, fail(alertUnexpected, "record type %d in the handshake", typ)
		}
	}
}

func (c *Conn) hash(m []byte) {
	c.md5h.Write(m)
	c.sha1h.Write(m)
}

// finished is a Finished message's verify data, for the handshake so far.
func (c *Conn) finished(ms []byte, label string) []byte {
	return prf10(ms, label, append(c.md5h.Sum(nil), c.sha1h.Sum(nil)...), 12)
}

// Handshake runs the handshake, if it hasn't run.
func (c *Conn) Handshake() error {
	c.hsMu.Lock()
	defer c.hsMu.Unlock()
	if c.hsDone {
		return c.hsErr
	}
	c.hsDone = true
	if c.hsErr = c.serverHandshake(); c.hsErr != nil {
		c.alertFor(c.hsErr)
	} else {
		c.up.Store(true)
	}
	return c.hsErr
}

type clientHello struct {
	version     uint16
	random      []byte
	suites      []uint16
	sni         string
	secureReneg bool
}

func (c *Conn) serverHandshake() error {
	c.md5h, c.sha1h = md5.New(), sha1.New()
	ch, err := c.readClientHello()
	if err != nil {
		return err
	}
	if ch.version < version10 {
		return fail(alertProtocolVersion, "the client offers only SSL %x", ch.version)
	}
	cert, err := c.cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: ch.sni, CipherSuites: ch.suites,
		SupportedVersions: []uint16{tls.VersionTLS10}, Conn: c.Conn})
	if err != nil {
		return fail(alertInternal, "certificate: %v", err)
	}
	key, ok := cert.PrivateKey.(*rsa.PrivateKey)
	if !ok || len(cert.Certificate) == 0 {
		return fail(alertHandshakeFailure, "the certificate's key is not RSA, which these clients need")
	}
	var s *suite
	for i := range suites {
		if slices.Contains(ch.suites, suites[i].id) {
			s = &suites[i]
			break
		}
	}
	if s == nil {
		return fail(alertHandshakeFailure, "no cipher suite in common: the client offers %04x", ch.suites)
	}

	sr := make([]byte, 32)
	rand.Read(sr)
	hello := binary.BigEndian.AppendUint16(nil, version10)
	hello = append(hello, sr...)
	hello = append(hello, 0) // no session id: nothing is resumed
	hello = binary.BigEndian.AppendUint16(hello, s.id)
	hello = append(hello, 0)
	if ch.secureReneg {
		hello = append(hello, 0, 5, 0xff, 0x01, 0, 1, 0) // renegotiation_info, empty
	}
	var chain []byte
	for _, der := range cert.Certificate {
		chain = appendU24(chain, len(der))
		chain = append(chain, der...)
	}
	flight := msg(hsServerHello, hello)
	flight = append(flight, msg(hsCertificate, appendU24(nil, len(chain)), chain...)...)
	flight = append(flight, msg(hsServerDone, nil)...)
	c.hash(flight)
	if err := c.writeRecord(recHandshake, flight); err != nil {
		return err
	}

	m, err := c.readHandshake()
	if err != nil {
		return err
	}
	if m[0] != hsClientKeyExchange {
		return fail(alertUnexpected, "handshake message %d, not the key exchange", m[0])
	}
	c.hash(m)
	ct, k := m[4:], key.Size()
	if len(ct) == k+2 && int(binary.BigEndian.Uint16(ct)) == k {
		ct = ct[2:]
	}
	if len(ct) != k {
		return fail(alertDecode, "key exchange of %d bytes", len(m)-4)
	}
	// A bad decryption, or the wrong version in it, goes on with a random
	// secret, in the same time, and fails at Finished (Bleichenbacher).
	pre, alt := make([]byte, 48), make([]byte, 48)
	rand.Read(pre)
	rand.Read(alt)
	if err := rsa.DecryptPKCS1v15SessionKey(nil, key, ct, pre); err != nil {
		return fail(alertInternal, "%v", err)
	}
	vOK := subtle.ConstantTimeByteEq(pre[0], byte(ch.version>>8)) &
		(subtle.ConstantTimeByteEq(pre[1], byte(ch.version)) | subtle.ConstantTimeByteEq(pre[1], 1))
	subtle.ConstantTimeCopy(1-vOK, pre, alt)

	ms := prf10(pre, "master secret", append(slices.Clone(ch.random), sr...), 48)
	ck, sk := s.keys(ms, ch.random, sr)
	clientFinished := c.finished(ms, "client finished")

	if len(c.hs) != 0 {
		return fail(alertUnexpected, "handshake data before ChangeCipherSpec")
	}
	typ, data, err := c.readRecord()
	if err != nil {
		return err
	}
	if typ == recAlert {
		return alertIn(data)
	}
	if typ != recCCS || len(data) != 1 || data[0] != 1 {
		return fail(alertUnexpected, "record type %d, not ChangeCipherSpec", typ)
	}
	c.in = s.newHalf(ck.mac, ck.key, ck.iv, false)
	if m, err = c.readHandshake(); err != nil {
		return err
	}
	if m[0] != hsFinished || len(m) != 4+12 {
		return fail(alertUnexpected, "handshake message %d, not Finished", m[0])
	}
	if subtle.ConstantTimeCompare(m[4:], clientFinished) != 1 {
		return fail(alertDecrypt, "the client's Finished is wrong")
	}
	c.hash(m)

	if err := c.writeRecord(recCCS, []byte{1}); err != nil {
		return err
	}
	c.outMu.Lock()
	c.out = s.newHalf(sk.mac, sk.key, sk.iv, true)
	c.outMu.Unlock()
	if err := c.writeRecord(recHandshake, msg(hsFinished, c.finished(ms, "server finished"))); err != nil {
		return err
	}
	c.suite = s.id
	return nil
}

func appendU24(b []byte, n int) []byte { return append(b, byte(n>>16), byte(n>>8), byte(n)) }

// msg is a handshake message.
func msg(typ uint8, body []byte, more ...byte) []byte {
	m := appendU24([]byte{typ}, len(body)+len(more))
	return append(append(m, body...), more...)
}

// readClientHello reads the hello, in either format.
func (c *Conn) readClientHello() (*clientHello, error) {
	b, err := c.br.Peek(1)
	if err != nil {
		return nil, err
	}
	if b[0]&0x80 != 0 {
		return c.readV2Hello()
	}
	m, err := c.readHandshake()
	if err != nil {
		return nil, err
	}
	if m[0] != hsClientHello {
		return nil, fail(alertUnexpected, "handshake message %d, not ClientHello", m[0])
	}
	c.hash(m)
	return parseClientHello(m[4:])
}

// readV2Hello reads an SSL 2.0-format ClientHello: its message (the bytes
// after the two of the record's length) is the handshake's first.
func (c *Conn) readV2Hello() (*clientHello, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
		return nil, err
	}
	n := int(hdr[0]&0x7f)<<8 | int(hdr[1])
	if n < 9 {
		return nil, fail(alertDecode, "SSL 2.0 hello of %d bytes", n)
	}
	m := make([]byte, n)
	if _, err := io.ReadFull(c.br, m); err != nil {
		return nil, err
	}
	if m[0] != 1 {
		return nil, fail(alertUnexpected, "SSL 2.0 message %d, not CLIENT-HELLO", m[0])
	}
	c.hash(m)
	u := func(i int) int { return int(binary.BigEndian.Uint16(m[i:])) }
	specLen, sidLen, chalLen := u(3), u(5), u(7)
	if 9+specLen+sidLen+chalLen != n || specLen%3 != 0 || chalLen < 16 || chalLen > 32 {
		return nil, fail(alertDecode, "bad SSL 2.0 hello")
	}
	ch := &clientHello{version: uint16(u(1))}
	specs := m[9 : 9+specLen]
	for i := 0; i < len(specs); i += 3 {
		if specs[i] == 0 { // a TLS suite; others are SSL 2.0's own
			id := binary.BigEndian.Uint16(specs[i+1:])
			ch.suites = append(ch.suites, id)
			if id == 0x00ff {
				ch.secureReneg = true
			}
		}
	}
	chal := m[9+specLen+sidLen:]
	ch.random = make([]byte, 32)
	copy(ch.random[32-len(chal):], chal)
	return ch, nil
}

type reader struct {
	b   []byte
	bad bool
}

func (r *reader) take(n int) []byte {
	if r.bad || len(r.b) < n {
		r.bad = true
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) u8() int {
	if v := r.take(1); v != nil {
		return int(v[0])
	}
	return 0
}

func (r *reader) u16() int {
	if v := r.take(2); v != nil {
		return int(binary.BigEndian.Uint16(v))
	}
	return 0
}

func parseClientHello(body []byte) (*clientHello, error) {
	r := &reader{b: body}
	ch := &clientHello{version: uint16(r.u16())}
	ch.random = slices.Clone(r.take(32))
	r.take(r.u8()) // session id
	ss := r.take(r.u16())
	if len(ss)%2 != 0 {
		r.bad = true
	}
	for i := 0; i+1 < len(ss); i += 2 {
		id := binary.BigEndian.Uint16(ss[i:])
		ch.suites = append(ch.suites, id)
		if id == 0x00ff {
			ch.secureReneg = true
		}
	}
	if comp := r.take(r.u8()); !r.bad && !slices.Contains(comp, 0) {
		return nil, fail(alertHandshakeFailure, "the client offers no null compression")
	}
	if len(r.b) > 0 && !r.bad {
		ext := &reader{b: r.take(r.u16())}
		for len(ext.b) > 0 && !ext.bad {
			typ, data := ext.u16(), ext.take(ext.u16())
			switch typ {
			case 0xff01:
				ch.secureReneg = true
			case 0x0000: // server name
				sn := &reader{b: data}
				list := &reader{b: sn.take(sn.u16())}
				if list.u8() == 0 {
					ch.sni = string(list.take(list.u16()))
				}
			}
		}
		r.bad = r.bad || ext.bad
	}
	if r.bad {
		return nil, fail(alertDecode, "bad ClientHello")
	}
	return ch, nil
}

// Read reads application data.
func (c *Conn) Read(p []byte) (int, error) {
	if err := c.Handshake(); err != nil {
		return 0, err
	}
	c.inMu.Lock()
	defer c.inMu.Unlock()
	for len(c.data) == 0 {
		if c.readErr != nil {
			return 0, c.readErr
		}
		typ, data, err := c.readRecord()
		switch {
		case err != nil:
			c.alertFor(err)
			c.readErr = err
		case typ == recData:
			c.data = data
		case typ == recAlert:
			if len(data) == 2 && data[0] == levelWarning && data[1] != alertCloseNotify {
				continue
			}
			c.readErr = alertIn(data)
		case typ == recHandshake: // the client would renegotiate: not here
			c.sendAlert(levelWarning, alertNoRenegotiation)
		default:
			c.readErr = fail(alertUnexpected, "record type %d", typ)
			c.alertFor(c.readErr)
		}
	}
	n := copy(p, c.data)
	c.data = c.data[n:]
	return n, nil
}

// Write writes application data.
func (c *Conn) Write(p []byte) (int, error) {
	if err := c.Handshake(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if err := c.writeRecord(recData, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close sends close_notify (after a handshake) and closes the connection.
func (c *Conn) Close() error {
	if c.up.Load() {
		c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		c.sendAlert(levelWarning, alertCloseNotify)
	}
	return c.Conn.Close()
}
