package textui

import (
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/screens"
)

// screen shows the sysop's display file of that name, if there is one
// for this terminal: ANSI art as it is, text a page at a time with its
// tags ({red}, {clear}: screens.Tags) as the terminal's escape sequences,
// @CLS@ and @PAUSE@ done.  It reports whether there was one.
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
	if sc.Format == screens.Art {
		for _, p := range screens.Expand(sc.Text, s.n.Vars()) {
			switch {
			case p.Clear:
				t.Clear()
			case p.Pause:
				if err := t.Pause(); err != nil {
					return true, err
				}
			default:
				t.Art(p.Text)
				if sc.Ext == ".rip" {
					t.ripScene, t.sinceKey = true, 0 // its buttons go when a key is chosen
				}
			}
		}
		return true, t.Flush()
	}
	if _, err := s.textScreen(sc.Text); err != nil {
		return true, err
	}
	return true, t.Flush()
}

// textScreen shows a text screen a page at a time, its tags as the
// terminal's escape sequences; false if the caller quit it.
func (s *session) textScreen(text string) (bool, error) {
	t := s.t
	var lines []string
	var line strings.Builder
	open := false  // a line begun
	fresh := false // just after a clear or a pause: its line's end is dropped
	tagged := false
	defer func() {
		if tagged {
			t.Print(t.Code("reset")) // the caller's text plain again
		}
	}()
	show := func() (bool, error) {
		if open {
			lines = append(lines, line.String())
			line.Reset()
			open = false
		}
		more, err := t.Page(lines)
		lines = nil
		return more, err
	}
	for _, p := range screens.ExpandTags(text, s.n.Vars()) {
		switch {
		case p.Clear, p.Pause:
			if more, err := show(); err != nil || !more {
				return more, err
			}
			if p.Clear {
				t.Clear()
			} else if err := t.Pause(); err != nil {
				return false, err
			}
			fresh = true
		case p.Tag != "":
			line.WriteString(t.Code(p.Tag))
			open, tagged = true, true
		default:
			text := p.Text
			if fresh {
				text = strings.TrimPrefix(text, "\n") // the rest of a {clear}'s line
				fresh = false
			}
			for i, part := range strings.Split(text, "\n") {
				if i > 0 {
					lines = append(lines, line.String())
					line.Reset()
					open = false
				}
				line.WriteString(bbs.CleanSpaced(part, 400))
				open = open || part != ""
			}
		}
	}
	return show()
}

// str is a string of the sysop's (or the default), filled in, its tags
// marked for Print.
func (s *session) str(key string) string {
	v, _ := s.n.StrMarked(key)
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
		t.Println(s.b.Conf().Goodbye)
	}
	return true, nil
}
