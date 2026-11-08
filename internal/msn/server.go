// Package msn is an MSN Messenger server (MSNP) for the BBS's chat, beside
// its IRC and AIM servers: MSN Messenger clients sign in with BBS names
// and passwords; their contact list is the user's friends list; an IM is
// a private message (a PRIVMSG to an IRC user, a line on a caller's
// terminal, an IM on AIM), kept for one away.
//
// It speaks MSNP2 to MSNP7: MSN Messenger 1.0 to 4.7 and Windows
// Messenger 4.x, which sign in by MD5 (or, MSN 1.0, by the password in
// the clear) over plain TCP.  Later versions sign in by Passport over
// HTTPS, not served here yet.
//
// One port (1863) serves the whole of it: the dispatch and notification
// server (a client's first command is VER) and the switchboards, the
// conversations (a client's first command is USR with a cookie, or ANS).
package msn

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/sockets"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/tcpserv"
)

// timeouts
const (
	loginTimeout = time.Minute      // to sign in, or to join a switchboard
	idleTimeout  = 10 * time.Minute // without a command (clients PNG every 50 s or so)
	sbIdle       = 30 * time.Minute // a switchboard connection without a command
	writeTimeout = 30 * time.Second
	cookieLife   = 2 * time.Minute // a switchboard cookie is used within this
	queueLen     = 512             // things waiting for a session's goroutine
)

// the protocol versions served
const (
	minVersion = 2
	maxVersion = 7
)

// Settings are the [msn] section's.
type Settings struct {
	Address string // where to listen
	// Host is where clients are sent for switchboards, host:port; "": the
	// address each one came to
	Host string
	// Domain makes users' handles: name@Domain.  Clients may sign in with
	// any domain (MSN 1.0 only takes @hotmail.com).
	Domain     string
	MaxClients int
	MaxPerIP   int
	// System is the name the BBS's notices come from
	System string
	// Site is a web address for the client's links (its inbox, its
	// profile pages); "": none
	Site string
}

// Server serves MSNP.
type Server struct {
	B *bbs.BBS
	S Settings

	mu      sync.Mutex
	ns      map[*nsSession]bool
	byNode  map[int]*nsSession
	sbs     map[string]*switchboard // by id
	cookies map[string]*sbCookie
	nextSB  int
}

// New checks the settings and makes the server.
func New(b *bbs.BBS, s Settings) (*Server, error) {
	if s.Address == "" {
		return nil, errors.New("msn: no address to listen on")
	}
	if s.Host != "" {
		if _, _, err := net.SplitHostPort(s.Host); err != nil {
			return nil, fmt.Errorf("msn: host: %w", err)
		}
	}
	if s.Domain == "" {
		s.Domain = "bbs.invalid"
	}
	if s.System == "" {
		s.System = "BBS"
	}
	if s.MaxClients == 0 {
		s.MaxClients = 256
	}
	return &Server{B: b, S: s, ns: map[*nsSession]bool{}, byNode: map[int]*nsSession{},
		sbs: map[string]*switchboard{}, cookies: map[string]*sbCookie{}, nextSB: 1}, nil
}

// Run listens until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	ln, err := sockets.Listen("tcp", s.S.Address)
	if err != nil {
		return fmt.Errorf("msn: %w", err)
	}
	return s.Serve(ctx, ln)
}

// Serve answers on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.B.Store.SetKeepMSN(true)
	stop := s.B.WatchPresence(s.presence)
	defer stop()
	stopFriends := s.B.WatchFriends(s.friendsChanged)
	defer stopFriends()
	ts := &tcpserv.Server{Name: "msn", MaxClients: s.S.MaxClients, MaxPerIP: s.S.MaxPerIP, Log: s.B.Log,
		Handle: s.handle,
		Refuse: func(c net.Conn) bool { return s.B.Refuse("msn", c.RemoteAddr()) }}
	return ts.Serve(ctx, ln)
}

// Clients is how many are signed in.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ns)
}

// handle serves a connection: a notification session, or a switchboard's.
func (s *Server) handle(c net.Conn) {
	host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	k := newConn(c, bufio.NewReaderSize(c, 8192))
	c.SetDeadline(time.Now().Add(loginTimeout))
	first, err := k.read()
	if err != nil {
		return
	}
	switch first.name {
	case "VER":
		s.notification(k, host, first)
	case "USR", "ANS":
		s.switchboardConn(k, host, first)
	}
}

// handleOf is a user's MSN handle: their name without its spaces, at the
// domain.
func (s *Server) handleOf(u *store.User) string {
	return strings.ReplaceAll(u.Name, " ", "") + "@" + s.S.Domain
}

// systemHandle is the handle the BBS's notices come from.
func (s *Server) systemHandle() string {
	return strings.ReplaceAll(s.S.System, " ", "") + "@" + s.S.Domain
}

// resolve is the account a handle names: its part before the @, as a
// name with or without its spaces, at any domain (nil: none).
func (s *Server) resolve(handle string) *store.User {
	local, _, _ := strings.Cut(decode(handle), "@")
	local = strings.TrimSpace(local)
	if local == "" {
		return nil
	}
	u, err := s.B.Store.UserByScreenName(local)
	if err != nil {
		return nil
	}
	return u
}

// may: the user may use the chat.
func (s *Server) may(u *store.User) error {
	if !s.B.LevelOf(u).Chat {
		return bbs.ErrNotAllowed
	}
	return nil
}

// register keeps a notification session on.
func (s *Server) register(ns *nsSession) {
	s.mu.Lock()
	s.ns[ns] = true
	s.byNode[ns.n.Num] = ns
	s.mu.Unlock()
}

// unregister forgets it.
func (s *Server) unregister(ns *nsSession) {
	s.mu.Lock()
	delete(s.ns, ns)
	if s.byNode[ns.n.Num] == ns {
		delete(s.byNode, ns.n.Num)
	}
	s.mu.Unlock()
}

// all are the notification sessions signed in.
func (s *Server) all() []*nsSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*nsSession, 0, len(s.ns))
	for ns := range s.ns {
		out = append(out, ns)
	}
	return out
}

// sessionsOf are a user's notification sessions.
func (s *Server) sessionsOf(id int64) []*nsSession {
	var out []*nsSession
	for _, ns := range s.all() {
		if ns.u.ID == id {
			out = append(out, ns)
		}
	}
	return out
}

// onNode is the session on a node (nil: the node is another kind).
func (s *Server) onNode(num int) *nsSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byNode[num]
}

// nodesOf are the nodes a user is on.
func (s *Server) nodesOf(name string) []bbs.NodeInfo {
	var out []bbs.NodeInfo
	for _, n := range s.B.Nodes() {
		if n.User != "" && strings.EqualFold(n.User, name) {
			out = append(out, n)
		}
	}
	return out
}

// presence is told of a BBS user coming online or going.
func (s *Server) presence(name string, online bool) {
	if len(s.all()) == 0 {
		return
	}
	go func() {
		u, err := s.B.Store.UserByName(name)
		if err != nil {
			return
		}
		s.changed(u)
	}()
}

// changed has every session told the user's state anew (where it
// differs from what it was told).
func (s *Server) changed(u *store.User) {
	for _, x := range s.all() {
		x.do(func() { x.contactChanged(u) })
	}
}

// friendsChanged is told that a user's friends or blocked list changed:
// their own sessions sync their lists, the others their reverse lists and
// what they see of them.
func (s *Server) friendsChanged(user int64) {
	go func() {
		u, err := s.B.Store.UserByID(user)
		if err != nil {
			return
		}
		for _, x := range s.all() {
			if x.u.ID == user {
				x.do(x.syncLists)
			} else {
				x.do(func() { x.reverseChanged(u) })
			}
		}
	}()
}

// randomHex is n random bytes, in hex.
func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// reachHost is where a client on conn is sent for a switchboard.
func (s *Server) reachHost(c net.Conn) string {
	if s.S.Host != "" {
		return s.S.Host
	}
	return c.LocalAddr().String()
}
