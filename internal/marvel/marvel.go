// Package marvel is the BBS as The Microsoft Network of 1995 to 1998 ("MSN
// Classic"), for the MSN client that came with Windows 95: its private
// protocol, codenamed Marvel, over a modem call or over TCP (port 569).
//
// The client's own windows draw everything; the host answers its services:
// LOGSRV signs a BBS account on, DIRSRV lists the BBS's message areas as
// MSN's Categories folders, and the "BBS" service shows an area's messages
// in MSN's bulletin-board reader and takes new posts and replies into it.
//
// The layers (each in its own file):
//
//	wire.go   the serial packet: byte stuffing, the CRC, header bytes;
//	          the TCP record
//	link.go   pipe frames, reassembly, Go-Back-N with retransmission
//	mpc.go    host blocks, tagged parameters, property records
//	conn.go   bring-up, pipe opens and closes, dispatch to services
//
// What the client expects comes from the reverse-engineered protocol notes
// of the marvel-msn project (github.com/gabspeck/marvel-msn: PROTOCOL.md
// and docs/), checked against the wire captures quoted there.  None of
// that project's code is used here.
package marvel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/sockets"
	"github.com/D-Modem/bbs/internal/tcpserv"
)

// Settings are the [marvel] section's.
type Settings struct {
	// Address is where to listen for the client over TCP: its port is
	// 569, which it can't be told otherwise ("" for none: modem callers
	// only, through a listener with frontend = marvel).
	Address    string
	MaxClients int
	MaxPerIP   int
}

// Server serves MSN clients.
type Server struct {
	B *bbs.BBS
	S Settings

	mu        sync.Mutex
	conns     map[*conn]bool
	roomIDs   map[string]uint32
	roomNames map[uint32]string
}

// New checks the settings and makes the server.
func New(b *bbs.BBS, s Settings) (*Server, error) {
	if s.Address != "" {
		if _, _, err := net.SplitHostPort(s.Address); err != nil {
			return nil, fmt.Errorf("marvel: address: %w", err)
		}
	}
	if s.MaxClients == 0 {
		s.MaxClients = 64
	}
	b.Store.SetKeepNTLM(true) // the 1996 client's NTLM sign-in checks against them
	return &Server{B: b, S: s, conns: map[*conn]bool{},
		roomIDs: map[string]uint32{}, roomNames: map[uint32]string{}}, nil
}

// Run listens on the settings' address until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	if s.S.Address == "" {
		return errors.New("marvel: no address to listen on")
	}
	ln, err := sockets.Listen("tcp", s.S.Address)
	if err != nil {
		return fmt.Errorf("marvel: %w", err)
	}
	return s.Serve(ctx, ln)
}

// Serve answers TCP clients on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	ts := &tcpserv.Server{Name: "marvel", MaxClients: s.S.MaxClients, MaxPerIP: s.S.MaxPerIP, Log: s.B.Log,
		Handle: s.handleTCP,
		Refuse: func(c net.Conn) bool { return s.B.Refuse("marvel", c.RemoteAddr()) }}
	return ts.Serve(ctx, ln)
}

// Clients is how many clients are connected.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

func (s *Server) handleTCP(c net.Conn) {
	host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	cn := s.newConn(host, "ip:"+host, &straightLink{c: c})
	cn.serve(nil)
}

// Line is a modem caller's connection: telnet (in binary mode) or raw.
type Line interface {
	io.Reader
	io.Writer
	SetReadDeadline(time.Time) error
}

// Call serves a modem caller on node n, whose line is l: the client sends
// a CR, is answered "COM", and the link starts.
func Call(b *bbs.BBS, n *bbs.Node, l Line) error {
	s, _ := New(b, Settings{})
	return s.Call(n, l)
}

// Call is Call on this server.
func (s *Server) Call(n *bbs.Node, l Line) error {
	rd := bufio.NewReader(l)
	l.SetReadDeadline(time.Now().Add(90 * time.Second))
	for {
		line, err := rd.ReadBytes(terminator)
		if err != nil {
			return errHandshake
		}
		if len(trimSpace(line[:len(line)-1])) == 0 {
			break
		}
	}
	l.SetReadDeadline(time.Time{})
	if _, err := io.WriteString(l, "COM\r"); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	cn := s.newConn(n.Caller.Remote, n.Caller.Source, nil)
	cn.l = newSelectLink(rwc{l}, rd, cn.logf)
	cn.serve(n)
	return nil
}

// trimSpace drops spaces, line feeds and NULs: what a line may carry
// around the CR that starts the link.
func trimSpace(b []byte) []byte {
	out := b[:0:0]
	for _, x := range b {
		if x != ' ' && x != '\n' && x != 0 && x != '\r' {
			out = append(out, x)
		}
	}
	return out
}

// rwc is a Line that can be closed (if it can).
type rwc struct{ Line }

func (r rwc) Close() error {
	if c, ok := r.Line.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (s *Server) newConn(host, source string, l link) *conn {
	return &conn{s: s, host: host, source: source, l: l, pipes: map[uint16]service{}}
}
