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
//   - ssh: SSH, with the BBS's passwords or users' keys (or "new", for the
//     BBS's own login prompt); the text BBS.
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
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/racingmars/go3270"
	"golang.org/x/crypto/ssh"

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
	PETSCII       string // yes (Commodores only), auto (ask a terminal that is not ANSI), no
	RIP           string // auto (ask a BBS terminal program), yes, no
	VT            string // auto: ask terminals what they are (DA1, DA2, ESC Z ...); no
	Header        bool   // read a session header first
	HeaderTimeout time.Duration
	TrustUser     bool // the header's MODEM_USER (rlogin's server user) is logged in
	CreateUsers   bool // and gets an account if it has none
	MaxPerIP      int  // connections at once from one address (not loopback)
	SocketMode    os.FileMode
	SocketGroup   string
	HostKeys      []string // ssh: the host keys' files
	Legacy        bool     // ssh: offer old algorithms too (CBC ciphers, DH group 1)
	// TLS: telnet or raw over TLS (telnets, port 992), with this
	// certificate and key
	CertFile, KeyFile string

	ssh *ssh.ServerConfig
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
	// the country (or city) database is read now, not on the first call
	go s.B.Geo().Refresh()
	var lns []net.Listener
	closeAll := func() {
		for _, ln := range lns {
			ln.Close()
		}
	}
	for _, l := range s.Listeners {
		if l.Kind == "ssh" {
			if err := s.sshSetup(l); err != nil {
				closeAll()
				return fmt.Errorf("[listen %s] %w", l.Name, err)
			}
		}
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
	if l.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(l.CertFile, l.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("tls: %w", err)
		}
		ln, err := net.Listen("tcp", l.Address)
		if err != nil {
			return nil, err
		}
		return tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert},
			MinVersion: tls.VersionTLS12}), nil
	}
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
	// made in a directory of its own that only this user can reach, given
	// its mode and group there, then moved into place: no moment when
	// others could connect, and no umask changed (the whole process's:
	// files others make meanwhile would lose their modes)
	tmp, err := os.MkdirTemp(filepath.Dir(path), ".s")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	made := filepath.Join(tmp, "s")
	ln, err := net.Listen("unix", made)
	if err != nil {
		return nil, err
	}
	ul := ln.(*net.UnixListener)
	ul.SetUnlinkOnClose(false) // the name it was made under is gone
	fail := func(err error) (net.Listener, error) {
		ul.Close()
		return nil, err
	}
	if err := os.Chmod(made, l.SocketMode); err != nil {
		return fail(err)
	}
	if l.SocketGroup != "" {
		g, err := user.LookupGroup(l.SocketGroup)
		if err == nil {
			gid, _ := strconv.Atoi(g.Gid)
			err = os.Chown(made, -1, gid)
		}
		if err != nil {
			return fail(fmt.Errorf("socket_group %s: %w", l.SocketGroup, err))
		}
	}
	if err := os.Rename(made, path); err != nil {
		return fail(err)
	}
	return &unixListener{ul, path}, nil
}

// unixListener removes its socket when it is closed.
type unixListener struct {
	*net.UnixListener
	path string
}

func (u *unixListener) Close() error {
	err := u.UnixListener.Close()
	os.Remove(u.path)
	return err
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
	if s.B.Refuse("[listen "+l.Name+"]", nc.RemoteAddr()) {
		return
	}
	if tc, ok := nc.(*tls.Conn); ok {
		// a client that says nothing does not hold the connection
		tc.SetDeadline(time.Now().Add(15 * time.Second))
		if err := tc.Handshake(); err != nil {
			s.B.Log.Printf("[listen %s] %s: TLS: %v", l.Name, nc.RemoteAddr(), err)
			return
		}
		tc.SetDeadline(time.Time{})
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
	if l.Kind == "ssh" {
		s.sshCall(c, l, caller, remote)
		return
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
	s.B.Log.Printf("node %d: %s on [listen %s]%s", node.Num, remote, l.Name, callDetails(caller, node.Place()))

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

func callDetails(c *bbs.Caller, p bbs.Place) string {
	var parts []string
	if c.Rate != "" {
		parts = append(parts, "rate "+c.Rate)
	}
	if c.CallerID != "" {
		from := "from " + c.CallerID
		if p.Number != "" && p.Number != c.CallerID {
			from += " (" + p.Number + ")"
		}
		parts = append(parts, from)
	}
	switch {
	case p.Name != "":
		parts = append(parts, "in "+p.String())
	case p.Service != "":
		a := "a "
		if strings.ContainsRune("aeiouAEIOU", rune(p.Service[0])) {
			a = "an "
		}
		parts = append(parts, a+p.Service+" number")
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
	node.SetTerminal(node.Caller.Via, tt)
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
	node.SetTerminal("tn3270", dev.TerminalType())
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
func (s *Server) text(rw io.ReadWriter, c deadliner, l *Listener, node *bbs.Node, tt string) error {
	conn := struct {
		io.ReadWriter
		deadliner
	}{rw, c}
	t := textui.NewTerm(conn)
	t.Clean8 = true
	t.Ident = node.Caller.Ident
	if sz, ok := rw.(interface{ Size() (int, int) }); ok {
		if cols, rows := sz.Size(); cols > 0 {
			t.Cols, t.Rows = cols, rows
		}
	}
	if tc, ok := rw.(*telnet.Conn); ok {
		// a telnet client that spoke telnet but refused binary mode
		t.Clean8 = tc.Binary() || !tc.Spoke()
	}
	if l.PETSCII == "yes" || petsciiTerm(tt) {
		// a Commodore: nothing ANSI's is asked of it
		t.SetPETSCII(true, 40)
		node.SetTerminal(node.Caller.Via, "petscii")
		return s.run(t, l, node)
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
	if !t.ANSI && !known && (l.VT == "auto" || l.VT == "yes") && l.ANSI != "no" {
		is52, err := t.DetectVT52(time.Second)
		if err != nil {
			return err
		}
		if is52 {
			t.SetVT52()
			node.SetIdent("VT52")
			s.B.Log.Printf("node %d: terminal VT52", node.Num)
			return s.run(t, l, node)
		}
	}
	if !t.ANSI && !known && l.PETSCII == "auto" {
		// perhaps a Commodore: its keys will tell (the login watches)
		t.WatchPETSCII, t.Upper = true, true
	}
	switch l.UTF8 {
	case "yes":
		t.UTF8 = true
	case "no":
		t.UTF8 = false
	default:
		t.UTF8 = utf8Term(tt)
	}
	if t.ANSI && (l.RIP == "yes" || l.RIP == "auto" && !utf8Term(tt)) {
		on, err := t.DetectRIP(1500 * time.Millisecond)
		if err != nil {
			return err
		}
		t.RIP = on || l.RIP == "yes"
	}
	sixel := t.ANSI && !t.RIP && (l.Sixel == "auto" || l.Sixel == "yes")
	if t.ANSI && (l.VT == "auto" || l.VT == "yes") {
		in, err := t.Identify(1500 * time.Millisecond)
		if err != nil {
			return err
		}
		if in.Answered() {
			node.SetIdent(in.Name())
			s.B.Log.Printf("node %d: terminal %s (%s)", node.Num, in.Name(), in.Details())
		}
		t.Sixel = sixel && in.Sixel()
		t.Mono = in.Mono()
		// a VT without UTF-8 (not a PC's BBS terminal, which has CP437):
		// its line drawing and Latin-1
		if in.Level() > 0 && in.CTerm == "" && !t.UTF8 && !cp437Term(tt) {
			t.SetDEC(in.Level() >= 2 && t.Clean8)
		}
	} else if l.Sixel == "always" {
		t.Sixel = t.ANSI // a terminal known to have it (the web terminal's)
	} else if sixel {
		on, err := t.DetectSixel(1500 * time.Millisecond)
		if err != nil {
			return err
		}
		t.Sixel = on
	}
	return s.run(t, l, node)
}

func (s *Server) run(t *textui.Term, l *Listener, node *bbs.Node) error {
	return textui.Run(s.B, node, t, textui.Options{CreateTrusted: l.CreateUsers,
		LogoFile: s.LogoFile})
}

// WebTerminal is a caller at the HTTP server's web terminal page: c its
// WebSocket as a stream, the page's xterm.js on it, cols x rows.
func (s *Server) WebTerminal(c net.Conn, remote, host string, cols, rows int) error {
	// the page's xterm.js has the image addon: sixel, without asking
	l := &Listener{Name: "web", Kind: "web", Via: "web terminal", ANSI: "yes", UTF8: "yes",
		Sixel: "always", RIP: "no", PETSCII: "no", VT: "no"}
	caller := &bbs.Caller{Via: l.Via, Listener: "http", Remote: remote, Terminal: "xterm.js"}
	if host != "" {
		caller.Source = "ip:" + host
	}
	node, err := s.B.Enter(caller, func() { c.Close() })
	if err != nil {
		io.WriteString(c, "\r\nAll lines are busy; please call again later.\r\n")
		return nil
	}
	defer node.Leave()
	node.SetIdent("web terminal")
	s.B.Log.Printf("node %d: %s on the web terminal (%dx%d)", node.Num, remote, cols, rows)
	// the WebSocket's own deadlines close it: the line's end only a read
	t := newSSHTerm(c, c, sshPty{Cols: uint32(cols), Rows: uint32(rows)})
	defer t.close()
	err = s.text(t, t, l, node, "xterm-256color")
	s.B.Log.Printf("node %d: gone", node.Num)
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

type deadliner interface{ SetReadDeadline(time.Time) error }

// cp437Term: a terminal type a PC's BBS terminal program says (CP437, not
// a VT's character sets)
func cp437Term(tt string) bool {
	tt = strings.ToLower(tt)
	for _, p := range []string{"ansi", "syncterm", "pcansi", "qansi", "netrunner", "cterm", "scoansi"} {
		if strings.HasPrefix(tt, p) {
			return true
		}
	}
	return false
}

// petsciiTerm: a terminal type a Commodore's terminal program says
func petsciiTerm(tt string) bool {
	tt = strings.ToLower(tt)
	for _, p := range []string{"petscii", "c64", "c128", "commodore", "cbm"} {
		if strings.HasPrefix(tt, p) {
			return true
		}
	}
	return false
}

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
	node.SetTerminal(caller.Via, tt)
	if caller.Rate == "" && speed != "" {
		if n, err := strconv.Atoi(speed); err == nil && n > 0 {
			node.SetRate(speed + "/" + speed)
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
