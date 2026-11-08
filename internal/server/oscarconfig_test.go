package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOSCARConfig: [oscar] with its defaults, none without it, and bad
// addresses, names and numbers refused.
func TestOSCARConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load("[oscar]\nhost = aim.example.org:5190\n")
	if err != nil {
		t.Fatal(err)
	}
	if o := c.OSCAR; o.Address != ":5190" || o.Host != "aim.example.org:5190" || o.MaxPerIP != 16 ||
		o.MaxClients != 256 || o.UINBase != 100000 || o.System != "OnlineHost" {
		t.Errorf("oscar %+v", o)
	}
	if c, err := load(""); err != nil || c.OSCAR != nil {
		t.Errorf("no [oscar]: %+v %v", c.OSCAR, err)
	}
	for body, want := range map[string]string{
		"[oscar]\naddress = 5190\n":               "address",
		"[oscar]\nhost = aim.example.org\n":       "host",
		"[oscar]\nsystem = A Name Far Too Long\n": "system",
		"[oscar]\nuin_base = 5\n":                 "uin_base",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
}
