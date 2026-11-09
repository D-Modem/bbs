// Package aol is the BBS as an AOL host, for AOL's own clients (3.0, 4.0
// and on) over TCP: P3 (package p3) carries tokens, a two-letter name and
// what follows it, each way; the client's forms send their fields as FDO91
// atom streams (package fdo), and the host answers with streams of its
// own that draw windows and run actions.
//
// The client signs on with a BBS account (the Welcome form's screen name
// and password, token Dd), and signs off with pE.  The client's own
// database has most of its windows; the host draws its own Welcome.
//
// Sources for what the client expects: Dialtone (MIT) and other hosts
// serving live clients, and notes on the 3.0 client's code.
package aol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/D-Modem/bbs/internal/aol/p3"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/sockets"
	"github.com/D-Modem/bbs/internal/tcpserv"
)

// Settings are the [aol] section's.
type Settings struct {
	// Address is where to listen: the client's port is 5190, which the
	// OSCAR server (AIM, ICQ) shares when it listens there too.
	Address    string
	MaxClients int
	MaxPerIP   int
}

// Server serves AOL clients.
type Server struct {
	B *bbs.BBS
	S Settings

	// Trace, if set, is told of every packet (for the sysop's debugging).
	Trace func(remote string, in bool, p p3.Packet)

	mu       sync.Mutex
	sessions map[*session]bool
}

// New checks the settings and makes the server.
func New(b *bbs.BBS, s Settings) (*Server, error) {
	if s.Address != "" {
		if _, _, err := net.SplitHostPort(s.Address); err != nil {
			return nil, fmt.Errorf("aol: address: %w", err)
		}
	}
	if s.MaxClients == 0 {
		s.MaxClients = 256
	}
	return &Server{B: b, S: s, sessions: map[*session]bool{}}, nil
}

// Run listens on the settings' address until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	if s.S.Address == "" {
		return errors.New("aol: no address to listen on")
	}
	ln, err := sockets.Listen("tcp", s.S.Address)
	if err != nil {
		return fmt.Errorf("aol: %w", err)
	}
	return s.Serve(ctx, ln)
}

// Serve answers on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	ts := &tcpserv.Server{Name: "aol", MaxClients: s.S.MaxClients, MaxPerIP: s.S.MaxPerIP, Log: s.B.Log,
		Handle: s.Handle,
		Refuse: func(c net.Conn) bool { return s.B.Refuse("aol", c.RemoteAddr()) }}
	return ts.Serve(ctx, ln)
}

// Clients is how many clients are connected.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// Handle serves one client's connection, from its first P3 packet (the
// OSCAR server hands over those that begin as P3).
func (s *Server) Handle(c net.Conn) {
	host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	opt := p3.Options{Logf: func(f string, a ...any) { s.B.Log.Printf("aol: %s: "+f, append([]any{host}, a...)...) }}
	if s.Trace != nil {
		opt.Trace = func(in bool, p p3.Packet) { s.Trace(host, in, p) }
	}
	l, err := p3.Serve(c, opt)
	if err != nil {
		s.B.Log.Printf("aol: %s: %v", host, err)
		return
	}
	ss := &session{s: s, l: l, host: host, init: l.Init(), parts: map[partKey][]byte{}}
	s.mu.Lock()
	s.sessions[ss] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.sessions, ss)
		s.mu.Unlock()
	}()
	s.B.Log.Printf("aol: %s: %s", host, ss.init)
	ss.serve()
}
