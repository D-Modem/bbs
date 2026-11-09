package server

import (
	"fmt"
	"net"

	"github.com/D-Modem/bbs/internal/aol"
	"github.com/D-Modem/bbs/internal/config"
)

// aolSection reads [aol]: the host for AOL's own clients.
func (c *Config) aolSection(cf *config.File) error {
	s := cf.Get("aol", "")
	if s == nil {
		return nil
	}
	err := func() error {
		a := &aol.Settings{Address: s.String("address", ":5190")}
		if _, _, err := net.SplitHostPort(a.Address); err != nil {
			return fmt.Errorf("address: %v", err)
		}
		var err error
		if a.MaxClients, err = s.Int("max_clients", 256, 1, 100000); err != nil {
			return err
		}
		if a.MaxPerIP, err = s.Int("max_per_ip", 4, 0, 1000); err != nil {
			return err
		}
		c.AOL = a
		return nil
	}()
	if err != nil {
		return fmt.Errorf("[aol] %w", err)
	}
	return nil
}
