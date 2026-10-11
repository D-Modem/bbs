package screens

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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

func TestSpans(t *testing.T) {
	got := Spans("plain {red}red {bright}hot{normal}{default} end")
	want := []Span{{"plain ", "", false}, {"red ", "red", false}, {"hot", "red", true}, {" end", "", false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%+v", got)
	}
	if s := StripTags("{blue}a{bright}b"); s != "ab" {
		t.Error(s)
	}
	if !strings.Contains(StripTags("{nope}"), "{nope}") {
		t.Error("an unknown tag was taken")
	}
}
