package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"

	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/ircd"
)

// ircSection reads [irc]: the IRC server for the chat rooms.
func (c *Config) ircSection(cf *config.File) error {
	s := cf.Get("irc", "")
	if s == nil {
		return nil
	}
	err := func() error {
		i := &ircd.Settings{Address: s.String("address", ":6667"), TLSAddress: s.String("tls_address", ""),
			CertFile: s.String("tls_cert", ""), KeyFile: s.String("tls_key", ""),
			ServerName: s.String("server_name", ""), Network: s.String("network", ""),
			Service: s.String("service", "BBSServ")}
		if !ircd.ValidNick(i.Service) {
			return errors.New("service: the bot's nick, a letter then letters, digits, - and _")
		}
		if i.Address == "none" { // TLS only
			i.Address = ""
		}
		for _, a := range []struct{ key, v string }{{"address", i.Address}, {"tls_address", i.TLSAddress}} {
			if a.v == "" {
				continue
			}
			if _, _, err := net.SplitHostPort(a.v); err != nil {
				return fmt.Errorf("%s: %v", a.key, err)
			}
		}
		if i.Address == "" && i.TLSAddress == "" {
			return errors.New("address or tls_address: somewhere to listen")
		}
		if i.TLSAddress != "" {
			if i.CertFile == "" || i.KeyFile == "" {
				return errors.New("tls_address needs tls_cert and tls_key")
			}
			if _, err := tls.LoadX509KeyPair(i.CertFile, i.KeyFile); err != nil {
				return fmt.Errorf("tls_cert, tls_key: %v", err)
			}
		}
		var err error
		if i.MaxClients, err = s.Int("max_clients", 64, 1, 10000); err != nil {
			return err
		}
		if i.MaxPerIP, err = s.Int("max_per_ip", 4, 0, 1000); err != nil {
			return err
		}
		if i.Address != "" && i.TLSAddress == "" {
			c.Warnings = append(c.Warnings, "[irc] passwords go in the clear: give it a tls_address too")
		}
		c.IRC = i
		return nil
	}()
	if err != nil {
		return fmt.Errorf("[irc] %w", err)
	}
	return nil
}
