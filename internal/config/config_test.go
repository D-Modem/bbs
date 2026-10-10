package config

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cf, err := Parse(strings.NewReader(`
; a comment
[general]
name = My BBS
banner = "\r\n  Welcome \x41\e[0m  "
max = 12

[listen telnet]
address = :23
`))
	if err != nil {
		t.Fatal(err)
	}
	g := cf.Get("general", "")
	if g.String("name", "") != "My BBS" {
		t.Error("name")
	}
	if got := g.String("banner", ""); got != "\r\n  Welcome A\x1b[0m  " {
		t.Errorf("banner = %q", got)
	}
	if n, err := g.Int("max", 0, 0, 10); err == nil {
		t.Errorf("max out of range taken: %d", n)
	}
	l := cf.Get("listen", "telnet")
	if l == nil || l.String("address", "") != ":23" {
		t.Error("listen telnet")
	}
	if u := cf.Unused(); len(u) != 0 {
		t.Errorf("unused: %v", u)
	}
	l.String("address", "")
	if cf.Get("listen", "nope") != nil {
		t.Error("found a missing section")
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{
		"key = value",             // outside a section
		"[general\nx = 1",         // bad header
		"[general]\nno equals",    // no =
		"[general]\na = 1\na = 2", // twice
		`[general]` + "\n" + `a = "\x4"`,
	} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("took %q", bad)
		}
	}
}

func TestUnused(t *testing.T) {
	cf, _ := Parse(strings.NewReader("[general]\nnmae = x\n"))
	cf.Get("general", "").String("name", "")
	if u := cf.Unused(); len(u) != 1 || !strings.Contains(u[0], "nmae") {
		t.Errorf("unused = %v", u)
	}
}
