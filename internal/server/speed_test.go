package server

import (
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/bbs"
	"golang.org/x/crypto/ssh"
)

// TestLineSpeed: an SSH caller picks a line speed on the profile and with
// Ctrl-B; it is kept, and the next call begins at it.
func TestLineSpeed(t *testing.T) {
	e, addr, _ := startSSH(t, nil)
	call := func() *dialog {
		c, err := sshDial(t, addr, "alice", ssh.Password("alicepass1"))
		if err != nil {
			t.Fatal(err)
		}
		d := shell(t, c, "xterm-256color", 100, 40)
		d.expect("Command: ")
		return d
	}
	prefs := func() string {
		u, _ := e.b.Store.UserByName("alice")
		return u.Prefs
	}
	d := call()
	d.send("P")
	d.expect("line speed full")
	d.expect("[B]aud: line speed (or Ctrl-B)")
	d.send("B")
	d.expect("Which? ")
	d.send("a") // 56k
	d.expect("Now at 56000 bit/s.")
	d.expect("line speed 56000 bit/s")
	if p := prefs(); p != "baud=56000" {
		t.Errorf("kept %q", p)
	}
	d.send("Q")
	d.expect("Command: ")

	d = call()
	d.send("P")
	d.expect("line speed 56000 bit/s")
	d.send("Q")
	d.expect("Command: ")
	d.send("\x02")
	d.expect("Line speed\x1b[0m (now 56k):")
	d.send("0")
	d.expect("Full speed.")
	d.expect("Command: ") // the prompt again
	d.send("P")
	d.expect("line speed full")
	if p := prefs(); p != "baud=off" {
		t.Errorf("kept %q", p)
	}
}

// TestLineSpeedOff: with line_speeds = no, nor the profile's B nor Ctrl-B
// is there, and a speed kept is not used.
func TestLineSpeedOff(t *testing.T) {
	e, addr, _ := startSSH(t, func(s *bbs.Settings) { s.LineSpeeds = false })
	u, _ := e.b.Store.UserByName("alice")
	if err := e.b.Store.SetPrefs(u.ID, "baud=300"); err != nil {
		t.Fatal(err)
	}
	c, err := sshDial(t, addr, "alice", ssh.Password("alicepass1"))
	if err != nil {
		t.Fatal(err)
	}
	d := shell(t, c, "xterm-256color", 100, 40)
	d.expect("Command: ")
	d.send("\x02")
	d.send("P")
	d.expect("Terminal:")
	d.expect("[Q]uit: ")
	if strings.Contains(string(d.before), "ine speed") {
		t.Errorf("line speeds offered: %q", d.before)
	}
}

// TestLineSpeedModem: a modem caller's line has its speed; none is offered.
func TestLineSpeedModem(t *testing.T) {
	e := start(t, nil, &Listener{Name: "menu", Kind: "raw", Address: "menu.sock", ANSI: "no",
		Header: true, TrustUser: true, CreateUsers: true})
	d := e.dial("menu.sock")
	d.send("MODEM_USER=erin\nMODEM_AUTH=pam\nMODEM_RATE=26400/31200\n\n")
	d.expect("Command: ")
	d.send("P")
	d.expect("Terminal:")
	d.expect("[Q]uit: ")
	if strings.Contains(string(d.before), "ine speed") {
		t.Errorf("line speeds offered a modem caller: %q", d.before)
	}
}
