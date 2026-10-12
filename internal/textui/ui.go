package textui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
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
	t.Idle = b.IdleTimeout
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
	s.welcome()
	return s.mainMenu()
}

// logon is the screen before the login: the sysop's, or the system's
// name, banner and how the caller came.
func (s *session) logon() error {
	t := s.t
	if shown, err := s.screen("logon"); err != nil {
		return err
	} else if !shown {
		if t.Sixel {
			t.Picture(sixelLogo(t, s.b.Name, s.o.LogoFile))
		}
		t.Println(t.C(Cyan, s.b.Name))
		if s.b.Banner != "" {
			t.Println(s.b.Banner)
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
	if v, set := s.n.Str("name_prompt"); set {
		p = v
	}
	if s.b.NewUsers {
		p = "Name" + t.C(Dim, " (or NEW)") + ": "
		if v, set := s.n.Str("new_name_prompt"); set {
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

	t.SetDeadline(time.Now().Add(s.b.LoginTimeout))
	defer t.SetDeadline(time.Time{})
	for try := 0; try < s.b.Attempts; try++ {
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
			if !s.b.NewUsers {
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
		if msg := bbs.ValidName(name); msg != "" {
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

func (s *session) welcome() {
	t := s.t
	u := s.user
	t.Println("")
	if shown, _ := s.screen("welcome"); !shown {
		if v, set := s.n.Str("welcome"); set {
			t.Println(v)
		} else {
			t.Println("Welcome, " + t.C(Yellow, u.Name) + ".")
		}
		if !u.LastLogin.IsZero() {
			t.Println(t.C(Dim, "Your last call was "+u.LastLogin.Format("2006-01-02 15:04")+"."))
		}
	}
	if motd := s.b.MOTDText(); motd != "" {
		t.Println("")
		var lines []string
		for _, l := range strings.Split(strings.TrimRight(motd, "\n"), "\n") {
			lines = append(lines, bbs.Clean(l, 200))
		}
		t.Page(lines)
	}
	s.user = s.n.Refresh()
}

func (s *session) mainMenu() error {
	t := s.t
	t.Notify = s.notify
	defer func() { t.Notify = nil }()
	if m := s.b.Menus["main"]; m != nil {
		return s.runMenu(m, 0)
	}
	for {
		u := s.n.Refresh()
		s.user = u
		s.n.Doing("Main menu")
		unread, _ := s.b.Store.UnreadMail(u.ID)
		mail := "E-mail"
		if unread > 0 {
			mail += fmt.Sprintf(" (%d new)", unread)
		}
		items := []choice{{"M", "Message areas"}, {"E", mail},
			{"W", "Who's online"}, {"U", "User list"},
			{"P", "Your profile"}, {"I", "System info"}}
		if s.b.Files != nil {
			items = append(items, choice{"F", "File areas"})
		}
		if len(s.doorsFor()) > 0 {
			items = append(items, choice{"D", "Doors"})
		}
		chat := "Chat"
		if u.Sysop {
			if ps := s.b.Pages(); len(ps) > 0 {
				chat += fmt.Sprintf(" (%d paging)", len(ps))
			}
		}
		items = append(items, choice{"C", chat}, choice{"S", "Page the sysop"})
		items = append(items, choice{"G", "Goodbye"})
		k, err := s.menu("Main menu", items, "\n"+s.str("command_prompt"), true)
		if err != nil {
			return err
		}
		switch k {
		case 'M':
			err = s.areas()
		case 'E':
			err = s.mail()
		case 'W':
			err = s.who()
		case 'F':
			if s.b.Files != nil {
				err = s.fileAreas()
			}
		case 'C':
			err = s.chat("")
		case 'S':
			err = s.page()
		case 'D':
			if len(s.doorsFor()) > 0 {
				err = s.doors()
			}
		case 'U':
			err = s.users()
		case 'P':
			err = s.profile()
		case 'I':
			err = s.info()
		case 'G', 'Q':
			if err := s.logoff(); err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
	}
}

func date(tm time.Time) string { return tm.Format("2006-01-02 15:04") }

// areas: the list of message areas
func (s *session) areas() error {
	t := s.t
	for {
		s.n.Doing("Message areas")
		areas, err := s.b.AreasFor(s.user)
		if err != nil {
			return err
		}
		t.Println("")
		t.Println(t.C(Cyan, "Message areas"))
		if len(areas) == 0 {
			t.Println("There are no message areas yet.")
		}
		var lines []string
		for i, a := range areas {
			newMark := ""
			if a.New > 0 {
				newMark = t.C(Green, fmt.Sprintf(" %d new", a.New))
			}
			if t.Narrow() {
				lines = append(lines, fmt.Sprintf("%s %s %4d%s", t.C(Yellow, fmt.Sprintf("%3d", i+1)),
					bbs.Fit(a.Name, 22), a.Messages, newMark))
				continue
			}
			lines = append(lines, fmt.Sprintf("%s %s %s%s", t.C(Yellow, fmt.Sprintf("%3d", i+1)),
				bbs.Fit(a.Name, 20), t.C(Dim, bbs.Fit(bbs.Trunc(a.Description, 36), 36)),
				fmt.Sprintf(" %4d", a.Messages)+newMark))
		}
		t.Page(lines)
		prompt := "\nArea number, [N]ew scan, [Q]uit: "
		if s.user.Sysop {
			prompt = "\nArea number, [N]ew scan, [+] add area, [Q]uit: "
		}
		cmd, err := t.Input(prompt, 6)
		if err != nil {
			return err
		}
		switch strings.ToUpper(cmd) {
		case "", "Q":
			return nil
		case "N":
			for _, a := range areas {
				if a.New > 0 {
					t.Println(t.C(Cyan, "\n"+a.Name))
					if quit, err := s.readNew(a.Area); err != nil || quit {
						return err
					}
				}
			}
			t.Println("No more new messages.")
			continue
		case "+":
			if s.user.Sysop {
				if err := s.addArea(); err != nil {
					return err
				}
			}
			continue
		}
		i, err := strconv.Atoi(cmd)
		if err != nil || i < 1 || i > len(areas) {
			t.Println(t.C(Red, "No such area."))
			continue
		}
		if err := s.area(areas[i-1].Area); err != nil {
			return err
		}
	}
}

func (s *session) addArea() error {
	t := s.t
	name, err := t.Input("Name of the new area: ", 30)
	if err != nil || name == "" {
		return err
	}
	desc, err := t.Input("Description: ", 60)
	if err != nil {
		return err
	}
	only, err := t.YesNo("Only the sysop posts?", false)
	if err != nil {
		return err
	}
	a, err := s.b.Store.CreateArea(bbs.Clean(name, 30), bbs.Clean(desc, 60), only)
	if err != nil {
		t.Println(t.C(Red, err.Error()))
		return nil
	}
	s.b.Log.Printf("node %d: %s added area %s", s.n.Num, s.user.Name, a.Name)
	return nil
}

// area: one message area's menu
func (s *session) area(a *store.Area) error {
	t := s.t
	for {
		s.n.Doing("In " + a.Name)
		a2, err := s.b.Store.Area(a.ID)
		if err != nil {
			return err
		}
		a = a2
		nw, _ := s.b.Store.NewCount(s.user.ID, a.ID)
		t.Println("")
		group := ""
		if a.Newsgroup != "" {
			group = t.C(Dim, " ("+a.Newsgroup+")")
		}
		if a.EchoTag != "" {
			group += t.C(Dim, " (echo "+a.EchoTag+")")
		}
		t.Printf("%s%s: %d messages, %d new\n", t.C(Cyan, a.Name), group, a.Messages, nw)
		menu := "[R]ead new, [L]ist, [#] read a number, "
		if bbs.CanPost(s.user, a) {
			menu += "[P]ost, "
		}
		cmd, err := t.Input(menu+"[Q]uit: ", 10)
		if err != nil {
			return err
		}
		switch strings.ToUpper(cmd) {
		case "", "Q":
			return nil
		case "R":
			if _, err := s.readNew(a); err != nil {
				return err
			}
		case "L":
			if err := s.list(a); err != nil {
				return err
			}
		case "P":
			if bbs.CanPost(s.user, a) {
				if err := s.post(a, "All", "", nil, 0); err != nil {
					return err
				}
			}
		default:
			id, err := strconv.ParseInt(strings.TrimPrefix(cmd, "#"), 10, 64)
			if err != nil {
				t.Println(t.C(Red, "What?"))
				continue
			}
			m, err := s.b.Store.Message(id)
			if err != nil || m.AreaID != a.ID {
				t.Println(t.C(Red, "No such message here."))
				continue
			}
			if _, err := s.read(a, m); err != nil {
				return err
			}
		}
	}
}

// readNew reads an area from the first unread message; true if the caller
// quit.
func (s *session) readNew(a *store.Area) (bool, error) {
	last, err := s.b.Store.LastRead(s.user.ID, a.ID)
	if err != nil {
		return false, err
	}
	ms, err := s.b.Store.Messages(a.ID, last, 1)
	if err != nil {
		return false, err
	}
	if len(ms) == 0 {
		s.t.Println("No new messages.")
		return false, nil
	}
	return s.read(a, ms[0])
}

// list: an area's messages, a screen at a time
func (s *session) list(a *store.Area) error {
	t := s.t
	var after int64
	per := t.Rows - 3
	if per < 5 {
		per = 5
	}
	for {
		ms, err := s.b.Store.Messages(a.ID, after, per)
		if err != nil {
			return err
		}
		if len(ms) == 0 {
			t.Println("No more messages.")
			return nil
		}
		if t.Narrow() {
			t.Println(t.C(Dim, fmt.Sprintf("%5s %-10s %s", "#", "From", "Subject")))
		} else {
			t.Println(t.C(Dim, fmt.Sprintf("%6s %-16s %-36s %s", "#", "From", "Subject", "Date")))
		}
		for _, m := range ms {
			if t.Narrow() {
				t.Printf("%s %s %s\n", t.C(Yellow, fmt.Sprintf("%5d", m.ID)), bbs.Fit(m.From, 10),
					bbs.Trunc(m.Subject, t.Cols-18))
			} else {
				t.Printf("%s %s %s %s\n", t.C(Yellow, fmt.Sprintf("%6d", m.ID)), bbs.Fit(m.From, 16),
					bbs.Fit(m.Subject, 36), m.Created.Format("01-02 15:04"))
			}
			after = m.ID
		}
		cmd, err := t.Input("Number to read, Enter for more, [Q]uit: ", 10)
		if err != nil {
			return err
		}
		switch strings.ToUpper(cmd) {
		case "":
			continue
		case "Q":
			return nil
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(cmd, "#"), 10, 64)
		if err != nil {
			continue
		}
		m, err := s.b.Store.Message(id)
		if err != nil || m.AreaID != a.ID {
			t.Println(t.C(Red, "No such message here."))
			continue
		}
		_, err = s.read(a, m)
		return err
	}
}

func (s *session) showMessage(m *store.Message, where string) error {
	t := s.t
	t.Clear()
	t.Println(t.C(Dim, where))
	t.Println(t.C(Cyan, "From: ") + t.C(Yellow, m.From) + "   " + t.C(Cyan, "To: ") + m.To)
	t.Println(t.C(Cyan, "Subj: ") + t.C(White, m.Subject))
	t.Println(t.C(Cyan, "Date: ") + date(m.Created))
	t.Println(t.C(Dim, strings.Repeat("-", min(t.Cols-1, 79))))
	_, err := t.Page(bbs.Wrap(m.Body, t.Cols-1))
	return err
}

// read: messages of an area from m on; true if the caller quit
func (s *session) read(a *store.Area, m *store.Message) (bool, error) {
	t := s.t
	for {
		s.n.Doing("Reading " + a.Name)
		where := fmt.Sprintf("%s  #%d", a.Name, m.ID)
		if err := s.showMessage(m, where); err != nil {
			return false, err
		}
		s.b.Store.SetLastRead(s.user.ID, a.ID, m.ID)
		canDelete := s.user.Sysop || s.user.ID == m.FromID
		prompt := "\n[N]ext, [P]revious, "
		if bbs.CanPost(s.user, a) {
			prompt += "[R]eply, "
		}
		prompt += "[M]ail reply, "
		if canDelete {
			prompt += "[D]elete, "
		}
		t.Print(prompt + "[Q]uit: ")
		k, err := t.Key()
		if err != nil {
			return false, err
		}
		t.Println("")
		switch k {
		case 'N', '\r', ' ':
			next, err := s.b.Store.Neighbour(a.ID, m.ID, true)
			if errors.Is(err, store.ErrNotFound) {
				t.Println("That was the last message.")
				return false, nil
			} else if err != nil {
				return false, err
			}
			m = next
		case 'P', '-':
			prev, err := s.b.Store.Neighbour(a.ID, m.ID, false)
			if errors.Is(err, store.ErrNotFound) {
				t.Println("That is the first message.")
				continue
			} else if err != nil {
				return false, err
			}
			m = prev
		case 'R':
			if bbs.CanPost(s.user, a) {
				if err := s.post(a, m.From, bbs.ReplySubject(m.Subject),
					bbs.Quote(m.From, m.Body, 8), m.ID); err != nil {
					return false, err
				}
			}
		case 'M':
			if err := s.compose(m.From, bbs.ReplySubject(m.Subject),
				bbs.Quote(m.From, m.Body, 8), 0); err != nil {
				return false, err
			}
		case 'D':
			if canDelete {
				ok, err := t.YesNo("Delete this message?", false)
				if err != nil {
					return false, err
				}
				if ok {
					if err := s.n.DeleteMessage(m); err != nil {
						t.Println(t.C(Red, err.Error()))
					}
					return false, nil
				}
			}
		case 'Q':
			return true, nil
		}
	}
}

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

// post: a message to an area
func (s *session) post(a *store.Area, to, subject string, quote []string, replyTo int64) error {
	t := s.t
	s.n.Doing("Writing in " + a.Name)
	t.Println("")
	who, err := t.Input("To ["+to+"]: ", bbs.MaxNameLen)
	if err != nil {
		return err
	}
	if who != "" {
		to = who
	}
	t.Print("Subject: ")
	l, err := t.ReadLine(LineOpts{Max: bbs.MaxSubjectLen, Initial: subject})
	if err != nil {
		return err
	}
	subject = strings.TrimSpace(l.Text)
	if subject == "" {
		t.Println("No subject: given up.")
		return nil
	}
	body, err := s.edit(quote)
	if err != nil || body == nil {
		return err
	}
	id, err := s.n.Post(a, to, subject, body, replyTo)
	if err != nil {
		t.Println(t.C(Red, err.Error()+"."))
		return nil
	}
	t.Println(t.C(Green, fmt.Sprintf("Posted as #%d.", id)))
	return nil
}

// compose: a private mail
func (s *session) compose(to, subject string, quote []string, replyTo int64) error {
	t := s.t
	s.n.Doing("Writing mail")
	t.Println("")
	for {
		prompt := "To (user name, Enter to give up): "
		if s.b.Netmail != nil {
			prompt = "To (user name, or Name@zone:net/node for FidoNet): "
		}
		if to != "" {
			prompt = "To [" + to + "]: "
		}
		who, err := t.Input(prompt, bbs.MaxMailToLen)
		if err != nil {
			return err
		}
		if who != "" {
			to = who
		}
		if to == "" {
			return nil
		}
		if _, _, ok := bbs.SplitNetmail(to); ok && s.b.Netmail != nil {
			break
		}
		if _, err := s.b.Store.UserByName(to); err != nil {
			t.Println(t.C(Red, "There is no such user."))
			to = ""
			continue
		}
		break
	}
	t.Print("Subject: ")
	l, err := t.ReadLine(LineOpts{Max: bbs.MaxSubjectLen, Initial: subject})
	if err != nil {
		return err
	}
	subject = strings.TrimSpace(l.Text)
	if subject == "" {
		t.Println("No subject: given up.")
		return nil
	}
	body, err := s.edit(quote)
	if err != nil || body == nil {
		return err
	}
	if err := s.n.SendMail(to, subject, body, replyTo); err != nil {
		t.Println(t.C(Red, err.Error()+"."))
		return nil
	}
	t.Println(t.C(Green, "Sent."))
	return nil
}

// mail: the inbox
func (s *session) mail() error {
	t := s.t
	for {
		s.n.Doing("Reading mail")
		inbox, err := s.b.Store.Inbox(s.user.ID)
		if err != nil {
			return err
		}
		t.Println("")
		t.Println(t.C(Cyan, "Your mail"))
		if len(inbox) == 0 {
			t.Println("You have no mail.")
		}
		var lines []string
		for i, m := range inbox {
			flag := " "
			if !m.Read {
				flag = t.C(Green, "N")
			}
			if t.Narrow() {
				lines = append(lines, fmt.Sprintf("%s %s %s %s", t.C(Yellow, fmt.Sprintf("%3d", i+1)), flag,
					bbs.Fit(m.From, 12), bbs.Trunc(m.Subject, t.Cols-20)))
				continue
			}
			lines = append(lines, fmt.Sprintf("%s %s %s %s %s", t.C(Yellow, fmt.Sprintf("%3d", i+1)), flag,
				bbs.Fit(m.From, 16), bbs.Fit(m.Subject, 40), m.Created.Format("01-02 15:04")))
		}
		t.Page(lines)
		cmd, err := t.Input("\nNumber to read, [S]end, [Q]uit: ", 6)
		if err != nil {
			return err
		}
		switch strings.ToUpper(cmd) {
		case "", "Q":
			return nil
		case "S":
			if err := s.compose("", "", nil, 0); err != nil {
				return err
			}
			continue
		}
		i, err := strconv.Atoi(cmd)
		if err != nil || i < 1 || i > len(inbox) {
			t.Println(t.C(Red, "No such mail."))
			continue
		}
		if err := s.readMail(inbox, i-1); err != nil {
			return err
		}
	}
}

func (s *session) readMail(inbox []*store.Message, i int) error {
	t := s.t
	for i < len(inbox) {
		m := inbox[i]
		if err := s.showMessage(m, fmt.Sprintf("Mail %d of %d", i+1, len(inbox))); err != nil {
			return err
		}
		s.b.Store.MarkMailRead(s.user.ID, m.ID)
		t.Print("\n[N]ext, [R]eply, [D]elete, [Q]uit: ")
		k, err := t.Key()
		if err != nil {
			return err
		}
		t.Println("")
		switch k {
		case 'N', '\r', ' ':
			i++
		case 'R':
			if err := s.compose(m.From, bbs.ReplySubject(m.Subject),
				bbs.Quote(m.From, m.Body, 8), m.ID); err != nil {
				return err
			}
		case 'D':
			if err := s.b.Store.DeleteMail(s.user.ID, m.ID); err != nil {
				return err
			}
			t.Println("Deleted.")
			return nil
		case 'Q':
			return nil
		}
	}
	t.Println("No more mail.")
	return nil
}

func (s *session) who() error {
	t := s.t
	t.Println("")
	t.Println(t.C(Cyan, "Who's online"))
	if t.Narrow() {
		t.Println(t.C(Dim, fmt.Sprintf("%4s  %-16s %-8s %s", "Node", "User", "Via", "Time")))
	} else {
		t.Println(t.C(Dim, fmt.Sprintf("%4s  %-20s %-16s %-6s %s", "Node", "User", "Via", "Time", "Doing")))
	}
	for _, n := range s.b.Nodes() {
		name := n.User
		if name == "" {
			name = "(logging in)"
		}
		mins := int(time.Since(n.Since).Minutes())
		if t.Narrow() {
			t.Printf("%4d  %s %s %s\n", n.Num, bbs.Fit(name, 16), bbs.Fit(n.Via, 8), fmt.Sprintf("%dm", mins))
			continue
		}
		t.Printf("%4d  %s %s %-6s %s\n", n.Num, bbs.Fit(name, 20), bbs.Fit(n.Via, 16),
			fmt.Sprintf("%dm", mins), bbs.Trunc(n.Activity, t.Cols-52))
	}
	return s.messageNode()
}

func (s *session) users() error {
	t := s.t
	us, err := s.b.Store.Users()
	if err != nil {
		return err
	}
	t.Println("")
	lines := []string{t.C(Dim, fmt.Sprintf("%-24s %-26s %6s  %s", "User", "From", "Posts", "Last call"))}
	if t.Narrow() {
		lines = []string{t.C(Dim, fmt.Sprintf("%-20s %-12s %5s", "User", "From", "Posts"))}
	}
	for _, u := range us {
		last := "never"
		if !u.LastLogin.IsZero() {
			last = u.LastLogin.Format("2006-01-02")
		}
		name := bbs.Fit(u.Name, 24)
		if t.Narrow() {
			name = bbs.Fit(u.Name, 20)
		}
		if u.Sysop {
			name = t.C(Yellow, name)
		}
		if t.Narrow() {
			lines = append(lines, fmt.Sprintf("%s %s %5d", name, bbs.Fit(u.Location, 12), u.Posts))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s %6d  %s", name, bbs.Fit(u.Location, 26), u.Posts, last))
	}
	if ok, err := t.Page(lines); err != nil || !ok {
		return err
	}
	return t.Pause()
}

func (s *session) profile() error {
	t := s.t
	for {
		u := s.n.Refresh()
		s.user = u
		s.n.Doing("Profile")
		t.Println("")
		t.Println(t.C(Cyan, "Your profile"))
		t.Printf("  Name:      %s\n", u.Name)
		t.Printf("  From:      %s\n", u.Location)
		t.Printf("  Member:    since %s, %d calls, %d posts\n", u.Created.Format("2006-01-02"), u.Logins, u.Posts)
		onOff := map[bool]string{true: "on", false: "off"}
		menu := "\n[L]ocation, [A]NSI on/off, [8]-bit UTF-8 on/off, [S]ixel on/off, [R]IP on/off, "
		ident := ""
		if t.Ident != "" {
			ident = t.Ident + ", "
		}
		switch {
		case t.PETSCII:
			t.Printf("  Terminal:  Commodore, %dx%d\n", t.Cols, t.Rows)
			menu = "\n[L]ocation, [W]idth 40/80, "
		case t.VT52:
			t.Printf("  Terminal:  VT52, %dx%d\n", t.Cols, t.Rows)
			menu = "\n[L]ocation, "
		default:
			charset := ""
			if t.DEC {
				charset = ", DEC graphics"
			}
			t.Printf("  Terminal:  %s%dx%d, ANSI %s, UTF-8 %s%s, sixel %s, RIP %s\n", ident, t.Cols, t.Rows,
				onOff[t.ANSI], onOff[t.UTF8], charset, onOff[t.Sixel], onOff[t.RIP])
		}
		if !u.External {
			menu += "[P]assword, SSH [K]eys, "
		}
		t.Print(menu + "[Q]uit: ")
		k, err := t.Key()
		if err != nil {
			return err
		}
		t.Println("")
		switch k {
		case 'L':
			loc, err := t.Input("Where are you calling from? ", bbs.MaxLocationLen)
			if err != nil {
				return err
			}
			if err := s.b.Store.SetLocation(u.ID, bbs.Clean(loc, bbs.MaxLocationLen)); err != nil {
				return err
			}
		case 'A', '8', 'S', 'R':
			if t.PETSCII || t.VT52 {
				continue
			}
		case 'W':
			if t.PETSCII {
				t.SetPETSCII(true, map[int]int{40: 80, 80: 40}[t.Cols])
			}
		}
		switch k {
		case 'A':
			t.ANSI = !t.ANSI
			if !t.ANSI {
				t.RIP = false
			}
		case '8':
			t.UTF8 = !t.UTF8
			if t.UTF8 && t.DEC {
				t.DEC = false
				t.setTranslator(nil)
			}
		case 'R':
			// on only where the terminal says it has it
			if t.RIP {
				t.RIP = false
			} else if t.ANSI {
				on, err := t.DetectRIP(2 * time.Second)
				if err != nil {
					return err
				}
				t.RIP = on
				if !on {
					t.Println(t.C(Red, "Your terminal does not say it has RIPscrip."))
				}
			}
		case 'S':
			// on only where the terminal can: sixel bytes are noise elsewhere
			if t.Sixel {
				t.Sixel = false
			} else if t.ANSI {
				on, err := t.DetectSixel(2 * time.Second)
				if err != nil {
					return err
				}
				t.Sixel = on
				if !on {
					t.Println(t.C(Red, "Your terminal does not say it has sixel graphics."))
				}
			}
		case 'P':
			if u.External {
				continue
			}
			old, err := t.Password("Your password now: ")
			if err != nil {
				return err
			}
			pw, err := t.Password("The new one: ")
			if err != nil {
				return err
			}
			again, err := t.Password("Again: ")
			if err != nil {
				return err
			}
			if pw != again {
				t.Println(t.C(Red, "They differ."))
				continue
			}
			if err := s.n.ChangePassword(old, pw); err != nil {
				t.Println(t.C(Red, err.Error()+"."))
				continue
			}
			t.Println(t.C(Green, "Changed."))
		case 'K':
			if u.External {
				continue
			}
			if err := s.sshKeys(); err != nil {
				return err
			}
		case 'Q', '\r':
			return nil
		}
	}
}

// sshKeys: the keys the user logs in by SSH with.
func (s *session) sshKeys() error {
	t := s.t
	for {
		ks, err := s.b.Store.SSHKeys(s.user.ID)
		if err != nil {
			return err
		}
		t.Println("")
		t.Println(t.C(Cyan, "Your SSH keys"))
		if len(ks) == 0 {
			t.Println("  none: you log in by SSH with your password")
		}
		for i, k := range ks {
			t.Printf("  %d. %s %s %s\n", i+1, k.Type, k.Fingerprint, k.Comment)
		}
		t.Print("\n[A]dd, [D]elete, [Q]uit: ")
		k, err := t.Key()
		if err != nil {
			return err
		}
		t.Println("")
		switch k {
		case 'A':
			// a key opens the account: whoever adds one knows its password
			pw, err := t.Password("Your password: ")
			if err != nil {
				return err
			}
			if _, err := s.b.Authenticate(fmt.Sprintf("node %d", s.n.Num), s.c.Source, s.user.Name, pw); err != nil {
				t.Println(t.C(Red, err.Error()+"."))
				continue
			}
			t.Println("Paste the public key (one line, as in ~/.ssh/id_ed25519.pub):")
			line, err := t.Input("", 2048)
			if err != nil {
				return err
			}
			if line == "" {
				continue
			}
			key, err := s.b.AddSSHKey(s.user, line)
			if err != nil {
				t.Println(t.C(Red, err.Error()+"."))
				continue
			}
			t.Println(t.C(Green, "Added "+key.Fingerprint+"."))
		case 'D':
			if len(ks) == 0 {
				continue
			}
			n, err := t.Input("Which (its number)? ", 3)
			if err != nil {
				return err
			}
			i, err := strconv.Atoi(n)
			if err != nil || i < 1 || i > len(ks) {
				continue
			}
			if err := s.b.Store.RemoveSSHKey(ks[i-1].ID, s.user.ID); err != nil {
				return err
			}
			s.b.Log.Printf("%s: SSH key deleted: %s", s.user.Name, ks[i-1].Fingerprint)
			t.Println(t.C(Green, "Deleted."))
		case 'Q', '\r':
			return nil
		}
	}
}

func (s *session) info() error {
	t := s.t
	b := s.b
	areas, _ := b.Store.Areas()
	msgs := 0
	for _, a := range areas {
		msgs += a.Messages
	}
	us, _ := b.Store.Users()
	t.Println("")
	t.Println(t.C(Cyan, b.Name))
	if b.Location != "" {
		t.Println("  Location:  " + b.Location)
	}
	t.Println("  Sysop:     " + b.Sysop)
	t.Printf("  Users:     %d\n", len(us))
	t.Printf("  Messages:  %d in %d areas\n", msgs, len(areas))
	t.Printf("  Nodes:     %d of %d in use\n", len(b.Nodes()), b.MaxNodes)
	t.Println("  Software:  bbs-sec: telnet, SSH, TN3270, rlogin, modems; ANSI, RIP, PETSCII")
	return t.Pause()
}
