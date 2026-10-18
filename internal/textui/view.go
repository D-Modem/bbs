package textui

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// The shared screens (internal/app) drawn on a text terminal: prompts and
// keys, a page at a time.

var tones = map[view.Tone]string{view.Dim: Dim, view.Bright: White, view.Good: Green,
	view.Bad: Red, view.Warn: Yellow}

// tone is s in a tone's color.
func (s *session) tone(tn view.Tone, text string) string {
	if c, ok := tones[tn]; ok {
		return s.t.C(c, text)
	}
	return text
}

// line is a view's line as text.
func (s *session) line(l view.Line) string {
	t := s.t
	switch {
	case l.Bar >= 0:
		width := max(t.Cols-34, 4)
		ch := "#"
		if t.UTF8 {
			ch = "█"
		}
		return fmt.Sprintf("  %s %5s %s", bbs.Fit(l.Label, 20), l.Text,
			t.C(Cyan, strings.Repeat(ch, int(l.Bar*float64(width)+0.5))))
	case l.Label != "":
		return fmt.Sprintf("  %-10s %s", l.Label, s.tone(l.Tone, l.Text))
	}
	return s.tone(l.Tone, l.Text)
}

// lines are view lines as text, each as line has it.
func (s *session) lines(ls []view.Line) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = s.line(l)
	}
	return out
}

// keyLabel is an action as a prompt shows it: its key in brackets, in the
// word where it begins one ("[R]ead new", "SSH [K]eys"), else before it.
func keyLabel(a view.Action) string {
	k := unicode.ToUpper(a.Key)
	rs := []rune(a.Label)
	for i, r := range rs {
		if unicode.ToUpper(r) == k && (i == 0 || rs[i-1] == ' ' || rs[i-1] == '-') {
			return string(rs[:i]) + "[" + string(k) + "]" + string(rs[i+1:])
		}
	}
	return "[" + string(k) + "] " + a.Label
}

// keyPrompt is the prompt for actions by key: "[N]ext, [R]eply, [Q]uit: ".
func keyPrompt(acts []view.Action) string {
	var parts []string
	for _, a := range acts {
		if a.Key != 0 {
			parts = append(parts, keyLabel(a))
		}
	}
	return strings.Join(append(parts, "[Q]uit: "), ", ")
}

// action is the action of a key (Q and Enter: Back).
func action(acts []view.Action, k rune) string {
	for _, a := range acts {
		if a.Key != 0 && unicode.ToUpper(a.Key) == unicode.ToUpper(k) {
			return a.ID
		}
	}
	return ""
}

// picture shows a picture as the terminal can, captioned with what it
// is (alone where it cannot): the rows it took.
func (s *session) picture(p *view.Picture, rows int) int {
	t := s.t
	used := t.ShowPicture(p, rows)
	if p.Alt != "" {
		t.Println(t.C(Dim, "["+p.Alt+"]"))
		used++
	}
	return used
}

// PageSize is the screen's rows less 5 for the heading and prompt, at
// least 5.
func (s *session) PageSize() int { return max(s.t.Rows-5, 5) }

// Narrow is the terminal's being under 60 columns.
func (s *session) Narrow() bool { return s.t.Narrow() }

// Menu shows a menu: its intro, the sysop's screen if there is one, else
// its items (in RIP buttons where it can; in brief, only on ?), and reads
// a key until it is an item's.  It is that item's key; "" for Enter (none
// chosen: the caller shows its menu again, or goes back if it will).
func (s *session) Menu(m *view.Menu) (string, error) {
	t := s.t
	if len(m.Intro) > 0 {
		t.Println("")
		if _, err := t.Page(s.lines(m.Intro)); err != nil {
			return "", err
		}
	}
	shown := false
	if m.Screen != "" {
		var err error
		if shown, err = s.screen(m.Screen); err != nil {
			return "", err
		}
	}
	items := make([]choice, len(m.Items))
	for i, it := range m.Items {
		items[i] = choice{it.Key, it.Label}
	}
	prompt := m.Prompt
	if prompt == "" {
		prompt = s.str("command_prompt")
	}
	brief := m.Brief && !s.t.RIP && !shown
	if brief {
		if p := strings.TrimRight(prompt, " "); strings.HasSuffix(p, ":") {
			prompt = strings.TrimSuffix(p, ":") + " (? for the menu): "
		} else {
			prompt = p + " (? for the menu) "
		}
	}
	k, err := s.menu(m.Title, items, "\n"+prompt, !shown && !brief)
	if err == nil && brief && k == '?' {
		k, err = s.menu(m.Title, items, "\n"+prompt, true)
	}
	for {
		if err != nil || k == '\r' {
			return "", err
		}
		for _, it := range m.Items {
			if strings.EqualFold(it.Key, string(k)) {
				return it.Key, nil
			}
		}
		// not an item's: asked again, as a 3270 does
		k, err = s.menu(m.Title, items, "\n"+prompt, false)
	}
}

// Table shows a table a page at a time, its columns fitted to the screen,
// and asks what next: a row's number (Pick, Row -1 if no row has it), an
// action's key, More at Enter if there is more, or Back.  With nothing to
// pick or do it pauses at the end and is Back.
func (s *session) Table(tb *view.Table) (view.Choice, error) {
	t := s.t
	t.Println("")
	t.Println(t.C(Cyan, tb.Title))
	for _, l := range tb.Note {
		t.Println(s.line(l))
	}
	numW := 0
	for _, r := range tb.Rows {
		numW = max(numW, len(r.Num))
	}
	if numW > 0 {
		numW = max(numW, 3)
	}
	mark := false
	for _, r := range tb.Rows {
		mark = mark || r.Mark
	}
	lead := numW
	if numW > 0 {
		lead++
	}
	if mark {
		lead += 2
	}
	idx, widths := view.Layout(tb.Columns, t.Narrow(), t.Cols-1-lead)
	var out []string
	if len(tb.Rows) == 0 {
		if tb.Empty != "" {
			out = append(out, "  "+tb.Empty)
		}
	} else if len(tb.Columns) > 0 {
		out = append(out, t.C(Dim, strings.Repeat(" ", lead)+view.Cells(tb.Columns, idx, widths, view.Titles(tb.Columns))))
	}
	for _, r := range tb.Rows {
		line := ""
		if numW > 0 {
			line = t.C(Yellow, fmt.Sprintf("%*s", numW, r.Num)) + " "
		}
		if mark {
			if r.Mark {
				line += t.C(Green, "*") + " "
			} else {
				line += "  "
			}
		}
		text := view.Cells(tb.Columns, idx, widths, r.Cells)
		if r.Hot {
			text = t.C(Yellow, text)
		}
		out = append(out, line+text)
	}
	if ok, err := t.Page(out); err != nil {
		return view.Choice{}, err
	} else if !ok && tb.Pick == "" && len(tb.Actions) == 0 {
		return view.Choice{Action: view.Back}, nil
	}
	if tb.Pick == "" && len(tb.Actions) == 0 {
		return view.Choice{Action: view.Back}, t.Pause()
	}
	prompt := tb.Prompt
	if prompt == "" {
		var parts []string
		if tb.Pick != "" {
			parts = append(parts, tb.Pick)
		}
		if tb.More {
			parts = append(parts, "Enter for more")
		}
		parts = append(parts, keyPrompt(tb.Actions))
		prompt = "\n" + strings.Join(parts, ", ")
	}
	for {
		cmd, err := t.Input(prompt, 10)
		if err != nil {
			return view.Choice{}, err
		}
		switch {
		case cmd == "" && tb.More:
			return view.Choice{Action: view.More}, nil
		case cmd == "" || strings.EqualFold(cmd, "Q"):
			return view.Choice{Action: view.Back}, nil
		}
		if r, _ := utf8.DecodeRuneInString(cmd); len(cmd) == utf8.RuneLen(r) {
			if id := action(tb.Actions, r); id != "" {
				return view.Choice{Action: id}, nil
			}
		}
		num := strings.TrimPrefix(cmd, "#")
		if _, err := strconv.ParseInt(num, 10, 64); err != nil || tb.Pick == "" {
			t.Println(t.C(Red, "What?"))
			if tb.Prompt == "" {
				prompt = strings.TrimPrefix(prompt, "\n")
			}
			continue
		}
		for i, r := range tb.Rows {
			if r.Num == num {
				return view.Choice{Action: view.Pick, Row: i, Num: num}, nil
			}
		}
		return view.Choice{Action: view.Pick, Row: -1, Num: num}, nil
	}
}

// Message shows a message from the top of the screen: its heading, its
// picture, its body a page at a time; then waits for an action's key (Q:
// Quit, Enter or space: Default, each Back if unset).
func (s *session) Message(m *view.Message) (string, error) {
	t := s.t
	t.Clear()
	t.Println(t.C(Dim, m.Where))
	t.Println(t.C(Cyan, "From: ") + t.C(Yellow, m.From) + "   " + t.C(Cyan, "To: ") + m.To)
	t.Println(t.C(Cyan, "Subj: ") + t.C(White, m.Subject))
	t.Println(t.C(Cyan, "Date: ") + date(m.Date))
	t.Println(t.C(Dim, strings.Repeat("-", min(t.Cols-1, 79))))
	used := 5
	if m.Picture != nil {
		rows := m.Picture.Rows
		if rows <= 0 {
			rows = 12
		}
		used += s.picture(m.Picture, min(rows, max(t.Rows-10, 4)))
	}
	if _, err := t.PageAfter(bbs.Wrap(m.Body, t.Cols-1), used); err != nil {
		return "", err
	}
	t.Print("\n" + keyPrompt(m.Actions))
	for {
		k, err := t.Key()
		if err != nil {
			return "", err
		}
		id := action(m.Actions, k)
		switch {
		case k == 'Q':
			id = m.Quit
			if id == "" {
				id = view.Back
			}
		case k == '\r' || k == ' ':
			id = m.Default
			if id == "" {
				id = view.Back
			}
		}
		if id != "" {
			t.Println("")
			return id, nil
		}
	}
}

// Form asks for each field in turn, Enter keeping its value; nothing typed
// for a first field with no value is giving up (false).
func (s *session) Form(f *view.Form) (map[string]string, bool, error) {
	t := s.t
	for _, l := range f.Intro {
		t.Println(s.line(l))
	}
	out := map[string]string{}
	for i, fd := range f.Fields {
		prompt := fd.Prompt
		if prompt == "" {
			prompt = fd.Label + ": "
		}
		var v string
		var err error
		if fd.Edit && !fd.Secret {
			t.Print(prompt)
			l, err := t.ReadLine(LineOpts{Max: cmp.Or(fd.Width, 255), Initial: fd.Value})
			if err != nil {
				return nil, false, err
			}
			out[fd.Name] = l.Text // spaces at its ends may matter (a prompt's)
			continue
		}
		if fd.Secret {
			v, err = t.Password(prompt)
		} else {
			if fd.Value != "" {
				prompt = strings.TrimRight(prompt, ": ?") + " [" + fd.Value + "]: "
			}
			v, err = t.Input(prompt, fd.Width)
		}
		if err != nil {
			return nil, false, err
		}
		if v == "" {
			if fd.Value == "" && i == 0 {
				return out, false, nil
			}
			v = fd.Value
		}
		out[fd.Name] = v
	}
	return out, true, nil
}

// Panel shows a panel's title, picture and lines, and reads a key: an
// action's ID, Back for Q or Enter, "" for any other.
func (s *session) Panel(p *view.Panel) (string, error) {
	t := s.t
	t.Println("")
	t.Println(t.C(Cyan, p.Title))
	if p.Picture != nil {
		rows := p.Picture.Rows
		if rows <= 0 {
			rows = t.Rows - len(p.Lines) - 5
		}
		s.picture(p.Picture, max(rows, 4))
	}
	for _, l := range p.Lines {
		if l.Label == "" && l.Bar < 0 {
			t.Println("  " + s.line(l))
		} else {
			t.Println(s.line(l))
		}
	}
	t.Print("\n" + keyPrompt(p.Actions))
	k, err := t.Key()
	if err != nil {
		return "", err
	}
	t.Println("")
	if k == 'Q' || k == '\r' {
		return view.Back, nil
	}
	return action(p.Actions, k), nil
}

// Edit asks who a draft is to (unless NoTo) and its subject, and has the
// body written in the full-screen editor if the caller gets it, else the
// line editor; true if it was sent.  An error sending is told the caller,
// and is false.
func (s *session) Edit(d *view.Draft) (bool, error) {
	t := s.t
	t.Println("")
	to := d.To
	for !d.NoTo {
		prompt := "To [" + to + "]: "
		if to == "" {
			hint := d.ToHint
			if hint != "" {
				hint += ", "
			}
			prompt = "To (" + hint + "Enter to give up): "
		}
		who, err := t.Input(prompt, bbs.MaxMailToLen)
		if err != nil {
			return false, err
		}
		if who != "" {
			to = who
		}
		if to == "" {
			return false, nil
		}
		if d.CheckTo != nil {
			if err := d.CheckTo(to); err != nil {
				t.Println(t.C(Red, capital(err.Error())+"."))
				to = ""
				continue
			}
		}
		break
	}
	if d.AnySubject {
		t.Print("Subject (Enter for none): ")
	} else {
		t.Print("Subject: ")
	}
	l, err := t.ReadLine(LineOpts{Max: bbs.MaxSubjectLen, Initial: d.Subject})
	if err != nil {
		return false, err
	}
	subject := strings.TrimSpace(l.Text)
	if subject == "" && !d.AnySubject {
		t.Println("No subject: given up.")
		return false, nil
	}
	var body []string
	if s.fullScreen() {
		body, err = s.fullEdit(to, subject, d.Body, d.Quote)
	} else {
		initial := d.Body
		if len(initial) == 0 {
			initial = d.Quote
		}
		body, err = s.edit(initial)
	}
	if err != nil || body == nil {
		return false, err
	}
	if err := d.Send(to, subject, body); err != nil {
		t.Println(t.C(Red, capital(err.Error())+"."))
		return false, nil
	}
	return true, nil
}

// Confirm asks a yes or no question, def at Enter.
func (s *session) Confirm(question string, def bool) (bool, error) {
	return s.t.YesNo(question, def)
}

// Say prints msg in its tone; a Bright one as a heading, after a blank
// line.
func (s *session) Say(tn view.Tone, msg string) {
	if tn == view.Bright {
		s.t.Println("")
		s.t.Println(s.t.C(Cyan, msg))
		return
	}
	s.t.Println(s.tone(tn, msg))
}

// Show shows a title and lines a page at a time, and waits for a key at
// the end (not if the caller quit before it).
func (s *session) Show(title string, ls []view.Line) error {
	t := s.t
	t.Println("")
	t.Println(t.C(Cyan, title))
	if ok, err := t.Page(s.lines(ls)); err != nil || !ok {
		return err
	}
	return t.Pause()
}
