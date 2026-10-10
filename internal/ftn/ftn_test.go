package ftn

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAddr(t *testing.T) {
	for in, want := range map[string]string{
		"2:5020/1":            "2:5020/1",
		"21:1/100.5":          "21:1/100.5",
		"1:2/3.0@FidoNet":     "1:2/3@fidonet",
		" 3:633/280 ":         "3:633/280",
		"2:5020/1@fsxnet.xyz": "",
		"2:5020":              "",
		"0:1/1":               "",
		"2:-1/1":              "",
		"2:1/1.":              "",
		"2:99999/1":           "",
		"a:b/c":               "",
	} {
		a, err := ParseAddr(in)
		if want == "" {
			if err == nil {
				t.Errorf("%q: read as %v", in, a)
			}
			continue
		}
		if err != nil || a.String() != want {
			t.Errorf("%q: %v %v, want %s", in, a, err, want)
		}
	}
}

func TestNet2D(t *testing.T) {
	got := ParseNet2D("5020/1 2 3 5030/4.1 x 10")
	want := []Net2D{{5020, 1}, {5020, 2}, {5020, 3}, {5030, 4}, {5030, 10}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	var many []Net2D
	for i := 1; i <= 40; i++ {
		many = append(many, Net2D{5020, i * 100})
	}
	many = append(many, Net2D{1, 1})
	lines := FormatNet2D("SEEN-BY: ", many, 70)
	var back []Net2D
	for _, l := range lines {
		if len(l) > 79 {
			t.Errorf("long line %q", l)
		}
		back = append(back, ParseNet2D(strings.TrimPrefix(l, "SEEN-BY: "))...)
	}
	if !reflect.DeepEqual(back, many) {
		t.Fatalf("%v", lines)
	}
}

func TestPacket(t *testing.T) {
	var b bytes.Buffer
	h := &Header{Orig: Addr{Zone: 21, Net: 1, Node: 100, Point: 2}, Dest: Addr{Zone: 21, Net: 1, Node: 1},
		Password: "SECRET", Date: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	if err := WriteHeader(&b, h); err != nil {
		t.Fatal(err)
	}
	m := &Message{OrigNode: 100, DestNode: 1, OrigNet: 1, DestNet: 1, DateTime: "10 Oct 26  12:00:00",
		To: "All", From: "Alice Smith", Subject: "Hello", Area: "FSX_GEN",
		Kludges: []Kludge{{"MSGID", "21:1/100.2 1234abcd"}, {"CHRS", "CP437 2"}},
		Lines:   []string{"Hi there \x82", "", "--- bbs-sec", " * Origin: Somewhere (21:1/100.2)"},
		SeenBy:  []Net2D{{1, 1}, {1, 100}}, Path: []Net2D{{1, 100}}}
	WriteMessage(&b, m)
	nm := &Message{OrigNode: 100, DestNode: 3, OrigNet: 1, DestNet: 2, Attr: AttrPrivate,
		DateTime: "10 Oct 26  12:00:01", To: "Sysop", From: "Alice", Subject: "Netmail",
		Kludges: []Kludge{{"INTL", "21:2/3 21:1/100"}, {"FMPT", "2"}, {"MSGID", "21:1/100.2 1234abce"}},
		Lines:   []string{"Private"}}
	WriteMessage(&b, nm)
	WriteEnd(&b)

	r, rh, err := NewReader(bytes.NewReader(b.Bytes()), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !rh.TwoPlus || !rh.Orig.Same(h.Orig) || !rh.Dest.Same(h.Dest) || rh.Password != "SECRET" || !rh.Date.Equal(h.Date) {
		t.Fatalf("header %+v", rh)
	}
	for _, want := range []*Message{m, nm} {
		got, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v\nwant %+v", got, want)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatal(err)
	}
	if d := m.Decoder()(m.Lines[0]); d != "Hi there é" {
		t.Errorf("CP437: %q", d)
	}
	if !ParseDate(m.DateTime).Equal(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)) {
		t.Error("date")
	}
	if !ParseDate("Sat 10 Oct 26 12:00").Equal(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)) {
		t.Error("SEAdog date")
	}

	// too big: skipped, the next one read
	r, _, _ = NewReader(bytes.NewReader(b.Bytes()), 80)
	if _, err := r.Next(); !errors.Is(err, ErrTooBig) {
		t.Fatal(err)
	}
	if got, err := r.Next(); err != nil || got.Subject != "Netmail" {
		t.Fatal(got, err)
	}
	// cut short
	r, _, _ = NewReader(bytes.NewReader(b.Bytes()[:100]), 1000)
	if _, err := r.Next(); !errors.Is(err, ErrPacket) {
		t.Fatal(err)
	}
	if _, _, err := NewReader(strings.NewReader("PK\x03\x04"), 10); err == nil {
		t.Fatal("zip read as a packet")
	}
}

func TestMSGID(t *testing.T) {
	a := Addr{Zone: 2, Net: 5020, Node: 1}
	id := MessageID("2:5020/1@fidonet 1234abcd")
	if id != "<MSGID_2=3A5020=2F1=40fidonet_1234abcd@fidonet.org>" {
		t.Fatal(id)
	}
	if back := MSGIDOf(id, a); back != "2:5020/1@fidonet 1234abcd" {
		t.Fatal(back)
	}
	if got := MSGIDOf("<abc@news.example>", a); !strings.HasPrefix(got, "2:5020/1 ") || len(got) != len("2:5020/1 ")+8 {
		t.Fatal(got)
	}
	x, y := NewMSGID(a), NewMSGID(a)
	if x == y {
		t.Fatal("repeated", x)
	}
}

func TestEncoder(t *testing.T) {
	if s := (Encoder{}).String("café ☃\x1b"); s != "caf\x82 ??" {
		t.Fatalf("%q", s)
	}
	m := &Message{Kludges: []Kludge{{"CHRS", "UTF-8 4"}}}
	if s := m.Decoder()("caf\xc3\xa9"); s != "café" {
		t.Fatal(s)
	}
	m = &Message{}
	if s := m.Decoder()("caf\xc3\xa9"); s != "café" {
		t.Fatal("unsaid UTF-8:", s)
	}
	if s := m.Decoder()("caf\x82"); s != "café" {
		t.Fatal("unsaid CP437:", s)
	}
	m = &Message{Kludges: []Kludge{{"CHRS", "CP866 2"}}}
	if s := m.Decoder()("\x8f\xe0\xa8\xa2\xa5\xe2"); s != "Привет" {
		t.Fatal(s)
	}
}
