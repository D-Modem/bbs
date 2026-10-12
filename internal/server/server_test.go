package server

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
	"github.com/Neo-Desktop/bbs-sec/internal/xfer"
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
	return startFull(t, g, tweak, nil, ls...)
}

// startFull is start with graphics settings, and prep to set the BBS up
// before it answers (dir: the test's directory).
func startFull(t *testing.T, g ui3270.Graphics, tweak func(*bbs.Settings), prep func(b *bbs.BBS, dir string), ls ...*Listener) *env {
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
	if _, err := st.CreateFileArea("Uploads", "", false); err != nil {
		t.Fatal(err)
	}
	if b.Files, err = files.Open(st, filepath.Join(dir, "files"), 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	for _, l := range ls {
		if !strings.Contains(l.Address, ":") { // host:port stays TCP
			l.Address = filepath.Join(dir, l.Address)
		}
		l.SocketMode = 0600
		if l.HeaderTimeout == 0 {
			l.HeaderTimeout = 2 * time.Second
		}
		if l.Via == "" {
			l.Via = l.Kind
		}
	}
	if prep != nil {
		prep(b, dir)
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
			if c, err := net.Dial("tcp", l.Address); err == nil {
				c.Close()
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
	d.expect("Send a node a message (its number, Enter for none): ")
	d.send("\r")
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
	// answers to the BBS's terminal queries: device attributes (CSI c)
	// and the cell size (CSI 16 t), "" for none
	da, cell string
	asked    int
	got      []byte
	recs     [][]byte // 3270 records, split at IAC EOR
	cur      []byte
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

// answer answers the terminal queries that have come.
func (tc *tnClient) answer() {
	for n := bytes.Count(tc.got, []byte("\x1b[c")); tc.asked < n; tc.asked++ {
		if tc.cell != "" {
			tc.c.Write([]byte(tc.cell))
		}
		if tc.da != "" {
			tc.c.Write([]byte(tc.da))
		}
	}
}

func (tc *tnClient) expect(t *testing.T, want string) {
	t.Helper()
	for !bytes.Contains(tc.got, []byte(want)) {
		tc.read(t)
		tc.answer()
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

func TestSixelLogo(t *testing.T) {
	e := start(t, nil, &Listener{Name: "telnet", Kind: "telnet", Address: "telnet.sock",
		TN3270: true, ANSI: "auto", UTF8: "auto", Sixel: "auto"})
	c, err := net.Dial("unix", filepath.Join(e.dir, "telnet.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	tc := &tnClient{c: c, ttype: "xterm-256color", da: "\x1b[?63;1;4;22c", cell: "\x1b[6;20;10t"}
	tc.expect(t, "\x1bP0;1;0q")
	tc.expect(t, "\x1b\\")
	tc.expect(t, "Test BBS")
	// the name, two rows of 20 pels high: 13 pels of font scaled by 3
	if !bytes.Contains(tc.got, []byte("\"1;1;")) {
		t.Error("no raster attributes")
	}
	tc.expect(t, "Name")
}

func TestNoSixel(t *testing.T) {
	e := start(t, nil, &Listener{Name: "telnet", Kind: "telnet", Address: "telnet.sock",
		TN3270: true, ANSI: "auto", UTF8: "auto", Sixel: "auto"})
	c, err := net.Dial("unix", filepath.Join(e.dir, "telnet.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// a VT220-like terminal: attributes without 4
	tc := &tnClient{c: c, ttype: "xterm", da: "\x1b[?62;1;6;22c"}
	tc.expect(t, "Name")
	if bytes.Contains(tc.got, []byte("\x1bP")) {
		t.Error("sixel sent to a terminal without it")
	}
}

// memIn is a file received by the test's terminal program.
type memIn struct {
	name string
	buf  bytes.Buffer
	ok   bool
}

func (m *memIn) Write(p []byte) (int, error) { return m.buf.Write(p) }
func (m *memIn) Commit() error               { m.ok = true; return nil }
func (m *memIn) Abort()                      {}

func TestFileTransfers(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	d := e.dial("modem.sock")
	d.expect("Name (or NEW): ")
	d.send("alice\ralicepass1\r")
	d.expect("Command: ")
	d.send("F")
	d.expect("[Q]uit: ")
	d.send("1\r")
	d.expect("[U]pload, [Q]uit: ")

	// upload by ZMODEM
	zdata := bytes.Repeat([]byte("ZMODEM \x18\x11\x13\r\x00\xff "), 3000)
	d.send("U\r")
	d.expect("Protocol, [Q]uit: ")
	d.send("Z")
	d.expect("ZMODEM upload now")
	if err := xfer.SendZMODEM(d.c, []xfer.Outgoing{{Name: "C:\\DOS\\z file.bin", Size: int64(len(zdata)),
		ModTime: time.Now(), Data: bytes.NewReader(zdata)}}); err != nil {
		t.Fatal(err)
	}
	d.got = nil
	d.expect("Received z file.bin")
	d.expect("Describe it: ")
	d.send("By ZMODEM\r")

	// upload by Kermit
	kdata := bytes.Repeat([]byte{1, 2, 3, '#', 0x80, 0xff}, 5000)
	d.expect("[U]pload, [Q]uit: ")
	d.send("U\r")
	d.expect("Protocol, [Q]uit: ")
	d.send("K")
	d.expect("Kermit upload now")
	if err := xfer.SendKermit(d.c, []xfer.Outgoing{{Name: "k.bin", Size: int64(len(kdata)),
		ModTime: time.Now(), Data: bytes.NewReader(kdata)}}); err != nil {
		t.Fatal(err)
	}
	d.got = nil
	d.expect("Received k.bin")
	d.expect("Describe it: ")
	d.send("\r")

	a, _ := e.b.Store.FileAreaByName("Uploads")
	for name, want := range map[string][]byte{"z file.bin": zdata, "k.bin": kdata} {
		f, err := e.b.Store.FileByName(a.ID, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r, _ := e.b.Files.OpenFile(f)
		got, _ := io.ReadAll(r)
		r.Close()
		if !bytes.Equal(got, want) {
			t.Errorf("%s: %d bytes stored", name, len(got))
		}
	}

	// download the ZMODEM one by YMODEM (files are listed by name: k.bin
	// is 1, "z file.bin" 2)
	d.expect("[U]pload, [Q]uit: ")
	d.send("2\r")
	d.expect("[Q]uit: ")
	d.send("D")
	d.expect("Protocol, [Q]uit: ")
	d.send("Y")
	d.expect("YMODEM (batch) download now")
	var got []*memIn
	err := xfer.ReceiveYMODEM(d.c, func(name string, size int64) (xfer.Incoming, error) {
		m := &memIn{name: name}
		got = append(got, m)
		return m, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].name != "z file.bin" || !bytes.Equal(got[0].buf.Bytes(), zdata) {
		t.Fatalf("YMODEM got %d files", len(got))
	}
	d.got = nil
	d.expect("Sent z file.bin")

	// and by XMODEM-1K
	d.expect("[Q]uit: ")
	d.send("D")
	d.expect("Protocol, [Q]uit: ")
	d.send("1")
	d.expect("XMODEM-1K download now")
	var x memIn
	if err := xfer.ReceiveXMODEM(d.c, func(string, int64) (xfer.Incoming, error) { return &x, nil }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(x.buf.Bytes(), zdata) {
		t.Errorf("XMODEM got %d bytes", x.buf.Len())
	}
	d.got = nil
	d.expect("Sent z file.bin")
	f, _ := e.b.Store.FileByName(a.ID, "z file.bin")
	if f.Downloads != 2 || f.Description != "By ZMODEM" {
		t.Errorf("record %+v", f)
	}
}

// IND$FILE with a real 3270 emulator: s3270 uploads a binary file into
// the file area, downloads it again, and asks for one that is not there
func TestIndFile(t *testing.T) {
	if _, err := exec.LookPath("s3270"); err != nil {
		t.Skip("no s3270")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	e := start(t, nil, &Listener{Name: "telnet", Kind: "telnet", Address: addr, TN3270: true})
	dir := t.TempDir()
	up, down := filepath.Join(dir, "up.bin"), filepath.Join(dir, "down.bin")
	want := make([]byte, 40000)
	for i := range want {
		want[i] = byte(i * 7)
	}
	os.WriteFile(up, want, 0600)
	script := strings.Join([]string{
		"Connect(" + addr + ")", "Wait(10,InputField)",
		`String("alice")`, "Tab()", `String("alicepass1")`, "Enter()", "Wait(5,InputField)",
		`String("f")`, "Enter()", "Wait(5,InputField)",
		`String("s")`, "Enter()", "Wait(5,InputField)",
		// s3270 4.1's Transfer returns before the transfer ends: wait
		"Transfer(Direction=send,Host=tso,LocalFile=" + up + ",HostFile=up.bin,Mode=binary)", "Wait(3,Seconds)",
		"Transfer(Direction=receive,Host=tso,LocalFile=" + down + ",HostFile=up.bin,Mode=binary,Exist=replace)", "Wait(3,Seconds)",
		"Transfer(Direction=receive,Host=tso,LocalFile=" + filepath.Join(dir, "x") + ",HostFile=nosuch,Mode=binary,Exist=replace)", "Wait(3,Seconds)",
		"Ascii()", "Quit()", ""}, "\n")
	cmd := exec.Command("s3270", "-model", "3279-2")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("s3270: %v\n%s", err, out)
	}
	a, _ := e.b.Store.FileAreaByName("Uploads")
	f, err := e.b.Store.FileByName(a.ID, "up.bin")
	if err != nil || f.Size != int64(len(want)) || f.Downloads != 1 {
		t.Fatalf("stored: %+v, %v\n%s", f, err, out)
	}
	got, _ := os.ReadFile(down)
	if !bytes.Equal(got, want) {
		t.Errorf("downloaded %d bytes", len(got))
	}
	if !strings.Contains(string(out), "File not found") {
		t.Errorf("no not-found message:\n%s", out)
	}
}
