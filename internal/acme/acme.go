// Package acme is the BBS's ACME client, in the manner of acme.sh: it gets
// certificates from Let's Encrypt, ZeroSSL or any ACME CA, keeps them
// beside the database, and renews them before they run out, for every TLS
// server of the BBS to share (internal/certs).
//
// Each certificate is proved by one challenge: HTTP-01 (a token on port
// 80: the BBS's own web server, or a listener of the client's), TLS-ALPN-01
// (a certificate on port 443, from any TLS server of the BBS there) or
// DNS-01 (a TXT record, through a DNS provider's API, a program of the
// sysop's, or acme.sh's own dnsapi scripts), the only one for wildcards.
//
// It is built on lego (github.com/go-acme/lego, MIT).
package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/challenge/tlsalpn01"
	"github.com/go-acme/lego/v4/lego"
	legolog "github.com/go-acme/lego/v4/log"

	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/sockets"
)

// The CAs known by name; ca may also be a directory's URL.
var CAs = map[string]string{
	"letsencrypt":         lego.LEDirectoryProduction,
	"letsencrypt-staging": lego.LEDirectoryStaging,
	"zerossl":             "https://acme.zerossl.com/v2/DV90",
	"buypass":             "https://api.buypass.com/acme/directory",
	"buypass-test":        "https://api.test4.buypass.no/acme/directory",
	"google":              "https://dv.acme-v02.api.pki.goog/directory",
	"google-test":         "https://dv.acme-v02.test-api.pki.goog/directory",
	"sslcom":              "https://acme.ssl.com/sslcom-dv-ecc",
}

// KeyTypes are the keys a certificate can have, by their names here.
var KeyTypes = map[string]certcrypto.KeyType{
	"ec256": certcrypto.EC256, "ec384": certcrypto.EC384,
	"rsa2048": certcrypto.RSA2048, "rsa3072": certcrypto.RSA3072, "rsa4096": certcrypto.RSA4096,
}

// Challenges.
const (
	HTTP01    = "http"
	TLSALPN01 = "tls-alpn"
	DNS01     = "dns"
)

// DefaultName is the name of [acme]'s own certificate (its domains).
const DefaultName = "default"

// Settings is [acme] and its [acme NAME] certificates.
type Settings struct {
	Dir         string        // the account and certificates
	Email       string        // the account's contact, told of problems
	CA          string        // the directory's URL
	EABKeyID    string        // External Account Binding (ZeroSSL's, Google's)
	EABHMAC     string        // base64url
	KeyType     string        // a KeyTypes name
	RenewDays   int           // renew this many days before the end (or when the CA says)
	HTTPAddress string        // a listener for HTTP-01 ("": the [http] server answers)
	DNSEnvFile  string        // KEY=value lines: the DNS provider's credentials
	Resolvers   []string      // asked whether a DNS-01 record is out yet
	DNSCheck    string        // how: "authoritative" (its zone's servers), "resolvers" (Resolvers) or "no"
	DNSWait     time.Duration // before the CA is told to look (acme.sh's --dnssleep)
	ACMEsh      string        // acme.sh, for dns = acmesh:dns_xx
	ACMEshHome  string        // its home (LE_WORKING_DIR): its saved credentials
	Hook        string        // run after a certificate is renewed
	Certs       []Cert        // the first is the default
}

// Cert is one certificate.
type Cert struct {
	Name      string
	Domains   []string // the first is its subject
	Challenge string   // HTTP01, TLSALPN01 or DNS01
	DNS       string   // DNS-01's provider: a DNSProviders name, exec:PROGRAM or acmesh:dns_xx
	KeyType   string
}

// Names lists the certificates' names.
func (s *Settings) Names() []string {
	var out []string
	for _, c := range s.Certs {
		out = append(out, c.Name)
	}
	return out
}

// Manager keeps the certificates.
type Manager struct {
	S   Settings
	Log *log.Logger
	Now func() time.Time

	mu     sync.RWMutex
	held   map[string]*held
	tokens map[string]string           // HTTP-01: token → key authorization
	alpn   map[string]*tls.Certificate // TLS-ALPN-01: name → its certificate
	ari    map[string]time.Time        // when the CA says to renew, by certificate
	envSet bool

	obtain sync.Mutex // one order at a time
}

// held is a certificate as it is on disk.
type held struct {
	cert    *tls.Certificate
	mod     time.Time
	checked time.Time
}

// New is a manager for s; it reads the certificates already got.
func New(s Settings, lg *log.Logger) (*Manager, error) {
	if lg == nil {
		lg = log.New(os.Stderr, "", log.LstdFlags)
	}
	if len(s.Certs) == 0 {
		return nil, errors.New("acme: no certificates (domains)")
	}
	if s.RenewDays == 0 {
		s.RenewDays = 30
	}
	m := &Manager{S: s, Log: lg, Now: time.Now, held: map[string]*held{},
		tokens: map[string]string{}, alpn: map[string]*tls.Certificate{}, ari: map[string]time.Time{}}
	for _, c := range s.Certs {
		m.held[c.Name] = &held{}
		m.load(c.Name)
	}
	return m, nil
}

func (m *Manager) cert(name string) *Cert {
	for i := range m.S.Certs {
		if m.S.Certs[i].Name == name {
			return &m.S.Certs[i]
		}
	}
	return nil
}

// Has reports whether there is a certificate called name.
func (m *Manager) Has(name string) bool { return m.cert(name) != nil }

// load reads name's certificate from disk if it has changed (looked at no
// more than every 30 seconds): renewed by this process, or by bbs acme
// in another.
func (m *Manager) load(name string) *tls.Certificate {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.held[name]
	if h == nil {
		return nil
	}
	now := time.Now()
	if h.cert != nil && now.Sub(h.checked) < 30*time.Second {
		return h.cert
	}
	h.checked = now
	crt, key := m.files(name)
	fi, err := os.Stat(crt)
	if err != nil || (h.cert != nil && fi.ModTime().Equal(h.mod)) {
		return h.cert
	}
	c, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		m.Log.Printf("acme: %s: %v", name, err)
		return h.cert
	}
	h.cert, h.mod = &c, fi.ModTime()
	return h.cert
}

// Certificate is the certificate called name, or ("") the one whose names
// cover hello's server name, or the first there is.
func (m *Manager) Certificate(name string, hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if name != "" {
		if c := m.load(name); c != nil {
			return c, nil
		}
		return nil, fmt.Errorf("acme: no certificate %s yet", name)
	}
	var first *tls.Certificate
	for _, c := range m.S.Certs {
		tc := m.load(c.Name)
		if tc == nil {
			continue
		}
		if first == nil {
			first = tc
		}
		if hello != nil && hello.ServerName != "" && tc.Leaf != nil && tc.Leaf.VerifyHostname(hello.ServerName) == nil {
			return tc, nil
		}
	}
	if first == nil {
		return nil, errors.New("acme: no certificate yet")
	}
	return first, nil
}

// Challenge is TLS-ALPN-01's certificate for serverName, while it is being
// validated.
func (m *Manager) Challenge(serverName string) *tls.Certificate {
	name := strings.ToLower(serverName)
	m.mu.RLock()
	c := m.alpn[name]
	m.mu.RUnlock()
	if c != nil || !safeName(name) {
		return c
	}
	// one bbs acme renew is answering through this process
	b, err := os.ReadFile(filepath.Join(m.challengeDir("alpn"), name))
	if err != nil {
		return nil
	}
	tc, err := tls.X509KeyPair(b, b)
	if err != nil {
		return nil
	}
	return &tc
}

// ServeHTTP answers HTTP-01's /.well-known/acme-challenge/TOKEN.
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.URL.Path, certs.ChallengePath)
	m.mu.RLock()
	ka, found := m.tokens[token]
	m.mu.RUnlock()
	if ok && !found && safeName(token) {
		// one bbs acme renew is answering through this process
		if b, err := os.ReadFile(filepath.Join(m.challengeDir("http"), token)); err == nil {
			ka, found = string(b), true
		}
	}
	if !ok || !found {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(ka))
}

// Run gets the certificates there are not yet, and renews them, until ctx
// is done; it also answers HTTP-01 on HTTPAddress, if set.
func (m *Manager) Run(ctx context.Context) error {
	if m.S.HTTPAddress != "" {
		ln, err := sockets.Listen("tcp", m.S.HTTPAddress)
		if err != nil {
			return fmt.Errorf("acme: http_address: %w", err)
		}
		hs := &http.Server{Handler: m, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
		go hs.Serve(ln)
		go func() { <-ctx.Done(); hs.Close() }()
		m.Log.Printf("acme: answering HTTP-01 on %s", ln.Addr())
	}
	for {
		wait := m.RenewAll(ctx, false)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// RenewAll gets each certificate that is due (all of them, if force), and
// says how long until it should look again: 12 hours, or one after a
// failure.
func (m *Manager) RenewAll(ctx context.Context, force bool) time.Duration {
	wait := 12*time.Hour + time.Duration(rand.Int64N(int64(time.Hour)))
	for _, c := range m.S.Certs {
		if ctx.Err() != nil {
			break
		}
		if !force {
			m.askARI(ctx, c.Name)
		}
		due, why := m.Due(c.Name)
		if !due && !force {
			continue
		}
		m.Log.Printf("acme: %s: getting a certificate for %s (%s)", c.Name, strings.Join(c.Domains, ", "), why)
		if err := m.Obtain(ctx, c.Name); err != nil {
			m.Log.Printf("acme: %s: %v", c.Name, err)
			wait = min(wait, time.Hour)
			continue
		}
		m.Log.Printf("acme: %s: got it", c.Name)
	}
	return wait
}

// Due reports whether name's certificate should be got now, and why.
func (m *Manager) Due(name string) (bool, string) {
	c := m.cert(name)
	tc := m.load(name)
	if c == nil {
		return false, "no such certificate"
	}
	if tc == nil || tc.Leaf == nil {
		return true, "none yet"
	}
	leaf := tc.Leaf
	for _, d := range c.Domains {
		if !slices.Contains(leaf.DNSNames, d) {
			return true, "the domains have changed"
		}
	}
	if len(leaf.DNSNames) != len(c.Domains) {
		return true, "the domains have changed"
	}
	if kt := keyType(leaf); kt != m.keyType(c) {
		return true, "the key type has changed"
	}
	left := leaf.NotAfter.Sub(m.Now())
	if left < time.Duration(m.S.RenewDays)*24*time.Hour {
		return true, fmt.Sprintf("it runs out %s", leaf.NotAfter.Format(time.DateOnly))
	}
	if t := m.suggested(name); !t.IsZero() && !m.Now().Before(t) {
		return true, "the CA asks for it to be renewed"
	}
	return false, ""
}

// keyType names a certificate's key as KeyTypes does.
func keyType(leaf *x509.Certificate) string {
	switch k := leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		return fmt.Sprintf("rsa%d", k.N.BitLen())
	case *ecdsa.PublicKey:
		return fmt.Sprintf("ec%d", k.Curve.Params().BitSize)
	}
	return ""
}

func (m *Manager) keyType(c *Cert) string {
	if c.KeyType != "" {
		return c.KeyType
	}
	if m.S.KeyType != "" {
		return m.S.KeyType
	}
	return "ec256"
}

// files are name's certificate (the chain) and key.
func (m *Manager) files(name string) (string, string) {
	return filepath.Join(m.certDir(), name+".crt"), filepath.Join(m.certDir(), name+".key")
}

func (m *Manager) certDir() string { return filepath.Join(m.S.Dir, "certs") }

// Files are the certificate's and key's paths (for a sysop's other
// programs).
func (m *Manager) Files(name string) (string, string) { return m.files(name) }

// Obtain gets name's certificate now, and keeps it.
func (m *Manager) Obtain(ctx context.Context, name string) error {
	c := m.cert(name)
	if c == nil {
		return fmt.Errorf("no certificate %s", name)
	}
	m.obtain.Lock()
	defer m.obtain.Unlock()
	legolog.Logger = log.New(m.Log.Writer(), m.Log.Prefix()+"acme: lego: ", m.Log.Flags())
	client, err := m.client(ctx, c)
	if err != nil {
		return err
	}
	if err := m.challenge(client, c); err != nil {
		return err
	}
	res, err := client.Certificate.Obtain(certificate.ObtainRequest{Domains: c.Domains, Bundle: true})
	if err != nil {
		return err
	}
	if err := m.keep(name, res); err != nil {
		return err
	}
	m.mu.Lock()
	m.held[name].checked = time.Time{}
	m.mu.Unlock()
	m.load(name)
	m.hook(c)
	return nil
}

// keep writes a certificate and its key, the key first and both by
// renaming, so a reader never sees half of one or a key that is not its.
func (m *Manager) keep(name string, res *certificate.Resource) error {
	crt, key := m.files(name)
	if err := os.MkdirAll(m.certDir(), 0o700); err != nil {
		return err
	}
	if err := writeFile(key, res.PrivateKey, 0o600); err != nil {
		return err
	}
	return writeFile(crt, res.Certificate, 0o644)
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// hook runs the sysop's command after a renewal, with the certificate's
// name, files and domains in its environment.
func (m *Manager) hook(c *Cert) {
	if m.S.Hook == "" {
		return
	}
	crt, key := m.files(c.Name)
	cmd := exec.Command("/bin/sh", "-c", m.S.Hook)
	cmd.Env = append(os.Environ(), "CERT_NAME="+c.Name, "CERT_FILE="+crt, "KEY_FILE="+key,
		"CERT_DOMAINS="+strings.Join(c.Domains, " "))
	out, err := cmd.CombinedOutput()
	if err != nil {
		m.Log.Printf("acme: %s: hook: %v: %s", c.Name, err, strings.TrimSpace(string(out)))
	}
}

// client is a lego client for c, with the account (registered now if it
// is new).
func (m *Manager) client(ctx context.Context, c *Cert) (*lego.Client, error) {
	u, err := m.account(ctx)
	if err != nil {
		return nil, err
	}
	cfg := lego.NewConfig(u)
	cfg.CADirURL = m.S.CA
	cfg.UserAgent = "D-Modem-BBS"
	cfg.Certificate.KeyType = KeyTypes[m.keyType(c)]
	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	if u.Registration == nil {
		if err := m.register(client, u); err != nil {
			return nil, err
		}
	}
	return client, nil
}

// challenge sets c's challenge on client.
func (m *Manager) challenge(client *lego.Client, c *Cert) error {
	var err error
	switch c.Challenge {
	case HTTP01:
		err = client.Challenge.SetHTTP01Provider(httpSolver{m})
	case TLSALPN01:
		err = client.Challenge.SetTLSALPN01Provider(alpnSolver{m})
	case DNS01:
		p, perr := m.dnsProvider(c.DNS)
		if perr != nil {
			return perr
		}
		var opts []dns01.ChallengeOption
		if len(m.S.Resolvers) > 0 {
			opts = append(opts, dns01.AddRecursiveNameservers(dns01.ParseNameservers(m.S.Resolvers)))
		}
		switch m.S.DNSCheck {
		case "resolvers":
			opts = append(opts, dns01.RecursiveNSsPropagationRequirement())
		case "no":
			opts = append(opts, dns01.PropagationWait(m.S.DNSWait, true))
		}
		if m.S.DNSWait > 0 && m.S.DNSCheck != "no" {
			opts = append(opts, dns01.PropagationWait(m.S.DNSWait, false))
		}
		err = client.Challenge.SetDNS01Provider(p, opts...)
	default:
		err = fmt.Errorf("challenge %q", c.Challenge)
	}
	return err
}

// challengeDir is where challenges are put for a running bbs serve to
// answer when another process (bbs acme renew) has them: kind "http"
// (TOKEN: the key authorization) or "alpn" (NAME: certificate and key).
func (m *Manager) challengeDir(kind string) string {
	return filepath.Join(m.S.Dir, "challenges", kind)
}

// safeName is a token or host name fit to be a file's name.
func safeName(s string) bool {
	if s == "" || len(s) > 255 || s[0] == '.' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// share puts a challenge on disk too (best effort), for a bbs serve to
// answer while this process is bbs acme renew.
func (m *Manager) share(kind, name string, data []byte) {
	if !safeName(name) {
		return
	}
	dir := m.challengeDir(kind)
	if os.MkdirAll(dir, 0o755) == nil {
		os.WriteFile(filepath.Join(dir, name), data, 0o644)
	}
}

func (m *Manager) unshare(kind, name string) {
	if safeName(name) {
		os.Remove(filepath.Join(m.challengeDir(kind), name))
	}
}

// httpSolver answers HTTP-01 through ServeHTTP.
type httpSolver struct{ m *Manager }

func (s httpSolver) Present(domain, token, keyAuth string) error {
	s.m.mu.Lock()
	s.m.tokens[token] = keyAuth
	s.m.mu.Unlock()
	s.m.share("http", token, []byte(keyAuth))
	return nil
}

func (s httpSolver) CleanUp(domain, token, keyAuth string) error {
	s.m.mu.Lock()
	delete(s.m.tokens, token)
	s.m.mu.Unlock()
	s.m.unshare("http", token)
	return nil
}

// alpnSolver answers TLS-ALPN-01 through the TLS servers (certs.Config).
type alpnSolver struct{ m *Manager }

func (s alpnSolver) Present(domain, token, keyAuth string) error {
	c, err := tlsalpn01.ChallengeCert(domain, keyAuth)
	if err != nil {
		return err
	}
	s.m.mu.Lock()
	s.m.alpn[strings.ToLower(domain)] = c
	s.m.mu.Unlock()
	if pemCert, pemKey, err := tlsalpn01.ChallengeBlocks(domain, keyAuth); err == nil {
		// the key is only this challenge's: it may be read by anyone
		s.m.share("alpn", strings.ToLower(domain), append(pemCert, pemKey...))
	}
	return nil
}

func (s alpnSolver) CleanUp(domain, token, keyAuth string) error {
	s.m.mu.Lock()
	delete(s.m.alpn, strings.ToLower(domain))
	s.m.mu.Unlock()
	s.m.unshare("alpn", strings.ToLower(domain))
	return nil
}

var _ certs.Manager = (*Manager)(nil)

// Status is a certificate as bbs acme list and bbs check show it.
type Status struct {
	Cert
	CertFile, KeyFile string
	Have              bool
	Issuer            string
	NotAfter          time.Time
	Due               bool
	Why               string
}

// Status describes each certificate.
func (m *Manager) Status() []Status {
	var out []Status
	for _, c := range m.S.Certs {
		st := Status{Cert: c}
		st.CertFile, st.KeyFile = m.files(c.Name)
		if tc := m.load(c.Name); tc != nil && tc.Leaf != nil {
			st.Have, st.Issuer, st.NotAfter = true, tc.Leaf.Issuer.CommonName, tc.Leaf.NotAfter
			if o := tc.Leaf.Issuer.Organization; len(o) > 0 {
				st.Issuer = o[0] + " " + st.Issuer
			}
		}
		st.Due, st.Why = m.Due(c.Name)
		out = append(out, st)
	}
	return out
}
