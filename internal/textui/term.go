// Package textui is the BBS on a character terminal: a telnet client, or a
// caller's terminal program over a modem.  The server echoes and edits
// what the caller types; ANSI color and cursor movement are used only when
// the terminal has them.
package textui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ErrIdle: the caller typed nothing for too long.
var ErrIdle = errors.New("idle timeout")

// Conn is what a terminal runs on: a net.Conn, or a telnet.Conn.
type Conn interface {
	io.ReadWriter
	SetReadDeadline(time.Time) error
}

// Term is the caller's terminal.
type Term struct {
	c     Conn
	r     *bufio.Reader
	w     *bufio.Writer
	ANSI  bool // color and cursor movement
	UTF8  bool // UTF-8 both ways; otherwise 7-bit ASCII
	Sixel bool // sixel graphics
	// Clean8 is an 8-bit clean line (a modem, rlogin, telnet in binary
	// mode): what X/Y/ZMODEM need
	Clean8 bool
	// a character cell in pixels, if the terminal said (for sixel)
	CellW, CellH int
	Cols         int
	Rows         int
	Idle         time.Duration // for each key

	deadline time.Time // a hard limit for keys (the login's)
	lastCR   bool
}

// NewTerm makes a terminal on c, 80x24 plain ASCII until told otherwise.
func NewTerm(c Conn) *Term {
	return &Term{c: c, r: bufio.NewReaderSize(c, 1024), w: bufio.NewWriterSize(c, 4096),
		Cols: 80, Rows: 24, Idle: 10 * time.Minute}
}

// SetDeadline sets a limit for every key from now on (zero: none).
func (t *Term) SetDeadline(d time.Time) { t.deadline = d }

// Colors, for C.
const (
	Reset   = "0"
	Bold    = "1"
	Red     = "1;31"
	Green   = "1;32"
	Yellow  = "1;33"
	Blue    = "1;34"
	Magenta = "1;35"
	Cyan    = "1;36"
	White   = "1;37"
	Dim     = "0;37"
)

// C wraps s in a color, on an ANSI terminal.
func (t *Term) C(color, s string) string {
	if !t.ANSI {
		return s
	}
	return "\x1b[" + color + "m" + s + "\x1b[0m"
}

// Print writes s: \n as CR LF, and on an ASCII terminal anything else as ?.
// Our own escape sequences (from C) pass; text from users never has them
// (bbs.Clean took them out).
func (t *Term) Print(s string) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '\n':
			t.w.WriteString("\r\n")
		case r < 0x80:
			t.w.WriteByte(byte(r))
		case t.UTF8 && r != utf8.RuneError:
			t.w.WriteRune(r)
		default:
			t.w.WriteByte('?')
		}
	}
}

// Printf prints a formatted string.
func (t *Term) Printf(format string, a ...any) { t.Print(fmt.Sprintf(format, a...)) }

// Println prints s and a new line.
func (t *Term) Println(s string) { t.Print(s + "\n") }

// Flush sends what is printed.
func (t *Term) Flush() error { return t.w.Flush() }

// Clear clears the screen (a blank line on a plain terminal).
func (t *Term) Clear() {
	if t.ANSI {
		t.w.WriteString("\x1b[2J\x1b[H")
	} else {
		t.w.WriteString("\r\n")
	}
}

// readRune reads one key: a rune, with arrow keys and other escape
// sequences dropped and CR LF taken as one CR.
func (t *Term) readRune() (rune, error) {
	if err := t.Flush(); err != nil {
		return 0, err
	}
	for {
		d := time.Now().Add(t.Idle)
		if !t.deadline.IsZero() && t.deadline.Before(d) {
			d = t.deadline
		}
		t.c.SetReadDeadline(d)
		r, err := t.next()
		t.c.SetReadDeadline(time.Time{})
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return 0, ErrIdle
			}
			return 0, err
		}
		if r == '\n' && t.lastCR {
			t.lastCR = false
			continue
		}
		t.lastCR = r == '\r'
		if r == 0x1b {
			if err := t.skipEscape(); err != nil {
				return 0, err
			}
			continue
		}
		return r, nil
	}
}

func (t *Term) next() (rune, error) {
	if t.UTF8 {
		r, _, err := t.r.ReadRune()
		return r, err
	}
	b, err := t.r.ReadByte()
	if b >= 0x80 {
		return utf8.RuneError, err
	}
	return rune(b), err
}

// skipEscape drops the rest of an escape sequence (ESC [ ... final, or
// ESC O x), waiting briefly: a lone ESC is dropped by itself.
func (t *Term) skipEscape() error {
	t.c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	defer t.c.SetReadDeadline(time.Time{})
	b, err := t.r.ReadByte()
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return nil
		}
		return err
	}
	switch b {
	case '[':
		for i := 0; i < 16; i++ {
			b, err := t.r.ReadByte()
			if err != nil {
				return nil
			}
			if b >= 0x40 && b <= 0x7e {
				return nil
			}
		}
	case 'O':
		t.r.ReadByte()
	default:
		t.r.UnreadByte()
	}
	return nil
}

// Key reads one key, upper-cased; Enter is '\r'.
func (t *Term) Key() (rune, error) {
	for {
		r, err := t.readRune()
		if err != nil {
			return 0, err
		}
		if r == '\n' {
			r = '\r'
		}
		if r == '\r' || unicode.IsPrint(r) {
			return unicode.ToUpper(r), nil
		}
	}
}

// LineOpts are ReadLine's options.
type LineOpts struct {
	Max     int    // runes, at most
	Mask    bool   // echo * (passwords)
	Initial string // already typed
	Wrap    bool   // at Max, a word being typed goes to the next line (Carry)
}

// Line is a line read: the text, and the word that wrapped to the next.
type Line struct {
	Text  string
	Carry string
}

// ReadLine reads a line, echoing and editing it: backspace or delete
// erase, ^U erases the line, ^W the last word.
func (t *Term) ReadLine(o LineOpts) (Line, error) {
	if o.Max <= 0 {
		o.Max = 79
	}
	buf := []rune(o.Initial)
	if len(buf) > o.Max {
		buf = buf[:o.Max]
	}
	t.Print(string(buf))
	echo := func(r rune) {
		if o.Mask {
			t.Print("*")
		} else {
			t.Print(string(r))
		}
	}
	erase := func(n int) {
		for i := 0; i < n; i++ {
			t.w.WriteString("\b \b")
		}
	}
	for {
		r, err := t.readRune()
		if err != nil {
			return Line{}, err
		}
		switch {
		case r == '\r' || r == '\n':
			t.Print("\n")
			return Line{Text: string(buf)}, nil
		case r == 8 || r == 0x7f:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				erase(1)
			}
		case r == 0x15: // ^U
			erase(len(buf))
			buf = buf[:0]
		case r == 0x17: // ^W
			n := len(buf)
			for n > 0 && buf[n-1] == ' ' {
				n--
			}
			for n > 0 && buf[n-1] != ' ' {
				n--
			}
			erase(len(buf) - n)
			buf = buf[:n]
		case r == '\t':
			if len(buf) < o.Max {
				buf = append(buf, ' ')
				echo(' ')
			}
		case r == utf8.RuneError || !unicode.IsPrint(r):
			// nothing
		case len(buf) < o.Max:
			buf = append(buf, r)
			echo(r)
		case o.Wrap:
			// the line is full: the word being typed goes on the next
			if r == ' ' {
				t.Print("\n")
				return Line{Text: string(buf)}, nil
			}
			cut := len(buf)
			for cut > 0 && buf[cut-1] != ' ' {
				cut--
			}
			if cut == 0 || len(buf)-cut > o.Max/2 {
				cut = len(buf) // one long word: break it
			}
			carry := string(buf[cut:]) + string(r)
			erase(len(buf) - cut)
			t.Print("\n")
			return Line{Text: strings.TrimRight(string(buf[:cut]), " "), Carry: carry}, nil
		}
	}
}

// Input prints a prompt and reads a line of at most max runes.
func (t *Term) Input(prompt string, max int) (string, error) {
	t.Print(prompt)
	l, err := t.ReadLine(LineOpts{Max: max})
	return strings.TrimSpace(l.Text), err
}

// Password prints a prompt and reads a masked line.
func (t *Term) Password(prompt string) (string, error) {
	t.Print(prompt)
	l, err := t.ReadLine(LineOpts{Max: 72, Mask: true})
	return l.Text, err
}

// YesNo asks a question; Enter is def.
func (t *Term) YesNo(prompt string, def bool) (bool, error) {
	hint := " [y/N] "
	if def {
		hint = " [Y/n] "
	}
	t.Print(prompt + hint)
	for {
		k, err := t.Key()
		if err != nil {
			return false, err
		}
		switch k {
		case 'Y':
			t.Println("Yes")
			return true, nil
		case 'N':
			t.Println("No")
			return false, nil
		case '\r':
			if def {
				t.Println("Yes")
			} else {
				t.Println("No")
			}
			return def, nil
		}
	}
}

// Pause waits for a key.
func (t *Term) Pause() error {
	t.Print(t.C(Dim, "[Press a key]"))
	_, err := t.Key()
	t.Print("\r" + strings.Repeat(" ", 13) + "\r")
	return err
}

// Page shows lines a screen at a time; false if the caller quit.
func (t *Term) Page(lines []string) (bool, error) {
	per := t.Rows - 2
	if per < 5 {
		per = 5
	}
	for i, l := range lines {
		if i > 0 && i%per == 0 {
			t.Print(t.C(Dim, "-- More -- [Enter] continue, [Q]uit "))
			k, err := t.Key()
			t.Print("\r" + strings.Repeat(" ", 37) + "\r")
			if err != nil {
				return false, err
			}
			if k == 'Q' {
				return false, nil
			}
		}
		t.Println(l)
	}
	return true, nil
}

var cprPattern = regexp.MustCompile(`\x1b\[(\d+);(\d+)R`)

// DetectANSI asks the terminal where its cursor is, after moving it as far
// down and right as it goes: an ANSI terminal answers, and the answer is
// its size.  A terminal that does not answer within timeout is not ANSI.
func (t *Term) DetectANSI(timeout time.Duration) (bool, error) {
	t.w.WriteString("\x1b7\x1b[999;999H\x1b[6n\x1b8")
	if err := t.Flush(); err != nil {
		return false, err
	}
	var got []byte
	end := time.Now().Add(timeout)
	for time.Now().Before(end) && len(got) < 64 {
		t.c.SetReadDeadline(end)
		b, err := t.r.ReadByte()
		t.c.SetReadDeadline(time.Time{})
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			return false, err
		}
		got = append(got, b)
		if m := cprPattern.FindSubmatch(got); m != nil {
			rows, _ := strconv.Atoi(string(m[1]))
			cols, _ := strconv.Atoi(string(m[2]))
			if rows >= 10 && rows <= 200 {
				t.Rows = rows
			}
			if cols >= 40 && cols <= 300 {
				t.Cols = cols
			}
			return true, nil
		}
	}
	// a plain terminal shows the request as text: wipe it
	t.w.WriteString("\r" + strings.Repeat(" ", 20) + "\r")
	return false, nil
}

var (
	daPattern   = regexp.MustCompile(`\x1b\[\?([0-9;]*)c`)
	cellPattern = regexp.MustCompile(`\x1b\[6;(\d+);(\d+)t`)
)

// DetectSixel asks the terminal for its device attributes (DA1), and its
// character cell's size in pixels (xterm's CSI 16 t, which not every
// terminal answers): a terminal whose attributes include 4 has sixel
// graphics.  Only for ANSI terminals; one that does not answer within
// timeout has none.
func (t *Term) DetectSixel(timeout time.Duration) (bool, error) {
	t.w.WriteString("\x1b[16t\x1b[c")
	if err := t.Flush(); err != nil {
		return false, err
	}
	var got []byte
	end := time.Now().Add(timeout)
	for time.Now().Before(end) && len(got) < 256 {
		t.c.SetReadDeadline(end)
		b, err := t.r.ReadByte()
		t.c.SetReadDeadline(time.Time{})
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			return false, err
		}
		got = append(got, b)
		if m := daPattern.FindSubmatch(got); m != nil {
			if c := cellPattern.FindSubmatch(got); c != nil {
				h, _ := strconv.Atoi(string(c[1]))
				w, _ := strconv.Atoi(string(c[2]))
				if w >= 4 && w <= 64 && h >= 4 && h <= 128 {
					t.CellW, t.CellH = w, h
				}
			}
			for _, a := range strings.Split(string(m[1]), ";") {
				if a == "4" {
					return true, nil
				}
			}
			return false, nil
		}
	}
	return false, nil
}

// Picture shows a sixel picture (sixel.Encode's) on its own lines.
func (t *Term) Picture(data []byte) {
	if !t.Sixel || len(data) == 0 {
		return
	}
	t.w.Write(data)
	t.w.WriteString("\r\n")
}

// port is the terminal as a line for file transfers: the same buffered
// reader (what was typed ahead is not lost), writes straight through.
type port struct{ t *Term }

func (p port) Read(b []byte) (int, error)        { return p.t.r.Read(b) }
func (p port) Write(b []byte) (int, error)       { return p.t.c.Write(b) }
func (p port) SetReadDeadline(d time.Time) error { return p.t.c.SetReadDeadline(d) }

// Port is the line for a file transfer; what was printed goes first.
func (t *Term) Port() (port, error) {
	if err := t.Flush(); err != nil {
		return port{}, err
	}
	return port{t}, nil
}

// Settle waits for the line to be quiet for d (the end of a transfer, a
// terminal program's leftovers) and drops what came.
func (t *Term) Settle(d time.Duration) {
	for i := 0; i < 1000; i++ {
		t.c.SetReadDeadline(time.Now().Add(d))
		_, err := t.r.ReadByte()
		if err != nil {
			break
		}
		for t.r.Buffered() > 0 {
			t.r.ReadByte()
		}
	}
	t.c.SetReadDeadline(time.Time{})
	t.lastCR = false
}
