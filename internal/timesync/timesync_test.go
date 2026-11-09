package timesync

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/facebook/time/ntp/ntske"
)

// ahead is a clock a given time ahead of the system's (a server's, as
// though it followed a better one).
func ahead(d time.Duration) *Clock {
	c := &Clock{}
	c.Follow(Sample{Source: "test", Offset: d, Stratum: 1, RefID: refASCII("GPS"), When: time.Now(), TimeSrc: SourceGNSS})
	return c
}

func near(t *testing.T, what string, got, want time.Duration) {
	t.Helper()
	if d := got - want; d > 20*time.Millisecond || d < -20*time.Millisecond {
		t.Errorf("%s: %v, want %v", what, got, want)
	}
}

func ntpServer(t *testing.T, ctx context.Context, s *NTPServer) string {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve(ctx, c)
	return c.LocalAddr().String()
}

// TestNTP: a client of the server measures its clock's offset; the server
// tells its stratum as one past what it follows.
func TestNTP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := ntpServer(t, ctx, &NTPServer{Clock: ahead(5 * time.Second)})
	sm, err := query(ctx, addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "offset", sm.Offset, 5*time.Second)
	if sm.Stratum != 2 || sm.Delay > 50*time.Millisecond || sm.Leap != 0 {
		t.Errorf("%+v", sm)
	}
	// the follower takes it, and is one stratum further
	f := &Follower{S: Settings{Servers: []Server{{"ntp", addr}}}, Clock: &Clock{}}
	f.Prepare()
	res := f.Poll(ctx)
	if !res[0].Chosen || res[0].Err != nil {
		t.Fatalf("%+v", res)
	}
	r := f.Clock.Ref()
	near(t, "followed", f.Clock.Now().Sub(time.Now()), 5*time.Second)
	if !r.Synced || r.Stratum != 3 || r.RefID != refIP(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("%+v", r)
	}
}

// TestNTPRefused: what is not a client's request is not answered, nor an
// address over its rate, nor a refused one; an unsynchronised server is
// not followed.
func TestNTPRefused(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &NTPServer{Clock: ahead(0)}
	b := make([]byte, 48)
	b[0] = settings(0, 4, 6) // control mode
	if s.answer(b, time.Now()) != nil {
		t.Error("mode 6 answered")
	}
	b[0] = settings(0, 4, modeClient)
	if s.answer(b, time.Now()) == nil {
		t.Error("a client not answered")
	}
	l := limiter{rate: 1, burst: 2}
	now := time.Now()
	if !l.allow("a", now) || !l.allow("a", now) || l.allow("a", now) || !l.allow("b", now) {
		t.Error("burst")
	}
	if !l.allow("a", now.Add(time.Second)) {
		t.Error("not refilled")
	}
	refused := ntpServer(t, ctx, &NTPServer{Clock: ahead(0), Refuse: func(net.IP) bool { return true }})
	if _, err := query(ctx, refused, nil); err == nil {
		t.Error("a refused address answered")
	}
	lost := ntpServer(t, ctx, &NTPServer{Clock: &Clock{Local: Local{}}})
	if kernelStatus().synced {
		t.Skip("this system's clock is synchronised")
	}
	if _, err := query(ctx, lost, nil); err == nil || !strings.Contains(err.Error(), "not synchronised") {
		t.Errorf("unsynchronised followed: %v", err)
	}
}

// selfSigned is a certificate for 127.0.0.1, and a file of it to trust.
func selfSigned(t *testing.T) (cert, key, ca string) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cert, key = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	kb, _ := x509.MarshalECPrivateKey(k)
	os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0600)
	return cert, key, cert
}

// keServer runs an NTS-KE server for an NTP server's port.
func keServer(t *testing.T, ctx context.Context, ks ntske.Keystore, ntpAddr string) (string, string) {
	t.Helper()
	cert, key, ca := selfSigned(t)
	_, port, _ := net.SplitHostPort(ntpAddr)
	p, _ := strconv.Atoi(port)
	s := &KEServer{CertFile: cert, KeyFile: key, NTPHost: "127.0.0.1", NTPPort: p, Keys: ks}
	cfg, err := s.TLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	context.AfterFunc(ctx, func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				s.Handle(tls.Server(c, cfg))
			}()
		}
	}()
	return ln.Addr().String(), ca
}

// TestNTS: a key exchange, then NTS-secured NTP: answered, the cookies
// spent replaced; a server that knows the cookies no more NAKs them, and
// the client exchanges keys again.
func TestNTS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ks, err := NewKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ntpAddr := ntpServer(t, ctx, &NTPServer{Clock: ahead(-3 * time.Second), Keys: ks})
	ke, ca := keServer(t, ctx, ks, ntpAddr)
	tc, err := RootCAs(ca)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := keyExchange(ctx, ke, tc)
	if err != nil {
		t.Fatal(err)
	}
	if sess.addr != ntpAddr || len(sess.cookies) != 8 || sess.aead != 15 {
		t.Fatalf("session for %s, %d cookies, aead %d", sess.addr, len(sess.cookies), sess.aead)
	}
	for range 3 {
		sm, err := query(ctx, sess.addr, sess)
		if err != nil {
			t.Fatal(err)
		}
		near(t, "offset", sm.Offset, -3*time.Second)
	}
	if len(sess.cookies) != 8 {
		t.Errorf("%d cookies after", len(sess.cookies))
	}
	// a plain query of the same server: answered too
	if _, err := query(ctx, ntpAddr, nil); err != nil {
		t.Error(err)
	}
	// a forged answer (another key's) is not taken
	bad := &ntsSession{aead: sess.aead, c2s: sess.c2s, s2c: make([]byte, len(sess.s2c)), addr: sess.addr,
		cookies: append([][]byte(nil), sess.cookies[:2]...)}
	if _, err := query(ctx, sess.addr, bad); err == nil {
		t.Error("an answer not authenticated taken")
	}
	// new keys, all of them: the old cookies are refused
	for range 7 {
		ks.(*ntske.InMemoryKeystore).Rotate()
	}
	if _, err := query(ctx, sess.addr, sess); err == nil || !strings.Contains(err.Error(), "NAK") {
		t.Errorf("an old cookie: %v", err)
	}
	f := &Follower{S: Settings{Servers: []Server{{"nts", ke}}, RootCAs: ca}, Clock: &Clock{}}
	if err := f.Prepare(); err != nil {
		t.Fatal(err)
	}
	res := f.Poll(ctx)
	if res[0].Err != nil || !res[0].Chosen {
		t.Fatalf("%+v", res[0])
	}
	near(t, "followed", f.Clock.Now().Sub(time.Now()), -3*time.Second)
	// untrusted certificate: no session
	if _, err := keyExchange(ctx, ke, nil); err == nil {
		t.Error("a certificate not trusted taken")
	}
}

// TestNTSFacebookKE: facebook's NTS-KE server, the cookies its keystore
// seals, opened by this NTP server: the two agree on keys and cookies.
func TestNTSFacebookKE(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ks, _ := NewKeys(ctx)
	ntpAddr := ntpServer(t, ctx, &NTPServer{Clock: ahead(2 * time.Second), Keys: ks})
	cert, key, ca := selfSigned(t)
	c, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ntpAddr)
	p, _ := strconv.Atoi(port)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	fb := &ntske.Server{TLSConfig: &tls.Config{Certificates: []tls.Certificate{c}}, Keystore: ks,
		NTPv4Server: "127.0.0.1", NTPv4Port: uint16(p)}
	go fb.ListenAndServe(ctx, addr)
	time.Sleep(100 * time.Millisecond)
	tc, _ := RootCAs(ca)
	sess, err := keyExchange(ctx, addr, tc)
	if err != nil {
		t.Fatal(err)
	}
	sm, err := query(ctx, sess.addr, sess)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "offset", sm.Offset, 2*time.Second)
}

// freePorts is a port, and the one past it, free on both addresses.
func freePorts(t *testing.T, ips ...string) int {
	t.Helper()
	for range 50 {
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(ips[0])})
		if err != nil {
			t.Fatal(err)
		}
		p := c.LocalAddr().(*net.UDPAddr).Port
		c.Close()
		ok := p < 65535
		for _, ip := range ips {
			for _, q := range []int{p, p + 1} {
				d, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(ip), Port: q})
				if err != nil {
					ok = false
					continue
				}
				d.Close()
			}
		}
		if ok {
			return p
		}
	}
	t.Fatal("no ports")
	return 0
}

// TestPTP: a grandmaster on 127.0.0.1 a few seconds ahead; a client on
// 127.0.0.2 asks for unicast transmission and measures it.
func TestPTP(t *testing.T) {
	if testing.Short() {
		t.Skip("takes seconds")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2)}); err != nil {
		t.Skip("no 127.0.0.2 here")
	} else {
		c.Close()
	}
	base := freePorts(t, "127.0.0.1", "127.0.0.2")
	gm := &PTP{S: PTPSettings{Address: "127.0.0.1", Master: true, Unicast: true, MinLog: -4, MaxClients: 4,
		Priority1: 128, Priority2: 128, UTCOffset: 37, basePort: base}, Clock: ahead(3 * time.Second)}
	if err := gm.Open(); err != nil {
		t.Fatal(err)
	}
	go gm.Run(ctx)
	cl := &PTP{S: PTPSettings{Address: "127.0.0.2", UTCOffset: 37, basePort: base}, Clock: &Clock{}}
	if err := cl.Open(); err != nil {
		t.Fatal(err)
	}
	f := &Follower{S: Settings{Servers: []Server{{"ptp", "127.0.0.1"}}, Poll: 16 * time.Second}, Clock: cl.Clock, PTP: cl}
	if err := f.Prepare(); err != nil {
		t.Fatal(err)
	}
	go cl.Run(ctx)
	time.Sleep(4 * time.Second)
	if gm.Clients() != 1 {
		t.Errorf("%d unicast clients", gm.Clients())
	}
	res := f.Poll(ctx)
	if res[0].Err != nil {
		t.Fatal(res[0].Err)
	}
	near(t, "offset", res[0].Sample.Offset, 3*time.Second)
	t.Logf("ptp: offset %v, round trip %v (kernel stamps %v)", res[0].Sample.Offset-3*time.Second, res[0].Sample.Delay, gm.stamped)
	// the server is GNSS-fed (class 6): the client is stratum 1 now
	if r := f.Clock.Ref(); r.Stratum != 1 || RefIDString(r.RefID, 1) != "PTP" {
		t.Errorf("%+v", r)
	}
	// and serves that onward
	q, flags, src := cl.quality()
	if q.ClockClass != 6 || flags == 0 || src != 0x40 {
		t.Errorf("%+v %x %x", q, flags, src)
	}
}

func TestParseServers(t *testing.T) {
	s, err := ParseServers("nts time.cloudflare.com, pool.ntp.org, ptp 192.0.2.1 ,ntp [2001:db8::1]:1123")
	if err != nil || len(s) != 4 || s[0] != (Server{"nts", "time.cloudflare.com"}) || s[1].Kind != "ntp" ||
		s[2] != (Server{"ptp", "192.0.2.1"}) || s[3].Host != "[2001:db8::1]:1123" {
		t.Errorf("%+v %v", s, err)
	}
	for _, bad := range []string{"sntp x", "ntp a b", "ptp h:319", "ntp h:x"} {
		if _, err := ParseServers(bad); err == nil {
			t.Errorf("%q taken", bad)
		}
	}
}

// TestLocal: with no server, the clock tells of the system's.
func TestLocal(t *testing.T) {
	c := &Clock{Local: Local{Stratum: 1, RefID: "GPS"}}
	r := c.Ref()
	if !r.Synced || r.Stratum != 1 || RefIDString(r.RefID, 1) != "GPS" || r.TimeSrc != SourceGNSS {
		t.Errorf("%+v", r)
	}
	if RefIDString(refIP(net.IPv4(192, 0, 2, 7)), 2) != "192.0.2.7" {
		t.Error("refid")
	}
}
