// Package oscar is an OSCAR server (the protocol of AIM and ICQ) for the
// BBS's chat, beside its IRC server: AIM and ICQ clients sign on with BBS
// names (or ICQ numbers made of the accounts') and passwords; their
// buddies are the BBS's users, online by any route; an IM is a private
// message (a PRIVMSG to an IRC user, a line on a caller's terminal), and
// a chat room is a BBS chat room, an IRC channel.
//
// One port (5190) serves the whole of it: logins (FLAP channel 1 and
// BUCP), the BOS connection and the services' (chat navigation, chat
// rooms), all told apart by their cookies.  AOL's own clients (P3) came on
// 5190 too: a connection whose first byte is P3's is handed to Server.P3.
package oscar

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/oldtls"
	"github.com/D-Modem/bbs/internal/sockets"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/tcpserv"
)

// timeouts
const (
	sniffWait    = time.Second      // for a client's first byte, before the server's hello
	loginTimeout = time.Minute      // to log in, or to bring up a connection
	idleTimeout  = 10 * time.Minute // without a frame (clients keep alive each minute or so)
	writeTimeout = 30 * time.Second
	cookieLife   = 5 * time.Minute // a cookie is used within this
	queueLen     = 512             // things waiting for a session's goroutine
)

// p3Sync is the first byte of an AOL client's P3 packet.
const p3Sync = 0x5a

// Settings are the [oscar] section's.
type Settings struct {
	Address string // where to listen
	// Host is where clients are sent to reconnect (the BOS and the
	// services), host:port; "": the address each one came to
	Host       string
	MaxClients int
	MaxPerIP   int
	// UINBase: an account's ICQ number is UINBase plus its id
	UINBase uint32
	// System is the screen name the BBS's notices come from
	System string
	// TLSAddress, if set, is where clients come by TLS (AIM 6.2 to 7.x,
	// clientLogin over HTTPS), with the certificate of TLSCert and TLSKey
	TLSAddress, TLSCert, TLSKey string
	// TLSHost is where clients that ask for TLS are sent (host:port, its
	// host the certificate's name); "": Host's host at TLSAddress's port.
	// Set it to a TLS proxy's when one is in front.
	TLSHost string
}

// Server serves OSCAR.
type Server struct {
	B *bbs.BBS
	S Settings
	// P3, if set, takes a connection that began as an AOL client's (P3):
	// its first bytes are still to be read from it
	P3 func(net.Conn)

	mu       sync.Mutex
	sessions map[*session]bool
	byNode   map[int]*session
	cookies  map[string]*cookie
	bagMu    sync.Mutex // placing friends in buddy lists
	web      web        // the HTTP logins
}

// New checks the settings and makes the server.
func New(b *bbs.BBS, s Settings) (*Server, error) {
	if s.Address == "" {
		return nil, errors.New("oscar: no address to listen on")
	}
	if s.Host != "" {
		if _, _, err := net.SplitHostPort(s.Host); err != nil {
			return nil, fmt.Errorf("oscar: host: %w", err)
		}
	}
	if s.UINBase == 0 {
		s.UINBase = 100000
	}
	if s.System == "" {
		s.System = "OnlineHost"
	}
	if s.MaxClients == 0 {
		s.MaxClients = 256
	}
	return &Server{B: b, S: s, sessions: map[*session]bool{}, byNode: map[int]*session{},
		cookies: map[string]*cookie{}}, nil
}

// Run listens until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	ln, err := sockets.Listen("tcp", s.S.Address)
	if err != nil {
		return fmt.Errorf("oscar: %w", err)
	}
	return s.Serve(ctx, ln)
}

// Serve answers on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.B.Store.SetKeepOSCAR(true)
	stop := s.B.WatchPresence(s.presence)
	defer stop()
	stopFriends := s.B.WatchFriends(s.friendsChanged)
	defer stopFriends()
	defer s.stopWeb()
	refuse := func(c net.Conn) bool { return s.B.Refuse("oscar", c.RemoteAddr()) }
	errs := make(chan error, 1)
	if s.S.TLSAddress != "" {
		src, err := certs.New(s.S.TLSCert, s.S.TLSKey)
		if err != nil {
			return fmt.Errorf("oscar: tls_cert, tls_key: %w", err)
		}
		tln, err := sockets.Listen("tcp", s.S.TLSAddress)
		if err != nil {
			return fmt.Errorf("oscar: %w", err)
		}
		if c, err := src.GetCertificate(&tls.ClientHelloInfo{}); err == nil {
			if _, ok := c.PrivateKey.(*rsa.PrivateKey); !ok {
				s.B.Log.Printf("oscar: tls_cert's key is not RSA: AIM 6.2 to 7.5 can't sign on by TLS")
			}
		}
		cfg := legacyTLS(src.GetCertificate)
		ts := &tcpserv.Server{Name: "oscar tls", MaxClients: s.S.MaxClients, MaxPerIP: s.S.MaxPerIP, Log: s.B.Log,
			Refuse: refuse, Handle: func(c net.Conn) {
				c.SetDeadline(time.Now().Add(loginTimeout))
				r := bufio.NewReader(c)
				first, err := r.Peek(3)
				if err != nil {
					return
				}
				var tc interface {
					net.Conn
					Handshake() error
				}
				if oldtls.IsSSLv2Hello(first) { // AIM 6.2 to 7.5, which crypto/tls can't take
					tc = oldtls.Server(&peeked{c, r}, &oldtls.Config{GetCertificate: src.GetCertificate})
				} else {
					tc = tls.Server(&peeked{c, r}, cfg)
				}
				if err := tc.Handshake(); err != nil {
					s.B.Log.Printf("oscar: %s: TLS: %v", c.RemoteAddr(), err)
					return
				}
				tc.SetDeadline(time.Time{})
				s.handle(tc)
			}}
		go func() { errs <- ts.Serve(ctx, tln) }()
	}
	ts := &tcpserv.Server{Name: "oscar", MaxClients: s.S.MaxClients, MaxPerIP: s.S.MaxPerIP, Log: s.B.Log,
		Handle: s.handle, Refuse: refuse}
	err := ts.Serve(ctx, ln)
	if s.S.TLSAddress != "" {
		<-errs
	}
	return err
}

// Clients is how many sessions are signed on.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// peeked is a connection whose first bytes were read into r.
type peeked struct {
	net.Conn
	r *bufio.Reader
}

func (p *peeked) Read(b []byte) (int, error) { return p.r.Read(b) }

// handle serves a connection: P3 to Server.P3, or OSCAR: a login, a BOS
// connection, or a service's.
func (s *Server) handle(c net.Conn) {
	host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	r := bufio.NewReaderSize(c, 8192)
	c.SetReadDeadline(time.Now().Add(sniffWait))
	first, err := r.Peek(1)
	var ne net.Error
	switch {
	case err == nil && first[0] == p3Sync:
		if s.P3 == nil {
			s.B.Log.Printf("oscar: %s: an AOL client (P3), which is not served here", host)
			return
		}
		c.SetReadDeadline(time.Time{})
		s.P3(&peeked{c, r})
		return
	case err == nil && isHTTP(first[0]):
		c.SetReadDeadline(time.Time{})
		s.serveHTTP(&peeked{c, r}) // a later AIM's login, by HTTP
		return
	case err == nil && first[0] != flapStart:
		return // neither
	case err != nil && !(errors.As(err, &ne) && ne.Timeout()):
		return
	}
	// OSCAR: the server's hello first (the client may have sent its own)
	c.SetDeadline(time.Now().Add(loginTimeout))
	fc := newFlapConn(c, r)
	if err := fc.hello(); err != nil {
		return
	}
	f, err := fc.read()
	for err == nil && f.ch == chanKeepAlive {
		f, err = fc.read()
	}
	if err != nil || f.ch != chanSignon || len(f.data) < 4 {
		return
	}
	t := parseTLVs(f.data[4:])
	switch {
	case t.has(0x0006):
		s.connect(fc, host, t)
	case t.has(0x0001):
		s.channelLogin(fc, host, t)
	default:
		s.bucp(fc, host)
	}
}

// cookie is a login's or a service's ticket to connect.
type cookie struct {
	user     *store.User
	sn       string // the screen name the client signed on as
	icq      bool
	clientID string
	service  uint16   // 0 for BOS
	room     string   // chat: the room
	exchange uint16   // chat: its exchange
	sess     *session // a service's: the session it serves
	tls      bool     // the client comes by TLS
	expires  time.Time
}

// newCookie keeps a cookie and gives its bytes.
func (s *Server) newCookie(k *cookie) []byte {
	b := make([]byte, 32)
	rand.Read(b)
	k.expires = time.Now().Add(cookieLife)
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, v := range s.cookies {
		if time.Now().After(v.expires) {
			delete(s.cookies, key)
		}
	}
	s.cookies[string(b)] = k
	return b
}

// takeCookie spends a cookie (nil: none such, or expired).
func (s *Server) takeCookie(b []byte) *cookie {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.cookies[string(b)]
	delete(s.cookies, string(b))
	if k == nil || time.Now().After(k.expires) {
		return nil
	}
	return k
}

// reconnectHost is where a client on conn is sent for the BOS or a
// service.
func (s *Server) reconnectHost(c net.Conn) string {
	if s.S.Host != "" {
		return s.S.Host
	}
	return c.LocalAddr().String()
}

// connect is a connection with a cookie: a BOS session's, or a service's.
func (s *Server) connect(fc *flapConn, host string, t tlvs) {
	v, _ := t.get(0x0006)
	k := s.takeCookie(v)
	if k == nil {
		var o tlvs
		o.addU16(0x0008, 0x0018) // reconnect, the cookie is stale
		fc.signoff(o)
		return
	}
	if k.service == 0 {
		s.bos(fc, host, k)
		return
	}
	if k.sess == nil || k.sess.closed() {
		return
	}
	k.sess.service(fc, k)
}

// register keeps a session on (its node known).
func (s *Server) register(ss *session) {
	s.mu.Lock()
	s.sessions[ss] = true
	s.byNode[ss.n.Num] = ss
	s.mu.Unlock()
}

// unregister forgets it.
func (s *Server) unregister(ss *session) {
	s.mu.Lock()
	delete(s.sessions, ss)
	if s.byNode[ss.n.Num] == ss {
		delete(s.byNode, ss.n.Num)
	}
	s.mu.Unlock()
}

// all are the sessions signed on.
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
		if ss.u.ID == id && ss.ready() {
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

// presence is told of a BBS user coming online or going: each session
// watching them is told, in its own goroutine.
func (s *Server) presence(name string, online bool) {
	ss := s.all()
	if len(ss) == 0 {
		return
	}
	go func() {
		u, err := s.B.Store.UserByName(name)
		if err != nil {
			return
		}
		for _, x := range ss {
			x.do(func() { x.buddyChanged(u) })
		}
	}()
}

// changed tells every session watching the user that their state is
// new (away, idle, signed on or off by AIM).
func (s *Server) changed(u *store.User) {
	for _, x := range s.all() {
		x.do(func() { x.buddyChanged(u) })
	}
}

// resolve is the account a screen name is (an ICQ number, or a name).
func (s *Server) resolve(sn string) *store.User {
	if isUIN(sn) {
		if id := s.accountOfUIN(sn); id != 0 {
			if u, err := s.B.Store.UserByID(id); err == nil {
				return u
			}
		}
		return nil
	}
	u, err := s.B.Store.UserByScreenName(sn)
	if err != nil {
		return nil
	}
	return u
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

// itoa is strconv's.
func itoa(n int) string { return strconv.Itoa(n) }
