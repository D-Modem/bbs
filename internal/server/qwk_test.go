package server

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
	"github.com/Neo-Desktop/bbs-sec/internal/xfer"
)

// qwkSetup posts a message for alice's packet, and makes her REP.
func qwkSetup(t *testing.T, e *env) []byte {
	t.Helper()
	gen, _ := e.b.Store.AreaByName("General")
	sysop, _ := e.b.Store.UserByName("sysop")
	n := e.b.Agent(sysop, "test")
	if _, err := n.Post(gen, "All", "Offline", []string{"Take me away."}, 0); err != nil {
		t.Fatal(err)
	}
	var rep bytes.Buffer
	qwk.WriteREP(&rep, "TESTBBS", []qwk.Message{{Conf: int(gen.ID), To: "sysop", Subject: "Re: Offline", Ref: 1,
		Body: "Taken."}})
	return rep.Bytes()
}

func qwkCheck(t *testing.T, e *env, packet []byte) {
	t.Helper()
	p, err := qwk.ReadPacket(packet)
	if err != nil || p.BBSID != "TESTBBS" || len(p.Messages) != 1 || p.Messages[0].Body != "Take me away." {
		t.Fatalf("packet %+v, %v", p, err)
	}
	gen, _ := e.b.Store.AreaByName("General")
	ms, _ := e.b.Store.Messages(gen.ID, 1, 10)
	if len(ms) != 1 || ms[0].From != "alice" || ms[0].Body != "Taken." {
		t.Errorf("replies %+v", ms)
	}
}

func TestQWKByZMODEM(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	rep := qwkSetup(t, e)
	d := e.dial("modem.sock")
	d.expect("Name (or NEW): ")
	d.send("alice\ralicepass1\r")
	d.expect("Command: ")
	d.send("O")
	d.expect("1 in 1 areas")
	d.expect("[Q]uit: ")
	d.send("D\r")
	d.expect("Protocol, [Q]uit: ")
	d.send("Z")
	d.expect("ZMODEM download now")
	var got []*memIn
	if err := xfer.ReceiveZMODEM(d.c, func(name string, size int64) (xfer.Incoming, error) {
		m := &memIn{name: name}
		got = append(got, m)
		return m, nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].name != "TESTBBS.QWK" {
		t.Fatalf("got %d files", len(got))
	}
	d.got = nil
	d.expect("1 messages packed")
	d.expect("[Q]uit: ")
	d.send("U\r")
	d.expect("Protocol, [Q]uit: ")
	d.send("Z")
	d.expect("ZMODEM upload now")
	if err := xfer.SendZMODEM(d.c, []xfer.Outgoing{{Name: "TESTBBS.REP", Size: int64(len(rep)),
		ModTime: time.Now(), Data: bytes.NewReader(rep)}}); err != nil {
		t.Fatal(err)
	}
	d.got = nil
	d.expect("Your replies: 1 posted, 0 mailed.")
	qwkCheck(t, e, got[0].buf.Bytes())
}

// a 3270's caller takes the packet and brings the replies by IND$FILE
func TestQWKByIndFile(t *testing.T) {
	if _, err := exec.LookPath("s3270"); err != nil {
		t.Skip("no s3270")
	}
	addr := freeAddr(t)
	e := start(t, nil, &Listener{Name: "telnet", Kind: "telnet", Address: addr, TN3270: true})
	rep := qwkSetup(t, e)
	dir := t.TempDir()
	up, down := filepath.Join(dir, "up.rep"), filepath.Join(dir, "down.qwk")
	os.WriteFile(up, rep, 0600)
	script := strings.Join([]string{
		"Connect(" + addr + ")", "Wait(10,InputField)",
		`String("alice")`, "Tab()", `String("alicepass1")`, "Enter()", "Wait(5,InputField)",
		`String("o")`, "Enter()", "Wait(5,InputField)",
		"Transfer(Direction=receive,Host=tso,LocalFile=" + down + ",HostFile=TESTBBS.QWK,Mode=binary,Exist=replace)", "Wait(3,Seconds)",
		"Transfer(Direction=send,Host=tso,LocalFile=" + up + ",HostFile=TESTBBS.REP,Mode=binary)", "Wait(3,Seconds)",
		"Ascii()", "Quit()", ""}, "\n")
	cmd := exec.Command("s3270", "-model", "3279-2")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("s3270: %v\n%s", err, out)
	}
	packet, _ := os.ReadFile(down)
	qwkCheck(t, e, packet)
	if !strings.Contains(string(out), "1 posted") {
		t.Errorf("no word of the replies:\n%s", out)
	}
}

// freeAddr is a local TCP address free to listen on.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}
