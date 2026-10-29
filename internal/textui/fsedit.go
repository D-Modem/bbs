package textui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/D-Modem/bbs/internal/bbs"
)

// The full-screen editor, for ANSI terminals: the message's lines on the
// screen, the cursor keys to move about, the text wrapping as it is
// typed, a window of the message replied to for quoting.

// fullScreen: this caller gets the full-screen editor.
func (s *session) fullScreen() bool {
	t := s.t
	return t.ANSI && !t.VT52 && !t.PETSCII && t.Rows >= 12 && t.Cols >= 40 &&
		s.n.Prefs()["editor"] != "line"
}

// fsEdit is the editor's screen: rows 1 and 2 the heading, the text
// below, the last row the keys (or a word to the caller).
type fsEdit struct {
	s       *session
	t       *Term
	b       *fsBuffer
	to, sub string
	top     int // the first line shown
	height  int // rows of text shown

	quote  []string // what may be quoted
	qOpen  bool     // the quote window is open
	qSel   int      // its line chosen
	qTop   int      // its first line shown
	status string
}

const textRow = 3 // the screen row the text begins on

// fullEdit edits a message on the whole screen; nil if the caller gave up.
func (s *session) fullEdit(to, subject string, body, quote []string) ([]string, error) {
	t := s.t
	notify := t.Notify
	t.Notify = nil // what other nodes send waits: it would draw over the text
	defer func() { t.Notify = notify }()
	e := &fsEdit{s: s, t: t, to: to, sub: subject, quote: quote,
		b: newBuffer(body, min(t.Cols-1, bbs.MaxLineLen), bbs.MaxBodyLines)}
	e.height = t.Rows - textRow
	e.drawAll()
	for {
		e.place()
		k, err := t.EditKey()
		if err != nil {
			return nil, err
		}
		if e.qOpen {
			e.quoteKey(k)
			continue
		}
		e.b.dirty, e.b.moved = -1, false
		e.status = ""
		ok := true
		switch {
		case k == 0x1a: // ^Z
			return e.done(), nil
		case k == 0x18 || k == 0x01: // ^X, ^A
			e.say("Give up this message? (Y/N)")
			e.place()
			if a, err := t.Key(); err != nil {
				return nil, err
			} else if a == 'Y' {
				t.Clear()
				return nil, nil
			}
			e.say("")
		case k == '\r' || k == '\n':
			if l := strings.ToLower(strings.TrimSpace(string(e.b.lines[e.b.row]))); l == "/s" || l == "/a" {
				// the line editor's commands, for fingers that know them
				e.b.KillLine()
				if l == "/s" {
					return e.done(), nil
				}
				t.Clear()
				return nil, nil
			}
			ok = e.b.Enter()
		case k == 8 || k == 0x7f:
			e.b.Backspace()
		case k == KeyDelete || k == 0x04: // ^D
			e.b.Delete()
		case k == 0x19: // ^Y
			e.b.KillLine()
		case k == 0x11: // ^Q
			if len(e.quote) == 0 {
				e.say("There is nothing to quote.")
			} else {
				e.openQuote()
			}
			continue
		case k == 0x0c: // ^L
			e.drawAll()
			continue
		case k >= KeyUp && k <= KeyDelete:
			e.b.Move(k, e.height-1)
		case k == '\t':
			for i := 0; i < 4 && ok; i++ {
				ok = e.b.Insert(' ')
			}
		case k == utf8.RuneError || !unicode.IsPrint(k):
			continue
		default:
			ok = e.b.Insert(k)
		}
		if !ok {
			e.say("That is as long as a message gets.")
		}
		e.redraw()
	}
}

// done clears the screen: the message's lines, to save.
func (e *fsEdit) done() []string {
	e.t.Clear()
	return e.b.Text()
}

// at is the escape sequence to a screen row and column (1-based).
func at(row, col int) string { return fmt.Sprintf("\x1b[%d;%dH", row, col) }

// line is buffer line i as drawn (or "" past the end).
func (e *fsEdit) line(i int) string {
	if i < len(e.b.lines) {
		return string(e.b.lines[i])
	}
	return ""
}

// drawAll draws the whole screen again: the To and Subject heading, the
// text from the top line shown, the last row.
func (e *fsEdit) drawAll() {
	t := e.t
	var w strings.Builder
	w.WriteString("\x1b[0m\x1b[2J")
	head := t.C(Cyan, "To: ") + t.C(Yellow, bbs.Trunc(e.to, 24)) + "  " + t.C(Cyan, "Subject: ") +
		t.C(White, bbs.Trunc(e.sub, max(t.Cols-40, 10)))
	w.WriteString(at(1, 1) + head)
	w.WriteString(at(2, 1) + t.C(Dim, strings.Repeat("-", min(t.Cols-1, 79))))
	t.Print(w.String())
	e.drawFrom(e.top)
	e.drawStatus()
}

// drawFrom draws the text rows from buffer line i down.
func (e *fsEdit) drawFrom(i int) {
	var w strings.Builder
	for r := max(i, e.top); r < e.top+e.height; r++ {
		w.WriteString(at(textRow+r-e.top, 1) + "\x1b[K" + e.line(r))
	}
	e.t.Print(w.String())
}

// drawStatus draws the last row: the word to the caller, or the keys, and
// the cursor's line of how many.
func (e *fsEdit) drawStatus() {
	t := e.t
	msg := e.status
	if msg == "" {
		msg = "^Z save  ^X give up  ^Y delete line  ^L redraw"
		if len(e.quote) > 0 {
			msg += "  ^Q quote"
		}
	}
	pos := fmt.Sprintf("%d/%d", e.b.row+1, len(e.b.lines))
	room := t.Cols - 2 - len(pos)
	t.Print(at(t.Rows, 1) + "\x1b[K" + t.C(Dim, bbs.Fit(bbs.Trunc(msg, room), room)) + " " + t.C(Dim, pos))
}

// say shows a word to the caller on the last row.
func (e *fsEdit) say(msg string) {
	e.status = msg
	e.drawStatus()
}

// scroll keeps the cursor's line on the screen; true if the text moved.
func (e *fsEdit) scroll() bool {
	top := e.top
	if e.b.row < e.top {
		e.top = e.b.row
	}
	if e.b.row >= e.top+e.height {
		e.top = e.b.row - e.height + 1
	}
	return top != e.top
}

// redraw draws what changed.
func (e *fsEdit) redraw() {
	switch {
	case e.scroll():
		e.drawFrom(e.top)
	case e.b.moved:
		e.drawFrom(e.b.dirty)
	case e.b.dirty >= 0:
		if e.b.dirty >= e.top && e.b.dirty < e.top+e.height {
			e.t.Print(at(textRow+e.b.dirty-e.top, 1) + "\x1b[K" + e.line(e.b.dirty))
		}
	}
	e.drawStatus()
}

// place puts the cursor where the caller types.
func (e *fsEdit) place() {
	if e.qOpen {
		e.t.Print(at(e.qRow()+1+e.qSel-e.qTop, 1))
		return
	}
	e.t.Print(at(textRow+e.b.row-e.top, e.b.col+1))
}

// The quote window: the lower half of the text rows.

// qRow is the quote window's title row, just below the text.
func (e *fsEdit) qRow() int { return textRow + e.height } // its title row
// qHeight is the quote window's rows of lines, down to the last row.
func (e *fsEdit) qHeight() int { return e.t.Rows - 1 - e.qRow() }

// openQuote gives the lower half of the text rows to the quote window, the
// cursor's line kept on the screen.
func (e *fsEdit) openQuote() {
	full := e.t.Rows - textRow
	e.height = full / 2
	e.qOpen = true
	e.scroll()
	e.drawFrom(e.top)
	e.drawQuote()
}

// closeQuote gives the text its rows back.
func (e *fsEdit) closeQuote() {
	e.qOpen = false
	e.height = e.t.Rows - textRow
	e.scroll()
	e.drawFrom(e.top)
	e.drawStatus()
}

// drawQuote draws the quote window, its chosen line in reverse, and which
// line of how many on the last row.
func (e *fsEdit) drawQuote() {
	t := e.t
	var w strings.Builder
	w.WriteString(at(e.qRow(), 1) + "\x1b[K" + t.C(Cyan, bbs.Trunc("-- Quote: Enter takes a line, ^Q or ESC closes "+
		strings.Repeat("-", 80), t.Cols-1)))
	for i := 0; i < e.qHeight(); i++ {
		j := e.qTop + i
		w.WriteString(at(e.qRow()+1+i, 1) + "\x1b[K")
		if j >= len(e.quote) {
			continue
		}
		l := bbs.Trunc(e.quote[j], t.Cols-1)
		if j == e.qSel {
			l = "\x1b[7m" + l + "\x1b[0m" // reverse
		}
		w.WriteString(l)
	}
	t.Print(w.String())
	e.say(fmt.Sprintf("Quoting %d of %d", e.qSel+1, len(e.quote)))
}

// quoteKey is a key while the quote window is open: ^Q or ESC closes it,
// the cursor keys choose a line, Enter puts it in at the cursor and goes on
// to the next; anything else is ignored.
func (e *fsEdit) quoteKey(k rune) {
	switch k {
	case 0x11, 0x1b:
		e.closeQuote()
		return
	case KeyUp:
		e.qSel = max(e.qSel-1, 0)
	case KeyDown:
		e.qSel = min(e.qSel+1, len(e.quote)-1)
	case KeyPgUp:
		e.qSel = max(e.qSel-e.qHeight(), 0)
	case KeyPgDn:
		e.qSel = min(e.qSel+e.qHeight(), len(e.quote)-1)
	case '\r', '\n':
		e.b.dirty, e.b.moved = -1, false
		if e.b.InsertLines(e.quote[e.qSel:e.qSel+1]) == 0 {
			e.say("That is as long as a message gets.")
			return
		}
		e.qSel = min(e.qSel+1, len(e.quote)-1)
		e.scroll()
		e.drawFrom(e.top)
	default:
		return
	}
	if e.qSel < e.qTop {
		e.qTop = e.qSel
	}
	if e.qSel >= e.qTop+e.qHeight() {
		e.qTop = e.qSel - e.qHeight() + 1
	}
	e.drawQuote()
}
