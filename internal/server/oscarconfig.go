package server

import (
	"fmt"
	"net"
	"strings"

	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/oscar"
)

// oscarSection reads [oscar]: the AIM and ICQ server for the chat.
func (c *Config) oscarSection(cf *config.File) error {
	s := cf.Get("oscar", "")
	if s == nil {
		return nil
	}
	err := func() error {
		o := &oscar.Settings{Address: s.String("address", ":5190"), Host: s.String("host", ""),
			System: s.String("system", "OnlineHost")}
		if _, _, err := net.SplitHostPort(o.Address); err != nil {
			return fmt.Errorf("address: %v", err)
		}
		if o.Host != "" {
			if _, _, err := net.SplitHostPort(o.Host); err != nil {
				return fmt.Errorf("host: %v (host:port, where clients reconnect)", err)
			}
		}
		if o.System == "" || len(o.System) > 16 || strings.ContainsAny(o.System, "@,;") {
			return fmt.Errorf("system: a screen name of 1 to 16 letters, digits and spaces")
		}
		var err error
		if o.MaxClients, err = s.Int("max_clients", 256, 1, 100000); err != nil {
			return err
		}
		// each client opens a few connections (sign-on, BOS, chat rooms)
		if o.MaxPerIP, err = s.Int("max_per_ip", 16, 0, 1000); err != nil {
			return err
		}
		base, err := s.Int("uin_base", 100000, 10000, 2000000000)
		if err != nil {
			return err
		}
		o.UINBase = uint32(base)
		c.OSCAR = o
		return nil
	}()
	if err != nil {
		return fmt.Errorf("[oscar] %w", err)
	}
	return nil
}
