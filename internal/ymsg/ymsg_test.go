package ymsg

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
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
	b    *bbs.BBS
	st   *store.Store
	srv  *Server
	addr string // the pager's
	web  string // the web half's (plain HTTP, for the test)
}

func freeAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// start is a BBS (alice, bob, Carol Ann and sysop, all password1; old,
// made before Yahoo digests were kept) with a Yahoo server.
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
	st.SetKeepYMSG(true)
	for _, name := range []string{"alice", "bob", "Carol Ann", "sysop"} {
		if _, err := st.CreateUser(name, "password1", "Here", name == "sysop"); err != nil {
			t.Fatal(err)
		}
	}
	s := bbs.Defaults()
	s.Name, s.FailDelay = "Test BBS", 0
	logs := &syncBuf{}
	b := bbs.New(s, st, log.New(logs, "", 0))
	e := &env{b: b, st: st, addr: freeAddr(t), web: freeAddr(t)}
	srv, err := New(b, Settings{Address: e.addr, HTTPAddress: e.web, MaxPerIP: 50, Host: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	e.srv = srv
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
	for _, a := range []string{e.addr, e.web} {
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

// client is a scripted Yahoo client.
type client struct {
	t       *testing.T
	c       net.Conn
	r       *bufio.Reader
	ver     uint16
	session uint32
	id      string
	ch      chan *packet
}

func dial(t *testing.T, e *env, ver uint16) *client {
	t.Helper()
	c, err := net.Dial("tcp", e.addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cl := &client{t: t, c: c, r: bufio.NewReader(c), ver: ver, ch: make(chan *packet, 256)}
	go func() {
		defer close(cl.ch)
		for {
			p, err := readPacket(cl.r)
			if err != nil {
				return
			}
			cl.ch <- p
		}
	}()
	return cl
}

func (cl *client) send(service uint16, status uint32, ps ...pair) {
	p := &packet{version: cl.ver, service: service, status: status, session: cl.session, pairs: ps}
	if _, err := cl.c.Write(p.encode()); err != nil {
		cl.t.Fatal(err)
	}
}

// expect waits for a packet of the service that match says is the one
// (nil: any of it), passing over others.
func (cl *client) expect(service uint16, match func(*packet) bool) *packet {
	cl.t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case p, ok := <-cl.ch:
			if !ok {
				cl.t.Fatalf("closed, waiting for service %#x", service)
			}
			if p.service == service && (match == nil || match(p)) {
				if p.session != 0 {
					cl.session = p.session
				}
				return p
			}
		case <-timeout:
			cl.t.Fatalf("no service %#x", service)
		}
	}
}

// quiet: nothing of the service comes for a while.
func (cl *client) quiet(service uint16, match func(*packet) bool) {
	cl.t.Helper()
	timeout := time.After(300 * time.Millisecond)
	for {
		select {
		case p, ok := <-cl.ch:
			if !ok {
				return
			}
			if p.service == service && (match == nil || match(p)) {
				cl.t.Fatalf("unexpected %#x: %v", service, p.pairs)
			}
		case <-timeout:
			return
		}
	}
}

func y64(b []byte) string { return store.Y64(b) }

// answers are what libyahoo2's pre-0x0b login sends for a password.
func answers(user, pw, seed string) (string, string) {
	p := md5.Sum([]byte(pw))
	c := md5.Sum([]byte(store.MD5Crypt(pw, "_2S43d5f")))
	ph, ch := y64(p[:]), y64(c[:])
	sv := seed[15] % 8 % 5
	var hp, hc string
	switch sv {
	case 0:
		k := seed[seed[7]%16]
		hp, hc = fmt.Sprintf("%c%s%s%s", k, ph, user, seed), fmt.Sprintf("%c%s%s%s", k, ch, user, seed)
	case 1:
		k := seed[seed[9]%16]
		hp, hc = fmt.Sprintf("%c%s%s%s", k, user, seed, ph), fmt.Sprintf("%c%s%s%s", k, user, seed, ch)
	case 2:
		k := seed[seed[15]%16]
		hp, hc = fmt.Sprintf("%c%s%s%s", k, seed, ph, user), fmt.Sprintf("%c%s%s%s", k, seed, ch, user)
	case 3:
		k := seed[seed[1]%16]
		hp, hc = fmt.Sprintf("%c%s%s%s", k, user, ph, seed), fmt.Sprintf("%c%s%s%s", k, user, ch, seed)
	case 4:
		k := seed[seed[3]%16]
		hp, hc = fmt.Sprintf("%c%s%s%s", k, ph, seed, user), fmt.Sprintf("%c%s%s%s", k, ch, seed, user)
	}
	r6, r96 := md5.Sum([]byte(hp)), md5.Sum([]byte(hc))
	return y64(r6[:]), y64(r96[:])
}

// login signs in by the challenge (YMSG 9 to 15): ok false, it was
// refused (the AUTHRESP's key 66 in code).
func (cl *client) login(user, pw string) (code string, ok bool) {
	cl.t.Helper()
	cl.id = user
	cl.send(svcVerify, 0)
	cl.expect(svcVerify, nil)
	cl.send(svcAuth, 0, kv(1, user))
	a := cl.expect(svcAuth, nil)
	if a.get(13) != "0" || len(a.get(94)) < 16 {
		cl.t.Fatalf("auth: %v", a.pairs)
	}
	r6, r96 := answers(user, pw, a.get(94))
	cl.send(svcAuthResp, 0, kv(0, user), kv(6, r6), kv(96, r96), kv(1, user))
	for {
		select {
		case p, ok := <-cl.ch:
			if !ok {
				return "closed", false
			}
			if p.service == svcAuthResp {
				return p.get(66), false
			}
			if p.service == svcList {
				return "", true
			}
		case <-time.After(3 * time.Second):
			cl.t.Fatal("no answer to the login")
		}
	}
}

// signedIn is a client signed in.
func signedIn(t *testing.T, e *env, user string, ver uint16) *client {
	t.Helper()
	cl := dial(t, e, ver)
	if code, ok := cl.login(user, "password1"); !ok {
		t.Fatalf("%s: refused (%s)", user, code)
	}
	if ver >= 15 {
		cl.expect(svcStatus15, nil)
	} else {
		cl.expect(svcLogon, func(p *packet) bool { return p.get(0) == user })
	}
	return cl
}

func TestChallengeLogin(t *testing.T) {
	e := start(t)
	cl := dial(t, e, 10)
	if code, ok := cl.login("alice", "password1"); !ok {
		t.Fatalf("refused: %s", code)
	}
	logon := cl.expect(svcLogon, nil)
	if logon.get(0) != "alice" || logon.get(1) != "alice" {
		t.Errorf("logon: %v", logon.pairs)
	}
	if cl.session == 0 {
		t.Error("no session id")
	}

	// a name with spaces, without them; a wrong password; an account with
	// no digests yet
	if _, ok := dial(t, e, 10).login("carolann", "password1"); !ok {
		t.Error("Carol Ann refused")
	}
	if code, ok := dial(t, e, 10).login("bob", "wrong"); ok || code != "13" {
		t.Errorf("wrong password: %q %v", code, ok)
	}
	if code, ok := dial(t, e, 10).login("old", "password1"); ok || code != "13" {
		t.Errorf("no digests: %q %v", code, ok)
	}
	if code, ok := dial(t, e, 10).login("nobody", "password1"); ok || code != "13" {
		t.Errorf("no such user: %q %v", code, ok)
	}
	// once logged in some other way, old has digests
	if _, err := e.b.Authenticate("test", "", "old", "password1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := dial(t, e, 10).login("old", "password1"); !ok {
		t.Error("old refused after a password login")
	}
}

// httpGet is a GET of the web half: the body's lines.
func httpGet(t *testing.T, e *env, path string) []string {
	t.Helper()
	r, err := http.Get("http://" + e.web + path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return strings.Split(strings.TrimRight(string(b), "\r\n"), "\r\n")
}

func TestTokenLogin(t *testing.T) {
	e := start(t)
	if l := httpGet(t, e, "/capacity"); len(l) != 2 || l[0] != "COLO_CAPACITY=1" || l[1] != "CS_IP_ADDRESS=127.0.0.1" {
		t.Errorf("capacity: %q", l)
	}
	cl := dial(t, e, 16)
	cl.send(svcAuth, 0, kv(1, "alice"))
	a := cl.expect(svcAuth, nil)
	if a.get(13) != "2" {
		t.Fatalf("auth type: %v", a.pairs)
	}
	seed := a.get(94)
	if l := httpGet(t, e, "/config/pwtoken_get?src=ymsgr&ts=&login=alice&passwd=wrong&chal="+url.QueryEscape(seed)); l[0] != "1212" {
		t.Errorf("wrong password: %q", l)
	}
	l := httpGet(t, e, "/config/pwtoken_get?src=ymsgr&ts=&login=alice&passwd=password1&chal="+url.QueryEscape(seed))
	if len(l) < 2 || l[0] != "0" || !strings.HasPrefix(l[1], "ymsgr=") {
		t.Fatalf("token: %q", l)
	}
	l = httpGet(t, e, "/config/pwtoken_login?src=ymsgr&ts=&token="+url.QueryEscape(strings.TrimPrefix(l[1], "ymsgr=")))
	if len(l) < 4 || l[0] != "0" || !strings.HasPrefix(l[1], "crumb=") || !strings.HasPrefix(l[3], "T=") {
		t.Fatalf("crumb: %q", l)
	}
	crumb := strings.TrimPrefix(l[1], "crumb=")
	y := strings.SplitN(strings.TrimPrefix(l[2], "Y="), ";", 2)[0]
	tc := strings.SplitN(strings.TrimPrefix(l[3], "T="), ";", 2)[0]
	h := md5.Sum([]byte(crumb + seed))
	cl.send(svcAuthResp, 0, kv(1, "alice"), kv(0, "alice"), kv(277, y), kv(278, tc), kv(307, y64(h[:])),
		kv(244, "4194239"), kv(2, "alice"), kv(2, "1"), kv(98, "us"), kv(135, "9.0.0.2162"))
	cl.expect(svcList, nil)
	list := cl.expect(svcList15, nil)
	if list.get(303) == "" {
		t.Errorf("list 15: %v", list.pairs)
	}
	cl.expect(svcStatus15, nil)

	// the cookie is spent
	c2 := dial(t, e, 16)
	c2.send(svcAuth, 0, kv(1, "alice"))
	a = c2.expect(svcAuth, nil)
	h = md5.Sum([]byte(crumb + a.get(94)))
	c2.send(svcAuthResp, 0, kv(1, "alice"), kv(0, "alice"), kv(278, tc), kv(307, y64(h[:])))
	if r := c2.expect(svcAuthResp, nil); r.status != stDisconnected || r.get(66) != "13" {
		t.Errorf("spent cookie: %v", r.pairs)
	}
}

func TestBuddiesAndIMs(t *testing.T) {
	e := start(t)
	alice := signedIn(t, e, "alice", 10)
	alice.send(svcAddBuddy, 0, kv(1, "alice"), kv(7, "bob"), kv(65, "Pals"), kv(14, ""))
	alice.expect(svcAddBuddy, func(p *packet) bool { return p.get(7) == "bob" && p.get(66) == "0" })
	alice.send(svcAddBuddy, 0, kv(1, "alice"), kv(7, "nobody"), kv(65, "Pals"))
	alice.expect(svcRemBuddy, func(p *packet) bool { return p.get(7) == "nobody" })
	a, _ := e.st.UserByName("alice")
	bobU, _ := e.st.UserByName("bob")
	fs, _ := e.st.Friends(a.ID)
	if len(fs) != 1 || fs[0].Name != "bob" || fs[0].Group != "Pals" {
		t.Fatalf("friends: %+v", fs)
	}

	// bob comes on: alice is told; IMs both ways
	bob := signedIn(t, e, "bob", 12)
	alice.expect(svcLogon, func(p *packet) bool { return p.get(7) == "bob" && p.get(13) == "1" })
	alice.send(svcMessage, stOffline, kv(1, "alice"), kv(5, "bob"),
		kv(14, "\x1b[1m<font face=\"Arial\" size=\"10\">hello bob</font>"), kv(97, "1"))
	m := bob.expect(svcMessage, nil)
	if m.get(4) != "alice" || m.get(14) != "hello bob" || m.get(97) != "1" {
		t.Errorf("to bob: %v", m.pairs)
	}
	bob.send(svcMessage, stOffline, kv(1, "bob"), kv(5, "alice"), kv(14, "caf\xe9")) // Windows-1252
	if m := alice.expect(svcMessage, nil); m.get(14) != "caf\xe9" {
		t.Errorf("to alice (YMSG 10, Windows-1252): %q", m.get(14))
	}
	if cs, _ := e.st.ChatsBetween(a.ID, bobU.ID, 10); len(cs) != 2 || cs[1].Text != "café" {
		t.Errorf("kept: %+v", cs)
	}

	// bob's status
	bob.send(svcIsAway, 99, kv(10, "99"), kv(19, "out walking"), kv(47, "1"))
	if p := alice.expect(svcIsAway, nil); p.get(7) != "bob" || p.get(19) != "out walking" || p.get(47) != "1" {
		t.Errorf("away: %v", p.pairs)
	}
	bob.send(svcIsBack, 0, kv(10, "0"))
	alice.expect(svcIsBack, func(p *packet) bool { return p.get(7) == "bob" && p.get(10) == "0" })

	// typing
	alice.send(svcNotify, stNotify, kv(4, "alice"), kv(5, "bob"), kv(14, " "), kv(13, "1"), kv(49, "TYPING"))
	bob.expect(svcNotify, func(p *packet) bool { return p.get(4) == "alice" && p.get(13) == "1" })

	// ignored: bob sees alice go offline, and her IMs are refused
	alice.send(svcIgnoreContact, 0, kv(1, "alice"), kv(7, "bob"), kv(13, "1"))
	alice.expect(svcIgnoreContact, func(p *packet) bool { return p.get(66) == "0" })
	if ok, _ := e.st.IsBlocked(a.ID, bobU.ID); !ok {
		t.Error("not blocked")
	}
	bob.send(svcMessage, stOffline, kv(1, "bob"), kv(5, "alice"), kv(14, "still there?"))
	bob.expect(svcMessage, func(p *packet) bool { return p.get(4) == "bbs" && strings.Contains(p.get(14), "not sent") })
	alice.quiet(svcMessage, nil)
	alice.send(svcIgnoreContact, 0, kv(1, "alice"), kv(7, "bob"), kv(13, "2"))
	alice.expect(svcIgnoreContact, func(p *packet) bool { return p.get(13) == "2" })

	// bob goes
	bob.send(svcLogoff, 0)
	alice.expect(svcLogoff, func(p *packet) bool { return p.get(7) == "bob" })
}

func TestKeptAndOtherRoutes(t *testing.T) {
	e := start(t)
	alice := signedIn(t, e, "alice", 16-4)
	alice.send(svcAddBuddy, 0, kv(1, "alice"), kv(7, "Carol Ann"), kv(65, "Friends"))
	alice.expect(svcAddBuddy, func(p *packet) bool { return p.get(7) == "carolann" })
	alice.send(svcMessage, stOffline, kv(1, "alice"), kv(5, "carolann"), kv(14, "call me"), kv(97, "1"))
	carol, _ := e.st.UserByName("Carol Ann")
	for i := 0; ; i++ {
		if cs, _ := e.st.KeptChats(carol.ID); len(cs) == 1 {
			break
		}
		if i > 100 {
			t.Fatal("not kept")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Carol Ann on a terminal: alice sees her on; she has what was kept,
	// then IMs alice
	n, err := e.b.Enter(&bbs.Caller{Via: "telnet"}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	n.SetUser(carol)
	alice.expect(svcLogon, func(p *packet) bool { return p.get(7) == "carolann" && strings.Contains(p.get(19), "telnet") })
	kept := n.Kept()
	if len(kept) != 1 || kept[0].Text != "call me" {
		t.Errorf("kept: %+v", kept)
	}
	a, _ := e.st.UserByName("alice")
	if _, err := n.TellUser(a, "hi from the terminal"); err != nil {
		t.Fatal(err)
	}
	if m := alice.expect(svcMessage, nil); m.get(4) != "carolann" || m.get(14) != "hi from the terminal" {
		t.Errorf("from the terminal: %v", m.pairs)
	}
	n.Leave()
	alice.expect(svcLogoff, func(p *packet) bool { return p.get(7) == "carolann" })

	// bob IMs alice while she is away: she has it when she signs in
	alice.send(svcLogoff, 0)
	time.Sleep(50 * time.Millisecond)
	bob, _ := e.st.UserByName("bob")
	nb, _ := e.b.Enter(&bbs.Caller{Via: "telnet"}, func() {})
	nb.SetUser(bob)
	nb.TellUser(a, "are you there")
	nb.Leave()
	again := dial(t, e, 16-4)
	if _, ok := again.login("alice", "password1"); !ok {
		t.Fatal("refused")
	}
	if m := again.expect(svcMessage, nil); m.get(4) != "bob" || !strings.Contains(m.get(14), "while you were away) are you there") || m.get(15) == "" {
		t.Errorf("kept for alice: %v", m.pairs)
	}
}

func TestListFormats(t *testing.T) {
	e := start(t)
	a, _ := e.st.UserByName("alice")
	b, _ := e.st.UserByName("bob")
	c, _ := e.st.UserByName("Carol Ann")
	e.st.PutFriend(a.ID, store.Friend{ID: b.ID, Group: "Pals"})
	e.st.PutFriend(a.ID, store.Friend{ID: c.ID})
	e.st.Block(a.ID, b.ID)

	old := dial(t, e, 10)
	old.login("alice", "password1")
	// login returned at LIST; fetch again to read it
	old.send(svcList, 0, kv(1, "alice"))
	l := old.expect(svcList, func(p *packet) bool { return p.has(87) })
	if l.get(87) != "Friends:carolann\nPals:bob\n" || l.get(88) != "bob" {
		t.Errorf("list: %q %q", l.get(87), l.get(88))
	}

	nw := dial(t, e, 15)
	nw.login("alice", "password1")
	l15 := nw.expect(svcList15, nil)
	var got []string
	for _, x := range l15.pairs {
		if x.k == 65 || x.k == 7 {
			got = append(got, fmt.Sprintf("%d=%s", x.k, x.v))
		}
	}
	if strings.Join(got, " ") != "65=Friends 7=carolann 65=Pals 7=bob 7=bob" || l15.all(303)[len(l15.all(303))-1] != "318" {
		t.Errorf("list 15: %v", l15.pairs)
	}
}

func TestChatRoom(t *testing.T) {
	e := start(t)
	alice := signedIn(t, e, "alice", 12)
	bob, _ := e.st.UserByName("bob")
	n, _ := e.b.Enter(&bbs.Caller{Via: "telnet"}, func() {})
	n.SetUser(bob)
	if _, err := n.JoinRoom("lobby"); err != nil {
		t.Fatal(err)
	}
	n.Say("before alice", false)

	alice.send(svcChatOnline, 0, kv(1, "alice"), kv(109, "alice"), kv(6, "abcde"))
	alice.expect(svcChatOnline, nil)
	alice.send(svcChatJoin, 0, kv(1, "alice"), kv(104, "Lobby:1"), kv(129, "0"), kv(62, "2"))
	j := alice.expect(svcChatJoin, func(p *packet) bool { return p.get(130) == "1" })
	if j.get(104) != "Lobby:1" || j.get(108) != "2" || !strings.Contains(fmt.Sprint(j.all(109)), "bob") {
		t.Errorf("join: %v", j.pairs)
	}
	alice.expect(svcComment, func(p *packet) bool { return p.get(109) == "bob" && p.get(117) == "before alice" })

	n.Say("hi alice", false)
	alice.expect(svcComment, func(p *packet) bool { return p.get(109) == "bob" && p.get(117) == "hi alice" })
	alice.send(svcComment, 0, kv(1, "alice"), kv(104, "Lobby:1"), kv(117, "hi bob"), kv(124, "1"))
	time.Sleep(100 * time.Millisecond)
	found := false
	for _, ev := range n.Pending() {
		if ev.Kind == bbs.Said && ev.From == "alice" && ev.Text == "hi bob" {
			found = true
		}
	}
	if !found {
		t.Error("bob did not hear alice")
	}
	n.LeaveRoom()
	alice.expect(svcChatExit, func(p *packet) bool { return p.get(109) == "bob" })
	alice.send(svcChatLogout, 0, kv(1, "alice"))
	alice.expect(svcChatLogout, nil)
	if len(e.b.RoomMembers("lobby")) != 0 {
		t.Errorf("still in: %v", e.b.RoomMembers("lobby"))
	}
	l := httpGet(t, e, "/ycontent/?chatcat=0")
	if !strings.Contains(strings.Join(l, ""), "<category") {
		t.Errorf("categories: %q", l)
	}
}

func TestConference(t *testing.T) {
	e := start(t)
	alice := signedIn(t, e, "alice", 12)
	bob := signedIn(t, e, "bob", 12)
	alice.send(svcConfInvite, 0, kv(1, "alice"), kv(50, "alice"), kv(52, "bob"), kv(52, "carolann"),
		kv(57, "alice-1234"), kv(58, "let's talk"), kv(13, "0"))
	inv := bob.expect(svcConfInvite, nil)
	if inv.get(50) != "alice" || inv.get(57) != "alice-1234" || inv.get(58) != "let's talk" {
		t.Errorf("invite: %v", inv.pairs)
	}
	// Carol Ann, not on Yahoo, has it as a private message
	carol, _ := e.st.UserByName("Carol Ann")
	if cs, _ := e.st.KeptChats(carol.ID); len(cs) != 1 || !strings.Contains(cs[0].Text, "yc-alice-1234") {
		t.Errorf("Carol Ann's invitation: %+v", cs)
	}
	bob.send(svcConfLogon, 0, kv(1, "bob"), kv(3, "alice"), kv(57, "alice-1234"))
	alice.expect(svcConfLogon, func(p *packet) bool { return p.get(53) == "bob" })
	alice.send(svcConfMsg, 0, kv(1, "alice"), kv(53, "bob"), kv(57, "alice-1234"), kv(14, "first"), kv(97, "1"))
	bob.expect(svcConfMsg, func(p *packet) bool { return p.get(3) == "alice" && p.get(14) == "first" })
	alice.quiet(svcConfMsg, nil) // her own line is not echoed
	bob.send(svcConfLogoff, 0, kv(1, "bob"), kv(3, "alice"), kv(57, "alice-1234"))
	alice.expect(svcConfLogoff, func(p *packet) bool { return p.get(56) == "bob" })
}

func TestText(t *testing.T) {
	if got := fromYahoo("\x1b[1m\x1b[#ff0000m<font face=\"Times\">hi</font> <FADE #ff0000,#0000ff>there</FADE>", true); got != "hi there" {
		t.Errorf("markup: %q", got)
	}
	for in, want := range map[string]string{"Lobby:1": "lobby", "Linux, FreeBSD, Solaris:2": "linux-freebsd-solari",
		"!!!": "lobby", "General Chat": "general-chat"} {
		if got := roomFor(in); got != want {
			t.Errorf("roomFor(%q) = %q, want %q", in, got, want)
		}
	}
	p := &packet{version: 10, service: svcMessage, status: 1, session: 99}
	p.add(1, "a").add(14, "").add(97, "1")
	q, err := readPacket(bytes.NewReader(p.encode()))
	if err != nil || q.service != svcMessage || q.session != 99 || len(q.pairs) != 3 || q.get(1) != "a" || !q.has(14) {
		t.Errorf("round trip: %+v %v", q, err)
	}
}

// TestOldTLS: the HTTPS login takes Windows XP's TLS 1.0 with RSA key
// exchange and 3DES.
func TestOldTLS(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "login.yahoo.com"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"login.yahoo.com"}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig(cert))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.(*tls.Conn).Handshake()
			c.Close()
		}
	}()
	for _, cs := range []uint16{tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_RSA_WITH_RC4_128_SHA} {
		c, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true,
			MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS10, CipherSuites: []uint16{cs}})
		if err != nil {
			t.Errorf("%s: %v", tls.CipherSuiteName(cs), err)
			continue
		}
		c.Close()
	}
}
