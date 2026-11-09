package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/certificate"
)

// issue makes a self-signed certificate for names, good until notAfter, as
// a CA's would be kept.
func issue(t *testing.T, notAfter time.Time, names ...string) *certificate.Resource {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: names[0]},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter, DNSNames: names}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	return &certificate.Resource{Certificate: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		PrivateKey: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})}
}

func testManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(Settings{Dir: t.TempDir(), CA: CAs["letsencrypt-staging"], Certs: []Cert{
		{Name: "default", Domains: []string{"bbs.example.org", "www.example.org"}, Challenge: HTTP01},
		{Name: "mail", Domains: []string{"mail.example.org"}, Challenge: TLSALPN01},
		{Name: "wild", Domains: []string{"*.example.net"}, Challenge: DNS01, DNS: "cloudflare"},
	}}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestCertificates: the certificate for the name asked for (SNI), the
// first for any other; one renewed on disk is read again.
func TestCertificates(t *testing.T) {
	m := testManager(t)
	if _, err := m.Certificate("", &tls.ClientHelloInfo{ServerName: "bbs.example.org"}); err == nil {
		t.Error("a certificate before there is one")
	}
	if due, why := m.Due("default"); !due || why != "none yet" {
		t.Errorf("due %v %q", due, why)
	}
	later := time.Now().Add(80 * 24 * time.Hour)
	m.keep("default", issue(t, later, "bbs.example.org", "www.example.org"))
	m.keep("mail", issue(t, later, "mail.example.org"))
	m.keep("wild", issue(t, later, "*.example.net"))
	for name, want := range map[string]string{"bbs.example.org": "bbs.example.org", "www.example.org": "bbs.example.org",
		"mail.example.org": "mail.example.org", "x.example.net": "*.example.net", "": "bbs.example.org", "other.test": "bbs.example.org"} {
		c, err := m.Certificate("", &tls.ClientHelloInfo{ServerName: name})
		if err != nil || c.Leaf.Subject.CommonName != want {
			t.Errorf("%q: %v %v", name, c, err)
		}
	}
	if c, err := m.Certificate("mail", &tls.ClientHelloInfo{ServerName: "bbs.example.org"}); err != nil || c.Leaf.Subject.CommonName != "mail.example.org" {
		t.Errorf("named: %v", err)
	}
	if due, why := m.Due("default"); due {
		t.Errorf("due: %s", why)
	}

	// renewed by another process: read again once its file changes
	old, _ := m.Certificate("default", nil)
	m.keep("default", issue(t, later.Add(time.Hour), "bbs.example.org", "www.example.org"))
	crt, _ := m.Files("default")
	os.Chtimes(crt, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	m.mu.Lock()
	m.held["default"].checked = time.Time{}
	m.mu.Unlock()
	if c, _ := m.Certificate("default", nil); c.Leaf.SerialNumber.Cmp(old.Leaf.SerialNumber) == 0 {
		t.Error("not read again")
	}
	if fi, _ := os.Stat(filepath.Join(m.certDir(), "default.key")); fi.Mode().Perm() != 0o600 {
		t.Errorf("key mode %v", fi.Mode())
	}
}

// TestDue: renewed when the domains change, the key type changes, or the
// end is near.
func TestDue(t *testing.T) {
	m := testManager(t)
	m.keep("default", issue(t, time.Now().Add(80*24*time.Hour), "bbs.example.org"))
	if due, why := m.Due("default"); !due || !strings.Contains(why, "domains") {
		t.Errorf("domains: %v %q", due, why)
	}
	m.keep("mail", issue(t, time.Now().Add(10*24*time.Hour), "mail.example.org"))
	if due, why := m.Due("mail"); !due || !strings.Contains(why, "runs out") {
		t.Errorf("end: %v %q", due, why)
	}
	m.S.Certs[2].KeyType = "rsa2048"
	m.keep("wild", issue(t, time.Now().Add(80*24*time.Hour), "*.example.net"))
	if due, why := m.Due("wild"); !due || !strings.Contains(why, "key") {
		t.Errorf("key: %v %q", due, why)
	}
}

// TestChallenges: HTTP-01 tokens and TLS-ALPN-01 certificates are
// answered from memory, and from disk (put there by bbs acme renew).
func TestChallenges(t *testing.T) {
	m := testManager(t)
	get := func(path string) (int, string) {
		w := httptest.NewRecorder()
		m.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w.Code, w.Body.String()
	}
	httpSolver{m}.Present("bbs.example.org", "tok-1", "tok-1.thumb")
	if code, body := get("/.well-known/acme-challenge/tok-1"); code != 200 || body != "tok-1.thumb" {
		t.Errorf("token: %d %q", code, body)
	}
	if code, _ := get("/.well-known/acme-challenge/../../etc/passwd"); code != 404 {
		t.Errorf("traversal: %d", code)
	}
	// another process's
	other := &Manager{S: m.S, Log: m.Log, held: map[string]*held{}, tokens: map[string]string{},
		alpn: map[string]*tls.Certificate{}, ari: map[string]time.Time{}}
	httpSolver{other}.Present("bbs.example.org", "tok-2", "tok-2.thumb")
	if code, body := get("/.well-known/acme-challenge/tok-2"); code != 200 || body != "tok-2.thumb" {
		t.Errorf("shared token: %d %q", code, body)
	}
	httpSolver{other}.CleanUp("bbs.example.org", "tok-2", "")
	httpSolver{m}.CleanUp("bbs.example.org", "tok-1", "")
	for _, tok := range []string{"tok-1", "tok-2"} {
		if code, _ := get("/.well-known/acme-challenge/" + tok); code != 404 {
			t.Errorf("%s after clean up: %d", tok, code)
		}
	}

	if m.Challenge("mail.example.org") != nil {
		t.Error("a TLS-ALPN-01 certificate before one is presented")
	}
	alpnSolver{other}.Present("mail.example.org", "t", "t.thumb")
	c := m.Challenge("MAIL.example.org")
	if c == nil {
		t.Fatal("no shared TLS-ALPN-01 certificate")
	}
	leaf, _ := x509.ParseCertificate(c.Certificate[0])
	found := false
	for _, ext := range leaf.Extensions {
		found = found || ext.Id.Equal(asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 31}) // id-pe-acmeIdentifier
	}
	if !found || leaf.DNSNames[0] != "mail.example.org" {
		t.Errorf("challenge certificate %v %v", leaf.DNSNames, found)
	}
	alpnSolver{other}.CleanUp("mail.example.org", "t", "")
	if m.Challenge("mail.example.org") != nil {
		t.Error("still there after clean up")
	}
}

// TestReadEnv: KEY=value lines as shells and systemd write them.
func TestReadEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "env")
	os.WriteFile(p, []byte("# Cloudflare\nCF_DNS_API_TOKEN=abc\nexport CF_Account_ID = \"x y\"\n\nQ='z'\n"), 0o600)
	env, err := ReadEnv(p)
	if err != nil || env["CF_DNS_API_TOKEN"] != "abc" || env["CF_Account_ID"] != "x y" || env["Q"] != "z" || len(env) != 3 {
		t.Errorf("%v %v", env, err)
	}
	os.WriteFile(p, []byte("not a line\n"), 0o600)
	if _, err := ReadEnv(p); err == nil {
		t.Error("took a bad line")
	}
}

// TestAcmesh: an acme.sh dnsapi script is run with acme.sh's functions,
// given the record's name and value; a missing one is an error.
func TestAcmesh(t *testing.T) {
	dir := t.TempDir()
	// enough of acme.sh: its last line runs its arguments, and the two
	// functions the bridge calls
	os.WriteFile(filepath.Join(dir, "acme.sh"), []byte(`
_initpath() { :; }
_findHook() { [ -f "$_SCRIPT_HOME/$2/$3.sh" ] && printf %s "$_SCRIPT_HOME/$2/$3.sh"; }
_info() { echo "$@"; }
main() { "$@"; }
main "$@"
`), 0o755)
	os.Mkdir(filepath.Join(dir, "dnsapi"), 0o755)
	os.WriteFile(filepath.Join(dir, "dnsapi", "dns_test.sh"), []byte(`
dns_test_add() { _info add; echo "add $1 $2 $T_Token" >> "$LE_WORKING_DIR/log"; }
dns_test_rm() { echo "rm $1 $2" >> "$LE_WORKING_DIR/log"; }
`), 0o644)
	m := testManager(t)
	m.S.ACMEsh = filepath.Join(dir, "acme.sh")
	t.Setenv("T_Token", "sekrit")
	p, err := m.acmesh("dns_test")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Present("example.org", "tok", "tok.thumb"); err != nil {
		t.Fatal(err)
	}
	if err := p.CleanUp("example.org", "tok", "tok.thumb"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "log"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "add _acme-challenge.example.org ") || !strings.HasSuffix(lines[0], " sekrit") ||
		!strings.HasPrefix(lines[1], "rm _acme-challenge.example.org ") {
		t.Errorf("log %q", lines)
	}
	if p, _ := m.acmesh("dns_none"); p.Present("example.org", "t", "t.k") == nil {
		t.Error("no error for a missing script")
	}
	if _, err := m.acmesh("../x"); err == nil {
		t.Error("took a path")
	}
}
