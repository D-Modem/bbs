package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQWKNetConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load(`
[qwknet DoveNet]
address = hub.example.org:21
user = MYBBS
password = secret
every = 30
2001 = DOVE General
2002 = DOVE Chat
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.QWKNets) != 1 {
		t.Fatalf("nets %v", c.QWKNets)
	}
	n := c.QWKNets[0]
	if n.Name != "dovenet" || n.TLS != "explicit" || n.Dir != "/qwk" || n.Every != 30*time.Minute ||
		n.Confs[2001] != "DOVE General" || n.Confs[2002] != "DOVE Chat" || len(n.Confs) != 2 {
		t.Errorf("net %+v", n)
	}
	if len(c.Warnings) != 0 {
		t.Errorf("warnings %v", c.Warnings)
	}
	base := "[qwknet x]\naddress = h:21\nuser = u\npassword = p\n"
	for body, want := range map[string]string{
		"[qwknet]\n":                           "a name",
		"[qwknet x]\naddress = nohost\n":       "address",
		base + "tls = maybe\n1 = A\n":          "tls",
		base:                                   "no conferences",
		base + "0 = A\n":                       "CONFERENCE = AREA",
		base + "1 = A\n2 = a\n":                "carries another",
		base + "hub_id = too-long-id\n1 = A\n": "hub_id",
		base + "1 = A\n" + base + "1 = A\n":    "given twice",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
	c, err = load(base + "tls = no\n1 = A\n")
	if err != nil || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "in the clear") {
		t.Errorf("tls = no: %v %v", c.Warnings, err)
	}
}
