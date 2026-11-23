package server

import (
	"strings"
	"testing"
)

// TestLoginSection: [dmodem-login], and [modemmenu], its old name, read
// the same; both at once is an error.
func TestLoginSection(t *testing.T) {
	const listen = "[listen telnet]\ntype = telnet\naddress = :2300\n"
	for _, name := range []string{"dmodem-login", "modemmenu"} {
		c, err := Parse([]byte(listen + "[" + name + "]\nauth_socket = /run/dmodem-login/auth.sock\ntimeout = 5\n"))
		if err != nil {
			t.Fatalf("[%s]: %v", name, err)
		}
		if c.MenuAuth == nil || c.MenuAuth.Socket != "/run/dmodem-login/auth.sock" || c.MenuAuth.Timeout.Seconds() != 5 {
			t.Errorf("[%s]: %+v", name, c.MenuAuth)
		}
	}
	_, err := Parse([]byte(listen + "[modemmenu]\nauth_socket = /a\n[dmodem-login]\nauth_socket = /b\n"))
	if err == nil || !strings.Contains(err.Error(), "one only") {
		t.Errorf("both: %v", err)
	}
	_, err = Parse([]byte(listen + "[modemmenu]\nauth_socket = relative\n"))
	if err == nil || !strings.Contains(err.Error(), "[modemmenu]") {
		t.Errorf("an error in the old section names it: %v", err)
	}
}
