// Package petscii is the Commodore 64's and 128's character set, for
// callers with CCGMS, Novaterm, SyncTERM in C64 mode and the like.
//
// Writer turns what the BBS writes for an ANSI terminal (UTF-8 text, SGR
// colors, clearing, cursor movement) into PETSCII for a C64 in its
// lower/upper case set: letters swapped as that set has them, colors as
// the C64's color codes, box drawing as its graphics characters, CR LF as
// RETURN.  Decode turns a C64's keys back into runes.
package petscii

import (
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// PETSCII control codes.
const (
	Return      = 0x0D
	Lowercase   = 0x0E // the lower/upper case set
	LockCase    = 0x08 // shift + C= no longer switches sets
	Down        = 0x11
	Up          = 0x91
	Right       = 0x1D
	Left        = 0x9D
	Home        = 0x13
	Clear       = 0x93
	Del         = 0x14
	ReverseOn   = 0x12
	ReverseOff  = 0x92
	White       = 0x05
	LightGray   = 0x9B
	DefaultText = LightGray
)

// colors: ANSI 30-37, normal and bright
var colors = [8][2]byte{
	{0x90, 0x97}, // black, dark gray
	{0x1C, 0x96}, // red, light red
	{0x1E, 0x99}, // green, light green
	{0x95, 0x9E}, // brown, yellow
	{0x1F, 0x9A}, // blue, light blue
	{0x9C, 0x9C}, // purple
	{0x9F, 0x9F}, // cyan
	{0x9B, 0x05}, // light gray, white
}

// graphics: Unicode's box drawing and blocks, as the set's characters
var graphics = map[rune]byte{
	'─': 0xC0, '━': 0xC0, '═': 0xC0,
	'│': 0xDD, '┃': 0xDD, '║': 0xDD,
	'┌': 0xB0, '┏': 0xB0, '╔': 0xB0, '╓': 0xB0, '╒': 0xB0,
	'┐': 0xAE, '┓': 0xAE, '╗': 0xAE, '╖': 0xAE, '╕': 0xAE,
	'└': 0xAD, '┗': 0xAD, '╚': 0xAD, '╙': 0xAD, '╘': 0xAD,
	'┘': 0xBD, '┛': 0xBD, '╝': 0xBD, '╜': 0xBD, '╛': 0xBD,
	'├': 0xAB, '┣': 0xAB, '╠': 0xAB, '╟': 0xAB, '╞': 0xAB,
	'┤': 0xB3, '┫': 0xB3, '╣': 0xB3, '╢': 0xB3, '╡': 0xB3,
	'┬': 0xB2, '┳': 0xB2, '╦': 0xB2, '╥': 0xB2, '╤': 0xB2,
	'┴': 0xB1, '┻': 0xB1, '╩': 0xB1, '╨': 0xB1, '╧': 0xB1,
	'┼': 0xDB, '╋': 0xDB, '╬': 0xDB, '╫': 0xDB, '╪': 0xDB,
	'▌': 0xA1, '▄': 0xA2, '▔': 0xA3, '▁': 0xA4, '▏': 0xA5,
	'░': 0xA6, '▒': 0xA6, '▓': 0xA6, '▕': 0xA7, '▗': 0xAC,
	'▂': 0xAF, '▎': 0xB4, '▃': 0xB9, '✓': 0xBA, '▖': 0xBB,
	'▝': 0xBC, '▘': 0xBE, '▚': 0xBF,
	'£': 0x5C, '↑': 0x5E, '←': 0x5F, '_': 0xA4, '|': 0xDD,
	' ': 0x20,
}

// reversed: blocks that are another character in reverse
var reversed = map[rune]byte{'█': 0x20, '▀': 0xA2, '▐': 0xA1}

// like: what a character can be shown as instead
var like = map[rune]string{
	'\\': "/", '`': "'", '{': "(", '}': ")", '~': "-",
	'–': "-", '—': "-", '‘': "'", '’': "'", '‚': ",", '“': "\"", '”': "\"",
	'„': "\"", '…': "...", '•': "*", '·': ".", '«': "<<", '»': ">>",
	'×': "x", '÷': "/", '°': "o", 'ß': "ss", 'æ': "ae", 'Æ': "AE", 'ø': "o", 'Ø': "O",
}

// Encode is a rune in the lower/upper case set (nil if it has none, but
// reversed blocks: see Writer).
func Encode(r rune) []byte {
	switch {
	case r >= 'a' && r <= 'z':
		return []byte{byte(r - 0x20)}
	case r >= 'A' && r <= 'Z':
		return []byte{byte(r + 0x80)}
	}
	if b, ok := graphics[r]; ok {
		return []byte{b}
	}
	if r >= 0x20 && r <= 0x5D && r != '\\' {
		return []byte{byte(r)}
	}
	if s, ok := like[r]; ok {
		var out []byte
		for _, c := range s {
			out = append(out, Encode(c)...)
		}
		return out
	}
	if r == '^' {
		return []byte{0x5E}
	}
	// a letter with an accent: the letter
	if d := norm.NFD.String(string(r)); d != string(r) {
		if b, _ := utf8.DecodeRuneInString(d); b < 0x80 && b != r {
			return Encode(b)
		}
	}
	return nil
}

// Decode is a key a C64 sent: '\r' for RETURN, '\b' for DEL, letters as
// the lower/upper case set has them; utf8.RuneError for what is no
// character (cursor keys, colors, function keys).
func Decode(b byte) rune {
	switch {
	case b == Return:
		return '\r'
	case b == 0x0A:
		return '\n'
	case b == Del:
		return '\b'
	case b >= 0x41 && b <= 0x5A:
		return rune(b) + 0x20
	case b >= 0x61 && b <= 0x7A, b >= 0xC1 && b <= 0xDA:
		return rune(b&0x1F) + 'A' - 1
	case b == 0x5C:
		return '£'
	case b == 0x5E:
		return '^'
	case b == 0x5F, b == 0xA4:
		return '_'
	case b >= 0x20 && b <= 0x5D:
		return rune(b)
	case b == 0xA0:
		return ' '
	}
	return utf8.RuneError
}

// Telltale reports a byte only a Commodore's keyboard sends: DEL, the
// cursor keys (but down, which is XON), the function keys, CLR, and
// shifted letters (a UTF-8 letter's first byte looks like one: see
// whether a continuation byte follows).  HOME is XOFF, and not one.
func Telltale(b byte) bool {
	switch {
	case b == Del, b == Up, b == Right, b == Left, b == Clear:
		return true
	case b >= 0x85 && b <= 0x8C:
		return true
	case b >= 0xC1 && b <= 0xDA:
		return true
	}
	return false
}

// Writer translates the BBS's output to PETSCII.
type Writer struct {
	w     io.Writer
	Width int // columns: 40, or 80 (a C128's)

	col    int
	rev    bool
	bold   bool
	fg     int  // 0-7
	pendCR bool // a CR: RETURN if LF follows, else back to column 0

	state  int // 0 text, 1 after ESC, 2 in CSI
	params []byte
	part   []byte // a rune begun
	out    []byte
}

// NewWriter writes PETSCII to w, width columns wide.
func NewWriter(w io.Writer, width int) *Writer {
	return &Writer{w: w, Width: width, fg: 7}
}

// Start is what sets a C64 up: the lower/upper case set, kept, the screen
// cleared, the default color.
func Start() []byte { return []byte{Lowercase, LockCase, Clear, DefaultText} }

// Write translates b.
func (p *Writer) Write(b []byte) (int, error) {
	p.out = p.out[:0]
	for _, c := range b {
		switch {
		case p.state != 0:
			p.escape(c)
		case len(p.part) > 0 || c >= 0x80:
			p.part = append(p.part, c)
			if utf8.FullRune(p.part) || len(p.part) >= utf8.UTFMax {
				r, _ := utf8.DecodeRune(p.part)
				p.part = p.part[:0]
				p.rune(r)
			}
		default:
			p.rune(rune(c))
		}
	}
	if len(p.out) > 0 {
		if _, err := p.w.Write(p.out); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

// Settle does a CR still waiting to see whether LF follows: the cursor to
// column 0 (before the BBS waits for a key).
func (p *Writer) Settle() error {
	if !p.pendCR {
		return nil
	}
	p.out = p.out[:0]
	p.settleCR()
	_, err := p.w.Write(p.out)
	return err
}

// Raw writes PETSCII as it is (a .seq screen); the column is then not
// known, and taken as 0.
func (p *Writer) Raw(b []byte) error {
	if err := p.Settle(); err != nil {
		return err
	}
	p.col, p.rev = 0, false
	_, err := p.w.Write(b)
	return err
}

// settleCR does the pending CR: the cursor left to column 0, there being no
// carriage return on a C64 but RETURN, which also goes down a line.
func (p *Writer) settleCR() {
	p.pendCR = false
	p.lefts(p.col)
}

// lefts moves the cursor n columns left, no further than column 0.
func (p *Writer) lefts(n int) {
	for i := 0; i < n && p.col > 0; i++ {
		p.out = append(p.out, Left)
		p.col--
	}
}

// rune translates one rune: CR held to see whether LF follows (CR LF is
// RETURN), LF down, backspace left, tab to the next eighth column in spaces,
// ESC the start of a sequence, other controls dropped; a character as the
// set has it, a reversed block, or "?" for one it lacks.
func (p *Writer) rune(r rune) {
	if p.pendCR {
		if r == '\n' {
			p.pendCR = false
			p.out = append(p.out, Return)
			p.col = 0
			return
		}
		p.settleCR()
	}
	switch {
	case r == '\r':
		p.pendCR = true
	case r == '\n':
		p.out = append(p.out, Down)
	case r == '\b':
		if p.col > 0 {
			p.out = append(p.out, Left)
			p.col--
		}
	case r == '\t':
		for n := 8 - p.col%8; n > 0; n-- {
			p.put([]byte{' '})
		}
	case r == 0x1b:
		p.state, p.params = 1, p.params[:0]
	case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == utf8.RuneError:
	default:
		if b, ok := reversed[r]; ok {
			// the other way round to whatever is on
			if p.rev {
				p.out = append(p.out, ReverseOff, b, ReverseOn)
			} else {
				p.out = append(p.out, ReverseOn, b, ReverseOff)
			}
			p.advance(1)
			return
		}
		b := Encode(r)
		if b == nil {
			b = []byte{'?'}
		}
		p.put(b)
	}
}

// put writes a character's bytes, a column each.
func (p *Writer) put(b []byte) {
	p.out = append(p.out, b...)
	p.advance(len(b))
}

// advance moves the column on n, wrapping at the width as the screen does.
func (p *Writer) advance(n int) {
	p.col += n
	for p.col >= p.Width {
		p.col -= p.Width // the C64 wraps to the next line
	}
}

// escape takes a byte of an escape sequence: CSI's parameters (32 at most)
// and its final byte, to csi; any other sequence is dropped.
func (p *Writer) escape(c byte) {
	switch p.state {
	case 1:
		if c == '[' {
			p.state = 2
		} else {
			p.state = 0 // ESC 7, ESC 8 and the like: nothing here
		}
	case 2:
		switch {
		case c >= 0x20 && c <= 0x3f:
			if len(p.params) < 32 {
				p.params = append(p.params, c)
			}
		case c >= 0x40 && c <= 0x7e:
			p.state = 0
			p.csi(c, string(p.params))
		default:
			p.state = 0
		}
	}
}

// nums is a CSI's parameters as numbers, 0 for one empty or not a number
// (always at least one).
func nums(s string) []int {
	var out []int
	for _, f := range strings.Split(s, ";") {
		n, _ := strconv.Atoi(f)
		out = append(out, n)
	}
	return out
}

// csi does what a CSI sequence can on a C64: colors (SGR), clearing the
// screen (2J), cursor position and movement (counts held to 100, rows to 50,
// columns to the width), and erasing to the end of a line (by spaces);
// private sequences and the rest are dropped.
func (p *Writer) csi(final byte, params string) {
	if strings.ContainsAny(params, "?=<>!") {
		return
	}
	ns := nums(params)
	n := ns[0]
	if n < 1 {
		n = 1
	}
	n = min(n, 100)
	rep := func(b byte, n int) {
		for i := 0; i < n; i++ {
			p.out = append(p.out, b)
		}
	}
	switch final {
	case 'm':
		p.sgr(ns)
	case 'J':
		if ns[0] == 2 {
			p.out = append(p.out, Clear)
			p.col = 0
		}
	case 'H', 'f':
		row, col := 1, 1
		if len(ns) > 0 && ns[0] > 0 {
			row = min(ns[0], 50)
		}
		if len(ns) > 1 && ns[1] > 0 {
			col = min(ns[1], p.Width)
		}
		p.out = append(p.out, Home)
		rep(Down, row-1)
		rep(Right, col-1)
		p.col = col - 1
	case 'A':
		rep(Up, n)
	case 'B':
		rep(Down, n)
	case 'C':
		n = min(n, p.Width-1-p.col)
		rep(Right, n)
		p.col += n
	case 'D':
		p.lefts(n)
	case 'G':
		to := min(n, p.Width) - 1
		if to < p.col {
			p.lefts(p.col - to)
		} else {
			rep(Right, to-p.col)
			p.col = to
		}
	case 'K':
		if ns[0] == 0 {
			// no erase to the end of the line: spaces, and back
			k := p.Width - 1 - p.col
			rep(' ', k)
			rep(Left, k)
		}
	}
}

// sgr sets reverse, which follows each change, and the color: one of the
// eight ANSI colors, normal or bright by bold (90-97 bright too), whose
// code it always writes.
func (p *Writer) sgr(ns []int) {
	fg, bold := p.fg, p.bold
	for _, n := range ns {
		switch {
		case n == 0:
			fg, bold = 7, false
			if p.rev {
				p.out = append(p.out, ReverseOff)
				p.rev = false
			}
		case n == 1:
			bold = true
		case n == 22:
			bold = false
		case n == 7:
			if !p.rev {
				p.out = append(p.out, ReverseOn)
				p.rev = true
			}
		case n == 27:
			if p.rev {
				p.out = append(p.out, ReverseOff)
				p.rev = false
			}
		case n >= 30 && n <= 37:
			fg = n - 30
		case n == 39:
			fg = 7
		case n >= 90 && n <= 97:
			fg, bold = n-90, true
		}
	}
	p.fg, p.bold = fg, bold
	i := 0
	if bold {
		i = 1
	}
	p.out = append(p.out, colors[fg][i])
}
