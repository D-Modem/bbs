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
}

type session struct {
	b    *bbs.BBS
	n    *bbs.Node
	t    *Term
	c    *bbs.Caller
	o    Options
	user *store.User
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

	if err := s.login(); err != nil {
		return err
	}
	s.welcome()
	return s.mainMenu()
}

// login: as vouched for, with the credentials given, or by asking
func (s *session) login() error {
	t := s.t
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
	newHint := ""
	if s.b.NewUsers {
		newHint = t.C(Dim, " (or NEW)")
	}
	for try := 0; try < s.b.Attempts; try++ {
		t.Print("Name" + newHint + ": ")
		l, err := t.ReadLine(LineOpts{Max: bbs.MaxNameLen, Initial: s.c.LoginUser})
		s.c.LoginUser = ""
		if err != nil {
			return err
		}
		name := strings.TrimSpace(l.Text)
		if name == "" {
			try--
			continue
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
		pw, err := t.Password("Password: ")
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
	t.Println("Welcome, " + t.C(Yellow, u.Name) + ".")
	if !u.LastLogin.IsZero() {
		t.Println(t.C(Dim, "Your last call was "+u.LastLogin.Format("2006-01-02 15:04")+"."))
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
	for {
		u := s.n.Refresh()
		s.user = u
		s.n.Doing("Main menu")
		unread, _ := s.b.Store.UnreadMail(u.ID)
		mail := "E-mail"
		if unread > 0 {
			mail += fmt.Sprintf(" (%d new)", unread)
		}
		t.Println("")
		t.Println(t.C(Cyan, "Main menu"))
		item := func(k, label string) string {
			return t.C(Yellow, "["+k+"]") + " " + bbs.Fit(label, 22)
		}
		t.Println("  " + item("M", "Message areas") + item("E", mail))
		t.Println("  " + item("W", "Who's online") + item("U", "User list"))
		t.Println("  " + item("P", "Your profile") + item("I", "System info"))
		t.Println("  " + item("G", "Goodbye"))
		t.Print("\nCommand: ")
		k, err := t.Key()
		if err != nil {
			return err
		}
		if k != '\r' {
			t.Println(string(k))
		} else {
			t.Println("")
		}
		switch k {
		case 'M':
			err = s.areas()
		case 'E':
			err = s.mail()
		case 'W':
			err = s.who()
		case 'U':
			err = s.users()
		case 'P':
			err = s.profile()
		case 'I':
			err = s.info()
		case 'G', 'Q':
			ok, err := t.YesNo("Log off?", true)
			if err != nil {
				return err
			}
			if ok {
				t.Println("")
				t.Println(s.b.Goodbye)
				s.b.Log.Printf("node %d: %s logged off", s.n.Num, u.Name)
				return errHangup
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
		t.Printf("%s: %d messages, %d new\n", t.C(Cyan, a.Name), a.Messages, nw)
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
		t.Println(t.C(Dim, fmt.Sprintf("%6s %-16s %-36s %s", "#", "From", "Subject", "Date")))
		for _, m := range ms {
			t.Printf("%s %s %s %s\n", t.C(Yellow, fmt.Sprintf("%6d", m.ID)), bbs.Fit(m.From, 16),
				bbs.Fit(m.Subject, 36), m.Created.Format("01-02 15:04"))
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
		if to != "" {
			prompt = "To [" + to + "]: "
		}
		who, err := t.Input(prompt, bbs.MaxNameLen)
		if err != nil {
			return err
		}
		if who != "" {
			to = who
		}
		if to == "" {
			return nil
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
	t.Println(t.C(Dim, fmt.Sprintf("%4s  %-20s %-16s %-6s %s", "Node", "User", "Via", "Time", "Doing")))
	for _, n := range s.b.Nodes() {
		name := n.User
		if name == "" {
			name = "(logging in)"
		}
		mins := int(time.Since(n.Since).Minutes())
		t.Printf("%4d  %s %s %-6s %s\n", n.Num, bbs.Fit(name, 20), bbs.Fit(n.Via, 16),
			fmt.Sprintf("%dm", mins), bbs.Trunc(n.Activity, t.Cols-52))
	}
	return t.Pause()
}

func (s *session) users() error {
	t := s.t
	us, err := s.b.Store.Users()
	if err != nil {
		return err
	}
	t.Println("")
	lines := []string{t.C(Dim, fmt.Sprintf("%-24s %-26s %6s  %s", "User", "From", "Posts", "Last call"))}
	for _, u := range us {
		last := "never"
		if !u.LastLogin.IsZero() {
			last = u.LastLogin.Format("2006-01-02")
		}
		name := bbs.Fit(u.Name, 24)
		if u.Sysop {
			name = t.C(Yellow, name)
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
		t.Printf("  Terminal:  %dx%d, ANSI %s, UTF-8 %s\n", t.Cols, t.Rows, onOff[t.ANSI], onOff[t.UTF8])
		menu := "\n[L]ocation, [A]NSI on/off, [8]-bit UTF-8 on/off, "
		if !u.External {
			menu += "[P]assword, "
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
		case 'A':
			t.ANSI = !t.ANSI
		case '8':
			t.UTF8 = !t.UTF8
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
	t.Println("  Software:  bbs-sec, with telnet, TN3270, rlogin and modem callers")
	return t.Pause()
}
