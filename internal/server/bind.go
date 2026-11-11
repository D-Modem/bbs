package server

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/D-Modem/bbs/internal/sockets"
	"github.com/D-Modem/bbs/internal/timesync"
	"github.com/D-Modem/bbs/internal/vhost"
)

// Bind binds every socket the configuration listens on (as root, before
// the BBS becomes [general]'s user): the listeners, TCP and unix, and the
// servers' ports, TCP and UDP (NTP, PTP).  They are handed down, each
// server taking its own.
func Bind(cfg *Config) ([]sockets.Socket, error) {
	var out []sockets.Socket
	fail := func(err error) ([]sockets.Socket, error) {
		for _, s := range out {
			s.File.Close()
		}
		return nil, err
	}
	tcp := func(addr string) error {
		if addr == "" {
			return nil
		}
		for _, s := range out {
			// ":443" and "0.0.0.0:443" are one port (the chat's front
			// ends share theirs: the first's address is the one asked for)
			if s.Network == "tcp" && vhost.Normal(s.Address) == vhost.Normal(addr) {
				return nil
			}
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("%s: %w", addr, err)
		}
		defer ln.Close()
		f, err := ln.(*net.TCPListener).File()
		if err != nil {
			return err
		}
		out = append(out, sockets.Socket{Network: "tcp", Address: addr, File: f})
		return nil
	}
	for _, l := range cfg.Listeners {
		if !l.Unix() {
			if err := tcp(l.Address); err != nil {
				return fail(fmt.Errorf("[listen %s] %w", l.Name, err))
			}
			continue
		}
		u, err := ListenUnix(l)
		if err != nil {
			return fail(fmt.Errorf("[listen %s] %w", l.Name, err))
		}
		if a := cfg.RunAs; a != nil {
			// the user's, as it would be had the BBS made it (its group
			// socket_group's, if said)
			gid := a.GID
			if l.SocketGroup != "" {
				gid = -1
			}
			if err := os.Lchown(l.path(), a.UID, gid); err != nil {
				u.Close()
				return fail(err)
			}
		}
		f, err := u.UnixListener.File()
		u.UnixListener.Close() // not u.Close: the socket stays where it is
		if err != nil {
			return fail(err)
		}
		out = append(out, sockets.Socket{Network: "unix", Address: l.path(), File: f})
	}
	var addrs []string
	if h := cfg.HTTP; h != nil {
		addrs = append(addrs, h.Address, h.Metrics)
	}
	if cfg.FTP != nil {
		addrs = append(addrs, cfg.FTP.Address)
	}
	if cfg.NNTP != nil {
		addrs = append(addrs, cfg.NNTP.Address)
	}
	for _, ms := range cfg.MailServers {
		if !strings.HasPrefix(ms.Set.Address, "/") {
			addrs = append(addrs, ms.Set.Address)
		}
	}
	if i := cfg.IRC; i != nil {
		addrs = append(addrs, i.Address, i.TLSAddress)
	}
	if cfg.Finger != nil {
		addrs = append(addrs, cfg.Finger.Address)
	}
	if cfg.Gopher != nil {
		addrs = append(addrs, cfg.Gopher.Address)
	}
	if cfg.Gemini != nil {
		addrs = append(addrs, cfg.Gemini.Address)
	}
	if cfg.Binkp != nil {
		addrs = append(addrs, cfg.Binkp.Address)
	}
	if t := cfg.Time; t != nil && t.NTS != nil {
		addrs = append(addrs, t.NTS.Address)
	}
	if a := cfg.ACME; a != nil && a.HTTPAddress != "" {
		addrs = append(addrs, a.HTTPAddress)
	}
	// the chat's front ends, at the old services' ports (443, 80, 569 ...),
	// their shared web ports first, in SharedPorts' order
	if m := cfg.MSN; m != nil {
		addrs = append(addrs, m.HTTPSAddress, m.HTTPAddress)
	}
	if y := cfg.YMSG; y != nil {
		addrs = append(addrs, y.HTTPSAddress, y.HTTPAddress)
	}
	if o := cfg.OSCAR; o != nil {
		addrs = append(addrs, o.TLSAddress, o.Address)
	}
	if a := cfg.AOL; a != nil && !(cfg.OSCAR != nil && samePort(a.Address, cfg.OSCAR.Address)) {
		addrs = append(addrs, a.Address) // else OSCAR's port takes AOL's clients
	}
	if m := cfg.MSN; m != nil {
		addrs = append(addrs, m.Address)
	}
	if m := cfg.Marvel; m != nil {
		addrs = append(addrs, m.Address)
	}
	if y := cfg.YMSG; y != nil {
		addrs = append(addrs, y.Address)
	}
	if d := cfg.DNS; d != nil {
		addrs = append(addrs, d.Address)
	}
	for _, a := range addrs {
		if err := tcp(a); err != nil {
			return fail(err)
		}
	}
	udp := func(addr string, prep func(*net.UDPConn) error) error {
		a, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return fmt.Errorf("%s: %w", addr, err)
		}
		c, err := net.ListenUDP("udp", a)
		if err != nil {
			return fmt.Errorf("%s: %w", addr, err)
		}
		defer c.Close()
		if prep != nil {
			if err := prep(c); err != nil {
				return fmt.Errorf("%s: %w", addr, err)
			}
		}
		f, err := c.File()
		if err != nil {
			return err
		}
		out = append(out, sockets.Socket{Network: "udp", Address: addr, File: f})
		return nil
	}
	if d := cfg.DNS; d != nil {
		if err := udp(d.Address, nil); err != nil {
			return fail(fmt.Errorf("[dns] %w", err))
		}
	}
	if t := cfg.Time; t != nil {
		if t.NTP != nil {
			if err := udp(t.NTP.Address, nil); err != nil {
				return fail(fmt.Errorf("[ntp] %w", err))
			}
		}
		if p := t.PTP; p != nil {
			ports := p.Ports()
			// hardware timestamps are turned on now, as root (the network
			// card's are CAP_NET_ADMIN's to change)
			var prep func(*net.UDPConn) error
			if p.Hardware {
				prep = func(c *net.UDPConn) error { return timesync.EnableTimestamps(c, true, p.Interface) }
			}
			if err := udp(ports[0], prep); err != nil {
				return fail(fmt.Errorf("[ptp] %w", err))
			}
			if err := udp(ports[1], nil); err != nil {
				return fail(fmt.Errorf("[ptp] %w", err))
			}
		}
	}
	return out, nil
}

// samePort: two addresses have one port.
func samePort(a, b string) bool {
	_, ap, err1 := net.SplitHostPort(a)
	_, bp, err2 := net.SplitHostPort(b)
	return err1 == nil && err2 == nil && ap == bp
}
