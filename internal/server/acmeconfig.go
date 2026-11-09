package server

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"slices"
	"strings"

	"github.com/D-Modem/bbs/internal/acme"
	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/config"
)

// acmeSection reads [acme] and its [acme NAME] certificates.
func (c *Config) acmeSection(cf *config.File) error {
	var s *config.Section
	var named []*config.Section
	for _, sec := range cf.All("acme") {
		if sec.Name == "" {
			if s != nil {
				return fmt.Errorf("[acme] line %d: a second [acme]", sec.Line)
			}
			s = sec
		} else {
			named = append(named, sec)
		}
	}
	if s == nil {
		if len(named) > 0 {
			return fmt.Errorf("[acme %s] needs an [acme] section", named[0].Name)
		}
		return nil
	}
	a := &acme.Settings{Dir: s.String("dir", filepath.Join(c.DataDir(), "acme")), Email: s.String("email", ""),
		EABKeyID: s.String("eab_kid", ""), EABHMAC: s.String("eab_hmac_key", ""),
		DNSEnvFile: s.String("dns_env_file", ""), ACMEsh: s.String("acmesh", ""),
		ACMEshHome: s.String("acmesh_home", ""), Hook: s.String("hook", "")}
	ca := s.String("ca", "letsencrypt")
	switch {
	case acme.CAs[strings.ToLower(ca)] != "":
		a.CA = acme.CAs[strings.ToLower(ca)]
	case strings.HasPrefix(ca, "https://"), strings.HasPrefix(ca, "http://"):
		a.CA = ca
	default:
		return fmt.Errorf("[acme] ca: %q is not a directory URL or one of letsencrypt, letsencrypt-staging, zerossl, buypass, google, sslcom", ca)
	}
	if (a.EABKeyID == "") != (a.EABHMAC == "") {
		return errors.New("[acme] eab_kid and eab_hmac_key go together")
	}
	if a.CA == acme.CAs["zerossl"] && a.EABKeyID == "" && a.Email == "" {
		return errors.New("[acme] ca = zerossl needs email (or eab_kid and eab_hmac_key)")
	}
	a.KeyType = strings.ToLower(s.String("key_type", "ec256"))
	if _, ok := acme.KeyTypes[a.KeyType]; !ok {
		return fmt.Errorf("[acme] key_type: %q is not ec256, ec384, rsa2048, rsa3072 or rsa4096", a.KeyType)
	}
	var err error
	if a.RenewDays, err = s.Int("renew_days", 30, 1, 89); err != nil {
		return fmt.Errorf("[acme] %w", err)
	}
	a.Resolvers = splitList(s.String("dns_resolvers", ""))
	switch a.DNSCheck = strings.ToLower(s.String("dns_check", "authoritative")); a.DNSCheck {
	case "authoritative", "no":
	case "resolvers":
		if len(a.Resolvers) == 0 {
			return errors.New("[acme] dns_check = resolvers needs dns_resolvers")
		}
	default:
		return fmt.Errorf("[acme] dns_check: %q is not authoritative, resolvers or no", a.DNSCheck)
	}
	if a.DNSWait, err = s.Seconds("dns_wait", 0, 0, 3600); err != nil {
		return fmt.Errorf("[acme] %w", err)
	}
	defChallenge := strings.ToLower(s.String("challenge", acme.HTTP01))
	defDNS := s.String("dns", "")
	cert := func(sec *config.Section, name string, required bool) error {
		ct := acme.Cert{Name: name, Domains: splitList(strings.ToLower(sec.String("domains", "")))}
		where := "[acme]"
		if sec != s {
			where = "[acme " + name + "]"
		}
		if len(ct.Domains) == 0 {
			if required {
				return fmt.Errorf("%s domains: none given", where)
			}
			return nil
		}
		ct.Challenge, ct.DNS, ct.KeyType = defChallenge, defDNS, a.KeyType
		if sec != s {
			ct.Challenge = strings.ToLower(sec.String("challenge", defChallenge))
			ct.DNS = sec.String("dns", defDNS)
			ct.KeyType = strings.ToLower(sec.String("key_type", a.KeyType))
		}
		if _, ok := acme.KeyTypes[ct.KeyType]; !ok {
			return fmt.Errorf("%s key_type: %q is not ec256, ec384, rsa2048, rsa3072 or rsa4096", where, ct.KeyType)
		}
		for _, d := range ct.Domains {
			if strings.HasPrefix(d, "*.") && ct.Challenge != acme.DNS01 {
				return fmt.Errorf("%s domains: %s, a wildcard, needs challenge = dns", where, d)
			}
			if strings.ContainsAny(d, "/: ") || !strings.Contains(d, ".") {
				return fmt.Errorf("%s domains: %q is not a domain name", where, d)
			}
		}
		switch ct.Challenge {
		case acme.HTTP01, acme.TLSALPN01:
		case acme.DNS01:
			if err := acme.CheckDNS(ct.DNS); err != nil {
				return fmt.Errorf("%s %w", where, err)
			}
			if strings.HasPrefix(ct.DNS, "acmesh:") && a.ACMEsh == "" {
				return fmt.Errorf("%s dns = %s needs acmesh, acme.sh's path, in [acme]", where, ct.DNS)
			}
		default:
			return fmt.Errorf("%s challenge: %q is not http, tls-alpn or dns", where, ct.Challenge)
		}
		a.Certs = append(a.Certs, ct)
		return nil
	}
	if err := cert(s, acme.DefaultName, false); err != nil {
		return err
	}
	for _, sec := range named {
		if sec.Name == acme.DefaultName || slices.Contains(a.Names(), sec.Name) {
			return fmt.Errorf("[acme %s] line %d: that name is taken", sec.Name, sec.Line)
		}
		if strings.ContainsAny(sec.Name, "/\\:") {
			return fmt.Errorf("[acme %s]: a name without / \\ or :", sec.Name)
		}
		if err := cert(sec, sec.Name, true); err != nil {
			return err
		}
	}
	if len(a.Certs) == 0 {
		return errors.New("[acme] domains: none given (here or in an [acme NAME])")
	}
	if a.Email == "" {
		c.Warnings = append(c.Warnings, "[acme] has no email: the CA cannot warn of a certificate that is not being renewed")
	}
	a.HTTPAddress = s.String("http_address", "")
	if strings.EqualFold(a.HTTPAddress, "no") {
		a.HTTPAddress = ""
	} else if a.HTTPAddress != "" {
		if _, _, err := net.SplitHostPort(a.HTTPAddress); err != nil {
			return fmt.Errorf("[acme] http_address: %v", err)
		}
	}
	c.ACME = a
	c.acmeHTTPDefault = !s.Has("http_address")
	return nil
}

// acmeHTTP settles where HTTP-01 is answered, the other sections read: a
// web server of the BBS's on port 80 answers it, or else a listener of the
// ACME client's on :80 does, if a certificate uses it.
func (c *Config) acmeHTTP() {
	a := c.ACME
	if a == nil || !c.acmeHTTPDefault {
		return
	}
	uses := false
	for _, ct := range a.Certs {
		uses = uses || ct.Challenge == acme.HTTP01
	}
	if !uses || c.port80() {
		return
	}
	a.HTTPAddress = ":80"
}

// port80 reports whether a plain web server of the BBS's is on port 80
// (they answer HTTP-01 themselves).
func (c *Config) port80() bool {
	on80 := func(addr string) bool {
		_, p, err := net.SplitHostPort(addr)
		return err == nil && p == "80"
	}
	if h := c.HTTP; h != nil && !h.TLS && on80(h.Address) {
		return true
	}
	if y := c.YMSG; y != nil && on80(y.HTTPAddress) {
		return true
	}
	return false
}

// acmeNames are the ACME certificates' names (nil: no [acme]).
func (c *Config) acmeNames() []string {
	if c.ACME == nil {
		return nil
	}
	return c.ACME.Names()
}

// tlsFiles is a TLS server's cert_file and key_file: those given, or the
// ACME client's if neither is; checked.
func (c *Config) tlsFiles(certFile, keyFile string) (string, string, error) {
	certFile, keyFile = certs.Default(certFile, keyFile, c.ACME != nil)
	return certFile, keyFile, certs.Check(certFile, keyFile, c.acmeNames())
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		out = append(out, f)
	}
	return out
}
