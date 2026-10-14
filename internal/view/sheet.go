package view

import (
	"strconv"
	"strings"
)

// Sheet is a full screen of entries to move among: menus to open, values
// to change, things to do.  Changes are the screen's code's to keep; F10
// (PF10) asks it to save them all and leave, F9 (PF9) to leave without.
type Sheet struct {
	Title   string
	Crumbs  string // where it is: "Configuration > Services"
	Note    []Line // above the entries
	Entries []Entry
	Cursor  int      // the entry the cursor begins on
	Keys    []Action // more keys (A Add, D Delete ...), with PF keys on a 3270
	Status  string   // the foot's left: "3 changes not saved"
}

// EntryKind is what an entry is.
type EntryKind int

// Entry kinds.
const (
	EntryMenu    EntryKind = iota // opens another sheet
	EntryText                     // a value typed
	EntryBool                     // yes or no: Enter or space turns it over
	EntryChoice                   // one of Choices: Enter or space goes to the next
	EntryAction                   // does something
	EntryInfo                     // a value only shown
	EntryHeading                  // a line between groups of entries
)

// Entry is a sheet's line.
type Entry struct {
	ID       string
	Kind     EntryKind
	Label    string
	Value    string   // as it is now ("": not set)
	Default  string   // shown, dim, when Value is ""
	Choices  []string // EntryChoice's
	Secret   bool     // shown as stars, typed unseen
	Changed  bool     // not saved yet: marked
	Note     string   // at the line's end: "restart", "not available yet"
	Help     string   // at the foot while the cursor is on it
	Disabled bool     // shown, not chosen
	Width    int      // the most a value may be (0: 255)
}

// Sheet results' actions beyond Pick and Back, and the keys' own IDs.
const (
	SheetSet  = "set"  // Values were changed
	SheetSave = "save" // F10: save all and leave
	SheetQuit = "quit" // F9: leave without saving
	SheetHelp = "help" // F1 (or ?): the entry's whole help
)

// SheetResult is what was done on a sheet.
type SheetResult struct {
	Action string            // Pick (a Menu or Action entry chosen), SheetSet, SheetSave, SheetQuit, Back, a Keys ID
	Entry  int               // the entry the cursor was on
	Values map[string]string // SheetSet: entries' new values by ID (a 3270 sends all it changed at once)
}

// SheetUI is a front end that draws sheets full-screen.
type SheetUI interface {
	Sheet(s *Sheet) (SheetResult, error)
}

// ShowSheet draws a sheet: full-screen where the front end can, else as a
// numbered table, a value at a time.
func ShowSheet(ui UI, s *Sheet) (SheetResult, error) {
	if su, ok := ui.(SheetUI); ok {
		return su.Sheet(s)
	}
	return LineSheet(ui, s)
}

// Shown is how an entry's value reads: the value, stars for a secret,
// its default in brackets, yes or no.
func (e Entry) Shown() string {
	switch {
	case e.Kind == EntryAction || e.Kind == EntryHeading:
		return ""
	case e.Kind == EntryMenu:
		return e.Value // what is in it, in a word
	case e.Value == "" && e.Default != "":
		return "(" + e.Default + ")"
	case e.Secret && e.Value != "":
		return "********"
	}
	return e.Value
}

// Next is the value an EntryBool or EntryChoice turns to.
func (e Entry) Next() string {
	cur := e.Value
	if cur == "" {
		cur = e.Default
	}
	if e.Kind == EntryBool {
		if strings.EqualFold(cur, "yes") {
			return "no"
		}
		return "yes"
	}
	for i, c := range e.Choices {
		if c == cur {
			return e.Choices[(i+1)%len(e.Choices)]
		}
	}
	if len(e.Choices) > 0 {
		return e.Choices[0]
	}
	return cur
}

// Choosable reports whether the cursor may rest on the entry.
func (e Entry) Choosable() bool {
	return !e.Disabled && e.Kind != EntryHeading && e.Kind != EntryInfo
}

// LineSheet is a sheet on a terminal that cannot draw one: a numbered
// table, a number typed to open or change an entry, S to save and leave,
// X to leave without.
func LineSheet(ui UI, s *Sheet) (SheetResult, error) {
	t := &Table{Title: s.Title, Note: s.Note, Pick: "Number",
		Columns: []Column{{Title: "", Width: 26}, {Title: "", Width: 0}, {Title: "", Width: 10, Wide: true}}}
	if s.Crumbs != "" {
		t.Note = append([]Line{T(Dim, s.Crumbs)}, t.Note...)
	}
	if s.Status != "" {
		t.Note = append(t.Note, T(Warn, s.Status))
	}
	var rows []int // the entry of each row (-1: a heading)
	n := 0
	for i, e := range s.Entries {
		if e.Kind == EntryHeading {
			h := ""
			if e.Label != "" {
				h = "-- " + e.Label
			}
			t.Rows = append(t.Rows, Row{Cells: []string{h, "", ""}})
			rows = append(rows, -1)
			continue
		}
		num := ""
		if e.Choosable() {
			n++
			num = strconv.Itoa(n)
		}
		label := e.Label
		if e.Kind == EntryMenu {
			label += " >"
		}
		v := e.Shown()
		if e.Changed {
			v += " *"
		}
		t.Rows = append(t.Rows, Row{Num: num, Cells: []string{label, v, e.Note}, Hot: e.Changed})
		rows = append(rows, i)
	}
	t.Actions = append(append([]Action{}, s.Keys...),
		Action{ID: SheetSave, Label: "Save and exit", Key: 'S', PF: 10},
		Action{ID: SheetQuit, Label: "Exit without saving", Key: 'X', PF: 9},
		Action{ID: SheetHelp, Label: "Help", Key: '?', PF: 1})
	for {
		c, err := ui.Table(t)
		if err != nil {
			return SheetResult{}, err
		}
		switch c.Action {
		case Back, SheetSave, SheetQuit:
			return SheetResult{Action: c.Action}, nil
		case SheetHelp:
			// of which?
			v, ok, err := ui.Form(&Form{Title: "Help", Fields: []Field{{Name: "n", Label: "Help on number", Width: 4}}})
			if err != nil || !ok {
				return SheetResult{}, err
			}
			for j, r := range t.Rows {
				if r.Num == strings.TrimSpace(v["n"]) && rows[j] >= 0 {
					return SheetResult{Action: SheetHelp, Entry: rows[j]}, nil
				}
			}
		case Pick:
			if c.Row < 0 || rows[c.Row] < 0 || !s.Entries[rows[c.Row]].Choosable() {
				continue
			}
			i := rows[c.Row]
			e := s.Entries[i]
			switch e.Kind {
			case EntryMenu, EntryAction:
				return SheetResult{Action: Pick, Entry: i}, nil
			case EntryBool, EntryChoice:
				return SheetResult{Action: SheetSet, Entry: i, Values: map[string]string{e.ID: e.Next()}}, nil
			case EntryText:
				if e.Help != "" {
					ui.Say(Dim, e.Help)
				}
				v, ok, err := ui.Form(&Form{Title: e.Label, Fields: []Field{{Name: "v", Label: e.Label,
					Width: e.Width, Value: e.Value, Edit: true, Secret: e.Secret}}, Submit: "Set"})
				if err != nil {
					return SheetResult{}, err
				}
				if ok && v["v"] != e.Value {
					return SheetResult{Action: SheetSet, Entry: i, Values: map[string]string{e.ID: v["v"]}}, nil
				}
			}
		default:
			return SheetResult{Action: c.Action, Entry: -1}, nil
		}
	}
}
