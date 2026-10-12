package server

import (
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestFullScreenEditor: an ANSI caller writes in the full-screen editor
// (moving about, a line deleted, the line editor's /S), replies quoting
// from the window, and can choose the line editor instead.
func TestFullScreenEditor(t *testing.T) {
	e, addr, _ := startSSH(t, nil)
	c, err := sshDial(t, addr, "alice", ssh.Password("alicepass1"))
	if err != nil {
		t.Fatal(err)
	}
	d := shell(t, c, "xterm-256color", 80, 24)
	d.expect("Command: ")
	d.send("M")
	d.expect("[Q]uit: ")
	d.send("1\r")
	d.expect("[P]ost, [Q]uit: ")
	d.send("P\r")
	d.expect("To [All]: ")
	d.send("\r")
	d.expect("Subject: ")
	d.send("Full screen\r")
	d.expect("^Z save")
	// two lines; up to the first, its start, a word in front; a line
	// typed and taken away again; saved
	d.send("world\rsecond line\r\x1b[A\x1b[A\x1b[Hhello \x1b[B\x1b[B" + "oops\x19")
	d.send("\x1a")
	d.expect("Posted as #1.")
	m, err := e.b.Store.Message(1)
	if err != nil || m.Body != "hello world\nsecond line\n" && m.Body != "hello world\nsecond line" {
		t.Fatalf("posted %q %v", m.Body, err)
	}

	// a reply: the quote window, a line taken from it, the line editor's /S
	d.expect("[P]ost, [Q]uit: ")
	d.send("1\r")
	d.expect("[R]eply")
	d.send("R")
	d.expect("To [alice]: ")
	d.send("\r")
	d.expect("Subject: ")
	d.send("\r")
	d.expect("^Q quote")
	d.send("\x11")
	d.expect("Quoting 1 of")
	d.send("\r\r\x1b") // the heading and the first line
	d.send("agreed\r/s\r")
	d.expect("Posted as #2.")
	m, _ = e.b.Store.Message(2)
	if got := strings.TrimRight(m.Body, "\n"); got != "alice wrote:\n> hello world second line\nagreed" {
		t.Errorf("reply %q", got)
	}

	// the line editor instead, chosen on the profile
	d.expect("[Q]uit: ")
	d.send("Q")
	d.expect("[Q]uit: ")
	d.send("Q\r")
	d.expect("[Q]uit: ")
	d.send("Q\r")
	d.expect("Command: ")
	d.send("P")
	d.expect("editor full-screen")
	d.send("E")
	d.expect("editor line")
	if u, _ := e.b.Store.UserByName("alice"); !strings.Contains(u.Prefs, "editor=line") {
		t.Errorf("kept %q", u.Prefs)
	}
}
