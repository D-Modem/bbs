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
	if t.ANSI {
		kind = screens.ANSI
	}
	sc, ok := s.b.Screens.Find(name, kind)
	if !ok {
		return false, nil
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

// logoff asks, says goodbye and hangs up; nil if the caller stays.
func (s *session) logoff() error {
	t := s.t
	ok, err := t.YesNo(s.str("logoff_confirm"), true)
	if err != nil || !ok {
		return err
	}
	t.Println("")
	if shown, err := s.screen("goodbye"); err != nil || !shown {
		t.Println(s.b.Goodbye)
	}
	s.b.Log.Printf("node %d: %s logged off", s.n.Num, s.user.Name)
	return errHangup
}

// runMenu runs a menu of the sysop's until it is left (back) or the
// caller logs off.
func (s *session) runMenu(m *bbs.Menu, depth int) error {
	t := s.t
	vars := s.n.Vars()
	for {
		s.user = s.n.Refresh()
		title := screens.String(m.Title, vars)
		s.n.Doing(title)
		shown := false
		if m.Screen != "" {
			var err error
			if shown, err = s.screen(m.Screen); err != nil {
				return err
			}
		}
		if !shown {
			t.Println("")
			t.Println(t.C(Cyan, title))
			var items []string
			for _, it := range m.Items {
				items = append(items, t.C(Yellow, "["+it.Key+"]")+" "+bbs.Fit(screens.String(it.Label, vars), 22))
			}
			for i := 0; i < len(items); i += 2 {
				t.Println("  " + strings.Join(items[i:min(i+2, len(items))], ""))
			}
		}
		prompt := s.str("command_prompt")
		if m.Prompt != "" {
			prompt = screens.String(m.Prompt, vars)
		}
		t.Print("\n" + prompt)
		k, err := t.Key()
		if err != nil {
			return err
		}
		if k == '\r' {
			t.Println("")
			continue
		}
		t.Println(string(k))
		it := m.Item(string(k))
		if it == nil {
			continue
		}
		if it.Action == "back" {
			if depth > 0 {
				return nil
			}
			continue
		}
		if err := s.do(it, depth); err != nil {
			return err
		}
	}
}

// do does a menu item.
func (s *session) do(it *bbs.MenuItem, depth int) error {
	t := s.t
	switch it.Action {
	case "areas":
		return s.areas()
	case "mail":
		return s.mail()
	case "who":
		return s.who()
	case "users":
		return s.users()
	case "profile":
		return s.profile()
	case "info":
		return s.info()
	case "files":
		if s.b.Files == nil {
			t.Println("There are no file areas.")
			return nil
		}
		return s.fileAreas()
	case "doors":
		return s.doors()
	case "chat":
		return s.chat("")
	case "page":
		return s.page()
	case "terminal":
		t.Println("That is for 3270 terminals.")
		return nil
	case "logoff":
		return s.logoff()
	case "menu":
		if depth >= 10 {
			t.Println("Menus nest too deep.")
			return nil
		}
		return s.runMenu(s.b.Menus[strings.ToLower(it.Arg)], depth+1)
	case "screen":
		shown, err := s.screen(it.Arg)
		if err != nil {
			return err
		}
		if !shown {
			t.Println("There is nothing to show.")
			return nil
		}
		return t.Pause()
	case "area":
		a, err := s.b.Store.AreaByName(it.Arg)
		if err != nil {
			t.Println(t.C(Red, "There is no area "+it.Arg+"."))
			return nil
		}
		return s.area(a)
	case "door":
		for _, d := range s.doorsFor() {
			if strings.EqualFold(d.Name, it.Arg) {
				return s.openDoor(d)
			}
		}
		t.Println("That door is not open to you.")
	}
	return nil
}
