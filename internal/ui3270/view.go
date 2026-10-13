package ui3270

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/screens"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// The shared screens (internal/app) drawn on a 3270: screens of fields,
// PF keys to act.

var toneColors = map[view.Tone]go3270.Color{view.Plain: go3270.Green, view.Dim: go3270.Blue,
	view.Bright: go3270.White, view.Good: go3270.Green, view.Bad: go3270.Red, view.Warn: go3270.Yellow}

var pfAIDs = map[go3270.AID]int{go3270.AIDPF1: 1, go3270.AIDPF2: 2, go3270.AIDPF3: 3, go3270.AIDPF4: 4,
	go3270.AIDPF5: 5, go3270.AIDPF6: 6, go3270.AIDPF7: 7, go3270.AIDPF8: 8, go3270.AIDPF9: 9,
	go3270.AIDPF10: 10, go3270.AIDPF11: 11, go3270.AIDPF12: 12}

// pfAction is the action of a PF key ("": none).
func pfAction(acts []view.Action, aid go3270.AID) string {
	n, ok := pfAIDs[aid]
	if !ok {
		return ""
	}
	for _, a := range acts {
		if a.PF == n {
			return a.ID
		}
	}
	return ""
}

// pfKeys is the key line's part for actions: "PF5=Read new  PF6=Post".
func pfKeys(acts []view.Action, sep string) string {
	var parts []string
	for _, a := range acts {
		if a.PF == 0 {
			continue
		}
		l := a.Short
		if l == "" {
			l = a.Label
		}
		parts = append(parts, fmt.Sprintf("PF%d=%s", a.PF, l))
	}
	return strings.Join(parts, sep)
}

// wrapped is a line's text as rows of a screen width wide.
func wrapped(l view.Line, width int) []string {
	text := l.Text
	indent := ""
	if l.Label != "" {
		indent = strings.Repeat(" ", 14)
		text = fmt.Sprintf("%-13s %s", l.Label, l.Text)
	}
	var out []string
	for {
		r := []rune(text)
		if len(r) <= width {
			return append(out, text)
		}
		cut := width
		if i := strings.LastIndex(string(r[:width]), " "); i > len(indent) {
			cut = len([]rune(string(r[:width])[:i]))
		}
		out = append(out, string(r[:cut]))
		text = indent + strings.TrimLeft(string(r[cut:]), " ")
	}
}

// viewLine is a view's line at row as fields, and the rows it took.
func (u *ui) viewLine(row int, l view.Line) (go3270.Screen, int) {
	if l.Bar >= 0 {
		width := u.cols - 32
		return go3270.Screen{
			u.text(row, 2, fmt.Sprintf("%s %5s", bbs.Fit(l.Label, 20), l.Text), go3270.Green, false),
			u.text(row, 29, u.bar(int(l.Bar*1000+0.5), 1000, width), go3270.Turquoise, false),
		}, 1
	}
	var out go3270.Screen
	rows := wrapped(l, u.cols-4)
	for i, t := range rows {
		out = append(out, u.text(row+i, 2, t, toneColors[l.Tone], l.Tone == view.Bright))
	}
	return out, len(rows)
}

func (u *ui) PageSize() int { return u.perPage() }
func (u *ui) Narrow() bool  { return false }

func (u *ui) Say(tn view.Tone, msg string) {
	if tn == view.Bad {
		u.sayBad = true
	}
	if u.say != "" {
		u.say += "  "
	}
	u.say += msg
}

func (u *ui) Menu(m *view.Menu) (string, error) {
	msg := ""
	for {
		scr := u.frame(screens.Unmark(m.Title), "Enter=Select  PF3=Back", msg)
		msg = ""
		row := 2
		for _, l := range m.Intro {
			if row >= u.rows-5-len(m.Items)-2 {
				break
			}
			fs, n := u.viewLine(row, l)
			scr = append(scr, fs...)
			row += n
		}
		if len(m.Intro) > 0 {
			row++
		}
		var pages [][]string
		var sc *screens.Screen
		shown := false
		if m.Screen != "" {
			pages, sc, shown = u.screenPages(m.Screen, u.rows-4-row)
		}
		if shown && len(pages) > 0 {
			for j, l := range pages[0] {
				scr = append(scr, u.lineFields(sc, row+j, l)...)
			}
		} else {
			if row+len(m.Items)+2 > u.rows-4 {
				row = 2
				scr = scr[:3] // no room for the intro
			}
			scr = append(scr, u.boxed(row, 2, row+len(m.Items)+1, 44, go3270.Blue)...)
			for i, it := range m.Items {
				scr = append(scr, u.text(row+1+i, 4, it.Key, go3270.Yellow, true))
				scr = append(scr, u.spanFields(row+1+i, 7, 44, it.Label, go3270.Green)...) // its tags' colors
			}
		}
		prompt := "Option ===>"
		if m.Prompt != "" {
			prompt = bbs.Trunc(screens.Unmark(m.Prompt), 20) // filled in already, tags marked for a text terminal
		}
		scr = append(scr, u.text(u.rows-3, 2, prompt, go3270.Turquoise, false))
		scr = append(scr, u.input(u.rows-3, 4+len([]rune(prompt)), 2, "opt", false)...)
		resp, v, err := u.show(scr, nil)
		if err != nil {
			return "", err
		}
		switch resp.AID {
		case go3270.AIDPF3:
			return view.Back, nil
		case go3270.AIDEnter:
		default:
			continue
		}
		key := strings.TrimSpace(v["opt"])
		if key == "" {
			continue
		}
		for _, it := range m.Items {
			if strings.EqualFold(it.Key, key) {
				return it.Key, nil
			}
		}
		msg = "No such option."
	}
}

func (u *ui) Table(t *view.Table) (view.Choice, error) {
	per := u.perPage()
	if t.Command != "" {
		per -= 2
	}
	pick := t.Pick != ""
	keyLine := func(sel, sep string) string {
		keys := []string{}
		if pick {
			keys = append(keys, sel)
		}
		if k := pfKeys(t.Actions, sep); k != "" {
			keys = append(keys, k)
		}
		return strings.Join(append(keys, "PF3=Back", "PF7/8=Page"), sep)
	}
	keys := keyLine("S+Enter=Select", "  ")
	if len(keys) > u.cols-1 {
		keys = keyLine("S=Select", " ") // many actions: closer
	}
	numW := 0
	mark := false
	for _, r := range t.Rows {
		numW = max(numW, len(r.Num))
		mark = mark || r.Mark
	}
	lead := 2
	if mark {
		lead++
	}
	if numW > 0 {
		lead += numW + 1
	}
	idx, widths := view.Layout(t.Columns, false, u.cols-2-lead)
	top := 0
	msg := ""
	for {
		scr := u.frame(t.Title, keys, msg)
		msg = ""
		if t.Command != "" {
			// the command line first: the cursor goes to it, where an
			// emulator types IND$FILE
			scr = append(scr, u.text(u.rows-3, 0, "Command ===>", go3270.Turquoise, false))
			scr = append(scr, u.input(u.rows-3, 13, u.cols-15, "cmd", false)...)
			scr = append(scr, u.text(u.rows-4, 0, t.Command, go3270.Blue, false))
		}
		if len(t.Note) > 0 {
			scr = append(scr, u.text(1, 2, t.Note[0].Text, toneColors[t.Note[0].Tone], false))
		}
		if len(t.Columns) > 0 {
			scr = append(scr, u.text(2, 0, strings.Repeat(" ", lead)+view.Cells(t.Columns, idx, widths, view.Titles(t.Columns)),
				go3270.Turquoise, false))
		}
		for i := 0; i < per && top+i < len(t.Rows); i++ {
			r := t.Rows[top+i]
			if pick {
				scr = append(scr, go3270.Field{Row: 3 + i, Col: 0, Write: true, Name: "s" + strconv.Itoa(i),
					Color: go3270.Green, Highlighting: go3270.Underscore})
			}
			line := ""
			if mark {
				line = map[bool]string{true: "*", false: " "}[r.Mark]
			}
			if numW > 0 {
				line += fmt.Sprintf("%*s ", numW, r.Num)
			}
			color := go3270.Green
			if r.Hot {
				color = go3270.White
			}
			scr = append(scr, u.text(3+i, 2, line+view.Cells(t.Columns, idx, widths, r.Cells), color, r.Hot))
		}
		if len(t.Rows) == 0 {
			empty := t.Empty
			if empty == "" {
				empty = "Nothing here yet."
			}
			scr = append(scr, u.text(4, 2, empty, go3270.White, false))
		}
		resp, v, err := u.show(scr, nil)
		if err != nil {
			return view.Choice{}, err
		}
		switch resp.AID {
		case go3270.AIDPF3:
			return view.Choice{Action: view.Back}, nil
		case go3270.AIDPF7:
			switch {
			case top > 0:
				top = max(0, top-per)
			case t.Less:
				return view.Choice{Action: view.Less}, nil
			default:
				msg = "That is the top."
			}
			continue
		case go3270.AIDPF8:
			switch {
			case top+per < len(t.Rows):
				top += per
			case t.More:
				return view.Choice{Action: view.More}, nil
			default:
				msg = "That is the end."
			}
			continue
		case go3270.AIDEnter:
			if pick {
				for i := 0; i < per && top+i < len(t.Rows); i++ {
					if strings.TrimSpace(v["s"+strconv.Itoa(i)]) != "" {
						return view.Choice{Action: view.Pick, Row: top + i, Num: t.Rows[top+i].Num}, nil
					}
				}
			}
			if cmd := strings.TrimSpace(v["cmd"]); cmd != "" {
				return view.Choice{Action: view.Cmd, Text: cmd}, nil
			}
			if !pick && len(t.Actions) == 0 {
				return view.Choice{Action: view.Back}, nil
			}
			continue
		}
		if id := pfAction(t.Actions, resp.AID); id != "" {
			return view.Choice{Action: id}, nil
		}
	}
}

func (u *ui) Message(m *view.Message) (string, error) {
	off := 0
	msg := ""
	keys := "PF3=Back PF7/8=Scroll"
	if k := pfKeys(m.Actions, " "); k != "" {
		keys += " " + k
	}
	for {
		scr, total := u.messageScreen(m, keys, msg, off)
		msg = ""
		resp, _, err := u.show(scr, nil)
		if err != nil {
			return "", err
		}
		if o, ok := u.scroll(resp.AID, off, total); ok {
			off = o
			continue
		}
		switch resp.AID {
		case go3270.AIDPF3:
			return view.Back, nil
		case go3270.AIDEnter:
			if m.Default != "" {
				return m.Default, nil
			}
			return view.Back, nil
		}
		if id := pfAction(m.Actions, resp.AID); id != "" {
			return id, nil
		}
	}
}

// messageScreen: a message's headers and a window of its body from off
func (u *ui) messageScreen(m *view.Message, keys, msg string, off int) (go3270.Screen, int) {
	scr := u.frame(m.Where, keys, msg)
	scr = append(scr,
		u.text(1, 0, "From:", go3270.Turquoise, false),
		u.text(1, 6, bbs.Fit(m.From, 24), go3270.Yellow, true),
		u.text(1, 32, "To:", go3270.Turquoise, false),
		u.text(1, 36, bbs.Fit(m.To, 24), go3270.Green, false),
		u.text(2, 0, "Subj:", go3270.Turquoise, false),
		u.text(2, 6, m.Subject, go3270.White, true),
		u.text(3, 0, "Date:", go3270.Turquoise, false),
		u.text(3, 6, m.Date.Format("2006-01-02 15:04"), go3270.Green, false),
		u.text(4, 0, u.rule(u.cols-2), go3270.Blue, false))
	// the body as written: lines of up to 79, the width of the text editor
	width := min(u.cols-1, bbs.MaxLineLen)
	lines := bbs.Wrap(m.Body, width)
	room := u.rows - 7
	for i := 0; i < room && off+i < len(lines); i++ {
		scr = append(scr, u.text(5+i, 0, bbs.Clean(lines[off+i], width), go3270.Green, false))
	}
	return scr, len(lines)
}

// field is a field to type in at row, col, width wide: past the end of the
// row it goes on in the rows below.  The row after it.
func (u *ui) field(row, col, width int, name string, hidden bool) ([]go3270.Field, int) {
	if col+1+width <= u.cols-1 {
		return u.input(row, col, width, name, hidden), row + 1
	}
	width = min(width, 4*u.cols-col-2)
	end := row*u.cols + col + 1 + width
	return []go3270.Field{
		{Row: row, Col: col, Write: true, Name: name, Hidden: hidden,
			Color: go3270.Green, Highlighting: go3270.Underscore},
		{Row: end / u.cols, Col: end % u.cols, Autoskip: true},
	}, end/u.cols + 1
}

func (u *ui) Form(f *view.Form) (map[string]string, bool, error) {
	submit := f.Submit
	if submit == "" {
		submit = "OK"
	}
	vals := map[string]string{}
	for _, fd := range f.Fields {
		vals[fd.Name] = fd.Value
	}
	for {
		scr := u.frame(f.Title, "Enter="+submit+"  PF3=Back", "")
		row := 2
		for _, l := range f.Intro {
			fs, n := u.viewLine(row, l)
			scr = append(scr, fs...)
			row += n
		}
		if len(f.Intro) > 0 {
			row++
		}
		var inputs go3270.Screen
		for _, fd := range f.Fields {
			if row >= u.rows-3 {
				break
			}
			scr = append(scr, u.text(row, 2, fmt.Sprintf("%-16s ===>", bbs.Trunc(fd.Label, 16)), go3270.Turquoise, false))
			fs, next := u.field(row, 24, fd.Width, fd.Name, fd.Secret) // the label ends at 23
			inputs = append(inputs, fs...)
			row = next
		}
		// the fields first: the cursor goes to the first
		scr = append(inputs, scr...)
		resp, v, err := u.show(scr, vals)
		if err != nil {
			return nil, false, err
		}
		switch resp.AID {
		case go3270.AIDPF3:
			return nil, false, nil
		case go3270.AIDEnter:
			out := map[string]string{}
			for _, fd := range f.Fields {
				if fd.Secret {
					out[fd.Name] = strings.TrimRight(v[fd.Name], "\x00 ")
				} else {
					out[fd.Name] = strings.TrimSpace(strings.Trim(v[fd.Name], "\x00"))
				}
			}
			return out, true, nil
		}
	}
}

func (u *ui) Panel(p *view.Panel) (string, error) {
	keys := "PF3=Back"
	if k := pfKeys(p.Actions, "  "); k != "" {
		keys = k + "  " + keys
	}
	scr := u.frame(p.Title, keys, "")
	row := 2
	for _, l := range p.Lines {
		if row >= u.rows-3 {
			break
		}
		fs, n := u.viewLine(row, l)
		scr = append(scr, fs...)
		row += n
	}
	resp, _, err := u.show(scr, nil)
	if err != nil {
		return "", err
	}
	if resp.AID == go3270.AIDPF3 {
		return view.Back, nil
	}
	return pfAction(p.Actions, resp.AID), nil
}

func (u *ui) Edit(d *view.Draft) (bool, error) {
	body := d.Body
	if len(body) == 0 {
		body = d.Quote
	}
	return u.editor(d.Title, d.To, d.Subject, body, func(to, subject string, body []string) error {
		if strings.TrimSpace(to) == "" {
			return fmt.Errorf("say who it is to")
		}
		if d.CheckTo != nil {
			if err := d.CheckTo(strings.TrimSpace(to)); err != nil {
				return err
			}
		}
		return d.Send(strings.TrimSpace(to), subject, body)
	})
}

func (u *ui) Confirm(question string, def bool) (bool, error) {
	yn := "N"
	if def {
		yn = "Y"
	}
	msg := ""
	for {
		scr := go3270.Screen{}
		scr = append(scr, u.input(4, 2+len([]rune(question))+8, 1, "yn", false)...)
		scr = append(scr, u.frame("", "Enter=Answer  PF3=No", msg)...)
		scr = append(scr, u.text(4, 2, question+" (Y/N)", go3270.White, true))
		resp, v, err := u.show(scr, map[string]string{"yn": yn})
		if err != nil {
			return false, err
		}
		if resp.AID == go3270.AIDPF3 {
			return false, nil
		}
		if resp.AID != go3270.AIDEnter {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(v["yn"])) {
		case "Y":
			return true, nil
		case "N":
			return false, nil
		case "":
			return def, nil
		}
		msg = "Y or N."
	}
}

func (u *ui) Show(title string, lines []view.Line) error {
	var out []string
	var colors []go3270.Color
	for _, l := range lines {
		if l.Bar >= 0 {
			out = append(out, fmt.Sprintf("  %s %5s %s", bbs.Fit(l.Label, 20), l.Text,
				u.bar(int(l.Bar*1000+0.5), 1000, u.cols-32)))
			colors = append(colors, go3270.Turquoise)
			continue
		}
		for _, w := range wrapped(l, u.cols-2) {
			out = append(out, w)
			colors = append(colors, toneColors[l.Tone])
		}
	}
	return u.listColored(title, "", out, colors)
}
