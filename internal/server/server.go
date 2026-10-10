// Package server takes the BBS's callers: it listens, works out what kind
// of terminal each one has and hands it to the text or 3270 front end.
//
// A listener is one of:
//
//   - telnet: a telnet server.  A client that says it is a 3270
//     (IBM-3278-2-E, IBM-DYNAMIC, ...) is negotiated into TN3270 by go3270
//     and gets the 3270 screens; any other gets the text BBS.
//   - raw: no protocol, the caller's bytes as they come: app_dmodem's
//     mode = tcp, a modemmenu tcp or unix item, a terminal on a serial line
//     server.
//   - rlogin: RFC 1282, as modemmenu's rlogin items speak it: the server
//     user is the name, the client user the password (Synchronet's
//     convention), or with trust_user the name alone logs in.
//
// Any of them may read a session header first: NAME=value lines and an
// empty line, the DMODEM_* of app_dmodem's header = yes or the MODEM_* of
// a modemmenu item's header = yes, with the call's rate, caller ID and
// number dialed, and with trust_user the user modemmenu logged in.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/telnet"
	"github.com/Neo-Desktop/bbs-sec/internal/textui"
	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
)

// Listener is a [listen NAME] section.
type Listener struct {
	Name          string
	Kind          string // telnet, raw, rlogin
	Address       string // host:port, or a unix socket's path
	Via           string // how its callers came, for who's online
	TN3270        bool   // telnet: take 3270 clients
	ANSI          string // auto, yes, no: the terminal's ANSI, if not told
	UTF8          string // auto, yes, no
	Sixel         string // auto or yes (ask the terminal), no
	Header        bool   // read a session header first
	HeaderTimeout time.Duration
	TrustUser     bool // the header's MODEM_USER (rlogin's server user) is logged in
	CreateUsers   bool // and gets an account if it has none
	MaxPerIP      int  // connections at once from one address (not loopback)
	SocketMode    os.FileMode
	SocketGroup   string
}

// Unix reports whether the listener is on a unix socket.
func (l *Listener) Unix() bool {
	return strings.HasPrefix(l.Address, "/") || strings.HasPrefix(l.Address, "unix:")
}

func (l *Listener) path() string { return strings.TrimPrefix(l.Address, "unix:") }

// Server runs the listeners.
type Server struct {
	B         *bbs.BBS
	Listeners []*Listener
	Graphics  ui3270.Graphics // for 3270s
	LogoFile  string          // the logo for sixel terminals ("": the name)

	mu    sync.Mutex
	perIP map[string]int
	conns map[net.Conn]bool
	wg    sync.WaitGroup
}

// New makes a server.
func New(b *bbs.BBS, ls []*Listener) *Server {
	return &Server{B: b, Listeners: ls, perIP: map[string]int{}, conns: map[net.Conn]bool{}}
}

// Run listens until ctx is done, then hangs up every caller.
func (s *Server) Run(ctx context.Context) error {
	var lns []net.Listener
	closeAll := func() {
		for _, ln := range lns {
			ln.Close()
		}
	}
	for _, l := range s.Listeners {
		ln, err := s.listen(l)
		if err != nil {
			closeAll()
			return fmt.Errorf("[listen %s] %s: %w", l.Name, l.Address, err)
		}
		lns = append(lns, ln)
		s.B.Log.Printf("listening on %s (%s)", l.Address, l.Kind)
	}
	for i, l := range s.Listeners {
		go s.accept(lns[i], l)
	}
	<-ctx.Done()
	s.B.Log.Printf("stopping")
	closeAll()
	for _, l := range s.Listeners {
		if l.Unix() {
			os.Remove(l.path())
		}
	}
	s.B.KickAll()
	s.mu.Lock()
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	return nil
}

func (s *Server) listen(l *Listener) (net.Listener, error) {
	if !l.Unix() {
		return net.Listen("tcp", l.Address)
	}
	path := l.path()
	if _, err := os.Lstat(path); err == nil {
		// a live server keeps its socket; a dead one's is replaced
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			c.Close()
			return nil, errors.New("another server is listening there")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	// made private, then opened up to socket_mode: no moment in between
	// when others could connect
	old := syscall.Umask(0177)
	ln, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, l.SocketMode); err != nil {
		ln.Close()
		return nil, err
	}
	if l.SocketGroup != "" {
		g, err := user.LookupGroup(l.SocketGroup)
		if err == nil {
			gid, _ := strconv.Atoi(g.Gid)
			err = os.Chown(path, -1, gid)
		}
		if err != nil {
			ln.Close()
			return nil, fmt.Errorf("socket_group %s: %w", l.SocketGroup, err)
		}
	}
	return ln, nil
}

func (s *Server) accept(ln net.Listener, l *Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			s.B.Log.Printf("[listen %s] accept: %v", l.Name, err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(c, l)
		}()
	}
}

// host is a TCP peer's address without the port, "" for loopback and unix
// peers (which are modem servers and menus: every call comes from them).
func host(c net.Conn) string {
	a, ok := c.RemoteAddr().(*net.TCPAddr)
	if !ok || a.IP.IsLoopback() {
		return ""
	}
	return a.IP.String()
}

func (s *Server) track(c net.Conn, l *Listener) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := host(c)
	if h != "" && l.MaxPerIP > 0 && s.perIP[h] >= l.MaxPerIP {
		return false
	}
	if h != "" {
		s.perIP[h]++
	}
	s.conns[c] = true
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h := host(c); h != "" {
		if s.perIP[h]--; s.perIP[h] <= 0 {
			delete(s.perIP, h)
		}
	}
	delete(s.conns, c)
}

// writeTimeout: how long a write may wait for a caller who reads nothing
const writeTimeout = 2 * time.Minute

// timedConn gives every write a deadline, so that a caller who stops
// reading does not hold a session (and its node) for ever.
type timedConn struct{ net.Conn }

func (c timedConn) Write(p []byte) (int, error) {
	c.Conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.Conn.Write(p)
}

func (s *Server) handle(nc net.Conn, l *Listener) {
	defer nc.Close()
	// a bug in a protocol library (go3270 has had panics a client could
	// cause) costs this caller their call, not everyone theirs
	defer func() {
		if r := recover(); r != nil {
			s.B.Log.Printf("[listen %s] panic: %v", l.Name, r)
		}
	}()
	c := timedConn{nc}
	remote := "local"
	if c.RemoteAddr() != nil && c.RemoteAddr().String() != "" && c.RemoteAddr().String() != "@" {
		remote = c.RemoteAddr().String()
	}
	if !s.track(c, l) {
		s.B.Log.Printf("[listen %s] %s: too many connections from there", l.Name, remote)
		c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		io.WriteString(c, "\r\nToo many connections from your address.\r\n")
		return
	}
	defer s.untrack(c)

	caller := &bbs.Caller{Via: l.Via, Listener: l.Name, Remote: remote}
	if h := host(c); h != "" {
		caller.Source = "ip:" + h
	}
	if l.Header {
		h, err := ReadHeader(c, l.HeaderTimeout)
		if err != nil {
			s.B.Log.Printf("[listen %s] %s: %v", l.Name, remote, err)
			return
		}
		applyHeader(caller, h, l.TrustUser)
	}
	if caller.Source == "" && caller.CallerID != "" {
		caller.Source = "cid:" + caller.CallerID
	}
	// a caller with neither (a modem server's, with no caller ID) is not
	// throttled: they all come from the same place, and one caller's
	// failures would lock out every other.  Each call still has its
	// attempts and fail_delay.

	node, err := s.B.Enter(caller, func() { c.Close() })
	if err != nil {
		s.B.Log.Printf("[listen %s] %s: %v", l.Name, remote, err)
		c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		io.WriteString(c, "\r\nAll lines are busy; please call again later.\r\n")
		return
	}
	defer node.Leave()
	s.B.Log.Printf("node %d: %s on [listen %s]%s", node.Num, remote, l.Name, callDetails(caller))

	switch l.Kind {
	case "telnet":
		err = s.telnet(c, l, node)
	case "rlogin":
		err = s.rlogin(c, l, node)
	default:
		err = s.text(c, c, l, node, "")
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		s.B.Log.Printf("node %d: %v", node.Num, err)
	}
	s.B.Log.Printf("node %d: gone", node.Num)
}

func callDetails(c *bbs.Caller) string {
	var parts []string
	if c.Rate != "" {
		parts = append(parts, "rate "+c.Rate)
	}
	if c.CallerID != "" {
		parts = append(parts, "from "+c.CallerID)
	}
	if c.Dialed != "" {
		parts = append(parts, "to "+c.Dialed)
	}
	if c.TrustedUser != "" {
		parts = append(parts, "as "+c.TrustedUser)
	}
	if len(parts) == 0 {
		return ""
	}
	return ": " + strings.Join(parts, ", ")
}

// telnet: a 3270 to go3270, anything else to the text BBS
func (s *Server) telnet(c net.Conn, l *Listener, node *bbs.Node) error {
	tc := telnet.New(c)
	tt, err := tc.ProbeTermType(3 * time.Second)
	if err != nil {
		return err
	}
	node.Caller.Terminal = tt
	if l.TN3270 && telnet.Is3270(tt) {
		return s.tn3270(tc, l, node)
	}
	if !tc.Spoke() {
		// not a telnet client at all: a raw terminal
		return s.text(tc, c, l, node, "")
	}
	if err := tc.StartNVT(true, 2*time.Second); err != nil {
		return err
	}
	return s.text(tc, c, l, node, tt)
}

func (s *Server) tn3270(tc *telnet.Conn, l *Listener, node *bbs.Node) error {
	raw, err := tc.Handoff3270(2 * time.Second)
	if err != nil {
		return err
	}
	// go3270 negotiates without deadlines of its own: a client that
	// stops answering is hung up
	watchdog := time.AfterFunc(30*time.Second, func() { raw.Close() })
	dev, err := go3270.NegotiateTelnet(raw)
	if !watchdog.Stop() {
		return errors.New("tn3270: negotiation timed out")
	}
	if err != nil {
		return fmt.Errorf("tn3270: %w", err)
	}
	node.Caller.Via = "tn3270"
	node.Caller.Terminal = dev.TerminalType()
	s.B.Log.Printf("node %d: tn3270 %s", node.Num, ui3270.Capabilities(dev))
	return ui3270.Run(s.B, node, raw, dev, ui3270.Options{CreateTrusted: l.CreateUsers,
		Graphics: s.Graphics})
}

// what terminal types say about ANSI and UTF-8
func ansiTerm(tt string) (ansi, known bool) {
	tt = strings.ToLower(tt)
	for _, p := range []string{"ansi", "xterm", "vt1", "vt2", "vt3", "vt4", "vt5", "linux",
		"screen", "tmux", "rxvt", "putty", "syncterm", "konsole", "gnome", "alacritty",
		"kitty", "foot", "wezterm", "cygwin", "eterm", "iterm", "st-", "pcansi", "qansi"} {
		if strings.HasPrefix(tt, p) {
			return true, true
		}
	}
	if tt == "st" {
		return true, true
	}
	for _, p := range []string{"dumb", "tty", "vt52", "glasstty", "ascii", "network"} {
		if strings.HasPrefix(tt, p) {
			return false, true
		}
	}
	return false, false
}

func utf8Term(tt string) bool {
	tt = strings.ToLower(tt)
	// BBS terminals that say "ansi" want CP437, not UTF-8
	for _, p := range []string{"xterm", "linux", "screen", "tmux", "rxvt", "putty", "konsole",
		"gnome", "alacritty", "kitty", "foot", "wezterm", "iterm", "st-"} {
		if strings.HasPrefix(tt, p) {
			return true
		}
	}
	return tt == "st"
}

// text: the text BBS on rw (c for deadlines), tt the terminal type if told
func (s *Server) text(rw io.ReadWriter, c net.Conn, l *Listener, node *bbs.Node, tt string) error {
	conn := struct {
		io.ReadWriter
		deadliner
	}{rw, c}
	t := textui.NewTerm(conn)
	t.Clean8 = true
	if tc, ok := rw.(*telnet.Conn); ok {
		if cols, rows := tc.Size(); cols > 0 {
			t.Cols, t.Rows = cols, rows
		}
		// a telnet client that spoke telnet but refused binary mode
		t.Clean8 = tc.Binary() || !tc.Spoke()
	}
	ansi, known := ansiTerm(tt)
	switch l.ANSI {
	case "yes":
		t.ANSI = true
	case "no":
		t.ANSI = false
	default:
		if known {
			t.ANSI = ansi
		} else {
			got, err := t.DetectANSI(2 * time.Second)
			if err != nil {
				return err
			}
			t.ANSI = got
		}
	}
	switch l.UTF8 {
	case "yes":
		t.UTF8 = true
	case "no":
		t.UTF8 = false
	default:
		t.UTF8 = utf8Term(tt)
	}
	if t.ANSI && (l.Sixel == "auto" || l.Sixel == "yes") {
		on, err := t.DetectSixel(1500 * time.Millisecond)
		if err != nil {
			return err
		}
		t.Sixel = on
	}
	return textui.Run(s.B, node, t, textui.Options{CreateTrusted: l.CreateUsers,
		LogoFile: s.LogoFile})
}

type deadliner interface{ SetReadDeadline(time.Time) error }

// rlogin: RFC 1282's start, then the text BBS
func (s *Server) rlogin(c net.Conn, l *Listener, node *bbs.Node) error {
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var fields []string
	var cur []byte
	b := make([]byte, 1)
	for len(fields) < 4 {
		if _, err := c.Read(b); err != nil {
			return fmt.Errorf("rlogin: %w", err)
		}
		if b[0] != 0 {
			if len(cur) >= 256 {
				return errors.New("rlogin: a field is too long")
			}
			cur = append(cur, b[0])
			continue
		}
		fields = append(fields, string(cur))
		cur = cur[:0]
	}
	c.SetReadDeadline(time.Time{})
	// fields[0] is empty: the protocol starts with a NUL
	client, server, term := fields[1], fields[2], fields[3]
	if _, err := c.Write([]byte{0}); err != nil {
		return err
	}
	tt, speed, _ := strings.Cut(term, "/")
	caller := node.Caller
	caller.Terminal = tt
	if caller.Rate == "" && speed != "" {
		if n, err := strconv.Atoi(speed); err == nil && n > 0 {
			caller.Rate = speed + "/" + speed
		}
	}
	server = strings.TrimSpace(server)
	if l.TrustUser && server != "" {
		if caller.TrustedUser == "" {
			caller.TrustedUser = server
		}
	} else {
		caller.LoginUser, caller.LoginPassword = server, client
	}
	return s.text(c, c, l, node, tt)
}
