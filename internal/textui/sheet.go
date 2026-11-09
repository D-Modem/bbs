package textui

import (
	"strings"
	"unicode"

	"github.com/D-Modem/bbs/internal/screens"
	"github.com/D-Modem/bbs/internal/view"
)

// Sheets on ANSI terminals, as the BBS setup programs of the 1990s drew
// them: a menu bar of pull-down menus at the top, a shaded blue desk, each
// sheet a window over the one it was opened from, the entry's help in a box
// below and the keys on a bar at the foot.  Values are changed in boxes
// that pop up over it (a line to type in, a list of choices), and questions
// and help come up in dialogs.  The desk stays up between sheets until
// EndSheets.  Elsewhere a sheet is view.LineSheet's table.

// desk is the full screen kept up between sheets.
type desk struct {
	on     *screen         // what the terminal shows (nil: not known)
	stack  []under         // the sheets under the one up, outermost first
	depth  int             // the one up's: how many sheets it was opened through
	last   *view.Sheet     // the sheet up last, and its cursor
	lastAt int             //
	notify func() []string // the terminal's Notify, put back at the end
	flash  string          // a word for the next foot (Say)
}

// under is a sheet under the one up: its cursor, and its depth.
type under struct {
	sh    *view.Sheet
	cur   int
	depth int
}

// deskOK is whether the terminal can have the desk.
func (s *session) deskOK() bool {
	t := s.t
	return t.ANSI && !t.VT52 && !t.PETSCII && t.Rows >= 14 && t.Cols >= 40
}

// startDesk puts the desk up, if it is not.
func (s *session) startDesk() *desk {
	if s.desk == nil {
		s.desk = &desk{notify: s.t.Notify}
		s.t.Notify = nil // what other nodes send waits: it would draw over the desk
	}
	return s.desk
}

// EndSheets takes the desk down: the screen cleared, the cursor shown.
func (s *session) EndSheets() {
	d := s.desk
	if d == nil {
		return
	}
	s.desk = nil
	s.t.Notify = d.notify
	s.t.Print("\x1b[0m\x1b[2J\x1b[H\x1b[?25h")
	s.t.Flush()
}

// deskBase is what a dialog goes over: the screen as it is.
func (s *session) deskBase() *screen {
	if s.desk.on != nil && s.desk.on.cols == s.t.Cols && s.desk.on.rows == s.t.Rows {
		return s.desk.on
	}
	return s.deskScreen(nil, -1)
}

// show sends what changed between the screen and scr.
func (s *session) show(scr *screen) {
	d := s.desk
	out := scr.render(d.on, s.t.Mono) + "\x1b[?25l"
	t := s.t
	switch {
	case t.UTF8:
		t.Print(out)
	case t.xl != nil: // a VT's: the arrows as letters
		t.Print(asciiGlyphs.Replace(out))
	default: // code page 437, the arrows its own
		t.w.Write(encode437(out))
	}
	d.on = scr.clone()
	s.t.Flush()
}

// glyphs code page 437 has in its control range, and an ellipsis for it
var cp437Glyphs = map[rune]byte{'►': 0x10, '◄': 0x11, '↕': 0x12, '↑': 0x18, '↓': 0x19, '→': 0x1a, '←': 0x1b,
	'▲': 0x1e, '▼': 0x1f, '…': 0xaf}

// asciiGlyphs are those for a terminal with neither
var asciiGlyphs = strings.NewReplacer("►", ">", "◄", "<", "↕", "|", "↑", "^", "↓", "v", "→", ">", "←", "<",
	"▲", "^", "▼", "v", "…", "~")

// encode437 is s in code page 437.
func encode437(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if b, ok := cp437Glyphs[r]; ok {
			out = append(out, b)
			continue
		}
		out = append(out, screens.EncodeCP437(string(r))...)
	}
	return out
}

// key reads a key; alt is true for a letter typed with Alt (Esc and the
// letter at once).
func (s *session) key() (k rune, alt bool, err error) {
	t := s.t
	k, err = t.EditKey()
	if err != nil || k != 0x1b || t.r.Buffered() == 0 {
		return k, false, err
	}
	n, err := t.EditKey()
	if err != nil {
		return 0, false, err
	}
	if n < 0x80 && unicode.IsLetter(n) {
		return unicode.ToUpper(n), true, nil
	}
	return 0x1b, false, nil
}

func isEnter(k rune) bool { return k == '\r' || k == '\n' }

// Sheet draws a sheet in its window on the desk and waits for the caller to
// do something on it.
func (s *session) Sheet(sh *view.Sheet) (view.SheetResult, error) {
	if !s.deskOK() {
		s.EndSheets()
		return view.LineSheet(s, sh)
	}
	d := s.startDesk()
	d.place(sh)
	w := &sheetWin{s: s, sh: sh, cur: -1}
	w.cur = w.step(sh.Cursor-1, 1)
	if w.cur < 0 {
		w.cur = max(sh.Cursor, 0)
	}
	res := func(action string) (view.SheetResult, error) {
		d.last, d.lastAt = sh, w.cur
		return view.SheetResult{Action: action, Entry: w.cur}, nil
	}
	set := func(id, v string) (view.SheetResult, error) {
		r, _ := res(view.SheetSet)
		r.Values = map[string]string{id: v}
		return r, nil
	}
	for {
		s.show(w.frame())
		k, alt, err := s.key()
		if err != nil {
			return view.SheetResult{}, err
		}
		if alt || k == '/' || k == KeyF1+11 {
			i := 0
			if alt {
				if i = w.barKey(k); i < 0 {
					continue
				}
			}
			r, ok, err := s.pullDown(w, i)
			if err != nil {
				return view.SheetResult{}, err
			}
			if ok {
				d.last, d.lastAt = sh, w.cur
				r.Entry = w.cur
				return r, nil
			}
			continue
		}
		switch {
		case k == KeyF1 || k == '?':
			return res(view.SheetHelp)
		case k == KeyF1+9:
			return res(view.SheetSave)
		case k == KeyF1+8:
			return res(view.SheetQuit)
		case k == 0x1b || k == KeyLeft:
			return res(view.Back)
		case k == KeyUp:
			w.move(w.step(w.cur, -1))
		case k == KeyDown || k == '\t':
			w.move(w.step(w.cur, 1))
		case k == KeyPgUp:
			w.move(w.page(-1))
		case k == KeyPgDn:
			w.move(w.page(1))
		case k == KeyHome:
			w.move(w.step(-1, 1))
		case k == KeyEnd:
			w.move(w.step(len(sh.Entries), -1))
		case k == 0x0c: // ^L: all of it again
			d.on = nil
		case isEnter(k) || k == ' ' || k == KeyRight:
			if w.cur < 0 || w.cur >= len(sh.Entries) {
				continue
			}
			e := sh.Entries[w.cur]
			if e.Disabled {
				continue
			}
			switch e.Kind {
			case view.EntryMenu, view.EntryAction:
				if k != ' ' {
					return res(view.Pick)
				}
			case view.EntryBool:
				if k != KeyRight {
					return set(e.ID, e.Next())
				}
			case view.EntryChoice:
				switch k {
				case ' ':
					return set(e.ID, e.Next())
				case KeyRight:
				default:
					v, ok, err := s.choiceBox(w.frame(), e)
					if err != nil {
						return view.SheetResult{}, err
					}
					if ok && v != e.Value {
						return set(e.ID, v)
					}
				}
			case view.EntryText:
				if k == ' ' {
					continue
				}
				v, ok, err := s.inputBox(w.frame(), e)
				if err != nil {
					return view.SheetResult{}, err
				}
				if ok && v != e.Value {
					return set(e.ID, v)
				}
			}
		default:
			if id := action(sh.Keys, k); id != "" {
				return res(id)
			}
			// a letter: the next entry beginning with it
			w.move(w.letter(unicode.ToUpper(k)))
		}
	}
}

// place puts the sheets this one was opened from under it: those its
// crumbs name, as they were last drawn (one jumped through, never drawn,
// leaves a gap).
func (d *desk) place(sh *view.Sheet) {
	parts := []string{sh.Title}
	if sh.Crumbs != "" {
		parts = strings.Split(sh.Crumbs, " > ")
	}
	names := parts[:len(parts)-1]
	var keep []under
	for _, u := range d.stack {
		if u.depth < len(names) && names[u.depth] == u.sh.Title {
			keep = append(keep, u)
		}
	}
	if l := d.last; l != nil && l != sh {
		for i := len(names) - 1; i >= 0; i-- {
			if names[i] != l.Title {
				continue
			}
			j := 0
			for j < len(keep) && keep[j].depth < i {
				j++
			}
			if j == len(keep) || keep[j].depth != i {
				keep = append(keep[:j], append([]under{{l, d.lastAt, i}}, keep[j:]...)...)
			}
			break
		}
	}
	d.stack, d.depth = keep, len(names)
	d.last = sh
}

// --- the sheet's window

type sheetWin struct {
	s   *session
	sh  *view.Sheet
	cur int // the entry the cursor is on
	top int // the first entry shown
}

// step is the next entry the cursor may rest on from i, by dir (-1 if none).
func (w *sheetWin) step(i, dir int) int {
	for j := i + dir; j >= 0 && j < len(w.sh.Entries); j += dir {
		if w.sh.Entries[j].Choosable() {
			return j
		}
	}
	if i >= 0 && i < len(w.sh.Entries) && w.sh.Entries[i].Choosable() {
		return i
	}
	return -1
}

// page is the entry the cursor goes to a page up (dir -1) or down (1).
func (w *sheetWin) page(dir int) int {
	g := w.s.geom(w.sh, w.s.desk.depth)
	j := min(max(w.cur+dir*(g.list-1), 0), len(w.sh.Entries)-1)
	if n := w.step(j-dir, dir); n >= 0 {
		return n
	}
	return w.step(j, -dir)
}

// letter is the next entry after the cursor, going round, that may be
// chosen and begins with r; -1 if none.
func (w *sheetWin) letter(r rune) int {
	n := len(w.sh.Entries)
	for k := 1; k <= n; k++ {
		j := (w.cur + k) % n
		e := w.sh.Entries[j]
		if e.Choosable() && strings.HasPrefix(strings.ToUpper(e.Label), string(r)) {
			return j
		}
	}
	return -1
}

// move puts the cursor on entry j (none if j < 0).
func (w *sheetWin) move(j int) {
	if j >= 0 {
		w.cur = j
	}
}

// geometry is a window's place: its top row and left column, its height
// and width (from 0, frame and all), the rows of notes and of entries.
type geometry struct{ row, col, h, w, notes, list int }

// layout is the desk's rows: the first and last a window may use, and
// whether there is a help box.
func (s *session) layout() (first, last int, help bool) {
	rows := s.t.Rows
	help = rows >= 20
	last = rows - 3 // above the key bar and the window's shadow
	if help {
		last = rows - 6 // the shadow under the help box
	}
	return 2, last, help
}

// geom is where a sheet's window goes at a depth (0: the first sheet).
func (s *session) geom(sh *view.Sheet, depth int) geometry {
	cols := s.t.Cols
	first, last, _ := s.layout()
	lw, vw := 0, 0
	for _, e := range sh.Entries {
		if e.Kind == view.EntryHeading {
			lw = max(lw, len([]rune(e.Label))+4-12)
			continue
		}
		lw = max(lw, len([]rune(e.Label)))
		v := e.Shown()
		if e.Note != "" {
			v += "  " + e.Note
		}
		vw = max(vw, len([]rune(v)))
	}
	for _, n := range sh.Note {
		vw = max(vw, len([]rune(n.Label+n.Text))-lw-4)
	}
	maxW := cols - 4
	width := 2 + 2 + lw + 2 + min(max(vw, 12), 40) + 3
	width = max(width, len([]rune(sh.Title))+10, min(46, maxW))
	width = min(width, maxW)
	off := min(depth, 5)
	g := geometry{w: width, col: 2 + 3*off, row: first + off}
	if g.col+g.w+2 > cols {
		g.col = max(cols-2-g.w, 0)
	}
	g.notes = min(len(sh.Note), 3)
	if g.notes > 0 {
		g.notes++ // the rule under them
	}
	g.list = max(len(sh.Entries), 1)
	room := last - g.row + 1
	if g.h = 2 + g.notes + g.list; g.h > room {
		if room < 2+g.notes+5 { // too far down: up as far as needed
			g.row = max(first, last+1-(2+g.notes+5))
			room = last - g.row + 1
		}
		g.h = min(g.h, room)
		g.list = max(g.h-2-g.notes, 1)
	}
	return g
}

// colors of a window, as it is up or under another
type winColors struct {
	frame, title, label, value, unset, note, mark, info, off, head attr
	bar, barValue                                                  attr
	box                                                            boxStyle
}

var (
	activeWin = winColors{
		frame: attr{cWhite, cBlue}, title: attr{cYellow, cBlue}, label: attr{cWhite, cBlue},
		value: attr{cYellow, cBlue}, unset: attr{cLightGray, cBlue}, note: attr{cCyan, cBlue},
		mark: attr{cLightRed, cBlue}, info: attr{cLightGray, cBlue}, off: attr{cDarkGray, cBlue},
		head: attr{cLightCyan, cBlue}, bar: attr{cWhite, cCyan}, barValue: attr{cYellow, cCyan}, box: boxDouble,
	}
	inactiveWin = winColors{
		frame: attr{cLightGray, cBlue}, title: attr{cLightGray, cBlue}, label: attr{cLightGray, cBlue},
		value: attr{cLightGray, cBlue}, unset: attr{cLightGray, cBlue}, note: attr{cLightGray, cBlue},
		mark: attr{cLightGray, cBlue}, info: attr{cLightGray, cBlue}, off: attr{cDarkGray, cBlue},
		head: attr{cLightGray, cBlue}, bar: attr{cBlack, cLightGray}, barValue: attr{cBlack, cLightGray}, box: boxSingle,
	}
)

// frame is the whole screen with the sheet up: the desk, the menu bar,
// the windows under it, its own, the help box and the keys.
func (w *sheetWin) frame() *screen {
	s, d, sh := w.s, w.s.desk, w.sh
	scr := s.deskScreen(sh.Menus, -1)
	for _, u := range d.stack {
		s.drawWin(scr, u.sh, u.depth, u.cur, nil, inactiveWin)
	}
	s.drawWin(scr, sh, d.depth, w.cur, &w.top, activeWin)
	var e *view.Entry
	if w.cur >= 0 && w.cur < len(sh.Entries) {
		e = &sh.Entries[w.cur]
	}
	status, flash := sh.Status, d.flash
	d.flash = ""
	if s.helpBox(scr, e, status, flash) {
		status = "" // on the help box
	} else if flash != "" {
		status = flash
	}
	keys := [][2]string{{"F1", "Help"}}
	if e != nil {
		switch e.Kind {
		case view.EntryText, view.EntryChoice, view.EntryBool:
			keys = append(keys, [2]string{"Enter", "Change"})
		default:
			keys = append(keys, [2]string{"Enter", "Select"})
		}
	}
	keys = append(keys, [2]string{"Esc", "Back"})
	if len(sh.Menus) > 0 {
		keys = append(keys, [2]string{"/", "Menu"})
	}
	for _, a := range sh.Keys {
		if a.Key != 0 { // a 3270's PF key alone: nothing to press here
			keys = append(keys, [2]string{string(a.Key), a.Label})
		}
	}
	keys = append(keys, [2]string{"F9", "Quit"}, [2]string{"F10", "Save"})
	s.keyBar(scr, keys, status)
	return scr
}

// deskScreen is the bare desk: the shading, and the menu bar (open: the
// menu shown pulled down, -1 none).
func (s *session) deskScreen(menus []view.PullDown, open int) *screen {
	t := s.t
	scr := newScreen(t.Cols, t.Rows)
	scr.fill(1, 0, t.Rows-2, t.Cols, '░', attr{cLightGray, cBlue})
	bar := attr{cBlack, cLightGray}
	scr.fill(0, 0, 1, t.Cols, ' ', bar)
	for i, m := range barMenus(menus) {
		a, hot := bar, attr{cRed, cLightGray}
		if i == open {
			a, hot = attr{cBlack, cGreen}, attr{cRed, cGreen}
		}
		scr.put(0, m.col-1, " "+m.label+" ", a)
		if m.hot >= 0 {
			scr.put(0, m.col+m.hot, string([]rune(m.label)[m.hot]), hot)
		}
	}
	name := " BBS Setup "
	if b := s.b; b != nil {
		if n := b.Conf().Name; n != "" {
			name = " " + n + " Setup "
		}
	}
	if menuEnd(menus)+len([]rune(name))+2 < t.Cols {
		scr.put(0, t.Cols-len([]rune(name))-1, name, bar)
	}
	return scr
}

// barMenu is a menu's place on the bar.
type barMenu struct {
	label string
	col   int // where its label begins
	hot   int // its hot key's rune in label (-1: none)
	key   rune
}

// helpMenu is the menu bar's last: what the keys do, and what this is.
var helpMenu = view.PullDown{Label: "Help", Items: []view.PullItem{
	{Label: "Keys", Action: "keys"}, {Label: "About", Action: "about"}}}

// barMenus are the bar's menus (with Help after them, if there are any),
// placed, each with a hot key no other has.
func barMenus(menus []view.PullDown) []barMenu {
	if len(menus) == 0 {
		return nil
	}
	menus = append(menus[:len(menus):len(menus)], helpMenu)
	var out []barMenu
	used := map[rune]bool{}
	col := 3
	for _, m := range menus {
		b := barMenu{label: m.Label, col: col, hot: -1}
		for i, r := range []rune(m.Label) {
			u := unicode.ToUpper(r)
			if unicode.IsLetter(r) && !used[u] {
				b.hot, b.key, used[u] = i, u, true
				break
			}
		}
		out = append(out, b)
		col += len([]rune(m.Label)) + 2
	}
	return out
}

// menuEnd is the column after the bar's last menu.
func menuEnd(menus []view.PullDown) int {
	bm := barMenus(menus)
	if len(bm) == 0 {
		return 0
	}
	l := bm[len(bm)-1]
	return l.col + len([]rune(l.label)) + 1
}

// barKey is the menu whose hot key k is (-1: none).
func (w *sheetWin) barKey(k rune) int {
	for i, m := range barMenus(w.sh.Menus) {
		if m.key == k {
			return i
		}
	}
	return -1
}

// drawWin draws a sheet's window at a depth, its cursor's entry barred;
// top (if not nil) is scrolled to keep the cursor in it.
func (s *session) drawWin(scr *screen, sh *view.Sheet, depth, cur int, top *int, c winColors) {
	g := s.geom(sh, depth)
	scr.box(g.row, g.col, g.h, g.w, c.box, c.frame, c.label)
	scr.shadow(g.row, g.col, g.h, g.w)
	scr.title(g.row, g.col, g.w, sh.Title, c.title)
	inner := g.w - 2
	for i := 0; i < min(len(sh.Note), 3); i++ {
		l := sh.Note[i]
		scr.putw(g.row+1+i, g.col+1, inner, " "+trunc(l.Label+l.Text, inner-2), toneAttr(l.Tone, c))
	}
	if g.notes > 0 {
		scr.rule(g.row+g.notes, g.col, g.w, c.box, c.frame)
	}
	t := 0
	if top != nil {
		t = *top
	}
	if cur >= 0 && cur < t {
		t = cur
		if t > 0 && sh.Entries[t-1].Kind == view.EntryHeading {
			t-- // a heading just above comes too
		}
	}
	if cur >= t+g.list {
		t = cur - g.list + 1
	}
	t = max(min(t, len(sh.Entries)-g.list), 0)
	if top != nil {
		*top = t
	}
	lw, vmax := 0, 0
	for _, e := range sh.Entries {
		if e.Kind != view.EntryHeading {
			lw = max(lw, len([]rune(e.Label)))
			vmax = max(vmax, len([]rune(e.Shown())))
		}
	}
	lw = min(lw, max(inner*11/20, inner-6-max(vmax, 8)))
	vw := inner - 2 - lw - 2 - 2
	first := g.row + 1 + g.notes
	for r := 0; r < g.list; r++ {
		i := t + r
		row := first + r
		if i >= len(sh.Entries) {
			break
		}
		e := sh.Entries[i]
		if e.Kind == view.EntryHeading {
			line := strings.Repeat("─", inner)
			scr.put(row, g.col+1, line, c.off)
			if e.Label != "" {
				scr.put(row, g.col+2, " "+trunc(e.Label, inner-4)+" ", c.head)
			}
			continue
		}
		label, value, unset, note, mark := c.label, c.value, c.unset, c.note, c.mark
		switch {
		case e.Disabled:
			label, value, unset, note = c.off, c.off, c.off, c.off
		case e.Kind == view.EntryInfo:
			value = c.info
		case e.Kind == view.EntryMenu:
			value = c.note
		}
		if i == cur {
			scr.fill(row, g.col+1, 1, inner, ' ', c.bar)
			label, unset, note, mark = c.bar, c.bar, c.bar, c.barValue
			value = c.barValue
			if e.Disabled {
				label, value = attr{cDarkGray, c.bar.bg}, attr{cDarkGray, c.bar.bg}
			}
		}
		if e.Changed {
			scr.put(row, g.col+1, "*", mark)
		}
		scr.putw(row, g.col+2, lw, trunc(e.Label, lw), label)
		v := e.Shown()
		va := value
		if e.Value == "" && e.Kind != view.EntryMenu {
			va = unset
		}
		x := g.col + 2 + lw + 2
		n := scr.put(row, x, trunc(v, vw), va)
		if e.Note != "" && n+3 < vw && vw >= 20 {
			scr.put(row, x+n+2, trunc(e.Note, vw-n-2), note)
		}
		switch e.Kind {
		case view.EntryMenu:
			scr.put(row, g.col+g.w-3, "►", value)
		case view.EntryChoice:
			scr.put(row, g.col+g.w-3, "↕", note)
		}
	}
	if t > 0 {
		scr.put(first, g.col+g.w-1, "▲", c.frame)
	}
	if t+g.list < len(sh.Entries) {
		scr.put(first+g.list-1, g.col+g.w-1, "▼", c.frame)
	}
}

// toneAttr is a note line's colors in a window.
func toneAttr(tn view.Tone, c winColors) attr {
	bg := c.label.bg
	switch tn {
	case view.Bad:
		return attr{cLightRed, bg}
	case view.Warn:
		return attr{cYellow, bg}
	case view.Good:
		return attr{cLightGreen, bg}
	case view.Dim:
		return attr{cLightGray, bg}
	case view.Bright:
		return attr{cWhite, bg}
	}
	return c.label
}

// helpBox draws the box of the cursor's entry's help above the key bar
// (where the screen has the rows).
func (s *session) helpBox(scr *screen, e *view.Entry, status, flash string) bool {
	_, last, ok := s.layout()
	if !ok {
		return false
	}
	cols := s.t.Cols
	row := last + 1
	scr.box(row, 0, 4, cols, boxSingle, attr{cLightCyan, cBlue}, attr{cWhite, cBlue})
	if status != "" {
		st := " " + trunc(status, cols/2) + " "
		scr.put(row+3, cols-len([]rune(st))-2, st, attr{cYellow, cBlue})
	}
	if flash != "" { // what was just said, in place of the help
		scr.title(row, 0, cols, "Note", attr{cYellow, cBlue})
		for i, l := range wrapText(flash, cols-4) {
			if i < 2 {
				scr.put(row+1+i, 2, l, attr{cLightRed, cBlue})
			}
		}
		return true
	}
	if e == nil {
		return true
	}
	scr.title(row, 0, cols, e.Label, attr{cYellow, cBlue})
	help := e.Help
	if help == "" && e.Kind == view.EntryText && len([]rune(e.Value)) > cols/2 {
		help = e.Value // a long value: whole here
	}
	lines := wrapText(capFirst(help), cols-4)
	more := ""
	switch {
	case e.Kind == view.EntryChoice && len(e.Choices) > 0:
		more = "One of: " + strings.Join(choiceLabels(e.Choices), ", ")
	case e.Default != "" && e.Default != "default" && e.Value == "":
		more = "Default: " + e.Default
	}
	if len(lines) > 2 || len(lines) == 2 && more != "" {
		lines[1] = trunc(lines[1], cols-14) + "  (F1: more)"
		lines = lines[:2]
	} else if more != "" && len(lines) < 2 {
		if len(lines) == 1 && lines[0] == "" {
			lines = lines[:0]
		}
		lines = append(lines, more)
	}
	for i, l := range lines {
		a := attr{cWhite, cBlue}
		if l == more {
			a = attr{cLightGray, cBlue}
		}
		scr.put(row+1+i, 2, trunc(l, cols-4), a)
	}
	return true
}

// capFirst is s with its first letter a capital.
func capFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

// choiceLabels are choices as they read: "" as "(default)".
func choiceLabels(cs []string) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c
		if c == "" {
			out[i] = "(default)"
		}
	}
	return out
}

// keyBar draws the bar of keys at the foot, and status at its right.
func (s *session) keyBar(scr *screen, keys [][2]string, status string) {
	cols, row := s.t.Cols, s.t.Rows-1
	bar, hot := attr{cBlack, cLightGray}, attr{cRed, cLightGray}
	scr.fill(row, 0, 1, cols, ' ', bar)
	room := cols - 1
	if status != "" {
		room -= min(len([]rune(status))+3, cols/2)
	}
	x := 1
	for _, k := range keys {
		n := len([]rune(k[0])) + 1 + len([]rune(k[1]))
		if x+n > room {
			break
		}
		scr.put(row, x, k[0], hot)
		scr.put(row, x+len([]rune(k[0]))+1, k[1], bar)
		x += n + 2
	}
	if status != "" {
		st := trunc(status, cols/2-2)
		scr.put(row, cols-len([]rune(st))-2, st, attr{cBlue, cLightGray})
	}
}

// --- pull-down menus

// pullDown shows the menu bar's menus, menu i pulled down, until one of
// their items is picked (true) or the caller leaves them.
func (s *session) pullDown(w *sheetWin, i int) (view.SheetResult, bool, error) {
	menus := append(w.sh.Menus[:len(w.sh.Menus):len(w.sh.Menus)], helpMenu)
	bm := barMenus(w.sh.Menus)
	if len(bm) == 0 {
		return view.SheetResult{}, false, nil
	}
	cur := -1
	first := func(m view.PullDown) int {
		for j, it := range m.Items {
			if !it.Heading {
				return j
			}
		}
		return -1
	}
	cur = first(menus[i])
	top := 0
	for {
		base := w.frame()
		scr := s.deskScreen(w.sh.Menus, i)
		// the bar as pulled down, over the sheet's frame
		for x := 0; x < scr.cols; x++ {
			*base.at(0, x) = *scr.at(0, x)
		}
		m := menus[i]
		width := 12
		for _, it := range m.Items {
			width = max(width, len([]rune(it.Label))+4)
		}
		width = min(width, s.t.Cols-2)
		col := min(bm[i].col-2, s.t.Cols-width-2)
		col = max(col, 0)
		room := s.t.Rows - 4
		n := min(len(m.Items), room-2)
		if cur >= 0 {
			if cur < top {
				top = cur
			}
			if cur >= top+n {
				top = cur - n + 1
			}
		}
		box := attr{cBlack, cLightGray}
		base.box(1, col, n+2, width, boxSingle, box, box)
		base.shadow(1, col, n+2, width)
		for r := 0; r < n; r++ {
			j := top + r
			it := m.Items[j]
			row := 2 + r
			if it.Heading {
				base.rule(row, col, width, boxSingle, box)
				if it.Label != "" {
					base.put(row, col+2, " "+trunc(it.Label, width-6)+" ", attr{cDarkGray, cLightGray})
				}
				continue
			}
			a := box
			if j == cur {
				a = attr{cBlack, cGreen}
			}
			base.putw(row, col+1, width-2, " "+trunc(it.Label, width-3), a)
		}
		if top > 0 {
			base.put(2, col+width-1, "▲", box)
		}
		if top+n < len(m.Items) {
			base.put(1+n, col+width-1, "▼", box)
		}
		s.keyBar(base, [][2]string{{"Enter", "Select"}, {"Esc", "Close"}, {"←→", "Menus"}, {"↑↓", "Items"}}, "")
		s.show(base)
		k, alt, err := s.key()
		if err != nil {
			return view.SheetResult{}, false, err
		}
		move := func(dir int) {
			for j := cur + dir; j >= 0 && j < len(m.Items); j += dir {
				if !m.Items[j].Heading {
					cur = j
					return
				}
			}
		}
		switch {
		case alt:
			if j := w.barKey(k); j >= 0 {
				i, cur, top = j, first(menus[j]), 0
			}
		case k == 0x1b || k == '/' || k == KeyF1+11:
			return view.SheetResult{}, false, nil
		case k == KeyLeft:
			i = (i + len(menus) - 1) % len(menus)
			cur, top = first(menus[i]), 0
		case k == KeyRight:
			i = (i + 1) % len(menus)
			cur, top = first(menus[i]), 0
		case k == KeyUp:
			move(-1)
		case k == KeyDown || k == '\t':
			move(1)
		case k == KeyHome:
			cur = -1
			move(1)
		case k == KeyEnd:
			cur = len(m.Items)
			move(-1)
		case k == KeyPgUp:
			for r := 0; r < n-1; r++ {
				move(-1)
			}
		case k == KeyPgDn:
			for r := 0; r < n-1; r++ {
				move(1)
			}
		case isEnter(k) || k == ' ':
			if cur < 0 {
				continue
			}
			it := m.Items[cur]
			switch {
			case it.Action == "keys":
				if err := s.messageBox(w.frame(), "Keys", keyHelp(len(w.sh.Menus) > 0)); err != nil {
					return view.SheetResult{}, false, err
				}
				return view.SheetResult{}, false, nil
			case it.Action == "about":
				if err := s.messageBox(w.frame(), "About", aboutHelp()); err != nil {
					return view.SheetResult{}, false, err
				}
				return view.SheetResult{}, false, nil
			case it.Action != "":
				return view.SheetResult{Action: it.Action}, true, nil
			case len(it.Path) > 0:
				return view.SheetResult{Action: view.SheetJump, Jump: it.Path}, true, nil
			}
		default:
			// a letter: the next item beginning with it
			u := unicode.ToUpper(k)
			for d := 1; d <= len(m.Items); d++ {
				j := (cur + d + len(m.Items)) % len(m.Items)
				it := m.Items[j]
				if !it.Heading && strings.HasPrefix(strings.ToUpper(it.Label), string(u)) {
					cur = j
					break
				}
			}
		}
	}
}

// keyHelp is the Keys dialog's lines.
func keyHelp(menus bool) []view.Line {
	ls := []view.Line{
		view.V("↑ ↓ PgUp PgDn  ", "move the bar"),
		view.V("Home End       ", "the first, the last"),
		view.V("A letter       ", "the next entry beginning with it"),
		view.V("Enter          ", "open a menu, change a value, do it"),
		view.V("Space          ", "turn a yes/no over, the next choice"),
		view.V("Esc ←          ", "back a window"),
		view.V("F1 ?           ", "the entry's whole help"),
	}
	if menus {
		ls = append(ls, view.V("/ F12 Alt+key  ", "the menu bar's pull-down menus"))
	}
	return append(ls,
		view.V("F9             ", "quit without saving"),
		view.V("F10            ", "save it all and leave"),
		view.V("Ctrl-L         ", "draw the screen again"),
		view.L(""),
		view.T(view.Dim, "A * marks a change not saved yet."))
}

// aboutHelp is the About dialog's lines.
func aboutHelp() []view.Line {
	return []view.Line{
		view.T(view.Bright, "D-Modem BBS Setup"),
		view.L(""),
		view.L("The board, its lines and modems, and every front end the BBS answers as: AOL, CompuServe, MSN, AIM and ICQ, Yahoo!, IRC, mail, news, the web, gopher, finger, Gemini, time servers and more."),
		view.L(""),
		view.T(view.Dim, "Nothing is written until you save (F10).  bbs.conf keeps its comments."),
	}
}

// --- dialogs over the desk

// dialog colors: a light gray box with a white frame
var (
	dlgFrame = attr{cWhite, cLightGray}
	dlgText  = attr{cBlack, cLightGray}
	dlgTitle = attr{cWhite, cLightGray}
	dlgField = attr{cYellow, cBlue}
	dlgBtn   = attr{cBlack, cGreen}
	dlgBtnOn = attr{cWhite, cGreen}
	dlgDim   = attr{cDarkGray, cLightGray}
)

// dlgTone is a line's color in a dialog.
func dlgTone(tn view.Tone) attr {
	switch tn {
	case view.Bad:
		return attr{cRed, cLightGray}
	case view.Warn:
		return attr{cBrown, cLightGray}
	case view.Good:
		return attr{cGreen, cLightGray}
	case view.Dim:
		return dlgDim
	case view.Bright:
		return attr{cBlue, cLightGray}
	}
	return dlgText
}

// dlgBox is where a dialog of inner width iw and h rows inside goes:
// centered on the desk.
func (s *session) dlgBox(iw, h int) (row, col, bw, bh int) {
	bw = min(iw+4, s.t.Cols-2)
	bh = min(h+2, s.t.Rows-3)
	row = max((s.t.Rows-bh)/2, 1)
	col = max((s.t.Cols-bw)/2, 0)
	return
}

// button draws a button, lit if on.
func button(scr *screen, row, col int, label string, on bool) int {
	a := dlgBtn
	if on {
		a = dlgBtnOn
		label = "►" + label + "◄"
	} else {
		label = " " + label + " "
	}
	n := scr.put(row, col, label, a)
	scr.put(row, col+n, "▄", attr{cBlack, cLightGray})
	scr.put(row+1, col+1, strings.Repeat("▀", n), attr{cBlack, cLightGray})
	return n
}

// messageBox shows lines in a dialog over base, scrolled with the cursor
// keys, until Enter or Esc.
func (s *session) messageBox(base *screen, title string, ls []view.Line) error {
	type tl struct {
		text string
		a    attr
	}
	iw := min(max(40, len([]rune(title))+6), s.t.Cols-6)
	for _, l := range ls {
		iw = max(iw, min(len([]rune(l.Label+l.Text)), s.t.Cols-6))
	}
	var lines []tl
	for _, l := range ls {
		for _, w := range wrapText(l.Label+l.Text, iw) {
			lines = append(lines, tl{w, dlgTone(l.Tone)})
		}
	}
	row, col, bw, bh := s.dlgBox(iw, len(lines)+3)
	body := bh - 5 // frame, a blank, the button and its shadow
	top := 0
	for {
		scr := base.clone()
		scr.box(row, col, bh, bw, boxDouble, dlgFrame, dlgText)
		scr.shadow(row, col, bh, bw)
		scr.title(row, col, bw, title, dlgTitle)
		for r := 0; r < body && top+r < len(lines); r++ {
			l := lines[top+r]
			scr.put(row+1+r, col+2, trunc(l.text, bw-4), l.a)
		}
		if top > 0 {
			scr.put(row+1, col+bw-1, "▲", dlgFrame)
		}
		if top+body < len(lines) {
			scr.put(row+body, col+bw-1, "▼", dlgFrame)
		}
		button(scr, row+bh-3, col+(bw-6)/2, "OK", true)
		keys := [][2]string{{"Enter", "OK"}}
		if len(lines) > body {
			keys = append(keys, [2]string{"↑↓ PgUp PgDn", "Scroll"})
		}
		s.keyBar(scr, keys, "")
		s.show(scr)
		k, _, err := s.key()
		if err != nil {
			return err
		}
		switch k {
		case KeyUp:
			top = max(top-1, 0)
		case KeyDown:
			top = max(min(top+1, len(lines)-body), 0)
		case KeyPgUp:
			top = max(top-body, 0)
		case KeyPgDn:
			top = max(min(top+body, len(lines)-body), 0)
		case KeyHome:
			top = 0
		case KeyEnd:
			top = max(len(lines)-body, 0)
		case '\r', '\n', 0x1b, ' ', 'O', 'o', 'Q', 'q':
			return nil
		}
	}
}

// confirmBox asks a yes or no question in a dialog over base, def lit.
func (s *session) confirmBox(base *screen, question string, def bool) (bool, error) {
	iw := min(max(36, len([]rune(question))), min(60, s.t.Cols-6))
	lines := wrapText(question, iw)
	row, col, bw, bh := s.dlgBox(iw, len(lines)+4)
	yes := def
	for {
		scr := base.clone()
		scr.box(row, col, bh, bw, boxDouble, dlgFrame, dlgText)
		scr.shadow(row, col, bh, bw)
		scr.title(row, col, bw, "Confirm", dlgTitle)
		for i, l := range lines {
			if 1+i < bh-4 {
				scr.put(row+1+i, col+2, l, dlgText)
			}
		}
		bx := col + bw/2 - 10
		button(scr, row+bh-3, bx, "Yes", yes)
		button(scr, row+bh-3, bx+12, "No", !yes)
		s.keyBar(scr, [][2]string{{"Y", "Yes"}, {"N", "No"}, {"←→", "Choose"}, {"Enter", "OK"}, {"Esc", "Cancel"}}, "")
		s.show(scr)
		k, _, err := s.key()
		if err != nil {
			return false, err
		}
		switch k {
		case 'y', 'Y':
			return true, nil
		case 'n', 'N', 0x1b:
			return false, nil
		case '\r', '\n', ' ':
			return yes, nil
		case KeyLeft, KeyRight, '\t':
			yes = !yes
		}
	}
}

// lineEdit edits a value in a field of w cells at row, col over base: the
// value and the key that ended it (Enter, Tab, Esc, Up or Down).
func (s *session) lineEdit(base *screen, row, col, w int, value string, maxLen int, secret bool) (string, rune, error) {
	if maxLen <= 0 {
		maxLen = 255
	}
	buf := []rune(value)
	pos, off := len(buf), 0
	for {
		if pos < off {
			off = pos
		}
		if pos >= off+w {
			off = pos - w + 1
		}
		shown := buf[off:min(len(buf), off+w)]
		str := string(shown)
		if secret {
			str = strings.Repeat("*", len(shown))
		}
		scr := base.clone()
		scr.putw(row, col, w, str, dlgField)
		if off > 0 {
			scr.put(row, col, "◄", attr{cLightCyan, cBlue})
		}
		if off+w < len(buf) {
			scr.put(row, col+w-1, "►", attr{cLightCyan, cBlue})
		}
		s.show(scr)
		s.t.Print(at(row+1, col+1+pos-off) + "\x1b[?25h")
		k, _, err := s.key()
		s.t.Print("\x1b[?25l")
		if err != nil {
			return "", 0, err
		}
		switch {
		case isEnter(k):
			return string(buf), '\r', nil
		case k == '\t' || k == 0x1b || k == KeyUp || k == KeyDown:
			return string(buf), k, nil
		case k == KeyLeft:
			pos = max(pos-1, 0)
		case k == KeyRight:
			pos = min(pos+1, len(buf))
		case k == KeyHome || k == 0x01:
			pos = 0
		case k == KeyEnd || k == 0x05:
			pos = len(buf)
		case k == 0x08 || k == 0x7f:
			if pos > 0 {
				buf = append(buf[:pos-1], buf[pos:]...)
				pos--
			}
		case k == KeyDelete || k == 0x04:
			if pos < len(buf) {
				buf = append(buf[:pos], buf[pos+1:]...)
			}
		case k == 0x15 || k == 0x19: // ^U ^Y: all of it
			buf, pos, off = buf[:0], 0, 0
		case k == 0x0b: // ^K: to the end
			buf = buf[:pos]
		case k >= ' ' && k < KeyUp && k != 0x7f && len(buf) < maxLen:
			buf = append(buf[:pos], append([]rune{k}, buf[pos:]...)...)
			pos++
		}
	}
}

// inputBox changes a text entry's value in a dialog over base: false if
// it was given up (Esc).
func (s *session) inputBox(base *screen, e view.Entry) (string, bool, error) {
	iw := min(64, s.t.Cols-8)
	help := wrapText(capFirst(e.Help), iw)
	if len(help) == 1 && help[0] == "" {
		help = nil
	}
	help = help[:min(len(help), 4)]
	var foot []string
	if e.Default != "" && e.Default != "default" {
		foot = append(foot, "Default: "+e.Default+" (left empty)")
	}
	if e.Secret {
		foot = append(foot, "Leave it empty to keep the one there is.")
	}
	row, col, bw, bh := s.dlgBox(iw, 2+len(help)+len(foot)+1)
	scr := base.clone()
	scr.box(row, col, bh, bw, boxDouble, dlgFrame, dlgText)
	scr.shadow(row, col, bh, bw)
	scr.title(row, col, bw, e.Label, dlgTitle)
	r := row + 1
	for _, h := range help {
		scr.put(r, col+2, h, dlgText)
		r++
	}
	r++
	field := r
	for i, f := range foot {
		scr.put(field+2+i, col+2, trunc(f, bw-4), dlgDim)
	}
	s.keyBar(scr, [][2]string{{"Enter", "OK"}, {"Esc", "Cancel"}, {"Ctrl-U", "Clear"}}, "")
	initial := e.Value
	if e.Secret {
		initial = ""
	}
	v, k, err := s.lineEdit(scr, field, col+2, bw-4, initial, e.Width, e.Secret)
	if err != nil || k == 0x1b {
		return "", false, err
	}
	if e.Secret && v == "" {
		return "", false, nil // nothing typed: kept
	}
	return v, true, nil
}

// choiceBox picks one of a choice entry's values from a list over base:
// false if it was given up.
func (s *session) choiceBox(base *screen, e view.Entry) (string, bool, error) {
	labels := choiceLabels(e.Choices)
	cur := 0
	now := e.Value
	if now == "" {
		now = e.Default
	}
	iw := len([]rune(e.Label)) + 6
	for i, c := range e.Choices {
		iw = max(iw, len([]rune(labels[i]))+6)
		if c == now || c == "" && e.Value == "" {
			cur = i
		}
	}
	iw = min(max(iw, 24), s.t.Cols-8)
	n := min(len(labels), s.t.Rows-10)
	row, col, bw, bh := s.dlgBox(iw, n+4)
	n = min(n, bh-6)
	top := 0
	for {
		if cur < top {
			top = cur
		}
		if cur >= top+n {
			top = cur - n + 1
		}
		scr := base.clone()
		scr.box(row, col, bh, bw, boxDouble, dlgFrame, dlgText)
		scr.shadow(row, col, bh, bw)
		scr.title(row, col, bw, e.Label, dlgTitle)
		scr.put(row+1, col+2, "Values", attr{cYellow, cLightGray})
		lw := bw - 4
		for r := 0; r < n; r++ {
			j := top + r
			a := attr{cBlack, cCyan}
			if j == cur {
				a = attr{cWhite, cGreen}
			}
			l := " " + labels[j]
			if e.Choices[j] == now || e.Choices[j] == "" && e.Value == "" {
				l += " *"
			}
			scr.putw(row+2+r, col+2, lw, l, a)
		}
		if top > 0 {
			scr.put(row+2, col+2+lw-1, "▲", attr{cBlue, cCyan})
		}
		if top+n < len(labels) {
			scr.put(row+1+n, col+2+lw-1, "▼", attr{cBlue, cCyan})
		}
		scr.put(row+bh-2, col+2, trunc("* as it is now", lw), dlgDim)
		s.keyBar(scr, [][2]string{{"Enter", "OK"}, {"Esc", "Cancel"}, {"↑↓", "Choose"}}, "")
		s.show(scr)
		k, _, err := s.key()
		if err != nil {
			return "", false, err
		}
		switch {
		case isEnter(k) || k == ' ':
			return e.Choices[cur], true, nil
		case k == 0x1b:
			return "", false, nil
		case k == KeyUp:
			cur = max(cur-1, 0)
		case k == KeyDown || k == '\t':
			cur = min(cur+1, len(labels)-1)
		case k == KeyHome || k == KeyPgUp:
			cur = 0
		case k == KeyEnd || k == KeyPgDn:
			cur = len(labels) - 1
		default:
			u := unicode.ToUpper(k)
			for d := 1; d <= len(labels); d++ {
				j := (cur + d) % len(labels)
				if strings.HasPrefix(strings.ToUpper(labels[j]), string(u)) {
					cur = j
					break
				}
			}
		}
	}
}

// formBox fills a form's fields in a dialog over base: Tab and the cursor
// keys between them, Enter on the last (or anywhere, with ^S) to finish;
// false if it was given up.
func (s *session) formBox(base *screen, f *view.Form) (map[string]string, bool, error) {
	iw := min(64, s.t.Cols-8)
	var intro []view.Line
	for _, l := range f.Intro {
		for _, w := range wrapText(l.Label+l.Text, iw) {
			intro = append(intro, view.Line{Text: w, Tone: l.Tone})
		}
	}
	lw := 0
	for _, fd := range f.Fields {
		lw = max(lw, len([]rune(fieldLabel(fd))))
	}
	lw = min(lw, iw/2)
	h := len(intro) + len(f.Fields)
	if len(intro) > 0 {
		h++
	}
	row, col, bw, bh := s.dlgBox(iw, h+3)
	vals := make([]string, len(f.Fields))
	for i, fd := range f.Fields {
		if fd.Edit && !fd.Secret {
			vals[i] = fd.Value
		}
	}
	cur := 0
	for {
		scr := base.clone()
		scr.box(row, col, bh, bw, boxDouble, dlgFrame, dlgText)
		scr.shadow(row, col, bh, bw)
		scr.title(row, col, bw, f.Title, dlgTitle)
		r := row + 1
		for _, l := range intro {
			scr.put(r, col+2, trunc(l.Text, bw-4), dlgTone(l.Tone))
			r++
		}
		if len(intro) > 0 {
			r++
		}
		fw := bw - 4 - lw - 1
		rows := make([]int, len(f.Fields))
		for i, fd := range f.Fields {
			rows[i] = r
			scr.putw(r, col+2, lw, trunc(fieldLabel(fd), lw), dlgText)
			v := vals[i]
			if fd.Secret {
				v = strings.Repeat("*", len([]rune(v)))
			}
			a := attr{cLightGray, cBlue}
			if v == "" && fd.Value != "" && !fd.Edit {
				v = fd.Value
			}
			if vals[i] != "" {
				a = dlgField
			}
			scr.putw(r, col+2+lw+1, fw, trunc(v, fw), a)
			r++
		}
		s.keyBar(scr, [][2]string{{"Enter", "Next/OK"}, {"Tab", "Next"}, {"Esc", "Cancel"}}, "")
		width := f.Fields[cur].Width
		v, k, err := s.lineEdit(scr, rows[cur], col+2+lw+1, fw, vals[cur], width, f.Fields[cur].Secret)
		if err != nil {
			return nil, false, err
		}
		vals[cur] = v
		switch {
		case k == 0x1b:
			return map[string]string{}, false, nil
		case k == KeyUp:
			cur = max(cur-1, 0)
		case k == KeyDown || k == '\t':
			cur = (cur + 1) % len(f.Fields)
		case k == '\r':
			if cur < len(f.Fields)-1 {
				cur++
				continue
			}
			out := map[string]string{}
			for i, fd := range f.Fields {
				v := vals[i]
				if v == "" && !fd.Edit {
					if fd.Value == "" && i == 0 {
						return out, false, nil
					}
					v = fd.Value
				}
				out[fd.Name] = v
			}
			return out, true, nil
		}
	}
}

// fieldLabel is a form field's label on a dialog.
func fieldLabel(fd view.Field) string {
	l := fd.Label
	if l == "" {
		l = strings.TrimRight(fd.Prompt, ": ?")
	}
	return l
}
