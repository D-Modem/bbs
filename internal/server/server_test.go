package server

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
)

func init() { store.BcryptCost = 4 } // fast hashes for tests

type env struct {
	t   *testing.T
	b   *bbs.BBS
	dir string
}

// start runs a BBS with the listeners (their Address a socket name in a
// temporary directory) and two users, alice and sysop.
func start(t *testing.T, tweak func(*bbs.Settings), ls ...*Listener) *env {
	t.Helper()
	return startGraphics(t, ui3270.Graphics{}, tweak, ls...)
}

// startGraphics is start with 3270 graphics settings.
func startGraphics(t *testing.T, g ui3270.Graphics, tweak func(*bbs.Settings), ls ...*Listener) *env {
	t.Helper()
	dir, err := os.MkdirTemp("", "bbs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	st, err := store.Open(filepath.Join(dir, "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.CreateUser("alice", "alicepass1", "Here", false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser("sysop", "sysoppass1", "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateArea("General", "Talk", false); err != nil {
		t.Fatal(err)
	}
	s := bbs.Defaults()
	s.Name = "Test BBS"
	s.FailDelay = 0
	if tweak != nil {
		tweak(&s)
	}
	var logbuf bytes.Buffer
	b := bbs.New(s, st, log.New(&logbuf, "", 0))
	for _, l := range ls {
		l.Address = filepath.Join(dir, l.Address)
		l.SocketMode = 0600
		if l.HeaderTimeout == 0 {
			l.HeaderTimeout = 2 * time.Second
		}
		if l.Via == "" {
			l.Via = l.Kind
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv := New(b, ls)
		srv.Graphics = g
		srv.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		if t.Failed() {
			t.Log("log:\n" + logbuf.String())
		}
	})
	for _, l := range ls {
		for i := 0; ; i++ {
			if _, err := os.Stat(l.Address); err == nil {
				break
			}
			if i > 200 {
				t.Fatalf("%s never listened", l.Address)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return &env{t, b, dir}
}

// dialog is a connection to the BBS, read as text.
type dialog struct {
	t   *testing.T
	c   net.Conn
	got []byte
}

func (e *env) dial(name string) *dialog {
	c, err := net.Dial("unix", filepath.Join(e.dir, name))
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { c.Close() })
	return &dialog{t: e.t, c: c}
}

func (d *dialog) send(s string) {
	d.t.Helper()
	if _, err := io.WriteString(d.c, s); err != nil {
		d.t.Fatalf("send %q: %v", s, err)
	}
}

// expect reads until want comes (and drops what came up to it).
func (d *dialog) expect(want string) {
	d.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 4096)
	for {
		if i := bytes.Index(d.got, []byte(want)); i >= 0 {
			d.got = d.got[i+len(want):]
			return
		}
		d.c.SetReadDeadline(deadline)
		n, err := d.c.Read(buf)
		d.got = append(d.got, buf[:n]...)
		if err != nil {
			d.t.Fatalf("waiting for %q: %v; got %q", want, err, d.got)
		}
	}
}

func TestHeaderTrust(t *testing.T) {
	e := start(t, nil, &Listener{Name: "menu", Kind: "raw", Address: "menu.sock", ANSI: "no",
		Header: true, TrustUser: true, CreateUsers: true})
	d := e.dial("menu.sock")
	d.send("MODEM_USER=erin\nMODEM_AUTH=pam\nMODEM_RATE=26400/31200\nMODEM_CALLERID=5551234\n\n")
	d.expect("modem at 26400 bit/s")
	d.expect("Welcome, erin.")
	d.expect("Command: ")
	d.send("W")
	d.expect("erin")
	d.expect("modem 26400")
	d.send("x")
	d.expect("Command: ")
	d.send("GY")
	d.expect("Thanks for calling.")
	u, err := e.b.Store.UserByName("erin")
	if err != nil || !u.External {
		t.Fatalf("erin: %+v, %v", u, err)
	}
	// an external account cannot log in by password
	if _, err := e.b.Store.CheckPassword("erin", ""); err == nil {
		t.Error("external account logged in with no password")
	}
}

func TestHeaderUntrusted(t *testing.T) {
	// without trust_user a header's user is not believed
	e := start(t, nil, &Listener{Name: "menu", Kind: "raw", Address: "menu.sock", ANSI: "no", Header: true})
	d := e.dial("menu.sock")
	d.send("MODEM_USER=sysop\nDMODEM_RATE=33600/33600\n\n")
	d.expect("Name (or NEW): ")
}

func TestLoginAndPost(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	d := e.dial("modem.sock")
	d.expect("Name (or NEW): ")
	d.send("alice\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("Welcome, alice.")
	d.expect("Command: ")
	d.send("M")
	d.expect("[Q]uit: ")
	d.send("1\r")
	d.expect("[P]ost, [Q]uit: ")
	d.send("P\r")
	d.expect("To [All]: ")
	d.send("\r")
	d.expect("Subject: ")
	d.send("Hi \x1b[2Jthere\r")
	d.expect("clears all.")
	d.send("First line\r\x1b]0;title\x07Second\r/s\r")
	d.expect("Posted as #1.")
	m, err := e.b.Store.Message(1)
	if err != nil {
		t.Fatal(err)
	}
	if m.To != "All" || m.From != "alice" || strings.ContainsAny(m.Subject+m.Body, "\x1b\x07") {
		t.Errorf("message: %+v", m)
	}
}

func TestRLogin(t *testing.T) {
	e := start(t, nil, &Listener{Name: "rlogin", Kind: "rlogin", Address: "rlogin.sock", ANSI: "auto"})
	d := e.dial("rlogin.sock")
	d.send("\x00alicepass1\x00alice\x00ansi/9600\x00")
	d.expect("\x00")
	d.expect("rlogin at 9600 bit/s")
	d.expect("Welcome, ")
	d.expect("alice")

	// a wrong password: asked again, the name typed in already
	d = e.dial("rlogin.sock")
	d.send("\x00wrong\x00alice\x00vt100/2400\x00")
	d.expect("Name")
	d.expect("alice")
	d.send("\r")
	d.expect("Password: ")
}

func TestRLoginTrust(t *testing.T) {
	e := start(t, nil, &Listener{Name: "rlogin", Kind: "rlogin", Address: "rlogin.sock", ANSI: "no", TrustUser: true})
	d := e.dial("rlogin.sock")
	d.send("\x00\x00alice\x00dumb/33600\x00")
	d.expect("Welcome, alice.")
}

func TestThrottle(t *testing.T) {
	e := start(t, func(s *bbs.Settings) { s.ThrottleFails = 2; s.Attempts = 5 },
		&Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no", Header: true})
	// failures count against the caller ID
	d := e.dial("modem.sock")
	d.send("DMODEM_CALLERID=5550001\n\n")
	for i := 0; i < 2; i++ {
		d.expect("Name (or NEW): ")
		d.send("alice\rwrong-pass\r")
		d.expect("wrong name or password.")
	}
	d.expect("Name (or NEW): ")
	d.send("alice\ralicepass1\r")
	d.expect("too many failed logins")

	// another caller is not locked out
	d = e.dial("modem.sock")
	d.send("DMODEM_CALLERID=5550002\n\n")
	d.expect("Name (or NEW): ")
	d.send("alice\ralicepass1\r")
	d.expect("Welcome, alice.")
}

func TestBusy(t *testing.T) {
	e := start(t, func(s *bbs.Settings) { s.MaxNodes = 1 },
		&Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	d1 := e.dial("modem.sock")
	d1.expect("Node 1")
	d2 := e.dial("modem.sock")
	d2.expect("All lines are busy")
}

func TestNewUser(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	d := e.dial("modem.sock")
	d.expect("Name (or NEW): ")
	d.send("new\r")
	d.expect("give up): ")
	d.send("alice\r")
	d.expect("That name is taken.")
	d.send("Zed\r")
	d.expect("Password (8 or more characters): ")
	d.send("zedpassword\rzedpassword\r")
	d.expect("calling from? ")
	d.send("Mars\r")
	d.expect("Welcome, Zed.")
	if u, err := e.b.Store.CheckPassword("zed", "zedpassword"); err != nil || u.Location != "Mars" {
		t.Errorf("Zed: %+v, %v", u, err)
	}
}

func TestTelnetANSI(t *testing.T) {
	e := start(t, nil, &Listener{Name: "telnet", Kind: "telnet", Address: "telnet.sock",
		TN3270: true, ANSI: "auto", UTF8: "auto"})
	c, err := net.Dial("unix", filepath.Join(e.dir, "telnet.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	tc := &tnClient{c: c, ttype: "ANSI"}
	tc.expect(t, "\x1b[1;36mTest BBS")
	tc.expect(t, "Name")
}

// tnClient is a telnet client that answers the server and keeps the data.
type tnClient struct {
	c     net.Conn
	ttype string
	got   []byte
	recs  [][]byte // 3270 records, split at IAC EOR
	cur   []byte
}

const (
	iac, sb, se, will, wont, do, dont, eor = 255, 250, 240, 251, 252, 253, 254, 239
)

func (tc *tnClient) read(t *testing.T) {
	t.Helper()
	buf := make([]byte, 4096)
	tc.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := tc.c.Read(buf)
	if err != nil {
		t.Fatalf("read: %v; got %q", err, tc.got)
	}
	b := buf[:n]
	for i := 0; i < len(b); i++ {
		if b[i] != iac {
			tc.got = append(tc.got, b[i])
			tc.cur = append(tc.cur, b[i])
			continue
		}
		// a command split across reads: IAC x, or IAC x option
		need := func() int {
			if i+1 < len(b) && b[i+1] != iac && b[i+1] != eor {
				return 2
			}
			return 1
		}
		for i+need() >= len(b) {
			m, err := tc.c.Read(buf[n:])
			if err != nil {
				t.Fatal(err)
			}
			n += m
			b = buf[:n]
		}
		switch b[i+1] {
		case iac:
			tc.got = append(tc.got, iac)
			tc.cur = append(tc.cur, iac)
			i++
		case eor:
			tc.recs = append(tc.recs, tc.cur)
			tc.cur = nil
			i++
		case do:
			switch b[i+2] {
			case 24, 25, 0, 3, 31:
				tc.c.Write([]byte{iac, will, b[i+2]})
			default:
				tc.c.Write([]byte{iac, wont, b[i+2]})
			}
			i += 2
		case will:
			tc.c.Write([]byte{iac, do, b[i+2]})
			i += 2
		case dont:
			tc.c.Write([]byte{iac, wont, b[i+2]})
			i += 2
		case wont:
			i += 2
		case sb:
			j := bytes.Index(b[i:], []byte{iac, se})
			if j < 0 {
				t.Fatal("split subnegotiation")
			}
			if b[i+2] == 24 && b[i+3] == 1 {
				msg := append([]byte{iac, sb, 24, 0}, tc.ttype...)
				tc.c.Write(append(msg, iac, se))
			}
			i += j + 1
		default:
			i++
		}
	}
}

func (tc *tnClient) expect(t *testing.T, want string) {
	t.Helper()
	for !bytes.Contains(tc.got, []byte(want)) {
		tc.read(t)
	}
}

// record waits for the next 3270 record.
func (tc *tnClient) record(t *testing.T) []byte {
	t.Helper()
	for len(tc.recs) == 0 {
		tc.read(t)
	}
	r := tc.recs[0]
	tc.recs = tc.recs[1:]
	return r
}

func TestTN3270(t *testing.T) {
	e := start(t, nil, &Listener{Name: "telnet", Kind: "telnet", Address: "telnet.sock",
		TN3270: true, ANSI: "auto", UTF8: "auto"})
	c, err := net.Dial("unix", filepath.Join(e.dir, "telnet.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	tc := &tnClient{c: c, ttype: "IBM-3278-2-E"}
	cp := go3270.Codepage1047()
	// go3270 clears the screen, then asks what the terminal is: no
	// answer but an empty query reply
	var screen string
	for i := 0; i < 5 && !strings.Contains(screen, "Logon"); i++ {
		rec := tc.record(t)
		if len(rec) > 0 && rec[0] == 0xf3 { // write structured field: the query
			tc.c.Write([]byte{0x88, iac, eor})
			continue
		}
		screen = cp.Decode(rec)
	}
	if !strings.Contains(screen, "Logon") || !strings.Contains(screen, "Test BBS") {
		t.Fatalf("no logon screen: %q", screen)
	}
	// PF3: exit
	tc.c.Write([]byte{0xf3, 0x40, 0x40, iac, eor})
	bye := cp.Decode(tc.record(t))
	if !strings.Contains(bye, "Thanks for calling.") {
		t.Errorf("goodbye screen: %q", bye)
	}
	nodes := e.b.Nodes()
	if len(nodes) > 1 {
		t.Errorf("nodes: %+v", nodes)
	}
}

// a PCOMM-like client's query replies: usable area with a 9x16 cell,
// character sets with Load PS (formats 1 and 3) and two loadable sets, and
// with vector, the Segment reply
func queryReply(vector bool) []byte {
	usable := []byte{0x81, 0x81, 0x01, 0x00, 0, 80, 0, 24, 0x01, 0, 1, 0, 0x50, 0, 1, 0, 0x50, 9, 16}
	charsets := []byte{0x81, 0x85, 0xb2, 0x00, 9, 16, 0x50, 0, 0, 0, 7,
		0x00, 0x00, 0x00, 0x02, 0xb9, 0x04, 0x17,
		0x01, 0x00, 0xf1, 0x03, 0xc3, 0x01, 0x36,
		0x02, 0x80, 0xff, 0x00, 0x00, 0x00, 0x00,
		0x03, 0x80, 0xff, 0x00, 0x00, 0x00, 0x00}
	replies := [][]byte{usable, charsets}
	if vector {
		replies = append(replies, []byte{0x81, 0xb0, 0x00})
	}
	r := []byte{0x88}
	for _, q := range replies {
		r = append(r, byte((len(q)+2)>>8), byte(len(q)+2))
		r = append(r, q...)
	}
	var out []byte
	for _, b := range r {
		out = append(out, b)
		if b == iac {
			out = append(out, iac)
		}
	}
	return append(out, iac, eor)
}

// graphicsSession connects a PCOMM-like client and returns the records it
// gets up to and including the logon screen, and the one after it.
func graphicsSession(t *testing.T, g ui3270.Graphics, vector bool) (before [][]byte, logon []byte, after []byte, e *env) {
	e = startGraphics(t, g, nil, &Listener{Name: "telnet", Kind: "telnet", Address: "telnet.sock",
		TN3270: true, ANSI: "auto", UTF8: "auto"})
	c, err := net.Dial("unix", filepath.Join(e.dir, "telnet.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	tc := &tnClient{c: c, ttype: "IBM-3278-2-E"}
	cp := go3270.Codepage1047()
	for i := 0; i < 10; i++ {
		rec := tc.record(t)
		if len(rec) > 3 && rec[0] == 0xf3 && rec[3] == 0x01 { // WSF Read Partition: the query
			tc.c.Write(queryReply(vector))
			continue
		}
		if strings.Contains(cp.Decode(rec), "Logon") {
			logon = rec
			break
		}
		before = append(before, rec)
	}
	if logon == nil {
		t.Fatal("no logon screen")
	}
	if vector {
		after = tc.record(t)
	}
	return before, logon, after, e
}

func TestProgrammedSymbolsLogo(t *testing.T) {
	before, logon, _, e := graphicsSession(t, ui3270.Graphics{Symbols: true}, false)
	// the logo's symbols are loaded before the logon screen: WSF, Load PS
	// into the first loadable set (RWS 2) as LCID C1, format 1
	var lps []byte
	for _, r := range before {
		if len(r) > 8 && r[0] == 0xf3 && r[3] == 0x06 {
			lps = r
		}
	}
	if lps == nil {
		t.Fatalf("no Load PS among %x", before)
	}
	if lps[4]&0x0f != 1 || lps[5] != 0xc1 || lps[6] != 0x41 || lps[7] != 0x02 {
		t.Errorf("Load PS header % x", lps[:8])
	}
	if n := (len(lps) - 8) % 18; n != 0 {
		t.Errorf("Load PS data is not whole format 1 symbols: %d bytes", len(lps)-8)
	}
	// the logon screen shows them: a field in character set C1
	if !bytes.Contains(logon, []byte{0x43, 0xc1}) {
		t.Errorf("no character set C1 field on the logon screen")
	}
	_ = e
}

func TestNoSymbolsWithoutSupport(t *testing.T) {
	// the plain TN3270 client of TestTN3270 says nothing of symbols: none
	// are loaded even with the setting on
	e := startGraphics(t, ui3270.Graphics{Symbols: true, Vector: true}, nil,
		&Listener{Name: "telnet", Kind: "telnet", Address: "telnet.sock", TN3270: true})
	c, err := net.Dial("unix", filepath.Join(e.dir, "telnet.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	tc := &tnClient{c: c, ttype: "IBM-3278-2-E"}
	cp := go3270.Codepage1047()
	for i := 0; i < 5; i++ {
		rec := tc.record(t)
		if len(rec) > 0 && rec[0] == 0xf3 {
			if len(rec) > 3 && rec[3] != 0x01 {
				t.Fatalf("a structured field other than the query: % x", rec)
			}
			tc.c.Write([]byte{0x88, iac, eor})
			continue
		}
		if strings.Contains(cp.Decode(rec), "Logon") {
			if bytes.Contains(rec, []byte{0x43, 0xc1}) {
				t.Error("symbols used without support")
			}
			return
		}
	}
	t.Fatal("no logon screen")
}

func TestVectorLogo(t *testing.T) {
	_, logon, after, _ := graphicsSession(t, ui3270.Graphics{Symbols: true, Vector: true}, true)
	if bytes.Contains(logon, []byte{0x43, 0xc1}) {
		t.Error("symbols as well as vector graphics")
	}
	// after the screen: WSF, Object Picture with a segment
	if len(after) < 12 || after[0] != 0xf3 || after[3] != 0x0f || after[4] != 0x10 || after[8] != 0x70 {
		t.Fatalf("no Object Picture after the logon screen: % x", after[:min(len(after), 16)])
	}
}
