package ircd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

type env struct {
	b    *bbs.BBS
	addr string
	logs *syncBuf
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

// Write appends p, under the lock.
func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

// String is what was written.
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// start is a BBS (alice, bob, Carol Ann, and sysop, all password1) with an
// IRC server listening, stopped when the test ends.
func start(t *testing.T) *env {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, u := range []struct {
		name  string
		sysop bool
	}{{"alice", false}, {"bob", false}, {"Carol Ann", false}, {"sysop", true}} {
		if _, err := st.CreateUser(u.name, "password1", "Here", u.sysop); err != nil {
			t.Fatal(err)
		}
	}
	s := bbs.Defaults()
	s.Name, s.FailDelay, s.MOTD = "Test BBS", 0, "Be nice."
	logs := &syncBuf{}
	b := bbs.New(s, st, log.New(logs, "", 0))
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	srv, err := New(b, Settings{Address: addr, ServerName: "irc.test", MaxClients: 10, MaxPerIP: 10})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		<-done
		if t.Failed() {
			t.Log("log:\n" + logs.String())
		}
	})
	for i := 0; ; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			break
		}
		if i > 100 {
			t.Fatal("never listened")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return &env{b: b, addr: addr, logs: logs}
}

// conn is a raw IRC connection.
type conn struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
}

// dial is a new connection to the server, closed when the test ends.
func (e *env) dial(t *testing.T) *conn {
	t.Helper()
	c, err := net.Dial("tcp", e.addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &conn{t: t, c: c, r: bufio.NewReader(c)}
}

// send writes each line, CRLF ended.
func (c *conn) send(lines ...string) {
	for _, l := range lines {
		c.c.Write([]byte(l + "\r\n"))
	}
}

// expect reads lines until one has want in it; the line.
func (c *conn) expect(want string) string {
	c.t.Helper()
	var seen []string
	for {
		c.c.SetReadDeadline(time.Now().Add(3 * time.Second))
		l, err := c.r.ReadString('\n')
		if err != nil {
			c.t.Fatalf("waiting for %q: %v; got %q", want, err, seen)
		}
		l = strings.TrimRight(l, "\r\n")
		if strings.Contains(l, want) {
			return l
		}
		seen = append(seen, l)
	}
}

// login registers with PASS name:password1, NICK nick and USER, and reads
// through the MOTD.
func (c *conn) login(name, nick string) {
	c.t.Helper()
	c.send("PASS "+name+":password1", "NICK "+nick, "USER u 0 * :Someone")
	c.expect(" 001 ")
	c.expect(" 376 ") // the end of the MOTD
}

// caller is a BBS caller on a terminal's node.
func (e *env) caller(t *testing.T, name string) *bbs.Node {
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

// events a node was given that waitEvent has not looked for yet
var (
	leftMu sync.Mutex
	left   = map[*bbs.Node][]bbs.Event{}
)

// waitEvent waits for an event of the node with want in it.
func waitEvent(t *testing.T, n *bbs.Node, want string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	var seen []string
	for {
		leftMu.Lock()
		es := append(left[n], n.Pending()...)
		for i, e := range es {
			if strings.Contains(e.String(), want) {
				left[n] = es[i+1:]
				leftMu.Unlock()
				return
			}
			seen = append(seen, e.String())
		}
		left[n] = nil
		leftMu.Unlock()
		select {
		case <-n.Wake():
		case <-deadline:
			t.Fatalf("no %q among %q", want, seen)
		}
	}
}

// TestLoginRefused: no PASS, or a wrong one, closes the connection (one
// failed login logged); a command before registering is 451.
func TestLoginRefused(t *testing.T) {
	e := start(t)
	c := e.dial(t)
	c.send("NICK alice", "USER u 0 * :x")
	c.expect(" 464 ")
	c.expect("ERROR")
	c = e.dial(t)
	c.send("PASS wrongpassword", "NICK alice", "USER u 0 * :x")
	c.expect(" 464 ")
	c.send("PRIVMSG #main :hi") // gone
	c = e.dial(t)
	c.send("JOIN #main")
	c.expect(" 451 ") // not registered
	if strings.Count(e.logs.String(), "failed login") != 1 {
		t.Errorf("log:\n%s", e.logs)
	}
}

// TestChatWithCallers: an IRC client and a BBS caller in a room: CAP and
// server-time, joins with the room's last lines, messages and actions
// both ways, private messages, WHO, WHOIS, LIST, refused nicks and rooms,
// PART, and the node gone with the connection.
func TestChatWithCallers(t *testing.T) {
	e := start(t)
	bob := e.caller(t, "bob")
	if _, err := bob.JoinRoom("main"); err != nil {
		t.Fatal(err)
	}
	bob.Say("before", false)

	c := e.dial(t)
	c.send("CAP LS 302")
	c.expect("CAP * LS :echo-message multi-prefix sasl=PLAIN server-time userhost-in-names")
	c.send("CAP REQ :server-time", "CAP END")
	c.expect("ACK server-time")
	c.login("alice", "alice")
	c.expect("MODE alice +i")

	c.send("JOIN #main,#Lounge")
	c.expect(":alice!irc@node2 JOIN #main")
	c.expect("353 alice = #main :")
	c.expect("366 alice #main")
	c.expect("PRIVMSG #main :before") // the room's last lines
	c.expect(":alice!irc@node2 JOIN #lounge")
	waitEvent(t, bob, "-> alice came in")

	bob.Say("hello from telnet", false)
	l := c.expect(":bob!telnet@node1 PRIVMSG #main :hello from telnet")
	if !strings.HasPrefix(l, "@time=") {
		t.Errorf("no time: %q", l)
	}
	c.send("PRIVMSG #main :hi \x02bob\x02", "PRIVMSG #main :\x01ACTION waves\x01")
	waitEvent(t, bob, "alice: hi bob")
	waitEvent(t, bob, "* alice waves")

	// private messages both ways
	c.send("PRIVMSG Bob :psst")
	waitEvent(t, bob, "Message from alice (node 2): psst")
	bob.Message(2, "hey you")
	c.expect(":bob!telnet@node1 PRIVMSG alice :hey you")
	c.send("PRIVMSG Alice :note to self") // no other node: it comes back
	c.expect(":alice!irc@node2 PRIVMSG Alice :note to self")

	c.send("PRIVMSG #nowhere :x", "PRIVMSG nobody :x")
	c.expect(" 404 alice #nowhere ")
	c.expect(" 401 alice nobody ")

	c.send("WHO #main")
	c.expect("352 alice #main telnet node1 irc.test bob H :0 bob")
	c.expect("315 alice #main")
	c.send("WHOIS bob")
	c.expect("311 alice bob telnet node1 * :bob, Here")
	c.expect("318 alice bob")
	c.send("LIST")
	c.expect("322 alice #lounge 1")
	c.expect("323 alice")
	c.send("NICK other")
	c.expect(" 432 alice other ")
	c.send("JOIN #bad.name", "JOIN #sysop-1")
	c.expect(" 476 alice #bad.name ")
	c.expect(" 473 alice #sysop-1 ")

	c.send("PART #main")
	c.expect(":alice!irc@node2 PART #main")
	waitEvent(t, bob, "<- alice left")
	bob.Say("after", false)
	c.send("PING :x")
	if l := c.expect("PONG"); strings.Contains(l, "after") {
		t.Error("a line after the part")
	}

	// the node goes with the connection
	c.send("QUIT :bye")
	c.expect("ERROR")
	for i := 0; ; i++ {
		if len(e.b.Nodes()) == 1 {
			break
		}
		if i > 100 {
			t.Fatalf("nodes: %v", e.b.Nodes())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestNickAndPages: a name with a space gets its nick changed to fit; a
// sysop joining #sysop-N answers that caller's page.
func TestNickAndPages(t *testing.T) {
	e := start(t)
	c := e.dial(t)
	c.send("PASS Carol Ann:password1", "NICK carol", "USER u 0 * :x")
	c.expect(":carol!irc@irc.test NICK Carol_Ann")
	c.expect(" 001 Carol_Ann ")

	s := e.dial(t)
	s.login("sysop", "sysop")
	pager := e.caller(t, "bob")
	if _, _, err := pager.PageSysop("help me"); err != nil {
		t.Fatal(err)
	}
	s.expect("NOTICE sysop :bob (node 3) is paging you: help me (/join #sysop-3 to answer)")
	s.send("JOIN #sysop-3")
	s.expect("JOIN #sysop-3")
	waitEvent(t, pager, "sysop answered your page")
	if len(e.b.Pages()) != 0 {
		t.Errorf("pages %v", e.b.Pages())
	}
}

// irc-go's own client, as a program would use it: SASL, echo-message
func TestIrcEventClient(t *testing.T) {
	e := start(t)
	got := make(chan string, 10)
	irc := &ircevent.Connection{Server: e.addr, Nick: "alice", User: "alice", RealName: "Alice",
		UseSASL: true, SASLLogin: "alice", SASLPassword: "password1",
		RequestCaps: []string{"server-time", "echo-message"}, Timeout: 5 * time.Second}
	irc.AddConnectCallback(func(ircmsg.Message) { irc.Join("#main") })
	irc.AddCallback("JOIN", func(m ircmsg.Message) {
		if m.Nick() == irc.CurrentNick() {
			got <- "join " + m.Params[0]
		}
	})
	irc.AddCallback("PRIVMSG", func(m ircmsg.Message) { got <- m.Nick() + ": " + m.Params[1] })
	if err := irc.Connect(); err != nil {
		t.Fatal(err)
	}
	go irc.Loop()
	defer irc.Quit()
	wait := func(want string) {
		t.Helper()
		select {
		case g := <-got:
			if g != want {
				t.Fatalf("got %q, want %q", g, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no %q", want)
		}
	}
	wait("join #main")
	bob := e.caller(t, "bob")
	bob.JoinRoom("main")
	bob.Say("hi alice", false)
	wait("bob: hi alice")
	irc.Privmsg("#main", "hi bob")
	wait("alice: hi bob") // echoed
	waitEvent(t, bob, "alice: hi bob")
	if !strings.Contains(e.logs.String(), "alice on IRC") {
		t.Errorf("log:\n%s", e.logs)
	}
}

// TestSASLRefused: SASL with a wrong password does not connect.
func TestSASLRefused(t *testing.T) {
	e := start(t)
	irc := &ircevent.Connection{Server: e.addr, Nick: "alice", User: "alice", UseSASL: true,
		SASLLogin: "alice", SASLPassword: "wrong", Timeout: 5 * time.Second}
	if err := irc.Connect(); err == nil {
		irc.Quit()
		t.Fatal("a wrong password worked")
	}
}

// TestNick: a BBS name's nick, its spaces and dots made underscores.
func TestNick(t *testing.T) {
	for in, want := range map[string]string{"alice": "alice", "Carol Ann": "Carol_Ann", "J.R. Bob": "J_R__Bob"} {
		if got := Nick(in); got != want {
			t.Errorf("Nick(%q) = %q", in, got)
		}
	}
}

// TestTLS: a login over TLS, with a certificate made here; a certificate
// file that is not there is refused.
func TestTLS(t *testing.T) {
	dir := t.TempDir()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "irc.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	kd, _ := x509.MarshalECPrivateKey(key)
	cert, keyf := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(keyf, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0600)

	e := start(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	srv, err := New(e.b, Settings{TLSAddress: addr, CertFile: cert, KeyFile: keyf})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx)
	var tc *tls.Conn
	for i := 0; ; i++ {
		if tc, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}); err == nil {
			break
		}
		if i > 100 {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer tc.Close()
	c := &conn{t: t, c: tc, r: bufio.NewReader(tc)}
	c.login("bob", "bob")
	if _, err := New(e.b, Settings{TLSAddress: addr, CertFile: "/nonexistent", KeyFile: keyf}); err == nil {
		t.Error("a missing certificate taken")
	}
}

// TestBadNick: a nick with a space or ! is 432, and registration goes on.
func TestBadNick(t *testing.T) {
	e := start(t)
	c := e.dial(t)
	c.send("NICK :a b", "NICK a!b")
	c.expect(" 432 * * :Erroneous")
	c.expect(" 432 * * :Erroneous")
	c.send("PASS alice:password1", "NICK alice", "USER u 0 * :x")
	c.expect(" 001 alice ")
}

// lines from a chat network (MRC) in a channel
func TestRelayedLines(t *testing.T) {
	e := start(t)
	c := e.dial(t)
	c.login("alice", "alice")
	c.send("JOIN #mrc-lobby")
	c.expect("366 alice #mrc-lobby")
	e.b.RelayEvent(bbs.Event{Kind: bbs.Said, Room: "mrc-lobby", From: "carol@Far_BBS", Text: "hi all"})
	c.expect(":carol_Far_BBS!mrc@far_bbs PRIVMSG #mrc-lobby :hi all")
	e.b.RelayEvent(bbs.Event{Kind: bbs.Emoted, Room: "mrc-lobby", From: "carol@Far_BBS", Text: "dances"})
	c.expect(":carol_Far_BBS!mrc@far_bbs PRIVMSG #mrc-lobby :\x01ACTION dances\x01")
	e.b.RelayEvent(bbs.Event{Kind: bbs.Notice, Room: "mrc-lobby", Text: "- dave has arrived."})
	c.expect(":irc.test NOTICE #mrc-lobby :- dave has arrived.")
}

// network is a linked network as the BBS's relays see one: dave in its
// rooms (net-), the lines sent to him kept.
type network struct {
	mu   sync.Mutex
	sent []string
}

func (n *network) Carries(room string) bool                 { return strings.HasPrefix(room, "net-") }
func (n *network) Allows(room string, u *store.User) bool   { return true }
func (n *network) Joined(room, user string)                 {}
func (n *network) Left(room, user string)                   {}
func (n *network) Said(room, user, text string, emote bool) {}
func (n *network) Command(room, user, line string)          {}
func (n *network) Members(room string) []bbs.RoomMember {
	return []bbs.RoomMember{{Name: "dave", Via: "~dv", Host: "far.example"}}
}
func (n *network) Lookup(nick string) (bbs.RemoteUser, bool) {
	if !strings.EqualFold(nick, "dave") {
		return bbs.RemoteUser{}, false
	}
	return bbs.RemoteUser{Nick: "dave", User: "~dv", Host: "far.example", Server: "hub.far.example",
		Network: "FarNet", Real: "Dave", Account: "dave"}, true
}
func (n *network) SendPrivate(from, to, text string, notice, emote bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, fmt.Sprintf("%s>%s %v %v: %s", from, to, notice, emote, text))
	return nil
}

// TestLinkedNetwork: a linked network's users in a room's NAMES and WHO,
// in WHOIS and ISON; PRIVMSGs to them (an ACTION too) sent by the relay;
// and their lines named as the network names them.
func TestLinkedNetwork(t *testing.T) {
	e := start(t)
	net := &network{}
	e.b.AddRelay(net)
	c := e.dial(t)
	c.login("alice", "alice")
	c.send("JOIN #net-retro")
	if l := c.expect(" 353 "); !strings.Contains(l, "dave") {
		t.Errorf("names %q", l)
	}
	c.send("WHO #net-retro")
	c.expect(" 352 alice #net-retro ~dv far.example irc.test dave H")
	c.send("WHOIS dave")
	c.expect(" 311 alice dave ~dv far.example * :Dave")
	c.expect(" 312 alice dave hub.far.example :on FarNet")
	c.expect(" 330 alice dave dave")
	c.send("ISON dave nobody")
	c.expect(" 303 alice :dave")
	c.send("PRIVMSG dave :hello", "PRIVMSG dave :\x01ACTION waves\x01", "PRIVMSG nobody :hm")
	c.expect(" 401 alice nobody")
	net.mu.Lock()
	sent := strings.Join(net.sent, "|")
	net.mu.Unlock()
	if sent != "alice>dave false false: hello|alice>dave false true: waves" {
		t.Errorf("sent %q", sent)
	}
	e.b.RelayEvent(bbs.Event{Kind: bbs.Said, Room: "net-retro", From: "dave", Mask: "dave!~dv@far.example", Text: "hi"})
	c.expect(":dave!~dv@far.example PRIVMSG #net-retro :hi")
	e.b.RelayPrivate("alice", bbs.Event{From: "dave", Mask: "dave!~dv@far.example", Text: "psst"})
	c.expect(":dave!~dv@far.example PRIVMSG alice :psst")
}

// TestKeptMessages: a PRIVMSG to a user not on is kept (301 says so), and
// given them when they come, by IRC (its time as server-time) or the
// terminal; a terminal's /msg by name, and the conversation read back.
func TestKeptMessages(t *testing.T) {
	e := start(t)
	a := e.dial(t)
	a.login("alice", "alice")
	a.send("PRIVMSG bob :are you there?")
	if l := a.expect(" 301 "); !strings.Contains(l, "bob") {
		t.Errorf("301 %q", l)
	}
	a.send("PRIVMSG nobody :hello")
	a.expect(" 401 ")

	// bob comes on a terminal, then talks back by /msg and his name
	bob := e.caller(t, "bob")
	bob.DeliverKept()
	waitEvent(t, bob, "Message from alice")
	if out, _, err := bob.ChatCommand("/msg alice yes, here"); err != nil || len(out) != 1 || out[0] != "Sent to alice." {
		t.Errorf("/msg %q %v", out, err)
	}
	a.expect("PRIVMSG alice :yes, here")
	if out, _, _ := bob.ChatCommand("/history alice"); len(out) != 3 || !strings.HasSuffix(out[1], "<alice> are you there?") {
		t.Errorf("/history %q", out)
	}

	// Carol Ann, on IRC later: a line kept for her has its time
	a.send("PRIVMSG Carol_Ann :for later")
	a.expect(" 301 ")
	c := e.dial(t)
	c.send("CAP REQ :server-time", "CAP END")
	c.login("Carol Ann", "Carol_Ann")
	if l := c.expect("PRIVMSG Carol_Ann :for later"); !strings.HasPrefix(l, "@time=") {
		t.Errorf("kept line %q", l)
	}
}

// TestFriends: BBSServ FRIENDS adds, lists (who is on) and removes; the
// terminal's /friends shows the same list.
func TestFriends(t *testing.T) {
	e := start(t)
	a := e.dial(t)
	a.login("alice", "alice")
	a.send("PRIVMSG BBSServ :FRIENDS ADD bob")
	a.expect("bob is on your friends list")
	a.send("PRIVMSG BBSServ :FRIENDS ADD nobody")
	a.expect("There is no user of that name")
	e.caller(t, "bob")
	a.send("PRIVMSG BBSServ :FRIENDS")
	a.expect("bob  on (telnet)")
	n := e.caller(t, "alice")
	if out, _, _ := n.ChatCommand("/friends"); len(out) != 2 || !strings.Contains(out[1], "bob") {
		t.Errorf("/friends %q", out)
	}
	a.send("PRIVMSG BBSServ :FRIENDS DEL bob")
	a.expect("bob is off your friends list")
	if out, _, _ := n.ChatCommand("/friends"); out[0] != "Your friends list is empty." {
		t.Errorf("/friends after %q", out)
	}
}
