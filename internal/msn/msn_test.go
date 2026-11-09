package msn

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/ircd"
	"github.com/D-Modem/bbs/internal/store"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type env struct {
	b       *bbs.BBS
	srv     *Server
	addr    string // MSN's
	ircAddr string
}

// freeAddr is a loopback address free to listen on.
func freeAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// start is a BBS (alice, bob, Carol Ann and sysop, all password1; old,
// made before MSN digests were kept) with MSN and IRC servers.
func start(t *testing.T) *env {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.CreateUser("old", "password1", "", false); err != nil {
		t.Fatal(err)
	}
	st.SetKeepMSN(true)
	for _, name := range []string{"alice", "bob", "Carol Ann", "sysop"} {
		if _, err := st.CreateUser(name, "password1", "Here", name == "sysop"); err != nil {
			t.Fatal(err)
		}
	}
	s := bbs.Defaults()
	s.Name, s.FailDelay = "Test BBS", 0
	logs := &syncBuf{}
	b := bbs.New(s, st, log.New(logs, "", 0))
	e := &env{b: b, addr: freeAddr(t), ircAddr: freeAddr(t)}
	srv, err := New(b, Settings{Address: e.addr, Domain: "bbs.test", MaxPerIP: 50})
	if err != nil {
		t.Fatal(err)
	}
	e.srv = srv
	is, err := ircd.New(b, ircd.Settings{Address: e.ircAddr, ServerName: "irc.test", MaxClients: 10, MaxPerIP: 10})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { srv.Run(ctx); wg.Done() }()
	go func() { is.Run(ctx); wg.Done() }()
	t.Cleanup(func() {
		cancel()
		wg.Wait()
		if t.Failed() {
			t.Log("log:\n" + logs.String())
		}
	})
	for _, a := range []string{e.addr, e.ircAddr} {
		for i := 0; ; i++ {
			if c, err := net.Dial("tcp", a); err == nil {
				c.Close()
				break
			}
			if i > 100 {
				t.Fatal("never listened")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return e
}

// client is a scripted MSN client's connection (to the notification
// server or a switchboard).
type client struct {
	t    *testing.T
	c    net.Conn
	r    *bufio.Reader
	trid int
}

func dial(t *testing.T, addr string) *client {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &client{t: t, c: c, r: bufio.NewReader(c)}
}

// cmd sends a command with the next transaction id, which it gives.
func (cl *client) cmd(name string, args ...string) string {
	cl.trid++
	id := strconv.Itoa(cl.trid)
	cl.c.Write([]byte(strings.Join(append([]string{name, id}, args...), " ") + "\r\n"))
	return id
}

// msg sends a MSG with a text payload.
func (cl *client) msg(ack, text string) string {
	cl.trid++
	id := strconv.Itoa(cl.trid)
	p := "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + text
	fmt.Fprintf(cl.c, "MSG %s %s %d\r\n%s", id, ack, len(p), p)
	return id
}

// line is the next line the server sends (and its payload, for a MSG).
func (cl *client) line() (string, string, error) {
	cl.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	l, err := cl.r.ReadString('\n')
	if err != nil {
		return "", "", err
	}
	l = strings.TrimRight(l, "\r\n")
	f := strings.Fields(l)
	if len(f) > 0 && (f[0] == "MSG" || f[0] == "NOT") {
		n, _ := strconv.Atoi(f[len(f)-1])
		p := make([]byte, n)
		if _, err := readFull(cl.r, p); err != nil {
			return "", "", err
		}
		return l, string(p), nil
	}
	return l, "", nil
}

// expect reads lines until one beginning with want; it and its payload.
func (cl *client) expect(want string) (string, string) {
	cl.t.Helper()
	var seen []string
	for {
		l, p, err := cl.line()
		if err != nil {
			cl.t.Fatalf("waiting for %q: %v; saw %q", want, err, seen)
		}
		if strings.HasPrefix(l, want) {
			return l, p
		}
		seen = append(seen, l)
	}
}

// quiet: nothing beginning with any of these comes for a moment.
func (cl *client) quiet(not ...string) {
	cl.t.Helper()
	cl.c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		l, _, err := cl.line()
		if err != nil {
			return
		}
		for _, n := range not {
			if strings.HasPrefix(l, n) {
				cl.t.Errorf("unwanted %q", l)
			}
		}
	}
}

func digest(salt, pw string) string {
	h := md5.Sum([]byte(salt + pw))
	return hex.EncodeToString(h[:])
}

// signIn signs a client in by MD5 at MSNP ver; the USR's answer.
func signIn(t *testing.T, addr, handle, pw string, ver int) (*client, string) {
	t.Helper()
	cl := dial(t, addr)
	cl.cmd("VER", fmt.Sprintf("MSNP%d", ver), "CVR0")
	if l, _ := cl.expect("VER "); l != fmt.Sprintf("VER 1 MSNP%d", ver) {
		t.Fatalf("version %q", l)
	}
	cl.cmd("INF")
	cl.expect("INF 2 MD5")
	cl.cmd("USR", "MD5", "I", handle)
	l, _ := cl.expect("USR 3 MD5 S ")
	salt := strings.Fields(l)[4]
	cl.cmd("USR", "MD5", "S", digest(salt, pw))
	l, _, err := cl.line()
	if err != nil {
		t.Fatal(err)
	}
	return cl, l
}

// online signs a client in at MSNP7, takes its lists and sets it online.
func online(t *testing.T, addr, name string) *client {
	t.Helper()
	cl, l := signIn(t, addr, name+"@bbs.test", "password1", 7)
	if !strings.HasPrefix(l, "USR 4 OK ") {
		t.Fatalf("%s: %q", name, l)
	}
	cl.expect("MSG Hotmail Hotmail ")
	cl.cmd("SYN", "0")
	cl.expect("LST 5 RL ")
	cl.cmd("CHG", "NLN")
	cl.expect("CHG 6 NLN")
	return cl
}

// switchboard has cl ask for a conversation and join it.
func (cl *client) switchboard(t *testing.T, handle string) *client {
	t.Helper()
	cl.cmd("XFR", "SB")
	l, _ := cl.expect("XFR ")
	f := strings.Fields(l)
	if len(f) != 6 || f[2] != "SB" || f[4] != "CKI" {
		t.Fatalf("xfr %q", l)
	}
	sc := dial(t, f[3])
	sc.cmd("USR", handle, f[5])
	sc.expect("USR 1 OK " + handle + " ")
	return sc
}

// answer has cl answer a ring (an RNG line) as handle; its switchboard
// connection, and the IRO lines it was told.
func answer(t *testing.T, rng, handle string) (*client, []string) {
	t.Helper()
	f := strings.Fields(rng)
	if len(f) < 7 || f[0] != "RNG" || f[3] != "CKI" {
		t.Fatalf("rng %q", rng)
	}
	sc := dial(t, f[2])
	sc.cmd("ANS", handle, f[4], f[1])
	var iro []string
	for {
		l, _ := sc.expect("")
		if l == "ANS 1 OK" {
			return sc, iro
		}
		if !strings.HasPrefix(l, "IRO 1 ") {
			t.Fatalf("answering: %q", l)
		}
		iro = append(iro, l)
	}
}

type ircConn struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
}

func ircLogin(t *testing.T, addr, name string) *ircConn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ic := &ircConn{t: t, c: c, r: bufio.NewReader(c)}
	ic.send("PASS "+name+":password1", "NICK "+ircd.Nick(name), "USER u 0 * :"+name)
	ic.expect(" 001 ")
	return ic
}

func (ic *ircConn) send(lines ...string) {
	for _, l := range lines {
		ic.c.Write([]byte(l + "\r\n"))
	}
}

func (ic *ircConn) expect(want string) string {
	ic.t.Helper()
	var seen []string
	for {
		ic.c.SetReadDeadline(time.Now().Add(3 * time.Second))
		l, err := ic.r.ReadString('\n')
		if err != nil {
			ic.t.Fatalf("waiting for %q: %v; saw %q", want, err, seen)
		}
		if strings.Contains(l, want) {
			return l
		}
		seen = append(seen, strings.TrimSpace(l))
	}
}

func (e *env) user(t *testing.T, name string) *store.User {
	t.Helper()
	u, err := e.b.Store.UserByName(name)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// TestSignIn: an MD5 sign-in (the handle at any domain, the name with or
// without its spaces), its profile and lists; a wrong password, an
// unknown handle, a version not served; an account from before digests
// were kept, until its password is used in the clear; MSN 1.0's CTP.
func TestSignIn(t *testing.T) {
	e := start(t)
	cl, l := signIn(t, e.addr, "alice@hotmail.com", "password1", 7)
	if l != "USR 4 OK alice@hotmail.com alice 1" {
		t.Errorf("signed in: %q", l)
	}
	if _, p := cl.expect("MSG Hotmail Hotmail "); !strings.Contains(p, "text/x-msmsgsprofile") {
		t.Errorf("profile %q", p)
	}
	cl.cmd("SYN", "41")
	want := []string{"SYN 5 42", "GTC 5 42 A", "BLP 5 42 AL", "LSG 5 42 1 1 0 Other%20Contacts 0",
		"LST 5 FL 42 0 0", "LST 5 AL 42 0 0", "LST 5 BL 42 0 0", "LST 5 RL 42 0 0"}
	for _, w := range want {
		if l, _ := cl.expect(""); l != w {
			t.Errorf("got %q, want %q", l, w)
		}
	}

	_, l = signIn(t, e.addr, "CarolAnn@bbs.test", "password1", 6)
	if l != "USR 4 OK CarolAnn@bbs.test Carol%20Ann 1" {
		t.Errorf("carol: %q", l)
	}
	if _, l = signIn(t, e.addr, "bob@bbs.test", "wrong", 5); l != "911 4" {
		t.Errorf("wrong password: %q", l)
	}
	if _, l = signIn(t, e.addr, "nobody@bbs.test", "password1", 5); l != "911 4" {
		t.Errorf("unknown: %q", l)
	}
	nc := dial(t, e.addr)
	nc.cmd("VER", "MSNP8", "CVR0")
	if l, _ := nc.expect("VER "); l != "VER 1 0" {
		t.Errorf("MSNP8: %q", l)
	}

	if _, l = signIn(t, e.addr, "old@bbs.test", "password1", 7); l != "911 4" {
		t.Errorf("old, no digest: %q", l)
	}
	if _, err := e.b.Store.CheckPassword("old", "password1"); err != nil {
		t.Fatal(err)
	}
	if _, l = signIn(t, e.addr, "old@bbs.test", "password1", 7); !strings.HasPrefix(l, "USR 4 OK old@bbs.test old") {
		t.Errorf("old, with its digest: %q", l)
	}

	// MSN 1.0: the password in the clear (MSNP2 only)
	cc := dial(t, e.addr)
	cc.cmd("VER", "MSNP2")
	cc.expect("VER 1 MSNP2")
	cc.cmd("USR", "CTP", "I", "sysop@hotmail.com", "password1")
	if l, _ := cc.expect("USR "); l != "USR 2 OK sysop@hotmail.com sysop" {
		t.Errorf("CTP: %q", l)
	}
}

// TestContacts: adding a friend (the store's friends list, and online at
// once), who sees one on their reverse list; status changes; a block, by
// which one appears offline; removing.
func TestContacts(t *testing.T) {
	e := start(t)
	alice := online(t, e.addr, "alice")
	bob := online(t, e.addr, "bob")
	aliceU, bobU := e.user(t, "alice"), e.user(t, "bob")

	tr := alice.cmd("ADD", "FL", "bob@bbs.test", "bob", "0")
	if l, _ := alice.expect("ADD " + tr + " "); !strings.HasPrefix(l, "ADD "+tr+" FL ") || !strings.HasSuffix(l, " bob@bbs.test bob 0") {
		t.Errorf("add %q", l)
	}
	if l, _ := alice.expect("ILN " + tr); l != "ILN "+tr+" NLN bob@bbs.test bob" {
		t.Errorf("bob on: %q", l)
	}
	if ok, _ := e.b.Store.IsFriend(aliceU.ID, bobU.ID); !ok {
		t.Error("bob is not alice's friend in the store")
	}
	if l, _ := bob.expect("ADD 0 RL "); !strings.HasSuffix(l, " alice@bbs.test alice") {
		t.Errorf("bob's reverse list: %q", l)
	}
	tr = alice.cmd("ADD", "FL", "bob@bbs.test", "bob", "0")
	if l, _ := alice.expect(""); l != "215 "+tr {
		t.Errorf("twice: %q", l)
	}
	tr = alice.cmd("ADD", "FL", "nobody@bbs.test", "nobody", "0")
	if l, _ := alice.expect(""); l != "205 "+tr {
		t.Errorf("nobody: %q", l)
	}

	// statuses and names
	bob.cmd("CHG", "BSY")
	if l, _ := alice.expect("NLN "); l != "NLN BSY bob@bbs.test bob" {
		t.Errorf("busy %q", l)
	}
	bob.cmd("REA", "bob@bbs.test", "Bobby%20B")
	bob.expect("REA ")
	if l, _ := alice.expect("NLN "); l != "NLN BSY bob@bbs.test Bobby%20B" {
		t.Errorf("renamed %q", l)
	}
	bob.cmd("CHG", "HDN")
	if l, _ := alice.expect("FLN "); l != "FLN bob@bbs.test" {
		t.Errorf("hidden %q", l)
	}
	bob.cmd("CHG", "NLN")
	alice.expect("NLN NLN bob@bbs.test")

	// bob blocks alice: offline to her, in the store, her messages refused
	tr = bob.cmd("ADD", "BL", "alice@bbs.test", "alice")
	bob.expect("ADD " + tr + " BL ")
	if l, _ := alice.expect("FLN "); l != "FLN bob@bbs.test" {
		t.Errorf("blocked %q", l)
	}
	if !e.b.Blocks(bobU, aliceU) {
		t.Error("not blocked in the store")
	}
	tr = bob.cmd("REM", "BL", "alice@bbs.test")
	bob.expect("REM " + tr + " BL ")
	alice.expect("NLN NLN bob@bbs.test")

	// a friend added from elsewhere (IRC, the terminal) comes to the list
	if _, err := e.b.AddFriend(aliceU, "Carol Ann"); err != nil {
		t.Fatal(err)
	}
	if l, _ := alice.expect("ADD 0 FL "); !strings.Contains(l, " CarolAnn@bbs.test Carol%20Ann") {
		t.Errorf("from elsewhere %q", l)
	}
	tr = alice.cmd("REM", "FL", "bob@bbs.test")
	alice.expect("REM " + tr + " FL ")
	if ok, _ := e.b.Store.IsFriend(aliceU.ID, bobU.ID); ok {
		t.Error("bob is still alice's friend")
	}
	bob.expect("REM 0 RL ")
}

// TestConversation: two MSN clients in a switchboard: a call rung and
// answered, a message both ways (acked, kept), one leaving.
func TestConversation(t *testing.T) {
	e := start(t)
	alice := online(t, e.addr, "alice")
	bob := online(t, e.addr, "bob")
	as := alice.switchboard(t, "alice@bbs.test")
	tr := as.cmd("CAL", "bob@bbs.test")
	if l, _ := as.expect("CAL "); !strings.HasPrefix(l, "CAL "+tr+" RINGING ") {
		t.Fatalf("call %q", l)
	}
	rng, _ := bob.expect("RNG ")
	if !strings.HasSuffix(rng, " alice@bbs.test alice") {
		t.Errorf("rung %q", rng)
	}
	bs, iro := answer(t, rng, "bob@bbs.test")
	if len(iro) != 1 || iro[0] != "IRO 1 1 1 alice@bbs.test alice" {
		t.Errorf("iro %q", iro)
	}
	if l, _ := as.expect("JOI "); l != "JOI bob@bbs.test bob" {
		t.Errorf("joi %q", l)
	}
	tr = as.msg("A", "hello bob")
	if l, p := bs.expect("MSG "); !strings.HasPrefix(l, "MSG alice@bbs.test alice ") || !strings.HasSuffix(p, "\r\n\r\nhello bob") {
		t.Errorf("bob got %q %q", l, p)
	}
	if l, _ := as.expect(""); l != "ACK "+tr {
		t.Errorf("ack %q", l)
	}
	bs.msg("N", "hi alice")
	if _, p := as.expect("MSG bob@bbs.test "); !strings.HasSuffix(p, "hi alice") {
		t.Errorf("alice got %q", p)
	}
	cs, err := e.b.Store.ChatsBetween(e.user(t, "alice").ID, e.user(t, "bob").ID, 10)
	if err != nil || len(cs) != 2 || cs[0].Text != "hello bob" || !cs[1].Delivered {
		t.Errorf("kept %v %v", cs, err)
	}
	bs.cmd("OUT")
	if l, _ := as.expect("BYE "); l != "BYE bob@bbs.test" {
		t.Errorf("bye %q", l)
	}
	tr = as.cmd("CAL", "nobody@bbs.test")
	if l, _ := as.expect(""); l != "208 "+tr {
		t.Errorf("nobody %q", l)
	}
}

// TestIRC: an MSN client and an IRC user: she is in the conversation at
// once, has its lines as PRIVMSGs, and hers come back into it; once it is
// left, hers ring the client into a new one.  One away has the client's
// lines kept, and the client told so.
func TestIRC(t *testing.T) {
	e := start(t)
	alice := online(t, e.addr, "alice")
	carol := ircLogin(t, e.ircAddr, "Carol Ann")
	as := alice.switchboard(t, "alice@bbs.test")
	as.cmd("CAL", "CarolAnn@bbs.test")
	as.expect("CAL ")
	if l, _ := as.expect("JOI "); l != "JOI CarolAnn@bbs.test Carol%20Ann" {
		t.Errorf("joi %q", l)
	}
	tr := as.msg("A", "hi carol")
	if l := carol.expect(" PRIVMSG Carol_Ann "); !strings.Contains(l, ":alice!") || !strings.HasSuffix(strings.TrimSpace(l), ":hi carol") {
		t.Errorf("carol got %q", l)
	}
	as.expect("ACK " + tr)
	carol.send("PRIVMSG alice :hi from IRC")
	if l, p := as.expect("MSG "); !strings.HasPrefix(l, "MSG CarolAnn@bbs.test Carol%20Ann ") || !strings.HasSuffix(p, "hi from IRC") {
		t.Errorf("alice got %q %q", l, p)
	}

	as.cmd("OUT")
	carol.send("PRIVMSG alice :still there?")
	rng, _ := alice.expect("RNG ")
	if !strings.HasSuffix(rng, " CarolAnn@bbs.test Carol%20Ann") {
		t.Errorf("rung %q", rng)
	}
	as2, iro := answer(t, rng, "alice@bbs.test")
	if len(iro) != 1 || !strings.Contains(iro[0], " CarolAnn@bbs.test ") {
		t.Errorf("iro %q", iro)
	}
	if _, p := as2.expect("MSG CarolAnn@bbs.test "); !strings.HasSuffix(p, "still there?") {
		t.Errorf("queued %q", p)
	}

	// carol goes: alice's lines are kept for her, and alice told once
	carol.send("QUIT")
	for i := 0; len(e.srv.nodesOf("Carol Ann")) > 0; i++ {
		if i > 300 {
			t.Fatal("carol never went")
		}
		time.Sleep(10 * time.Millisecond)
	}
	as2.msg("A", "bye then")
	if _, p := as2.expect("MSG BBS@bbs.test BBS "); !strings.Contains(p, "Carol Ann is not on now") {
		t.Errorf("told %q", p)
	}
	as2.expect("ACK ")
	if cs, _ := e.b.Store.KeptChats(e.user(t, "Carol Ann").ID); len(cs) != 1 || cs[0].Text != "bye then" {
		t.Errorf("kept %v", cs)
	}
}

// TestKept: lines kept while one was away come at their first status,
// rung into a conversation, with their time; a blocked user's do not.
func TestKept(t *testing.T) {
	e := start(t)
	carol := ircLogin(t, e.ircAddr, "Carol Ann")
	carol.send("PRIVMSG bob :are you there?")
	carol.expect(" 301 ")
	bobU, carolU := e.user(t, "bob"), e.user(t, "Carol Ann")

	bob := online(t, e.addr, "bob")
	rng, _ := bob.expect("RNG ")
	bs, _ := answer(t, rng, "bob@bbs.test")
	if _, p := bs.expect("MSG CarolAnn@bbs.test "); !strings.Contains(p, "while you were away) are you there?") {
		t.Errorf("kept %q", p)
	}
	if cs, _ := e.b.Store.KeptChats(bobU.ID); len(cs) != 0 {
		t.Errorf("still kept %v", cs)
	}

	// bob blocks carol: her messages are refused, now and while he is away
	tr := bob.cmd("ADD", "BL", "CarolAnn@bbs.test", "Carol%20Ann")
	bob.expect("ADD " + tr + " BL ")
	carol.send("PRIVMSG bob :hello?")
	if l := carol.expect(" 404 "); !strings.Contains(l, "not taking your messages") {
		t.Errorf("carol told %q", l)
	}
	bs.quiet("MSG CarolAnn")
	bob.quiet("RNG ")
	if !e.b.Blocks(bobU, carolU) {
		t.Error("not blocked")
	}
}
