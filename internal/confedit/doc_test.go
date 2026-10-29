package confedit

import (
	"bytes"
	"strings"
	"testing"

	bbssec "github.com/D-Modem/bbs"
	"github.com/D-Modem/bbs/internal/config"
)

const file = `; my BBS
[general]
name = Test BBS
; shown before the login
;banner = "Hello"
;banner_file = /etc/bbs/banner.txt
goodbye = Bye.

; the HTTP API
;[http]
;address = :8443
; the forum's theme
;forum_theme = ipb3

[listen telnet]
type = telnet
address = :2300
`

// parse is s parsed, or the test failed.
func parse(t *testing.T, s string) *Doc {
	t.Helper()
	d, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestRoundTrip: a file, and the sample, parsed and written again are what
// they were, byte for byte.
func TestRoundTrip(t *testing.T) {
	d := parse(t, file)
	if got := string(d.Bytes()); got != file {
		t.Fatalf("changed:\n%s", got)
	}
	d = parse(t, string(bbssec.Sample))
	if !bytes.Equal(d.Bytes(), bbssec.Sample) {
		t.Fatal("the sample changed")
	}
}

// TestSet: a key there is changed in its place; a new one goes below its
// commented example, else below the section's last key; a commented-out
// section is made where it is, a new one at the end; the BBS reads it back.
func TestSet(t *testing.T) {
	d := parse(t, file)
	g := Section{Kind: "general"}
	// a key there: changed in its place
	if err := d.Set(g, "name", "Other BBS"); err != nil {
		t.Fatal(err)
	}
	// a new key: below its example
	if err := d.Set(g, "banner", " Hi there "); err != nil {
		t.Fatal(err)
	}
	// a key with no example: below the last key
	if err := d.Set(g, "sysop", "Ann"); err != nil {
		t.Fatal(err)
	}
	// a commented-out section: made there, its key below its example
	h := Section{Kind: "http"}
	if err := d.Set(h, "forum_theme", "vb3"); err != nil {
		t.Fatal(err)
	}
	// a new section at the end
	if err := d.Set(Section{"listen", "ssh"}, "type", "ssh"); err != nil {
		t.Fatal(err)
	}
	want := `; my BBS
[general]
name = Other BBS
; shown before the login
;banner = "Hello"
banner = " Hi there "
;banner_file = /etc/bbs/banner.txt
goodbye = Bye.
sysop = Ann

; the HTTP API
[http]
;address = :8443
; the forum's theme
;forum_theme = ipb3
forum_theme = vb3

[listen telnet]
type = telnet
address = :2300

[listen ssh]
type = ssh
`
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	cf, err := config.Parse(bytes.NewReader(d.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if v := cf.Get("general", "").String("banner", ""); v != " Hi there " {
		t.Errorf("banner %q", v)
	}
	if v, _ := d.Get(g, "banner"); v != " Hi there " {
		t.Errorf("Get banner %q", v)
	}
	if ks := d.Keys(g); strings.Join(ks, " ") != "name banner goodbye sysop" {
		t.Errorf("keys %v", ks)
	}
}

// TestUnsetRemoveRename: a key unset is commented out, and set again goes
// below it; a section renamed, removed (commented out, keys and all) and
// had back has its old keys as examples; a section is not made twice.
func TestUnsetRemoveRename(t *testing.T) {
	d := parse(t, file)
	g := Section{Kind: "general"}
	if !d.Unset(g, "goodbye") || d.Unset(g, "goodbye") {
		t.Fatal("unset")
	}
	if _, ok := d.Get(g, "goodbye"); ok {
		t.Fatal("still there")
	}
	// set again: below the line it left
	d.Set(g, "goodbye", "Later.")
	if !strings.Contains(string(d.Bytes()), ";goodbye = Bye.\ngoodbye = Later.\n") {
		t.Fatalf("got:\n%s", d.Bytes())
	}
	l := Section{"listen", "telnet"}
	if err := d.Rename(l, "tn"); err != nil {
		t.Fatal(err)
	}
	if err := d.Rename(Section{"listen", "tn"}, "bad name"); err == nil {
		t.Fatal("renamed to a name with a space")
	}
	tn := Section{"listen", "tn"}
	if !d.RemoveSection(tn) || d.Has(tn) {
		t.Fatal("remove")
	}
	if !strings.Contains(string(d.Bytes()), ";[listen tn]\n;type = telnet\n;address = :2300\n") {
		t.Fatalf("got:\n%s", d.Bytes())
	}
	// had back, its keys as examples
	if err := d.AddSection(tn); err != nil {
		t.Fatal(err)
	}
	d.Set(tn, "address", ":23")
	if !strings.Contains(string(d.Bytes()), "[listen tn]\n;type = telnet\n;address = :2300\naddress = :23\n") {
		t.Fatalf("got:\n%s", d.Bytes())
	}
	if err := d.AddSection(tn); err == nil {
		t.Fatal("made twice")
	}
	if _, err := Parse(d.Bytes()); err != nil {
		t.Fatal(err)
	}
}

// TestQuote: a value quoted reads back as it was, and a plain one is not
// quoted.
func TestQuote(t *testing.T) {
	for _, v := range []string{"", "plain", " spaced ", `"quoted"`, "a\tb\r\n\x1b[1m\x01", `back\slash`, `"half`} {
		q := Quote(v)
		got, err := config.Unquote(q)
		if err != nil || got != v {
			t.Errorf("%q: quoted %s, read back %q (%v)", v, q, got, err)
		}
	}
	if Quote(`back\slash`) != `back\slash` {
		t.Error("quoted a plain value")
	}
}

// TestDiff: what changed between two files, in order, and whether each
// change takes effect at a reload.
func TestDiff(t *testing.T) {
	a := parse(t, file)
	b := parse(t, file)
	b.Set(Section{Kind: "general"}, "name", "New")
	b.Unset(Section{Kind: "general"}, "goodbye")
	b.Set(Section{Kind: "http"}, "forum_theme", "vb3")
	b.Set(Section{Kind: "http"}, "address", ":80")
	var got []string
	for _, c := range Diff(a, b) {
		got = append(got, c.String())
		if c.Live() != (c.Section.Kind == "general" || c.Key == "forum_theme") {
			t.Errorf("%s: live %v", c, c.Live())
		}
	}
	want := `-[general] goodbye|~[general] name = New (was Test BBS)|+[http]|+[http] address = :80|+[http] forum_theme = vb3`
	if strings.Join(got, "|") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(got, "|"), want)
	}
}

// TestBadInput: a file with a key given twice, a key with a space, and a
// section's name with a ] are refused.
func TestBadInput(t *testing.T) {
	if _, err := Parse([]byte("[general]\nname = a\nname = b\n")); err == nil {
		t.Error("parsed a key given twice")
	}
	d := parse(t, file)
	if err := d.Set(Section{Kind: "general"}, "bad key", "x"); err == nil {
		t.Error("set a key with a space")
	}
	if err := d.AddSection(Section{"door", "a]b"}); err == nil {
		t.Error("made [door a]b]")
	}
}

// TestCatalog: the catalog read from the sample has keys' help, examples and
// defaults, menu items as the sysop's own keys, named kinds, help for every
// kind, and which keys are live.
func TestCatalog(t *testing.T) {
	c := NewCatalog(bbssec.Sample)
	k := c.Key("general", "idle_timeout")
	if k == nil || !strings.Contains(k.Help, "seconds") || k.Examples[0] != "600" {
		t.Fatalf("idle_timeout: %+v", k)
	}
	if k := c.Key("listen", "petscii"); k == nil || k.Default != "no" || !strings.Contains(k.Help, "Commodore") {
		t.Fatalf("petscii: %+v", k)
	}
	if k := c.Key("menu", "x"); k == nil || !strings.Contains(k.Help, "ACTION") {
		t.Fatalf("menu item: %+v", k)
	}
	if c.Key("menu", "zz") != nil || c.Key("general", "nonsense") != nil {
		t.Fatal("found a key not there")
	}
	if !c.Kind("door").Named || c.Kind("general").Named {
		t.Fatal("named")
	}
	for _, kd := range c.Kinds {
		if kd.Help == "" {
			t.Errorf("[%s] has no help", kd.Name)
		}
	}
	if !Live("chan", "max_file") || Live("chan", "dir") || Live("general", "database") || !Live("level", "time") {
		t.Fatal("live")
	}
}
