package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// areas: the list of message areas
func (s *Session) areas() error {
	for {
		s.N.Doing("Message areas")
		areas, err := s.B.AreasFor(s.User)
		if err != nil {
			return err
		}
		t := &view.Table{Title: "Message areas", Empty: "There are no message areas yet.",
			Pick: "Area number",
			Columns: []view.Column{{Title: "Area", Width: 20}, {Title: "Description", Width: 36, Wide: true},
				{Title: "Msgs", Width: 5, Right: true}, {Title: "New", Width: 5, Right: true}},
			Actions: []view.Action{{ID: "new", Label: "New scan", Key: 'N', PF: 5}}}
		if s.User.Sysop {
			t.Actions = append(t.Actions, view.Action{ID: "add", Label: "add area", Key: '+', PF: 6, Short: "Add area"})
		}
		for i, a := range areas {
			nw := ""
			if a.New > 0 {
				nw = strconv.Itoa(a.New)
			}
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Mark: a.New > 0,
				Cells: []string{a.Name, a.Description, strconv.Itoa(a.Messages), nw}})
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case "new":
			found := false
			for _, a := range areas {
				if a.New == 0 {
					continue
				}
				found = true
				s.UI.Say(view.Bright, a.Name)
				if quit, err := s.readNew(a.Area); err != nil || quit {
					return err
				}
			}
			if !found {
				s.UI.Say(view.Plain, "No new messages.")
			} else {
				s.UI.Say(view.Plain, "No more new messages.")
			}
		case "add":
			if s.User.Sysop {
				if err := s.addArea(); err != nil {
					return err
				}
			}
		case view.Pick:
			if c.Row < 0 {
				s.UI.Say(view.Bad, "No such area.")
				continue
			}
			if err := s.area(areas[c.Row].Area); err != nil {
				return err
			}
		}
	}
}

func (s *Session) addArea() error {
	v, ok, err := s.UI.Form(&view.Form{Title: "New area", Submit: "Add", Fields: []view.Field{
		{Name: "name", Label: "Name", Prompt: "Name of the new area: ", Width: 30},
		{Name: "desc", Label: "Description", Width: 60}}})
	if err != nil || !ok || strings.TrimSpace(v["name"]) == "" {
		return err
	}
	only, err := s.UI.Confirm("Only the sysop posts?", false)
	if err != nil {
		return err
	}
	a, err := s.B.Store.CreateArea(bbs.Clean(v["name"], 30), bbs.Clean(v["desc"], 60), only)
	if err != nil {
		s.UI.Say(view.Bad, err.Error())
		return nil
	}
	s.B.Log.Printf("node %d: %s added area %s", s.N.Num, s.User.Name, a.Name)
	s.UI.Say(view.Good, "Added "+a.Name+".")
	return nil
}

// area: an area's messages, a page at a time
func (s *Session) area(a *store.Area) error {
	starts := []int64{0} // where each page begins
	for {
		s.N.Doing("In " + a.Name)
		a2, err := s.B.Store.Area(a.ID)
		if err != nil {
			return err
		}
		a = a2
		last, err := s.B.Store.LastRead(s.User.ID, a.ID)
		if err != nil {
			return err
		}
		nw, _ := s.B.Store.NewCount(s.User.ID, a.ID)
		per := s.UI.PageSize()
		ms, err := s.B.Store.Messages(a.ID, starts[len(starts)-1], per+1)
		if err != nil {
			return err
		}
		more := len(ms) > per
		if more {
			ms = ms[:per]
		}
		title := a.Name
		if a.Newsgroup != "" {
			title += " (" + a.Newsgroup + ")"
		}
		if a.EchoTag != "" {
			title += " (echo " + a.EchoTag + ")"
		}
		t := &view.Table{Title: title, Empty: "No messages here yet.",
			Note: []view.Line{view.T(view.Dim, fmt.Sprintf("%d messages, %d new", a.Messages, nw))},
			Pick: "Number to read", More: more, Less: len(starts) > 1,
			Columns: []view.Column{{Title: "From", Width: 16}, {Title: "Subject", Width: 36},
				{Title: "Date", Width: 11, Wide: true}},
			Actions: []view.Action{{ID: "new", Label: "Read new", Key: 'R', PF: 5}}}
		if s.B.CanPost(s.User, a) {
			t.Actions = append(t.Actions, view.Action{ID: "post", Label: "Post", Key: 'P', PF: 6})
		}
		for _, m := range ms {
			t.Rows = append(t.Rows, view.Row{Num: strconv.FormatInt(m.ID, 10), Mark: m.ID > last,
				Cells: []string{m.From, m.Subject, m.Created.Format("01-02 15:04")}})
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case view.More:
			if more {
				starts = append(starts, ms[len(ms)-1].ID)
			}
		case view.Less:
			if len(starts) > 1 {
				starts = starts[:len(starts)-1]
			}
		case "new":
			if _, err := s.readNew(a); err != nil {
				return err
			}
		case "post":
			if s.B.CanPost(s.User, a) {
				if err := s.post(a, "All", "", nil, 0); err != nil {
					return err
				}
			}
		case view.Pick:
			var m *store.Message
			if c.Row >= 0 {
				m = ms[c.Row]
			} else if id, err := strconv.ParseInt(strings.TrimPrefix(c.Num, "#"), 10, 64); err == nil {
				if m, err = s.B.Store.Message(id); err != nil || m.AreaID != a.ID {
					m = nil
				}
			}
			if m == nil {
				s.UI.Say(view.Bad, "No such message here.")
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
func (s *Session) readNew(a *store.Area) (bool, error) {
	last, err := s.B.Store.LastRead(s.User.ID, a.ID)
	if err != nil {
		return false, err
	}
	ms, err := s.B.Store.Messages(a.ID, last, 1)
	if err != nil {
		return false, err
	}
	if len(ms) == 0 {
		s.UI.Say(view.Plain, "No new messages.")
		return false, nil
	}
	return s.read(a, ms[0])
}

// message is a stored message for reading.
func message(m *store.Message, where string) *view.Message {
	return &view.Message{Where: where, From: m.From, To: m.To, Subject: m.Subject,
		Date: m.Created, Body: m.Body, Default: "next"}
}

// read: messages of an area from m on; true if the caller quit
func (s *Session) read(a *store.Area, m *store.Message) (bool, error) {
	for {
		s.N.Doing("Reading " + a.Name)
		s.B.Store.SetLastRead(s.User.ID, a.ID, m.ID)
		canDelete := s.User.Sysop || s.User.ID == m.FromID
		vm := message(m, fmt.Sprintf("%s  #%d", a.Name, m.ID))
		vm.Actions = []view.Action{{ID: "next", Label: "Next", Key: 'N', PF: 11},
			{ID: "prev", Label: "Previous", Key: 'P', PF: 10, Short: "Prev"}}
		if s.B.CanPost(s.User, a) {
			vm.Actions = append(vm.Actions, view.Action{ID: "reply", Label: "Reply", Key: 'R', PF: 4})
		}
		vm.Actions = append(vm.Actions, view.Action{ID: "mail", Label: "Mail reply", Key: 'M', PF: 5, Short: "Mail"})
		if canDelete {
			vm.Actions = append(vm.Actions, view.Action{ID: "delete", Label: "Delete", Key: 'D', PF: 9, Short: "Del"})
		}
		vm.Quit = "quit"
		act, err := s.UI.Message(vm)
		if err != nil {
			return false, err
		}
		switch act {
		case "quit":
			return true, nil
		case view.Back:
			return false, nil
		case "next", "prev":
			nm, err := s.B.Store.Neighbour(a.ID, m.ID, act == "next")
			if errors.Is(err, store.ErrNotFound) {
				if act == "next" {
					s.UI.Say(view.Plain, "That was the last message.")
					return false, nil
				}
				s.UI.Say(view.Plain, "That is the first message.")
				continue
			} else if err != nil {
				return false, err
			}
			m = nm
		case "reply":
			if s.B.CanPost(s.User, a) {
				if err := s.post(a, m.From, bbs.ReplySubject(m.Subject),
					bbs.Quote(m.From, m.Body, 8), m.ID); err != nil {
					return false, err
				}
			}
		case "mail":
			if err := s.compose(m.From, bbs.ReplySubject(m.Subject),
				bbs.Quote(m.From, m.Body, 8), 0); err != nil {
				return false, err
			}
		case "delete":
			if !canDelete {
				continue
			}
			ok, err := s.UI.Confirm("Delete this message?", false)
			if err != nil {
				return false, err
			}
			if ok {
				if err := s.N.DeleteMessage(m); err != nil {
					s.UI.Say(view.Bad, err.Error())
					continue
				}
				s.UI.Say(view.Good, "Deleted.")
				return false, nil
			}
		}
	}
}

// post: a message to an area
func (s *Session) post(a *store.Area, to, subject string, quote []string, replyTo int64) error {
	s.N.Doing("Writing in " + a.Name)
	var id int64
	sent, err := s.UI.Edit(&view.Draft{Title: "Post in " + a.Name, To: to, Subject: subject, Quote: quote,
		Send: func(to, subject string, body []string) error {
			var err error
			id, err = s.N.Post(a, to, subject, body, replyTo)
			return err
		}})
	if err == nil && sent {
		s.UI.Say(view.Good, fmt.Sprintf("Posted as #%d.", id))
	}
	return err
}

// compose: a private mail
func (s *Session) compose(to, subject string, quote []string, replyTo int64) error {
	s.N.Doing("Writing mail")
	d := &view.Draft{Title: "New mail", To: to, ToHint: "user name", Subject: subject, Quote: quote,
		CheckTo: func(to string) error {
			if _, _, ok := bbs.SplitNetmail(to); ok && s.B.Netmail != nil {
				return nil
			}
			if _, err := s.B.Store.UserByName(to); err != nil {
				return errors.New("there is no such user")
			}
			return nil
		},
		Send: func(to, subject string, body []string) error {
			return s.N.SendMail(to, subject, body, replyTo)
		}}
	if s.B.Netmail != nil {
		d.ToHint = "user name, or Name@zone:net/node for FidoNet"
	}
	sent, err := s.UI.Edit(d)
	if err == nil && sent {
		s.UI.Say(view.Good, "Sent.")
	}
	return err
}

// mail: the inbox
func (s *Session) mail() error {
	for {
		s.N.Doing("Reading mail")
		inbox, err := s.B.Store.Inbox(s.User.ID)
		if err != nil {
			return err
		}
		t := &view.Table{Title: "Your mail", Empty: "You have no mail.", Pick: "Number to read",
			Columns: []view.Column{{Title: "From", Width: 16}, {Title: "Subject", Width: 40},
				{Title: "Date", Width: 11, Wide: true}},
			Actions: []view.Action{{ID: "send", Label: "Send", Key: 'S', PF: 6, Short: "New mail"}}}
		for i, m := range inbox {
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Mark: !m.Read,
				Cells: []string{m.From, m.Subject, m.Created.Format("01-02 15:04")}})
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case "send":
			if err := s.compose("", "", nil, 0); err != nil {
				return err
			}
		case view.Pick:
			if c.Row < 0 {
				s.UI.Say(view.Bad, "No such mail.")
				continue
			}
			if err := s.readMail(inbox, c.Row); err != nil {
				return err
			}
		}
	}
}

func (s *Session) readMail(inbox []*store.Message, i int) error {
	for {
		m := inbox[i]
		s.B.Store.MarkMailRead(s.User.ID, m.ID)
		vm := message(m, fmt.Sprintf("Mail %d of %d", i+1, len(inbox)))
		vm.Actions = []view.Action{{ID: "next", Label: "Next", Key: 'N', PF: 11},
			{ID: "prev", Label: "Previous", Key: 'P', PF: 10, Short: "Prev"},
			{ID: "reply", Label: "Reply", Key: 'R', PF: 4},
			{ID: "delete", Label: "Delete", Key: 'D', PF: 9}}
		act, err := s.UI.Message(vm)
		if err != nil {
			return err
		}
		switch act {
		case view.Back:
			return nil
		case "next":
			if i+1 >= len(inbox) {
				s.UI.Say(view.Plain, "No more mail.")
				return nil
			}
			i++
		case "prev":
			if i == 0 {
				s.UI.Say(view.Plain, "That is the first.")
				continue
			}
			i--
		case "reply":
			if err := s.compose(m.From, bbs.ReplySubject(m.Subject),
				bbs.Quote(m.From, m.Body, 8), m.ID); err != nil {
				return err
			}
		case "delete":
			if err := s.B.Store.DeleteMail(s.User.ID, m.ID); err != nil {
				return err
			}
			s.UI.Say(view.Good, "Deleted.")
			return nil
		}
	}
}
