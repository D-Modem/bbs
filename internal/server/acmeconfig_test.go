package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/acme"
)

// TestACMEConfig: [acme] and [acme NAME] with their defaults; TLS servers
// with no files take the ACME client's certificates, Gemini and Yahoo
// only when told; HTTP-01's listener left to a web server on port 80;
// bad settings refused.
func TestACMEConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[general]\ndatabase = /var/lib/bbs/bbs.db\n[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load(`
[acme]
email = sysop@example.org
domains = bbs.example.org, www.example.org
[acme mail]
domains = mail.example.org
challenge = tls-alpn
key_type = rsa2048
[acme wild]
domains = *.example.org
challenge = dns
dns = cloudflare
[http]
address = :443
[irc]
tls_address = :6697
tls_cert = acme:wild
[gemini]
[nntp]
tls = implicit
`)
	if err != nil {
		t.Fatal(err)
	}
	a := c.ACME
	if a.CA != acme.CAs["letsencrypt"] || a.Dir != "/var/lib/bbs/acme" || a.KeyType != "ec256" || a.RenewDays != 30 ||
		a.HTTPAddress != ":80" || strings.Join(a.Names(), ",") != "default,mail,wild" {
		t.Errorf("acme %+v", a)
	}
	if ct := a.Certs[0]; strings.Join(ct.Domains, ",") != "bbs.example.org,www.example.org" || ct.Challenge != "http" {
		t.Errorf("default %+v", ct)
	}
	if ct := a.Certs[1]; ct.Challenge != "tls-alpn" || ct.KeyType != "rsa2048" {
		t.Errorf("mail %+v", ct)
	}
	if c.HTTP.CertFile != "acme" || c.IRC.CertFile != "acme:wild" || c.NNTP.CertFile != "acme" {
		t.Errorf("http %q irc %q nntp %q", c.HTTP.CertFile, c.IRC.CertFile, c.NNTP.CertFile)
	}
	if c.Gemini.CertFile != "/var/lib/bbs/gemini-cert.pem" {
		t.Errorf("gemini %q: should keep its pinned certificate", c.Gemini.CertFile)
	}

	// a plain web server on port 80 answers HTTP-01 itself
	c, err = load("[acme]\ndomains = bbs.example.org\n[http]\naddress = :80\ntls = no\n")
	if err != nil || c.ACME.HTTPAddress != "" {
		t.Errorf("port 80: %q %v", c.ACME.HTTPAddress, err)
	}
	// no HTTP-01 certificate: no listener
	c, err = load("[acme]\ndomains = bbs.example.org\nchallenge = tls-alpn\n")
	if err != nil || c.ACME.HTTPAddress != "" {
		t.Errorf("tls-alpn: %q %v", c.ACME.HTTPAddress, err)
	}
	// only [acme NAME]s; Gemini told to use one
	c, err = load("[acme]\nca = zerossl\nemail = a@example.org\n[acme g]\ndomains = gem.example.org\n[gemini]\ncert_file = acme:g\n")
	if err != nil || c.ACME.CA != acme.CAs["zerossl"] || c.Gemini.CertFile != "acme:g" ||
		!strings.Contains(strings.Join(c.Warnings, "\n"), "pin") {
		t.Errorf("gemini acme: %+v %v %v", c.Gemini, c.Warnings, err)
	}

	for _, bad := range []string{
		"[acme]\n",                                     // no domains
		"[acme]\ndomains = *.example.org\n",            // wildcard over HTTP-01
		"[acme]\ndomains = a.example.org\nca = nope\n", // unknown CA
		"[acme]\ndomains = a.example.org\nkey_type = dsa\n",
		"[acme]\ndomains = a.example.org\nchallenge = dns\n",                      // no provider
		"[acme]\ndomains = a.example.org\nchallenge = dns\ndns = nowhere\n",       // unknown provider
		"[acme]\ndomains = a.example.org\nchallenge = dns\ndns = acmesh:dns_cf\n", // no acmesh
		"[acme]\ndomains = a.example.org\nca = zerossl\n",                         // no email
		"[acme]\ndomains = a.example.org\neab_kid = x\n",
		"[acme]\ndomains = a.example.org\n[acme default]\ndomains = b.example.org\n",
		"[acme x]\ndomains = a.example.org\n", // no [acme]
		"[acme]\ndomains = a.example.org\n[irc]\ntls_address = :6697\ntls_cert = acme:none\n",
		"[http]\naddress = :443\n",                                        // TLS with no files, no [acme]
		"[irc]\ntls_address = :6697\ntls_cert = acme\n",                   // no [acme]
		"[acme]\ndomains = a.example.org\n[ymsg]\nhttps_address = :443\n", // Yahoo: never by default
	} {
		if _, err := load(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
