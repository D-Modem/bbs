package server

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/finger"
	"github.com/Neo-Desktop/bbs-sec/internal/gopher"
)

// infoSections reads [finger] and [gopher].
func (c *Config) infoSections(cf *config.File) error {
	var err error
	if s := cf.Get("finger", ""); s != nil {
		f := &finger.Settings{Address: s.String("address", ":79")}
		if _, _, err := net.SplitHostPort(f.Address); err != nil {
			return fmt.Errorf("[finger] address: %v", err)
		}
		if f.Online, err = s.Bool("online", true); err != nil {
			return fmt.Errorf("[finger] %w", err)
		}
		if f.Users, err = s.Bool("users", true); err != nil {
			return fmt.Errorf("[finger] %w", err)
		}
		if f.MaxClients, err = s.Int("max_clients", 16, 1, 1000); err != nil {
			return fmt.Errorf("[finger] %w", err)
		}
		if f.MaxPerIP, err = s.Int("max_per_ip", 4, 0, 1000); err != nil {
			return fmt.Errorf("[finger] %w", err)
		}
		c.Finger = f
	}
	if s := cf.Get("gopher", ""); s != nil {
		g := &gopher.Settings{Address: s.String("address", ":70")}
		_, port, err := net.SplitHostPort(g.Address)
		if err != nil {
			return fmt.Errorf("[gopher] address: %v", err)
		}
		host, _ := os.Hostname()
		g.Host = s.String("hostname", host)
		p, _ := strconv.Atoi(port)
		if g.Port, err = s.Int("port", p, 1, 65535); err != nil {
			return fmt.Errorf("[gopher] %w", err)
		}
		g.Areas = s.String("areas", "")
		if g.Files, err = s.Bool("files", false); err != nil {
			return fmt.Errorf("[gopher] %w", err)
		}
		if g.MaxClients, err = s.Int("max_clients", 32, 1, 1000); err != nil {
			return fmt.Errorf("[gopher] %w", err)
		}
		if g.MaxPerIP, err = s.Int("max_per_ip", 8, 0, 1000); err != nil {
			return fmt.Errorf("[gopher] %w", err)
		}
		if g.Areas != "" || g.Files {
			c.Warnings = append(c.Warnings, "[gopher] shows message or file areas to anyone, without a login")
		}
		c.Gopher = g
	}
	return nil
}
