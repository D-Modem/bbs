package vt

import (
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// seq splits a stream into text runes and whole escape sequences (ESC x,
// ESC ( x, CSI ... final, DCS or OSC ... ST), across writes.
type seq struct {
	state int // 0 text, 1 ESC, 2 ESC intermediate, 3 CSI, 4 DCS/OSC, 5 ESC in DCS/OSC
	buf   []byte
	part  []byte // a rune begun
}

// feed gives each text rune to text and each sequence to esc.
func (s *seq) feed(b []byte, text func(rune), esc func([]byte)) {
	for _, c := range b {
		switch s.state {
		case 0:
			switch {
			case c == 0x1b:
				s.state, s.buf = 1, append(s.buf[:0], c)
			case len(s.part) > 0 || c >= 0x80:
				s.part = append(s.part, c)
				if utf8.FullRune(s.part) || len(s.part) >= utf8.UTFMax {
					r, _ := utf8.DecodeRune(s.part)
					s.part = s.part[:0]
					text(r)
				}
			default:
				text(rune(c))
			}
		case 1:
			s.buf = append(s.buf, c)
			switch {
			case c == '[':
				s.state = 3
			case c == 'P' || c == ']':
				s.state = 4
			case c >= 0x20 && c <= 0x2f:
				s.state = 2
			default:
				s.state = 0
				esc(s.buf)
			}
		case 2:
			s.buf = append(s.buf, c)
			if c >= 0x30 {
				s.state = 0
				esc(s.buf)
			}
		case 3:
			s.buf = append(s.buf, c)
			if c >= 0x40 && c <= 0x7e {
				s.state = 0
				esc(s.buf)
			} else if len(s.buf) > 64 {
				s.state = 0 // not a sequence of ours: dropped
			}
		case 4, 5:
			s.buf = append(s.buf, c)
			switch {
			case c == 0x07 && s.buf[1] == ']':
				s.state = 0
				esc(s.buf)
			case s.state == 5 && c == '\\':
				s.state = 0
				esc(s.buf)
			default:
				s.state = 4
				if c == 0x1b {
					s.state = 5
				}
			}
		}
	}
}

// graphics: what DEC Special Graphics (ESC ( 0) draws, by the character
// that draws it
var graphics = map[rune]byte{
	'─': 'q', '━': 'q', '═': 'q', '│': 'x', '┃': 'x', '║': 'x',
	'┌': 'l', '┏': 'l', '╔': 'l', '╓': 'l', '╒': 'l',
	'┐': 'k', '┓': 'k', '╗': 'k', '╖': 'k', '╕': 'k',
	'└': 'm', '┗': 'm', '╚': 'm', '╙': 'm', '╘': 'm',
	'┘': 'j', '┛': 'j', '╝': 'j', '╜': 'j', '╛': 'j',
	'├': 't', '┣': 't', '╠': 't', '╟': 't', '╞': 't',
	'┤': 'u', '┫': 'u', '╣': 'u', '╢': 'u', '╡': 'u',
	'┬': 'w', '┳': 'w', '╦': 'w', '╥': 'w', '╤': 'w',
	'┴': 'v', '┻': 'v', '╩': 'v', '╨': 'v', '╧': 'v',
	'┼': 'n', '╋': 'n', '╬': 'n', '╫': 'n', '╪': 'n',
	'░': 'a', '▒': 'a', '▓': 'a', '█': 'a', '▀': 'a', '▄': 'a', '▌': 'a', '▐': 'a',
	'◆': '`', '♦': '`', '≤': 'y', '≥': 'z', 'π': '{', '≠': '|', '⎺': 'o', '⎻': 'p', '⎼': 'r', '⎽': 's',
	'␉': 'b', '␌': 'c', '␍': 'd', '␊': 'e', '␤': 'h', '␋': 'i',
}

// graphics also for Latin-1's, where Latin-1 cannot go
var graphicsLatin = map[rune]byte{'°': 'f', '±': 'g', '£': '}', '·': '~'}

// like: ASCII for what has no character
var like = map[rune]string{
	'–': "-", '—': "-", '‘': "'", '’': "'", '‚': ",", '“': "\"", '”': "\"", '„': "\"",
	'…': "...", '•': "*", '«': "<<", '»': ">>", '×': "x", '÷': "/", '©': "(c)", '®': "(R)",
	'ß': "ss", 'æ': "ae", 'Æ': "AE", 'ø': "o", 'Ø': "O", ' ': " ", '¡': "!", '¿': "?",
	'€': "EUR", '™': "TM", '←': "<-", '→': "->", '↑': "^", '↓': "v",
	'─': "-", '│': "|", '┼': "+", '░': "#", '▒': "#", '▓': "#", '█': "#",
}

// ascii is r as ASCII: itself, a look alike, or the letter without its
// accent; "?" if none.
func ascii(r rune) string {
	if r < 0x80 {
		return string(r)
	}
	if s, ok := like[r]; ok {
		return s
	}
	if d := norm.NFD.String(string(r)); d != string(r) {
		if b, _ := utf8.DecodeRuneInString(d); b < 0x80 {
			return string(b)
		}
	}
	if _, ok := graphics[r]; ok {
		return "+"
	}
	return "?"
}

// DECWriter writes UTF-8 to a VT that has no UTF-8: Latin-1 (a VT220's
// DEC Multinational has it) as 8-bit characters when Latin1, line
// drawing and blocks in DEC Special Graphics, anything else as ASCII.
// Escape sequences pass as they are.
type DECWriter struct {
	w      io.Writer
	Latin1 bool

	s   seq
	gfx bool // G0 is the graphics set
	out []byte
}

// NewDECWriter writes to w.
func NewDECWriter(w io.Writer, latin1 bool) *DECWriter { return &DECWriter{w: w, Latin1: latin1} }

func (d *DECWriter) Write(b []byte) (int, error) {
	d.out = d.out[:0]
	d.s.feed(b, d.text, func(e []byte) { d.out = append(d.out, e...) })
	if len(d.out) > 0 {
		if _, err := d.w.Write(d.out); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

func (d *DECWriter) text(r rune) {
	g, isG := graphics[r]
	if !isG && !d.Latin1 {
		g, isG = graphicsLatin[r]
	}
	if isG {
		if !d.gfx {
			d.out = append(d.out, "\x1b(0"...)
			d.gfx = true
		}
		d.out = append(d.out, g)
		return
	}
	if d.gfx && r >= 0x20 && r != 0x7f {
		d.out = append(d.out, "\x1b(B"...)
		d.gfx = false
	}
	switch {
	case r < 0x80:
		d.out = append(d.out, byte(r))
	case d.Latin1 && r >= 0xa0 && r <= 0xff:
		d.out = append(d.out, byte(r))
	default:
		d.out = append(d.out, ascii(r)...)
	}
}

// Settle puts G0 back to ASCII (before the BBS waits for a key, or the
// line is given to a file transfer).
func (d *DECWriter) Settle() error {
	if !d.gfx {
		return nil
	}
	d.gfx = false
	_, err := d.w.Write([]byte("\x1b(B"))
	return err
}

// Raw writes b as it is.
func (d *DECWriter) Raw(b []byte) error {
	if err := d.Settle(); err != nil {
		return err
	}
	_, err := d.w.Write(b)
	return err
}

// VT52Writer writes for a VT52: ANSI's clearing and cursor movement as
// its escapes, attributes and what it has no escape for dropped, ASCII
// only.
type VT52Writer struct {
	w   io.Writer
	s   seq
	out []byte
}

// NewVT52Writer writes to w.
func NewVT52Writer(w io.Writer) *VT52Writer { return &VT52Writer{w: w} }

func (v *VT52Writer) Write(b []byte) (int, error) {
	v.out = v.out[:0]
	v.s.feed(b, func(r rune) { v.out = append(v.out, ascii(r)...) }, v.esc)
	if len(v.out) > 0 {
		if _, err := v.w.Write(v.out); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

func (v *VT52Writer) esc(e []byte) {
	if len(e) < 3 || e[1] != '[' {
		return // ESC 7, charsets, DCS (sixel), OSC: nothing on a VT52
	}
	params, final := string(e[2:len(e)-1]), e[len(e)-1]
	if strings.ContainsAny(params, "?>=<!") {
		return
	}
	ns := strings.Split(params, ";")
	num := func(i, def int) int {
		if i < len(ns) {
			if n, err := strconv.Atoi(ns[i]); err == nil && n > 0 {
				return min(n, 100)
			}
		}
		return def
	}
	rep := func(s string, n int) {
		for i := 0; i < n; i++ {
			v.out = append(v.out, s...)
		}
	}
	switch final {
	case 'J':
		if num(0, 0) == 2 {
			v.out = append(v.out, "\x1bH\x1bJ"...)
		} else {
			v.out = append(v.out, "\x1bJ"...)
		}
	case 'K':
		if num(0, 0) == 0 {
			v.out = append(v.out, "\x1bK"...)
		}
	case 'H', 'f':
		row, col := min(num(0, 1), 24), min(num(1, 1), 80)
		v.out = append(v.out, 0x1b, 'Y', byte(31+row), byte(31+col))
	case 'A':
		rep("\x1bA", num(0, 1))
	case 'B':
		rep("\x1bB", num(0, 1))
	case 'C':
		rep("\x1bC", num(0, 1))
	case 'D':
		rep("\x1bD", num(0, 1))
	}
}

// Settle has nothing to do on a VT52.
func (v *VT52Writer) Settle() error { return nil }

// Raw writes b as it is.
func (v *VT52Writer) Raw(b []byte) error {
	_, err := v.w.Write(b)
	return err
}
