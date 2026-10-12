// Package view is what the BBS shows, described once and drawn by each
// front end its own way: a text terminal (prompts and keys, a page at a
// time, RIPscrip buttons where the terminal has them) and a 3270 (screens
// of fields and PF keys).
//
// A screen is a description (a Menu, a Table, a Message ...) handed to the
// front end's UI, which draws it, waits for the caller, and says what they
// chose.  The screens' logic is internal/app's; nothing here knows how
// anything is drawn.
package view

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Tone is how a line is shown.
type Tone int

// Tones.
const (
	Plain  Tone = iota
	Dim         // asides
	Bright      // what matters
	Good        // done
	Bad         // refused, failed
	Warn        // to note
)

// Line is a line of text, or a labelled value ("Name:  alice"), and a bar
// for a share of something (Bar < 0: none).
type Line struct {
	Label string
	Text  string
	Tone  Tone
	Bar   float64
}

// L is a plain line of text.
func L(text string) Line { return Line{Text: text, Bar: -1} }

// T is a line of text in a tone.
func T(tone Tone, text string) Line { return Line{Text: text, Tone: tone, Bar: -1} }

// V is a labelled value.
func V(label, text string) Line { return Line{Label: label, Text: text, Bar: -1} }

// Action is something a screen offers.  Key is a text terminal's key for
// it, PF a 3270's PF key (0: none, so not offered there).
type Action struct {
	ID    string
	Label string
	Key   rune
	PF    int
	Short string // a 3270's key line ("": Label)
}

// Actions the screens share.
const (
	Back = "back" // leave the screen (Q, Enter on an empty prompt, PF3)
	Pick = "pick" // a row chosen
	More = "more" // the rows after these (Enter on a text prompt, PF8)
	Less = "less" // the rows before (PF7)
	Cmd  = "cmd"  // a 3270's command line (IND$FILE)
)

// Item is a menu's choice.
type Item struct {
	Key   string // one character
	Label string
}

// Menu is a list of choices by key.
type Menu struct {
	Title  string
	Intro  []Line // shown before the items (a welcome)
	Items  []Item
	Prompt string // "" the default
	// a display file of the sysop's shown in place of the items
	Screen string
	// Brief: the prompt alone, the items when the caller asks (?): an
	// expert's menus
	Brief bool
}

// Column is a table's column.
type Column struct {
	Title string
	Width int  // 0: the rest of the line
	Right bool // numbers
	Wide  bool // only on screens of 60 columns or more
}

// Row is a table's row.
type Row struct {
	Num   string // what a text caller types to choose it; a 3270's caller types S by it
	Cells []string
	Mark  bool // new, unread
	Hot   bool // shown brighter (a sysop)
}

// Table is rows to read, and perhaps to choose from.
type Table struct {
	Title   string
	Note    []Line // under the title
	Columns []Column
	Rows    []Row
	Empty   string // shown when there are no rows
	// Pick says what choosing a row is for ("Area number", "Number to
	// read"); "" rows are not chosen
	Pick    string
	Prompt  string // a text terminal's whole prompt, in place of the one made
	Actions []Action
	More    bool   // there are rows after these (the screen's code fetches them)
	Less    bool   // and before
	Command string // a 3270's command line, with this hint above it ("": none)
}

// Choice is what the caller did on a table.
type Choice struct {
	Action string // an Action's ID, Pick, More, Less, Back or Cmd
	Row    int    // the row picked (-1: a number typed that is no row here: Num)
	Num    string
	Text   string // the command line's
}

// Message is a message to read.
type Message struct {
	Where   string // "General  #12", "Mail 1 of 3"
	From    string
	To      string
	Subject string
	Date    time.Time
	Body    string
	Actions []Action
	Default string // the action Enter takes (Next)
	Quit    string // the action Q and PF3 take ("": Back)
}

// Field is a form's field.
type Field struct {
	Name   string
	Label  string // a 3270's label
	Prompt string // a text terminal's question ("": Label and a colon)
	Width  int
	Secret bool
	Value  string // as it is: shown, and kept if nothing is typed
}

// Form is fields to fill.
type Form struct {
	Title  string
	Intro  []Line
	Fields []Field
	Submit string // what Enter does on a 3270 ("Save")
}

// Panel is facts, and things to do.
type Panel struct {
	Title   string
	Lines   []Line
	Actions []Action
}

// Draft is a message being written: To and Subject asked, the body edited,
// then Send called (its error shown).
type Draft struct {
	Title   string
	To      string
	ToHint  string             // what To may be ("user name, or Name@zone:net/node")
	CheckTo func(string) error // To checked at once on a text terminal (nil: by Send)
	Subject string
	Body    []string
	// Quote is what may be quoted (a reply's): a full-screen editor offers
	// it in a window, another begins the body with it
	Quote []string
	Send  func(to, subject string, body []string) error
}

// UI is a front end's drawing of the screens.
type UI interface {
	// PageSize is how many rows a Table shows at once.
	PageSize() int
	// Narrow is a screen under 60 columns.
	Narrow() bool
	// Menu returns the key chosen, or Back (a 3270's PF3).
	Menu(m *Menu) (string, error)
	Table(t *Table) (Choice, error)
	Message(m *Message) (string, error)
	// Form returns the values, or ok false if the caller gave up.
	Form(f *Form) (map[string]string, bool, error)
	Panel(p *Panel) (string, error)
	// Edit writes a Draft; true if it was sent.
	Edit(d *Draft) (bool, error)
	Confirm(question string, def bool) (bool, error)
	// Say tells the caller something (at once, or with the next screen).
	Say(tone Tone, msg string)
	// Show shows lines until the caller has read them.
	Show(title string, lines []Line) error
}

// Layout fits a table's columns to room characters: the indexes of the
// columns shown (those for wide screens only when not narrow) and their
// widths, the widest giving way when they are too many.
func Layout(cols []Column, narrow bool, room int) (idx, widths []int) {
	fixed, rest := 0, -1
	for i, c := range cols {
		if c.Wide && narrow {
			continue
		}
		idx = append(idx, i)
		widths = append(widths, c.Width)
		if c.Width == 0 {
			rest = len(widths) - 1
		} else {
			fixed += c.Width + 1
		}
	}
	for fixed > room {
		w := 0
		for j := range widths {
			if widths[j] > widths[w] {
				w = j
			}
		}
		if widths[w] <= 4 {
			break
		}
		widths[w]--
		fixed--
	}
	if rest >= 0 {
		widths[rest] = max(room-fixed, 4)
	}
	return idx, widths
}

// Cells are a row's cells (or the columns' titles) laid out by Layout.
func Cells(cols []Column, idx, widths []int, cs []string) string {
	var b strings.Builder
	for j, i := range idx {
		if j > 0 {
			b.WriteByte(' ')
		}
		v := ""
		if i < len(cs) {
			v = cs[i]
		}
		if r := []rune(v); len(r) > widths[j] {
			v = string(r[:widths[j]])
		}
		pad := strings.Repeat(" ", max(widths[j]-utf8.RuneCountInString(v), 0))
		switch {
		case cols[i].Right:
			b.WriteString(pad + v)
		case j == len(idx)-1:
			b.WriteString(v)
		default:
			b.WriteString(v + pad)
		}
	}
	return b.String()
}

// Titles are the columns' titles, for Cells.
func Titles(cols []Column) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Title
	}
	return out
}
