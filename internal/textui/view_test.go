package textui

import (
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/view"
)

// TestKeyLabel: a key in brackets where it begins a word of its label,
// else before it; actions with no key are left out of the prompt.
func TestKeyLabel(t *testing.T) {
	for _, c := range []struct {
		key        rune
		label, out string
	}{{'R', "Read new", "[R]ead new"}, {'K', "SSH keys", "SSH [K]eys"}, {'K', "lock", "[K] lock"},
		{'+', "add area", "[+] add area"}, {'8', "8-bit UTF-8 on/off", "[8]-bit UTF-8 on/off"},
		{'V', "Validate (level 20)", "[V]alidate (level 20)"}} {
		if got := keyLabel(view.Action{Key: c.key, Label: c.label}); got != c.out {
			t.Errorf("%c %q: %q", c.key, c.label, got)
		}
	}
	if got := keyPrompt([]view.Action{{Key: 'N', Label: "Next"}, {PF: 5, Label: "3270 only"}}); got != "[N]ext, [Q]uit: " {
		t.Errorf("prompt %q", got)
	}
}

// TestSheetFoot: the key bar names a sheet's keys, leaving out those with
// a 3270 PF key alone (no NUL for them).
func TestSheetFoot(t *testing.T) {
	term, _ := ansiTerm()
	s := &session{t: term}
	s.startDesk()
	w := &sheetWin{s: s, sh: &view.Sheet{Title: "T", Keys: []view.Action{{Key: 'A', Label: "Add"}, {PF: 5, Label: "PFonly"}}}, cur: -1}
	bar := screenRow(w.frame(), term.Rows-1)
	if strings.Contains(bar, "\x00") || strings.Contains(bar, "PFonly") || !strings.Contains(bar, "A Add  F9 Quit") {
		t.Errorf("key bar %q", bar)
	}
}

// screenRow is a screen's row as text.
func screenRow(scr *screen, row int) string {
	var b strings.Builder
	for x := 0; x < scr.cols; x++ {
		b.WriteRune(scr.at(row, x).r)
	}
	return b.String()
}
