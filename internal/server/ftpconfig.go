package server

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/ftp"
)

// parseSize reads a size: a number of bytes, or with K, M or G.
func parseSize(s *config.Section, key string, def int64) (int64, error) {
	v := strings.ToUpper(strings.TrimSpace(s.String(key, "")))
	if v == "" {
		return def, nil
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(v, "K"):
		mult, v = 1<<10, strings.TrimSuffix(v, "K")
	case strings.HasSuffix(v, "M"):
		mult, v = 1<<20, strings.TrimSuffix(v, "M")
	case strings.HasSuffix(v, "G"):
		mult, v = 1<<30, strings.TrimSuffix(v, "G")
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 || n > (1<<42)/mult {
		return 0, fmt.Errorf("%s: %q is not a size (bytes, or with K, M, G)", key, s.String(key, ""))
	}
	return n * mult, nil
}

// files reads the file areas' options of [general].
func (c *Config) files(g *config.Section) error {
	c.FilesDir = g.String("files_dir", c.FilesDir)
	var err error
	if c.MaxFileSize, err = parseSize(g, "max_file_size", 64<<20); err != nil {
		return err
	}
	if c.MaxFileSize == 0 {
		return errors.New("max_file_size: 0 takes no files")
	}
	if c.UploadQuota, err = parseSize(g, "upload_quota", 256<<20); err != nil {
		return err
	}
	return nil
}

// ftpSection reads [ftp].
func (c *Config) ftpSection(f *config.Section) error {
	if f == nil {
		return nil
	}
	set := &ftp.Settings{Address: f.String("address", ":2121"), Banner: c.Settings.Name}
	if _, _, err := net.SplitHostPort(set.Address); err != nil {
		return fmt.Errorf("address: %v", err)
	}
	set.PublicHost = f.String("public_host", "")
	if set.PublicHost != "" && net.ParseIP(set.PublicHost) == nil {
		return errors.New("public_host: an IP address (the library takes no names)")
	}
	if r := f.String("passive_ports", ""); r != "" {
		lo, hi, ok := strings.Cut(r, "-")
		a, err1 := strconv.Atoi(strings.TrimSpace(lo))
		b, err2 := strconv.Atoi(strings.TrimSpace(hi))
		if !ok || err1 != nil || err2 != nil || a < 1024 || b > 65535 || a > b {
			return fmt.Errorf("passive_ports: %q is not a range like 50000-50100", r)
		}
		set.PassiveMin, set.PassiveMax = a, b
	}
	set.TLS = strings.ToLower(f.String("tls", ftp.TLSRequired))
	switch set.TLS {
	case ftp.TLSNo:
		c.Warnings = append(c.Warnings, "[ftp] tls = no: passwords go over the network in the clear")
	case ftp.TLSOptional:
		c.Warnings = append(c.Warnings, "[ftp] tls = optional: clients that do not ask for TLS send passwords in the clear")
	case ftp.TLSRequired, ftp.TLSImplicit:
	default:
		return fmt.Errorf("tls: %q is not required, implicit, optional or no", set.TLS)
	}
	set.CertFile = f.String("cert_file", "")
	set.KeyFile = f.String("key_file", "")
	if set.TLS != ftp.TLSNo && (set.CertFile == "" || set.KeyFile == "") {
		return errors.New("cert_file and key_file are needed for TLS (or tls = no)")
	}
	var err error
	if set.Anonymous, err = f.Bool("anonymous", false); err != nil {
		return err
	}
	if set.MaxClients, err = f.Int("max_clients", 32, 1, 10000); err != nil {
		return err
	}
	if set.MaxPerIP, err = f.Int("max_per_ip", 4, 0, 1000); err != nil {
		return err
	}
	if set.IdleTimeout, err = f.Seconds("idle_timeout", 300, 30, 86400); err != nil {
		return err
	}
	c.FTPAdvertise = f.String("advertise", set.Address)
	c.FTP = set
	return nil
}
