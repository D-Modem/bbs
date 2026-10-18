package textui

import "strings"

// fsBuffer is the full-screen editor's text: lines no wider than width,
// a cursor, and what changed for the screen to draw again.
type fsBuffer struct {
	lines    [][]rune
	row, col int
	width    int
	max      int // lines, at most

	// what changed: the first line (-1: none), and whether the lines
	// after it moved too
	dirty int
	moved bool
}

// newBuffer is body to edit, its lines wrapped to width, at most max lines;
// the cursor at the start and nothing to draw again.
func newBuffer(body []string, width, max int) *fsBuffer {
	b := &fsBuffer{width: width, max: max, dirty: -1}
	for _, l := range body {
		if max > 0 && len(b.lines) >= max {
			break // the rest past the limit, which nothing could save
		}
		b.lines = append(b.lines, []rune(l))
	}
	if len(b.lines) == 0 {
		b.lines = [][]rune{nil}
	}
	for i := 0; i < len(b.lines); i++ {
		b.wrap(i)
	}
	b.row, b.col, b.dirty, b.moved = 0, 0, -1, false
	return b
}

// touch notes that line i changed (and those after it, if moved).
func (b *fsBuffer) touch(i int, moved bool) {
	if b.dirty < 0 || i < b.dirty {
		b.dirty = i
	}
	b.moved = b.moved || moved
}

// Text is the lines, without trailing spaces or empty lines at the end.
func (b *fsBuffer) Text() []string {
	var out []string
	for _, l := range b.lines {
		out = append(out, strings.TrimRight(string(l), " "))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// full: there is no room for another line.
func (b *fsBuffer) full() bool { return len(b.lines) >= b.max }

// insertLine puts l in as line at, those after it moving down.  It does not
// look at max: callers see to the room.
func (b *fsBuffer) insertLine(at int, l []rune) {
	b.lines = append(b.lines, nil)
	copy(b.lines[at+1:], b.lines[at:])
	b.lines[at] = l
	b.touch(at, true)
}

// wrap breaks line i where it is too wide, at a space where there is one,
// the rest going on a line of its own after it; the cursor goes with its
// text.  False if there was no room for the lines.
func (b *fsBuffer) wrap(i int) bool {
	for i < len(b.lines) && len(b.lines[i]) > b.width {
		l := b.lines[i]
		cut, skip := b.width, 0
		for j := b.width; j > 0; j-- {
			if l[j] == ' ' {
				cut, skip = j, 1
				break
			}
		}
		if b.full() {
			b.lines[i] = l[:b.width]
			if b.row == i && b.col > b.width {
				b.col = b.width
			}
			b.touch(i, false)
			return false
		}
		rest := append([]rune(nil), l[cut+skip:]...)
		b.lines[i] = l[:cut:cut]
		b.insertLine(i+1, rest)
		if b.row == i && b.col >= cut+skip {
			b.row, b.col = i+1, b.col-cut-skip
		} else if b.row == i && b.col > cut {
			b.col = cut
		}
		i++
	}
	return true
}

// Insert types r at the cursor; false if there was no room for it.
func (b *fsBuffer) Insert(r rune) bool {
	l := b.lines[b.row]
	if len(l) >= b.width && b.full() {
		return false
	}
	l = append(l[:b.col], append([]rune{r}, l[b.col:]...)...)
	b.lines[b.row] = l
	b.col++
	b.touch(b.row, false)
	return b.wrap(b.row)
}

// Enter breaks the line at the cursor.
func (b *fsBuffer) Enter() bool {
	if b.full() {
		return false
	}
	l := b.lines[b.row]
	rest := append([]rune(nil), l[b.col:]...)
	b.lines[b.row] = l[:b.col:b.col]
	b.touch(b.row, false)
	b.insertLine(b.row+1, rest)
	b.row, b.col = b.row+1, 0
	return true
}

// join puts line i+1 on the end of line i.
func (b *fsBuffer) join(i int) {
	b.lines[i] = append(b.lines[i], b.lines[i+1]...)
	b.lines = append(b.lines[:i+1], b.lines[i+2:]...)
	b.touch(i, true)
	b.wrap(i)
}

// Backspace takes away what is before the cursor: a character, or the
// line break.
func (b *fsBuffer) Backspace() {
	switch {
	case b.col > 0:
		l := b.lines[b.row]
		b.lines[b.row] = append(l[:b.col-1], l[b.col:]...)
		b.col--
		b.touch(b.row, false)
	case b.row > 0:
		b.row--
		b.col = len(b.lines[b.row])
		b.join(b.row)
	}
}

// Delete takes away what is at the cursor.
func (b *fsBuffer) Delete() {
	l := b.lines[b.row]
	switch {
	case b.col < len(l):
		b.lines[b.row] = append(l[:b.col], l[b.col+1:]...)
		b.touch(b.row, false)
	case b.row+1 < len(b.lines):
		b.join(b.row)
	}
}

// KillLine takes away the cursor's line.
func (b *fsBuffer) KillLine() {
	if len(b.lines) == 1 {
		b.lines[0] = nil
	} else {
		b.lines = append(b.lines[:b.row], b.lines[b.row+1:]...)
		if b.row >= len(b.lines) {
			b.row = len(b.lines) - 1
		}
	}
	b.col = 0
	b.touch(b.row, true)
}

// InsertLines puts lines (a quote) before the cursor's line, or in its
// place if it is empty; as many as there is room for.
func (b *fsBuffer) InsertLines(ls []string) int {
	n := 0
	for _, l := range ls {
		r := []rune(l)
		if len(r) > b.width {
			r = r[:b.width]
		}
		if len(b.lines[b.row]) == 0 && b.row == len(b.lines)-1 {
			// the last line, empty: the quote goes there, a new empty one after
			if b.full() {
				break
			}
			b.lines[b.row] = r
			b.insertLine(b.row+1, nil)
		} else {
			if b.full() {
				break
			}
			b.insertLine(b.row, r)
		}
		b.row++
		n++
	}
	b.col = 0
	return n
}

// Move moves the cursor: up and down a line (or page), left and right a
// character (over line ends), to the line's start or end.
func (b *fsBuffer) Move(k rune, page int) {
	switch k {
	case KeyUp:
		b.row = max(b.row-1, 0)
	case KeyDown:
		b.row = min(b.row+1, len(b.lines)-1)
	case KeyPgUp:
		b.row = max(b.row-page, 0)
	case KeyPgDn:
		b.row = min(b.row+page, len(b.lines)-1)
	case KeyLeft:
		if b.col > 0 {
			b.col--
		} else if b.row > 0 {
			b.row--
			b.col = len(b.lines[b.row])
		}
		return
	case KeyRight:
		if b.col < len(b.lines[b.row]) {
			b.col++
		} else if b.row+1 < len(b.lines) {
			b.row, b.col = b.row+1, 0
		}
		return
	case KeyHome:
		b.col = 0
		return
	case KeyEnd:
		b.col = len(b.lines[b.row])
		return
	}
	b.col = min(b.col, len(b.lines[b.row]))
}
