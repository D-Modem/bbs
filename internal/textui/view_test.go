package textui

import (
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

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
