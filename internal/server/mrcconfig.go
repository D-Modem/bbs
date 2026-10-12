package server

import (
	"errors"
	"fmt"
	"net"
	"regexp"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/mrc"
)

var roomPrefix = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,9}$`)

// mrcSection reads [mrc]: Multi Relay Chat.
func (c *Config) mrcSection(cf *config.File) error {
	s := cf.Get("mrc", "")
	if s == nil {
		return nil
	}
	err := func() error {
		m := &mrc.Settings{Address: s.String("address", mrc.DefaultAddress), Prefix: s.String("prefix", "mrc-"),
			SystemName: s.String("system_name", ""), Web: s.String("web", ""), Telnet: s.String("telnet", ""),
			SSH: s.String("ssh", ""), Sysop: s.String("sysop", c.Settings.Sysop),
			Description: s.String("description", c.Settings.Name), Require: s.String("require", "")}
		var err error
		if m.TLS, err = s.Bool("tls", false); err != nil {
			return err
		}
		if _, _, err := net.SplitHostPort(m.Address); err != nil {
			return fmt.Errorf("address: host:port (%v)", err)
		}
		if !roomPrefix.MatchString(m.Prefix) {
			return errors.New("prefix: what begins MRC's rooms here: up to 10 lower case letters, digits and -")
		}
		if _, err := bbs.ParseRequire(m.Require); err != nil {
			return fmt.Errorf("require: %v", err)
		}
		c.MRC = m
		return nil
	}()
	if err != nil {
		return fmt.Errorf("[mrc] %w", err)
	}
	return nil
}
