package screens

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestFind: each terminal gets its own kind of file, else the plain ones; an
// .ans is CP437 with its SAUCE dropped; a file too big, a name not valid or a
// path out, or no set, finds nothing; EncodeCP437 puts '?' for what CP437
// lacks.
func TestFind(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "welcome.ans"), []byte("\x1b[1;33m\xc9\xcd\xbb Hi @USER@\r\n\x1aSAUCE00 junk"), 0644)
	os.WriteFile(filepath.Join(dir, "welcome.asc"), []byte("Hi @USER@, plain"), 0644)
	os.WriteFile(filepath.Join(dir, "welcome.3270"), []byte("{yellow}Hi{default} @USER@"), 0644)
	os.WriteFile(filepath.Join(dir, "only.txt"), []byte("text"), 0644)
	os.WriteFile(filepath.Join(dir, "big.asc"), make([]byte, MaxSize+1), 0644)
	s := &Set{Dir: dir}
	for _, c := range []struct {
		name string
		kind Kind
		want string
		f    Format
	}{
		{"welcome", ANSI, "\x1b[1;33m╔═╗ Hi @USER@\n", Art},
		{"welcome", Plain, "Hi @USER@, plain", Text},
		{"welcome", TN3270, "{yellow}Hi{default} @USER@", Color},
		{"only", TN3270, "text", Text},
		{"only", ANSI, "text", Text},
	} {
		sc, ok := s.Find(c.name, c.kind)
		if !ok || sc.Text != c.want || sc.Format != c.f {
			t.Errorf("%s/%d: %+v %v", c.name, c.kind, sc, ok)
		}
	}
	for _, name := range []string{"none", "big", "../etc/passwd", "Welcome", ""} {
		if _, ok := s.Find(name, ANSI); ok {
			t.Errorf("%q found", name)
		}
	}
	if _, ok := (*Set)(nil).Find("welcome", ANSI); ok {
		t.Error("found in no set")
	}
	if got := string(EncodeCP437("╔═╗ é ☃")); got != "\xc9\xcd\xbb \x82 ?" {
		t.Errorf("%q", got)
	}
}

// TestExpand: @-codes filled in and padded, left and right and cut short; @CLS@
// and @PAUSE@ split the text; what is not a code (an address, an unknown code)
// stays.
func TestExpand(t *testing.T) {
	vars := func(c string) (string, bool) {
		switch c {
		case "USER":
			return "alice", true
		case "NODE":
			return "7", true
		}
		return "", false
	}
	got := Expand("@CLS@Hi @USER:8@|@NODE:-3@|@USER:2@ mail me@example.com @NOPE@@PAUSE@bye", vars)
	want := []Piece{{Clear: true}, {Text: "Hi alice   |  7|al mail me@example.com @NOPE@"}, {Pause: true}, {Text: "bye"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%+v", got)
	}
	if s := String("a@CLS@b@PAUSE@c", nil); s != "abc" {
		t.Error(s)
	}
}

// TestSpans: a marked .3270 line's spans, their colors and brightness to each
// {default}, {normal} and {reset}; a caller's braces are not tags; StripTags
// drops tags and marks but not unknown tags.
func TestSpans(t *testing.T) {
	// a line of a .3270, marked as screenPages marks it
	line := String("", nil)
	for _, p := range ExpandTags("plain {red}red {bright}hot{normal}{default} {cyan}cool{reset} end", nil) {
		if p.Tag != "" {
			line += Mark(p.Tag)
		} else {
			line += p.Text
		}
	}
	got := Spans(line)
	want := []Span{{"plain ", "", false}, {"red ", "red", false}, {"hot", "red", true}, {" ", "", false},
		{"cool", "turquoise", false}, {" end", "", false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%+v", got)
	}
	// braces a caller wrote are not tags
	if got := Spans("{red}x"); len(got) != 1 || got[0].Color != "" {
		t.Errorf("%+v", got)
	}
	if s := StripTags("{blue}a{bright}b" + Mark("on red") + "c"); s != "abc" {
		t.Error(s)
	}
	if !strings.Contains(StripTags("{nope}"), "{nope}") {
		t.Error("an unknown tag was taken")
	}
}

// TestTags: ExpandTags splits at the sysop's tags but never at a value's, and
// unknown tags stay text; Expand leaves tags be; String drops them, Marked
// keeps them as marks and Unmark drops those; ColorNumber's colors (pink and
// turquoise among them, not teal).
func TestTags(t *testing.T) {
	vars := func(c string) (string, bool) {
		if c == "USER" {
			return "{red}bob", true // a caller may be named so
		}
		return "", false
	}
	got := ExpandTags("{clear}{yellow}{on blue}Hi @USER@{default}{pause}{nope} {bright}x", vars)
	want := []Piece{{Clear: true}, {Tag: "yellow"}, {Tag: "on blue"}, {Text: "Hi {red}bob"}, {Tag: "default"},
		{Pause: true}, {Text: "{nope} "}, {Tag: "bright"}, {Text: "x"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%+v", got)
	}
	// Expand leaves them be (ANSI art, whose braces are art)
	if got := Expand("{red}a", nil); len(got) != 1 || got[0].Text != "{red}a" {
		t.Errorf("%+v", got)
	}
	if s := String("{cls}{green}Hi @USER@{reset}", vars); s != "Hi {red}bob" {
		t.Error(s)
	}
	m := Marked("{green}Hi @USER@{pause}{cls}", vars)
	if m != Mark("green")+"Hi {red}bob"+Mark("clear") {
		t.Errorf("%q", m)
	}
	// its marks dropped, the caller's braces kept
	if s := Unmark(m); s != "Hi {red}bob" {
		t.Errorf("%q", s)
	}
	for _, c := range []struct {
		name string
		n    int
		ok   bool
	}{{"black", 0, true}, {"white", 7, true}, {"pink", 5, true}, {"turquoise", 6, true}, {"teal", 0, false}} {
		if n, ok := ColorNumber(c.name); n != c.n || ok != c.ok {
			t.Errorf("%s: %d %v", c.name, n, ok)
		}
	}
}

// TestFit: a marked text padded or cut to a width, its marks taking none, a
// {reset} after the text when it has marks, and the marks past the cut dropped.
func TestFit(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want string
	}{
		{"plain", 8, "plain   "},
		{"toolong", 4, "tool"},
		{Mark("red") + "Red" + Mark("default") + " x", 7, Mark("red") + "Red" + Mark("default") + " x" + Mark("reset") + "  "},
		{"ab" + Mark("red") + "cdef" + Mark("blue") + "g", 4, "ab" + Mark("red") + "cd" + Mark("reset")},
	} {
		if got := Fit(c.in, c.w); got != c.want {
			t.Errorf("Fit(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
}
