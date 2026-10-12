package petscii

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

func run(width int, parts ...string) []byte {
	var buf bytes.Buffer
	p := NewWriter(&buf, width)
	for _, s := range parts {
		p.Write([]byte(s))
	}
	p.Settle()
	return buf.Bytes()
}

func TestWriter(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want []byte
	}{
		// letters as the lower/upper case set has them
		{[]string{"Hello, World 42!"}, []byte("\xc8ELLO, \xd7ORLD 42!")},
		// CR LF is RETURN, a lone LF is down
		{[]string{"ab\r\ncd\n"}, []byte{0x41, 0x42, Return, 0x43, 0x44, Down}},
		// CR LF split across writes is still RETURN
		{[]string{"ab\r", "\nc"}, []byte{0x41, 0x42, Return, 0x43}},
		// a CR alone goes back to column 0
		{[]string{"abc\rx"}, []byte{0x41, 0x42, 0x43, Left, Left, Left, 0x58}},
		{[]string{"abc\r"}, []byte{0x41, 0x42, 0x43, Left, Left, Left}},
		// colors, bold, reset, reverse
		{[]string{"\x1b[1;36mA\x1b[0m"}, []byte{0x9F, 0xC1, LightGray}},
		{[]string{"\x1b[1;33mA\x1b[0;37mb"}, []byte{0x9E, 0xC1, LightGray, 0x42}},
		{[]string{"\x1b[31mr\x1b[1mR"}, []byte{0x1C, 0x52, 0x96, 0xD2}},
		{[]string{"\x1b[7mx\x1b[27m"}, []byte{ReverseOn, LightGray, 0x58, ReverseOff, LightGray}},
		// clear and home, cursor movement
		{[]string{"\x1b[2J\x1b[H"}, []byte{Clear, Home}},
		{[]string{"\x1b[3;5H"}, []byte{Home, Down, Down, Right, Right, Right, Right}},
		{[]string{"ab\x1b[2D\x1b[A"}, []byte{0x41, 0x42, Left, Left, Up}},
		// what is not ANSI's here goes
		{[]string{"\x1b7\x1b[?25l\x1b[6n\x1b8z"}, []byte{0x5A}},
		// box drawing, blocks, others
		{[]string{"┌─┐│└┘"}, []byte{0xB0, 0xC0, 0xAE, 0xDD, 0xAD, 0xBD}},
		{[]string{"╔═╗"}, []byte{0xB0, 0xC0, 0xAE}},
		{[]string{"█"}, []byte{ReverseOn, 0x20, ReverseOff}},
		{[]string{"_|£"}, []byte{0xA4, 0xDD, 0x5C}},
		{[]string{"é…{x}"}, []byte{0x45, '.', '.', '.', '(', 0x58, ')'}},
		{[]string{"日"}, []byte{'?'}},
		// a rune split across writes
		{[]string{"\xe2\x94", "\x80"}, []byte{0xC0}},
		// backspace erasing
		{[]string{"a\b \b"}, []byte{0x41, Left, ' ', Left}},
	} {
		if got := run(40, c.in...); !bytes.Equal(got, c.want) {
			t.Errorf("%q: % x, want % x", c.in, got, c.want)
		}
	}
}

func TestWrapAndErase(t *testing.T) {
	// 40 characters wrap: column 0 again, so CR goes nowhere
	line := bytes.Repeat([]byte("a"), 40)
	got := run(40, string(line)+"\rb")
	if !bytes.Equal(got, append(bytes.Repeat([]byte{0x41}, 40), 0x42)) {
		t.Errorf("% x", got)
	}
	// erase to the end of the line: spaces and back
	got = run(40, "abc\x1b[K")
	want := append([]byte{0x41, 0x42, 0x43}, bytes.Repeat([]byte{' '}, 36)...)
	want = append(want, bytes.Repeat([]byte{Left}, 36)...)
	if !bytes.Equal(got, want) {
		t.Errorf("% x", got)
	}
}

func TestDecode(t *testing.T) {
	for b, want := range map[byte]rune{
		0x41: 'a', 0x5A: 'z', 0xC1: 'A', 0xDA: 'Z', 0x61: 'A', 0x7A: 'Z',
		Return: '\r', Del: '\b', '1': '1', ' ': ' ', '@': '@', 0x5C: '£',
		Up: utf8.RuneError, Clear: utf8.RuneError, 0x85: utf8.RuneError,
	} {
		if got := Decode(b); got != want {
			t.Errorf("%#x: %q, want %q", b, got, want)
		}
	}
	// what is written reads back
	for _, r := range "The quick brown fox, 12 Main St. @home" {
		if got := Decode(Encode(r)[0]); got != r {
			t.Errorf("%q came back as %q", r, got)
		}
	}
}
