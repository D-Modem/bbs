package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// who: who's online, and a line to one of them
func (s *Session) who() error {
	s.N.Doing("Who's online")
	t := &view.Table{Title: "Who's online", Pick: "Node",
		Prompt: "Send a node a message (its number, Enter for none): ",
		Columns: []view.Column{{Title: "User", Width: 20}, {Title: "Via", Width: 16},
			{Title: "Time", Width: 6, Right: true}, {Title: "Doing", Wide: true}}}
	nodes := s.B.Nodes()
	from := false // a column of the callers' countries, when any is known
	for _, n := range nodes {
		from = from || n.Place.Name != ""
	}
	if from {
		t.Columns = append(t.Columns[:2:2], append([]view.Column{{Title: "From", Width: 14}}, t.Columns[2:]...)...)
	}
	for _, n := range nodes {
		name := n.User
		if name == "" {
			name = "(logging in)"
		}
		cells := []string{name, n.Via, fmt.Sprintf("%dm", int(time.Since(n.Since).Minutes())), n.Activity}
		if from {
			// the country alone: the region and number are the sysop's to see
			cells = append(cells[:2:2], append([]string{n.Place.Name}, cells[2:]...)...)
		}
		t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(n.Num), Cells: cells})
	}
	c, err := s.UI.Table(t)
	if err != nil || c.Action != view.Pick {
		return err
	}
	num, err := strconv.Atoi(c.Num)
	if err != nil {
		s.UI.Say(view.Plain, "That is not a node's number.")
		return nil
	}
	v, ok, err := s.UI.Form(&view.Form{Title: fmt.Sprintf("A message to node %d", num), Submit: "Send",
		Fields: []view.Field{{Name: "text", Label: "Message", Width: bbs.MaxChatLine}}})
	if err != nil || !ok || v["text"] == "" {
		return err
	}
	if err := s.N.Message(num, v["text"]); err != nil {
		if errors.Is(err, bbs.ErrNoNode) {
			s.UI.Say(view.Bad, "There is no one on that node.")
		} else {
			s.UI.Say(view.Bad, err.Error()+".")
		}
		return nil
	}
	s.UI.Say(view.Good, "Sent.")
	return nil
}

func (s *Session) users() error {
	s.N.Doing("User list")
	us, err := s.B.Store.Users()
	if err != nil {
		return err
	}
	for {
		t := &view.Table{Title: "User list", Pick: "Number for their details",
			Columns: []view.Column{{Title: "User", Width: 24}, {Title: "From", Width: 26},
				{Title: "Posts", Width: 6, Right: true}, {Title: "Last call", Width: 10, Wide: true}}}
		for i, u := range us {
			last := "never"
			if !u.LastLogin.IsZero() {
				last = u.LastLogin.Format("2006-01-02")
			}
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Hot: u.Sysop,
				Cells: []string{u.Name, u.Location, strconv.Itoa(u.Posts), last}})
		}
		c, err := s.UI.Table(t)
		if err != nil || c.Action == view.Back {
			return err
		}
		if c.Action != view.Pick {
			continue
		}
		if c.Row < 0 {
			s.UI.Say(view.Bad, "No such user.")
			continue
		}
		if err := s.userDetail(us[c.Row]); err != nil {
			return err
		}
	}
}

// userDetail: what there is to know of a user, and mail to them
func (s *Session) userDetail(u *store.User) error {
	last := "never"
	if !u.LastLogin.IsZero() {
		last = u.LastLogin.Format("2006-01-02 15:04")
	}
	p := &view.Panel{Title: u.Name, Lines: []view.Line{
		view.V("Name:", u.Name),
		view.V("From:", u.Location),
		view.V("Member:", fmt.Sprintf("since %s, %d calls, %d posts", u.Created.Format("2006-01-02"), u.Logins, u.Posts)),
		view.V("Last call:", last)}}
	if u.About != "" {
		p.Lines = append(p.Lines, view.V("About:", u.About))
	}
	if u.Sysop {
		p.Lines = append(p.Lines, view.T(view.Bright, "The sysop."))
	}
	if me := s.User; me.Sysop {
		// what sysops alone see
		lvl := s.B.LevelOf(u)
		p.Lines = append(p.Lines, view.V("Access:", fmt.Sprintf("level %d (%s), flags %q, %s",
			u.Level, lvl.Name, u.Flags, map[bool]string{true: "locked", false: "not locked"}[u.Locked])),
			view.V("Files:", fmt.Sprintf("%d downloaded, %d uploaded", u.Downloads, u.Uploads)))
	}
	if u.ID != s.User.ID {
		p.Actions = []view.Action{{ID: "mail", Label: "Mail them", Key: 'M', PF: 5, Short: "Mail"}}
	}
	act, err := s.UI.Panel(p)
	if err != nil || act != "mail" {
		return err
	}
	return s.compose(u.Name, "", nil, 0)
}

// lastCallers: who called lately
func (s *Session) lastCallers() error {
	s.N.Doing("Last callers")
	cs, err := s.B.Store.LastCalls(20)
	if err != nil {
		return err
	}
	t := &view.Table{Title: "Last callers", Empty: "No one has called yet.",
		Columns: []view.Column{{Title: "User", Width: 20}, {Title: "From", Width: 22, Wide: true},
			{Title: "Via", Width: 8}, {Title: "Node", Width: 4, Right: true}, {Title: "When", Width: 11}}}
	for _, c := range cs {
		when := c.At.Format("01-02 15:04")
		t.Rows = append(t.Rows, view.Row{Cells: []string{c.Name, c.Location, c.Via, strconv.Itoa(c.Node), when}})
	}
	_, err = s.UI.Table(t)
	return err
}

// sysop is the sysop mail goes to: the first one not locked ("": none).
func (s *Session) sysop() string {
	us, _ := s.B.Store.Users()
	for _, u := range us {
		if u.Sysop && !u.Locked {
			return u.Name
		}
	}
	return ""
}

// feedback: mail to the sysop (which any caller may send)
func (s *Session) feedback() error {
	name := s.sysop()
	if name == "" {
		s.UI.Say(view.Plain, "There is no sysop to write to.")
		return nil
	}
	s.N.Doing("Writing to the sysop")
	sent, err := s.UI.Edit(&view.Draft{Title: "Feedback to the sysop", To: name, Subject: "Feedback",
		Send: func(to, subject string, body []string) error {
			return s.N.SendMail(name, subject, body, 0)
		}})
	if err == nil && sent {
		s.UI.Say(view.Good, "Sent to "+name+".")
	}
	return err
}

func (s *Session) info() error {
	s.N.Doing("System info")
	b := s.B
	areas, _ := b.Store.Areas()
	msgs := 0
	for _, a := range areas {
		msgs += a.Messages
	}
	us, _ := b.Store.Users()
	var lines []view.Line
	if b.Conf().Location != "" {
		lines = append(lines, view.V("Location:", b.Conf().Location))
	}
	if c := b.Conf(); c.PostalAddress != "" {
		lines = append(lines, view.V("Post:", c.PostalAddress))
	}
	if c := b.Conf(); c.Phone != "" {
		lines = append(lines, view.V("Dial in:", c.Phone))
	}
	if c := b.Conf(); c.Addresses != "" {
		lines = append(lines, view.V("Internet:", c.Addresses))
	}
	lines = append(lines,
		view.V("Sysop:", b.Conf().Sysop),
		view.V("Users:", strconv.Itoa(len(us))),
		view.V("Messages:", fmt.Sprintf("%d in %d areas", msgs, len(areas))),
		view.V("Nodes:", fmt.Sprintf("%d of %d in use", len(b.Nodes()), b.Conf().MaxNodes)),
		view.V("Software:", "bbs-sec: telnet, SSH, TN3270, rlogin, modems; ANSI, RIP, PETSCII"))
	if len(areas) > 0 {
		lines = append(lines, view.L(""), view.T(view.Bright, "Messages by area:"))
		most := 0
		for _, a := range areas {
			most = max(most, a.Messages)
		}
		for _, a := range areas {
			share := 0.0
			if most > 0 {
				share = float64(a.Messages) / float64(most)
			}
			lines = append(lines, view.Line{Label: a.Name, Text: strconv.Itoa(a.Messages), Bar: share})
		}
	}
	return s.UI.Show(b.Conf().Name, lines)
}

func (s *Session) profile() error {
	for {
		u := s.refresh()
		s.N.Doing("Profile")
		lvl := s.B.LevelOf(u)
		access := fmt.Sprintf("level %d (%s)", u.Level, lvl.Name)
		if u.Flags != "" {
			access += ", flags " + u.Flags
		}
		today := "no time limit"
		if left, ok := s.N.TimeLeft(); ok {
			today = fmt.Sprintf("%d minutes left", int(left.Round(time.Minute).Minutes()))
		}
		if lvl.Downloads > 0 {
			today += fmt.Sprintf(", %d of %d downloads", u.DownloadsToday(), lvl.Downloads)
		}
		p := &view.Panel{Title: "Your profile", Lines: []view.Line{
			view.V("Name:", u.Name),
			view.V("From:", u.Location),
			view.V("Member:", fmt.Sprintf("since %s, %d calls, %d posts", u.Created.Format("2006-01-02"), u.Logins, u.Posts)),
			view.V("Access:", access),
			view.V("Today:", today),
			view.V("Terminal:", s.UI.Terminal())},
			Actions: []view.Action{{ID: "location", Label: "Location", Key: 'L', PF: 5}}}
		if u.About != "" {
			p.Lines = append(p.Lines, view.V("About:", u.About))
		}
		if u.Alias != "" {
			p.Lines = append(p.Lines, view.V("Alias:", u.Alias))
		}
		if u.Signature != "" {
			p.Lines = append(p.Lines, view.V("Signature:", strings.ReplaceAll(u.Signature, "\n", " / ")))
		}
		p.Actions = append(p.Actions, view.Action{ID: "about", Label: "About you", Key: 'Y', PF: 4, Short: "About"},
			view.Action{ID: "alias", Label: "Alias", Key: 'I', PF: 2},
			view.Action{ID: "signature", Label: "Signature", Key: 'G', PF: 1, Short: "Sig"})
		if s.UI.Kind() == Text {
			mode := "off"
			if s.expert() {
				mode = "on"
			}
			p.Lines = append(p.Lines, view.V("Menus:", map[string]string{"on": "expert (the prompt alone)", "off": "in full"}[mode]))
			p.Actions = append(p.Actions, view.Action{ID: "expert", Label: "Expert menus on/off", Key: 'X'})
		}
		p.Actions = append(p.Actions, s.UI.TerminalActions()...)
		if !u.External {
			p.Actions = append(p.Actions, view.Action{ID: "password", Label: "Password", Key: 'P', PF: 6})
		}
		p.Actions = append(p.Actions, view.Action{ID: "keys", Label: "SSH keys", Key: 'K', PF: 9},
			view.Action{ID: "code", Label: "Sign-in code (for the web)", Key: 'C', PF: 10, Short: "Code"})
		act, err := s.UI.Panel(p)
		if err != nil {
			return err
		}
		switch act {
		case view.Back:
			return nil
		case "":
		case "location":
			v, ok, err := s.UI.Form(&view.Form{Title: "Your profile", Submit: "Save", Fields: []view.Field{
				{Name: "loc", Label: "Calling from", Prompt: "Where are you calling from? ",
					Width: bbs.MaxLocationLen, Value: u.Location}}})
			if err != nil {
				return err
			}
			if ok {
				if err := s.B.Store.SetLocation(u.ID, bbs.Clean(v["loc"], bbs.MaxLocationLen)); err != nil {
					return err
				}
			}
		case "about":
			v, ok, err := s.UI.Form(&view.Form{Title: "About you", Submit: "Save", Fields: []view.Field{
				{Name: "about", Label: "About you", Prompt: "A line about yourself (Enter keeps it, - clears it): ",
					Width: 60, Value: u.About}}})
			if err != nil {
				return err
			}
			if ok {
				about := bbs.Clean(v["about"], 60)
				if about == "-" {
					about = ""
				}
				if err := s.B.Store.SetAbout(u.ID, about); err != nil {
					return err
				}
			}
		case "alias":
			v, ok, err := s.UI.Form(&view.Form{Title: "Your alias", Submit: "Save", Fields: []view.Field{
				{Name: "alias", Label: "Alias", Prompt: "An alias to post under where areas allow (Enter keeps it, - clears it): ",
					Width: bbs.MaxNameLen, Value: u.Alias}}})
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			alias := strings.TrimSpace(v["alias"])
			if alias == "-" {
				alias = ""
			}
			if alias != "" {
				if msg := s.N.CheckNewName(alias); msg != "" {
					s.UI.Say(view.Bad, msg)
					continue
				}
			}
			switch err := s.B.Store.SetAlias(u.ID, alias); {
			case errors.Is(err, store.ErrExists):
				s.UI.Say(view.Bad, "That is someone's name or alias.")
			case err != nil:
				return err
			}
		case "signature":
			v, ok, err := s.UI.Form(&view.Form{Title: "Your signature", Submit: "Save",
				Intro: []view.Line{view.L("Up to three lines, put under your posts and mail; - clears it.")},
				Fields: []view.Field{{Name: "1", Label: "Line 1", Width: 70}, {Name: "2", Label: "Line 2", Width: 70},
					{Name: "3", Label: "Line 3", Width: 70}}})
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			var ls []string
			for _, k := range []string{"1", "2", "3"} {
				if l := bbs.Clean(v[k], 70); l != "" {
					ls = append(ls, l)
				}
			}
			if len(ls) == 1 && ls[0] == "-" {
				ls = nil
			}
			if err := s.B.Store.SetSignature(u.ID, strings.Join(ls, "\n")); err != nil {
				return err
			}
		case "expert":
			v := "on"
			if s.expert() {
				v = "off"
			}
			if err := s.N.SetPref("expert", v); err != nil {
				return err
			}
		case "password":
			if u.External {
				continue
			}
			if err := s.password(); err != nil {
				return err
			}
		case "code":
			code, until := s.B.LoginCode(u)
			s.B.Log.Printf("node %d: %s made a sign-in code", s.N.Num, u.Name)
			if err := s.UI.Show("Sign-in code", []view.Line{
				view.L("Your sign-in code, good once, until " + until.Format("15:04") + ":"), view.L(""),
				view.T(view.Bright, "    "+code), view.L(""),
				view.L("On the web forum's Sign In page, type it alone; anywhere else (the web"),
				view.L("terminal, the API), type it for your password.  A new one ends this one."),
			}); err != nil {
				return err
			}
		case "keys":
			if err := s.sshKeys(); err != nil {
				return err
			}
		default:
			if err := s.UI.TerminalAction(act); err != nil {
				return err
			}
		}
	}
}

func (s *Session) password() error {
	v, ok, err := s.UI.Form(&view.Form{Title: "Your password", Submit: "Change", Fields: []view.Field{
		{Name: "old", Label: "Password now", Prompt: "Your password now: ", Width: 32, Secret: true},
		{Name: "new", Label: "New password", Prompt: "The new one: ", Width: 32, Secret: true},
		{Name: "again", Label: "Again", Width: 32, Secret: true}}})
	if err != nil || !ok {
		return err
	}
	if v["new"] != v["again"] {
		s.UI.Say(view.Bad, "They differ.")
		return nil
	}
	if err := s.N.ChangePassword(v["old"], v["new"]); err != nil {
		s.UI.Say(view.Bad, err.Error()+".")
		return nil
	}
	s.UI.Say(view.Good, "Changed.")
	return nil
}

// sshKeys: the keys the user logs in by SSH with.
func (s *Session) sshKeys() error {
	for {
		ks, err := s.B.Store.SSHKeys(s.User.ID)
		if err != nil {
			return err
		}
		p := &view.Panel{Title: "Your SSH keys",
			Actions: []view.Action{{ID: "add", Label: "Add", Key: 'A', PF: 5}}}
		if len(ks) == 0 {
			p.Lines = append(p.Lines, view.L("none: you log in by SSH with your password"))
		} else {
			p.Actions = append(p.Actions, view.Action{ID: "delete", Label: "Delete", Key: 'D', PF: 9})
		}
		for i, k := range ks {
			p.Lines = append(p.Lines, view.L(fmt.Sprintf("%d. %s %s %s", i+1, k.Type, k.Fingerprint, k.Comment)))
		}
		act, err := s.UI.Panel(p)
		if err != nil {
			return err
		}
		switch act {
		case view.Back:
			return nil
		case "add":
			// a key opens the account: whoever adds one knows its password
			v, ok, err := s.UI.Form(&view.Form{Title: "Add an SSH key", Submit: "Next", Fields: []view.Field{
				{Name: "pw", Label: "Your password", Width: 32, Secret: true}}})
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if _, err := s.B.Authenticate(fmt.Sprintf("node %d", s.N.Num), s.N.Caller.Source, s.User.Name, v["pw"]); err != nil {
				s.UI.Say(view.Bad, err.Error()+".")
				continue
			}
			v, ok, err = s.UI.Form(&view.Form{Title: "Add an SSH key", Submit: "Add",
				Intro:  []view.Line{view.L("Paste the public key (one line, as in ~/.ssh/id_ed25519.pub):")},
				Fields: []view.Field{{Name: "key", Label: "Key", Prompt: "", Width: 2048}}})
			if err != nil {
				return err
			}
			if !ok || strings.TrimSpace(v["key"]) == "" {
				continue
			}
			key, err := s.B.AddSSHKey(s.User, v["key"])
			if err != nil {
				s.UI.Say(view.Bad, err.Error()+".")
				continue
			}
			s.UI.Say(view.Good, "Added "+key.Fingerprint+".")
		case "delete":
			v, ok, err := s.UI.Form(&view.Form{Title: "Delete an SSH key", Submit: "Delete", Fields: []view.Field{
				{Name: "n", Label: "Its number", Prompt: "Which (its number)? ", Width: 3}}})
			if err != nil {
				return err
			}
			i, e := strconv.Atoi(strings.TrimSpace(v["n"]))
			if !ok || e != nil || i < 1 || i > len(ks) {
				continue
			}
			if err := s.B.Store.RemoveSSHKey(ks[i-1].ID, s.User.ID); err != nil {
				return err
			}
			s.B.Log.Printf("%s: SSH key deleted: %s", s.User.Name, ks[i-1].Fingerprint)
			s.UI.Say(view.Good, "Deleted.")
		}
	}
}

// validate: the sysop goes through the users waiting to be validated.
func (s *Session) validate() error {
	waiting, err := s.B.Store.UsersBelow(s.B.Conf().ValidatedLevel)
	if err != nil {
		return err
	}
	by := fmt.Sprintf("node %d: %s", s.N.Num, s.User.Name)
	for i := 0; i < len(waiting); i++ {
		w := waiting[i]
		act, err := s.UI.Panel(&view.Panel{Title: "Validate users", Lines: []view.Line{
			view.T(view.Bright, w.Name),
			view.T(view.Dim, fmt.Sprintf("from %q, since %s, %d calls, level %d",
				w.Location, w.Created.Format("2006-01-02"), w.Logins, w.Level))},
			Actions: []view.Action{
				{ID: "validate", Label: fmt.Sprintf("Validate (level %d)", s.B.Conf().ValidatedLevel), Key: 'V', PF: 5, Short: "Validate"},
				{ID: "level", Label: "Level...", Key: 'L', PF: 6, Short: "Level"},
				{ID: "lock", Label: "lock", Key: 'K', PF: 9, Short: "Lock"},
				{ID: "skip", Label: "Skip", Key: 'S', PF: 8}}})
		if err != nil {
			return err
		}
		switch act {
		case view.Back:
			return nil
		case "validate":
			if err := s.B.Validate(w, by); err != nil {
				return err
			}
			s.UI.Say(view.Good, "Validated.")
		case "level":
			v, ok, err := s.UI.Form(&view.Form{Title: "Level for " + w.Name, Submit: "Set", Fields: []view.Field{
				{Name: "level", Label: "Level (0-255)", Width: 3}}})
			if err != nil {
				return err
			}
			if !ok {
				i--
				continue
			}
			l, err := strconv.Atoi(strings.TrimSpace(v["level"]))
			if err != nil || l < 0 || l > bbs.MaxLevel {
				s.UI.Say(view.Bad, "That is not a level.")
				i--
				continue
			}
			if err := s.B.Store.SetLevel(w.ID, l); err != nil {
				return err
			}
			s.B.Log.Printf("%s set %s's level to %d", by, w.Name, l)
			s.UI.Say(view.Good, "Done.")
		case "lock":
			if err := s.B.Store.SetLocked(w.ID, true); err != nil {
				return err
			}
			s.B.Log.Printf("%s locked %s", by, w.Name)
			s.UI.Say(view.Good, "Locked.")
		case "skip":
		default:
			i-- // the same one again
		}
	}
	s.UI.Say(view.Plain, "No one else is waiting.")
	return nil
}

// page: page the sysop, and wait for them in chat
func (s *Session) page() error {
	v, ok, err := s.UI.Form(&view.Form{Title: "Page the sysop", Submit: "Page", Fields: []view.Field{
		{Name: "reason", Label: "What is it about?", Prompt: "Page the sysop about (Enter to give up): ", Width: 60}}})
	reason := strings.TrimSpace(v["reason"])
	if err != nil || !ok || reason == "" {
		return err
	}
	room, told, err := s.N.PageSysop(reason)
	if err != nil {
		s.UI.Say(view.Bad, err.Error()+".")
		return nil
	}
	if told > 0 {
		return s.UI.Chat(room, "The sysop has been paged.  Wait for them here (leave the chat to give up).")
	}
	s.N.CancelPage()
	msg := "The sysop is not available just now."
	if len(s.B.Conf().PageCommand) > 0 {
		msg += "  They have been told you called for them."
	}
	s.UI.Say(view.Plain, msg)
	ok, err = s.UI.Confirm("Leave them a message?", true)
	if err != nil || !ok {
		return err
	}
	if name := s.sysop(); name != "" {
		return s.compose(name, "Page: "+reason, nil, 0)
	}
	s.UI.Say(view.Plain, "There is no sysop to write to.")
	return nil
}
