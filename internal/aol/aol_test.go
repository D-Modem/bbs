package aol

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/aol/fdo"
	"github.com/D-Modem/bbs/internal/aol/p3"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/oscar"
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

// newServer is a BBS (alice, password1) with an AOL host.
func newServer(t *testing.T) (*Server, *syncBuf) {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.CreateUser("alice", "password1", "Here", false); err != nil {
		t.Fatal(err)
	}
	s := bbs.Defaults()
	s.Name, s.FailDelay = "Test BBS", 0
	logs := &syncBuf{}
	b := bbs.New(s, st, log.New(logs, "", 0))
	srv, err := New(b, Settings{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("log:\n" + logs.String())
		}
	})
	return srv, logs
}

// client is a scripted AOL client: P3 by hand, tokens in DATA.
type client struct {
	t      *testing.T
	c      net.Conn
	tx, rx uint8
	pkts   chan p3.Packet
}

func connect(t *testing.T, srv *Server) *client {
	t.Helper()
	a, b := net.Pipe()
	go srv.Handle(b)
	return attach(t, a)
}

// attach begins P3 on a, a connection to the host.
func attach(t *testing.T, a net.Conn) *client {
	t.Helper()
	c := &client{t: t, c: a, tx: p3.SeqInit, rx: p3.SeqInit, pkts: make(chan p3.Packet, 256)}
	go func() {
		r := p3.NewReader(a)
		for {
			p, err := r.Read()
			if err != nil {
				close(c.pkts)
				return
			}
			c.pkts <- p
		}
	}()
	t.Cleanup(func() { a.Close() })
	init := make([]byte, 0x34)
	init[0], init[1] = p3.PlatformWindows, 3
	c.write(p3.Packet{TX: p3.SeqInit, RX: p3.SeqInit, Type: p3.INIT, Data: init})
	if p := c.next(); p.Type != p3.ACK {
		t.Fatalf("INIT answered with %v", p)
	}
	return c
}

func (c *client) write(p p3.Packet) {
	p.Type |= p3.FromClient
	if _, err := c.c.Write(p3.Append(nil, p)); err != nil {
		c.t.Fatal(err)
	}
}

// send sends a token in a DATA packet.
func (c *client) send(token string, data []byte) {
	c.tx = next(c.tx)
	c.write(p3.Packet{TX: c.tx, RX: c.rx, Type: p3.DATA, Data: append([]byte(token), data...)})
}

func next(s uint8) uint8 {
	if s >= p3.SeqMax {
		return p3.SeqMin
	}
	return s + 1
}

func (c *client) next() p3.Packet {
	c.t.Helper()
	select {
	case p, ok := <-c.pkts:
		if !ok {
			c.t.Fatal("hung up")
		}
		return p
	case <-time.After(3 * time.Second):
		c.t.Fatal("nothing from the host")
	}
	return p3.Packet{}
}

// token is the next token the host sends (acknowledging it), and its
// data; the host's ACKs are passed over.
func (c *client) token() (string, []byte) {
	c.t.Helper()
	for {
		p := c.next()
		if p.Kind() != p3.DATA {
			continue
		}
		c.rx = p.TX
		c.write(p3.Packet{TX: c.tx, RX: c.rx, Type: p3.ACK})
		tok, rest := p3.Token(p.Data)
		return tok, rest
	}
}

// stream is the next atom stream the host sends (aT, or at in pieces),
// decompiled.
func (c *client) stream() string {
	c.t.Helper()
	var held []byte
	for {
		tok, d := c.token()
		switch tok {
		case "aT":
			src, err := fdo.Decompile(d)
			if err != nil {
				c.t.Fatal(err)
			}
			return src
		case "at":
			held = append(held, d[4:]...)
			if src, err := fdo.Decompile(held); err == nil && strings.Contains(src, "uni_end_stream") {
				return src
			}
		default:
			c.t.Fatalf("token %q, wanted a stream", tok)
		}
	}
}

// signOnForm is the Welcome form's Dd, as the 3.0 client sends it: its
// stream id, then the screen name and password as de_data.
func signOnForm(name, pw string) []byte {
	src := `uni_start_stream
  man_set_context_relative <00x, 00x, 00x, 01x>
  man_set_context_index <00x, 00x, 00x, 01x>
  de_data <` + fdo.Quote(name) + `>
  man_end_context
  man_end_context
  man_set_context_relative <00x, 00x, 00x, 02x>
  de_data <` + fdo.Quote(pw) + `>
  man_end_context
uni_end_stream
`
	return append([]byte{0x00, 0x15}, fdo.MustCompile(src)...)
}

func TestSignOn(t *testing.T) {
	srv, logs := newServer(t)
	c := connect(t, srv)
	c.send("Dd", signOnForm("alice     ", "password1"))
	if s := c.stream(); !strings.Contains(s, "async_online") || !strings.Contains(s, "uni_invoke_local <32-296>") {
		t.Errorf("online:\n%s", s)
	}
	if s := c.stream(); !strings.Contains(s, `man_start_object <ind_group, "Welcome, alice!">`) ||
		!strings.Contains(s, "Welcome to Test BBS, alice!") {
		t.Errorf("welcome:\n%s", s)
	}
	nodes := srv.B.Nodes()
	if len(nodes) != 1 || nodes[0].User != "alice" {
		t.Errorf("nodes %+v", nodes)
	}

	// a token not served yet is answered, so the client doesn't wait
	c.send("k1", []byte{0, 1})
	if s := c.stream(); !strings.Contains(s, "uni_wait_off") {
		t.Errorf("k1:\n%s", s)
	}

	// Sign Off
	c.send("pE", []byte{0, 1})
	if tok, d := c.token(); tok != "XS" || !bytes.Equal(d, []byte{0}) {
		t.Errorf("pE answered %q % x", tok, d)
	}
	c.c.Close()
	for i := 0; len(srv.B.Nodes()) > 0; i++ {
		if i > 100 {
			t.Fatal("node not given back")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "alice on AOL") {
		t.Error("not logged")
	}
}

func TestSignOnRefused(t *testing.T) {
	srv, _ := newServer(t)
	for _, c := range []struct{ name, pw, want string }{
		{"alice", "wrong", "Incorrect screen name or password"},
		{"New User", "", "call it by telnet or the web first"},
	} {
		cl := connect(t, srv)
		cl.send("Dd", signOnForm(c.name, c.pw))
		if s := cl.stream(); !strings.Contains(s, "async_alert <info, ") || !strings.Contains(s, c.want) {
			t.Errorf("%s: alert:\n%s", c.name, s)
		}
		if s := cl.stream(); !strings.Contains(s, "async_force_off") {
			t.Errorf("%s: then:\n%s", c.name, s)
		}
		// nothing else until it signs on
		cl.send("k1", []byte{0, 1})
		select {
		case p := <-cl.pkts:
			if p.Kind() == p3.DATA {
				t.Errorf("%s: answered %v", c.name, p)
			}
		case <-time.After(300 * time.Millisecond):
		}
	}
	if n := len(srv.B.Nodes()); n != 0 {
		t.Errorf("%d nodes", n)
	}
}

// TestLongStreams: a stream too big for a packet goes as at pieces; one
// the client spreads over packets is put together.
func TestLongStreams(t *testing.T) {
	srv, _ := newServer(t)
	c := connect(t, srv)
	// the 4.0 client's long Dd, in two packets of one stream id
	d := signOnForm("alice", "password1")
	c.send("Dd", d[:9])
	c.send("Dd", append([]byte{0x00, 0x15}, d[9:]...))
	c.stream()
	c.stream()

	ss := func() *session {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		for ss := range srv.sessions {
			return ss
		}
		return nil
	}()
	long := alertForm(strings.Repeat("All work and no play. ", 100))
	if err := ss.send(long); err != nil {
		t.Fatal(err)
	}
	if s := c.stream(); !strings.Contains(s, "All work and no play. All work") {
		t.Errorf("long:\n%.200s", s)
	}
}

func TestForms(t *testing.T) {
	for _, src := range []string{onlineForm, alertForm(`"Quotes" and \ and é`), welcomeForm("BBS", "Zoë", 3, 1),
		windowForm("50-1-1", "Mailbox", "a\r\nb", 4, []button{{"Read", "read mail 1"}})} {
		if _, err := fdo.Compile(src); err != nil {
			t.Errorf("%v:\n%s", err, src)
		}
	}
}

// TestOSCARPort: on OSCAR's port, an AOL client is handed over and signs
// on, while AIM clients go on as before.
func TestOSCARPort(t *testing.T) {
	srv, _ := newServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	osrv, err := oscar.New(srv.B, oscar.Settings{Address: ln.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	osrv.P3 = srv.Handle
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { osrv.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c := attach(t, conn)
	c.send("Dd", signOnForm("alice", "password1"))
	if s := c.stream(); !strings.Contains(s, "async_online") {
		t.Errorf("online:\n%s", s)
	}
}

// signedOn is a client signed on as name (the welcome passed over).
func signedOn(t *testing.T, srv *Server, name string) *client {
	t.Helper()
	c := connect(t, srv)
	c.send("Dd", signOnForm(name, "password1"))
	c.stream()
	c.stream()
	return c
}

// chatLine is the next AB (a line in the room window), as "name: text".
func (c *client) chatLine() string {
	c.t.Helper()
	for {
		tok, d := c.token()
		if tok != "AB" {
			continue
		}
		if len(d) < 11 || d[10] != 0 || d[len(d)-1] != 0 {
			c.t.Fatalf("AB % x", d)
		}
		return strings.TrimRight(string(d[:10]), " ") + ": " + string(d[11:len(d)-1])
	}
}

// deData is a form's fields as the client sends them, after a stream id.
func deData(fields ...string) []byte {
	src := "uni_start_stream\n"
	for _, f := range fields {
		src += "  de_data <" + fdo.Quote(f) + ">\n"
	}
	return append([]byte{0, 1}, fdo.MustCompile(src+"uni_end_stream\n")...)
}

func TestChat(t *testing.T) {
	srv, _ := newServer(t)
	if _, err := srv.B.Store.CreateUser("bob", "password1", "There", false); err != nil {
		t.Fatal(err)
	}
	a := signedOn(t, srv, "alice")
	a.send("CJ", []byte{0, 1})
	if s := a.stream(); !strings.Contains(s, "man_close") {
		t.Errorf("close first:\n%s", s)
	}
	if s := a.stream(); !strings.Contains(s, "uni_invoke_local <32-117>") || !strings.Contains(s, "chat_add_user") ||
		!strings.Contains(s, `man_preset_title <"main">`) {
		t.Errorf("room:\n%s", s)
	}
	a.stream() // uni_wait_off
	if l := a.chatLine(); l != `OnlineHost: *** You are in "main". ***` {
		t.Error(l)
	}

	// bob on a terminal node comes in: CA
	b := srv.B
	u, _ := b.Store.UserByName("bob")
	n, err := b.Enter(&bbs.Caller{Via: "telnet"}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	n.SetUser(u)
	if _, err := n.JoinRoom("main"); err != nil {
		t.Fatal(err)
	}
	if tok, d := a.token(); tok != "CA" || !bytes.Equal(d, []byte("\x02bob\x00")) {
		t.Errorf("CA: %q % x", tok, d)
	}
	n.Say("hi alice", false)
	if l := a.chatLine(); l != "bob: hi alice" {
		t.Error(l)
	}
	// alice's line goes to the room, and back to her window
	a.send("Aa", deData("hello bob"))
	if l := a.chatLine(); l != "alice: hello bob" {
		t.Error(l)
	}
	if e := waitEvent(t, n, "alice"); e.Kind != bbs.Said || e.From != "alice" || e.Text != "hello bob" {
		t.Errorf("bob had %+v", e)
	}
	n.LeaveRoom()
	if tok, d := a.token(); tok != "CB" || !bytes.Equal(d, []byte{2}) {
		t.Errorf("CB: %q % x", tok, d)
	}
}

func waitEvent(t *testing.T, n *bbs.Node, from string) bbs.Event {
	t.Helper()
	for i := 0; i < 300; i++ {
		for _, e := range n.Pending() {
			if (e.Kind == bbs.Said || e.Kind == bbs.Private) && e.From == from {
				return e
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no event")
	return bbs.Event{}
}

func TestIM(t *testing.T) {
	srv, _ := newServer(t)
	for _, name := range []string{"bob", "Carol Ann"} {
		if _, err := srv.B.Store.CreateUser(name, "password1", "There", false); err != nil {
			t.Fatal(err)
		}
	}
	a := signedOn(t, srv, "alice")
	c := signedOn(t, srv, "CarolAnn") // her AOL name, without its space

	// alice to Carol Ann: in Carol's IM window, and alice's own
	a.send("iS", deData("CarolAnn", "hi there"))
	if s := c.stream(); !strings.Contains(s, "uni_invoke_local <32-223>") ||
		!strings.Contains(s, `"Instant Message From: alice"`) || !strings.Contains(s, `"alice:  hi there\r"`) {
		t.Errorf("Carol's IM:\n%s", s)
	}
	if s := a.stream(); !strings.Contains(s, `"Instant Message From: Carol Ann"`) || !strings.Contains(s, `"alice:  hi there\r"`) {
		t.Errorf("alice's own:\n%s", s)
	}
	// Carol answers in the same window (its global id kept)
	c.send("iS", deData("alice", "<b>hello</b> back"))
	if s := a.stream(); !strings.Contains(s, "man_preset_gid <50-0-1>") || !strings.Contains(s, `"Carol Ann:  hello back\r"`) {
		t.Errorf("answer:\n%s", s)
	}
	c.stream()

	// to bob, who is away: kept, and the sender told
	a.send("iS", deData("bob", "see you"))
	if s := a.stream(); !strings.Contains(s, "not signed on now") {
		t.Errorf("away:\n%s", s)
	}
	a.stream()
	b := signedOn(t, srv, "bob")
	if s := b.stream(); !strings.Contains(s, "while you were away") || !strings.Contains(s, "see you") {
		t.Errorf("kept:\n%s", s)
	}

	// to no one; and who is on
	a.send("iS", deData("nobody", "x"))
	if s := a.stream(); !strings.Contains(s, "nobody is not a member here") {
		t.Errorf("nobody:\n%s", s)
	}
	a.send("iO", deData("bob"))
	if s := a.stream(); !strings.Contains(s, "bob is online") {
		t.Errorf("iO:\n%s", s)
	}
}

// mailForm is a mail as the Write Mail form sends it (m4): its header and
// text, in one stream or in two.
func mailForm(to, subject, text string, split bool) [][]byte {
	head := "  mip_addressee <" + fdo.Quote(to) + ">\n  mip_subject <" + fdo.Quote(subject) + ">\n"
	body := "  mip_text <" + fdo.Quote(text) + ">\n  mip_end_message\n"
	st := func(src string) []byte {
		return append([]byte{0, 7}, fdo.MustCompile("uni_start_stream\n"+src+"uni_end_stream\n")...)
	}
	if split {
		return [][]byte{st(head), st(body)}
	}
	return [][]byte{st(head + body)}
}

// button0 is the keyword the button labelled so in a host window sends
// (sm_send_token_raw <"Kk", keyword>, read from the compiled stream).
func button0(t *testing.T, src, label string) string {
	t.Helper()
	b := fdo.MustCompile(src)
	i := bytes.Index(b, append([]byte(label), 0x02)) // its title, then its first atom
	if i < 0 {
		t.Fatalf("no button %q:\n%s", label, src)
	}
	j := bytes.Index(b[i:], []byte("Kk"))
	if j < 0 {
		t.Fatalf("button %q sends nothing", label)
	}
	rest := b[i+j+2:]
	n := 0
	for n < len(rest) && rest[n] >= 0x20 && rest[n] < 0x7F {
		n++
	}
	return string(rest[:n])
}

func TestMail(t *testing.T) {
	srv, _ := newServer(t)
	if _, err := srv.B.Store.CreateUser("Carol Ann", "password1", "There", false); err != nil {
		t.Fatal(err)
	}
	a := signedOn(t, srv, "alice")

	// alice writes to Carol Ann, in one stream; then in two
	a.send("m4", mailForm("CarolAnn", "Lunch", "Are you free?\r\nNoon?", false)[0])
	if s := a.stream(); !strings.Contains(s, "mip_message_accepted") {
		t.Errorf("sent:\n%s", s)
	}
	parts := mailForm("CarolAnn", "Again", "<html><b>Bold</b> &amp; plain<br>line 2</html>", true)
	a.send("m4", parts[0])
	if s := a.stream(); !strings.Contains(s, "mip_header_accepted") {
		t.Errorf("header:\n%s", s)
	}
	a.send("m4", parts[1])
	if s := a.stream(); !strings.Contains(s, "mip_message_accepted") {
		t.Errorf("sent:\n%s", s)
	}
	u, _ := srv.B.Store.UserByName("Carol Ann")
	inbox, err := srv.B.Store.Inbox(u.ID)
	if err != nil || len(inbox) != 2 {
		t.Fatalf("inbox %v %v", inbox, err)
	}
	if m := inbox[0]; m.From != "alice" || m.Subject != "Lunch" || m.Body != "Are you free?\nNoon?" {
		t.Errorf("first %+v", m)
	}
	if m := inbox[1]; m.Body != "Bold & plain\nline 2" {
		t.Errorf("4.0's HTML: %q", m.Body)
	}
	// to no one here
	a.send("m4", mailForm("nobody", "Hi", "x", false)[0])
	if s := a.stream(); strings.Contains(s, "mip_message_accepted") {
		t.Errorf("to nobody:\n%s", s)
	}
	if s := a.stream(); !strings.Contains(s, "not sent to") || !strings.Contains(s, "nobody") {
		t.Errorf("to nobody:\n%s", s)
	}

	// Carol is told at sign-on, and reads it by keyword and buttons
	c := connect(t, srv)
	c.send("Dd", signOnForm("CarolAnn", "password1"))
	c.stream()
	if s := c.stream(); !strings.Contains(s, "You have 2 new messages") {
		t.Errorf("welcome:\n%s", s)
	}
	c.send("Kk", deData("Mail"))
	c.stream() // the last mailbox closed
	box := c.stream()
	if !strings.Contains(box, "You have 2 messages, 2 new") {
		t.Errorf("mailbox:\n%s", box)
	}
	kw := button0(t, box, "* "+inbox[0].Created.Format("Jan 2")+"  alice: Lunch")
	c.send("Kk", []byte(kw))
	c.stream()
	letter := c.stream()
	if !strings.Contains(letter, "Subject: Lunch") || !strings.Contains(letter, `Are you free?\r\nNoon?`) {
		t.Errorf("letter:\n%s", letter)
	}
	if m, _ := srv.B.Store.Mail(u.ID, inbox[0].ID); !m.Read {
		t.Error("not marked read")
	}
	c.send("Kk", []byte(button0(t, letter, "Delete")))
	c.stream() // the letter closed
	c.stream() // the last mailbox closed
	if s := c.stream(); !strings.Contains(s, "You have 1 message, 1 new") {
		t.Errorf("after delete:\n%s", s)
	}
	// no one reads another's mail
	a.send("Kk", []byte(fmt.Sprintf("read mail %d", inbox[1].ID)))
	if s := a.stream(); !strings.Contains(s, "not in your mailbox") {
		t.Errorf("another's:\n%s", s)
	}
}

func TestKeywords(t *testing.T) {
	srv, _ := newServer(t)
	a := signedOn(t, srv, "alice")
	a.send("Kk", deData("  HELP "))
	a.stream()
	if s := a.stream(); !strings.Contains(s, "Mail: your mailbox") || button0(t, s, "Chat") != "chat" {
		t.Errorf("help:\n%s", s)
	}
	a.send("Kk", []byte("who"))
	a.stream()
	if s := a.stream(); !strings.Contains(s, "1 caller on now") || !strings.Contains(s, "alice, by") {
		t.Errorf("who:\n%s", s)
	}
	a.send("Kk", []byte("chat"))
	if s := a.stream(); !strings.Contains(s, "man_close") {
		t.Errorf("chat:\n%s", s)
	}
	if s := a.stream(); !strings.Contains(s, "uni_invoke_local <32-117>") {
		t.Errorf("chat:\n%s", s)
	}
	a.stream()
	a.chatLine()
	a.send("Kk", deData("nowhere"))
	if s := a.stream(); !strings.Contains(s, `There is no keyword \"nowhere\" here`) {
		t.Errorf("unknown:\n%s", s)
	}
}

func TestRooms(t *testing.T) {
	srv, _ := newServer(t)
	a := signedOn(t, srv, "alice")
	a.send("Kk", []byte("room tea"))
	a.stream()
	if s := a.stream(); !strings.Contains(s, `man_preset_title <"tea">`) {
		t.Errorf("tea:\n%s", s)
	}
	a.stream()
	a.chatLine()
	// List Rooms, from the room window: LP and an argument
	a.send("LP", []byte{0, 1, 0})
	a.stream()
	s := a.stream()
	if !strings.Contains(s, "2 rooms") {
		t.Errorf("rooms:\n%s", s)
	}
	if k := button0(t, s, "tea (1)"); k != "room tea" {
		t.Errorf("tea's button: %q", k)
	}
	if k := button0(t, s, "main (0)"); k != "room main" {
		t.Errorf("main's button: %q", k)
	}
	a.send("Kk", []byte("room no such room!"))
	if s := a.stream(); !strings.Contains(s, "You can't go into") {
		t.Errorf("bad name:\n%s", s)
	}
}

func TestBoards(t *testing.T) {
	srv, _ := newServer(t)
	st := srv.B.Store
	ar, err := st.CreateArea("General", "Talk about anything", false)
	if err != nil {
		t.Fatal(err)
	}
	a := signedOn(t, srv, "alice")
	a.send("Kk", deData("Boards"))
	a.stream()
	s := a.stream()
	if k := button0(t, s, "General"); k != fmt.Sprintf("board %d", ar.ID) {
		t.Fatalf("boards:\n%s", s)
	}
	a.send("Kk", []byte(fmt.Sprintf("board %d", ar.ID)))
	a.stream()
	s = a.stream()
	if !strings.Contains(s, "There are no topics yet") {
		t.Errorf("board:\n%s", s)
	}
	// a new topic: the post window, and its Send
	a.send("Kk", []byte(button0(t, s, "Start a topic")))
	a.stream()
	if s := a.stream(); !strings.Contains(s, "edit_view") || !strings.Contains(s, `de_ez_send_form <"Zp">`) {
		t.Errorf("post window:\n%s", s)
	}
	a.send("Zp", deData("Hello", "First post\rhere"))
	a.stream() // the post window closed
	a.stream() // the last topic closed
	s = a.stream()
	if !strings.Contains(s, `"Hello"`) || !strings.Contains(s, `First post\r\nhere`) {
		t.Errorf("topic:\n%s", s)
	}
	ms, _ := st.Messages(ar.ID, 0, 10)
	if len(ms) != 1 || ms[0].From != "alice" || ms[0].Subject != "Hello" {
		t.Fatalf("posted %+v", ms)
	}
	// a reply
	a.send("Kk", []byte(button0(t, s, "Reply")))
	a.stream()
	if s := a.stream(); !strings.Contains(s, `"Re: Hello"`) {
		t.Errorf("reply window:\n%s", s)
	}
	a.send("Zp", deData("Re: Hello", "Second"))
	a.stream()
	a.stream()
	if s := a.stream(); !strings.Contains(s, "First post") || !strings.Contains(s, "Second") {
		t.Errorf("topic:\n%s", s)
	}
	ms, _ = st.Messages(ar.ID, 0, 10)
	if len(ms) != 2 || ms[1].ReplyTo != ms[0].ID || ms[1].To != "alice" {
		t.Errorf("reply %+v", ms[1])
	}
	// the board lists it; a send with no window open is refused
	a.send("Kk", []byte(fmt.Sprintf("board %d", ar.ID)))
	a.stream()
	if s := a.stream(); !strings.Contains(s, "Hello (2)") {
		t.Errorf("board:\n%s", s)
	}
	a.send("Zp", deData("x", "y"))
	if s := a.stream(); !strings.Contains(s, "to post") {
		t.Errorf("no window:\n%s", s)
	}
	a.send("Kk", []byte("board 999"))
	if s := a.stream(); !strings.Contains(s, "no such board") {
		t.Errorf("999:\n%s", s)
	}
}

func TestReadMail(t *testing.T) {
	srv, _ := newServer(t)
	st := srv.B.Store
	if _, err := st.CreateUser("Carol Ann", "password1", "There", false); err != nil {
		t.Fatal(err)
	}
	a := signedOn(t, srv, "alice")
	a.send("m4", mailForm("CarolAnn", "Lunch", "Are you free?\r\nNoon?", false)[0])
	a.stream()
	a.send("m4", mailForm("CarolAnn", "Later", "Or tea", false)[0])
	a.stream()
	u, _ := st.UserByName("Carol Ann")
	inbox, _ := st.Inbox(u.ID)
	if len(inbox) != 2 {
		t.Fatalf("inbox %v", inbox)
	}
	c := signedOn(t, srv, "CarolAnn")

	// the New Mail list: both, newest first, each a message of its own
	c.send("m7", nil)
	s := c.stream()
	if strings.Count(s, "mip_start_message") != 3 || strings.Count(s, "mip_start_block") != 2 ||
		!strings.Contains(s, hexText("alice")) || strings.Index(s, hexText("Later")) > strings.Index(s, hexText("Lunch")) {
		t.Errorf("new mail:\n%s", s)
	}
	c.send("m9", nil)
	if s := c.stream(); !strings.Contains(s, "You have no old mail.") {
		t.Errorf("old mail:\n%s", s)
	}

	// reading one, with and without a stream id before the atoms
	m3 := func(id int64) []byte {
		return fdo.MustCompile(fmt.Sprintf("mip_preset_message_id <%d>\nmip_read_mail <1>\n", id))
	}
	c.send("m3", m3(inbox[0].ID))
	s = c.stream()
	if !strings.Contains(s, hexText("Lunch")) || !strings.Contains(s, hexText("Are you free?\r\nNoon?")) || !strings.Contains(s, "mip_end_message") {
		t.Errorf("read:\n%s", s)
	}
	c.send("m3", append([]byte{0, 9}, m3(inbox[1].ID)...))
	if s := c.stream(); !strings.Contains(s, hexText("Or tea")) {
		t.Errorf("read with a stream id:\n%s", s)
	}
	if n, _ := st.UnreadMail(u.ID); n != 0 {
		t.Errorf("%d unread after reading both", n)
	}
	c.send("m9", nil)
	if s := c.stream(); strings.Count(s, "mip_start_block") != 2 {
		t.Errorf("old mail:\n%s", s)
	}
	// a mail not in her mailbox, nor sent by her
	a.send("m3", m3(inbox[1].ID+100))
	if s := a.stream(); !strings.Contains(s, "not in your mailbox") {
		t.Errorf("no such mail:\n%s", s)
	}
	// but alice sees what she sent
	a.send("m8", nil)
	if s := a.stream(); !strings.Contains(s, hexText("Carol Ann")) || strings.Count(s, "mip_start_block") != 2 {
		t.Errorf("sent mail:\n%s", s)
	}

	// Keep As New for the first, Delete for the second
	c.send("m2", fdo.MustCompile(fmt.Sprintf("mip_preset_message_id <%d>\nmip_change_message_status <2>\n"+
		"mip_preset_message_id <%d>\nmip_change_message_status <4>\n", inbox[0].ID, inbox[1].ID)))
	c.stream()
	left, _ := st.Inbox(u.ID)
	if len(left) != 1 || left[0].ID != inbox[0].ID || left[0].Read {
		t.Errorf("after m2: %+v", left)
	}
}

// hexText is text as the decompiler shows a raw atom's data.
func hexText(s string) string {
	var parts []string
	for _, c := range []byte(s) {
		parts = append(parts, fmt.Sprintf("%02xx", c))
	}
	return strings.Join(parts, ", ")
}
