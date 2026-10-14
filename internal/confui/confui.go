// Package confui is the configuration screens: bbs.conf's sections and
// keys, each with the sample file's help and when it takes effect,
// changed, checked as the BBS would read them, and written back with
// every comment kept.  The same screens run on the console (bbs config,
// with the BBS stopped or not) and on the sysop's menu, where what may
// change while callers are on is put in force at once.
package confui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	bbssec "github.com/Neo-Desktop/bbs-sec"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/confedit"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// Editor is the screens, on a front end's UI.
type Editor struct {
	UI    view.UI
	Tools bbs.ConfigTools
	Who   string                        // who edits, for the log ("": the console)
	Log   func(format string, a ...any) // nil: none

	cat    *confedit.Catalog
	read   []byte // the file as read (absent: none)
	absent bool   // there was no file: it is begun from the sample
	orig   *confedit.Doc
	doc    *confedit.Doc
}

// Run shows the screens until the sysop leaves them.
func (e *Editor) Run() error {
	e.cat = confedit.NewCatalog(bbssec.Sample)
	if ok, err := e.load(); err != nil || !ok {
		return err
	}
	for {
		n := len(confedit.Diff(e.orig, e.doc))
		changes := "View changes"
		if n > 0 {
			changes += fmt.Sprintf(" (%d)", n)
		}
		write := "Write"
		if e.Tools.Reload != nil {
			write = "Write and put in force"
		}
		k, err := e.UI.Menu(&view.Menu{Title: "Configuration: " + e.Tools.Path, Prompt: "Configuration: ",
			Items: []view.Item{{Key: "S", Label: "Sections"}, {Key: "N", Label: "New section"},
				{Key: "C", Label: "Check"}, {Key: "V", Label: changes}, {Key: "W", Label: write},
				{Key: "U", Label: "Undo all changes"}, {Key: "Q", Label: "Quit"}}})
		if err != nil {
			return err
		}
		switch strings.ToUpper(k) {
		case "S":
			err = e.sections()
		case "N":
			err = e.newSection()
		case "C":
			err = e.check()
		case "V":
			err = e.changes()
		case "W":
			err = e.write()
		case "U":
			if n == 0 {
				e.UI.Say(view.Plain, "Nothing has changed.")
				continue
			}
			if yes, err := e.UI.Confirm(fmt.Sprintf("Forget %d changes and read the file again?", n), false); err != nil || !yes {
				if err != nil {
					return err
				}
				continue
			}
			if ok, err := e.load(); err != nil || !ok {
				return err
			}
		case "Q", view.Back:
			if n == 0 {
				return nil
			}
			if yes, err := e.UI.Confirm(fmt.Sprintf("Leave without writing %d changes?", n), false); err != nil || yes {
				return err
			}
		}
		if err != nil {
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

// --- sections

func (e *Editor) sections() error {
	for {
		secs := e.doc.Sections()
		t := &view.Table{Title: "Sections", Pick: "Number to see",
			Columns: []view.Column{{Title: "Section", Width: 24}, {Title: "Keys", Width: 4, Right: true},
				{Title: "What it is", Wide: true}},
			Empty:   "No sections: the defaults.",
			Actions: []view.Action{{ID: "new", Label: "New section", Key: 'N', PF: 5, Short: "New"}}}
		for i, s := range secs {
			help := ""
			if k := e.cat.Kind(s.Kind); k != nil {
				help = firstSentence(k.Help)
			}
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1),
				Cells: []string{s.String(), strconv.Itoa(len(e.doc.Keys(s))), help}})
		}
		c, err := e.UI.Table(t)
		if err != nil || c.Action == view.Back {
			return err
		}
		switch {
		case c.Action == "new":
			err = e.newSection()
		case c.Action == view.Pick && c.Row >= 0:
			err = e.section(secs[c.Row])
		}
		if err != nil {
			return err
		}
	}
}

func (e *Editor) newSection() error {
	t := &view.Table{Title: "New section", Pick: "Number of the kind",
		Columns: []view.Column{{Title: "Kind", Width: 9}, {Title: "Many", Width: 4},
			{Title: "What it is", Width: 0}}}
	kinds := e.cat.Kinds
	for i, k := range kinds {
		many := ""
		if k.Named {
			many = "yes"
		}
		t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Cells: []string{k.Name, many, firstSentence(k.Help)}})
	}
	c, err := e.UI.Table(t)
	if err != nil || c.Action != view.Pick || c.Row < 0 {
		return err
	}
	k := kinds[c.Row]
	s := confedit.Section{Kind: k.Name}
	if k.Named {
		v, ok, err := e.UI.Form(&view.Form{Title: "New [" + k.Name + "]",
			Intro:  []view.Line{view.L("Its name (one word): [" + k.Name + " NAME]")},
			Fields: []view.Field{{Name: "name", Label: "Name", Width: 32}}, Submit: "Make"})
		if err != nil || !ok || strings.TrimSpace(v["name"]) == "" {
			return err
		}
		s.Name = strings.TrimSpace(v["name"])
	}
	if e.doc.Has(s) {
		e.UI.Say(view.Plain, s.String()+" is there already.")
	} else if err := e.doc.AddSection(s); err != nil {
		e.UI.Say(view.Bad, capital(err.Error())+".")
		return nil
	} else {
		e.UI.Say(view.Good, s.String()+" made.")
	}
	return e.section(s)
}

// section is a section's keys: those set, then those it may have.
func (e *Editor) section(s confedit.Section) error {
	for {
		if !e.doc.Has(s) {
			return nil
		}
		kind := e.cat.Kind(s.Kind)
		set := e.doc.Keys(s)
		keys := append([]string(nil), set...)
		isSet := map[string]bool{}
		for _, k := range set {
			isSet[k] = true
		}
		if kind != nil {
			for _, k := range kind.Keys {
				if !isSet[k.Name] {
					keys = append(keys, k.Name)
				}
			}
		}
		t := &view.Table{Title: s.String(), Pick: "Number to see or change",
			Columns: []view.Column{{Title: "Key", Width: 18}, {Title: "Value", Width: 0},
				{Title: "Takes effect", Width: 12, Wide: true}},
			Actions: []view.Action{{ID: "add", Label: "Add a key", Key: 'A', PF: 5, Short: "Add"},
				{ID: "help", Label: "Help", Key: 'H', PF: 1},
				{ID: "del", Label: "Delete section", Key: 'D', PF: 9, Short: "Delete"}}}
		if kind != nil && kind.Named {
			t.Actions = append(t.Actions, view.Action{ID: "rename", Label: "Rename", Key: 'R', PF: 10})
		}
		for i, k := range keys {
			v := "-"
			if isSet[k] {
				v, _ = e.doc.Get(s, k)
				v = mask(k, v)
			} else if x := e.cat.Key(s.Kind, k); x != nil && x.Default != "" {
				v = "- (" + x.Default + ")"
			}
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Hot: isSet[k],
				Cells: []string{k, v, when(s.Kind, k)}})
		}
		c, err := e.UI.Table(t)
		if err != nil || c.Action == view.Back {
			return err
		}
		switch c.Action {
		case view.Pick:
			if c.Row >= 0 {
				err = e.key(s, keys[c.Row])
			}
		case "add":
			err = e.addKey(s)
		case "help":
			help := "A section of its own: the BBS does not know it."
			if kind != nil {
				help = kind.Help
				if kind.Free != "" {
					help += "  Its own keys: " + kind.Free + "."
				}
			}
			err = e.UI.Show(s.String(), wrap(help, e.width()))
		case "rename":
			v, ok, err := e.UI.Form(&view.Form{Title: "Rename " + s.String(),
				Fields: []view.Field{{Name: "name", Label: "New name", Width: 32, Value: s.Name, Edit: true}}, Submit: "Rename"})
			if err != nil {
				return err
			}
			if ok && v["name"] != s.Name {
				if err := e.doc.Rename(s, strings.TrimSpace(v["name"])); err != nil {
					e.UI.Say(view.Bad, capital(err.Error())+".")
				} else {
					s.Name = strings.TrimSpace(v["name"])
					e.changed()
				}
			}
		case "del":
			yes, err := e.UI.Confirm("Delete "+s.String()+"? (It is commented out, to be had back.)", false)
			if err != nil {
				return err
			}
			if yes {
				e.doc.RemoveSection(s)
				e.changed()
				return nil
			}
		}
		if err != nil {
			return err
		}
	}
}

func (e *Editor) addKey(s confedit.Section) error {
	v, ok, err := e.UI.Form(&view.Form{Title: "A key in " + s.String(),
		Fields: []view.Field{{Name: "key", Label: "Key", Width: 32}, {Name: "value", Label: "Value", Width: 60, Edit: true}},
		Submit: "Add"})
	if err != nil || !ok {
		return err
	}
	key := strings.ToLower(strings.TrimSpace(v["key"]))
	if key == "" {
		return nil
	}
	if e.cat.Kind(s.Kind) != nil && e.cat.Key(s.Kind, key) == nil {
		yes, err := e.UI.Confirm(key+" is not a key "+s.String()+" has: add it anyway?", false)
		if err != nil || !yes {
			return err
		}
	}
	if err := e.doc.Set(s, key, v["value"]); err != nil {
		e.UI.Say(view.Bad, capital(err.Error())+".")
		return nil
	}
	e.changed()
	return nil
}

// key is a key: its help, value, and what it may be.
func (e *Editor) key(s confedit.Section, key string) error {
	x := e.cat.Key(s.Kind, key)
	for {
		v, isSet := e.doc.Get(s, key)
		var lines []view.Line
		if isSet {
			lines = append(lines, view.V("Value:", mask(key, v)))
		} else {
			lines = append(lines, view.V("Value:", "not set"))
		}
		var acts []view.Action
		acts = append(acts, view.Action{ID: "edit", Label: "Edit", Key: 'E', PF: 5})
		if isSet {
			acts = append(acts, view.Action{ID: "unset", Label: "Unset: its default", Key: 'U', PF: 6, Short: "Unset"})
		}
		if x != nil {
			if x.Default != "" {
				lines = append(lines, view.V("Default:", x.Default))
			}
			if len(x.Examples) > 0 {
				lines = append(lines, view.V("Examples:", strings.Join(quoted(x.Examples), ", ")))
				acts = append(acts, view.Action{ID: "example", Label: "Take an example", Key: 'T', PF: 9, Short: "Example"})
			}
		}
		lines = append(lines, view.V("Takes effect:", effect(s.Kind, key, e.Tools.Reload != nil)), view.L(""))
		switch {
		case x != nil && x.Help != "":
			lines = append(lines, wrap(x.Help, e.width())...)
		case x != nil:
			if k := e.cat.Kind(s.Kind); k != nil {
				lines = append(lines, wrap("(No help of its own.)  "+k.Help, e.width())...)
			}
		default:
			lines = append(lines, view.T(view.Warn, "The BBS does not know this key: a mistake?"))
		}
		act, err := e.UI.Panel(&view.Panel{Title: s.String() + " " + key, Lines: lines, Actions: acts})
		if err != nil || act == view.Back || act == "" {
			return err
		}
		switch act {
		case "edit":
			f, ok, err := e.UI.Form(&view.Form{Title: s.String() + " " + key,
				Fields: []view.Field{{Name: "v", Label: key, Width: 72, Value: v, Edit: true, Secret: confedit.Secret(key)}},
				Submit: "Set"})
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			nv := f["v"]
			if nv == "" && isSet {
				e.doc.Unset(s, key)
				e.UI.Say(view.Plain, key+": unset, its default holds.")
			} else if nv != v || !isSet {
				if err := e.doc.Set(s, key, nv); err != nil {
					e.UI.Say(view.Bad, capital(err.Error())+".")
					continue
				}
			}
			e.changed()
		case "unset":
			e.doc.Unset(s, key)
			e.changed()
		case "example":
			ex := x.Examples
			if len(ex) > 1 {
				t := &view.Table{Title: key + ": examples", Pick: "Number to take",
					Columns: []view.Column{{Title: "Value"}}}
				for i, v := range ex {
					t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Cells: []string{v}})
				}
				c, err := e.UI.Table(t)
				if err != nil {
					return err
				}
				if c.Action != view.Pick || c.Row < 0 {
					continue
				}
				ex = ex[c.Row:]
			}
			if err := e.doc.Set(s, key, ex[0]); err != nil {
				e.UI.Say(view.Bad, capital(err.Error())+".")
				continue
			}
			e.changed()
		}
	}
}

// changed: the file as it now is is checked, and anything that would stop
// the BBS starting said at once.
func (e *Editor) changed() {
	if e.Tools.Check == nil {
		return
	}
	if _, err := e.Tools.Check(e.doc.Bytes()); err != nil {
		e.UI.Say(view.Bad, "The BBS would not take this: "+err.Error()+".")
	}
}

// --- checking, changes, writing

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

func (e *Editor) changes() error {
	ch := confedit.Diff(e.orig, e.doc)
	if len(ch) == 0 {
		e.UI.Say(view.Plain, "Nothing has changed.")
		return nil
	}
	return e.UI.Show("Changes", e.changeLines(ch))
}

func (e *Editor) changeLines(ch []confedit.Change) []view.Line {
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

func (e *Editor) write() error {
	ch := confedit.Diff(e.orig, e.doc)
	if len(ch) == 0 && !e.absent {
		e.UI.Say(view.Plain, "Nothing has changed.")
		return nil
	}
	data := e.doc.Bytes()
	if e.Tools.Check != nil {
		if _, err := e.Tools.Check(data); err != nil {
			e.UI.Say(view.Bad, "Not written: the BBS would not start: "+err.Error()+".")
			return nil
		}
	}
	disk, err := os.ReadFile(e.Tools.Path)
	if e.absent && err == nil || !e.absent && (err != nil || !bytes.Equal(disk, e.read)) {
		e.UI.Say(view.Bad, "Not written: "+e.Tools.Path+" has changed since it was read.  Undo (U) reads it again.")
		return nil
	}
	if err := confedit.WriteFile(e.Tools.Path, data, e.read, e.absent); err != nil {
		e.UI.Say(view.Bad, "Not written: "+err.Error()+".")
		return nil
	}
	if e.Log != nil {
		who := e.Who
		if who == "" {
			who = "the console"
		}
		var cs []string
		for _, c := range ch {
			cs = append(cs, c.String())
		}
		e.Log("configuration: %s written by %s: %s", e.Tools.Path, who, strings.Join(cs, "; "))
	}
	e.read, e.absent = data, false
	e.orig, _ = confedit.Parse(data)
	lines := []view.Line{view.T(view.Good, "Written ("+e.Tools.Path+"; the last as "+filepath.Base(e.Tools.Path)+".bak).")}
	if e.Tools.Reload == nil {
		lines = append(lines, view.L(""),
			view.L("A BBS running takes the changes marked live at a reload (systemctl reload bbs,"),
			view.L("or SIGHUP); the rest when it starts again."), view.L(""))
		lines = append(lines, e.changeLines(ch)...)
		return e.UI.Show("Written", lines)
	}
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
	return e.UI.Show("Written", lines)
}

// --- words

func (e *Editor) width() int {
	if e.UI.Narrow() {
		return 38
	}
	return 74
}

func when(kind, key string) string {
	if confedit.Live(kind, key) {
		return "live"
	}
	return "restart"
}

func effect(kind, key string, online bool) string {
	switch {
	case !confedit.Live(kind, key):
		return "when the BBS starts again"
	case online:
		return "at once, when written"
	}
	return "when written, at a reload (SIGHUP)"
}

func mask(key, v string) string {
	if confedit.Secret(key) && v != "" {
		return "********"
	}
	return v
}

func quoted(vs []string) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = confedit.Quote(v)
		if out[i] == "" {
			out[i] = `""`
		}
	}
	return out
}

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

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
