// Package screens is the sysop's own screens: display files in a
// directory, chosen by name and by the caller's terminal, with @-codes
// filled in.
//
//	NAME.ans    ANSI art, in CP437 (a SAUCE record after ^Z is dropped)
//	NAME.asc    plain text (UTF-8 or ASCII), paged
//	NAME.txt    the same
//	NAME.3270   text for 3270s, with {color} tags
//	NAME.rip    RIPscrip (graphics, and ANSI), in CP437, for RIP terminals
//	NAME.seq    PETSCII, sent to a Commodore as it is (no @-codes)
//
// An ANSI terminal gets .ans, else .asc, else .txt; a RIP terminal .rip
// before those; a plain one .asc or .txt; a 3270 .3270, .asc or .txt; a
// Commodore .seq, .asc or .txt.
package screens

import (
	"errors"
	"io"
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
	RIP
	PETSCII
)

// Format is a display file's.
type Format int

// Formats of display files.
const (
	Text  Format = iota // .asc, .txt
	Art                 // .ans
	Color               // .3270
	Raw                 // .seq: Bytes, as they are
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
	Bytes  []byte // Raw's
	Ext    string // the file's: .ans, .rip, ...
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
	case RIP:
		tries = []string{".rip", ".ans", ".asc", ".txt"}
	case PETSCII:
		tries = []string{".seq", ".asc", ".txt"}
	}
	for _, ext := range tries {
		b, err := readFile(filepath.Join(s.Dir, name+ext))
		if err != nil {
			continue
		}
		sc := &Screen{Name: name, Ext: ext}
		switch ext {
		case ".ans", ".rip":
			sc.Format = Art
			sc.Text = decodeCP437(stripSAUCE(b))
		case ".seq":
			sc.Format, sc.Bytes = Raw, b
			return sc, true
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

// readFile is the file at path, read whole, if it is a regular file of MaxSize
// or less; an error for anything else (a directory, a device, one too big).
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
	// all of it (one Read may stop short), and no more than MaxSize should
	// it have grown since
	b, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxSize {
		return nil, errors.New("too big")
	}
	return b, nil
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

// decodeCP437 is CP437 as UTF-8: the bytes below 0x80 as ASCII (control codes,
// not CP437's pictures for them), the rest as CP437's characters.
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

// Piece is part of a screen: text, or a clear or a pause, or (from
// ExpandTags) a tag.
type Piece struct {
	Text  string
	Clear bool
	Pause bool
	Tag   string // a text terminal's tag (Tags), without its braces
}

// @CODE@ or @CODE:WIDTH@ (negative: right-aligned)
var code = regexp.MustCompile(`@([A-Z][A-Z0-9]*)(?::(-?[0-9]{1,3}))?@`)

// Expand fills in a text's @-codes, and splits it at @CLS@ and @PAUSE@.
// A code that is not one stays as it was.
func Expand(text string, vars Vars) []Piece { return expand(text, vars, false) }

// ExpandTags is Expand that splits the text at its tags (Tags) too: the
// sysop's text's, never a value's (a caller named "{red}" stays so).
// {clear} and {pause} are pieces as @CLS@ and @PAUSE@ are.
func ExpandTags(text string, vars Vars) []Piece { return expand(text, vars, true) }

// expand is Expand, and with tags ExpandTags: the @-codes filled in (padded to
// their widths), and @CLS@, @PAUSE@ and the sysop's text's tags made pieces of
// their own; the text between is one piece.
func expand(text string, vars Vars, tags bool) []Piece {
	var out []Piece
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, Piece{Text: cur.String()})
			cur.Reset()
		}
	}
	// literal is the sysop's own text, split at its tags
	literal := func(s string) {
		if !tags {
			cur.WriteString(s)
			return
		}
		last := 0
		for _, m := range termTag.FindAllStringSubmatchIndex(s, -1) {
			cur.WriteString(s[last:m[0]])
			last = m[1]
			var name string
			if m[2] >= 0 { // {tag}
				name = s[m[2]:m[3]]
			} else { // its mark
				name = s[m[4]:m[5]]
			}
			flush()
			switch name {
			case "clear", "cls":
				out = append(out, Piece{Clear: true})
			case "pause":
				out = append(out, Piece{Pause: true})
			default:
				out = append(out, Piece{Tag: name})
			}
		}
		cur.WriteString(s[last:])
	}
	last := 0
	for _, m := range code.FindAllStringSubmatchIndex(text, -1) {
		literal(text[last:m[0]])
		last = m[1]
		name := text[m[2]:m[3]]
		switch name {
		case "CLS", "PAUSE":
			flush()
			out = append(out, Piece{Clear: name == "CLS", Pause: name == "PAUSE"})
			continue
		}
		v, ok := "", false
		if vars != nil {
			v, ok = vars(name)
		}
		if !ok {
			literal(text[m[0]:m[1]])
			continue
		}
		if m[4] >= 0 {
			w, _ := strconv.Atoi(text[m[4]:m[5]])
			v = pad(v, w)
		}
		cur.WriteString(v)
	}
	literal(text[last:])
	flush()
	return out
}

// String is the text with its @-codes filled in, and its tags (and
// clears and pauses) dropped: for where they cannot be shown.
func String(text string, vars Vars) string {
	var b strings.Builder
	for _, p := range ExpandTags(text, vars) {
		b.WriteString(p.Text)
	}
	return b.String()
}

// Marked is String with the tags kept, as marks (Mark) a text terminal
// turns into its escape sequences when it prints them (and anything else
// drops, with String).  A value cannot make one: the marks are control
// characters, which the values never have.
func Marked(text string, vars Vars) string {
	var b strings.Builder
	for _, p := range ExpandTags(text, vars) {
		switch {
		case p.Clear:
			b.WriteString(Mark("clear"))
		case p.Tag != "":
			b.WriteString(Mark(p.Tag))
		default:
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// Mark is a tag (Tags) as Marked writes it.
func Mark(tag string) string { return "\x1e" + tag + "\x1f" }

// Unmark is a marked text (Marked) with its marks dropped: for where it
// cannot be shown.
func Unmark(s string) string { return Marks.ReplaceAllString(s, "") }

// Fit fits a marked text to width columns as bbs.Fit does a plain one:
// its marks take none, the text past width is cut (and its marks with
// it), and when it has any it ends with a {reset} before its padding, so
// its colors stay in it.
func Fit(s string, width int) string {
	var b strings.Builder
	n, marked := 0, false
	last := 0
	for _, m := range append(Marks.FindAllStringIndex(s, -1), []int{len(s), len(s)}) {
		for _, r := range s[last:m[0]] {
			if n == width {
				break
			}
			b.WriteRune(r)
			n++
		}
		if m[0] < m[1] && n < width {
			b.WriteString(s[m[0]:m[1]])
			marked = true
		}
		last = m[1]
	}
	if marked {
		b.WriteString(Mark("reset"))
	}
	return b.String() + strings.Repeat(" ", width-n)
}

// Marks finds the marks in a text: [start, end, name start, name end].
var Marks = regexp.MustCompile("\x1e([a-z ]+)\x1f")

// Colors are the tags' colors, in ANSI's order; pink and turquoise (the
// 3270's names) are magenta and cyan.
var Colors = []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}

// Tags are what a text screen, a [strings] value or a menu's prompt may
// say for a text terminal, in braces:
//
//	{clear} {cls}              clear the screen
//	{pause}                    wait for a key
//	{home} {eol}               the cursor to the top left; clear to the end of the line
//	{black} {red} {green} {yellow} {blue} {magenta} {cyan} {white}
//	{pink} {turquoise}         (magenta and cyan, as on a 3270)
//	{on blue} ...              a background
//	{bright} {bold} {normal}   intensity
//	{blink} {underline} {reverse}
//	{default} {reset}          everything back to plain
//
// A previously marked text's marks (Mark) count as its tags too.
var Tags = []string{"clear", "cls", "pause", "home", "eol", "bright", "bold", "normal",
	"blink", "underline", "reverse", "default", "reset"}

var termTag = func() *regexp.Regexp {
	colors := strings.Join(append(append([]string{}, Colors...), "pink", "turquoise"), "|")
	names := strings.Join(Tags, "|") + "|" + colors + "|on (?:" + colors + ")"
	return regexp.MustCompile(`\{(` + names + `)\}|\x1e(` + names + `)\x1f`)
}()

// ColorNumber is a color tag's ANSI number (0-7), with pink and turquoise as
// magenta and cyan; false if it is not a color.
func ColorNumber(name string) (int, bool) {
	switch name {
	case "pink":
		name = "magenta"
	case "turquoise":
		name = "cyan"
	}
	for i, c := range Colors {
		if c == name {
			return i, true
		}
	}
	return 0, false
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

// a 3270's colors for the tags' (black is the default: a 3270's screen is
// black already)
var colors3270 = []string{"", "red", "green", "yellow", "blue", "pink", "turquoise", "white"}

// Spans splits a line, from ExpandTags with its tags as marks (Mark), at
// them: {blue} ... {white} (with magenta and cyan as pink and turquoise),
// {default}, {bright} or {normal} intensity, and {reset} for both; the
// tags a 3270 has nothing for are dropped.
func Spans(line string) []Span {
	var out []Span
	color, bright := "", false
	last := 0
	for _, m := range Marks.FindAllStringSubmatchIndex(line, -1) {
		if m[0] > last {
			out = append(out, Span{line[last:m[0]], color, bright})
		}
		last = m[1]
		switch t := line[m[2]:m[3]]; t {
		case "bright", "bold":
			bright = true
		case "normal":
			bright = false
		case "default":
			color = ""
		case "reset":
			color, bright = "", false
		default:
			if n, ok := ColorNumber(t); ok {
				color = colors3270[n]
			}
		}
	}
	if last < len(line) {
		out = append(out, Span{line[last:], color, bright})
	}
	return out
}

// StripTags is a line without its tags, as braces or marks.
func StripTags(line string) string { return termTag.ReplaceAllString(line, "") }
