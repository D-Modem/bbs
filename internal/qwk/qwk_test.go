package qwk

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestID(t *testing.T) {
	for in, want := range map[string]string{"The Cave BBS": "THECAVEB", "x-1": "X1", "!!": "BBS"} {
		if got := MakeID(in); got != want || !ValidID(got) {
			t.Errorf("MakeID(%q) = %q, want %q", in, got, want)
		}
	}
	if ValidID("toolongid") || ValidID("a b") || ValidID("") {
		t.Error("ValidID took a bad id")
	}
}

func TestMBF(t *testing.T) {
	// known values: 1 is 00 00 00 81, 2 is 00 00 00 82, 3 is 00 00 40 82
	for n, want := range map[int][]byte{0: {0, 0, 0, 0}, 1: {0, 0, 0, 0x81}, 2: {0, 0, 0, 0x82}, 3: {0, 0, 0x40, 0x82}} {
		if got := mbf(n); !bytes.Equal(got, want) {
			t.Errorf("mbf(%d) = % x, want % x", n, got, want)
		}
	}
}

func TestPacketRoundTrip(t *testing.T) {
	when := time.Date(2026, 3, 4, 13, 45, 0, 0, time.UTC)
	long := "A subject a good deal longer than twenty-five characters"
	p := &Packet{BBSID: "TESTBBS", BBSName: "Test BBS", Location: "Here", Phone: "555", Sysop: "Sysop",
		User: "Alice", Created: when, Welcome: "Hello there",
		Confs: []Conference{{0, "Mail"}, {3, "General"}},
		Messages: []Message{
			{Conf: 0, Number: 7, Private: true, To: "Alice", From: "Bob", Subject: "Hi", Date: when, Body: "Line one\nCafé ░ π\n\nend"},
			{Conf: 3, Number: 12, To: "All", From: "Carol", Subject: long, Date: when, Ref: 11,
				Body: strings.Repeat("x", 300)},
		}}
	var b bytes.Buffer
	if err := p.Write(&b); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range z.File {
		names[f.Name] = true
	}
	for _, n := range []string{"CONTROL.DAT", "MESSAGES.DAT", "000.NDX", "003.NDX", "PERSONAL.NDX", "DOOR.ID", "HELLO"} {
		if !names[n] {
			t.Errorf("no %s in the packet: %v", n, names)
		}
	}
	got, err := ReadPacket(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.BBSID != "TESTBBS" || got.BBSName != "Test BBS" || got.User != "ALICE" || got.Sysop != "Sysop" ||
		!got.Created.Equal(when) || len(got.Confs) != 2 || got.Confs[1] != (Conference{3, "General"}) {
		t.Errorf("packet = %+v", got)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("%d messages", len(got.Messages))
	}
	m := got.Messages[0]
	if m.Conf != 0 || m.Number != 7 || !m.Private || m.To != "Alice" || m.From != "Bob" || !m.Date.Equal(when) ||
		m.Body != "Line one\nCafé ░ ?\n\nend" {
		t.Errorf("message 1 = %+v", m)
	}
	m = got.Messages[1]
	if m.Conf != 3 || m.Subject != long || m.Ref != 11 || m.Body != strings.Repeat("x", 300) {
		t.Errorf("message 2 = %+v", m)
	}
}

func TestREP(t *testing.T) {
	ms := []Message{
		{Conf: 0, Private: true, To: "Sysop", From: "Alice", Subject: "Help", Body: "Please"},
		{Conf: 3, To: "All", From: "Alice", Subject: strings.Repeat("s", 40), Ref: 12, Body: "Reply\nhere"},
	}
	var b bytes.Buffer
	if err := WriteREP(&b, "TESTBBS", ms); err != nil {
		t.Fatal(err)
	}
	got, err := ReadREP(b.Bytes(), "TESTBBS")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d messages", len(got))
	}
	for i := range ms {
		g := got[i]
		g.Date = time.Time{}
		if g != ms[i] {
			t.Errorf("message %d = %+v, want %+v", i, g, ms[i])
		}
	}
	if _, err := ReadREP(b.Bytes(), "OTHER"); !errors.Is(err, ErrNotREP) {
		t.Errorf("another BBS's REP: %v", err)
	}
	if _, err := ReadREP([]byte("nonsense"), "TESTBBS"); !errors.Is(err, ErrNotREP) {
		t.Errorf("nonsense: %v", err)
	}
	// the bare .MSG, ended by ^Z, a bad block count
	z, _ := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	r, _ := z.File[0].Open()
	var raw bytes.Buffer
	raw.ReadFrom(r)
	msg := append(raw.Bytes(), 0x1a)
	if got, err := ReadREP(msg, "TESTBBS"); err != nil || len(got) != 2 {
		t.Errorf("bare .MSG: %d, %v", len(got), err)
	}
	bad := bytes.Clone(raw.Bytes())
	copy(bad[block+116:], "999999")
	if _, err := ReadREP(bad, "TESTBBS"); err == nil {
		t.Error("a bad block count passed")
	}
}
