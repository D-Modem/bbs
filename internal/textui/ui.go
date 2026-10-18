package textui

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/app"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// Options are a session's settings from its listener.
type Options struct {
	// a trusted user unknown here gets an account of its own
	CreateTrusted bool
	// the logo (a PNG; "": the system's name), on sixel terminals
	LogoFile string
}

type session struct {
	b    *bbs.BBS
	n    *bbs.Node
	t    *Term
	c    *bbs.Caller
	o    Options
	user *store.User

	chatting bool // in a chat room: its lines shown as such
}

// errHangup: the caller logged off.
var errHangup = errors.New("logged off")

// Run is a caller's session on a text terminal, from the banner to the
// goodbye.  It returns when the caller logs off or goes away.
func Run(b *bbs.BBS, n *bbs.Node, t *Term, o Options) error {
	s := &session{b: b, n: n, t: t, c: n.Caller, o: o}
	t.SetEcho(n.Echo)
	n.SetEchoing()
	t.Idle = b.Conf().IdleTimeout
	err := s.run()
	if errors.Is(err, errHangup) {
		err = nil
	}
	if errors.Is(err, ErrIdle) {
		t.Println("\n" + t.C(Red, "Timed out.  Goodbye."))
	}
	t.Flush()
	return err
}

// run is the session from the logon screen: the login, the caller's
// preferences, then app.Run with Notify's lines shown while keys are
// awaited.  A logoff is errHangup.
func (s *session) run() error {
	t := s.t
	t.Clear()
	t.PausePrompt = s.str("pause")
	if err := s.logon(); err != nil {
		return err
	}
	if err := s.login(); err != nil {
		return err
	}
	s.applyPrefs()
	t.Notify = s.notify
	defer func() { t.Notify = nil }()
	err := app.Run(s.b, s.n, s)
	if errors.Is(err, app.ErrLogoff) {
		err = errHangup
	}
	return err
}

// logon is the screen before the login: the sysop's, or the system's
// name, banner and how the caller came.
func (s *session) logon() error {
	t := s.t
	if shown, err := s.screen("logon"); err != nil {
		return err
	} else if !shown {
		if t.Sixel {
			t.Picture(sixelLogo(t, s.b.Conf().Name, s.o.LogoFile))
		}
		t.Println(t.C(Cyan, s.b.Conf().Name))
		if s.b.Conf().Banner != "" {
			t.Println(s.b.Conf().Banner)
		}
		via := "Node " + strconv.Itoa(s.n.Num) + ", " + s.c.Via
		if baud := s.c.Baud(); baud != "" {
			via += " at " + baud + " bit/s"
		}
		t.Println(t.C(Dim, via))
		t.Println("")
	}
	return nil
}

// becomePETSCII: the caller turned out to have a Commodore: the screen is
// cleared (and what the questions left on it), and begun again.
func (s *session) becomePETSCII() error {
	t := s.t
	t.WatchPETSCII, t.Upper = false, false
	t.SetPETSCII(true, 40)
	s.n.SetTerminal(s.c.Via, "petscii")
	s.b.Log.Printf("node %d: a Commodore (PETSCII)", s.n.Num)
	return s.logon()
}

// settleTerminal decides, from a name typed while it was not known whether
// the terminal is a Commodore, what it is: a name in lower case is ASCII;
// one all in capitals is what a Commodore's unshifted keys send, or ASCII
// in capitals, and DEL tells which.  It returns the name as the caller
// typed it.
func (s *session) settleTerminal(name string) (string, error) {
	t := s.t
	t.WatchPETSCII = false
	defer func() { t.Upper = false }()
	if strings.ToUpper(name) != name || strings.ToLower(name) == name {
		return name, nil // lower case, or no letters
	}
	t.Print("PRESS DEL (OR BACKSPACE): ")
	b, err := t.RawKey(30 * time.Second)
	if err != nil && !errors.Is(err, ErrIdle) {
		return "", err
	}
	t.Print("\n")
	if b != 0x14 {
		return name, nil
	}
	if err := s.becomePETSCII(); err != nil {
		return "", err
	}
	// its unshifted letters are lower case
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		case r >= 'a' && r <= 'z':
			return r - ('a' - 'A')
		}
		return r
	}, name)
	t.Println(s.namePrompt() + name)
	return name, nil
}

// namePrompt is the login's first question.
func (s *session) namePrompt() string {
	t := s.t
	p := "Name: "
	if v, set := s.n.StrMarked("name_prompt"); set {
		p = v
	}
	if s.b.Conf().NewUsers {
		p = "Name" + t.C(Dim, " (or NEW)") + ": "
		if v, set := s.n.StrMarked("new_name_prompt"); set {
			p = v
		}
	}
	return p
}

// login: as vouched for, with the credentials given, or by asking
func (s *session) login() error {
	t := s.t
	// a terminal not known by the end is not a Commodore
	defer func() { t.WatchPETSCII, t.Upper = false, false }()
	if s.c.TrustedUser != "" {
		u, err := s.n.LoginTrusted(s.c.TrustedUser, s.o.CreateTrusted)
		if err == nil {
			s.user = u
			return nil
		}
		s.b.Log.Printf("node %d: upstream user %q: %v", s.n.Num, s.c.TrustedUser, err)
		t.Println(t.C(Red, "You could not be logged in: "+err.Error()))
		return errHangup
	}
	if s.c.LoginUser != "" && s.c.LoginPassword != "" {
		if u, err := s.n.Login(s.c.LoginUser, s.c.LoginPassword); err == nil {
			s.user = u
			return nil
		}
	}

	t.SetDeadline(time.Now().Add(s.b.Conf().LoginTimeout))
	defer t.SetDeadline(time.Time{})
	for try := 0; try < s.b.Conf().Attempts; try++ {
		t.Print(s.namePrompt())
		l, err := t.ReadLine(LineOpts{Max: bbs.MaxNameLen, Initial: s.c.LoginUser})
		s.c.LoginUser = ""
		if errors.Is(err, ErrPETSCII) {
			if err := s.becomePETSCII(); err != nil {
				return err
			}
			try--
			continue
		}
		if err != nil {
			return err
		}
		name := strings.TrimSpace(l.Text)
		if name == "" {
			try--
			continue
		}
		if t.WatchPETSCII {
			if name, err = s.settleTerminal(name); err != nil {
				return err
			}
		}
		if strings.EqualFold(name, "new") {
			if !s.b.Conf().NewUsers {
				t.Println(t.C(Red, "This system takes no new users."))
				continue
			}
			ok, err := s.register()
			if err != nil || ok {
				return err
			}
			continue
		}
		pw, err := t.Password(s.str("password_prompt"))
		if err != nil {
			return err
		}
		t.Print(t.C(Dim, "Checking..."))
		t.Flush()
		u, err := s.n.Login(name, pw)
		t.Print("\r           \r")
		if err == nil {
			s.user = u
			return nil
		}
		t.Println(t.C(Red, err.Error()+"."))
		if errors.Is(err, bbs.ErrThrottled) {
			break
		}
	}
	t.Println("Goodbye.")
	return errHangup
}

// register: a new user's account; false if they gave up
func (s *session) register() (bool, error) {
	t := s.t
	t.Println("")
	if _, err := s.screen("newuser"); err != nil {
		return false, err
	}
	t.Println(t.C(Cyan, "New user"))
	t.Println("Pick a name: letters, digits, spaces, dots and dashes, 2 to 24 long.")
	var name string
	for {
		var err error
		name, err = t.Input("Name (Enter to give up): ", bbs.MaxNameLen)
		if err != nil || name == "" {
			return false, err
		}
		if msg := s.n.CheckNewName(name); msg != "" {
			t.Println(t.C(Red, msg))
			continue
		}
		if _, err := s.b.Store.UserByName(name); err == nil {
			t.Println(t.C(Red, "That name is taken."))
			continue
		}
		break
	}
	var pw string
	for {
		var err error
		pw, err = t.Password("Password (8 or more characters): ")
		if err != nil {
			return false, err
		}
		if pw == "" {
			return false, nil
		}
		if msg := bbs.ValidPassword(pw, name); msg != "" {
			t.Println(t.C(Red, msg))
			continue
		}
		again, err := t.Password("Again: ")
		if err != nil {
			return false, err
		}
		if again != pw {
			t.Println(t.C(Red, "They differ."))
			continue
		}
		break
	}
	loc, err := t.Input("Where are you calling from? ", bbs.MaxLocationLen)
	if err != nil {
		return false, err
	}
	u, err := s.n.Register(name, pw, loc)
	if err != nil {
		t.Println(t.C(Red, err.Error()+"."))
		return false, nil
	}
	s.user = u
	return true, nil
}

// date is tm as a message's date: "2006-01-02 15:04".
func date(tm time.Time) string { return tm.Format("2006-01-02 15:04") }

// edit: the line editor.  lines are the start (a quote); nil if the
// caller gave up.
func (s *session) edit(lines []string) ([]string, error) {
	t := s.t
	width := min(t.Cols-1, bbs.MaxLineLen)
	t.Println(t.C(Dim, "Type your message.  On a line of its own: /S saves, /A gives up,"))
	t.Println(t.C(Dim, "/L lists what you have, /D deletes the last line, /C clears all."))
	t.Println(t.C(Dim, strings.Repeat("-", width)))
	for _, l := range lines {
		t.Println(l)
	}
	carry := ""
	for {
		if len(lines) >= bbs.MaxBodyLines {
			t.Println(t.C(Red, "That is as long as a message gets."))
			carry = ""
		}
		l, err := t.ReadLine(LineOpts{Max: width, Initial: carry, Wrap: true})
		if err != nil {
			return nil, err
		}
		carry = l.Carry
		if l.Carry == "" && len(l.Text) == 2 && l.Text[0] == '/' {
			switch strings.ToUpper(l.Text[1:]) {
			case "S":
				return lines, nil
			case "A":
				ok, err := t.YesNo("Give up this message?", false)
				if err != nil {
					return nil, err
				}
				if ok {
					return nil, nil
				}
				continue
			case "L":
				t.Println(t.C(Dim, strings.Repeat("-", width)))
				t.Page(lines)
				continue
			case "D":
				if len(lines) > 0 {
					lines = lines[:len(lines)-1]
					t.Println(t.C(Dim, "(the last line is gone)"))
				}
				continue
			case "C":
				lines = nil
				t.Println(t.C(Dim, "(cleared)"))
				continue
			}
		}
		if len(lines) < bbs.MaxBodyLines {
			lines = append(lines, l.Text)
		}
	}
}

// Console is the screens on a terminal of the machine's own, with no
// login: for bbs config's screens (internal/confui), which want a
// view.UI and nothing of a caller's.
func Console(b *bbs.BBS, n *bbs.Node, t *Term) view.UI {
	t.PausePrompt = "[Press a key]"
	return &session{b: b, n: n, t: t, c: n.Caller}
}
