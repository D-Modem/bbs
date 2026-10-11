// Package screens is the sysop's own screens: display files in a
// directory, chosen by name and by the caller's terminal, with @-codes
// filled in.
//
//	NAME.ans    ANSI art, in CP437 (a SAUCE record after ^Z is dropped)
//	NAME.asc    plain text (UTF-8 or ASCII), paged
//	NAME.txt    the same
//	NAME.3270   text for 3270s, with {color} tags
//
// An ANSI terminal gets .ans, else .asc, else .txt; a plain one .asc or
// .txt; a 3270 .3270, .asc or .txt.
package screens

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// Kind is the caller's terminal.
type Kind int

// Kinds of terminal.
const (
	Plain Kind = iota
	ANSI
	TN3270
)

// Format is a display file's.
type Format int

// Formats of display files.
const (
	Text  Format = iota // .asc, .txt
	Art                 // .ans
	Color               // .3270
)

// MaxSize is the largest display file read.
const MaxSize = 256 << 10

// Set is a directory of screens ("": none).
type Set struct{ Dir string }

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// ValidName checks a screen's name: lower case letters, digits, _ and -.
func ValidName(name string) bool { return validName.MatchString(name) }

// Screen is a display file read.
type Screen struct {
	Name   string
	Format Format
	Text   string // as UTF-8 (an .ans decoded from CP437)
}

// Find reads the screen of that name for the terminal; false if there is
// none (or no directory).
func (s *Set) Find(name string, kind Kind) (*Screen, bool) {
	if s == nil || s.Dir == "" || !ValidName(name) {
		return nil, false
	}
	var tries []string
	switch kind {
	case ANSI:
		tries = []string{".ans", ".asc", ".txt"}
	case Plain:
		tries = []string{".asc", ".txt"}
	case TN3270:
		tries = []string{".3270", ".asc", ".txt"}
	}
	for _, ext := range tries {
		b, err := readFile(filepath.Join(s.Dir, name+ext))
		if err != nil {
			continue
		}
		sc := &Screen{Name: name}
		switch ext {
		case ".ans":
			sc.Format = Art
			sc.Text = decodeCP437(stripSAUCE(b))
		case ".3270":
			sc.Format = Color
			sc.Text = strings.ToValidUTF8(string(b), "?")
		default:
			sc.Text = strings.ToValidUTF8(string(b), "?")
		}
		sc.Text = strings.ReplaceAll(sc.Text, "\r\n", "\n")
		return sc, true
	}
	return nil, false
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return nil, errors.New("not a file")
	}
	if fi.Size() > MaxSize {
		return nil, errors.New("too big")
	}
	b := make([]byte, fi.Size())
	n, err := f.Read(b)
	return b[:n], err
}

// stripSAUCE drops what follows ^Z: a SAUCE record, comments.
func stripSAUCE(b []byte) []byte {
	for i, c := range b {
		if c == 0x1a {
			return b[:i]
		}
	}
	return b
}

func decodeCP437(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c < 0x80 {
			sb.WriteByte(c)
		} else {
			sb.WriteRune(charmap.CodePage437.DecodeByte(c))
		}
	}
	return sb.String()
}

// EncodeCP437 turns text back into CP437 bytes for a CP437 terminal
// ('?' for what CP437 lacks).
func EncodeCP437(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 0x80 {
			out = append(out, byte(r))
		} else if c, ok := charmap.CodePage437.EncodeRune(r); ok {
			out = append(out, c)
		} else {
			out = append(out, '?')
		}
	}
	return out
}

// Vars gives the value of an @-code (USER, NODE, ...), false if it is not
// one.
type Vars func(code string) (string, bool)

// Piece is part of a screen: text, or a clear or a pause.
type Piece struct {
	Text  string
	Clear bool
	Pause bool
}

// @CODE@ or @CODE:WIDTH@ (negative: right-aligned)
var code = regexp.MustCompile(`@([A-Z][A-Z0-9]*)(?::(-?[0-9]{1,3}))?@`)

// Expand fills in a text's @-codes, and splits it at @CLS@ and @PAUSE@.
// A code that is not one stays as it was.
func Expand(text string, vars Vars) []Piece {
	var out []Piece
	var cur strings.Builder
	last := 0
	for _, m := range code.FindAllStringSubmatchIndex(text, -1) {
		cur.WriteString(text[last:m[0]])
		last = m[1]
		name := text[m[2]:m[3]]
		switch name {
		case "CLS", "PAUSE":
			if cur.Len() > 0 {
				out = append(out, Piece{Text: cur.String()})
				cur.Reset()
			}
			out = append(out, Piece{Clear: name == "CLS", Pause: name == "PAUSE"})
			continue
		}
		v, ok := "", false
		if vars != nil {
			v, ok = vars(name)
		}
		if !ok {
			cur.WriteString(text[m[0]:m[1]])
			continue
		}
		if m[4] >= 0 {
			w, _ := strconv.Atoi(text[m[4]:m[5]])
			v = pad(v, w)
		}
		cur.WriteString(v)
	}
	cur.WriteString(text[last:])
	if cur.Len() > 0 {
		out = append(out, Piece{Text: cur.String()})
	}
	return out
}

// String is Expand's text alone (clears and pauses dropped).
func String(text string, vars Vars) string {
	var b strings.Builder
	for _, p := range Expand(text, vars) {
		b.WriteString(p.Text)
	}
	return b.String()
}

// pad fits v to |w| runes: cut, or padded with spaces (on the left if w
// is negative).
func pad(v string, w int) string {
	right := w < 0
	if right {
		w = -w
	}
	if w == 0 {
		return v
	}
	n := utf8.RuneCountInString(v)
	if n > w {
		return string([]rune(v)[:w])
	}
	if right {
		return strings.Repeat(" ", w-n) + v
	}
	return v + strings.Repeat(" ", w-n)
}

// Span is a run of a .3270 line in one color.
type Span struct {
	Text   string
	Color  string // blue red pink green turquoise yellow white ("": default)
	Bright bool
}

var tag = regexp.MustCompile(`\{(blue|red|pink|green|turquoise|yellow|white|default|bright|normal)\}`)

// Spans splits a .3270 line at its {color} tags: {blue} ... {white},
// {default}, and {bright} or {normal} intensity.
func Spans(line string) []Span {
	var out []Span
	color, bright := "", false
	last := 0
	for _, m := range tag.FindAllStringSubmatchIndex(line, -1) {
		if m[0] > last {
			out = append(out, Span{line[last:m[0]], color, bright})
		}
		last = m[1]
		switch t := line[m[2]:m[3]]; t {
		case "bright":
			bright = true
		case "normal":
			bright = false
		case "default":
			color = ""
		default:
			color = t
		}
	}
	if last < len(line) {
		out = append(out, Span{line[last:], color, bright})
	}
	return out
}

// StripTags is a .3270 line without its tags.
func StripTags(line string) string { return tag.ReplaceAllString(line, "") }
