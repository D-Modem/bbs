package fido

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/archive"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// storedMsg is a *.msg file's header and text (FTS-0001's stored message).
type storedMsg struct{ from, to, subject, text string }

func readMsgDir(t *testing.T, dir string) []storedMsg {
	t.Helper()
	var out []storedMsg
	for i := 1; i < 100; i++ {
		b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%d.msg", i)))
		if err != nil {
			continue
		}
		if len(b) < 190 {
			continue
		}
		z := func(b []byte) string { return string(bytes.SplitN(b, []byte{0}, 2)[0]) }
		out = append(out, storedMsg{z(b[0:36]), z(b[36:72]), z(b[72:144]), z(b[190:])})
	}
	return out
}

// run runs a command, its output to a file (a pipe would wait for any
// child it leaves behind).
func run(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	of, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer of.Close()
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = of, of
	err = cmd.Run()
	out, _ := os.ReadFile(of.Name())
	return string(out), err
}

func freePort(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// TestBinkdCrashmail exchanges echomail and netmail with a hub run by
// binkd and crashmail, both ways, calling it and called by it.
func TestBinkdCrashmail(t *testing.T) {
	for _, p := range []string{"binkd", "crashmail", "crashwrite", "zip", "unzip"} {
		if _, err := exec.LookPath(p); err != nil {
			t.Skipf("no %s", p)
		}
	}
	dir := t.TempDir()
	hub := filepath.Join(dir, "hub")
	for _, d := range []string{"inb", "outb", "msg/TEST", "msg/NETMAIL", "msg/BAD", "tmp", "write"} {
		os.MkdirAll(filepath.Join(hub, d), 0700)
	}
	hubAddr, ourAddr := freePort(t), freePort(t)
	prefs := filepath.Join(hub, "crashmail.prefs")
	os.WriteFile(prefs, []byte(strings.ReplaceAll(`SYSOP "Hub Sysop"
LOGFILE "@H@/crashmail.log"
LOGLEVEL 6
DUPEFILE "@H@/dupes" 200
DUPEMODE BAD
LOOPMODE LOG+BAD
DEFAULTZONE 21
INBOUND "@H@/inb"
OUTBOUND "@H@/outb"
TEMPDIR "@H@/tmp"
CREATEPKTDIR "@H@/tmp"
PACKETDIR "@H@/outb"
CHECKSEENBY
WEEKDAYNAMING
PACKER "ZIP" "/usr/bin/zip -j %a %f" "/usr/bin/unzip -j %a" "PK"
AKA 21:1/100.0
DOMAIN "fidonet"
NODE 21:1/200.0 "ZIP" "" PACKNETMAIL
ROUTE "21:1/200.*" "21:1/200.0" 21:1/100.0
NETMAIL "NETMAIL" 21:1/100.0 MSG "@H@/msg/NETMAIL"
AREA "BAD" 21:1/100.0 MSG "@H@/msg/BAD"
AREA "TEST" 21:1/100.0 MSG "@H@/msg/TEST"
EXPORT 21:1/200.0
`, "@H@", hub)), 0600)
	cfg := filepath.Join(hub, "binkd.cfg")
	os.WriteFile(cfg, []byte(strings.ReplaceAll(fmt.Sprintf(`log @H@/binkd.log
loglevel 6
domain fidonet @H@/outb 21
address 21:1/100@fidonet
sysname "Test Hub"
location "Test"
sysop "Hub Sysop"
nodeinfo 115200,TCP,BINKP
listen %s
inbound @H@/inb
inbound-nonsecure @H@/inb
pid-file @H@/binkd.pid
node 21:1/200@fidonet %s secret
`, hubAddr, ourAddr), "@H@", hub)), 0600)
	crashmail := func(args ...string) {
		t.Helper()
		out, err := run(t, "crashmail", append(args, "SETTINGS", prefs)...)
		if err != nil {
			t.Fatalf("crashmail %v: %v\n%s", args, err, out)
		}
	}
	crashwrite := func(text string, args ...string) {
		t.Helper()
		time.Sleep(1100 * time.Millisecond) // crashwrite's MSGIDs are the clock's
		tf := filepath.Join(hub, "text.txt")
		os.WriteFile(tf, []byte(text), 0600)
		out, err := run(t, "crashwrite", append([]string{"DIR", filepath.Join(hub, "write"), "TEXT", tf}, args...)...)
		if err != nil {
			t.Fatalf("crashwrite: %v\n%s", err, out)
		}
		crashmail("TOSSDIR", filepath.Join(hub, "write"), "NOSECURITY")
	}
	binkd := exec.Command("binkd", cfg)
	binkd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // its client manager too
	if err := binkd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		syscall.Kill(-binkd.Process.Pid, syscall.SIGKILL)
		binkd.Wait()
		if t.Failed() {
			l, _ := os.ReadFile(filepath.Join(hub, "binkd.log"))
			c, _ := os.ReadFile(filepath.Join(hub, "crashmail.log"))
			t.Logf("binkd:\n%s\ncrashmail:\n%s", l, c)
		}
	})
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("tcp", hubAddr); err == nil {
			c.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// us: 21:1/200
	st, err := store.Open(filepath.Join(dir, "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.CreateUser("alice", "alicepass1", "", false)
	a, _ := st.CreateArea("General", "Talk", false)
	st.SetEchoTag(a.ID, "TEST")
	lb := &syncBuf{}
	b := bbs.New(bbs.Defaults(), st, nil)
	b.Log.SetOutput(lb)
	link := &Link{Name: "hub", Addr: mustAddr("21:1/100"), Host: hubAddr, Password: "secret",
		Echoes: "TEST", Route: true, Packer: archive.ZIP}
	f, err := New(b, Settings{Addrs: []ftn.Addr{mustAddr("21:1/200")}, System: "Our BBS", Sysop: "Alice",
		Origin: "Our BBS", Dir: filepath.Join(dir, "fido"), Links: []*Link{link}})
	if err != nil {
		t.Fatal(err)
	}
	b.Netmail = f.Netmail
	ln, err := net.Listen("tcp", ourAddr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go (&Server{F: f}).Serve(ctx, ln)
	defer func() {
		if t.Failed() {
			t.Logf("our log:\n%s", lb)
		}
	}()
	f.ScanAll()

	n, _ := b.Enter(&bbs.Caller{Via: "test"}, func() {})
	defer n.Leave()
	u, _ := st.UserByName("alice")
	n.SetUser(u)
	if _, err := n.Post(a, "All", "From the BBS", []string{"Hello, hub. Ünïcödé — CP437"}, 0); err != nil {
		t.Fatal(err)
	}
	if err := n.SendMail("Hub Sysop@21:1/100", "Netmail from the BBS", []string{"Private words"}, 0); err != nil {
		t.Fatal(err)
	}

	// we call the hub: it gets the bundle, crashmail tosses it
	if err := f.Call(ctx, link); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	crashmail("TOSS")
	echo := readMsgDir(t, filepath.Join(hub, "msg/TEST"))
	if len(echo) != 1 || echo[0].subject != "From the BBS" || echo[0].from != "alice" ||
		!strings.Contains(echo[0].text, "Hello, hub. \x9a") || !strings.Contains(echo[0].text, " * Origin: Our BBS (21:1/200)") ||
		!strings.Contains(echo[0].text, "\x01MSGID: 21:1/200 ") {
		t.Fatalf("the hub's TEST: %q", echo)
	}
	net1 := readMsgDir(t, filepath.Join(hub, "msg/NETMAIL"))
	if len(net1) != 1 || net1[0].to != "Hub Sysop" || !strings.Contains(net1[0].text, "Private words") {
		t.Fatalf("the hub's NETMAIL: %q", net1)
	}

	// the hub writes back: echomail and netmail, which crashmail packs for
	// us; we call and get them
	crashwrite("A reply from the hub.\r", "FROMNAME", "Hub Sysop", "FROMADDR", "21:1/100", "TONAME", "alice",
		"SUBJECT", "Re: From the BBS", "AREA", "TEST", "ORIGIN", "The hub")
	crashwrite("Netmail back.\r", "FROMNAME", "Hub Sysop", "FROMADDR", "21:1/100", "TONAME", "alice",
		"TOADDR", "21:1/200", "SUBJECT", "Re: Netmail from the BBS")
	if err := f.Call(ctx, link); err != nil {
		t.Fatal(err)
	}
	ms, _ := st.Messages(a.ID, 0, 10)
	if len(ms) != 2 || ms[1].Subject != "Re: From the BBS" || ms[1].From != "Hub Sysop" ||
		!strings.Contains(ms[1].Body, "A reply from the hub.") || ms[1].Origin != "ftn:hub" {
		t.Fatalf("our General: %+v", ms)
	}
	in, _ := st.Inbox(u.ID)
	if len(in) != 1 || in[0].From != "Hub Sysop@21:1/100" || !strings.Contains(in[0].Body, "Netmail back.") {
		t.Fatalf("alice's inbox: %+v", in)
	}

	// the hub calls us
	crashwrite("Called you.\r", "FROMNAME", "Hub Sysop", "FROMADDR", "21:1/100", "TONAME", "All",
		"SUBJECT", "Polling", "AREA", "TEST", "ORIGIN", "The hub")
	out, err := run(t, "binkd", "-p", "-P", "21:1/200@fidonet", cfg)
	if err != nil {
		t.Fatalf("binkd -P: %v\n%s", err, out)
	}
	for i := 0; i < 100; i++ {
		if ms, _ = st.Messages(a.ID, 0, 10); len(ms) == 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(ms) != 3 || ms[2].Subject != "Polling" {
		t.Fatalf("after the hub called: %+v\n%s", ms, out)
	}
	if !strings.Contains(lb.String(), "binkp hub (") || !strings.Contains(lb.String(), "logged in (secure)") {
		t.Errorf("our log: %s", lb)
	}
}
