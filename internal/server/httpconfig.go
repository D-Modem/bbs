package server

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/api"
	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/forum"
)

// httpSection reads [http].
func (c *Config) httpSection(s *config.Section) error {
	if s == nil {
		return nil
	}
	h := &api.Settings{Address: s.String("address", ":8443")}
	host, _, err := net.SplitHostPort(h.Address)
	if err != nil {
		return fmt.Errorf("address: %v", err)
	}
	if h.TLS, err = s.Bool("tls", true); err != nil {
		return err
	}
	h.CertFile, h.KeyFile = s.String("cert_file", ""), s.String("key_file", "")
	if h.TLS && (h.CertFile == "" || h.KeyFile == "") {
		return errors.New("cert_file and key_file are needed for TLS (or tls = no, behind a proxy that has it)")
	}
	if h.TrustedProxies, err = api.ParseProxies(s.String("trusted_proxies", "")); err != nil {
		return fmt.Errorf("trusted_proxies: %v", err)
	}
	if h.SecureCookies, err = s.Bool("secure_cookies", h.TLS); err != nil {
		return err
	}
	if !h.TLS && len(h.TrustedProxies) > 0 && !h.SecureCookies {
		c.Warnings = append(c.Warnings, "[http] behind a proxy without secure_cookies = yes: logins' cookies may cross in the clear if the proxy also serves http://")
	}
	if !h.TLS {
		if ip := net.ParseIP(host); host == "" || ip == nil || !ip.IsLoopback() {
			c.Warnings = append(c.Warnings, "[http] tls = no on an address others reach: tokens and passwords go in the clear")
		}
	}
	if h.Anonymous, err = s.Bool("anonymous", true); err != nil {
		return err
	}
	if h.CORS = s.String("cors", ""); h.CORS != "" {
		u, err := url.Parse(h.CORS)
		if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" || strings.Contains(h.CORS, "*") {
			return errors.New("cors: one origin, like https://www.example.org (no *)")
		}
	}
	if h.Metrics = s.String("metrics", ""); h.Metrics != "" {
		if _, _, err := net.SplitHostPort(h.Metrics); err != nil {
			return fmt.Errorf("metrics: %v", err)
		}
	}
	if h.MaxPerIP, err = s.Int("max_per_ip", 20, 0, 10000); err != nil {
		return err
	}
	rate := s.String("rate", "10")
	if h.Rate, err = strconv.ParseFloat(rate, 64); err != nil || h.Rate < 0 || h.Rate > 10000 {
		return fmt.Errorf("rate: %q is not requests a second (0: no limit)", rate)
	}
	if h.Burst, err = s.Int("burst", 20, 1, 100000); err != nil {
		return err
	}
	hours, err := s.Int("session_hours", 24, 1, 24*90)
	if err != nil {
		return err
	}
	h.SessionTTL = time.Duration(hours) * time.Hour
	if h.Terminal, err = s.Bool("terminal", true); err != nil {
		return err
	}
	if h.Forum, err = s.Bool("forum", true); err != nil {
		return err
	}
	if h.Guests, err = s.Bool("forum_guests", true); err != nil {
		return err
	}
	if h.Theme = s.String("forum_theme", forum.Themes[0].Name); !forum.KnownTheme(h.Theme) {
		var names []string
		for _, t := range forum.Themes {
			names = append(names, t.Name)
		}
		return fmt.Errorf("forum_theme: %q is not one of %s", h.Theme, strings.Join(names, ", "))
	}
	if h.Chan, err = s.Bool("chan", true); err != nil {
		return err
	}
	h.ChanSettings = c.Chan
	c.HTTP = h
	return nil
}
