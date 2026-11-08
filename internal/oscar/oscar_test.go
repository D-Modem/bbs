package oscar

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"log"
	"net"
	"path/filepath"
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
	addr    string // OSCAR's
	ircAddr string
	logs    *syncBuf
	p3      chan []byte
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
// made before AIM digests were kept) with OSCAR and IRC servers.
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
	st.SetKeepOSCAR(true)
	for _, u := range []struct {
		name  string
		sysop bool
	}{{"alice", false}, {"bob", false}, {"Carol Ann", false}, {"sysop", true}} {
		if _, err := st.CreateUser(u.name, "password1", "Here", u.sysop); err != nil {
			t.Fatal(err)
		}
	}
	s := bbs.Defaults()
	s.Name, s.FailDelay = "Test BBS", 0
	logs := &syncBuf{}
	b := bbs.New(s, st, log.New(logs, "", 0))
	e := &env{b: b, logs: logs, p3: make(chan []byte, 1)}
	e.addr = freeAddr(t)
	srv, err := New(b, Settings{Address: e.addr, MaxPerIP: 50})
	if err != nil {
		t.Fatal(err)
	}
	srv.P3 = func(c net.Conn) {
		p := make([]byte, 4)
		n, _ := c.Read(p)
		e.p3 <- p[:n]
	}
	e.srv = srv
	e.ircAddr = freeAddr(t)
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

// client is a scripted OSCAR client's connection.
type client struct {
	t   *testing.T
	c   net.Conn
	fc  *flapConn
	req uint32
}

// dial connects, sends its hello with TLVs and takes the server's.
func dial(t *testing.T, addr string, hello tlvs) *client { return dialWait(t, addr, hello, false) }

// dialWait is dial, waiting for the server's hello first if wait.
func dialWait(t *testing.T, addr string, hello tlvs, wait bool) *client {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cl := &client{t: t, c: c, fc: newFlapConn(c, bufio.NewReader(c))}
	if !wait {
		// as libpurple does: its hello at once
		cl.fc.write(chanSignon, append([]byte{0, 0, 0, 1}, hello.bytes()...))
	}
	f := cl.frame()
	if f.ch != chanSignon || !bytes.Equal(f.data, []byte{0, 0, 0, 1}) {
		t.Fatalf("hello %+v", f)
	}
	if wait {
		cl.fc.write(chanSignon, append([]byte{0, 0, 0, 1}, hello.bytes()...))
	}
	return cl
}

func (cl *client) frame() frame {
	cl.t.Helper()
	cl.c.SetReadDeadline(time.Now().Add(3 * time.Second))
	f, err := cl.fc.read()
	if err != nil {
		cl.t.Fatalf("read: %v", err)
	}
	return f
}

// send sends a SNAC.
func (cl *client) send(fam, sub uint16, body []byte) {
	cl.req++
	cl.fc.snac(fam, sub, 0, cl.req, body)
}

// expect reads SNACs until fam/sub; its body.
func (cl *client) expect(fam, sub uint16) []byte {
	cl.t.Helper()
	var seen []string
	for {
		f := cl.frame()
		if f.ch != chanData {
			seen = append(seen, "frame "+itoa(int(f.ch)))
			if f.ch == chanSignoff {
				cl.t.Fatalf("signed off waiting for %04x/%04x: %v (%v)", fam, sub, parseTLVs(f.data), seen)
			}
			continue
		}
		h, body, err := parseSNAC(f.data)
		if err != nil {
			cl.t.Fatal(err)
		}
		if h.fam == fam && h.sub == sub {
			return body
		}
		seen = append(seen, hex4(h.fam)+"/"+hex4(h.sub))
	}
}

func hex4(v uint16) string {
	const d = "0123456789abcdef"
	return string([]byte{d[v>>12], d[v>>8&15], d[v>>4&15], d[v&15]})
}

// login signs on by BUCP; the BOS address and cookie.
func login(t *testing.T, addr, sn, pw string, strong bool) (string, []byte, tlvs) {
	t.Helper()
	cl := dial(t, addr, nil)
	var q tlvs
	q.addStr(tlvScreenName, sn)
	cl.send(0x17, 0x06, q.bytes())
	r := &rd{p: cl.expect(0x17, 0x07)}
	key := r.str16()
	hash := store.OSCARWeak(key, pw)
	if strong {
		hash = store.OSCARStrong(key, pw)
	}
	var l tlvs
	l.addStr(tlvScreenName, sn)
	l.add(tlvPassHash, hash)
	l.addStr(tlvClientID, "AOL Instant Messenger, version 5.5.3415/WIN32")
	if strong {
		l.add(tlvStrongHash, nil)
	}
	cl.send(0x17, 0x02, l.bytes())
	res := parseTLVs(cl.expect(0x17, 0x03))
	ck, _ := res.get(tlvCookie)
	return res.str(tlvReconnect), ck, res
}

// roastedLogin signs on in the signon frame, as AIM 3 and ICQ do.
func roastedLogin(t *testing.T, addr, sn, pw string) (string, []byte, tlvs) {
	t.Helper()
	var l tlvs
	l.addStr(tlvScreenName, sn)
	roasted := []byte(unroast([]byte(pw), roastAIM))
	l.add(tlvRoasted, roasted)
	l.addStr(tlvClientID, "ICQ Inc. - Product of ICQ (TM).2003b.5.56.1.3916.85")
	cl := dialWait(t, addr, l, true) // a client that waits for the server's hello
	f := cl.frame()
	if f.ch != chanSignoff {
		t.Fatalf("not a signoff: %+v", f)
	}
	res := parseTLVs(f.data)
	ck, _ := res.get(tlvCookie)
	return res.str(tlvReconnect), ck, res
}

// bos brings up a BOS connection, ready, with buddies on its list.
func bos(t *testing.T, host string, ck []byte, buddies ...string) *client {
	t.Helper()
	var h tlvs
	h.add(tlvCookie, ck)
	cl := dial(t, host, h)
	r := &rd{p: cl.expect(0x01, 0x03)}
	fams := map[uint16]bool{}
	for r.left() >= 2 {
		fams[r.u16()] = true
	}
	if !fams[0x04] || !fams[0x13] {
		t.Fatalf("families %v", fams)
	}
	var v buf
	for _, f := range []uint16{1, 2, 3, 4, 9, 0x13} {
		v.u16(f)
		v.u16(map[uint16]uint16{1: 4, 0x13: 4}[f] | 1)
	}
	cl.send(0x01, 0x17, v)
	cl.expect(0x01, 0x18)
	cl.send(0x01, 0x06, nil)
	rates := cl.expect(0x01, 0x07)
	if binary.BigEndian.Uint16(rates) != 1 {
		t.Fatalf("rates %x", rates[:8])
	}
	cl.send(0x01, 0x08, []byte{0, 1})
	cl.send(0x01, 0x0e, nil)
	cl.expect(0x01, 0x0f)
	cl.send(0x02, 0x02, nil)
	cl.expect(0x02, 0x03)
	var b buf
	for _, sn := range buddies {
		b.str8(sn)
	}
	if len(buddies) > 0 {
		cl.send(0x03, 0x04, b)
	}
	cl.send(0x04, 0x04, nil)
	cl.expect(0x04, 0x05)
	cl.send(0x01, 0x02, nil)
	return cl
}

// sendIM sends a channel 1 IM.
func (cl *client) sendIM(to, text string, ack bool) {
	var t tlvs
	t.add(0x0002, imData(text))
	if ack {
		t.add(0x0003, nil)
	}
	var b buf
	b.raw(newICBMCookie())
	b.u16(1)
	b.str8(to)
	b.raw(t.bytes())
	cl.send(0x04, 0x06, b)
}

// recvIM reads an IM: from whom, its text.
func (cl *client) recvIM() (string, string) {
	cl.t.Helper()
	r := &rd{p: cl.expect(0x04, 0x07)}
	r.take(8)
	if ch := r.u16(); ch != 1 {
		cl.t.Fatalf("channel %d", ch)
	}
	from := r.str8()
	r.u16()
	r.tlvBlock()
	data, _ := parseTLVs(r.rest()).get(0x0002)
	return from, imText(data)
}

// arrival reads a buddy's arrival: their name and user info TLVs.
func (cl *client) arrival() (string, tlvs) {
	cl.t.Helper()
	r := &rd{p: cl.expect(0x03, 0x0b)}
	sn := r.str8()
	r.u16()
	return sn, r.tlvBlock()
}

// ircConn is a raw IRC client logged in.
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

// TestAIMToIRC: BUCP logins (strong and weak digests), buddies arriving,
// IMs between two AIM clients (their HTML kept, a host ack), and with an
// IRC user both ways (PRIVMSG), a refused password and an unknown name.
func TestAIMToIRC(t *testing.T) {
	e := start(t)
	host, ck, _ := login(t, e.addr, "alice", "password1", true)
	if host != e.addr || len(ck) == 0 {
		t.Fatalf("login: %q %x", host, ck)
	}
	alice := bos(t, host, ck, "bob", "CarolAnn")
	host, ck, res := login(t, e.addr, "BOB", "password1", false)
	if res.str(tlvScreenName) != "bob" {
		t.Errorf("screen name %q", res.str(tlvScreenName))
	}
	bob := bos(t, host, ck, "alice")
	if sn, info := alice.arrival(); sn != "bob" || !info.has(0x0003) {
		t.Errorf("arrival %q %v", sn, info)
	}
	if sn, _ := bob.arrival(); sn != "alice" {
		t.Errorf("bob saw %q", sn)
	}
	alice.sendIM("Bob", "<HTML><B>hello</B> bob</HTML>", true)
	alice.expect(0x04, 0x0c)
	if from, text := bob.recvIM(); from != "alice" || text != "<HTML><B>hello</B> bob</HTML>" {
		t.Errorf("bob got %q %q", from, text)
	}

	// Carol Ann, on IRC: she arrives, and IMs go both ways
	carol := ircLogin(t, e.ircAddr, "Carol Ann")
	// (bob's arrival can come again first, as his session settles)
	sn, info := alice.arrival()
	for i := 0; sn == "bob" && i < 3; i++ {
		sn, info = alice.arrival()
	}
	if sn != "CarolAnn" && sn != "Carol Ann" {
		t.Errorf("carol arrival %q %v", sn, info)
	}
	alice.sendIM("Carol Ann", "<HTML>hi &amp; welcome</HTML>", false)
	if l := carol.expect(" PRIVMSG Carol_Ann "); !strings.Contains(l, ":alice!") || !strings.HasSuffix(strings.TrimSpace(l), ":hi & welcome") {
		t.Errorf("carol got %q", l)
	}
	carol.send("PRIVMSG alice :hello <aim>")
	if from, text := alice.recvIM(); from != "CarolAnn" || !strings.Contains(text, "hello &lt;aim&gt;") {
		t.Errorf("alice got %q %q", from, text)
	}

	// offline, and unknown: not logged on
	alice.sendIM("sysop", "anyone?", false)
	if r := (&rd{p: alice.expect(0x04, 0x01)}); r.u16() != icbmNotLoggedOn {
		t.Error("no error to an offline user")
	}

	// bob goes: alice is told
	bob.c.Close()
	r := &rd{p: alice.expect(0x03, 0x0c)}
	if sn := r.str8(); sn != "bob" {
		t.Errorf("departed %q", sn)
	}

	// a wrong password, an unknown name
	_, _, res = login(t, e.addr, "alice", "wrong", true)
	if c, _ := res.u16(tlvErrorCode); c != errBadPassword {
		t.Errorf("wrong password: %v", res)
	}
	_, _, res = login(t, e.addr, "nobody", "password1", true)
	if c, _ := res.u16(tlvErrorCode); c != errBadName {
		t.Errorf("unknown: %v", res)
	}
}

// TestDigestsKept: an account from before AIM digests are kept fails a
// BUCP login until its password is used once in the clear; a roasted
// login works at once.
func TestDigestsKept(t *testing.T) {
	e := start(t)
	_, _, res := login(t, e.addr, "old", "password1", true)
	if c, _ := res.u16(tlvErrorCode); c != errBadPassword {
		t.Fatalf("old account's digest login: %v", res)
	}
	if !strings.Contains(e.logs.String(), "no AIM password digest yet") {
		t.Error("not logged")
	}
	host, ck, _ := roastedLogin(t, e.addr, "old", "password1")
	if len(ck) == 0 {
		t.Fatal("roasted login failed")
	}
	bos(t, host, ck)
	if _, _, res := login(t, e.addr, "old", "password1", false); !res.has(tlvCookie) {
		t.Fatalf("after a login in the clear: %v", res)
	}
}

// TestChatRoom: chat navigation makes a room of "Test Room", the chat
// connection joins test-room (#test-room on IRC), lines go both ways.
func TestChatRoom(t *testing.T) {
	e := start(t)
	host, ck, _ := login(t, e.addr, "alice", "password1", true)
	alice := bos(t, host, ck)
	carol := ircLogin(t, e.ircAddr, "Carol Ann")
	carol.send("JOIN #test-room")
	carol.expect("366")

	// chat navigation
	var b buf
	b.u16(0x000d)
	alice.send(0x01, 0x04, b)
	svc := parseTLVs(alice.expect(0x01, 0x05))
	nck, _ := svc.get(0x0006)
	var h tlvs
	h.add(0x0006, nck)
	nav := dial(t, svc.str(0x0005), h)
	nav.expect(0x01, 0x03)
	nav.send(0x01, 0x02, nil)
	nav.send(0x0d, 0x02, nil)
	nav.expect(0x0d, 0x09)
	var c buf
	c.u16(4)
	c.str8("create")
	c.u16(0xffff)
	c.u8(1)
	var ct tlvs
	ct.addStr(0x00d3, "Test Room")
	ct.addStr(0x00d6, "us-ascii")
	c.raw(ct.block())
	nav.send(0x0d, 0x08, c)
	info, _ := parseTLVs(nav.expect(0x0d, 0x09)).get(0x0004)
	ri := &rd{p: info}
	ex := ri.u16()
	cookie := ri.str8()
	if ex != 4 || cookie != "4-0-test-room" {
		t.Fatalf("room %d %q", ex, cookie)
	}

	// the room's connection
	var sr buf
	sr.u16(0x000e)
	var rt buf
	rt.u16(4)
	rt.str8(cookie)
	rt.u16(0)
	var st tlvs
	st.add(0x0001, rt)
	sr.raw(st.bytes())
	alice.send(0x01, 0x04, sr)
	svc = parseTLVs(alice.expect(0x01, 0x05))
	cck, _ := svc.get(0x0006)
	var ch tlvs
	ch.add(0x0006, cck)
	room := dial(t, svc.str(0x0005), ch)
	room.expect(0x01, 0x03)
	room.send(0x01, 0x02, nil)
	room.expect(0x0e, 0x02)
	joined := &rd{p: room.expect(0x0e, 0x03)}
	var names []string
	for joined.left() > 0 && !joined.bad {
		names = append(names, joined.str8())
		joined.u16()
		joined.tlvBlock()
	}
	if strings.Join(names, ",") != "Carol Ann,alice" && strings.Join(names, ",") != "alice,Carol Ann" {
		t.Errorf("in the room: %q", names)
	}
	carol.expect("JOIN #test-room")

	carol.send("PRIVMSG #test-room :hi aim")
	mr := &rd{p: room.expect(0x0e, 0x06)}
	mr.take(10)
	mt := parseTLVs(mr.rest())
	sender, _ := mt.get(0x0003)
	minfo, _ := mt.get(0x0005)
	if s := (&rd{p: sender}).str8(); s != "Carol Ann" || !strings.Contains(parseTLVs(minfo).str(0x0001), "hi aim") {
		t.Errorf("room line from %q: %q", s, parseTLVs(minfo).str(0x0001))
	}

	var m buf
	m.raw(newICBMCookie())
	m.u16(3)
	var mi tlvs
	mi.addStr(0x0001, "<HTML>hello irc</HTML>")
	mi.addStr(0x0002, "us-ascii")
	var mt2 tlvs
	mt2.add(0x0001, nil)
	mt2.add(0x0005, mi.bytes())
	m.raw(mt2.bytes())
	room.send(0x0e, 0x05, m)
	if l := carol.expect("PRIVMSG #test-room"); !strings.Contains(l, ":alice!") || !strings.Contains(l, ":hello irc") {
		t.Errorf("irc got %q", l)
	}

	// leaving the room's connection parts the room
	room.c.Close()
	carol.expect("PART #test-room")
}

// TestFeedbag: items kept and given back, buddies in it watched.
func TestFeedbag(t *testing.T) {
	e := start(t)
	host, ck, _ := login(t, e.addr, "alice", "password1", true)
	alice := bos(t, host, ck)
	var b buf
	itemBytes(&b, store.FeedbagItem{Name: "", GroupID: 0, ItemID: 0, ClassID: fbGroup})
	itemBytes(&b, store.FeedbagItem{Name: "Buddies", GroupID: 1, ItemID: 0, ClassID: fbGroup})
	itemBytes(&b, store.FeedbagItem{Name: "bob", GroupID: 1, ItemID: 7, ClassID: fbBuddy, Attrs: []byte{0x01, 0x31, 0, 2, 'B', 'B'}})
	alice.send(0x13, 0x08, b)
	codes := alice.expect(0x13, 0x0e)
	if !bytes.Equal(codes, []byte{0, 0, 0, 0, 0, 0}) {
		t.Fatalf("insert: %x", codes)
	}
	var again buf
	itemBytes(&again, store.FeedbagItem{Name: "bob", GroupID: 1, ItemID: 7, ClassID: fbBuddy})
	alice.send(0x13, 0x08, again)
	if c := alice.expect(0x13, 0x0e); !bytes.Equal(c, []byte{0, fbExists}) {
		t.Errorf("again: %x", c)
	}
	alice.send(0x13, 0x04, nil)
	r := &rd{p: alice.expect(0x13, 0x06)}
	r.u8()
	if n := r.u16(); n != 3 {
		t.Fatalf("%d items", n)
	}
	host, ck, _ = login(t, e.addr, "bob", "password1", true)
	bos(t, host, ck)
	if sn, _ := alice.arrival(); sn != "bob" {
		t.Errorf("arrival %q", sn)
	}
}

// TestICQ: a roasted login by ICQ number, and a short info request.
func TestICQ(t *testing.T) {
	e := start(t)
	bobID := int64(0)
	if u, err := e.b.Store.UserByName("bob"); err == nil {
		bobID = u.ID
	}
	uin := e.srv.uinOf(bobID)
	host, ck, res := roastedLogin(t, e.addr, uin, "password1")
	if res.str(tlvScreenName) != uin || len(ck) == 0 {
		t.Fatalf("login: %v", res)
	}
	bob := bos(t, host, ck)
	var q le
	q.u16(0) // patched below
	q.u32(e.srv.S.UINBase + uint32(bobID))
	q.u16(icqMetaReq)
	q.u16(7)
	q.u16(0x04ba)
	q.u32(e.srv.S.UINBase + uint32(bobID))
	binary.LittleEndian.PutUint16(q, uint16(len(q)-2))
	var t1 tlvs
	t1.add(0x0001, q)
	bob.send(0x15, 0x02, t1.bytes())
	v, _ := parseTLVs(bob.expect(0x15, 0x03)).get(0x0001)
	r := &lrd{rd{p: v}}
	r.u16()
	r.u32()
	if k := r.u16(); k != icqMetaReply {
		t.Fatalf("kind %x", k)
	}
	r.u16()
	if sub, ok := r.u16(), r.u8(); sub != 0x0104 || ok != icqOK {
		t.Fatalf("reply %x %x", sub, ok)
	}
	if nick := r.str(); nick != "bob" {
		t.Errorf("nick %q", nick)
	}
}

// TestP3: a connection beginning with P3's sync byte goes to Server.P3,
// its first bytes unread.
func TestP3(t *testing.T) {
	e := start(t)
	c, err := net.Dial("tcp", e.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte{0x5a, 0x12, 0x34, 0x00})
	select {
	case p := <-e.p3:
		if !bytes.Equal(p, []byte{0x5a, 0x12, 0x34, 0x00}) {
			t.Errorf("P3 got %x", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not given to P3")
	}
}
