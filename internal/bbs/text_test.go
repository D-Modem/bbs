package bbs

import (
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"  pad  ", "pad"},
		{"\x1b[31mred\x1b[0m", "[31mred[0m"},
		{"bell\x07 and nul\x00", "bell and nul"},
		{"tab\there", "tab here"},
		{"c1\u009b2J", "c12J"},
		{"bidi‮gnp.exe", "bidignp.exe"},
		{"bad\xffutf8", "bad?utf8"},
		{"fffd�x", "fffd?x"},
		{"été", "été"},
	}
	for _, c := range cases {
		if got := Clean(c.in, 100); got != c.want {
			t.Errorf("Clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := Clean(strings.Repeat("x", 50), 10); got != strings.Repeat("x", 10) {
		t.Errorf("Clean did not cut: %q", got)
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"alice", "Bob Smith", "x.y-z_1", "ab"} {
		if msg := ValidName(ok); msg != "" {
			t.Errorf("ValidName(%q) = %q", ok, msg)
		}
	}
	for _, bad := range []string{"a", "1abc", "new", "SysOp", "a  b", "bad\x1bname", "trailing ",
		strings.Repeat("a", 25), "émile"} {
		if ValidName(bad) == "" {
			t.Errorf("ValidName(%q) took it", bad)
		}
	}
}

func TestValidPassword(t *testing.T) {
	if ValidPassword("short", "x") == "" {
		t.Error("took a short password")
	}
	if ValidPassword("alicealice", "AliceAlice") == "" {
		t.Error("took the name as password")
	}
	if ValidPassword("good password", "alice") != "" {
		t.Error("refused a good password")
	}
	if ValidPassword(strings.Repeat("x", 73), "alice") == "" {
		t.Error("took a password bcrypt would cut")
	}
}

func TestWrap(t *testing.T) {
	got := Wrap("the quick brown fox jumps over the lazy dog", 15)
	for _, l := range got {
		if len(l) > 15 {
			t.Errorf("line too long: %q", l)
		}
	}
	if strings.Join(got, " ") != "the quick brown fox jumps over the lazy dog" {
		t.Errorf("Wrap lost words: %q", got)
	}
	if got := Wrap(strings.Repeat("x", 25), 10); len(got) != 3 {
		t.Errorf("a long word: %q", got)
	}
}

func TestCleanBody(t *testing.T) {
	body := CleanBody([]string{"  indented", "esc\x1b[2Jape", "", "", ""})
	if body != "  indented\nesc[2Jape" {
		t.Errorf("CleanBody = %q", body)
	}
	many := make([]string, MaxBodyLines+10)
	for i := range many {
		many[i] = "line"
	}
	if n := strings.Count(CleanBody(many), "\n") + 1; n != MaxBodyLines {
		t.Errorf("CleanBody kept %d lines", n)
	}
}

func TestReplySubject(t *testing.T) {
	if ReplySubject("hello") != "Re: hello" || ReplySubject("RE: hello") != "RE: hello" {
		t.Error("ReplySubject")
	}
}

func TestQuote(t *testing.T) {
	body := strings.Repeat("word ", 15) + "end\nnext line\n\n> old quote"
	got := Quote("bob", body, 10)
	want := []string{"bob wrote:",
		"> " + strings.TrimSpace(strings.Repeat("word ", 15)), "> end next line",
		">", "> > old quote", ""}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Quote =\n%q\nwant\n%q", got, want)
	}
}

func TestPrefs(t *testing.T) {
	p := ParsePrefs("utf8=off width=40 bogus=1 ansi=maybe rip=on")
	if p.String() != "rip=on utf8=off width=40" {
		t.Errorf("parsed %q", p)
	}
	if on, ok := p.On("rip"); !on || !ok {
		t.Error("rip")
	}
	if _, ok := p.On("ansi"); ok {
		t.Error("ansi set")
	}
	got, err := ChangePrefs("utf8=off width=40", map[string]string{"width": "", "editor": "full"})
	if err != nil || got != "editor=full utf8=off" {
		t.Errorf("changed %q %v", got, err)
	}
	for _, bad := range []map[string]string{{"width": "132"}, {"nope": "on"}, {"nope": ""}} {
		if _, err := ChangePrefs("", bad); err == nil {
			t.Errorf("%v taken", bad)
		}
	}
}

func TestWrapBody(t *testing.T) {
	long := strings.Repeat("word ", 30) + "https://example.org/end"
	got := WrapBody("short\r\n" + long + "\n> " + long)
	if got[0] != "short" {
		t.Fatalf("got %q", got)
	}
	var plain, quoted []string
	for _, l := range got[1:] {
		if len([]rune(l)) > MaxLineLen {
			t.Errorf("line too long: %q", l)
		}
		if strings.HasPrefix(l, "> ") {
			quoted = append(quoted, strings.TrimPrefix(l, "> "))
		} else {
			plain = append(plain, l)
		}
	}
	if strings.Join(plain, " ") != strings.TrimSpace(long) || strings.Join(quoted, " ") != strings.TrimSpace(long) {
		t.Errorf("words lost: %q", got)
	}
	if len(quoted) < 2 {
		t.Errorf("quote not carried on: %q", got)
	}
}
