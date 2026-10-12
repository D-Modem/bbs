package vt

import (
	"bytes"
	"testing"
)

func TestParse(t *testing.T) {
	for _, c := range []struct {
		name, reply, want string
		level             int
		sixel, mono, hw   bool
	}{
		// DEC's own
		{"vt100", "\x1b[?1;2c\x1b[24;80R", "VT100", 1, false, false, false},
		{"vt102", "\x1b[?6c\x1b[24;80R", "VT102", 1, false, false, false},
		{"vt220", "\x1b[?62;1;2;6;7;8;9c\x1b[>1;10;0c\x1b[24;80R", "VT220", 2, false, true, true},
		{"vt340", "\x1b[?63;1;2;3;4;6;8;9;15c\x1b[>19;10;0c\x1b[24;80R", "VT340", 3, true, true, true},
		{"vt420", "\x1b[?64;1;2;6;8;9;15;18;21c\x1b[>41;20;0c\x1b[24;80R", "VT420", 4, false, true, true},
		{"vt525", "\x1b[?65;1;2;6;8;9;15;18;21;22c\x1b[>65;10;0c\x1b[24;80R", "VT525", 5, false, false, true},
		// emulators
		{"xterm", "\x1b[?64;1;2;4;6;9;15;16;17;18;21;22;28c\x1b[>41;390;0c\x1bP>|XTerm(390)\x1b\\\x1b[6;20;10t\x1b[24;80R",
			"XTerm(390)", 4, true, false, false},
		{"old xterm", "\x1b[?63;1;2;6;9;15;22c\x1b[>41;297;0c\x1b[24;80R", "xterm (patch 297)", 3, false, false, false},
		{"vte", "\x1b[?65;1;9c\x1b[>65;7600;1c\x1b[24;80R", "VTE", 5, false, false, false},
		{"tmux", "\x1b[?1;2c\x1b[>84;0;0c\x1b[24;80R", "tmux", 1, false, false, false},
		{"syncterm", "\x1b[=67;84;101;114;109;1;316c\x1b[24;80R", "SyncTERM (CTerm 1.316)", 0, false, false, false},
		{"putty", "PuTTY\x1b[?6c\x1b[>0;136;0c\x1b[24;80R", "PuTTY", 1, false, false, false},
		{"linux", "\x1b[?6c\x1b[24;80R", "VT102", 1, false, false, false},
		{"vt52", "\x1b/Z", "VT52", 0, false, false, false},
		{"nothing", "\x1b[24;80R", "", 0, false, false, false},
	} {
		in := Parse([]byte(c.reply))
		if in.Name() != c.want || in.Level() != c.level || in.Sixel() != c.sixel || in.Mono() != c.mono || in.Hardware() != c.hw {
			t.Errorf("%s: %q level %d sixel %v mono %v hw %v (%s)", c.name, in.Name(), in.Level(),
				in.Sixel(), in.Mono(), in.Hardware(), in.Details())
		}
	}
	in := Parse([]byte("\x1b[?64;4c\x1b[6;20;10t\x1b[24;80R"))
	if in.CellW != 10 || in.CellH != 20 || !Done([]byte("\x1b[24;80R")) || Done([]byte("\x1b[?6c")) {
		t.Errorf("cell %dx%d", in.CellW, in.CellH)
	}
	// junk is kept out of names
	if in := Parse([]byte("\x01bad\x7fname\x1bP>|ev\x07il(1)\x1b\\")); in.Answerback != "badname" || in.Version != "" {
		t.Errorf("%+v", in)
	}
}

func TestDECWriter(t *testing.T) {
	for _, c := range []struct {
		in     []string
		latin1 bool
		want   string
	}{
		{[]string{"┌─┐\r\n└─┘ ok"}, true, "\x1b(0lqk\r\nmqj\x1b(B ok"},
		{[]string{"café £5 ±1"}, true, "caf\xe9 \xa35 \xb11"},
		{[]string{"café £5 ±1"}, false, "cafe \x1b(0}\x1b(B5 \x1b(0g\x1b(B1"},
		{[]string{"“hi”… 日"}, true, "\"hi\"... ?"},
		// escape sequences pass, and do not end the graphics
		{[]string{"─\x1b[1;36m─x"}, true, "\x1b(0q\x1b[1;36mq\x1b(Bx"},
		{[]string{"\xe2\x94", "\x80\x1b[", "0m"}, true, "\x1b(0q\x1b[0m"},
	} {
		var buf bytes.Buffer
		d := NewDECWriter(&buf, c.latin1)
		for _, s := range c.in {
			d.Write([]byte(s))
		}
		d.Settle()
		want := c.want
		if bytes.Count([]byte(want), []byte("\x1b(0")) > bytes.Count([]byte(want), []byte("\x1b(B")) {
			want += "\x1b(B"
		}
		if buf.String() != want {
			t.Errorf("%q: %q, want %q", c.in, buf.String(), want)
		}
	}
}

func TestVT52Writer(t *testing.T) {
	var buf bytes.Buffer
	v := NewVT52Writer(&buf)
	v.Write([]byte("\x1b[2J\x1b[H\x1b[1;36mTitle\x1b[0m\r\n\x1b[3;10Hx\x1b[K\x1b[2A\x1bPq#0;2;0;0;0-\x1b\\é─"))
	want := "\x1bH\x1bJ\x1bY  Title\r\n\x1bY\")x\x1bK\x1bA\x1bAe-"
	if buf.String() != want {
		t.Errorf("%q\nwant %q", buf.String(), want)
	}
}
