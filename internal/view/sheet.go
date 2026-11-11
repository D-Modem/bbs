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
	// Menus are the pull-down menus of a menu bar, where the front end
	// draws one (a text terminal's full screen): picking an item jumps
	// to its Path, or does its Action
	Menus []PullDown
}

// PullDown is a pull-down menu of a menu bar.
type PullDown struct {
	Label string
	Items []PullItem
}

// PullItem is a pull-down menu's line.
type PullItem struct {
	Label   string
	Path    []string // where it jumps: the labels of the entries to open, from the first sheet
	Action  string   // or what it does (SheetSave, SheetQuit), as a sheet's result
	Heading bool     // a line between groups (Label: its words, if any)
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
	ID      string
	Kind    EntryKind
	Label   string
	Value   string   // as it is now ("": not set)
	Default string   // shown, dim, when Value is ""
	Choices []string // EntryChoice's
	// ChoiceNotes are a word beside each of Choices, where it helps ("the
	// [acme] certificate", "level 20: Member"); shorter than Choices: none
	// for the rest
	ChoiceNotes []string
	// Open is an EntryChoice whose Choices are what the file has to offer,
	// a value of one's own typed as well
	Open bool
	// Multi is an EntryChoice of some of Choices at once, by commas
	Multi    bool
	Secret   bool   // shown as stars, typed unseen
	Changed  bool   // not saved yet: marked
	Note     string // at the line's end: "restart", "not available yet"
	Help     string // at the foot while the cursor is on it
	Disabled bool   // shown, not chosen
	Width    int    // the most a value may be (0: 255)
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
	Jump   []string          // SheetJump: a pull-down item's Path
}

// SheetJump is a sheet's result when a pull-down item was picked: go to
// its Path.
const SheetJump = "jump"

// SheetEnder is a front end that keeps its full screen up between sheets
// (to put questions and help over it): EndSheets puts it away when they
// are done.
type SheetEnder interface {
	EndSheets()
}

// Unhurried is a front end whose idle timeout can be held off, for the
// sysop's own screens (the configuration): HoldIdle lifts it, and the
// func it returns puts it back.  The login's hard limit still holds.
type Unhurried interface {
	HoldIdle() (restore func())
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

// ChoiceNote is the word beside the choice i ("" for none).
func (e Entry) ChoiceNote(i int) string {
	if i < len(e.ChoiceNotes) {
		return e.ChoiceNotes[i]
	}
	return ""
}

// Picked is the choices a Multi entry's value has: its comma-separated
// parts, trimmed.
func (e Entry) Picked() []string {
	var out []string
	for _, p := range strings.Split(e.Value, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Listed is whether an EntryChoice's choices are better picked from a
// list than turned over one at a time: Open or Multi ones, or more than
// four.
func (e Entry) Listed() bool {
	return e.Kind == EntryChoice && (e.Open || e.Multi || len(e.Choices) > 4)
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
			case EntryChoice:
				if !e.Listed() {
					return SheetResult{Action: SheetSet, Entry: i, Values: map[string]string{e.ID: e.Next()}}, nil
				}
				v, ok, err := PickChoice(ui, e)
				if err != nil {
					return SheetResult{}, err
				}
				if ok && v != e.Value {
					return SheetResult{Action: SheetSet, Entry: i, Values: map[string]string{e.ID: v}}, nil
				}
			case EntryBool:
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

// ChoiceLabel is how a choice reads: "" as "(default)".
func ChoiceLabel(c string) string {
	if c == "" {
		return "(default)"
	}
	return c
}

// PickChoice is an EntryChoice's value picked from a table of its choices
// (a number each, its note beside it, * by those it has now): for Multi
// ones a number turns one on or off until Done; for Open ones O types a
// value of one's own.  ok false: left as it was.
func PickChoice(ui UI, e Entry) (string, bool, error) {
	picked := map[string]bool{}
	if e.Multi {
		for _, p := range e.Picked() {
			picked[p] = true
		}
	}
	for {
		t := &Table{Title: e.Label, Pick: "Number",
			Columns: []Column{{Title: "", Width: 2}, {Title: "Value", Width: 28}, {Title: "", Width: 0, Wide: true}}}
		if e.Help != "" {
			t.Note = []Line{T(Dim, e.Help)}
		}
		if e.Value != "" && (e.Multi || !contains(e.Choices, e.Value)) {
			t.Note = append(t.Note, L("Now: "+e.Value))
		}
		for i, c := range e.Choices {
			on := c == e.Value || c == "" && e.Value == ""
			if e.Multi {
				on = picked[c]
			}
			mark := ""
			if on {
				mark = "*"
			}
			t.Rows = append(t.Rows, Row{Num: strconv.Itoa(i + 1), Cells: []string{mark, ChoiceLabel(c), e.ChoiceNote(i)}, Hot: on})
		}
		if e.Multi {
			t.Actions = append(t.Actions, Action{ID: "done", Label: "Done", Key: 'D', PF: 5})
		}
		if e.Open {
			t.Actions = append(t.Actions, Action{ID: "other", Label: "Other value", Key: 'O', PF: 4})
		}
		c, err := ui.Table(t)
		if err != nil {
			return "", false, err
		}
		switch c.Action {
		case Pick:
			if c.Row < 0 || c.Row >= len(e.Choices) {
				continue
			}
			v := e.Choices[c.Row]
			if !e.Multi {
				return v, true, nil
			}
			if v == "" {
				picked = map[string]bool{}
				continue
			}
			picked[v] = !picked[v]
		case "done":
			return JoinPicked(e, picked), true, nil
		case "other":
			f, ok, err := ui.Form(&Form{Title: e.Label, Fields: []Field{{Name: "v", Label: e.Label,
				Width: e.Width, Value: e.Value, Edit: true}}, Submit: "Set"})
			if err != nil || !ok {
				return "", false, err
			}
			return strings.TrimSpace(f["v"]), true, nil
		default:
			return "", false, nil
		}
	}
}

// JoinPicked is a Multi entry's value of the choices picked: in the order
// of its Choices, then any of its own it had, by commas.
func JoinPicked(e Entry, picked map[string]bool) string {
	var out []string
	for _, c := range e.Choices {
		if c != "" && picked[c] {
			out = append(out, c)
		}
	}
	for _, p := range e.Picked() {
		if picked[p] && !contains(e.Choices, p) {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

// contains is whether s is one of ss.
func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
