package server

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/irclink"
)

// irclinkSection reads [irclink] and its [ircpeer NAME]s: the BBS as a
// server on an IRC network.
func (c *Config) irclinkSection(cf *config.File) error {
	s := cf.Get("irclink", "")
	peers := cf.All("ircpeer")
	if s == nil {
		if len(peers) > 0 {
			return errors.New("[ircpeer] without [irclink]")
		}
		return nil
	}
	err := func() error {
		l := &irclink.Settings{ServerName: s.String("server_name", ""), SID: s.String("sid", ""),
			Description: s.String("description", ""), Network: s.String("network", ""),
			Tag: s.String("tag", ""), Host: s.String("host", ""), Prefix: s.String("prefix", "irc-"),
			Address: s.String("address", ":7000"), TLSAddress: s.String("tls_address", ""),
			CertFile: s.String("tls_cert", ""), KeyFile: s.String("tls_key", "")}
		if l.Prefix == "none" {
			l.Prefix = ""
		}
		if l.Prefix != "" && !roomPrefix.MatchString(l.Prefix) {
			return errors.New("prefix: what begins the linked rooms: up to 10 lower case letters, digits and -")
		}
		if l.Address == "none" {
			l.Address = ""
		}
		var err error
		if l.NickLen, err = s.Int("nick_length", 30, 9, 64); err != nil {
			return err
		}
		if l.Retry, err = s.Seconds("retry", 30, 1, 3600); err != nil {
			return err
		}
		for _, a := range []struct{ key, v string }{{"address", l.Address}, {"tls_address", l.TLSAddress}} {
			if _, _, err := net.SplitHostPort(a.v); a.v != "" && err != nil {
				return fmt.Errorf("%s: %v", a.key, err)
			}
		}
		if l.TLSAddress != "" && (l.CertFile == "" || l.KeyFile == "") {
			return errors.New("tls_address needs tls_cert and tls_key")
		}
		if l.Network == "" {
			l.Network = c.Settings.Name
		}
		for _, ps := range peers {
			p := irclink.Peer{Name: ps.Name, Address: ps.String("address", ""),
				SendPassword: ps.String("send_password", ""), AcceptPassword: ps.String("accept_password", ""),
				Tag: ps.String("tag", "")}
			if p.TLS, err = ps.Bool("tls", false); err != nil {
				return fmt.Errorf("[ircpeer %s] %w", ps.Name, err)
			}
			if p.TLSVerify, err = ps.Bool("tls_verify", true); err != nil {
				return fmt.Errorf("[ircpeer %s] %w", ps.Name, err)
			}
			switch proto := strings.ToLower(ps.String("protocol", "ts6")); proto {
			case "ts6":
			case "hybrid":
				p.Hybrid = true
			default:
				return fmt.Errorf("[ircpeer %s] protocol: ts6 or hybrid, not %q", ps.Name, proto)
			}
			if p.Address == "none" {
				p.Address = ""
			}
			if _, _, err := net.SplitHostPort(p.Address); p.Address != "" && err != nil {
				return fmt.Errorf("[ircpeer %s] address: %v", ps.Name, err)
			}
			l.Peers = append(l.Peers, p)
		}
		if err := l.Check(c.Settings.Name); err != nil {
			return err
		}
		if len(l.Peers) == 0 {
			c.Warnings = append(c.Warnings, "[irclink] no [ircpeer NAME]: nothing links to it")
		}
		if l.Address != "" && l.TLSAddress == "" {
			c.Warnings = append(c.Warnings, "[irclink] links in by plain TCP: their passwords go in the clear (tls_address)")
		}
		c.IRCLink = l
		return nil
	}()
	if err != nil {
		return fmt.Errorf("[irclink] %w", err)
	}
	return nil
}
