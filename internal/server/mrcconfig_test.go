package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/mrc"
)

func TestMRCConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load("[general]\nname = My BBS\nsysop = Sid\n[mrc]\ntls = yes\naddress = mrc.bottomlessabyss.net:5001\n")
	if err != nil {
		t.Fatal(err)
	}
	m := c.MRC
	if m == nil || !m.TLS || m.Prefix != "mrc-" || m.Sysop != "Sid" || m.Description != "My BBS" {
		t.Errorf("mrc %+v", m)
	}
	if c, _ := load("[mrc]\n"); c.MRC.Address != mrc.DefaultAddress {
		t.Errorf("default address %q", c.MRC.Address)
	}
	for body, want := range map[string]string{
		"[mrc]\naddress = nowhere\n": "address",
		"[mrc]\nprefix = MRC_\n":     "prefix",
		"[mrc]\nrequire = 999\n":     "require",
		"[mrc]\ntls = maybe\n":       "tls",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
}
