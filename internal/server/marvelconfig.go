package server

import (
	"fmt"
	"net"

	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/marvel"
)

// marvelSection reads [marvel]: the host for the MSN client of Windows 95
// (The Microsoft Network, 1995 to 1998), over TCP.
func (c *Config) marvelSection(cf *config.File) error {
	s := cf.Get("marvel", "")
	if s == nil {
		return nil
	}
	err := func() error {
		m := &marvel.Settings{Address: s.String("address", ":569")}
		if _, _, err := net.SplitHostPort(m.Address); err != nil {
			return fmt.Errorf("address: %v", err)
		}
		var err error
		if m.MaxClients, err = s.Int("max_clients", 64, 1, 100000); err != nil {
			return err
		}
		if m.MaxPerIP, err = s.Int("max_per_ip", 4, 0, 1000); err != nil {
			return err
		}
		c.Marvel = m
		return nil
	}()
	if err != nil {
		return fmt.Errorf("[marvel] %w", err)
	}
	return nil
}
