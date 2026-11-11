package server

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/msn"
)

// msnSection reads [msn]: the MSN Messenger server for the chat.
func (c *Config) msnSection(cf *config.File) error {
	s := cf.Get("msn", "")
	if s == nil {
		return nil
	}
	err := func() error {
		m := &msn.Settings{Address: s.String("address", ":1863"), Host: s.String("host", ""),
			Domain: strings.ToLower(s.String("domain", "")), System: s.String("system", "BBS"),
			Site: s.String("site", ""), HTTPSAddress: s.String("https_address", ""),
			HTTPAddress: s.String("http_address", ""), CertFile: s.String("tls_cert", ""),
			KeyFile: s.String("tls_key", ""), PassportHost: s.String("passport_host", "")}
		for _, a := range []struct{ key, v string }{{"https_address", m.HTTPSAddress}, {"http_address", m.HTTPAddress}} {
			if a.v == "" {
				continue
			}
			if _, _, err := net.SplitHostPort(a.v); err != nil {
				return fmt.Errorf("%s: %v", a.key, err)
			}
		}
		if m.HTTPSAddress != "" {
			if m.CertFile == "" || m.KeyFile == "" && !certs.IsACME(m.CertFile) {
				return errors.New("https_address needs tls_cert and tls_key (one for nexus.passport.com, " +
					"login.passport.com and login.live.com: not names a CA will sign for this host)")
			}
			if err := certs.Check(m.CertFile, m.KeyFile, c.acmeNames()); err != nil {
				return fmt.Errorf("tls_cert, tls_key: %v", err)
			}
		}
		if m.PassportHost != "" && strings.ContainsAny(m.PassportHost, "/ @") {
			return errors.New("passport_host: a host name, with :port if not 443 (where clients log in)")
		}
		if _, _, err := net.SplitHostPort(m.Address); err != nil {
			return fmt.Errorf("address: %v", err)
		}
		if m.Host != "" {
			if _, _, err := net.SplitHostPort(m.Host); err != nil {
				return fmt.Errorf("host: %v (host:port, where clients are sent for conversations)", err)
			}
		}
		if m.Domain != "" && (strings.ContainsAny(m.Domain, "@ /") || !strings.Contains(m.Domain, ".")) {
			return fmt.Errorf("domain: a domain name, as msn.example.org (handles are name@domain)")
		}
		if m.System == "" || len(m.System) > 32 || strings.ContainsAny(m.System, "@ ") {
			return fmt.Errorf("system: a name of 1 to 32 letters and digits")
		}
		if m.Site != "" && !strings.HasPrefix(m.Site, "http://") && !strings.HasPrefix(m.Site, "https://") {
			return fmt.Errorf("site: a web address (http:// or https://)")
		}
		var err error
		if m.MaxClients, err = s.Int("max_clients", 256, 1, 100000); err != nil {
			return err
		}
		// each client opens a connection per conversation
		if m.MaxPerIP, err = s.Int("max_per_ip", 16, 0, 1000); err != nil {
			return err
		}
		c.MSN = m
		return nil
	}()
	if err != nil {
		return fmt.Errorf("[msn] %w", err)
	}
	return nil
}
