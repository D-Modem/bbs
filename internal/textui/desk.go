package textui

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// The desk: a full screen drawn in character cells, as the setup programs
// of the 1990s drew theirs (blue windows over a shaded desktop, a menu bar,
// a bar of keys).  What is drawn is kept, and only the cells that changed
// are sent, so that a caller at 2400 bit/s is not sent the whole screen for
// a moved bar.

// The sixteen colors of a PC's text mode: 0-7 as backgrounds too.
const (
	cBlack = iota
	cBlue
	cGreen
	cCyan
	cRed
	cMagenta
	cBrown
	cLightGray
	cDarkGray
	cLightBlue
	cLightGreen
	cLightCyan
	cLightRed
	cLightMagenta
	cYellow
	cWhite
)

// attr is a cell's colors: foreground (0-15) and background (0-7).
type attr struct{ fg, bg uint8 }

// cell is a character cell.
type cell struct {
	r rune
	a attr
}

// screen is a screen's cells.
type screen struct {
	cols, rows int
	c          []cell
}

func newScreen(cols, rows int) *screen {
	s := &screen{cols: cols, rows: rows, c: make([]cell, cols*rows)}
	for i := range s.c {
		s.c[i] = cell{' ', attr{cLightGray, cBlack}}
	}
	return s
}

// clone is a copy of s.
func (s *screen) clone() *screen {
	n := &screen{cols: s.cols, rows: s.rows, c: make([]cell, len(s.c))}
	copy(n.c, s.c)
	return n
}

// at is the cell at row and column (from 0), nil off the screen.
func (s *screen) at(row, col int) *cell {
	if row < 0 || col < 0 || row >= s.rows || col >= s.cols {
		return nil
	}
	return &s.c[row*s.cols+col]
}

// put writes str from row, col in a, cut at the screen's edge; how many
// cells it took.
func (s *screen) put(row, col int, str string, a attr) int {
	n := 0
	for _, r := range str {
		if c := s.at(row, col+n); c != nil {
			*c = cell{r, a}
		}
		n++
	}
	return n
}

// putw writes str in exactly w cells: cut, or padded with spaces.
func (s *screen) putw(row, col, w int, str string, a attr) {
	s.put(row, col, fit(str, w), a)
}

// fill fills a rectangle with r in a.
func (s *screen) fill(row, col, h, w int, r rune, a attr) {
	for y := row; y < row+h; y++ {
		for x := col; x < col+w; x++ {
			if c := s.at(y, x); c != nil {
				*c = cell{r, a}
			}
		}
	}
}

// paint sets the colors of a rectangle, keeping its characters.
func (s *screen) paint(row, col, h, w int, a attr) {
	for y := row; y < row+h; y++ {
		for x := col; x < col+w; x++ {
			if c := s.at(y, x); c != nil {
				c.a = a
			}
		}
	}
}

// boxStyle is a box's lines: corners, sides and the joins of a line
// across it.
type boxStyle struct{ tl, tr, bl, br, h, v, lj, rj rune }

var (
	boxDouble = boxStyle{'╔', '╗', '╚', '╝', '═', '║', '╟', '╢'}
	boxSingle = boxStyle{'┌', '┐', '└', '┘', '─', '│', '├', '┤'}
)

// box draws a box's frame and fills its inside.
func (s *screen) box(row, col, h, w int, st boxStyle, frame, inside attr) {
	s.fill(row+1, col+1, h-2, w-2, ' ', inside)
	for x := col + 1; x < col+w-1; x++ {
		s.put(row, x, string(st.h), frame)
		s.put(row+h-1, x, string(st.h), frame)
	}
	for y := row + 1; y < row+h-1; y++ {
		s.put(y, col, string(st.v), frame)
		s.put(y, col+w-1, string(st.v), frame)
	}
	s.put(row, col, string(st.tl), frame)
	s.put(row, col+w-1, string(st.tr), frame)
	s.put(row+h-1, col, string(st.bl), frame)
	s.put(row+h-1, col+w-1, string(st.br), frame)
}

// rule draws a line across a box at row, joined to its sides.
func (s *screen) rule(row, col, w int, st boxStyle, a attr) {
	s.put(row, col, string(st.lj), a)
	for x := col + 1; x < col+w-1; x++ {
		s.put(row, x, "─", a)
	}
	s.put(row, col+w-1, string(st.rj), a)
}

// title puts a title in a box's top line, centered: "[ Title ]".
func (s *screen) title(row, col, w int, t string, a attr) {
	t = " " + trunc(t, w-6) + " "
	s.put(row, col+(w-len([]rune(t)))/2, t, a)
}

// shadow darkens the cells to the right of and below a rectangle, as a
// window's shadow on the desk.
func (s *screen) shadow(row, col, h, w int) {
	dark := attr{cDarkGray, cBlack}
	s.paint(row+1, col+w, h, 2, dark)
	s.paint(row+h, col+2, 1, w, dark)
}

// render is what turns the terminal's screen, old (nil: unknown), into s:
// each changed run of cells, with its colors.  The last cell of the last
// line is left alone (writing it scrolls some terminals).
func (s *screen) render(old *screen, mono bool) string {
	var b strings.Builder
	if old == nil || old.cols != s.cols || old.rows != s.rows {
		b.WriteString("\x1b[0m\x1b[2J")
		old = nil
	}
	cur := attr{255, 255}
	for y := 0; y < s.rows; y++ {
		last := s.cols
		if y == s.rows-1 {
			last--
		}
		at := -1 // where the cursor is after the last cell written (-1: unknown)
		for x := 0; x < last; x++ {
			c := s.c[y*s.cols+x]
			if old != nil && old.c[y*s.cols+x] == c {
				// unchanged: skipped, unless a change follows so near
				// that writing the cells costs less than moving
				if at != x || !s.changedWithin(old, y, x, last, 8) {
					continue
				}
			}
			if at != x {
				b.WriteString("\x1b[" + strconv.Itoa(y+1) + ";" + strconv.Itoa(x+1) + "H")
			}
			if c.a != cur {
				b.WriteString(sgr(c.a, mono))
				cur = c.a
			}
			r := c.r
			if r < ' ' || r == 0x7f || !utf8.ValidRune(r) {
				r = ' '
			}
			b.WriteRune(r)
			at = x + 1
		}
	}
	return b.String()
}

// changedWithin is whether a cell of row y from x, before last, within n
// of x, differs from old's.
func (s *screen) changedWithin(old *screen, y, x, last, n int) bool {
	for i := x; i < min(x+n, last); i++ {
		if old.c[y*s.cols+i] != s.c[y*s.cols+i] {
			return true
		}
	}
	return false
}

// sgr is the escape that gives a's colors (in mono: bright as bold, a light
// background as reverse).
func sgr(a attr, mono bool) string {
	if mono {
		s := "\x1b[0"
		if a.fg >= 8 {
			s += ";1"
		}
		if a.bg == cLightGray || a.bg == cCyan || a.bg == cGreen {
			s += ";7"
		}
		return s + "m"
	}
	ansi := []int{0, 4, 2, 6, 1, 5, 3, 7} // the PC's order to ANSI's
	s := "\x1b[0"
	if a.fg >= 8 {
		s += ";1"
	}
	return s + ";" + strconv.Itoa(30+ansi[a.fg%8]) + ";" + strconv.Itoa(40+ansi[a.bg%8]) + "m"
}

// fit is s cut or padded with spaces to exactly w runes.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > w {
		return string(r[:w])
	}
	return s + strings.Repeat(" ", w-len(r))
}

// trunc is s cut to w runes, with "…" where it was cut.
func trunc(s string, w int) string {
	r := []rune(s)
	if w <= 0 {
		return ""
	}
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return string(r[:1])
	}
	return string(r[:w-1]) + "…"
}

// wrapText breaks s into lines of at most w runes, at spaces where it can.
func wrapText(s string, w int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, wd := range words {
			for len([]rune(wd)) > w {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				r := []rune(wd)
				out = append(out, string(r[:w]))
				wd = string(r[w:])
			}
			switch {
			case line == "":
				line = wd
			case len([]rune(line))+1+len([]rune(wd)) <= w:
				line += " " + wd
			default:
				out = append(out, line)
				line = wd
			}
		}
		out = append(out, line)
	}
	return out
}
