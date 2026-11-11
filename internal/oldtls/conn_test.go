package oldtls

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"hash"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
)

var (
	certOnce sync.Once
	testCert tls.Certificate
)

func cert(t *testing.T) *tls.Certificate {
	t.Helper()
	certOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
			IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour),
			NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		testCert = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	})
	return &testCert
}

// echo serves one connection by oldtls, echoing what it reads; the
// server's error comes on the channel.
func echo(t *testing.T) (string, chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	c := cert(t)
	errs := make(chan error, 1)
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			errs <- err
			return
		}
		sc := Server(nc, &Config{GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return c, nil }})
		defer sc.Close()
		sc.SetDeadline(time.Now().Add(10 * time.Second))
		_, err = io.Copy(sc, sc)
		errs <- err
	}()
	return ln.Addr().String(), errs
}

// TestGoClient: Go's own TLS client, at TLS 1.0, with each suite.
func TestGoClient(t *testing.T) {
	for _, id := range []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA, tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_RSA_WITH_RC4_128_SHA} {
		t.Run(tls.CipherSuiteName(id), func(t *testing.T) {
			addr, errs := echo(t)
			c, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10,
				MaxVersion: tls.VersionTLS10, CipherSuites: []uint16{id}})
			if err != nil {
				t.Fatal(err)
			}
			if st := c.ConnectionState(); st.CipherSuite != id || st.Version != tls.VersionTLS10 {
				t.Fatalf("state %+v", st)
			}
			msg := bytes.Repeat([]byte("ping "), 9000) // more than a record
			go c.Write(msg)
			got := make([]byte, len(msg))
			if _, err := io.ReadFull(c, got); err != nil || !bytes.Equal(got, msg) {
				t.Fatalf("echo: %v", err)
			}
			c.Close()
			if err := <-errs; err != nil {
				t.Errorf("server: %v", err)
			}
		})
	}
}

// v2Client is a TLS 1.0 client that opens with an SSL 2.0-format hello,
// as AIM 6.2's NSS does.
type v2Client struct {
	t          *testing.T
	nc         net.Conn
	srv        Conn // its record layer, reused for the client's side
	hs         []byte
	finishedOK bool
}

func v2Hello(suites []uint16, challenge []byte) []byte {
	var specs []byte
	for _, s := range suites {
		specs = append(specs, 0, byte(s>>8), byte(s))
	}
	specs = append(specs, 0x01, 0x00, 0x80) // SSL 2.0's RC4_128_WITH_MD5, to be passed over
	m := []byte{1, 3, 1}
	m = binary.BigEndian.AppendUint16(m, uint16(len(specs)))
	m = binary.BigEndian.AppendUint16(m, 0)
	m = binary.BigEndian.AppendUint16(m, uint16(len(challenge)))
	m = append(append(m, specs...), challenge...)
	return append([]byte{0x80 | byte(len(m)>>8), byte(len(m))}, m...)
}

// handshake runs the client's side: the wrong Finished if badFinished.
func (v *v2Client) handshake(id uint16, badFinished bool) error {
	t := v.t
	chal := make([]byte, 16)
	rand.Read(chal)
	hello := v2Hello([]uint16{0x00ff, id}, chal)
	c := &v.srv
	c.Conn, c.br = v.nc, nil
	c.md5h, c.sha1h = newHashes()
	c.hash(hello[2:])
	if _, err := v.nc.Write(hello); err != nil {
		return err
	}
	c.br = bufioReader(v.nc)
	sh, err := c.readHandshake()
	if err != nil {
		return err
	}
	if sh[0] != hsServerHello || binary.BigEndian.Uint16(sh[4:]) != version10 {
		t.Fatalf("server hello %x", sh)
	}
	sr := sh[6:38]
	if got := binary.BigEndian.Uint16(sh[39:]); got != id {
		t.Fatalf("suite %04x", got)
	}
	if !bytes.HasSuffix(sh, []byte{0xff, 0x01, 0, 1, 0}) {
		t.Errorf("no renegotiation_info: %x", sh)
	}
	c.hash(sh)
	ce, err := c.readHandshake()
	if err != nil {
		return err
	}
	c.hash(ce)
	pc, err := x509.ParseCertificate(ce[10:])
	if err != nil {
		t.Fatal(err)
	}
	done, err := c.readHandshake()
	if err != nil || done[0] != hsServerDone {
		t.Fatalf("server hello done %x %v", done, err)
	}
	c.hash(done)

	pre := make([]byte, 48)
	rand.Read(pre)
	pre[0], pre[1] = 3, 1
	ct, err := rsa.EncryptPKCS1v15(rand.Reader, pc.PublicKey.(*rsa.PublicKey), pre)
	if err != nil {
		t.Fatal(err)
	}
	cke := msg(hsClientKeyExchange, binary.BigEndian.AppendUint16(nil, uint16(len(ct))), ct...)
	c.hash(cke)
	cr := make([]byte, 32)
	copy(cr[16:], chal)
	ms := prf10(pre, "master secret", append(append([]byte{}, cr...), sr...), 48)
	var s *suite
	for i := range suites {
		if suites[i].id == id {
			s = &suites[i]
		}
	}
	ck, sk := s.keys(ms, cr, sr)
	fin := c.finished(ms, "client finished")
	if badFinished {
		fin[0] ^= 1
	}
	finMsg := msg(hsFinished, fin)
	c.hash(finMsg)
	if err := c.writeRecord(recHandshake, cke); err != nil {
		return err
	}
	if err := c.writeRecord(recCCS, []byte{1}); err != nil {
		return err
	}
	c.out = s.newHalf(ck.mac, ck.key, ck.iv, true)
	if err := c.writeRecord(recHandshake, finMsg); err != nil {
		return err
	}
	typ, data, err := c.readRecord()
	if err != nil {
		return err
	}
	if typ == recAlert {
		return alertIn(data)
	}
	if typ != recCCS {
		t.Fatalf("record %d, not CCS", typ)
	}
	c.in = s.newHalf(sk.mac, sk.key, sk.iv, false)
	sf, err := c.readHandshake()
	if err != nil {
		return err
	}
	if !bytes.Equal(sf[4:], c.finished(ms, "server finished")) {
		t.Fatalf("server Finished wrong")
	}
	return nil
}

// TestSSLv2Hello: a client opening with an SSL 2.0-format hello is served,
// with each suite, and its data echoed.
func TestSSLv2Hello(t *testing.T) {
	for _, s := range suites {
		t.Run(tls.CipherSuiteName(s.id), func(t *testing.T) {
			addr, errs := echo(t)
			nc, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer nc.Close()
			nc.SetDeadline(time.Now().Add(10 * time.Second))
			v := &v2Client{t: t, nc: nc}
			if err := v.handshake(s.id, false); err != nil {
				t.Fatal(err)
			}
			c := &v.srv
			want := bytes.Repeat([]byte("hello "), 5000)
			if err := c.writeRecord(recData, want); err != nil {
				t.Fatal(err)
			}
			var got []byte
			for len(got) < len(want) {
				typ, data, err := c.readRecord()
				if err != nil || typ != recData {
					t.Fatalf("record %d: %v", typ, err)
				}
				got = append(got, data...)
			}
			if !bytes.Equal(got, want) {
				t.Fatal("echo differs")
			}
			c.sendAlert(levelWarning, alertCloseNotify)
			if err := <-errs; err != nil {
				t.Errorf("server: %v", err)
			}
		})
	}
}

// TestBadFinished: a wrong Finished is refused, with an alert.
func TestBadFinished(t *testing.T) {
	addr, errs := echo(t)
	nc, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	nc.SetDeadline(time.Now().Add(10 * time.Second))
	v := &v2Client{t: t, nc: nc}
	if err := v.handshake(0x002f, true); err == nil {
		t.Fatal("handshake passed")
	}
	if err := <-errs; err == nil {
		t.Error("server took it")
	}
}

// TestSSL3Refused: a client offering only SSL 3.0 is refused.
func TestSSL3Refused(t *testing.T) {
	addr, errs := echo(t)
	nc, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	nc.SetDeadline(time.Now().Add(10 * time.Second))
	h := v2Hello([]uint16{0x002f}, make([]byte, 16))
	h[4] = 0 // version 3.0
	nc.Write(h)
	b, _ := io.ReadAll(nc)
	if len(b) < 7 || b[0] != recAlert || b[6] != alertProtocolVersion {
		t.Errorf("answer %x", b)
	}
	if err := <-errs; err == nil {
		t.Error("server took it")
	}
}

func TestIsSSLv2Hello(t *testing.T) {
	if !IsSSLv2Hello(v2Hello([]uint16{0x002f}, make([]byte, 16))) {
		t.Error("v2 hello not seen")
	}
	if IsSSLv2Hello([]byte{0x16, 3, 1, 0, 5}) {
		t.Error("a TLS record taken for v2")
	}
}

func TestPRF(t *testing.T) {
	// The TLS 1.0 PRF test vector (48 bytes of 0xab secret, 64 of 0xcd
	// seed) from the IETF TLS list: its first bytes.
	secret := bytes.Repeat([]byte{0xab}, 48)
	seed := bytes.Repeat([]byte{0xcd}, 64)
	got := prf10(secret, "PRF Testvector", seed, 104)
	want := []byte{0xd3, 0xd4, 0xd1, 0xe3, 0x49, 0xb5, 0xd5, 0x15}
	if !bytes.Equal(got[:8], want) {
		t.Errorf("prf %x", got[:8])
	}
}

func newHashes() (hash.Hash, hash.Hash) { return md5.New(), sha1.New() }

func bufioReader(c net.Conn) *bufio.Reader { return bufio.NewReader(c) }
