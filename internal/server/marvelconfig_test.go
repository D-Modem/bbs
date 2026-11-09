package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMarvelConfig: [marvel] listens on 569 by default, and a listener's
// frontend = marvel is for telnet and raw lines.
func TestMarvelConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte(body), 0600)
		return Load(p)
	}
	c, err := load("[listen msn]\ntype = raw\naddress = 127.0.0.1:2325\nfrontend = marvel\n[marvel]\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Marvel == nil || c.Marvel.Address != ":569" || c.Marvel.MaxClients != 64 {
		t.Errorf("marvel %+v", c.Marvel)
	}
	if len(c.Listeners) != 1 || c.Listeners[0].Frontend != "marvel" {
		t.Errorf("listener %+v", c.Listeners)
	}
	for body, want := range map[string]string{
		"[marvel]\naddress = nowhere\n":                                                "address",
		"[listen x]\ntype = ssh\naddress = :2222\nfrontend = marvel\n":                 "frontend",
		"[listen x]\ntype = raw\naddress = :2222\nfrontend = marvel\nconnect = 9600\n": "connect",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
}
