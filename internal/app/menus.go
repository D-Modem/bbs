package app

import (
	"embed"
	"fmt"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/screens"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/view"
)

// The menus: the sysop's (bbs.conf's [menu NAME]s and menus_dir's files)
// over the built-in ones of the Classic theme (menus/*.menu), run as a
// stack: menu.gosub puts a menu on it, menu.return takes it off,
// menu.goto puts one in the place of the top one, menu.main goes back to
// the main menu.

//go:embed menus/*.menu
var classicFiles embed.FS

// classic is the built-in menus, by name.
var classic = func() map[string]*bbs.Menu {
	out := map[string]*bbs.Menu{}
	ents, err := classicFiles.ReadDir("menus")
	if err != nil {
		panic(err)
	}
	for _, e := range ents {
		f, err := classicFiles.Open("menus/" + e.Name())
		if err != nil {
			panic(err)
		}
		cf, err := config.Parse(f)
		f.Close()
		if err != nil {
			panic(fmt.Sprintf("menus/%s: %v", e.Name(), err))
		}
		name := strings.TrimSuffix(e.Name(), ".menu")
		m, err := bbs.ParseMenuFile(name, cf)
		if err != nil {
			panic(fmt.Sprintf("menus/%s: %v", e.Name(), err))
		}
		out[name] = m
	}
	return out
}()

// BuiltinMenu is the built-in menu of a name, or nil.
func BuiltinMenu(name string) *bbs.Menu { return classic[name] }

// maxMenuDepth is how many menus the stack holds at most.
const maxMenuDepth = 10

// menuFrame is a menu on the stack.
type menuFrame struct {
	name    string
	started bool // its @FIRST is done
}

// menuRun is the stack of menus of a session.
type menuRun struct {
	stack []menuFrame
	moved bool // the stack changed: the rest of an item's commands is not done
}

func (r *menuRun) top() *menuFrame { return &r.stack[len(r.stack)-1] }

// menuNamed is the sysop's menu of a name, else the built-in one, else nil.
func (s *Session) menuNamed(name string) *bbs.Menu {
	if m := s.B.Menu(name); m != nil {
		return m
	}
	return classic[name]
}

// menuCommand is how the session does a command, and when.
type menuCommand struct {
	run func(s *Session, c bbs.Command, r *menuRun) error
	// avail: whether the caller can use it now; an item whose first
	// command is not available is not shown (nil: always)
	avail func(s *Session, u *store.User) bool
	// note: what @NOTE@ in the item's label becomes (nil: nothing)
	note func(s *Session, u *store.User) string
}

// plain is a command that is a function of the session's.
func plain(f func(s *Session) error) func(*Session, bbs.Command, *menuRun) error {
	return func(s *Session, _ bbs.Command, _ *menuRun) error { return f(s) }
}

// menuCommands are the commands of bbs.Commands, done.
var menuCommands map[string]menuCommand

func init() {
	menuCommands = map[string]menuCommand{
		"areas": {run: plain((*Session).areas)},
		"mail": {run: plain((*Session).mail), note: func(s *Session, u *store.User) string {
			if n, _ := s.B.Store.UnreadMail(u.ID); n > 0 {
				return fmt.Sprintf(" (%d new)", n)
			}
			return ""
		}},
		"who":     {run: plain((*Session).who)},
		"users":   {run: plain((*Session).users)},
		"profile": {run: plain((*Session).profile)},
		"info":    {run: plain((*Session).info)},
		"files": {run: func(s *Session, _ bbs.Command, _ *menuRun) error {
			if s.B.Files == nil {
				s.UI.Say(view.Plain, "There are no file areas.")
				return nil
			}
			return s.fileAreas()
		}, avail: func(s *Session, _ *store.User) bool { return s.B.Files != nil }},
		"boards": {run: func(s *Session, _ bbs.Command, _ *menuRun) error {
			if !s.hasBoards() {
				s.UI.Say(view.Plain, "There are no image boards.")
				return nil
			}
			return s.imageBoards()
		}, avail: func(s *Session, _ *store.User) bool { return s.hasBoards() }},
		"doors": {run: plain((*Session).doors),
			avail: func(s *Session, _ *store.User) bool { return len(s.UI.Doors()) > 0 }},
		"chat": {run: func(s *Session, _ bbs.Command, _ *menuRun) error {
			if !s.B.LevelOf(s.User).Chat {
				s.UI.Say(view.Bad, "Chat is not open to you.")
				return nil
			}
			return s.UI.Chat("", "")
		}, avail: func(s *Session, u *store.User) bool { return s.B.LevelOf(u).Chat },
			note: func(s *Session, u *store.User) string {
				if u.Sysop {
					if ps := s.B.Pages(); len(ps) > 0 {
						return fmt.Sprintf(" (%d paging)", len(ps))
					}
				}
				return ""
			}},
		"page": {run: func(s *Session, _ bbs.Command, _ *menuRun) error {
			if !s.B.LevelOf(s.User).Page {
				s.UI.Say(view.Bad, "Paging is not open to you.")
				return nil
			}
			return s.page()
		}, avail: func(s *Session, u *store.User) bool { return s.B.LevelOf(u).Page }},
		"callers":  {run: plain((*Session).lastCallers)},
		"feedback": {run: plain((*Session).feedback)},
		"qwk":      {run: plain((*Session).offline)},
		"news": {run: func(s *Session, _ bbs.Command, _ *menuRun) error {
			if !s.hasNews() {
				s.UI.Say(view.Plain, "There is no news here.")
				return nil
			}
			return s.newsMenu()
		}, avail: func(s *Session, _ *store.User) bool { return s.hasNews() },
			note: func(s *Session, _ *store.User) string { return s.newsLabel() }},
		"headlines": {run: plain((*Session).headlines)},
		"weather":   {run: plain((*Session).weather)},
		"quotes":    {run: plain((*Session).stockQuotes)},
		"validate": {run: func(s *Session, _ bbs.Command, _ *menuRun) error {
			if !s.User.Sysop {
				s.UI.Say(view.Bad, "That is for the sysop.")
				return nil
			}
			return s.validate()
		}, avail: func(s *Session, u *store.User) bool { return s.waiting(u) > 0 },
			note: func(s *Session, u *store.User) string { return fmt.Sprintf(" (%d)", s.waiting(u)) }},
		"sysop": {run: func(s *Session, _ bbs.Command, _ *menuRun) error {
			if !s.User.Sysop {
				s.UI.Say(view.Bad, "That is for the sysop.")
				return nil
			}
			return s.sysopMenu()
		}, avail: func(_ *Session, u *store.User) bool { return u.Sysop }},
		"terminal": {run: func(s *Session, _ bbs.Command, _ *menuRun) error {
			if s.UI.Kind() != TN3270 {
				s.UI.Say(view.Plain, "That is for 3270 terminals.")
				return nil
			}
			return s.UI.TerminalInfo()
		}, avail: func(s *Session, _ *store.User) bool { return s.UI.Kind() == TN3270 }},
		"logoff": {run: plain((*Session).logoff)},
		"door": {run: func(s *Session, c bbs.Command, _ *menuRun) error {
			for _, d := range s.UI.Doors() {
				if strings.EqualFold(d.Name, c.Arg) {
					return s.UI.OpenDoor(d)
				}
			}
			s.UI.Say(view.Bad, "That door is not open to you.")
			return nil
		}},
		"screen": {run: func(s *Session, c bbs.Command, _ *menuRun) error {
			shown, err := s.UI.ShowScreen(c.Arg, true)
			if err == nil && !shown {
				s.UI.Say(view.Plain, "There is nothing to show.")
			}
			return err
		}},
		"script": {run: func(s *Session, c bbs.Command, _ *menuRun) error { return s.runScript(c.Arg) }},
		"area": {run: func(s *Session, c bbs.Command, _ *menuRun) error {
			a, err := s.B.Store.AreaByName(c.Arg)
			if err != nil || !s.B.CanRead(s.User, a) {
				s.UI.Say(view.Bad, "There is no area "+c.Arg+".")
				return nil
			}
			return s.area(a)
		}},
		"menu.gosub": {run: func(s *Session, c bbs.Command, r *menuRun) error {
			if len(r.stack) >= maxMenuDepth {
				s.UI.Say(view.Bad, "Menus nest too deep.")
				return nil
			}
			r.stack = append(r.stack, menuFrame{name: c.Arg})
			r.moved = true
			return nil
		}},
		"menu.goto": {run: func(s *Session, c bbs.Command, r *menuRun) error {
			*r.top() = menuFrame{name: c.Arg}
			r.moved = true
			return nil
		}},
		"menu.return": {run: func(s *Session, c bbs.Command, r *menuRun) error {
			if len(r.stack) > 1 {
				r.stack = r.stack[:len(r.stack)-1]
				r.moved = true
			}
			return nil
		}},
		"menu.main": {run: func(s *Session, c bbs.Command, r *menuRun) error {
			r.stack = []menuFrame{{name: "main"}}
			r.moved = true
			return nil
		}},
	}
}

// waiting is how many users wait for a sysop's validation (0 for others).
func (s *Session) waiting(u *store.User) int {
	if !u.Sysop || s.B.Conf().ValidatedLevel <= s.B.Conf().NewUserLevel {
		return 0
	}
	w, _ := s.B.Store.UsersBelow(s.B.Conf().ValidatedLevel)
	return len(w)
}

// itemAvail is whether u may choose the item now: its requirement, and
// its first command's being available.
func (s *Session) itemAvail(it *bbs.MenuItem, u *store.User) bool {
	if !bbs.MustRequire(it.Require).Allows(u) {
		return false
	}
	if c, ok := menuCommands[it.Do[0].Name]; ok && c.avail != nil {
		return c.avail(s, u)
	}
	return true
}

// runMenus runs the menus from the main menu until the caller logs off.
func (s *Session) runMenus(intro []view.Line) error {
	r := &menuRun{stack: []menuFrame{{name: "main"}}}
	for {
		if err := s.summoned(); err != nil {
			return err
		}
		u := s.refresh()
		top := r.top()
		full := s.menuNamed(top.name)
		if full == nil {
			// gone in a reload
			s.UI.Say(view.Bad, "There is no menu "+top.name+".")
			r.stack = []menuFrame{{name: "main"}}
			continue
		}
		if len(r.stack) > 1 && !bbs.MustRequire(full.Require).Allows(u) {
			s.UI.Say(view.Bad, "That menu is not for you.")
			if full.Fail != "" && full.Fail != top.name && s.menuNamed(full.Fail) != nil {
				*top = menuFrame{name: full.Fail}
			} else {
				r.stack = r.stack[:len(r.stack)-1]
			}
			continue
		}
		var items []*bbs.MenuItem
		for i := range full.Items {
			if s.itemAvail(&full.Items[i], u) {
				items = append(items, &full.Items[i])
			}
		}
		special := func(key string) *bbs.MenuItem {
			for _, it := range items {
				if it.Key == key {
					return it
				}
			}
			return nil
		}
		if !top.started {
			top.started = true
			if it := special("@FIRST"); it != nil {
				if moved, err := s.doItem(it, r); err != nil || moved {
					if err != nil {
						return err
					}
					continue
				}
			}
		}
		if it := special("@EVERY"); it != nil {
			if moved, err := s.doItem(it, r); err != nil || moved {
				if err != nil {
					return err
				}
				continue
			}
		}

		// the title, labels and prompt with their tags marked, for the
		// front ends to show (or drop); Who's online gets the title plain
		vars := s.N.Vars()
		title := screens.Marked(full.Title, vars)
		s.N.Doing(screens.Unmark(title))
		vm := &view.Menu{Title: title, Intro: intro, Screen: full.Screen, Brief: s.expert(), Line: full.Words()}
		intro = nil
		for _, it := range items {
			switch {
			case it.Special():
			case it.Hidden:
				vm.Hidden = append(vm.Hidden, it.Key)
			default:
				vm.Items = append(vm.Items, view.Item{Key: it.Key, Label: screens.Marked(it.Label, s.itemVars(it, u, vars))})
			}
		}
		if full.Prompt != "" {
			vm.Prompt = screens.Marked(full.Prompt, vars)
		}
		k, err := s.UI.Menu(vm)
		if err != nil {
			return err
		}
		var it *bbs.MenuItem
		switch k {
		case view.Back:
			// a 3270's PF3: up a menu, or off from the main menu
			if len(r.stack) > 1 {
				r.stack = r.stack[:len(r.stack)-1]
				continue
			}
			for _, x := range items {
				if !x.Special() && x.Do[0].Name == "logoff" {
					it = x
				}
			}
		case "":
			it = special("@ENTER")
		default:
			for _, x := range items {
				if !x.Special() && strings.EqualFold(x.Key, k) {
					it = x
				}
			}
		}
		if it == nil {
			continue
		}
		if _, err := s.doItem(it, r); err != nil {
			return err
		}
	}
}

// itemVars are the @-codes of an item's label: the node's, and @NOTE@.
func (s *Session) itemVars(it *bbs.MenuItem, u *store.User, vars screens.Vars) screens.Vars {
	return func(code string) (string, bool) {
		if code == "NOTE" {
			if c, ok := menuCommands[it.Do[0].Name]; ok && c.note != nil {
				return c.note(s, u), true // made here: no caller's text
			}
			return "", true
		}
		return vars(code)
	}
}

// doItem does an item's commands in turn, until one moves the menus;
// it reports whether one did.
func (s *Session) doItem(it *bbs.MenuItem, r *menuRun) (bool, error) {
	r.moved = false
	for _, c := range it.Do {
		mc, ok := menuCommands[c.Name]
		if !ok {
			return false, fmt.Errorf("menu command %s not done", c.Name)
		}
		if err := mc.run(s, c, r); err != nil {
			return r.moved, err
		}
		if r.moved {
			return true, nil
		}
	}
	return false, nil
}
