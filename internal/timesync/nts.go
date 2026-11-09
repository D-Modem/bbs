package timesync

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/facebook/time/ntp/ntske"
	ntp "github.com/facebook/time/ntp/protocol"
)

// The NTS-KE port (RFC 8915), and its TLS application protocol.
const (
	KEPort = 4460
	alpn   = "ntske/1"
)

// AEADs offered, best first: AES-SIV-CMAC-256 (RFC 8915's must), then
// AES-128-GCM-SIV (chrony's own first) and AES-SIV-CMAC-512.
var aeads = []uint16{uint16(ntp.AEADAESSIVCMAC256), uint16(ntp.AEADAES128GCMSIV), uint16(ntp.AEADAESSIVCMAC512)}

func keyLen(id uint16) int {
	switch ntp.AEADAlgorithm(id) {
	case ntp.AEADAES128GCMSIV:
		return 16
	case ntp.AEADAESSIVCMAC256:
		return 32
	case ntp.AEADAESSIVCMAC512:
		return 64
	}
	return 0
}

// exportKeys are the two keys, client to server and back, an NTS-KE
// session's TLS makes (RFC 8915 5.1).
func exportKeys(cs tls.ConnectionState, aead uint16) (c2s, s2c []byte, err error) {
	ctx := make([]byte, 5)
	binary.BigEndian.PutUint16(ctx[2:], aead) // next protocol 0: NTPv4
	if c2s, err = cs.ExportKeyingMaterial("EXPORTER-network-time-security", ctx, keyLen(aead)); err != nil {
		return nil, nil, err
	}
	ctx[4] = 1
	s2c, err = cs.ExportKeyingMaterial("EXPORTER-network-time-security", ctx, keyLen(aead))
	return c2s, s2c, err
}

// readRecords reads NTS-KE records to the end of message, at most 64 KB.
func readRecords(r io.Reader) ([]ntske.Record, error) {
	var buf []byte
	h := make([]byte, 4)
	for {
		if _, err := io.ReadFull(r, h); err != nil {
			return nil, err
		}
		n := int(binary.BigEndian.Uint16(h[2:]))
		if len(buf)+4+n > 64<<10 {
			return nil, errors.New("ntske: message too long")
		}
		buf = append(buf, h...)
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
		buf = append(buf, body...)
		if binary.BigEndian.Uint16(h)&0x7fff == ntske.RecordEndOfMessage {
			return ntske.Parse(buf)
		}
	}
}

// KEServer is an NTS-KE server: TLS 1.3 on TCP, whose clients go away
// with keys and cookies for the NTP server.
type KEServer struct {
	CertFile, KeyFile string
	NTPHost           string // the NTP server clients are told of ("": this one)
	NTPPort           int    // and its port (0, 123: not told)
	Keys              ntske.Keystore

	mu      sync.Mutex
	cert    *tls.Certificate
	certMod time.Time
}

// TLSConfig is the server's TLS: 1.3, ntske/1, the certificate read again
// when its file changes (a renewal).
func (s *KEServer) TLSConfig() (*tls.Config, error) {
	if _, err := s.certificate(); err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, NextProtos: []string{alpn},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return s.certificate() }}, nil
}

func (s *KEServer) certificate() (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fi, err := os.Stat(s.CertFile)
	if err == nil && s.cert != nil && !fi.ModTime().After(s.certMod) {
		return s.cert, nil
	}
	c, err := tls.LoadX509KeyPair(s.CertFile, s.KeyFile)
	if err != nil {
		if s.cert != nil {
			return s.cert, nil // the one that worked, until the new one does
		}
		return nil, err
	}
	s.cert = &c
	if fi != nil {
		s.certMod = fi.ModTime()
	}
	return s.cert, nil
}

// Handle answers one client on c, a TLS connection (tls.Server).
func (s *KEServer) Handle(c *tls.Conn) error {
	c.SetDeadline(time.Now().Add(10 * time.Second))
	if err := c.Handshake(); err != nil {
		return err
	}
	cs := c.ConnectionState()
	if cs.NegotiatedProtocol != alpn {
		return errors.New("ntske: not ntske/1")
	}
	recs, err := readRecords(c)
	if err != nil {
		return err
	}
	resp, err := s.respond(recs, cs)
	if err != nil {
		var pe protoErr
		code := uint16(2) // internal server error
		if errors.As(err, &pe) {
			code = uint16(pe)
		}
		b, _ := ntske.MarshalRecords([]ntske.Record{ntske.NewError(code), ntske.NewEndOfMessage()})
		c.Write(b)
		return err
	}
	b, err := ntske.MarshalRecords(resp)
	if err != nil {
		return err
	}
	_, err = c.Write(b)
	return err
}

// protoErr is an NTS-KE error code to send back: 0 unrecognised critical
// record, 1 bad request.
type protoErr uint16

func (e protoErr) Error() string { return "ntske: error " + strconv.Itoa(int(e)) }

// respond is the answer to a request's records.
func (s *KEServer) respond(recs []ntske.Record, cs tls.ConnectionState) ([]ntske.Record, error) {
	var protos, offered []uint16
	sawProto, sawAEAD, eom, compliant := false, false, false, false
	for _, r := range recs {
		switch r.Type {
		case ntske.RecordEndOfMessage:
			eom = true
		case ntske.RecordNextProtocol:
			if sawProto {
				return nil, protoErr(1)
			}
			sawProto = true
			var err error
			if protos, err = ntske.ParseUint16s(r.Body); err != nil {
				return nil, protoErr(1)
			}
		case ntske.RecordAEADAlgorithm:
			if sawAEAD {
				return nil, protoErr(1)
			}
			sawAEAD = true
			var err error
			if offered, err = ntske.ParseUint16s(r.Body); err != nil {
				return nil, protoErr(1)
			}
		case ntske.RecordCompliant128GCMExport:
			compliant = true
		case ntske.RecordError, ntske.RecordWarning, ntske.RecordNewCookie,
			ntske.RecordServerNegotiation, ntske.RecordPortNegotiation:
		default:
			if r.Critical {
				return nil, protoErr(0)
			}
		}
	}
	if !eom || !slices.Contains(protos, ntske.NextProtocolNTPv4) || len(offered) == 0 {
		return nil, protoErr(1)
	}
	var aead uint16
	for _, a := range offered {
		// AES-128-GCM-SIV only for clients that say they make its keys as
		// the RFC says (chrony before 4.4 did not)
		if slices.Contains(aeads, a) && (a != uint16(ntp.AEADAES128GCMSIV) || compliant) {
			aead = a
			break
		}
	}
	if aead == 0 {
		return nil, protoErr(1)
	}
	c2s, s2c, err := exportKeys(cs, aead)
	if err != nil {
		return nil, err
	}
	out := []ntske.Record{ntske.NewNextProtocol(ntske.NextProtocolNTPv4), ntske.NewAEADAlgorithm(aead)}
	if compliant && aead == uint16(ntp.AEADAES128GCMSIV) {
		out = append(out, ntske.NewCompliant128GCMExport())
	}
	if s.NTPHost != "" {
		out = append(out, ntske.NewServerNegotiation(s.NTPHost))
	}
	if s.NTPPort != 0 && s.NTPPort != 123 {
		out = append(out, ntske.NewPortNegotiation(uint16(s.NTPPort)))
	}
	for range 8 {
		ck, err := s.Keys.SealCookie(ntp.AEADAlgorithm(aead), c2s, s2c)
		if err != nil {
			return nil, err
		}
		out = append(out, ntske.NewCookie(ck))
	}
	return append(out, ntske.NewEndOfMessage()), nil
}

// NewKeys is a keystore for cookies: a new master key now, and each day
// (the last week's kept, so cookies out there stay good), until ctx is
// done.
func NewKeys(ctx context.Context) (ntske.Keystore, error) {
	ks, err := ntske.NewInMemoryKeystore(ntske.InMemoryKeystoreOptions{MaxKeys: 7})
	if err != nil {
		return nil, err
	}
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				ks.Rotate()
			}
		}
	}()
	return ks, nil
}

// ntsSession is a client's: the keys and cookies one NTS-KE exchange gave,
// for the NTP server it named.
type ntsSession struct {
	aead     ntp.AEADAlgorithm
	c2s, s2c []byte
	addr     string // the NTP server's host:port

	mu      sync.Mutex
	cookies [][]byte
	failed  int
}

// take is a cookie to spend.
func (s *ntsSession) take() ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cookies) == 0 {
		return nil, false
	}
	c := s.cookies[0]
	s.cookies = s.cookies[1:]
	return c, true
}

// wanted is how many more cookies to ask for (placeholders), to keep eight.
func (s *ntsSession) wanted() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return max(0, 7-len(s.cookies))
}

func (s *ntsSession) give(c [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cookies = append(s.cookies, c...)
	s.failed = 0
}

// lost: the server knows the cookies no more (its keys changed).
func (s *ntsSession) lost() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cookies = nil
}

// spent: a request went unanswered; after a few the session is begun again.
func (s *ntsSession) spent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed++
	return s.failed >= 4 || len(s.cookies) == 0
}

// keyExchange is an NTS-KE exchange with host (host[:port], 4460 if none):
// a session for the NTP server it names.
func keyExchange(ctx context.Context, host string, tc *tls.Config) (*ntsSession, error) {
	h, port, err := net.SplitHostPort(host)
	if err != nil {
		h, port = host, strconv.Itoa(KEPort)
	}
	cfg := &tls.Config{ServerName: h}
	if tc != nil {
		cfg = tc.Clone()
		if cfg.ServerName == "" {
			cfg.ServerName = h
		}
	}
	cl := &ntske.Client{SupportedAEAD: aeads, RequestCompliantExport: true, Timeout: 10 * time.Second}
	res, err := cl.Handshake(ctx, net.JoinHostPort(h, port), cfg)
	if err != nil {
		return nil, err
	}
	if len(res.Cookies) == 0 {
		return nil, fmt.Errorf("nts-ke %s: no cookies", host)
	}
	ntpHost, ntpPort := h, "123"
	if res.NTPServer != "" {
		ntpHost = res.NTPServer
	}
	if res.NTPPort != 0 {
		ntpPort = strconv.Itoa(int(res.NTPPort))
	}
	return &ntsSession{aead: ntp.AEADAlgorithm(res.AEAD), c2s: res.C2S, s2c: res.S2C,
		addr: net.JoinHostPort(ntpHost, ntpPort), cookies: res.Cookies}, nil
}
