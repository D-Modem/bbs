package textui

import (
	"strings"
	"unicode"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// Sheets on ANSI terminals: the whole screen, a bar of where it is at the
// top, the entries in a window the cursor keys move through, the entry's
// help and the keys at the foot.  Elsewhere a sheet is view.LineSheet's
// table.

// Sheet draws a sheet and waits for the caller to do something on it.
func (s *session) Sheet(sh *view.Sheet) (view.SheetResult, error) {
	t := s.t
	if !t.ANSI || t.VT52 || t.PETSCII || t.Rows < 12 || t.Cols < 40 {
		return view.LineSheet(s, sh)
	}
	notify := t.Notify
	t.Notify = nil // what other nodes send waits: it would draw over the sheet
	defer func() {
		t.Notify = notify
		t.Print("\x1b[?25h\x1b[0m\x1b[2J\x1b[H")
		t.Flush()
	}()
	d := &sheetDraw{t: t, sh: sh, cur: -1}
	d.cur = d.step(sh.Cursor-1, 1)
	if d.cur < 0 {
		d.cur = max(sh.Cursor, 0)
	}
	d.drawAll()
	for {
		t.Print("\x1b[?25l") // no cursor while moving: the bar shows where
		k, err := t.EditKey()
		if err != nil {
			return view.SheetResult{}, err
		}
		res := func(action string) view.SheetResult { return view.SheetResult{Action: action, Entry: d.cur} }
		switch {
		case k == KeyF1 || k == '?':
			return res(view.SheetHelp), nil
		case k == KeyF1+9:
			return res(view.SheetSave), nil
		case k == KeyF1+8:
			return res(view.SheetQuit), nil
		case k == 0x1b || k == KeyLeft:
			return res(view.Back), nil
		case k == KeyUp:
			d.move(d.step(d.cur, -1))
		case k == KeyDown || k == '\t':
			d.move(d.step(d.cur, 1))
		case k == KeyPgUp:
			d.move(d.page(-1))
		case k == KeyPgDn:
			d.move(d.page(1))
		case k == KeyHome:
			d.move(d.step(-1, 1))
		case k == KeyEnd:
			d.move(d.step(len(sh.Entries), -1))
		case k == 0x0c: // ^L
			d.drawAll()
		case k == '\r' || k == ' ' || k == KeyRight:
			if d.cur < 0 || d.cur >= len(sh.Entries) {
				continue
			}
			e := sh.Entries[d.cur]
			switch e.Kind {
			case view.EntryMenu, view.EntryAction:
				if k != ' ' {
					return res(view.Pick), nil
				}
			case view.EntryBool, view.EntryChoice:
				if k != KeyRight {
					r := res(view.SheetSet)
					r.Values = map[string]string{e.ID: e.Next()}
					return r, nil
				}
			case view.EntryText:
				if k == ' ' {
					continue
				}
				v, ok, err := d.edit(e)
				if err != nil {
					return view.SheetResult{}, err
				}
				if ok {
					r := res(view.SheetSet)
					r.Values = map[string]string{e.ID: v}
					return r, nil
				}
				d.drawAll()
			}
		default:
			if id := action(sh.Keys, k); id != "" {
				return res(id), nil
			}
			// a letter: the next entry beginning with it
			d.move(d.letter(unicode.ToUpper(k)))
		}
	}
}

type sheetDraw struct {
	t   *Term
	sh  *view.Sheet
	cur int // the entry the cursor is on
	top int // the first entry shown
}

// rows: where the entries' window begins, and how many rows it has
func (d *sheetDraw) first() int { return 3 + min(len(d.sh.Note), 3) }

// height is how many rows the entries' window has, at least 3.
func (d *sheetDraw) height() int { return max(d.t.Rows-3-d.first()+1, 3) }

// step is the next entry the cursor may rest on from i, by dir (-1 if none).
func (d *sheetDraw) step(i, dir int) int {
	for j := i + dir; j >= 0 && j < len(d.sh.Entries); j += dir {
		if d.sh.Entries[j].Choosable() {
			return j
		}
	}
	if i >= 0 && i < len(d.sh.Entries) && d.sh.Entries[i].Choosable() {
		return i
	}
	return -1
}

// page is the entry the cursor goes to a page up (dir -1) or down (1):
// the nearest that may be chosen, -1 if none.
func (d *sheetDraw) page(dir int) int {
	j := min(max(d.cur+dir*(d.height()-1), 0), len(d.sh.Entries)-1)
	if n := d.step(j-dir, dir); n >= 0 {
		return n
	}
	return d.step(j, -dir)
}

// letter is the next entry after the cursor, going round, that may be
// chosen and begins with r; -1 if none.
func (d *sheetDraw) letter(r rune) int {
	n := len(d.sh.Entries)
	for k := 1; k <= n; k++ {
		j := (d.cur + k) % n
		e := d.sh.Entries[j]
		if e.Choosable() && strings.HasPrefix(strings.ToUpper(e.Label), string(r)) {
			return j
		}
	}
	return -1
}

// move puts the cursor on entry j, drawing what changed.
func (d *sheetDraw) move(j int) {
	if j < 0 || j == d.cur {
		return
	}
	old := d.cur
	d.cur = j
	if d.scroll() {
		d.drawEntries()
	} else {
		d.drawEntry(old)
		d.drawEntry(j)
	}
	d.drawFoot()
}

// scroll keeps the cursor's entry in the window; true if the window moved.
func (d *sheetDraw) scroll() bool {
	top := d.top
	if d.cur < d.top {
		d.top = d.cur
		// a heading just above comes too
		if d.top > 0 && d.sh.Entries[d.top-1].Kind == view.EntryHeading {
			d.top--
		}
	}
	if d.cur >= d.top+d.height() {
		d.top = d.cur - d.height() + 1
	}
	return top != d.top
}

// drawAll draws the whole sheet: the title bar (the crumbs, if any), up to
// three lines of note, the entries, the foot.
func (d *sheetDraw) drawAll() {
	t := d.t
	d.scroll()
	var w strings.Builder
	w.WriteString("\x1b[0m\x1b[2J")
	bar := " " + d.sh.Title
	if d.sh.Crumbs != "" {
		bar = " " + d.sh.Crumbs
	}
	w.WriteString(at(1, 1) + "\x1b[0;1;37;44m" + bbs.Fit(bbs.Trunc(bar, t.Cols-1), t.Cols-1) + "\x1b[0m")
	for i, l := range d.sh.Note {
		if i == 3 {
			break
		}
		w.WriteString(at(2+i, 1) + d.tone(l))
	}
	t.Print(w.String())
	d.drawEntries()
	d.drawFoot()
}

// tone is a note's line, fitted to the screen, in its tone's color.
func (d *sheetDraw) tone(l view.Line) string {
	t := d.t
	s := bbs.Trunc(" "+l.Label+l.Text, t.Cols-1)
	switch l.Tone {
	case view.Bad:
		return t.C(Red, s)
	case view.Warn:
		return t.C(Yellow, s)
	case view.Good:
		return t.C(Green, s)
	case view.Dim:
		return t.C(Dim, s)
	case view.Bright:
		return t.C(White, s)
	}
	return s
}

// drawEntries draws every row of the entries' window.
func (d *sheetDraw) drawEntries() {
	for r := 0; r < d.height(); r++ {
		d.drawEntry(d.top + r)
	}
}

// columns: the label's, the note's
func (d *sheetDraw) widths() (label, note int) {
	note = 0
	for _, e := range d.sh.Entries {
		note = max(note, len(e.Note))
		if e.Kind != view.EntryHeading {
			label = max(label, len([]rune(e.Label))+2)
		}
	}
	label = min(max(label, 16), d.t.Cols/2)
	return label, min(note, d.t.Cols/4)
}

// drawEntry draws entry i on its row, if it is in the window (a row past
// the entries is cleared): a heading, or its changed mark, label, value
// and note in columns, in reverse under the cursor.
func (d *sheetDraw) drawEntry(i int) {
	t := d.t
	row := d.first() + i - d.top
	if i < d.top || row >= d.first()+d.height() {
		return
	}
	if i < 0 || i >= len(d.sh.Entries) {
		t.Print(at(row, 1) + "\x1b[0m\x1b[K")
		return
	}
	e := d.sh.Entries[i]
	if e.Kind == view.EntryHeading {
		t.Print(at(row, 1) + "\x1b[0m\x1b[K" + t.C(Cyan, bbs.Trunc(" "+e.Label, t.Cols-1)))
		return
	}
	lw, nw := d.widths()
	room := t.Cols - 1
	mark := "  "
	if e.Changed {
		mark = " *"
	}
	label := e.Label
	val := e.Shown()
	switch e.Kind {
	case view.EntryMenu:
		label += " >"
	}
	vw := max(room-2-lw-1-nw-1, 4)
	cells := mark + bbs.Fit(bbs.Trunc(label, lw), lw) + " " + bbs.Fit(bbs.Trunc(val, vw), vw) + " " +
		bbs.Fit(bbs.Trunc(e.Note, nw), nw)
	cells = bbs.Fit(bbs.Trunc(cells, room), room)
	var out string
	switch {
	case i == d.cur:
		out = "\x1b[0;7m" + cells + "\x1b[0m"
	case e.Disabled:
		out = t.C(Dim, cells)
	default:
		// the parts in their colors
		vc := val
		switch {
		case e.Changed:
			vc = t.C(Yellow, bbs.Fit(bbs.Trunc(val, vw), vw))
		case e.Value == "":
			vc = t.C(Dim, bbs.Fit(bbs.Trunc(val, vw), vw))
		default:
			vc = t.C(White, bbs.Fit(bbs.Trunc(val, vw), vw))
		}
		m := mark
		if e.Changed {
			m = t.C(Yellow, mark)
		}
		out = m + bbs.Fit(bbs.Trunc(label, lw), lw) + " " + vc + " " + t.C(Dim, bbs.Fit(bbs.Trunc(e.Note, nw), nw))
	}
	t.Print(at(row, 1) + "\x1b[0m\x1b[K" + out)
}

// drawFoot draws the entry's help, the status and the keys.
func (d *sheetDraw) drawFoot() {
	t := d.t
	help := ""
	if d.cur >= 0 && d.cur < len(d.sh.Entries) {
		e := d.sh.Entries[d.cur]
		help = e.Help
		if help == "" && e.Kind == view.EntryText && len(e.Value) > t.Cols/2 {
			help = e.Value // a long value: whole here
		}
	}
	keys := "Enter change  Esc back  F1 help  F9 quit  F10 save+exit"
	for _, a := range d.sh.Keys {
		keys = string(a.Key) + " " + a.Label + "  " + keys
	}
	w := t.Cols - 1
	var b strings.Builder
	b.WriteString(at(t.Rows-2, 1) + "\x1b[0m\x1b[K" + t.C(Dim, bbs.Trunc(" "+help, w)))
	b.WriteString(at(t.Rows-1, 1) + "\x1b[0m\x1b[K" + t.C(Yellow, bbs.Trunc(" "+d.sh.Status, w)))
	b.WriteString(at(t.Rows, 1) + "\x1b[0;30;46m" + bbs.Fit(bbs.Trunc(" "+keys, w), w) + "\x1b[0m")
	t.Print(b.String())
}

// edit reads a text entry's new value on the help row: false if it was
// left as it was.
func (d *sheetDraw) edit(e view.Entry) (string, bool, error) {
	t := d.t
	prompt := " " + e.Label + ": "
	t.Print(at(t.Rows-2, 1) + "\x1b[0m\x1b[K" + t.C(Cyan, prompt) + "\x1b[?25h")
	max := e.Width
	if max <= 0 {
		max = 255
	}
	var l Line
	var err error
	if e.Secret {
		l, err = t.ReadLine(LineOpts{Max: max, Mask: true})
		if err == nil && l.Text == "" {
			return "", false, nil // nothing typed: kept
		}
	} else {
		l, err = t.ReadLine(LineOpts{Max: max, Initial: e.Value})
	}
	t.Print("\x1b[?25l")
	if err != nil {
		return "", false, err
	}
	return l.Text, l.Text != e.Value, nil
}
