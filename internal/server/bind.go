package server

import (
	"fmt"
	"net"
	"os"

	"github.com/Neo-Desktop/bbs-sec/internal/sockets"
)

// Bind binds every socket the configuration listens on (as root, before
// the BBS becomes [general]'s user): the listeners, TCP and unix, and the
// servers' ports.  They are handed down, each server taking its own.
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
			if s.Network == "tcp" && s.Address == addr {
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
	if i := cfg.IRC; i != nil {
		addrs = append(addrs, i.Address, i.TLSAddress)
	}
	if cfg.Finger != nil {
		addrs = append(addrs, cfg.Finger.Address)
	}
	if cfg.Gopher != nil {
		addrs = append(addrs, cfg.Gopher.Address)
	}
	if cfg.Binkp != nil {
		addrs = append(addrs, cfg.Binkp.Address)
	}
	for _, a := range addrs {
		if err := tcp(a); err != nil {
			return fail(err)
		}
	}
	return out, nil
}
