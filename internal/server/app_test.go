package server

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
)

// TestShared3270: the screens internal/app shares, on a 3270 (s3270): the
// areas, a post from the editor, the message read back, the profile; and
// the post as a text caller reads it.
func TestShared3270(t *testing.T) {
	if _, err := exec.LookPath("s3270"); err != nil {
		t.Skip("no s3270")
	}
	addr := freeTCP(t)
	e := startFull(t, ui3270.Graphics{}, nil, nil,
		&Listener{Name: "telnet", Kind: "telnet", Address: addr, TN3270: true},
		&Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	script := strings.Join([]string{
		"Connect(" + addr + ")", "Wait(10,InputField)",
		`String("alice")`, "Tab()", `String("alicepass1")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		`String("m")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		`String("s")`, "Enter()", "Wait(5,InputField)",
		"PF(6)", "Wait(5,InputField)",
		"Tab()", `String("Hello from a 3270")`, "Tab()", `String("A line of the body.")`,
		"PF(5)", "Wait(5,InputField)", "Ascii()",
		`String("s")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		"PF(3)", "Wait(5,InputField)", "PF(3)", "Wait(5,InputField)", "PF(3)", "Wait(5,InputField)",
		`String("p")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		"PF(3)", "Wait(5,InputField)", "PF(3)", "Wait(3,Seconds)", "Ascii()", "Quit()", ""}, "\n")
	cmd := exec.Command("s3270", "-model", "3279-2")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("s3270: %v\n%s", err, out)
	}
	for _, want := range []string{"Welcome, alice.", "Message areas", "General", "PF5=New", "PF10=Find",
		"Posted as #1.", "Hello from a 3270", "A line of the body.", "PF4=Reply",
		"Your profile", "Access:", "Terminal:", "PF5=Location"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}

	d := login(e, "sysop", "sysoppass1")
	d.send("M")
	d.expect("Area number, [N]ew scan, [T]o you, [F]ind, [S]can setup, [+] add area, [Q]uit: ")
	d.send("1\r")
	d.expect("Hello from a 3270")
	d.expect("Number to read, [R]ead new, [P]ost, [L] New poll, [Q]uit: ")
	d.send("1\r")
	d.expect("A line of the body.")
	d.expect("[N]ext, [P]revious, [R]eply, [M]ail reply, [D]elete, [V]ote, [T]hread, [Q]uit: ")
	d.send("Q")
	d.expect("[P]ost, [L] New poll, [Q]uit: ")
	d.send("Q\r")
	d.expect("[+] add area, [Q]uit: ")
	d.send("Q\r")
	d.expect("Command: ")
}
