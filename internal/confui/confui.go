// Package confui is the configuration screens: menus by what a sysop
// thinks of (the board, the system, connections, services, users,
// maintenance) over bbs.conf's sections and the accounts in the database.
// Nothing is changed until the sysop saves (F10, or Save and exit on the
// main menu): then the file is checked as the BBS would read it and
// written with every comment kept, the accounts' and areas' changes are
// made, and on a running BBS what may change while callers are on is put
// in force.  F9 (or Quit without saving) leaves it all as it was.  The
// same screens run on the console (bbs config) and on the sysop's menu.
package confui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	bbssec "github.com/D-Modem/bbs"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/confedit"
	"github.com/D-Modem/bbs/internal/view"
)

// Editor is the screens, on a front end's UI.
type Editor struct {
	UI    view.UI
	Tools bbs.ConfigTools
	// Open is the BBS whose database the accounts and areas are in
	// (nil, or an error: those menus say why they are not there)
	Open func() (*bbs.BBS, error)
	Who  string                        // who edits, for the log ("": the console)
	Log  func(format string, a ...any) // nil: none

	cat    *confedit.Catalog
	read   []byte // the file as read
	absent bool   // there was no file: it is begun from the sample
	orig   *confedit.Doc
	doc    *confedit.Doc

	db     *data // the accounts and areas, as read and as changed (nil: not read yet)
	crumbs []string
	flash  string // a word for the next screen's foot
}

var (
	errSave  = errors.New("save")  // F10: save all and leave
	errQuit  = errors.New("quit")  // F9: leave without
	errClose = errors.New("close") // the page's subject is gone: back a page
)

// Run shows the screens until the sysop saves or leaves.
func (e *Editor) Run() error {
	e.cat = confedit.NewCatalog(bbssec.Sample)
	if ok, err := e.load(); err != nil || !ok {
		return err
	}
	for {
		e.crumbs = nil
		err := e.show(e.mainMenu())
		switch {
		case err == nil || errors.Is(err, errQuit):
			if n := e.changes(); n > 0 {
				yes, err := e.UI.Confirm(fmt.Sprintf("Leave without saving %d changes?", n), false)
				if err != nil {
					return err
				}
				if !yes {
					continue
				}
			}
			return nil
		case errors.Is(err, errSave):
			done, err := e.save()
			if err != nil || done {
				return err
			}
		default:
			return err
		}
	}
}

// load reads the file (or, if there is none, offers the sample to begin
// from); false: it cannot be edited here.
func (e *Editor) load() (bool, error) {
	data, err := os.ReadFile(e.Tools.Path)
	e.absent = false
	switch {
	case errors.Is(err, os.ErrNotExist):
		yes, err := e.UI.Confirm("There is no "+e.Tools.Path+".  Begin it from the sample?", true)
		if err != nil || !yes {
			return false, err
		}
		e.absent, e.read = true, nil
		data = bbssec.Sample
	case err != nil:
		e.UI.Say(view.Bad, capital(err.Error())+".")
		return false, nil
	default:
		e.read = data
	}
	doc, err := confedit.Parse(data)
	if err != nil {
		e.UI.Say(view.Bad, e.Tools.Path+": "+err.Error()+": put that right by hand first.")
		return false, nil
	}
	e.doc = doc
	if e.absent {
		e.orig, _ = confedit.Parse(nil)
	} else {
		e.orig, _ = confedit.Parse(data)
	}
	return true, nil
}

// --- pages

// item is an entry and what it does.
type item struct {
	view.Entry
	open func() error         // EntryMenu: another page
	set  func(v string) error // a value's
	do   func() error         // EntryAction
	help string               // the whole help, for F1 (Entry.Help: its first line)
}

// page is a screen of items, made again each time it is drawn.
type page struct {
	title string
	note  []view.Line
	items func() []item
	keys  []view.Action
	key   func(id string) error // a key of keys
}

// show draws a page until the sysop goes back from it, doing what is picked
// or set on it; its title is in the crumbs while it is up.  errSave and
// errQuit come back from any depth; errClose from what a page does is only
// back a page (nil).
func (e *Editor) show(p page) error {
	e.crumbs = append(e.crumbs, p.title)
	defer func() { e.crumbs = e.crumbs[:len(e.crumbs)-1] }()
	cursor := 0
	for {
		its := p.items()
		sh := &view.Sheet{Title: p.title, Crumbs: strings.Join(e.crumbs, " > "), Note: p.note,
			Cursor: cursor, Keys: p.keys, Status: e.status()}
		for _, it := range its {
			sh.Entries = append(sh.Entries, it.Entry)
		}
		r, err := view.ShowSheet(e.UI, sh)
		if err != nil {
			return err
		}
		if r.Entry >= 0 && r.Entry < len(its) {
			cursor = r.Entry
		}
		switch r.Action {
		case view.Back:
			return nil
		case view.SheetSave:
			return errSave
		case view.SheetQuit:
			return errQuit
		case view.SheetHelp:
			if r.Entry >= 0 && r.Entry < len(its) {
				err = e.helpOf(its[r.Entry])
			}
		case view.Pick:
			if r.Entry < 0 || r.Entry >= len(its) {
				continue
			}
			switch it := its[r.Entry]; {
			case it.open != nil:
				err = it.open()
			case it.do != nil:
				err = it.do()
			}
		case view.SheetSet:
			for id, v := range r.Values {
				for _, it := range its {
					if it.ID == id && it.set != nil {
						if err := it.set(v); err != nil {
							e.say(view.Bad, capital(err.Error())+".")
						}
					}
				}
			}
		default:
			if p.key != nil {
				err = p.key(r.Action)
			}
		}
		if errors.Is(err, errClose) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// helpOf shows an item's whole help (F1), wrapped to the screen, and when a
// change to it takes effect.
func (e *Editor) helpOf(it item) error {
	help := it.help
	if help == "" {
		help = it.Help
	}
	if help == "" {
		help = "No help for this."
	}
	lines := wrap(help, e.width())
	if it.Note != "" {
		lines = append(lines, view.L(""), view.T(view.Dim, "Takes effect: "+it.Note))
	}
	return e.UI.Show(it.Label, lines)
}

// say puts a word at the foot of the next screen.
func (e *Editor) say(tone view.Tone, msg string) {
	e.flash = msg
	if _, ok := e.UI.(view.SheetUI); !ok {
		e.UI.Say(tone, msg)
		e.flash = ""
	}
}

// status is the word for a screen's foot: the flash said, once, or else how
// many changes are not saved ("" for none).
func (e *Editor) status() string {
	if e.flash != "" {
		s := e.flash
		e.flash = ""
		return s
	}
	switch n := e.changes(); n {
	case 0:
		return ""
	case 1:
		return "1 change not saved"
	default:
		return fmt.Sprintf("%d changes not saved", n)
	}
}

// changes counts what saving would do.
func (e *Editor) changes() int {
	n := len(confedit.Diff(e.orig, e.doc))
	if e.absent && n == 0 {
		n = 1 // the file itself
	}
	if e.db != nil {
		n += len(e.db.ops())
	}
	return n
}

// --- checking, changes, saving

// checkDoc: anything that would stop the BBS starting said at once.
func (e *Editor) checkDoc() {
	if e.Tools.Check == nil {
		return
	}
	if _, err := e.Tools.Check(e.doc.Bytes()); err != nil {
		e.say(view.Bad, "The BBS would not take this: "+err.Error()+".")
	}
}

// check shows the file as it will be written as the BBS would take it: why
// it would not start, or that it would, and its warnings.
func (e *Editor) check() error {
	if e.Tools.Check == nil {
		return nil
	}
	warnings, err := e.Tools.Check(e.doc.Bytes())
	var lines []view.Line
	if err != nil {
		lines = append(lines, view.T(view.Bad, "The BBS would not start: "+err.Error()))
	} else {
		lines = append(lines, view.T(view.Good, "As the BBS would take it."))
	}
	for _, w := range warnings {
		lines = append(lines, view.T(view.Warn, "Warning: "+w))
	}
	return e.UI.Show("Check", lines)
}

// review shows what saving would do.
func (e *Editor) review() error {
	lines := e.changeLines()
	if len(lines) == 0 {
		lines = []view.Line{view.L("Nothing has changed.")}
	}
	return e.UI.Show("Changes not saved", lines)
}

// changeLines are what saving would do, a line each: the file's changes
// (those at a restart marked) and then the database's.
func (e *Editor) changeLines() []view.Line {
	lines := e.diffLines(confedit.Diff(e.orig, e.doc))
	if e.db != nil {
		for _, o := range e.db.ops() {
			lines = append(lines, view.T(view.Good, o.desc))
		}
	}
	return lines
}

// save writes the file and makes the database's changes: true when it is
// done (the screens close), false when it could not be (they stay).
func (e *Editor) save() (bool, error) {
	ch := confedit.Diff(e.orig, e.doc)
	var ops []op
	if e.db != nil {
		ops = e.db.ops()
	}
	if len(ch) == 0 && !e.absent && len(ops) == 0 {
		return true, nil
	}
	data := e.doc.Bytes()
	var lines []view.Line
	if len(ch) > 0 || e.absent {
		if e.Tools.Check != nil {
			if _, err := e.Tools.Check(data); err != nil {
				e.say(view.Bad, "Not saved: the BBS would not start: "+err.Error()+".")
				return false, nil
			}
		}
		disk, err := os.ReadFile(e.Tools.Path)
		if e.absent && err == nil || !e.absent && (err != nil || !bytes.Equal(disk, e.read)) {
			e.say(view.Bad, "Not saved: "+e.Tools.Path+" has changed since it was read.  Quit, and run it again.")
			return false, nil
		}
		if err := confedit.WriteFile(e.Tools.Path, data, e.read, e.absent); err != nil {
			e.say(view.Bad, "Not saved: "+err.Error()+".")
			return false, nil
		}
		var cs []string
		for _, c := range ch {
			cs = append(cs, c.String())
		}
		e.log("configuration: %s written by %s: %s", e.Tools.Path, e.who(), strings.Join(cs, "; "))
		lines = append(lines, view.T(view.Good, "Written: "+e.Tools.Path+" (the last as "+filepath.Base(e.Tools.Path)+".bak)."))
		e.read, e.absent = data, false
	}
	for _, o := range ops {
		if err := o.run(); err != nil {
			lines = append(lines, view.T(view.Bad, o.desc+": "+err.Error()))
			continue
		}
		e.log("configuration: %s by %s", o.desc, e.who())
		lines = append(lines, view.T(view.Good, o.desc))
	}
	if len(ch) > 0 {
		if e.Tools.Reload == nil {
			lines = append(lines, view.L(""),
				view.L("A BBS running puts the green changes in force at a reload (systemctl reload"),
				view.L("bbs, or SIGHUP); the yellow ones when it starts again."), view.L(""))
			lines = append(lines, e.diffLines(ch)...)
		} else {
			applied, restart, err := e.Tools.Reload()
			if err != nil {
				lines = append(lines, view.T(view.Bad, "Not put in force: "+err.Error()))
			}
			if len(applied) > 0 {
				lines = append(lines, view.L(""), view.T(view.Bright, "In force now:"))
				for _, a := range applied {
					lines = append(lines, view.T(view.Good, "  "+a))
				}
			}
			if len(restart) > 0 {
				lines = append(lines, view.L(""), view.T(view.Bright, "When the BBS starts again:"))
				for _, r := range restart {
					lines = append(lines, view.T(view.Warn, "  "+r))
				}
			}
		}
	}
	return true, e.UI.Show("Saved", lines)
}

// diffLines are the file's changes ch, a line each, those that wait for a
// restart marked.
func (e *Editor) diffLines(ch []confedit.Change) []view.Line {
	var lines []view.Line
	for _, c := range ch {
		if c.Live() {
			lines = append(lines, view.T(view.Good, c.String()))
		} else {
			lines = append(lines, view.T(view.Warn, c.String()+"  (at a restart)"))
		}
	}
	return lines
}

// log logs by e.Log, if there is one.
func (e *Editor) log(format string, a ...any) {
	if e.Log != nil {
		e.Log(format, a...)
	}
}

// who is who edits, for the log: e.Who, or "the console".
func (e *Editor) who() string {
	if e.Who == "" {
		return "the console"
	}
	return e.Who
}

// --- words

// width is the columns help is wrapped to: 38 on a narrow screen, 74.
func (e *Editor) width() int {
	if e.UI.Narrow() {
		return 38
	}
	return 74
}

// firstSentence is s to the end of its first sentence (a full stop and two
// spaces), for an entry's one line of help; s whole if it has one.
func firstSentence(s string) string {
	if i := strings.Index(s, ".  "); i > 0 {
		return s[:i+1]
	}
	return s
}

// wrap breaks text into lines of at most width.
func wrap(text string, width int) []view.Line {
	var out []view.Line
	line := ""
	for _, w := range strings.Fields(text) {
		if line != "" && len(line)+1+len(w) > width {
			out = append(out, view.L(line))
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" {
		out = append(out, view.L(line))
	}
	return out
}

// capital is s with its first byte upper case, to make an error a sentence.
func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
