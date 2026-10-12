package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/guard"
	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
)

// selfSigned writes a certificate and key for localhost.
func selfSigned(t *testing.T, dir string) (cert, key string) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(k)
	cert, key = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0600)
	return cert, key
}

// TestSecurity: telnet over TLS; the deny file closing connections; a
// name the name filter refuses; the hack log.
func TestSecurity(t *testing.T) {
	dir := t.TempDir()
	cert, key := selfSigned(t, dir)
	deny, names, hack := filepath.Join(dir, "deny"), filepath.Join(dir, "names"), filepath.Join(dir, "hack.log")
	os.WriteFile(deny, nil, 0644)
	os.WriteFile(names, []byte("*spam*\n"), 0644)
	tlsAddr, rawAddr := freeTCP(t), freeTCP(t)
	e := startFull(t, ui3270.Graphics{}, func(s *bbs.Settings) { s.NewUsers = true },
		func(b *bbs.BBS, _ string) {
			b.Guard = &guard.Guard{DenyFile: deny, NameDenyFile: names, HackLog: hack, Reload: time.Nanosecond}
		},
		&Listener{Name: "telnets", Kind: "telnet", Address: tlsAddr, ANSI: "no", CertFile: cert, KeyFile: key},
		&Listener{Name: "raw", Kind: "raw", Address: rawAddr, ANSI: "no"},
		&Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})

	c, err := tls.Dial("tcp", tlsAddr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	tc := &tnClient{c: c, ttype: "dumb"}
	tc.expect(t, "Name (or NEW): ")
	c.Write([]byte("alice\r"))
	tc.expect(t, "Password: ")
	c.Write([]byte("alicepass1\r"))
	tc.expect(t, "Welcome, alice.")
	if !c.ConnectionState().HandshakeComplete {
		t.Error("no TLS")
	}

	// a plain telnet to the TLS port gets nothing
	p, err := net.Dial("tcp", tlsAddr)
	if err != nil {
		t.Fatal(err)
	}
	p.Write([]byte("hello\r\n"))
	p.SetReadDeadline(time.Now().Add(5 * time.Second))
	if b, _ := io.ReadAll(p); strings.Contains(string(b), "Name") {
		t.Errorf("plain text on the TLS port: %q", b)
	}
	p.Close()

	// the name filter
	d := e.dial("modem.sock")
	d.expect("Name (or NEW): ")
	d.send("new\r")
	d.expect("give up): ")
	d.send("Spam Lord\r")
	d.expect("That name is not allowed here.")

	// denied: the connection closes at once
	os.WriteFile(deny, []byte("127.0.0.1\n"), 0644)
	os.Chtimes(deny, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	r, err := net.Dial("tcp", rawAddr)
	if err != nil {
		t.Fatal(err)
	}
	r.SetReadDeadline(time.Now().Add(5 * time.Second))
	if b, err := io.ReadAll(r); len(b) != 0 || err != nil {
		t.Errorf("denied, yet: %q %v", b, err)
	}
	r.Close()
	log, _ := os.ReadFile(hack)
	for _, want := range []string{`new user "Spam Lord" refused by name_deny_file`,
		"127.0.0.1 [listen raw]: refused: in deny_file (127.0.0.1/32)"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("no %q in the hack log:\n%s", want, log)
		}
	}
}

func TestSecurityConfig(t *testing.T) {
	dir := t.TempDir()
	cert, key := selfSigned(t, dir)
	deny := filepath.Join(dir, "deny")
	os.WriteFile(deny, []byte("192.0.2.0/24\nnot an address\n"), 0644)
	load := func(body string) (*Config, error) {
		p := filepath.Join(dir, "bbs.conf")
		os.WriteFile(p, []byte(body), 0600)
		return Load(p)
	}
	c, err := load("[security]\ndeny_file = " + deny + "\nhack_log = " + filepath.Join(dir, "hack.log") +
		"\nban_minutes = 30\n[listen s]\ntype = telnet\naddress = :992\ntls_cert = " + cert + "\ntls_key = " + key + "\n")
	if err != nil || c.Guard.BanTime != 30*time.Minute || c.Listeners[0].CertFile != cert {
		t.Fatalf("%+v %v", c, err)
	}
	if !strings.Contains(strings.Join(c.Warnings, "\n"), `"not an address" is not an address`) {
		t.Errorf("warnings %v", c.Warnings)
	}
	for body, want := range map[string]string{
		"[listen s]\ntype = telnet\naddress = :992\ntls_cert = " + cert + "\n":                        "both, or neither",
		"[listen s]\ntype = ssh\naddress = :22\ntls_cert = " + cert + "\ntls_key = " + key + "\n":     "only for telnet and raw",
		"[listen s]\ntype = telnet\naddress = :992\ntls_cert = " + deny + "\ntls_key = " + key + "\n": "tls_cert, tls_key",
		"[security]\nban_minutes = -1\n[listen s]\ntype = telnet\naddress = :23\n":                    "ban_minutes",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}
