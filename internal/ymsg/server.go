// Package ymsg is a Yahoo! Messenger server (YMSG) for the BBS's chat,
// beside its IRC, AIM and MSN servers: Yahoo! Messenger clients sign in
// with BBS names and passwords; their buddy list is the user's friends
// list and their ignore list the blocked list; an IM is a private message
// (a PRIVMSG to an IRC user, a line on a caller's terminal, an IM on AIM
// or MSN), kept for one away; a Yahoo chat room is a BBS chat room, and a
// conference a room of its own.
//
// It speaks YMSG 9 to 16.  Clients before YMSG 16 (Y!M 5.x, and those
// that take the login the server offers) sign in by the challenge login
// whose digests the store keeps (auth type 0); YMSG 16 clients (Y!M 9 and
// later) send the password over HTTPS to "login.yahoo.com", which the web
// half of the server answers (http.go), and then prove the token it gave.
package ymsg

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
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
	loginTimeout = time.Minute
	idleTimeout  = 15 * time.Minute // clients PING or KEEPALIVE every minute or so
	writeTimeout = 30 * time.Second
	tokenLife    = 5 * time.Minute // from pwtoken_get to the sign-in
	queueLen     = 512
)

// Settings are the [ymsg] section's.
type Settings struct {
	Address string // the pager, where to listen (":5050")
	// HTTPAddress and HTTPSAddress serve the web half: the capacity
	// lookup, the room list and (HTTPS) the YMSG 16 login ("": none)
	HTTPAddress  string
	HTTPSAddress string
	CertFile     string
	KeyFile      string
	// Host is the address the capacity lookup gives clients for the pager
	// ("": the address each one asked on)
	Host       string
	MaxClients int
	MaxPerIP   int
	// System is the Yahoo ID the BBS's notices come from
	System string
}

// Server serves YMSG.
type Server struct {
	B *bbs.BBS
	S Settings

	mu       sync.Mutex
	sessions map[*session]bool
	byNode   map[int]*session
	tokens   map[string]*token // pwtoken_get's, by token
	cookies  map[string]*token // pwtoken_login's, by T cookie
}

// token is a YMSG 16 login in progress: the user whose password was
// checked, and what pwtoken_login gave.
type token struct {
	u       *store.User
	crumb   string
	y, t    string
	expires time.Time
}

// New checks the settings and makes the server.
func New(b *bbs.BBS, s Settings) (*Server, error) {
	if s.Address == "" {
		return nil, errors.New("ymsg: no address to listen on")
	}
	if s.System == "" {
		s.System = "bbs"
	}
	if s.MaxClients == 0 {
		s.MaxClients = 256
	}
	return &Server{B: b, S: s, sessions: map[*session]bool{}, byNode: map[int]*session{},
		tokens: map[string]*token{}, cookies: map[string]*token{}}, nil
}

// Run listens until ctx is done: the pager, and the web half if set.
func (s *Server) Run(ctx context.Context) error {
	ln, err := sockets.Listen("tcp", s.S.Address)
	if err != nil {
		return fmt.Errorf("ymsg: %w", err)
	}
	errs := make(chan error, 3)
	if s.S.HTTPAddress != "" || s.S.HTTPSAddress != "" {
		go func() { errs <- s.serveWeb(ctx) }()
	}
	go func() { errs <- s.Serve(ctx, ln) }()
	return <-errs
}

// Serve answers the pager on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.B.Store.SetKeepYMSG(true)
	stop := s.B.WatchPresence(s.presence)
	defer stop()
	stopFriends := s.B.WatchFriends(s.friendsChanged)
	defer stopFriends()
	ts := &tcpserv.Server{Name: "ymsg", MaxClients: s.S.MaxClients, MaxPerIP: s.S.MaxPerIP, Log: s.B.Log,
		Handle: s.handle,
		Refuse: func(c net.Conn) bool { return s.B.Refuse("ymsg", c.RemoteAddr()) }}
	return ts.Serve(ctx, ln)
}

// Clients is how many are signed in.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// idOf is a user's Yahoo ID: their name in lower case, without spaces.
func idOf(u *store.User) string { return idOfName(u.Name) }

// idOfName is the Yahoo ID for a BBS name.
func idOfName(name string) string { return strings.ToLower(strings.ReplaceAll(name, " ", "")) }

// systemID is the ID the BBS's notices come from.
func (s *Server) systemID() string { return idOfName(s.S.System) }

// resolve is the account a Yahoo ID names (nil: none): a name with or
// without its spaces, in any case, without any "@yahoo.com".
func (s *Server) resolve(id string) *store.User {
	id = strings.TrimSpace(id)
	if i := strings.IndexByte(id, '@'); i >= 0 {
		id = id[:i]
	}
	if id == "" {
		return nil
	}
	u, err := s.B.Store.UserByScreenName(id)
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

func (s *Server) register(ss *session) {
	s.mu.Lock()
	s.sessions[ss] = true
	s.byNode[ss.n.Num] = ss
	s.mu.Unlock()
}

func (s *Server) unregister(ss *session) {
	s.mu.Lock()
	delete(s.sessions, ss)
	if s.byNode[ss.n.Num] == ss {
		delete(s.byNode, ss.n.Num)
	}
	s.mu.Unlock()
}

// all are the sessions signed in.
func (s *Server) all() []*session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*session, 0, len(s.sessions))
	for ss := range s.sessions {
		out = append(out, ss)
	}
	return out
}

// sessionsOf are a user's sessions.
func (s *Server) sessionsOf(id int64) []*session {
	var out []*session
	for _, ss := range s.all() {
		if ss.u.ID == id {
			out = append(out, ss)
		}
	}
	return out
}

// onNode is the session on a node (nil: the node is another kind).
func (s *Server) onNode(num int) *session {
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
		x.do(func() { x.buddyChanged(u) })
	}
}

// friendsChanged is told that a user's friends or blocked list changed:
// their own sessions sync their lists; the others what they see of them.
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
				x.do(func() { x.buddyChanged(u) })
			}
		}
	}()
}

// randomString is n random letters and digits.
func randomString(n int) string {
	const digits = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		x, _ := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		b[i] = digits[x.Int64()]
	}
	return string(b)
}

// randomSession is a session id (never 0).
func randomSession() uint32 {
	var b [4]byte
	for {
		rand.Read(b[:])
		if v := binary.BigEndian.Uint32(b[:]) & 0x7fffffff; v != 0 {
			return v
		}
	}
}
