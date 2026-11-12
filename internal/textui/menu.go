package textui

import (
	"strings"

	"github.com/D-Modem/bbs/internal/rip"
	"github.com/D-Modem/bbs/internal/screens"
)

// choice is a menu's item.
type choice struct{ key, label, help string }

// menu shows a menu and reads its key: the title and items (unless list
// is false: a screen of the sysop's showed them) in two columns, or one
// on a narrow screen, or as buttons on a RIP terminal; then the prompt.
// Enter is '\r'.
func (s *session) menu(title string, items []choice, prompt string, list bool) (rune, error) {
	t := s.t
	if t.RIP && list {
		// what the last choice printed is read before the buttons cover it
		if t.sinceKey > 0 {
			if err := t.Pause(); err != nil {
				return 0, err
			}
		}
		ri := make([]rip.Item, len(items))
		for i, it := range items {
			ri[i] = rip.Item{Key: it.key, Label: screens.Unmark(it.label)}
		}
		scene, _ := rip.Menu(screens.Unmark(title), ri)
		t.RIPScene(scene)
		t.Print(strings.TrimLeft(prompt, "\n"))
	} else {
		if list {
			t.Println("")
			t.Println(t.C(Cyan, title))
			cols, width := 2, 22
			if t.Narrow() {
				cols, width = 1, max(t.Cols-7, 10)
			}
			helps := false
			for _, it := range items {
				helps = helps || it.help != ""
			}
			var cells []string
			for _, it := range items {
				cell := t.C(Yellow, "["+it.key+"]") + " " + screens.Fit(it.label, width)
				if helps && !t.Narrow() {
					// one column, what each does beside it
					cols = 1
					if room := t.Cols - 2 - 4 - width - 2 - 1; it.help != "" && room > 10 {
						cell += "  " + t.C(Dim, trunc(it.help, room))
					}
				}
				cells = append(cells, cell)
			}
			for i := 0; i < len(cells); i += cols {
				t.Println("  " + strings.Join(cells[i:min(i+cols, len(cells))], ""))
			}
		}
		t.Print(prompt)
	}
	k, err := t.Key()
	if err != nil {
		return 0, err
	}
	if t.ripScene {
		t.RIPClear()
	} else if k != '\r' {
		t.Println(string(k))
	} else {
		t.Println("")
	}
	return k, nil
}
