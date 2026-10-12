package server

import (
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestPrefsKept: what a caller switches on their profile is how the next
// call begins.
func TestPrefsKept(t *testing.T) {
	e, addr, _ := startSSH(t, nil)
	call := func() *dialog {
		c, err := sshDial(t, addr, "alice", ssh.Password("alicepass1"))
		if err != nil {
			t.Fatal(err)
		}
		d := shell(t, c, "xterm-256color", 100, 40)
		d.expect("Command: ")
		d.send("P")
		return d
	}
	d := call()
	d.expect("Terminal:  100x40, ANSI on, UTF-8 on")
	d.send("8")
	d.expect("Terminal:  100x40, ANSI on, UTF-8 off")
	d.send("Q")
	d.expect("Command: ")
	if u, _ := e.b.Store.UserByName("alice"); u.Prefs != "utf8=off" {
		t.Errorf("kept %q", u.Prefs)
	}

	d = call()
	d.expect("Terminal:  100x40, ANSI on, UTF-8 off")
}
