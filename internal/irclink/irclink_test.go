package irclink

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// peerServer is a TS6 server the hub links out to, as a test drives it.
type peerServer struct {
	t     *testing.T
	ln    net.Listener
	conn  net.Conn
	lines chan string
	seen  []string
}

// newPeer listens for the hub's link.
func newPeer(t *testing.T) *peerServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return &peerServer{t: t, ln: ln, lines: make(chan string, 1000)}
}

// accept takes the hub's link and reads its lines.
func (p *peerServer) accept() {
	p.t.Helper()
	p.ln.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
	c, err := p.ln.Accept()
	if err != nil {
		p.t.Fatal(err)
	}
	p.conn = c
	p.t.Cleanup(func() { c.Close() })
	go func() {
		r := bufio.NewScanner(c)
		for r.Scan() {
			p.lines <- strings.TrimRight(r.Text(), "\r")
		}
		close(p.lines)
	}()
}

// say sends lines to the hub.
func (p *peerServer) say(lines ...string) {
	for _, l := range lines {
		p.conn.Write([]byte(l + "\r\n"))
	}
}

// expect waits for a line from the hub with every one of wants in it.
func (p *peerServer) expect(wants ...string) string {
	p.t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case l, ok := <-p.lines:
			if !ok {
				p.t.Fatalf("link closed; no %q among %q", wants, p.seen)
			}
			p.seen = append(p.seen, l)
			all := true
			for _, w := range wants {
				if !strings.Contains(l, w) {
					all = false
				}
			}
			if all {
				return l
			}
		case <-deadline:
			p.t.Fatalf("no %q among %q", wants, p.seen)
		}
	}
}

// link is the TS6 handshake from the peer's side, with its burst.
func (p *peerServer) link(name, sid string, burst ...string) {
	p.t.Helper()
	p.accept()
	p.expect("PASS", "sendpw", "TS", "6", "1BB")
	p.expect("CAPAB", "EUID")
	p.expect("SERVER", "irc.test.example", "1")
	p.expect("PING")
	p.say("PASS acceptpw TS 6 :"+sid, "CAPAB :QS EX IE ENCAP TB EUID SAVE", "SERVER "+name+" 1 :a test server",
		"SVINFO 6 6 0 :1700000000")
	p.say(burst...)
	p.say(":" + sid + " PONG " + name + " :irc.test.example")
}

type env struct {
	b   *bbs.BBS
	hub *Hub
}

// start is a BBS (alice and bob) with its hub, linking out to peers.
func start(t *testing.T, peers ...*peerServer) *env {
	t.Helper()
	return startWith(t, nil, peers...)
}

// startWith is start, the settings changed first by change.
func startWith(t *testing.T, change func(*Settings), peers ...*peerServer) *env {
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
	set := Settings{ServerName: "irc.test.example", SID: "1BB", Network: "TestNet", Prefix: "irc-",
		Retry: 100 * time.Millisecond}
	names := []string{"far.example", "other.example"}
	for i, p := range peers {
		set.Peers = append(set.Peers, Peer{Name: names[i], Address: p.ln.Addr().String(),
			SendPassword: "sendpw", AcceptPassword: "acceptpw"})
	}
	if change != nil {
		change(&set)
	}
	h, err := New(b, set)
	if err != nil {
		t.Fatal(err)
	}
	b.AddRelay(h)
	return &env{b: b, hub: h}
}

// run runs the hub until the test ends.
func (e *env) run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.hub.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

// node is a node on telnet with the user name.
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

// held are events taken from a node and not yet waited for.
var held = map[*bbs.Node][]bbs.Event{}

// wait waits for a node's event with want in it; it is the event.
func wait(t *testing.T, n *bbs.Node, want string) bbs.Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	var seen []string
	for {
		evs := append(held[n], n.Pending()...)
		held[n] = nil
		for i, e := range evs {
			if strings.Contains(e.String(), want) {
				held[n] = evs[i+1:]
				return e
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

// TestLink: the handshake; a BBS user whose nick the peer has renamed
// Nick|Tag before they are sent; the rooms and channels as one (joins,
// talk, emotes, members, the other's name beside a BBS user's); private
// messages both ways; WHOIS of a BBS user from the network; one coming on
// the BBS later put on the network.
func TestLink(t *testing.T) {
	p := newPeer(t)
	e := start(t, p)
	alice := e.node(t, "alice")
	e.run(t)
	p.link("far.example", "42X",
		":42X UID alice 1 100 +i al far.host 0 42XAAAAAA :Alice of far",
		":42X UID carol 1 100 +i ca far.host 0 42XAAAAAB :Carol",
		":42X SJOIN 100 #retro +nt :@42XAAAAAA 42XAAAAAB")
	p.expect("EUID alice|Test 1", "+i bbs irc.test.example 0 1BBAAAAAA * alice :alice")
	p.expect(":1BB PING irc.test.example :42X")

	// the room is the channel
	if _, err := alice.JoinRoom("irc-retro"); err != nil {
		t.Fatal(err)
	}
	p.expect(":1BBAAAAAA JOIN 100 #retro +")
	names := map[string]bool{}
	for _, m := range e.b.RoomMembers("irc-retro") {
		names[m.Name] = true
	}
	if !names["alice"] || !names["alice|far"] || !names["carol"] {
		t.Errorf("members %v", names)
	}
	p.say(":42XAAAAAB PRIVMSG #retro :hi there", ":42XAAAAAA PRIVMSG #retro :\x01ACTION waves\x01")
	if ev := wait(t, alice, "carol: hi there"); ev.Mask != "carol!ca@far.host" {
		t.Errorf("mask %q", ev.Mask)
	}
	wait(t, alice, "* alice|far waves")
	if err := alice.Say("hello all", false); err != nil {
		t.Fatal(err)
	}
	p.expect(":1BBAAAAAA PRIVMSG #retro :hello all")
	if err := alice.Say("dances", true); err != nil {
		t.Fatal(err)
	}
	p.expect(":1BBAAAAAA PRIVMSG #retro :\x01ACTION dances\x01")

	// private messages
	p.say(":42XAAAAAB PRIVMSG 1BBAAAAAA :psst")
	wait(t, alice, "carol")
	if err := alice.MessageRemote("carol", "hi carol", false, false); err != nil {
		t.Fatal(err)
	}
	p.expect(":1BBAAAAAA PRIVMSG 42XAAAAAB :hi carol")
	if err := alice.MessageRemote("alice|far", "hello me", false, false); err != nil {
		t.Fatal(err)
	}
	p.expect(":1BBAAAAAA PRIVMSG 42XAAAAAA :hello me")
	if r, ok := e.b.LookupRemote("carol"); !ok || r.Server != "far.example" || r.Host != "far.host" {
		t.Errorf("lookup %+v %v", r, ok)
	}

	// WHOIS from the network
	p.say(":42XAAAAAB WHOIS 1BB :alice|Test")
	p.expect(":1BB 311 42XAAAAAB alice|Test bbs irc.test.example *")
	p.expect(":1BB 319 42XAAAAAB alice|Test :#retro")
	p.expect(":1BB 318 42XAAAAAB alice|Test")

	// a nick change to one of ours: ours is renamed
	bob := e.node(t, "bob")
	p.expect("EUID bob 1", "1BBAAAAAB")
	p.say(":42XAAAAAB NICK bob :200")
	p.expect(":1BBAAAAAB NICK bob|Test")
	bob.Leave()
	p.expect(":1BBAAAAAB QUIT :Signed off")

	// a remote user leaves the channel: the room is told
	p.say(":42XAAAAAB PART #retro :bye")
	wait(t, alice, "<- bob left") // carol is bob now, and the BBS's bob is gone: bob as is
}

// TestHub: a second server is given the first's servers, users and
// channels; lines between them go through; a split of one is told to the
// other; a collision between their users is settled by TS.
func TestHub(t *testing.T) {
	p1, p2 := newPeer(t), newPeer(t)
	e := start(t, p1, p2)
	e.run(t)
	p1.link("far.example", "42X",
		":42X SID leaf.example 2 42Y :behind far",
		":42Y UID dave 2 100 +i da leaf.host 0 42YAAAAAA :Dave",
		":42X UID erin 1 100 +i er far.host 0 42XAAAAAA :Erin",
		":42X SJOIN 100 #retro +nt :@42XAAAAAA 42YAAAAAA")
	p1.expect(":1BB PING irc.test.example :42X")
	p2.link("other.example", "43Z",
		":43Z UID erin 1 50 +i er2 other.host 0 43ZAAAAAA :Older Erin",
		":43Z UID frank 1 100 +i fr other.host 0 43ZAAAAAB :Frank")
	// p2 gets the network
	p2.expect(":1BB SID far.example 2 42X")
	p2.expect(":42X SID leaf.example 3 42Y")
	p2.expect("EUID dave", "42YAAAAAA")
	p2.expect(":1BB SJOIN 100 #retro +nt", "42YAAAAAA")
	// the newer erin (far's) was killed, everywhere
	p1.expect(":1BB KILL 42XAAAAAA")
	p1.expect(":43Z UID erin 1 50")
	p1.expect(":43Z UID frank")
	// lines go through
	p2.say(":43ZAAAAAB PRIVMSG 42YAAAAAA :hi dave")
	p1.expect(":43ZAAAAAB PRIVMSG 42YAAAAAA :hi dave")
	p1.say(":42YAAAAAA PRIVMSG #retro :hi all")
	p2.expect(":42YAAAAAA PRIVMSG #retro :hi all")
	// a split
	p1.conn.Close()
	p2.expect(":1BB SQUIT 42X")
	if d, n := e.hub.Links(); d != 1 || n != 1 {
		t.Errorf("links %d, %d", d, n)
	}
	if _, ok := e.b.LookupRemote("dave"); ok {
		t.Error("dave is still on")
	}
}

// TestRefused: a bad password, and a server not configured, are refused.
func TestRefused(t *testing.T) {
	p := newPeer(t)
	e := start(t, p)
	e.run(t)
	p.accept()
	p.expect("PING")
	p.say("PASS wrong TS 6 :42X", "CAPAB :QS ENCAP", "SERVER far.example 1 :x")
	p.expect("ERROR", "bad password")
}

// TestTagged: Nick|Tag cut to the nick length, then numbered.
func TestTagged(t *testing.T) {
	e := start(t)
	h := e.hub
	h.S.NickLen = 9
	if n := h.tagged("alexander", h.me, nil, ""); n != "alex|Test" {
		t.Errorf("tagged %q", n)
	}
	h.nicks[fold("alex|Test")] = &user{}
	if n := h.tagged("alexander", h.me, nil, ""); n != "ale|Test2" {
		t.Errorf("numbered %q", n)
	}
	if fold("Nick[A]~") != "nick{a}^" {
		t.Error("rfc1459 folding")
	}
}
