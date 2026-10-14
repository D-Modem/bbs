package confui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bbssec "github.com/Neo-Desktop/bbs-sec"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// script is a view.UI that answers from a list, and keeps what it was
// told.
type script struct {
	t       *testing.T
	answers []any // string (a Menu's key, a Panel's action), view.Choice, map[string]string, bool
	said    []string
}

func (s *script) next() any {
	s.t.Helper()
	if len(s.answers) == 0 {
		s.t.Fatal("out of answers")
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	return a
}

func (s *script) PageSize() int { return 20 }
func (s *script) Narrow() bool  { return false }
func (s *script) Menu(m *view.Menu) (string, error) {
	return s.next().(string), nil
}
func (s *script) Table(t *view.Table) (view.Choice, error) {
	return s.next().(view.Choice), nil
}
func (s *script) Message(m *view.Message) (string, error) { return view.Back, nil }
func (s *script) Form(f *view.Form) (map[string]string, bool, error) {
	return s.next().(map[string]string), true, nil
}
func (s *script) Panel(p *view.Panel) (string, error) { return s.next().(string), nil }
func (s *script) Edit(d *view.Draft) (bool, error)    { return false, nil }
func (s *script) Confirm(q string, def bool) (bool, error) {
	s.said = append(s.said, q)
	return s.next().(bool), nil
}
func (s *script) Say(tn view.Tone, msg string) { s.said = append(s.said, msg) }
func (s *script) Show(title string, ls []view.Line) error {
	for _, l := range ls {
		s.said = append(s.said, l.Text)
	}
	return nil
}

func (s *script) told(sub string) bool {
	return strings.Contains(strings.Join(s.said, "\n"), sub)
}

const file = "[general]\nname = Test BBS\n\n[listen telnet]\ntype = telnet\naddress = :2300\n"

func pick(row int) view.Choice { return view.Choice{Action: view.Pick, Row: row} }

var back = view.Choice{Action: view.Back}

// rename: Sections, [general], name, Edit to v, back out to the menu
func rename(v string) []any {
	return []any{"S", pick(0), pick(0), "edit", map[string]string{"v": v}, view.Back, back, back}
}

func TestWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bbs.conf")
	os.WriteFile(p, []byte(file), 0o600)
	var checked []byte
	check := func(data []byte) ([]string, error) {
		checked = data
		if strings.Contains(string(data), "name = bad") {
			return nil, errors.New("[general] name: bad")
		}
		return []string{"a warning"}, nil
	}
	reloaded := false
	reload := func() ([]string, []string, error) {
		reloaded = true
		return []string{"~[general] name = New BBS (was Test BBS)"}, nil, nil
	}
	ui := &script{t: t}
	ui.answers = append(rename("bad"), "W")
	ui.answers = append(ui.answers, rename("New BBS")...)
	ui.answers = append(ui.answers, "C", "W", "Q")
	e := &Editor{UI: ui, Tools: bbs.ConfigTools{Path: p, Check: check, Reload: reload}}
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	if !ui.told("The BBS would not take this: [general] name: bad") || !ui.told("Not written: the BBS would not start") {
		t.Errorf("a bad value not refused: %q", ui.said)
	}
	if !ui.told("Warning: a warning") || !reloaded || !ui.told("In force now:") {
		t.Errorf("said %q", ui.said)
	}
	data, _ := os.ReadFile(p)
	if string(data) != strings.Replace(file, "Test BBS", "New BBS", 1) || string(checked) != string(data) {
		t.Errorf("written:\n%s", data)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	if old, _ := os.ReadFile(p + ".bak"); string(old) != file {
		t.Errorf(".bak:\n%s", old)
	}
}

// TestChangedOnDisk: a file changed by another since it was read is not
// written over.
func TestChangedOnDisk(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bbs.conf")
	os.WriteFile(p, []byte(file), 0o600)
	// the sysop edits by hand meanwhile: when the key's panel is left
	byHand := func() any {
		os.WriteFile(p, []byte(file+"\n; by hand\n"), 0o600)
		return view.Back
	}
	ui := &script{t: t, answers: []any{"S", pick(0), pick(0), "edit", map[string]string{"v": "New BBS"},
		byHand, back, back, "W", "Q", true}}
	e := &Editor{UI: &hook{ui}, Tools: bbs.ConfigTools{Path: p}}
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	if !ui.told("has changed since it was read") || !ui.told("Leave without writing 1 changes?") {
		t.Errorf("said %q", ui.said)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), "by hand") {
		t.Error("written over")
	}
}

// hook runs an answer that is a func when its turn comes.
type hook struct{ *script }

func (h *hook) Panel(p *view.Panel) (string, error) {
	if f, ok := h.answers[0].(func() any); ok {
		h.answers = h.answers[1:]
		return f().(string), nil
	}
	return h.script.Panel(p)
}

// TestFromSample: with no file, the sample is begun from.
func TestFromSample(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bbs.conf")
	ui := &script{t: t, answers: []any{true, "W", "Q"}}
	if err := (&Editor{UI: ui, Tools: bbs.ConfigTools{Path: p}}).Run(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != string(bbssec.Sample) {
		t.Errorf("not the sample: %d bytes", len(data))
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", fi.Mode())
	}
	if _, err := os.Stat(p + ".bak"); err == nil {
		t.Error("a .bak of nothing")
	}
}
