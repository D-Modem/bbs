package server

import (
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
)

// TestVT220: a VT220 on a raw line says what it is: it gets DEC line
// drawing and Latin-1, bold for colors, and shows as a VT220.
func TestVT220(t *testing.T) {
	e := startFull(t, ui3270.Graphics{}, nil, screensOnly(map[string]string{"welcome.asc": "┌──┐ café ░\n"}),
		&Listener{Name: "vt", Kind: "raw", Address: "vt.sock", ANSI: "auto", UTF8: "auto", Sixel: "auto", RIP: "auto", VT: "auto"})
	d := e.dial("vt.sock")
	d.expect("\x1b[6n")
	d.send("\x1b[24;80R") // ANSI
	d.expect("\x1b[!\x1b[6n")
	d.send("\x1b[24;80R") // no RIP
	d.expect("\x05\x1b[c\x1b[>c\x1b[>0q\x1b[16t\x1b[6n")
	d.send("\x1b[?62;1;2;6;7;8;9c\x1b[>1;10;0c\x1b[24;80R")
	d.expect("Name")
	d.send("alice\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("\x1b(0lqqk\x1b(B caf\xe9 \x1b(0a\r\n\x1b(B")
	d.expect("\x1b[1m[M]\x1b[0m") // bold, not yellow
	d.expect("Command: ")
	if n := e.b.Nodes(); len(n) != 1 || n[0].Terminal != "VT220" {
		t.Errorf("nodes: %+v", n)
	}
	d.send("P")
	d.expect("Terminal:  VT220, 80x24, ANSI on, UTF-8 off, DEC graphics, sixel off, RIP off")
	if strings.Contains(string(d.got), "\x1b[1;33m") {
		t.Errorf("color to a VT220: %q", d.got)
	}
}

// TestVT52: a terminal that does not answer ANSI's question but answers a
// VT52's identify gets the VT52's escapes and plain text.
func TestVT52(t *testing.T) {
	e := start(t, nil, &Listener{Name: "vt", Kind: "raw", Address: "vt.sock", ANSI: "auto", RIP: "auto", VT: "auto"})
	d := e.dial("vt.sock")
	d.expect("\x1b[6n")
	time.Sleep(10 * time.Millisecond) // no answer: not ANSI
	d.expect("\x1bZ")
	d.send("\x1b/Z")
	d.expect("Name (or NEW): ")
	d.send("alice\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("Command: ")
	d.send("\x1bA") // a cursor key is no command
	d.send("I")
	d.expect("Sysop:")
	d.expect("[Press a key]")
	d.send(" ")
	d.send("P")
	d.expect("Terminal:  VT52, 80x24")
	if i := strings.Index(string(d.got), "\x1b["); i >= 0 {
		t.Errorf("ANSI to a VT52: %q", d.got)
	}
	if n := e.b.Nodes(); len(n) != 1 || n[0].Terminal != "VT52" {
		t.Errorf("nodes: %+v", n)
	}
}

// TestVTPlain: a terminal that answers neither is plain, with the Z its
// VT52 question showed wiped.
func TestVTPlain(t *testing.T) {
	e := start(t, nil, &Listener{Name: "vt", Kind: "raw", Address: "vt.sock", ANSI: "auto", VT: "auto"})
	d := e.dial("vt.sock")
	d.expect("\x1bZ")
	d.expect("\r \r")
	d.expect("Name (or NEW): ")
}
