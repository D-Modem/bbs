package mrc

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// TestParse: a line's fields, and the line made again; a short line
// refused; names and text sanitized.
func TestParse(t *testing.T) {
	m, ok := Parse("bob~Far~lobby~~~lobby~|03<|14bob|03> |07hi~\r\n")
	if !ok || m.FromUser != "bob" || m.FromSite != "Far" || m.ToRoom != "lobby" || m.Body != "|03<|14bob|03> |07hi" {
		t.Errorf("%+v %v", m, ok)
	}
	if m.Line() != "bob~Far~lobby~~~lobby~|03<|14bob|03> |07hi~" {
		t.Errorf("line %q", m.Line())
	}
	if _, ok := Parse("HELLO"); ok {
		t.Error("a short line parsed")
	}
	if got := SanitizeName("|12Carol Ann\x01~" + strings.Repeat("x", 40)); got != "Carol_Ann}"[:9]+strings.Repeat("x", 21) {
		t.Errorf("sanitized %q", got)
	}
	if got := sanitizeText("a~b\x7fc é"); got != "abc " {
		t.Errorf("text %q", got)
	}
}

// fakeServer is an MRC server: it reads the handshake, says hello, and
// gives what each connection sends.
type fakeServer struct {
	addr  string
	mu    sync.Mutex
	conn  net.Conn
	lines chan string
	conns chan string // each handshake
}

// newServer is a fake MRC server listening, closed when the test ends.
func newServer(t *testing.T) *fakeServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s := &fakeServer{addr: ln.Addr().String(), lines: make(chan string, 100), conns: make(chan string, 10)}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			r := bufio.NewReader(c)
			hs, _ := r.ReadString('\n')
			s.conns <- strings.TrimSpace(hs)
			c.Write([]byte("|07HELLO from the test server\n"))
			s.mu.Lock()
			s.conn = c
			s.mu.Unlock()
			go func() {
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					s.lines <- strings.TrimRight(l, "\n")
				}
			}()
		}
	}()
	return s
}

// say sends a line to the last connection.
func (s *fakeServer) say(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn.Write([]byte(line + "\n"))
}

// drop closes the last connection.
func (s *fakeServer) drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn.Close()
}

// expect waits for a line from the client.
func (s *fakeServer) expect(t *testing.T, want string) {
	t.Helper()
	var seen []string
	deadline := time.After(3 * time.Second)
	for {
		select {
		case l := <-s.lines:
			if l == want {
				return
			}
			seen = append(seen, l)
		case <-deadline:
			t.Fatalf("no %q among %q", want, seen)
		}
	}
}

type env struct {
	b      *bbs.BBS
	c      *Client
	server *fakeServer
}

// start is a BBS (alice and bob) whose MRC client, with require, is
// connected to a fake server: its handshake and first lines checked.
func start(t *testing.T, require string) *env {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, n := range []string{"alice", "bob"} {
		st.CreateUser(n, "password1", "", false)
	}
	s := bbs.Defaults()
	s.Name = "Test BBS"
	b := bbs.New(s, st, log.New(&bytes.Buffer{}, "", 0))
	srv := newServer(t)
	c, err := New(b, Settings{Address: srv.addr, ReconnectDelay: 50 * time.Millisecond,
		PingInterval: 200 * time.Millisecond, Sysop: "Sid", Require: require})
	if err != nil {
		t.Fatal(err)
	}
	b.SetRelay(c)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	if hs := <-srv.conns; !strings.HasPrefix(hs, "Test BBS~BBSSEC/") || !strings.HasSuffix(hs, "/"+ProtocolVersion) {
		t.Errorf("handshake %q", hs)
	}
	srv.expect(t, "CLIENT~Test_BBS~~SERVER~~~INFOSYS:Sid~")
	srv.expect(t, "CLIENT~Test_BBS~~SERVER~~~IMALIVE:Test BBS~")
	srv.expect(t, "CLIENT~Test_BBS~~SERVER~~~CAPABILITIES: MCI~")
	return &env{b: b, c: c, server: srv}
}

// node is a node on telnet with the user name, gone when the test ends.
func (e *env) node(t *testing.T, name string) *bbs.Node {
	t.Helper()
	u, _ := e.b.Store.UserByName(name)
	n, err := e.b.Enter(&bbs.Caller{Via: "telnet"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	n.SetUser(u)
	t.Cleanup(n.Leave)
	return n
}

// wait waits for a node's event with want in it.
func wait(t *testing.T, n *bbs.Node, want string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	var seen []string
	for {
		for _, e := range n.Pending() {
			if strings.Contains(e.String(), want) {
				return
			}
			seen = append(seen, e.String())
		}
		select {
		case <-n.Wake():
		case <-deadline:
			t.Fatalf("no %q among %q", want, seen)
		}
	}
}

// TestRelay: callers in MRC's rooms here: their arrivals, lines, emotes,
// commands and moves between rooms sent; others' lines, notices and
// direct messages shown to whom they are for, not our own twice; the
// server's PING answered; a dropped line reconnected, those here back on;
// rooms not MRC's not sent.
func TestRelay(t *testing.T) {
	e := start(t, "")
	srv := e.server
	alice, bob := e.node(t, "alice"), e.node(t, "bob")

	if _, err := alice.JoinRoom("mrc-lobby"); err != nil {
		t.Fatal(err)
	}
	srv.expect(t, "alice~Test_BBS~~SERVER~~~IAMHERE~")
	srv.expect(t, "alice~Test_BBS~~NOTME~~~|07- |11alice |03has arrived.~")
	srv.expect(t, "alice~Test_BBS~~SERVER~~~NEWROOM::lobby~")
	bob.JoinRoom("mrc-lobby")
	srv.expect(t, "bob~Test_BBS~~SERVER~~~NEWROOM::lobby~")

	// said here: to MRC, and to those here as ever
	alice.Say("hello", false)
	srv.expect(t, "alice~Test_BBS~lobby~~~lobby~|03<|11alice|03> |07hello~")
	wait(t, bob, "alice: hello")
	alice.Say("waves", true)
	srv.expect(t, "alice~Test_BBS~lobby~~~lobby~|15* |13alice waves~")

	// said there
	srv.say("carol~Far_BBS~lobby~~~lobby~|03<|14carol|03> |07hi all~")
	wait(t, alice, "carol@Far_BBS: hi all")
	srv.say("carol~Far_BBS~lobby~~~lobby~|15* |13carol dances~")
	wait(t, alice, "* carol@Far_BBS dances")
	srv.say("dave~Far_BBS~~NOTME~~~|07- |11dave |03has arrived.~")
	wait(t, alice, "- dave has arrived.")
	srv.say("SERVER~~~~~lobby~|10dave has joined the room~")
	wait(t, alice, "dave has joined the room")
	srv.say("carol~Far_BBS~lobby~~~other~not here~") // another room: no one here
	// ours, come back from the server, is not shown twice
	srv.say("alice~Test_BBS~lobby~~~lobby~|03<|11alice|03> |07hello~")
	// to alice alone
	srv.say("carol~Far_BBS~lobby~alice~~lobby~|11(|03DirectMSG|11)|07 psst~")
	wait(t, alice, "carol@Far_BBS: (DirectMSG) psst")
	for _, ev := range bob.Pending() {
		if strings.Contains(ev.String(), "psst") || strings.Contains(ev.String(), "not here") {
			t.Errorf("bob got %q", ev)
		}
	}

	// the server's ping, and commands
	srv.say("SERVER~~~CLIENT~~~PING~")
	srv.expect(t, "CLIENT~Test_BBS~~SERVER~~~IMALIVE:Test BBS~")
	alice.Say("!motd", false)
	srv.expect(t, "alice~Test_BBS~lobby~SERVER~~lobby~motd~")
	srv.say("SERVER~~~alice~~lobby~|15Welcome to MRC~")
	wait(t, alice, "MRC: Welcome to MRC")
	alice.Say("!!wow", false)
	srv.expect(t, "alice~Test_BBS~lobby~~~lobby~|03<|11alice|03> |07!wow~")
	srv.expect(t, "alice~Test_BBS~lobby~SERVER~~lobby~IAMHERE~") // each minute (here, less)

	// moving between two of MRC's rooms here
	alice.JoinAlso("mrc-games")
	srv.expect(t, "alice~Test_BBS~lobby~SERVER~~lobby~NEWROOM:lobby:games~")
	alice.SayIn("mrc-lobby", "back", false)
	srv.expect(t, "alice~Test_BBS~games~SERVER~~games~NEWROOM:games:lobby~")
	srv.expect(t, "alice~Test_BBS~lobby~~~lobby~|03<|11alice|03> |07back~")
	alice.Part("mrc-lobby")
	srv.expect(t, "alice~Test_BBS~lobby~SERVER~~lobby~NEWROOM:lobby:games~")
	alice.Part("mrc-games")
	srv.expect(t, "alice~Test_BBS~~NOTME~~~|07- |12alice |04has left chat|08.~")
	srv.expect(t, "alice~Test_BBS~~SERVER~~~LOGOFF~")

	// the line drops: back, with bob still there
	srv.drop()
	<-srv.conns
	srv.expect(t, "bob~Test_BBS~~SERVER~~~IAMHERE~")
	srv.expect(t, "bob~Test_BBS~~SERVER~~~NEWROOM::lobby~")
	srv.say("carol~Far_BBS~lobby~~~lobby~|03<carol|03> |07again~")
	wait(t, bob, "carol@Far_BBS: again")

	// rooms not MRC's are not sent
	bob.JoinRoom("main")
	bob.Say("private here", false)
	srv.expect(t, "bob~Test_BBS~~NOTME~~~|07- |12bob |04has left chat|08.~")
	select {
	case l := <-srv.lines:
		if strings.Contains(l, "private here") {
			t.Errorf("sent %q", l)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

// TestRequire: a user the require does not allow cannot join MRC's rooms,
// only the others.
func TestRequire(t *testing.T) {
	e := start(t, "sysop")
	alice := e.node(t, "alice")
	if _, err := alice.JoinRoom("mrc-lobby"); err != bbs.ErrNotAllowed {
		t.Errorf("join: %v", err)
	}
	if _, err := alice.JoinRoom("main"); err != nil {
		t.Errorf("main: %v", err)
	}
}
