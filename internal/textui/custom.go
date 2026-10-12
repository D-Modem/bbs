package textui

import (
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/screens"
)

// screen shows the sysop's display file of that name, if there is one
// for this terminal: ANSI art as it is, text a page at a time, @CLS@ and
// @PAUSE@ done.  It reports whether there was one.
func (s *session) screen(name string) (bool, error) {
	t := s.t
	kind := screens.Plain
	switch {
	case t.PETSCII:
		kind = screens.PETSCII
	case t.RIP:
		kind = screens.RIP
	case t.ANSI:
		kind = screens.ANSI
	}
	sc, ok := s.b.Screens.Find(name, kind)
	if !ok {
		return false, nil
	}
	if sc.Format == screens.Raw {
		return true, t.Raw(sc.Bytes)
	}
	for _, p := range screens.Expand(sc.Text, s.n.Vars()) {
		switch {
		case p.Clear:
			t.Clear()
		case p.Pause:
			if err := t.Pause(); err != nil {
				return true, err
			}
		case sc.Format == screens.Art:
			t.Art(p.Text)
			if sc.Ext == ".rip" {
				t.ripScene, t.sinceKey = true, 0 // its buttons go when a key is chosen
			}
		default:
			lines := strings.Split(strings.TrimSuffix(p.Text, "\n"), "\n")
			for i, l := range lines {
				lines[i] = bbs.Clean(l, 400)
			}
			if more, err := t.Page(lines); err != nil || !more {
				return true, err
			}
		}
	}
	return true, t.Flush()
}

// str is a string of the sysop's (or the default), filled in.
func (s *session) str(key string) string {
	v, _ := s.n.Str(key)
	return v
}

// ShowScreen is screen, and a pause after it when the caller is to read
// it before going on.
func (s *session) ShowScreen(name string, wait bool) (bool, error) {
	shown, err := s.screen(name)
	if err == nil && shown && wait {
		err = s.t.Pause()
	}
	return shown, err
}

// Logoff asks, and says goodbye.
func (s *session) Logoff() (bool, error) {
	t := s.t
	ok, err := t.YesNo(s.str("logoff_confirm"), true)
	if err != nil || !ok {
		return false, err
	}
	t.Println("")
	if shown, err := s.screen("goodbye"); err != nil || !shown {
		t.Println(s.b.Goodbye)
	}
	return true, nil
}
