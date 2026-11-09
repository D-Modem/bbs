package server

import (
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/bbs"
)

// TestCompuServeDefault: TAPCIS's ALT-P (PARAMS.SCR, 1991), its sends and
// waits in order, through GO DEFAULT's permanent settings; then the
// settings at the next logon, and ;TTY.
func TestCompuServeDefault(t *testing.T) {
	e := start(t, nil, cisListener())
	d := cisLogin(e, "72000,1", "alicepass1")
	steps := []struct{ send, wait string }{
		{"G DEFAULT", "TERM"}, {"-", "!"},
		{"2", "!"}, {"3", "!"}, {"1", "!"}, {"2", "!"}, {"", "!"},
		{"2", "!"}, {"1", "!"}, {"4", "!"}, {"2", "!"}, {"2", "!"},
		{"4", "!"}, {"1", "!"}, {"5", "!"}, {"1", "!"}, {"6", "!"}, {"2", "!"}, {"", "!"},
		{"3", "!"}, {"1", "!"}, {"2", "!"},
		{"2", "<CR>"}, {"-", "."}, {"-", "!"},
		{"1", "!"}, {"", "!"}, {"", "!"},
		{"4", "!"}, {"1", "!"}, {"8", "!"}, {"", "!"},
		{"2", ":"}, {"79", "!"}, {"3", ":"}, {"0", "!"},
		{"6", "!"}, {"1", "!"}, {"7", "!"}, {"1", "!"},
		{"8", "!"}, {"3", "!"}, {"9", ":"}, {"0", "!"},
		{"10", "!"}, {"1", "!"}, {"11", "!"}, {"2", "!"}, {"", "!"},
		{"5", "!"}, {"1", "!"}, {"1", "!"}, {"", "!"},
		{"", "!"}, {"", "!"},
		{"", "CHANGE PERMANENT SETTINGS"}, {"-", "!"},
		{"1", "CHANGE YOUR USER PROFILE"}, {"-", "!"},
	}
	// "-": no send, a second wait on the same screen
	for _, st := range steps {
		if st.send != "-" {
			d.send(st.send + "\r")
		}
		d.expect(st.wait)
	}
	d.send("OFF\r")
	d.expect("Thank you for using CompuServe!")

	u, err := e.b.Store.UserByName("alice")
	if err != nil {
		t.Fatal(err)
	}
	p := bbs.ParsePrefs(u.Prefs)
	for k, v := range map[string]string{
		"cis-paged": "no", "cis-brief": "yes", "cis-width": "79", "cis-lines": "0",
		"cis-first": "command", "cis-mailwait": "notify", "cis-top": "main", "cis-editor": "edit",
		"cis-forummode": "command", "cis-term": "other", "cis-parity": "zero", "cis-delays": "0",
		"cis-erase": "yes", "cis-inquiry": "no", "cis-protocol": "b", "cis-case": "ul", "cis-caps": "no",
	} {
		if p[k] != v {
			t.Errorf("%s = %q, want %q (%s)", k, p[k], v, u.Prefs)
		}
	}

	// the next logon is brief; with ;TTY it is not
	d = e.dial("cis.sock")
	d.expect("Host Name: ")
	d.send("CIS\r")
	d.expect("User ID: ")
	d.send("72000,1\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("12 Business/Other Interests\r\n!")
	d.send("OFF\r")
	d.expect("Thank you for using CompuServe!")
	d = cisLogin(e, "72000,1;TTY", "alicepass1")
	if strings.Contains(string(d.before), "\n!") {
		t.Errorf("brief with ;TTY: %q", d.before)
	}
	d.send("GO TERMINAL\r")
	d.expect("TERMINAL SETTINGS/SERVICE OPTIONS")
	d.expect("Enter choice !")
	d.send("4\r")
	d.expect(" BRIEF prompts                     NO         YES\r\n")
	d.expect(" Screen WIDTH                      80         79\r\n")
	d.send("\r")
	d.expect("Enter choice !")
	d.send("OFF\r")
	d.expect("Thank you for using CompuServe!")
}
