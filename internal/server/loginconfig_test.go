package server

import (
	"strings"
	"testing"
)

// TestLoginSection: [dmodem-login] is read; [modemmenu], its old name,
// is an unknown section.
func TestLoginSection(t *testing.T) {
	const listen = "[listen telnet]\ntype = telnet\naddress = :2300\n"
	c, err := Parse([]byte(listen + "[dmodem-login]\nauth_socket = /run/dmodem-login/auth.sock\ntimeout = 5\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.MenuAuth == nil || c.MenuAuth.Socket != "/run/dmodem-login/auth.sock" || c.MenuAuth.Timeout.Seconds() != 5 {
		t.Errorf("%+v", c.MenuAuth)
	}
	_, err = Parse([]byte(listen + "[modemmenu]\nauth_socket = /run/dmodem-login/auth.sock\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown section [modemmenu]") {
		t.Errorf("[modemmenu]: %v", err)
	}
}
