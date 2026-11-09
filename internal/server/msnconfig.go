package server

import (
	"fmt"
	"net"
	"strings"

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
			Site: s.String("site", "")}
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
