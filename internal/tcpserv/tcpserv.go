// Package tcpserv serves small one-question protocols (finger, gopher):
// connections limited at once and from one address, each with a time
// limit, a panic in one not taking the rest down.
package tcpserv

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

// Server answers connections with Handle.
type Server struct {
	Name       string // for the log
	MaxClients int    // at once (0: no limit)
	MaxPerIP   int    // from one address (0: no limit)
	Timeout    time.Duration
	Log        *log.Logger
	Handle     func(c net.Conn)
	Busy       func(c net.Conn) // told when a limit turns one away (may be nil)
	// Refuse, if set, says to close a connection at once (the sysop's
	// deny list, bans)
	Refuse func(c net.Conn) bool

	mu      sync.Mutex
	clients int
	perIP   map[string]int
	conns   map[net.Conn]bool
}

// Serve answers on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.perIP = map[string]int{}
	s.conns = map[net.Conn]bool{}
	s.Log.Printf("%s: listening on %s", s.Name, ln.Addr())
	go func() {
		<-ctx.Done()
		ln.Close()
		s.mu.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.mu.Unlock()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go s.serve(c)
	}
}

// host is the connection's remote address without its port.
func host(c net.Conn) string {
	h, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	return h
}

// serve answers c with Handle, under the limits and time limit, a panic
// logged; one refused or over a limit is closed (told Busy, if over).
func (s *Server) serve(c net.Conn) {
	defer c.Close()
	if s.Refuse != nil && s.Refuse(c) {
		return
	}
	h := host(c)
	s.mu.Lock()
	if s.MaxClients > 0 && s.clients >= s.MaxClients || s.MaxPerIP > 0 && s.perIP[h] >= s.MaxPerIP {
		s.mu.Unlock()
		if s.Busy != nil {
			c.SetDeadline(time.Now().Add(5 * time.Second))
			s.Busy(c)
		}
		return
	}
	s.clients++
	s.perIP[h]++
	s.conns[c] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.clients--
		if s.perIP[h]--; s.perIP[h] <= 0 {
			delete(s.perIP, h)
		}
		delete(s.conns, c)
		s.mu.Unlock()
	}()
	defer func() {
		if r := recover(); r != nil {
			s.Log.Printf("%s %s: panic: %v", s.Name, c.RemoteAddr(), r)
		}
	}()
	if s.Timeout > 0 {
		c.SetDeadline(time.Now().Add(s.Timeout))
	}
	s.Handle(c)
}

// ReadLine reads the request line: up to max bytes, to LF (CR dropped).
func ReadLine(c net.Conn, max int) (string, error) {
	var b []byte
	buf := make([]byte, 1)
	for len(b) <= max {
		n, err := c.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				if len(b) > 0 && b[len(b)-1] == '\r' {
					b = b[:len(b)-1]
				}
				return string(b), nil
			}
			b = append(b, buf[0])
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("request too long")
}
