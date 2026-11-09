package server

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/ymsg"
)

// ymsgSection reads [ymsg]: the Yahoo! Messenger server for the chat.
func (c *Config) ymsgSection(cf *config.File) error {
	s := cf.Get("ymsg", "")
	if s == nil {
		return nil
	}
	err := func() error {
		y := &ymsg.Settings{Address: s.String("address", ":5050"), HTTPAddress: s.String("http_address", ""),
			HTTPSAddress: s.String("https_address", ""), CertFile: s.String("tls_cert", ""),
			KeyFile: s.String("tls_key", ""), Host: s.String("host", ""), System: s.String("system", "bbs")}
		for _, a := range []struct{ key, v string }{{"address", y.Address}, {"http_address", y.HTTPAddress},
			{"https_address", y.HTTPSAddress}} {
			if a.v == "" {
				continue
			}
			if _, _, err := net.SplitHostPort(a.v); err != nil {
				return fmt.Errorf("%s: %v", a.key, err)
			}
		}
		if y.Address == "" {
			return errors.New("address: where the pager listens (:5050)")
		}
		if y.HTTPSAddress != "" {
			if y.CertFile == "" || y.KeyFile == "" && !certs.IsACME(y.CertFile) {
				return errors.New("https_address needs tls_cert and tls_key (one for login.yahoo.com: not a name a CA will sign for this host)")
			}
			if err := certs.Check(y.CertFile, y.KeyFile, c.acmeNames()); err != nil {
				return fmt.Errorf("tls_cert, tls_key: %v", err)
			}
		}
		if y.Host != "" && strings.ContainsAny(y.Host, ":/ ") && net.ParseIP(y.Host) == nil {
			return errors.New("host: the pager's IP address or host name, without a port")
		}
		if y.System == "" || len(y.System) > 32 || strings.ContainsAny(y.System, "@ ") {
			return errors.New("system: a Yahoo ID of 1 to 32 letters and digits")
		}
		var err error
		if y.MaxClients, err = s.Int("max_clients", 256, 1, 100000); err != nil {
			return err
		}
		if y.MaxPerIP, err = s.Int("max_per_ip", 4, 0, 1000); err != nil {
			return err
		}
		c.YMSG = y
		return nil
	}()
	if err != nil {
		return fmt.Errorf("[ymsg] %w", err)
	}
	return nil
}
