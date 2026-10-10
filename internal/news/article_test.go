package news

import (
	"strings"
	"testing"
	"time"
)

const sample = "Path: a.example!b.example!not-for-mail\r\n" +
	"From: =?ISO-8859-1?Q?J=F6rg_Schmidt?= <joerg@example.org>\r\n" +
	"Newsgroups: bbs.general,bbs.test\r\n" +
	"Subject: =?UTF-8?B?w4ljaG8=?= and\r\n" +
	"\tmore\r\n" +
	"Date: Sat, 10 Oct 2026 12:00:00 +0000\r\n" +
	"Message-ID: <abc.123@example.org>\r\n" +
	"Content-Type: text/plain; charset=iso-8859-1\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n" +
	"\r\n" +
	"Gr=FC=DFe\r\n" +
	".dot line\r\n"

func TestParse(t *testing.T) {
	a, err := Parse([]byte(sample), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(a, "<abc.123@example.org>"); err != nil {
		t.Fatal(err)
	}
	if got := Poster(a); got != "Jörg Schmidt" {
		t.Errorf("poster %q", got)
	}
	if got := Subject(a); got != "Écho and more" {
		t.Errorf("subject %q", got)
	}
	if got := Text(a); got != "Grüße\n.dot line\n" {
		t.Errorf("text %q", got)
	}
	if p := Path(a); len(p) != 3 || p[0] != "a.example" {
		t.Errorf("path %v", p)
	}
	if g := Groups(a.Get("Newsgroups")); len(g) != 2 || g[1] != "bbs.test" {
		t.Errorf("groups %v", g)
	}
	// round trip
	b, _ := Parse(a.Bytes(), 1<<20)
	if string(b.Bytes()) != string(a.Bytes()) {
		t.Error("not stable")
	}
}

func TestCheck(t *testing.T) {
	base := func() *Article { a, _ := Parse([]byte(sample), 1<<20); return a }
	for what, f := range map[string]func(*Article){
		"no from":      func(a *Article) { a.Set("From", "") },
		"bad id":       func(a *Article) { a.Set("Message-ID", "<no at sign>") },
		"bad date":     func(a *Article) { a.Set("Date", "yesterday") },
		"control":      func(a *Article) { a.Set("Control", "cancel <x@y>") },
		"bad group":    func(a *Article) { a.Set("Newsgroups", "Bad Group") },
		"other msg id": func(a *Article) { a.Set("Message-ID", "<other@example.org>") },
	} {
		a := base()
		f(a)
		if Check(a, "<abc.123@example.org>") == nil {
			t.Errorf("%s: taken", what)
		}
	}
	if _, err := Parse([]byte(strings.Repeat("X-A: b\r\n", MaxHeaderFields+1)+"\r\n"), 1<<20); err == nil {
		t.Error("too many fields taken")
	}
	if _, err := Parse([]byte(sample), 100); err == nil {
		t.Error("too large taken")
	}
}

func TestNew(t *testing.T) {
	id := NewMessageID("bbs.example")
	if !ValidMessageID(id) {
		t.Fatalf("id %q", id)
	}
	a := New(Local{Name: "Ålice", Address: "alice@bbs.example", Group: "bbs.general",
		Subject: "Grüße", Body: "line one\nline two", MessageID: id, PathHost: "bbs.example",
		References: []string{"<x@y>"}})
	b, err := Parse(a.Bytes(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(b, id); err != nil {
		t.Fatal(err)
	}
	if Poster(b) != "Ålice" || Subject(b) != "Grüße" || Text(b) != "line one\nline two\n" {
		t.Errorf("%q %q %q", Poster(b), Subject(b), Text(b))
	}
	if b.Get("Path") != "bbs.example!not-for-mail" || b.Get("References") != "<x@y>" {
		t.Errorf("path %q refs %q", b.Get("Path"), b.Get("References"))
	}
	if _, err := time.Parse(time.RFC1123Z, b.Get("Date")); err != nil {
		t.Error(err)
	}
}

func TestMultipart(t *testing.T) {
	raw := "From: x@y.z\r\nNewsgroups: a.b\r\nSubject: s\r\nDate: Sat, 10 Oct 2026 12:00:00 +0000\r\n" +
		"Message-ID: <m@y.z>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=XX\r\n\r\n" +
		"--XX\r\nContent-Type: text/html\r\n\r\n<b>no</b>\r\n--XX\r\nContent-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\naGVsbG8g\r\nd29ybGQ=\r\n--XX--\r\n"
	a, err := Parse([]byte(raw), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got := Text(a); got != "hello world" {
		t.Errorf("text %q", got)
	}
}

func TestWildmat(t *testing.T) {
	cases := []struct {
		p, s string
		ok   bool
	}{
		{"*", "bbs.general", true},
		{"bbs.*", "bbs.general", true},
		{"bbs.*,!bbs.test", "bbs.test", false},
		{"bbs.*,!bbs.test", "bbs.tests", true},
		{"comp.?ang.go", "comp.lang.go", true},
		{"alt.*", "bbs.x", false},
		{"*a*a*a*a*a*a*a*a*a*a*b", strings.Repeat("a", 80), false},
	}
	for _, c := range cases {
		if Wildmat(c.p, c.s) != c.ok {
			t.Errorf("%q %q", c.p, c.s)
		}
	}
	if !ValidGroup("bbs.general") || ValidGroup("general") || ValidGroup("BBS.General") || ValidGroup("a..b") {
		t.Error("ValidGroup")
	}
}
