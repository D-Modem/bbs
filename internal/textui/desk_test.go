package textui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/view"
)

// keyConn is a terminal whose keys are given and whose output is kept.
type keyConn struct {
	in  *bytes.Reader
	out bytes.Buffer
}

func (c *keyConn) Read(p []byte) (int, error)      { return c.in.Read(p) }
func (c *keyConn) Write(p []byte) (int, error)     { return c.out.Write(p) }
func (c *keyConn) SetReadDeadline(time.Time) error { return nil }
func deskTerm(keys string, utf8 bool) (*session, *keyConn) {
	c := &keyConn{in: bytes.NewReader([]byte(keys))}
	t := NewTerm(c)
	t.ANSI, t.UTF8, t.Cols, t.Rows = true, utf8, 80, 25
	return &session{t: t}, c
}

// sample is a sheet of each kind of entry, with a pull-down menu.
func sample() *view.Sheet {
	return &view.Sheet{Title: "Services", Crumbs: "Configuration > Services",
		Entries: []view.Entry{
			{ID: "h", Kind: view.EntryHeading, Label: "Servers"},
			{ID: "name", Kind: view.EntryText, Label: "BBS name", Value: "Old", Help: "the name"},
			{ID: "on", Kind: view.EntryBool, Label: "Enabled", Value: "no"},
			{ID: "tls", Kind: view.EntryChoice, Label: "TLS", Choices: []string{"no", "optional", "required"}, Value: "no"},
			{ID: "aol", Kind: view.EntryMenu, Label: "AOL"},
			{ID: "cert", Kind: view.EntryChoice, Label: "Certificate", Open: true, Choices: []string{"", "acme", "acme:mail"},
				ChoiceNotes: []string{"", "bbs.example.org", "mail.example.org"}},
			{ID: "dnsbl", Kind: view.EntryChoice, Label: "Blocklists", Multi: true, Value: "a.example",
				Choices: []string{"a.example", "b.example", "c.example"}},
		},
		Menus: []view.PullDown{{Label: "File", Items: []view.PullItem{{Label: "Save", Action: view.SheetSave}}},
			{Label: "Online", Items: []view.PullItem{{Label: "Online services", Heading: true},
				{Label: "America Online", Path: []string{"Online services", "America Online"}},
				{Label: "CompuServe", Path: []string{"Online services", "CompuServe"}}}}}}
}

// TestDeskSheet: a value typed in its box, a yes/no turned over, a choice
// picked from its list, a menu opened, a pull-down's item.
func TestDeskSheet(t *testing.T) {
	for _, c := range []struct {
		name, keys string
		want       view.SheetResult
	}{
		{"text", "\r\x15New\r", view.SheetResult{Action: view.SheetSet, Entry: 1, Values: map[string]string{"name": "New"}}},
		{"bool", "\x1b[B\r", view.SheetResult{Action: view.SheetSet, Entry: 2, Values: map[string]string{"on": "yes"}}},
		{"choice", "\x1b[B\x1b[B\r\x1b[B\x1b[B\r", view.SheetResult{Action: view.SheetSet, Entry: 3, Values: map[string]string{"tls": "required"}}},
		{"menu", "A\r", view.SheetResult{Action: view.Pick, Entry: 4}},
		{"open choice", "C\r\x1b[B\x1b[B\r", view.SheetResult{Action: view.SheetSet, Entry: 5, Values: map[string]string{"cert": "acme:mail"}}},
		{"own value", "C\r\x1b[F\r/etc/x.crt\r", view.SheetResult{Action: view.SheetSet, Entry: 5, Values: map[string]string{"cert": "/etc/x.crt"}}},
		{"some of", "Bl\r \x1b[B\x1b[B \r", view.SheetResult{Action: view.SheetSet, Entry: 6, Values: map[string]string{"dnsbl": "c.example"}}},
		{"save", "\x1b[21~", view.SheetResult{Action: view.SheetSave, Entry: 1}},
		{"pull-down", "/\x1b[C\x1b[B\r", view.SheetResult{Action: view.SheetJump, Entry: 1, Jump: []string{"Online services", "CompuServe"}}},
		{"pull-down action", "/\r", view.SheetResult{Action: view.SheetSave, Entry: 1}},
	} {
		s, conn := deskTerm(c.keys, true)
		r, err := s.Sheet(sample())
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if r.Action != c.want.Action || r.Entry != c.want.Entry || len(r.Values) != len(c.want.Values) ||
			strings.Join(r.Jump, ">") != strings.Join(c.want.Jump, ">") {
			t.Errorf("%s: %+v, not %+v", c.name, r, c.want)
		}
		for k, v := range c.want.Values {
			if r.Values[k] != v {
				t.Errorf("%s: %s = %q, not %q", c.name, k, r.Values[k], v)
			}
		}
		out := conn.out.String()
		for _, want := range []string{"╔", "Services", "░", "nline"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: no %q drawn", c.name, want)
			}
		}
	}
}

// TestDeskCP437: a terminal without UTF-8 gets the lines and shading in
// code page 437, not question marks.
func TestDeskCP437(t *testing.T) {
	s, conn := deskTerm("\x1b[21~", false)
	if _, err := s.Sheet(sample()); err != nil {
		t.Fatal(err)
	}
	out := conn.out.Bytes()
	for _, b := range []byte{0xC9, 0xBB, 0xB0, 0xCD} { // ╔ ╗ ░ ═
		if !bytes.Contains(out, []byte{b}) {
			t.Errorf("no %#x", b)
		}
	}
	if !bytes.Contains(out, []byte{0x10}) { // ►
		t.Errorf("no ►")
	}
	if bytes.Contains(bytes.ReplaceAll(out, []byte("\x1b[?25l"), nil), []byte("?")) {
		t.Errorf("a ? in %q", out)
	}
}

// TestDeskDialogs: with the desk up, a question and help come up as
// dialogs over it; EndSheets clears it.
func TestDeskDialogs(t *testing.T) {
	s, conn := deskTerm("\x1b[21~"+"y"+"\r", true)
	if _, err := s.Sheet(sample()); err != nil {
		t.Fatal(err)
	}
	yes, err := s.Confirm("Leave without saving 3 changes?", false)
	if err != nil || !yes {
		t.Fatalf("confirm: %v %v", yes, err)
	}
	if err := s.Show("Check", []view.Line{view.T(view.Good, "As the BBS would take it.")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conn.out.String(), "Confirm") || !strings.Contains(conn.out.String(), "As the BBS would take it.") {
		t.Errorf("no dialogs in %q", conn.out.String())
	}
	s.EndSheets()
	if s.desk != nil || !strings.HasSuffix(conn.out.String(), "\x1b[0m\x1b[2J\x1b[H\x1b[?25h") {
		t.Errorf("desk not ended")
	}
}

// TestRender: only what changed is sent again.
func TestRender(t *testing.T) {
	a := newScreen(20, 5)
	a.put(1, 1, "hello", attr{cYellow, cBlue})
	first := a.render(nil, false)
	if !strings.Contains(first, "\x1b[2J") || !strings.Contains(first, "hello") {
		t.Errorf("first %q", first)
	}
	b := a.clone()
	b.put(1, 1, "j", attr{cYellow, cBlue})
	if got := b.render(a, false); got != "\x1b[2;2H\x1b[0;1;33;44mj" {
		t.Errorf("diff %q", got)
	}
	if got := b.render(b, false); got != "" {
		t.Errorf("no change %q", got)
	}
}
