package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// The new scan's areas, messages to the caller, and finding messages.

// most is how many messages a list of found ones holds.
const most = 100

// scanSetup: which areas the new scan goes through
func (s *Session) scanSetup(areas []bbs.AreaNew) error {
	for {
		s.N.Doing("New scan setup")
		off, err := s.B.Store.ScanOff(s.User.ID)
		if err != nil {
			return err
		}
		t := &view.Table{Title: "Your new scan", Empty: "There are no message areas yet.",
			Pick: "Area number to switch",
			Note: []view.Line{view.T(view.Dim, "The new scan goes through the areas marked *.")},
			Columns: []view.Column{{Title: "Area", Width: 20}, {Title: "Description", Width: 36, Wide: true},
				{Title: "Scan", Width: 4}},
			Actions: []view.Action{{ID: "all", Label: "All in", Key: 'A', PF: 5},
				{ID: "none", Label: "None", Key: 'X', PF: 6}}}
		for i, a := range areas {
			in := map[bool]string{true: "yes", false: "no"}[!off[a.ID]]
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Mark: !off[a.ID],
				Cells: []string{a.Name, a.Description, in}})
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case "all", "none":
			for _, a := range areas {
				if err := s.B.Store.SetScan(s.User.ID, a.ID, c.Action == "all"); err != nil {
					return err
				}
			}
		case view.Pick:
			if c.Row < 0 {
				s.UI.Say(view.Bad, "No such area.")
				continue
			}
			a := areas[c.Row]
			if err := s.B.Store.SetScan(s.User.ID, a.ID, off[a.ID]); err != nil {
				return err
			}
		}
	}
}

// byID are areas by their ids.
func byID(areas []bbs.AreaNew) map[int64]*store.Area {
	out := map[int64]*store.Area{}
	for _, a := range areas {
		out[a.ID] = a.Area
	}
	return out
}

// toYou: the newest messages to the caller, in the areas they read
func (s *Session) toYou(areas []bbs.AreaNew) error {
	s.N.Doing("Messages to them")
	all, err := s.B.Store.MessagesTo(s.User.Name, most)
	if err != nil {
		return err
	}
	return s.found("Messages to you", "There are no messages to you.", all, byID(areas))
}

// find: messages with words in them
func (s *Session) find(areas []bbs.AreaNew) error {
	v, ok, err := s.UI.Form(&view.Form{Title: "Find messages", Submit: "Find", Fields: []view.Field{
		{Name: "text", Label: "Find", Prompt: "Find (in subjects, text and authors): ", Width: 40}}})
	text := strings.TrimSpace(v["text"])
	if err != nil || !ok || text == "" {
		return err
	}
	if len([]rune(text)) < 2 {
		s.UI.Say(view.Bad, "Give two characters or more.")
		return nil
	}
	s.N.Doing("Finding messages")
	ids := make([]int64, len(areas))
	for i, a := range areas {
		ids[i] = a.ID
	}
	ms, err := s.B.Store.SearchMessages(ids, text, most)
	if err != nil {
		return err
	}
	return s.found("Found: "+text, "Nothing has "+text+" in it.", ms, byID(areas))
}

// found lists messages found (in the areas the caller reads) to read.
func (s *Session) found(title, empty string, all []*store.Message, areas map[int64]*store.Area) error {
	var ms []*store.Message
	for _, m := range all {
		if areas[m.AreaID] != nil {
			ms = append(ms, m)
		}
	}
	for {
		t := &view.Table{Title: title, Empty: empty, Pick: "Number to read",
			Columns: []view.Column{{Title: "Area", Width: 14}, {Title: "From", Width: 16},
				{Title: "Subject", Width: 30}, {Title: "Date", Width: 11, Wide: true}}}
		last := map[int64]int64{}
		for i, m := range ms {
			l, ok := last[m.AreaID]
			if !ok {
				l, _ = s.B.Store.LastRead(s.User.ID, m.AreaID)
				last[m.AreaID] = l
			}
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Mark: m.ID > l,
				Cells: []string{areas[m.AreaID].Name, m.From, m.Subject, m.Created.Format("01-02 15:04")}})
		}
		c, err := s.UI.Table(t)
		if err != nil || c.Action == view.Back {
			return err
		}
		if c.Action != view.Pick {
			continue
		}
		if c.Row < 0 {
			s.UI.Say(view.Bad, "No such message here.")
			continue
		}
		if err := s.browse(ms, c.Row, areas); err != nil {
			return err
		}
	}
}

// browse reads messages found, from the i'th, in their order; where the
// caller has read up to in each area stays as it was.
func (s *Session) browse(ms []*store.Message, i int, areas map[int64]*store.Area) error {
	for {
		m := ms[i]
		a := areas[m.AreaID]
		s.N.Doing("Reading " + a.Name)
		vm := message(m, fmt.Sprintf("%s  #%d  (%d of %d)", a.Name, m.ID, i+1, len(ms)))
		vm.Actions = []view.Action{{ID: "next", Label: "Next", Key: 'N', PF: 11},
			{ID: "prev", Label: "Previous", Key: 'P', PF: 10, Short: "Prev"}}
		if s.B.CanPost(s.User, a) {
			vm.Actions = append(vm.Actions, view.Action{ID: "reply", Label: "Reply", Key: 'R', PF: 4})
		}
		vm.Actions = append(vm.Actions, view.Action{ID: "mail", Label: "Mail reply", Key: 'M', PF: 5, Short: "Mail"})
		act, err := s.UI.Message(vm)
		if err != nil {
			return err
		}
		switch act {
		case view.Back:
			return nil
		case "next":
			if i+1 >= len(ms) {
				s.UI.Say(view.Plain, "That was the last.")
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
			if s.B.CanPost(s.User, a) {
				if err := s.post(a, m.From, bbs.ReplySubject(m.Subject), bbs.Quote(m.From, m.Body, 8), m.ID); err != nil {
					return err
				}
			}
		case "mail":
			if err := s.compose(m.From, bbs.ReplySubject(m.Subject), bbs.Quote(m.From, m.Body, 8), 0); err != nil {
				return err
			}
		}
	}
}
