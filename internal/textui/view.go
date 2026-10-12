package textui

import (
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
	if len(rs) > 0 && unicode.ToUpper(rs[0]) == k {
		return "[" + string(k) + "]" + string(rs[1:])
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

func (s *session) PageSize() int { return max(s.t.Rows-5, 5) }
func (s *session) Narrow() bool  { return s.t.Narrow() }

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
	k, err := s.menu(m.Title, items, "\n"+prompt, !shown)
	if err != nil || k == '\r' {
		return "", err
	}
	return string(k), nil
}

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

func (s *session) Message(m *view.Message) (string, error) {
	t := s.t
	t.Clear()
	t.Println(t.C(Dim, m.Where))
	t.Println(t.C(Cyan, "From: ") + t.C(Yellow, m.From) + "   " + t.C(Cyan, "To: ") + m.To)
	t.Println(t.C(Cyan, "Subj: ") + t.C(White, m.Subject))
	t.Println(t.C(Cyan, "Date: ") + date(m.Date))
	t.Println(t.C(Dim, strings.Repeat("-", min(t.Cols-1, 79))))
	if _, err := t.Page(bbs.Wrap(m.Body, t.Cols-1)); err != nil {
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

func (s *session) Panel(p *view.Panel) (string, error) {
	t := s.t
	t.Println("")
	t.Println(t.C(Cyan, p.Title))
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

func (s *session) Edit(d *view.Draft) (bool, error) {
	t := s.t
	t.Println("")
	to := d.To
	for {
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
	t.Print("Subject: ")
	l, err := t.ReadLine(LineOpts{Max: bbs.MaxSubjectLen, Initial: d.Subject})
	if err != nil {
		return false, err
	}
	subject := strings.TrimSpace(l.Text)
	if subject == "" {
		t.Println("No subject: given up.")
		return false, nil
	}
	body, err := s.edit(d.Body)
	if err != nil || body == nil {
		return false, err
	}
	if err := d.Send(to, subject, body); err != nil {
		t.Println(t.C(Red, capital(err.Error())+"."))
		return false, nil
	}
	return true, nil
}

func (s *session) Confirm(question string, def bool) (bool, error) {
	return s.t.YesNo(question, def)
}

func (s *session) Say(tn view.Tone, msg string) {
	if tn == view.Bright {
		s.t.Println("")
		s.t.Println(s.t.C(Cyan, msg))
		return
	}
	s.t.Println(s.tone(tn, msg))
}

func (s *session) Show(title string, ls []view.Line) error {
	t := s.t
	t.Println("")
	t.Println(t.C(Cyan, title))
	if ok, err := t.Page(s.lines(ls)); err != nil || !ok {
		return err
	}
	return t.Pause()
}
