package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIRCConfig: [irc] with its defaults and the warning that it is in the
// clear, none without it, and bad addresses, TLS, service and limits refused.
func TestIRCConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load("[irc]\nnetwork = TestNet\nmax_per_ip = 2\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.IRC.Address != ":6667" || c.IRC.Network != "TestNet" || c.IRC.MaxPerIP != 2 || c.IRC.MaxClients != 64 || c.IRC.Service != "BBSServ" {
		t.Errorf("irc %+v", c.IRC)
	}
	if len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "in the clear") {
		t.Errorf("warnings %v", c.Warnings)
	}
	if c, err := load(""); err != nil || c.IRC != nil {
		t.Errorf("no [irc]: %+v %v", c.IRC, err)
	}
	for body, want := range map[string]string{
		"[irc]\naddress = 6667\n":      "address",
		"[irc]\naddress = none\n":      "somewhere to listen",
		"[irc]\ntls_address = :6697\n": "tls_cert",
		"[irc]\ntls_address = :6697\ntls_cert = /nonexistent\ntls_key = /nonexistent\n": "tls_cert, tls_key",
		"[irc]\nservice = Bad Bot\n": "service",
		"[irc]\nmax_clients = 0\n":   "max_clients",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
}
