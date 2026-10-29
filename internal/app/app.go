// Package app is the BBS's screens after the login, written once for every
// front end: the menus, message areas, mail, file areas, who's online, the
// profile and the rest.  Each screen is described in internal/view's terms
// and drawn by the caller's front end (a text terminal or a 3270), which
// also supplies what only it can do: chat, transfers, doors, its display
// files and goodbye.
package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/door"
	"github.com/D-Modem/bbs/internal/screens"
	"github.com/D-Modem/bbs/internal/script"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/view"
)

// ErrLogoff: the caller logged off.
var ErrLogoff = errors.New("logged off")

// Kinds of front end.
const (
	Text   = "text"
	TN3270 = "3270"
)

// Frontend is a front end: its drawing of the screens, and what only it
// does.
type Frontend interface {
	view.UI
	// Kind is Text or TN3270.
	Kind() string
	// ShowScreen shows the sysop's display file of that name, if there is
	// one for this terminal; wait: the caller reads it before going on.
	ShowScreen(name string, wait bool) (bool, error)
	// Chat is the chat rooms from room ("": the main one), with a note to
	// begin with.
	Chat(room, note string) error
	// Doors are the doors the caller may open here.
	Doors() []*door.Door
	OpenDoor(d *door.Door) error
	// Transfers: files go by a transfer protocol (Download and Upload);
	// otherwise by the file area's command line (FileCommand).
	Transfers() bool
	// Download sends files, many at once where the protocol can.
	Download(fs []FileRef) error
	Upload(a *store.FileArea) error
	// FileCommand runs what was typed on a file area's command line; what
	// to tell the caller after.
	FileCommand(a *store.FileArea, cmd string) (string, error)
	// SendData sends a file made for the caller (a QWK packet) by a
	// transfer protocol; true if it went.
	SendData(name string, data []byte) (bool, error)
	// ReceiveData receives a file by a transfer protocol, up to max bytes;
	// nil if none came.
	ReceiveData(max int64) ([]byte, error)
	// DataCommand runs a transfer typed on a 3270's command line for
	// files made for the caller: get makes what a GET asks for (and sent,
	// called once it has gone), put takes what a PUT brings.  What to tell
	// the caller after.
	DataCommand(cmd string, get func(name string) (data []byte, sent func(), err error),
		put func(name string, data []byte) string) (string, error)
	// Terminal is the terminal's line on the profile, and what the caller
	// may change of it there.
	Terminal() string
	TerminalActions() []view.Action
	TerminalAction(id string) error
	// TerminalInfo shows all that is known of the terminal (a 3270's).
	TerminalInfo() error
	// Watch shows a sysop another node's screen as it is sent, until a
	// key (a text terminal's).
	Watch(node int) error
	// Logoff says goodbye; false if the caller thought better of it.
	Logoff() (bool, error)
}

// FileRef is a file and its area.
type FileRef struct {
	Area *store.FileArea
	File *store.File
}

// Session is a caller's time on the BBS after the login.
type Session struct {
	B    *bbs.BBS
	N    *bbs.Node
	UI   Frontend
	User *store.User

	since time.Time // the caller's last call: new files are since then
	batch []FileRef // files tagged to download together

	chanName string // the name last posted to an image board under
}

// Run is the session from the welcome to the logoff (ErrLogoff) or the
// caller's going.
func Run(b *bbs.BBS, n *bbs.Node, ui Frontend) error {
	s := &Session{B: b, N: n, UI: ui, User: n.Refresh()}
	s.since = s.User.LastLogin
	if s.since.IsZero() {
		s.since = time.Now().AddDate(0, 0, -30)
	}
	shown, err := ui.ShowScreen("welcome", ui.Kind() == TN3270)
	if err != nil {
		return err
	}
	intro := s.welcome(shown)
	// the sysop's logon script, before the menus
	if b.ScriptsDir != "" && script.For(b.ScriptsDir).Has("logon") {
		if err := s.runScript("logon"); err != nil {
			return err
		}
	}
	if m := b.Menu("main"); m != nil {
		return s.runMenu(m, 0, intro)
	}
	return s.mainMenu(intro)
}

// refresh is the caller's user read again (kept in s.User), for what the sysop
// or another node changed since; the one held if it cannot be read.
func (s *Session) refresh() *store.User {
	if u := s.N.Refresh(); u != nil {
		s.User = u
	}
	return s.User
}

// str is a string of the sysop's (or the default), filled in.
func (s *Session) str(key string) string {
	v, _ := s.N.Str(key)
	return v
}

// welcome is what the first menu says first: the welcome (unless the
// sysop's screen said it), the time left, a word on validation, the MOTD.
func (s *Session) welcome(shown bool) []view.Line {
	u := s.User
	var out []view.Line
	if !shown {
		out = append(out, view.T(view.Bright, s.str("welcome")))
		if !u.LastLogin.IsZero() {
			out = append(out, view.T(view.Dim, "Your last call was "+u.LastLogin.Format("2006-01-02 15:04")+"."))
		}
	}
	if left, ok := s.N.TimeLeft(); ok {
		out = append(out, view.T(view.Dim, fmt.Sprintf("You have %d minutes today.", int(left.Round(time.Minute).Minutes()))))
	}
	if !u.Sysop && u.Level < s.B.Conf().ValidatedLevel {
		out = append(out, view.T(view.Warn, "Your account waits for the sysop's validation; mail the sysop if you like."))
	}
	if motd := s.B.MOTDText(); motd != "" {
		out = append(out, view.L(""))
		for _, l := range strings.Split(strings.TrimRight(motd, "\n"), "\n") {
			out = append(out, view.L(bbs.Clean(l, 200)))
		}
	}
	return out
}

// mainMenu is the built-in main menu.
func (s *Session) mainMenu(intro []view.Line) error {
	for {
		if err := s.summoned(); err != nil {
			return err
		}
		u := s.refresh()
		s.N.Doing("Main menu")
		unread, _ := s.B.Store.UnreadMail(u.ID)
		mail := "E-mail"
		if unread > 0 {
			mail += fmt.Sprintf(" (%d new)", unread)
		}
		items := []view.Item{{Key: "M", Label: "Message areas"}, {Key: "E", Label: mail},
			{Key: "W", Label: "Who's online"}, {Key: "U", Label: "User list"},
			{Key: "P", Label: "Your profile"}, {Key: "I", Label: "System info"}}
		if s.B.Files != nil {
			items = append(items, view.Item{Key: "F", Label: "File areas"})
		}
		if len(s.UI.Doors()) > 0 {
			items = append(items, view.Item{Key: "D", Label: "Doors"})
		}
		if s.hasBoards() {
			items = append(items, view.Item{Key: "B", Label: "Image boards"})
		}
		lvl := s.B.LevelOf(u)
		if lvl.Chat {
			chat := "Chat"
			if u.Sysop {
				if ps := s.B.Pages(); len(ps) > 0 {
					chat += fmt.Sprintf(" (%d paging)", len(ps))
				}
			}
			items = append(items, view.Item{Key: "C", Label: chat})
		}
		if lvl.Page {
			items = append(items, view.Item{Key: "S", Label: "Page the sysop"})
		}
		waiting := 0
		if u.Sysop && s.B.Conf().ValidatedLevel > s.B.Conf().NewUserLevel {
			w, _ := s.B.Store.UsersBelow(s.B.Conf().ValidatedLevel)
			if waiting = len(w); waiting > 0 {
				items = append(items, view.Item{Key: "V", Label: fmt.Sprintf("Validate users (%d)", waiting)})
			}
		}
		items = append(items, view.Item{Key: "O", Label: "Offline mail (QWK)"},
			view.Item{Key: "L", Label: "Last callers"},
			view.Item{Key: "K", Label: "Feedback to the sysop"})
		if u.Sysop {
			items = append(items, view.Item{Key: "*", Label: "Sysop tools"})
		}
		if s.UI.Kind() == TN3270 {
			items = append(items, view.Item{Key: "T", Label: "Your terminal"})
		}
		items = append(items, view.Item{Key: "G", Label: "Goodbye"})
		k, err := s.UI.Menu(&view.Menu{Title: "Main menu", Intro: intro, Items: items, Brief: s.expert()})
		intro = nil
		if err != nil {
			return err
		}
		switch strings.ToUpper(k) {
		case "M":
			err = s.areas()
		case "B":
			if s.hasBoards() {
				err = s.imageBoards()
			}
		case "E":
			err = s.mail()
		case "O":
			err = s.offline()
		case "L":
			err = s.lastCallers()
		case "*":
			if u.Sysop {
				err = s.sysopMenu()
			}
		case "K":
			err = s.feedback()
		case "W":
			err = s.who()
		case "U":
			err = s.users()
		case "P":
			err = s.profile()
		case "I":
			err = s.info()
		case "F":
			if s.B.Files != nil {
				err = s.fileAreas()
			}
		case "D":
			err = s.doors()
		case "C":
			if lvl.Chat {
				err = s.UI.Chat("", "")
			}
		case "S":
			if lvl.Page {
				err = s.page()
			}
		case "V":
			if waiting > 0 {
				err = s.validate()
			}
		case "T":
			if s.UI.Kind() == TN3270 {
				err = s.UI.TerminalInfo()
			}
		case "G", "Q", view.Back:
			err = s.logoff()
		}
		if err != nil {
			return err
		}
	}
}

// expert: the caller wants their menus brief.
func (s *Session) expert() bool { return s.N.Prefs()["expert"] == "on" }

// logoff: ErrLogoff once the front end has said goodbye.
func (s *Session) logoff() error {
	ok, err := s.UI.Logoff()
	if err != nil || !ok {
		return err
	}
	s.B.Log.Printf("node %d: %s logged off", s.N.Num, s.User.Name)
	return ErrLogoff
}

// runMenu runs a menu of the sysop's until it is left (back) or the caller
// logs off.
func (s *Session) runMenu(full *bbs.Menu, depth int, intro []view.Line) error {
	vars := s.N.Vars()
	for {
		if err := s.summoned(); err != nil {
			return err
		}
		u := s.refresh()
		m := s.B.MenuFor(full, u)
		// the title, labels and prompt with their tags marked, for the
		// front ends to show (or drop); Who's online gets the title plain
		title := screens.Marked(m.Title, vars)
		s.N.Doing(screens.Unmark(title))
		vm := &view.Menu{Title: title, Intro: intro, Screen: m.Screen, Brief: s.expert()}
		intro = nil
		for _, it := range m.Items {
			vm.Items = append(vm.Items, view.Item{Key: it.Key, Label: screens.Marked(it.Label, vars)})
		}
		if m.Prompt != "" {
			vm.Prompt = screens.Marked(m.Prompt, vars)
		}
		k, err := s.UI.Menu(vm)
		if err != nil {
			return err
		}
		if k == view.Back {
			// a 3270's PF3: up a menu, or off from the top one
			if depth > 0 {
				return nil
			}
			for _, it := range m.Items {
				if it.Action == "logoff" {
					k = it.Key
				}
			}
		}
		it := m.Item(k)
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
func (s *Session) do(it *bbs.MenuItem, depth int) error {
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
	case "terminal":
		if s.UI.Kind() != TN3270 {
			s.UI.Say(view.Plain, "That is for 3270 terminals.")
			return nil
		}
		return s.UI.TerminalInfo()
	case "files":
		if s.B.Files == nil {
			s.UI.Say(view.Plain, "There are no file areas.")
			return nil
		}
		return s.fileAreas()
	case "doors":
		return s.doors()
	case "boards":
		if !s.hasBoards() {
			s.UI.Say(view.Plain, "There are no image boards.")
			return nil
		}
		return s.imageBoards()
	case "door":
		for _, d := range s.UI.Doors() {
			if strings.EqualFold(d.Name, it.Arg) {
				return s.UI.OpenDoor(d)
			}
		}
		s.UI.Say(view.Bad, "That door is not open to you.")
	case "chat":
		return s.UI.Chat("", "")
	case "callers":
		return s.lastCallers()
	case "sysop":
		if !s.User.Sysop {
			s.UI.Say(view.Bad, "That is for the sysop.")
			return nil
		}
		return s.sysopMenu()
	case "feedback":
		return s.feedback()
	case "qwk":
		return s.offline()
	case "page":
		return s.page()
	case "logoff":
		return s.logoff()
	case "menu":
		if depth >= 10 {
			s.UI.Say(view.Bad, "Menus nest too deep.")
			return nil
		}
		sub := s.B.Menu(strings.ToLower(it.Arg))
		if !bbs.MustRequire(sub.Require).Allows(s.User) {
			s.UI.Say(view.Bad, "That menu is not for you.")
			return nil
		}
		return s.runMenu(sub, depth+1, nil)
	case "screen":
		shown, err := s.UI.ShowScreen(it.Arg, true)
		if err == nil && !shown {
			s.UI.Say(view.Plain, "There is nothing to show.")
		}
		return err
	case "script":
		return s.runScript(it.Arg)
	case "area":
		a, err := s.B.Store.AreaByName(it.Arg)
		if err != nil || !s.B.CanRead(s.User, a) {
			s.UI.Say(view.Bad, "There is no area "+it.Arg+".")
			return nil
		}
		return s.area(a)
	}
	return nil
}

// runScript runs a script of the sysop's for the caller.
func (s *Session) runScript(name string) error {
	if s.B.ScriptsDir == "" {
		s.UI.Say(view.Bad, "There are no scripts.")
		return nil
	}
	err := script.For(s.B.ScriptsDir).Run(name, s.B, s.N, s.UI)
	s.refresh()
	return err
}

// doors: the list of doors, and into one
func (s *Session) doors() error {
	if s.UI.Kind() == TN3270 {
		s.UI.Say(view.Plain, "Doors are for text terminals (telnet, modem).")
		return nil
	}
	for {
		s.N.Doing("Doors")
		ds := s.UI.Doors()
		t := &view.Table{Title: "Doors", Empty: "(none)", Pick: "Door number",
			Prompt:  "Door number (Enter to go back): ",
			Columns: []view.Column{{Title: "Door", Width: 20}, {Title: "Description"}}}
		for i, d := range ds {
			t.Rows = append(t.Rows, view.Row{Num: fmt.Sprint(i + 1), Cells: []string{d.Name, d.Description}})
		}
		c, err := s.UI.Table(t)
		if err != nil || c.Action != view.Pick {
			return err
		}
		if c.Row < 0 {
			s.UI.Say(view.Plain, "No such door.")
			continue
		}
		if err := s.UI.OpenDoor(ds[c.Row]); err != nil {
			return err
		}
	}
}
