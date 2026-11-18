package textui

import (
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/view"
)

// TestMenuKeys: a hidden key is taken but not shown; a key that is not
// the menu's asks again; with Line, the caller types a word and Enter.
func TestMenuKeys(t *testing.T) {
	m := &view.Menu{Title: "Main", Prompt: "Choice: ", Hidden: []string{"Q"},
		Items: []view.Item{{Key: "M", Label: "Messages"}}}
	s, c := deskTerm("zq", false)
	if k, err := s.Menu(m); err != nil || k != "Q" {
		t.Fatalf("%q %v", k, err)
	}
	if out := c.out.String(); !strings.Contains(out, "Messages") || strings.Count(out, "Choice: ") != 2 {
		t.Errorf("%q", out)
	}
	m.Line = true
	m.Items = append(m.Items, view.Item{Key: "JOIN", Label: "Join"})
	s, _ = deskTerm("nope\rjoin\r", false)
	if k, err := s.Menu(m); err != nil || k != "JOIN" {
		t.Fatalf("%q %v", k, err)
	}
	s, _ = deskTerm("\r", false)
	if k, err := s.Menu(m); err != nil || k != "" {
		t.Fatalf("Enter: %q %v", k, err)
	}
}
