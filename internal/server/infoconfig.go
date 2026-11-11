package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/finger"
	"github.com/D-Modem/bbs/internal/gemini"
	"github.com/D-Modem/bbs/internal/gopher"
)

// infoSections reads [finger], [gopher] and [gemini].
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
	if s := cf.Get("gemini", ""); s != nil {
		g := &gemini.Settings{Address: s.String("address", ":1965")}
		if _, _, err := net.SplitHostPort(g.Address); err != nil {
			return fmt.Errorf("[gemini] address: %v", err)
		}
		host, _ := os.Hostname()
		g.Host = s.String("hostname", host)
		g.CertFile, g.KeyFile = s.String("cert_file", ""), s.String("key_file", "")
		switch {
		case certs.IsCA(g.CertFile):
			// signed by the BBS's CA for hostname when the BBS starts
		case certs.IsACME(g.CertFile):
			if err := certs.Check(g.CertFile, g.KeyFile, c.acmeNames()); err != nil {
				return fmt.Errorf("[gemini] %v", err)
			}
			c.Warnings = append(c.Warnings, "[gemini] cert_file = acme: Gemini clients pin the certificate they first see, and a renewed one has a new key")
		case g.CertFile == "" && g.KeyFile == "":
			g.CertFile = filepath.Join(c.DataDir(), "gemini-cert.pem")
			g.KeyFile = filepath.Join(c.DataDir(), "gemini-key.pem")
		case g.CertFile == "" || g.KeyFile == "":
			return errors.New("[gemini] cert_file and key_file go together (or neither, for one made by bbs gemini cert)")
		}
		g.Areas = s.String("areas", "")
		if g.Files, err = s.Bool("files", false); err != nil {
			return fmt.Errorf("[gemini] %w", err)
		}
		if g.Users, err = s.Bool("users", true); err != nil {
			return fmt.Errorf("[gemini] %w", err)
		}
		if g.MaxClients, err = s.Int("max_clients", 32, 1, 1000); err != nil {
			return fmt.Errorf("[gemini] %w", err)
		}
		if g.MaxPerIP, err = s.Int("max_per_ip", 8, 0, 1000); err != nil {
			return fmt.Errorf("[gemini] %w", err)
		}
		if g.Areas != "" || g.Files {
			c.Warnings = append(c.Warnings, "[gemini] shows message or file areas to anyone, without a login")
		}
		c.Gemini = g
	}
	return nil
}
