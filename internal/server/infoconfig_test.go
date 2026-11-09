package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInfoConfig: page_command split into words, [finger] and [gopher]
// with their defaults and the warning that gopher shows areas to anyone;
// bad addresses, ports, commands and QWK IDs refused.
func TestInfoConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load(`
[general]
page_command = /usr/local/bin/notify 'BBS page' {user} {reason}
[finger]
users = no
[gopher]
address = :7070
hostname = bbs.example.org
areas = general,announce*
files = yes
`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.Settings.PageCommand, "|") != "/usr/local/bin/notify|BBS page|{user}|{reason}" {
		t.Errorf("page command %q", c.Settings.PageCommand)
	}
	if c.Finger.Address != ":79" || !c.Finger.Online || c.Finger.Users {
		t.Errorf("finger %+v", c.Finger)
	}
	if g := c.Gopher; g.Host != "bbs.example.org" || g.Port != 7070 || g.Areas != "general,announce*" || !g.Files {
		t.Errorf("gopher %+v", g)
	}
	if !strings.Contains(strings.Join(c.Warnings, "\n"), "[gopher] shows message or file areas to anyone") {
		t.Errorf("no warning: %v", c.Warnings)
	}
	c, err = load("[gopher]\n")
	if err != nil || c.Gopher.Port != 70 || c.Gopher.Areas != "" || c.Gopher.Files {
		t.Errorf("defaults %+v %v", c.Gopher, err)
	}
	c, err = load("[general]\ndatabase = /var/lib/bbs/bbs.db\n[gemini]\nareas = *\n")
	if g := c.Gemini; err != nil || g.Address != ":1965" || !g.Users || g.Areas != "*" ||
		g.CertFile != "/var/lib/bbs/gemini-cert.pem" || g.KeyFile != "/var/lib/bbs/gemini-key.pem" {
		t.Errorf("gemini %+v %v", g, err)
	}
	if !strings.Contains(strings.Join(c.Warnings, "\n"), "[gemini] shows message or file areas to anyone") {
		t.Errorf("no gemini warning: %v", c.Warnings)
	}
	c, err = load("[gemini]\naddress = :1966\ncert_file = /etc/c.pem\nkey_file = /etc/k.pem\nusers = no\n")
	if g := c.Gemini; err != nil || g.Users || g.CertFile != "/etc/c.pem" || len(c.Warnings) != 0 {
		t.Errorf("gemini %+v %v %v", g, err, c.Warnings)
	}
	for body, want := range map[string]string{
		"[finger]\naddress = 79\n":           "[finger] address",
		"[gemini]\ncert_file = /etc/c.pem\n": "[gemini] cert_file",
		"[gemini]\naddress = x\n":            "[gemini] address",
		"[gopher]\nport = 0\n":               "[gopher]",
		"[general]\npage_command = 'x\n":     "page_command",
		"[general]\nqwk_id = too-long-id\n":  "qwk_id",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
}
