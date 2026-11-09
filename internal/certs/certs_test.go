package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writePair(t *testing.T, dir, cn string) (string, string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{cn}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	crt, k := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	os.WriteFile(k, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0o600)
	os.WriteFile(crt, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	return crt, k
}

type fakeManager struct{ cert, challenge *tls.Certificate }

func (f fakeManager) Certificate(string, *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return f.cert, nil
}
func (f fakeManager) Challenge(string) *tls.Certificate { return f.challenge }
func (f fakeManager) Has(string) bool                   { return true }
func (f fakeManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("answered"))
}

// TestFiles: a file pair is read again when it changes; a bad new one
// leaves the old in use.
func TestFiles(t *testing.T) {
	dir := t.TempDir()
	crt, key := writePair(t, dir, "one.example")
	s, err := New(crt, key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.GetCertificate(nil)
	if c.Leaf.Subject.CommonName != "one.example" {
		t.Fatalf("%v", c.Leaf.Subject)
	}
	writePair(t, dir, "two.example")
	os.Chtimes(crt, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	s.checked = time.Time{}
	if c, _ = s.GetCertificate(nil); c.Leaf.Subject.CommonName != "two.example" {
		t.Errorf("not read again: %v", c.Leaf.Subject)
	}
	os.WriteFile(crt, []byte("junk"), 0o644)
	os.Chtimes(crt, time.Now().Add(2*time.Minute), time.Now().Add(2*time.Minute))
	s.checked = time.Time{}
	if c, err = s.GetCertificate(nil); err != nil || c.Leaf.Subject.CommonName != "two.example" {
		t.Errorf("bad file: %v %v", c, err)
	}
	if _, err := New(filepath.Join(dir, "none"), key); err == nil {
		t.Error("no error for a missing file")
	}
}

// TestACME: cert_file = acme goes to the manager, and TLS-ALPN-01 is
// answered only for a client asking for acme-tls/1 alone.
func TestACME(t *testing.T) {
	if Check("acme", "", nil) == nil || Check("acme:x", "", []string{"default"}) == nil || Check("acme:x", "", []string{"x"}) != nil {
		t.Error("Check")
	}
	if c, k := Default("", "", true); c != "acme" || k != "" {
		t.Error("Default")
	}
	if c, _ := Default("", "", false); c != "" {
		t.Error("Default without acme")
	}
	SetManager(nil)
	if _, err := New("acme", ""); err == nil {
		t.Error("acme with no manager")
	}
	dir := t.TempDir()
	crt, key := writePair(t, dir, "bbs.example")
	pair, _ := tls.LoadX509KeyPair(crt, key)
	crt2, key2 := writePair(t, t.TempDir(), "challenge")
	ch, _ := tls.LoadX509KeyPair(crt2, key2)
	SetManager(fakeManager{cert: &pair, challenge: &ch})
	defer SetManager(nil)
	cfg, err := Config("acme", "")
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: "bbs.example"}); c != &pair {
		t.Error("not the manager's")
	}
	if c, _ := cfg.GetConfigForClient(&tls.ClientHelloInfo{SupportedProtos: []string{"h2"}}); c != nil {
		t.Error("challenge config for an ordinary client")
	}
	c, _ := cfg.GetConfigForClient(&tls.ClientHelloInfo{ServerName: "bbs.example", SupportedProtos: []string{ALPNProto}})
	if c == nil || c.NextProtos[0] != ALPNProto || len(c.Certificates) != 1 {
		t.Errorf("challenge config %+v", c)
	}

	h := WrapHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("site")) }))
	for path, want := range map[string]string{"/.well-known/acme-challenge/x": "answered", "/": "site"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Body.String() != want {
			t.Errorf("%s: %q", path, w.Body.String())
		}
	}
}
