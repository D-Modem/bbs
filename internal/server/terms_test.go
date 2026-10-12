package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/petscii"
	"github.com/Neo-Desktop/bbs-sec/internal/screens"
	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
)

// pet is text as a Commodore is sent it (no colors).
func pet(s string) string {
	var b []byte
	for _, r := range s {
		if r == '\n' {
			b = append(b, petscii.Return)
			continue
		}
		b = append(b, petscii.Encode(r)...)
	}
	return string(b)
}

// typed is what a Commodore sends when s is typed (unshifted letters are
// lower case).
func typed(s string) string {
	var b []byte
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b = append(b, byte(r-0x20))
		case r >= 'A' && r <= 'Z':
			b = append(b, byte(r+0x80))
		case r == '\r':
			b = append(b, petscii.Return)
		case r == '\b':
			b = append(b, petscii.Del)
		default:
			b = append(b, byte(r))
		}
	}
	return string(b)
}

func screensOnly(files map[string]string) func(*bbs.BBS, string) {
	return func(b *bbs.BBS, top string) {
		dir := filepath.Join(top, "screens")
		os.Mkdir(dir, 0755)
		for name, body := range files {
			os.WriteFile(filepath.Join(dir, name), []byte(body), 0644)
		}
		b.Screens = &screens.Set{Dir: dir}
	}
}

// TestPETSCII: a Commodore on a petscii = yes line: set up, logging in
// (with DEL), one column menus, colors, a .seq screen, the profile.
func TestPETSCII(t *testing.T) {
	seq := "\x1c\x12HI\x92\x0d" // red, reverse "hi"
	e := startFull(t, ui3270.Graphics{}, nil, screensOnly(map[string]string{"welcome.seq": seq}),
		&Listener{Name: "c64", Kind: "raw", Address: "c64.sock", ANSI: "auto", PETSCII: "yes", RIP: "auto"})
	d := e.dial("c64.sock")
	d.expect(string(petscii.Start()))
	d.expect(pet("Name"))
	d.expect(pet("(or NEW)"))
	d.send(typed("alicx\be\r"))
	d.expect("\x9d \x9d") // DEL erased the x
	d.expect(pet("Password: "))
	d.send(typed("alicepass1\r"))
	d.expect(seq)
	d.expect(pet("Main menu"))
	// one column: each item on its own line
	d.expect("\x9e" + pet("[M]") + "\x9b " + pet("Message areas"))
	d.expect("\x0d")
	d.expect("\x9e" + pet("[E]"))
	d.expect(pet("Command: "))
	d.send(typed("p"))
	d.expect(pet("Terminal:  Commodore, 40x25"))
	d.expect(pet("[W]idth 40/80"))
	d.send(typed("w"))
	d.expect(pet("Terminal:  Commodore, 80x25"))
	d.send(typed("q"))
	d.expect(pet("Command: "))
	d.send(typed("w"))
	d.expect(pet("Who's online"))
	d.expect(pet("alice"))
	d.expect(pet("Send a node a message"))
	d.send(typed("\r"))
	d.expect(pet("Command: "))
	d.send(typed("gy"))
	d.expect(pet("Thanks for calling."))
	// no ANSI escape went out
	if strings.Contains(string(d.got), "\x1b") {
		t.Errorf("an escape: %q", d.got)
	}
}

// TestPETSCIIFound: on a petscii = auto line a terminal that is not ANSI
// is watched: a key only a Commodore sends makes it PETSCII at once; a
// name all in capitals brings the DEL question; one in lower case is
// ASCII, with nothing asked.
func TestPETSCIIFound(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "auto", PETSCII: "auto", RIP: "auto"})
	login := func(name string) *dialog {
		d := e.dial("modem.sock")
		d.expect("TEST BBS") // capitals while it is not known
		d.expect("NAME (OR NEW): ")
		if name != "" {
			d.send(name)
		}
		return d
	}

	// DEL while typing the name
	d := login("AL\x14")
	d.expect(string(petscii.Start()))
	d.expect(pet("Test BBS")) // the logon again, in PETSCII
	d.expect(pet("Name"))
	d.send(typed("alice\r"))
	d.expect(pet("Password: "))
	d.send(typed("alicepass1\r"))
	d.expect(pet("Welcome"))
	d.expect(pet("Command: "))

	// a shifted letter
	d = login("\xc1")
	d.expect(string(petscii.Start()))

	// a name in capitals: the question; DEL makes it a Commodore, and the
	// name was lower case
	d = login("ALICE\r")
	d.expect("PRESS DEL (OR BACKSPACE): ")
	d.send("\x14")
	d.expect(string(petscii.Start()))
	d.expect(pet("Name"))
	d.expect(pet("alice"))
	d.expect(pet("Password: "))
	d.send(typed("alicepass1\r"))
	d.expect(pet("Welcome"))

	// in capitals, but a backspace: ASCII, and lower case from now on
	d = login("ALICE\r")
	d.expect("PRESS DEL (OR BACKSPACE): ")
	d.send("\x7f")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("Welcome")

	// lower case: ASCII, nothing asked; UTF-8 is not a shifted letter
	d = login("caf\xc3\xa9 alice\r")
	d.expect("Password: ")
	if strings.Contains(string(d.got), "PRESS DEL") {
		t.Errorf("asked: %q", d.got)
	}
}

// TestRIP: a RIP terminal (it answers ESC [ !) gets its menus as buttons,
// a .rip screen, and the text screens between; an ANSI one that does not
// answer gets the text menus.
func TestRIP(t *testing.T) {
	e := startFull(t, ui3270.Graphics{}, nil, screensOnly(map[string]string{
		"welcome.rip": "!|*\r\n!|c0F\r\n!|@0A0AWelcome @USER@\r\n", "welcome.ans": "not this"}),
		&Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "yes", RIP: "auto", Sixel: "auto"})
	d := e.dial("modem.sock")
	d.expect("\x1b[!\x1b[6n")
	d.send("RIPSCRIP015400\x1b[25;80R")
	d.expect("Name")
	d.send("alice\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("!|@0A0AWelcome alice\r\n") // the .rip screen, @-codes filled
	d.expect("!|*\r\n")                  // the menu's scene
	d.expect("<>[M] Message areas<>M\r\n")
	d.expect("<>[G] Goodbye<>G\r\n")
	d.expect("!|#\r\n")
	d.expect("Command: ")
	d.send("W") // a button sends its key
	d.expect("!|1K\r\n!|*\r\n")
	d.expect("Who's online")
	d.expect("Send a node a message")
	d.send("\r")
	d.expect("!|*\r\n") // the menu again: nothing to keep
	d.expect("Command: ")
	d.send("I")
	d.expect("Sysop:")
	d.expect("[Press a key]")
	d.send(" ")
	d.expect("<>[M] Message areas<>M")
	d.send("P")
	d.expect("RIP on")
	d.send("Q")
	// what the profile printed waits for a key before the buttons
	d.expect("[Press a key]")
	d.send(" ")
	d.expect("!|*\r\n")

	d = e.dial("modem.sock")
	d.expect("\x1b[!\x1b[6n")
	d.send("\x1b[25;80R")
	d.expect("Name")
	d.send("alice\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("not this")
	d.expect("[M]")
	if strings.Contains(string(d.got), "!|") {
		t.Errorf("RIP to a terminal without it: %q", d.got)
	}
}

func TestTerminalConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte(body), 0600)
		return Load(p)
	}
	c, err := load("[listen c64]\ntype = raw\naddress = :6400\npetscii = yes\nrip = no\n[listen t]\ntype = telnet\naddress = :23\n")
	if err != nil {
		t.Fatal(err)
	}
	if l := c.Listeners[0]; l.PETSCII != "yes" || l.RIP != "no" {
		t.Errorf("%+v", l)
	}
	if l := c.Listeners[1]; l.PETSCII != "no" || l.RIP != "auto" {
		t.Errorf("defaults %+v", l)
	}
	for _, body := range []string{"[listen x]\ntype = raw\naddress = :1\npetscii = c64\n",
		"[listen x]\ntype = raw\naddress = :1\nrip = maybe\n"} {
		if _, err := load(body); err == nil {
			t.Errorf("%q taken", body)
		}
	}
}
