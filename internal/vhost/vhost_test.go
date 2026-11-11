package vhost

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testCert(t *testing.T, names ...string) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: names[0]},
		DNSNames: names, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c := &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return c, nil }
}

func say(s string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tls := "plain"
		if r.TLS != nil {
			tls = "tls"
		}
		io.WriteString(w, s+" "+tls+" "+r.URL.Path)
	})
}

func startPort(t *testing.T, p *Port) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return ln.Addr().String()
}

// get asks addr for path as host (sni: the TLS server name, "" none).
func get(t *testing.T, addr string, useTLS bool, sni, host, path string) string {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if useTLS {
		tc := tls.Client(c, &tls.Config{ServerName: sni, InsecureSkipVerify: true})
		if err := tc.Handshake(); err != nil {
			t.Fatal(err)
		}
		c = tc
	}
	io.WriteString(c, "GET "+path+" HTTP/1.1\r\nHost: "+host+"\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestTLSRoutes(t *testing.T) {
	msnCert := testCert(t, "login.live.com")
	oscarConns := make(chan string, 1)
	p := &Port{Address: ":443", TLS: true, Routes: []*Route{
		{Name: "msn", Names: []string{"login.live.com", "*.passport.com"}, Paths: []string{"/RST.srf", "/login2.srf"},
			Cert: msnCert, HTTP: say("msn")},
		{Name: "ymsg", Names: []string{"login.yahoo.com"}, Paths: []string{"/config/"},
			Cert: testCert(t, "login.yahoo.com"), HTTP: say("ymsg")},
		{Name: "oscar", Names: []string{"api.screenname.aol.com"}, Paths: []string{"/auth/", "/$"},
			Cert: testCert(t, "api.screenname.aol.com"), HTTP: say("oscar"), SSLv2: true,
			Conn: func(c net.Conn) {
				b := make([]byte, 4)
				io.ReadFull(c, b)
				oscarConns <- string(b)
			}},
	}}
	addr := startPort(t, p)
	cases := []struct{ sni, host, path, want string }{
		{"login.yahoo.com", "login.yahoo.com", "/config/pwtoken_get", "ymsg tls /config/pwtoken_get"},
		{"login.passport.com", "login.passport.com", "/login2.srf", "msn tls /login2.srf"},
		{"", "login.yahoo.com", "/x", "ymsg tls /x"},             // Windows XP: no SNI, by Host
		{"", "192.168.77.1", "/RST.srf", "msn tls /RST.srf"},     // by path
		{"", "192.168.77.1:443", "/", "oscar tls /"},             // AIM's Kerberos login, exact path
		{"", "192.168.77.1", "/elsewhere", "msn tls /elsewhere"}, // the first
		{"api.screenname.aol.com", "API.ScreenName.aol.com.", "/auth/clientLogin", "oscar tls /auth/clientLogin"},
	}
	for _, c := range cases {
		if got := get(t, addr, true, c.sni, c.host, c.path); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
	// no SNI: the first front end's certificate
	c, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	if cn := c.ConnectionState().PeerCertificates[0].Subject.CommonName; cn != "login.live.com" {
		t.Errorf("certificate without SNI: %s", cn)
	}
	// OSCAR over TLS (a FLAP, not HTTP): to the front end for other connections
	io.WriteString(c, "*\x01\x00\x01")
	select {
	case got := <-oscarConns:
		if got != "*\x01\x00\x01" {
			t.Errorf("OSCAR got %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Error("OSCAR connection not handed over")
	}
	c.Close()
}

func TestPlainRoutes(t *testing.T) {
	p := &Port{Address: ":80", Routes: []*Route{
		{Name: "msn", Names: []string{"nexus.passport.com"}, Paths: []string{"/rdr/"}, HTTP: say("msn")},
		{Name: "ymsg", Names: []string{"*.msg.yahoo.com"}, Paths: []string{"/capacity"}, HTTP: say("ymsg")},
	}}
	addr := startPort(t, p)
	for _, c := range []struct{ host, path, want string }{
		{"vcs1.msg.yahoo.com", "/capacity", "ymsg plain /capacity"},
		{"nexus.passport.com", "/rdr/pprdr.asp", "msn plain /rdr/pprdr.asp"},
		{"10.0.0.1", "/capacity", "ymsg plain /capacity"},
		{"msg.yahoo.com", "/", "msn plain /"}, // not under *.msg.yahoo.com: the first
	} {
		if got := get(t, addr, false, "", c.host, c.path); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

func TestClaim(t *testing.T) {
	Claim("0.0.0.0:8443")
	if !Claimed(":8443") || !Claimed("[::]:8443") || Claimed(":8444") || Claimed("127.0.0.1:8443") {
		t.Error("Claimed")
	}
	if !strings.HasPrefix(Normal("[::]:1"), "[") && Normal("[::]:1") != ":1" {
		t.Error(Normal("[::]:1"))
	}
}
