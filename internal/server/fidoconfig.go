package server

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/archive"
	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/fido"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
)

// BinkpSettings are the [binkp] section's: the mailer that answers.
type BinkpSettings struct {
	Address    string
	MaxClients int
	MaxPerIP   int
}

// fidoSections reads [fido], [binkp] and the [link NAME]s.
func (c *Config) fidoSections(cf *config.File) error {
	s := cf.Get("fido", "")
	if s == nil {
		if cf.Get("binkp", "") != nil || len(cf.All("link")) > 0 {
			return errors.New("[binkp] and [link] need a [fido] section")
		}
		return nil
	}
	f := &fido.Settings{System: c.Settings.Name, Sysop: c.Settings.Sysop}
	for _, a := range strings.Fields(strings.ReplaceAll(s.String("address", ""), ",", " ")) {
		ad, err := ftn.ParseAddr(a)
		if err != nil {
			return fmt.Errorf("[fido] address %s: %w", a, err)
		}
		f.Addrs = append(f.Addrs, ad)
	}
	if len(f.Addrs) == 0 {
		return errors.New("[fido] address: ours, like 21:1/999 (more after it are AKAs)")
	}
	f.Sysop = s.String("sysop", f.Sysop)
	f.Location = s.String("location", c.Settings.Location)
	f.Origin = s.String("origin", c.Settings.Name)
	f.Dir = s.String("dir", "fido")
	switch cs := strings.ToLower(s.String("charset", "cp437")); cs {
	case "cp437":
	case "utf-8", "utf8":
		f.UTF8 = true
	default:
		return fmt.Errorf("[fido] charset: %q is not cp437 or utf-8", cs)
	}
	max, err := parseSize(s, "max_message", 64<<10)
	if err != nil {
		return fmt.Errorf("[fido] %w", err)
	}
	f.MaxMessage = int(max)
	f.Nodelists = strings.Fields(s.String("nodelist", ""))
	for _, p := range f.Nodelists {
		if _, err := filepath.Match(p, ""); err != nil {
			return fmt.Errorf("[fido] nodelist %s: %v", p, err)
		}
	}
	for _, field := range []string{f.System, f.Sysop, f.Location} {
		if strings.ContainsAny(field, "\r\n\x00") {
			return errors.New("[fido] names must be one line")
		}
	}

	names := map[string]bool{}
	routes := 0
	for _, ls := range cf.All("link") {
		l, err := link(ls)
		if err != nil {
			return fmt.Errorf("[link %s] %w", ls.Name, err)
		}
		if names[strings.ToLower(l.Name)] {
			return fmt.Errorf("[link %s] given twice", l.Name)
		}
		names[strings.ToLower(l.Name)] = true
		for _, o := range f.Links {
			if o.Addr.Same(l.Addr) {
				return fmt.Errorf("[link %s] has the address of [link %s]", l.Name, o.Name)
			}
		}
		for _, o := range f.Addrs {
			if o.Same(l.Addr) {
				return fmt.Errorf("[link %s] has our own address", l.Name)
			}
		}
		if l.Route {
			routes++
		}
		if l.Password == "" {
			c.Warnings = append(c.Warnings, fmt.Sprintf("[link %s] has no password: anyone may say they are %s", l.Name, l.Addr))
		} else if l.PlainOK {
			c.Warnings = append(c.Warnings, fmt.Sprintf("[link %s] plain_password = yes: its password may cross in the clear", l.Name))
		}
		f.Links = append(f.Links, l)
	}
	if routes > 1 {
		return errors.New("[link] route = yes on more than one link")
	}
	c.Fido = f

	if b := cf.Get("binkp", ""); b != nil {
		bs := &BinkpSettings{Address: b.String("address", ":24554")}
		if _, _, err := net.SplitHostPort(bs.Address); err != nil {
			return fmt.Errorf("[binkp] address: %v", err)
		}
		if bs.MaxClients, err = b.Int("max_clients", 16, 1, 1000); err != nil {
			return fmt.Errorf("[binkp] %w", err)
		}
		if bs.MaxPerIP, err = b.Int("max_per_ip", 4, 0, 1000); err != nil {
			return fmt.Errorf("[binkp] %w", err)
		}
		c.Binkp = bs
	}
	return nil
}

// link reads a [link NAME], a FidoNet system mail is exchanged with: its
// address, host (port 24554 if none is given), passwords, echoes, AreaFix,
// packer and polling.
func link(s *config.Section) (*fido.Link, error) {
	if s.Name == "" || strings.ContainsAny(s.Name, "/\\. ") {
		return nil, errors.New("a link needs a name of letters and digits: [link NAME]")
	}
	l := &fido.Link{Name: s.Name}
	var err error
	if l.Addr, err = ftn.ParseAddr(s.String("address", "")); err != nil {
		return nil, fmt.Errorf("address: %w", err)
	}
	if aka := s.String("aka", ""); aka != "" {
		if l.AKA, err = ftn.ParseAddr(aka); err != nil {
			return nil, fmt.Errorf("aka: %w", err)
		}
	}
	if l.Host = s.String("host", ""); l.Host != "" && l.Host != fido.NodelistHost {
		if _, _, err := net.SplitHostPort(l.Host); err != nil {
			l.Host = net.JoinHostPort(l.Host, "24554")
		}
	}
	l.Password = s.String("password", "")
	if l.PlainOK, err = s.Bool("plain_password", false); err != nil {
		return nil, err
	}
	l.PktPassword = s.String("packet_password", "")
	if len(l.PktPassword) > 8 {
		return nil, errors.New("packet_password: 8 characters at most")
	}
	l.Echoes = s.String("echoes", "*")
	l.FileEchoes = s.String("fileechoes", "")
	if l.AreaFix, err = s.Bool("areafix", false); err != nil {
		return nil, err
	}
	l.AreafixPassword = s.String("areafix_password", "")
	if l.AreaFix && l.AreafixPassword == "" {
		return nil, errors.New("areafix = yes needs an areafix_password")
	}
	if strings.ContainsAny(l.AreafixPassword, " \t") {
		return nil, errors.New("areafix_password: one word")
	}
	l.TicPassword = s.String("tic_password", "")
	if l.Create, err = s.Bool("create", false); err != nil {
		return nil, err
	}
	if l.Route, err = s.Bool("route", false); err != nil {
		return nil, err
	}
	switch p := strings.ToLower(s.String("packer", "zip")); p {
	case "zip", "arc", "arj", "lha":
		l.Packer = archive.Kind(p)
	case "lzh":
		l.Packer = archive.LHA
	case "none":
	default:
		return nil, fmt.Errorf("packer: %q is not zip, arc, arj, lha or none", p)
	}
	if l.Poll, err = s.Seconds("poll", 0, 0, 7*86400); err != nil {
		return nil, err
	}
	return l, nil
}
