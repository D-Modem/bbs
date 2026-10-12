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

	"github.com/Neo-Desktop/bbs-sec/internal/petscii"
	"github.com/Neo-Desktop/bbs-sec/internal/rip"
	"github.com/Neo-Desktop/bbs-sec/internal/screens"
	"github.com/Neo-Desktop/bbs-sec/internal/vt"
)

// ErrIdle: the caller typed nothing for too long.
var ErrIdle = errors.New("idle timeout")

// ErrPETSCII: while WatchPETSCII, a key only a Commodore sends.
var ErrPETSCII = errors.New("a Commodore's key")

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
	// PETSCII: a Commodore 64 or 128 (SetPETSCII); colors, but not ANSI
	PETSCII bool
	// RIP: an ANSI terminal with RIPscrip graphics (RIPterm, SyncTERM)
	RIP bool
	// DEC: a VT without UTF-8 (SetDEC): DEC Special Graphics, Latin-1
	DEC bool
	// VT52: a VT52 (SetVT52): its escapes, no attributes
	VT52 bool
	// Mono: a terminal without color: bold for colors
	Mono bool
	// Ident is what the terminal said it is (vt.Info's Name), if anything
	Ident string
	// WatchPETSCII: the terminal may be a Commodore's: a key only one
	// sends ends a read with ErrPETSCII
	WatchPETSCII bool
	// Upper: ASCII letters printed in capitals (which a Commodore in its
	// power-on set and anyone else both read)
	Upper bool
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

	// Notify, if set, gives lines to show the caller while a key is
	// awaited (chat, messages from other nodes): they are printed above
	// the line being typed, which is then drawn again
	Notify func() []string
	cur    []rune // what has been printed of the line the cursor is on

	// PausePrompt is Pause's ("": "[Press a key]")
	PausePrompt string

	xl       translator // under w: PETSCII, DEC, VT52
	latin1   bool       // a DEC terminal's 8-bit characters are Latin-1
	sinceKey int        // lines printed since a key was read
	ripScene bool       // a RIP scene is on the screen
}

// translator turns what the BBS writes (UTF-8, ANSI) into a terminal's.
type translator interface {
	io.Writer
	Settle() error      // before waiting for a key
	Raw(b []byte) error // bytes as they are
}

func (t *Term) setTranslator(x translator) {
	t.Flush()
	t.xl = x
	if x == nil {
		t.w = bufio.NewWriterSize(t.c, 4096)
	} else {
		t.w = bufio.NewWriterSize(x, 4096)
	}
}

// SetDEC makes the terminal a VT without UTF-8: line drawing in DEC
// Special Graphics, Latin-1 if latin1 (a VT220's or later, on an 8-bit
// line).
func (t *Term) SetDEC(latin1 bool) {
	t.DEC, t.UTF8, t.latin1 = true, false, latin1
	t.setTranslator(vt.NewDECWriter(t.c, latin1))
}

// SetVT52 makes the terminal a VT52: no ANSI, its own escapes.
func (t *Term) SetVT52() {
	t.VT52, t.ANSI, t.UTF8, t.Sixel, t.RIP, t.Mono = true, false, false, false, false, true
	t.Cols, t.Rows = 80, 24
	t.setTranslator(vt.NewVT52Writer(t.c))
}

// SetPETSCII makes the terminal a Commodore's (width 40, or a C128's 80)
// or no longer one.
func (t *Term) SetPETSCII(on bool, width int) {
	if !on {
		t.PETSCII = false
		t.setTranslator(nil)
		return
	}
	t.PETSCII, t.ANSI, t.UTF8, t.Sixel, t.RIP = true, false, false, false, false
	t.Cols, t.Rows = width, 25
	pet := petscii.NewWriter(t.c, width)
	t.setTranslator(pet)
	pet.Raw(petscii.Start())
	t.cur = t.cur[:0]
}

// Raw writes bytes as they are, past any translation (a .seq screen to a
// Commodore).
func (t *Term) Raw(b []byte) error {
	if err := t.Flush(); err != nil {
		return err
	}
	t.cur = t.cur[:0]
	if t.xl != nil {
		return t.xl.Raw(b)
	}
	_, err := t.c.Write(b)
	return err
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
	switch {
	case t.VT52 || !t.ANSI && !t.PETSCII:
		return s
	case t.Mono:
		// bright colors bold, the rest plain
		if strings.HasPrefix(color, "1") {
			return "\x1b[1m" + s + "\x1b[0m"
		}
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
		switch r {
		case '\n':
			t.cur = t.cur[:0]
			t.sinceKey++
		case '\b':
			if len(t.cur) > 0 {
				t.cur = t.cur[:len(t.cur)-1]
			}
		default:
			t.cur = append(t.cur, r)
		}
		if t.Upper && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		switch {
		case r == '\n':
			t.w.WriteString("\r\n")
		case r < 0x80:
			t.w.WriteByte(byte(r))
		case (t.UTF8 || t.xl != nil) && r != utf8.RuneError:
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
func (t *Term) Flush() error {
	if err := t.w.Flush(); err != nil {
		return err
	}
	if t.xl != nil {
		return t.xl.Settle()
	}
	return nil
}

// Clear clears the screen (a blank line on a plain terminal).
func (t *Term) Clear() {
	t.cur = t.cur[:0]
	if t.ANSI || t.PETSCII || t.VT52 {
		t.w.WriteString("\x1b[2J\x1b[H")
	} else {
		t.w.WriteString("\r\n")
	}
}

// readRune reads one key: a rune, with arrow keys and other escape
// sequences dropped and CR LF taken as one CR.  While it waits it shows
// what Notify has.
func (t *Term) readRune() (rune, error) {
	if err := t.Flush(); err != nil {
		return 0, err
	}
	start := time.Now()
	for {
		limit := start.Add(t.Idle)
		if !t.deadline.IsZero() && t.deadline.Before(limit) {
			limit = t.deadline
		}
		if err := t.await(limit); err != nil {
			return 0, err
		}
		t.c.SetReadDeadline(limit)
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
		t.sinceKey = 0
		return r, nil
	}
}

// await waits until a key is there (or limit), showing Notify's lines as
// they come.
func (t *Term) await(limit time.Time) error {
	for t.r.Buffered() == 0 {
		t.notices()
		if err := t.Flush(); err != nil {
			return err
		}
		now := time.Now()
		if !now.Before(limit) {
			return ErrIdle
		}
		slice := limit
		if t.Notify != nil && now.Add(500*time.Millisecond).Before(limit) {
			slice = now.Add(500 * time.Millisecond)
		}
		t.c.SetReadDeadline(slice)
		_, err := t.r.Peek(1)
		t.c.SetReadDeadline(time.Time{})
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			return err
		}
	}
	return nil
}

// notices prints Notify's lines above the line being typed, and draws
// that again.
func (t *Term) notices() {
	if t.Notify == nil {
		return
	}
	ls := t.Notify()
	if len(ls) == 0 {
		return
	}
	cur := string(t.cur)
	if t.ANSI {
		t.w.WriteString("\r\x1b[K")
	} else if cur != "" {
		t.w.WriteString("\r\n")
	}
	t.cur = t.cur[:0]
	for _, l := range ls {
		t.Println(l)
	}
	t.Print(cur)
}

func (t *Term) next() (rune, error) {
	if t.PETSCII {
		b, err := t.r.ReadByte()
		if err != nil {
			return 0, err
		}
		return petscii.Decode(b), nil
	}
	if t.WatchPETSCII && !t.UTF8 {
		b, err := t.r.ReadByte()
		if err != nil {
			return 0, err
		}
		switch {
		case !petscii.Telltale(b):
		case b >= 0xC1 && b <= 0xDA && t.continuation():
			t.r.ReadByte() // a UTF-8 letter, not a shifted one
		default:
			return 0, ErrPETSCII
		}
		if b >= 0x80 {
			return utf8.RuneError, nil
		}
		return rune(b), nil
	}
	if t.UTF8 {
		r, _, err := t.r.ReadRune()
		return r, err
	}
	b, err := t.r.ReadByte()
	if b >= 0x80 {
		if t.latin1 && b >= 0xa0 {
			return rune(b), err
		}
		return utf8.RuneError, err
	}
	return rune(b), err
}

// continuation: the next byte, if one comes at once, continues a UTF-8
// character
func (t *Term) continuation() bool {
	if t.r.Buffered() == 0 {
		t.c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		defer t.c.SetReadDeadline(time.Time{})
	}
	b, err := t.r.Peek(1)
	return err == nil && b[0] >= 0x80 && b[0] <= 0xBF
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
	case 'A', 'B', 'C', 'D':
		if !t.VT52 {
			t.r.UnreadByte()
		} // a VT52's cursor keys
	case '?':
		if t.VT52 {
			t.r.ReadByte() // its keypad
		} else {
			t.r.UnreadByte()
		}
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
			t.Print("\b \b")
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
			t.sinceKey = 0
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
	p := t.PausePrompt
	if p == "" {
		p = "[Press a key]"
	}
	t.Print(t.C(Dim, p))
	_, err := t.Key()
	t.Print("\r" + strings.Repeat(" ", min(utf8.RuneCountInString(p), 79)) + "\r")
	return err
}

// Art writes ANSI art as it is (its escape sequences too): UTF-8 to a
// UTF-8 terminal, CP437 to any other; \n as CR LF.
func (t *Term) Art(s string) {
	s = strings.ReplaceAll(s, "\n", "\r\n")
	if t.UTF8 || t.xl != nil {
		t.w.WriteString(s)
	} else {
		t.w.Write(screens.EncodeCP437(s))
	}
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		t.cur = t.cur[:0]
	}
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

// RawKey reads one byte as it comes (no decoding), waiting up to d
// (ErrIdle after).
func (t *Term) RawKey(d time.Duration) (byte, error) {
	if err := t.Flush(); err != nil {
		return 0, err
	}
	t.c.SetReadDeadline(time.Now().Add(d))
	b, err := t.r.ReadByte()
	t.c.SetReadDeadline(time.Time{})
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return 0, ErrIdle
	}
	return b, err
}

// Identify asks an ANSI terminal what it is (vt.Query): its answers, and
// its character cell (for sixel) if it says.
func (t *Term) Identify(timeout time.Duration) (vt.Info, error) {
	t.w.WriteString(vt.Query)
	if err := t.Flush(); err != nil {
		return vt.Info{}, err
	}
	got, err := t.collect(timeout, 1024, vt.Done)
	if err != nil {
		return vt.Info{}, err
	}
	in := vt.Parse(got)
	if in.CellW > 0 {
		t.CellW, t.CellH = in.CellW, in.CellH
	}
	t.Ident = in.Name()
	return in, nil
}

// DetectVT52 asks a terminal that is not ANSI whether it is a VT52 (ESC
// Z); any other shows a Z, which is wiped.
func (t *Term) DetectVT52(timeout time.Duration) (bool, error) {
	t.w.WriteString(vt.QueryVT52)
	if err := t.Flush(); err != nil {
		return false, err
	}
	got, err := t.collect(timeout, 16, func(b []byte) bool { return vt.Parse(b).VT52 })
	if err != nil {
		return false, err
	}
	if vt.Parse(got).VT52 {
		t.Ident = "VT52"
		return true, nil
	}
	t.w.WriteString("\r \r")
	return false, nil
}

// collect reads what comes until done says so, max bytes, or timeout.
func (t *Term) collect(timeout time.Duration, max int, done func([]byte) bool) ([]byte, error) {
	var got []byte
	end := time.Now().Add(timeout)
	for time.Now().Before(end) && len(got) < max {
		t.c.SetReadDeadline(end)
		b, err := t.r.ReadByte()
		t.c.SetReadDeadline(time.Time{})
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			return nil, err
		}
		got = append(got, b)
		if done(got) {
			break
		}
	}
	return got, nil
}

// Narrow is a screen too narrow for the usual columns (a C64's 40).
func (t *Term) Narrow() bool { return t.Cols < 60 }

// DetectRIP asks an ANSI terminal whether it has RIPscrip, with the
// position report every ANSI terminal answers after it: a RIP terminal's
// answer comes first.
func (t *Term) DetectRIP(timeout time.Duration) (bool, error) {
	t.w.WriteString(rip.Query + "\x1b[6n")
	if err := t.Flush(); err != nil {
		return false, err
	}
	var got []byte
	end := time.Now().Add(timeout)
	for time.Now().Before(end) && len(got) < 128 {
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
		if cprPattern.Match(got) {
			break
		}
	}
	return rip.Version.Match(got), nil
}

// RIPScene draws a RIP scene (rip.Menu's), from the first column.
func (t *Term) RIPScene(scene string) {
	t.w.WriteString("\r\n" + scene)
	t.cur = t.cur[:0]
	t.ripScene, t.sinceKey = true, 0
}

// RIPClear takes a RIP scene off the screen: graphics, buttons, the text
// window as it was.
func (t *Term) RIPClear() {
	if !t.ripScene {
		return
	}
	t.w.WriteString("\r\n" + (&rip.Script{}).KillMouseFields().Reset().String() + "\x1b[2J\x1b[H")
	t.cur = t.cur[:0]
	t.ripScene, t.sinceKey = false, 0
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
