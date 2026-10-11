package textui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
)

// notify is the terminal's Notify: what other nodes sent, shown at any
// prompt (in chat, as the room's lines; elsewhere, marked out).
func (s *session) notify() []string {
	t := s.t
	var out []string
	for _, e := range s.n.Pending() {
		line := bbs.Trunc(e.String(), 400)
		switch {
		case s.chatting && e.Kind == bbs.Said:
			out = append(out, t.C(Cyan, e.From+":")+" "+e.Text)
		case s.chatting && e.Kind != bbs.Private && e.Kind != bbs.Paged:
			out = append(out, t.C(Dim, line))
		default:
			out = append(out, t.C(Yellow, "** "+line+" **"))
		}
	}
	return out
}

// chat: the chat rooms, from room (the main one if "")
func (s *session) chat(room string) error {
	t := s.t
	if room == "" {
		room = bbs.MainRoom
	}
	t.Println("")
	t.Println(t.C(Cyan, "Chat") + t.C(Dim, "  (/help for commands, /quit to leave)"))
	hist, err := s.n.JoinRoom(room)
	if err != nil {
		t.Println(t.C(Red, err.Error()+"."))
		return nil
	}
	s.chatting = true
	defer func() {
		s.chatting = false
		s.n.LeaveRoom()
	}()
	for _, e := range hist {
		t.Println(t.C(Dim, e.String()))
	}
	t.Println(t.C(Dim, "You are in "+s.n.Room()+"."))
	for {
		s.n.Doing("Chatting in " + s.n.Room())
		t.Print(t.C(Green, "> "))
		l, err := t.ReadLine(LineOpts{Max: min(bbs.MaxChatLine, max(t.Cols-3, 20))})
		if err != nil {
			return err
		}
		text := strings.TrimSpace(l.Text)
		if t.ANSI && text != "" && !strings.HasPrefix(text, "/") {
			// what was typed comes back as the room's line: not twice
			t.w.WriteString("\x1b[A\r\x1b[K")
		}
		out, leave, err := s.n.ChatCommand(text)
		for _, o := range out {
			t.Println(t.C(Dim, o))
		}
		if err != nil {
			t.Println(t.C(Red, err.Error()+"."))
		}
		if leave {
			if s.n.Room() == fmt.Sprintf("sysop-%d", s.n.Num) {
				s.n.CancelPage()
			}
			t.Println(t.C(Dim, "You left the chat."))
			return nil
		}
		t.notices()
	}
}

// page: page the sysop, and wait for them in chat
func (s *session) page() error {
	t := s.t
	t.Println("")
	reason, err := t.Input("Page the sysop about (Enter to give up): ", 60)
	if err != nil || reason == "" {
		return err
	}
	room, told, err := s.n.PageSysop(reason)
	if err != nil {
		t.Println(t.C(Red, err.Error()+"."))
		return nil
	}
	if told > 0 {
		t.Println(t.C(Green, "The sysop has been paged.  Wait for them here (/quit to give up)."))
		return s.chat(room)
	}
	t.Println("The sysop is not available just now.")
	if len(s.b.PageCommand) > 0 {
		t.Println("They have been told you called for them.")
	}
	s.n.CancelPage()
	ok, err := t.YesNo("Leave them a message?", true)
	if err != nil || !ok {
		return err
	}
	us, err := s.b.Store.Users()
	if err != nil {
		return err
	}
	for _, u := range us {
		if u.Sysop && !u.Locked {
			return s.compose(u.Name, "Page: "+reason, nil, 0)
		}
	}
	t.Println("There is no sysop to write to.")
	return nil
}

// messageNode: a line to another node, from who's online
func (s *session) messageNode() error {
	t := s.t
	num, err := t.Input("Send a node a message (its number, Enter for none): ", 3)
	if err != nil || num == "" {
		return err
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		t.Println("That is not a node's number.")
		return nil
	}
	text, err := t.Input("Message: ", bbs.MaxChatLine)
	if err != nil || text == "" {
		return err
	}
	if err := s.n.Message(n, text); err != nil {
		if errors.Is(err, bbs.ErrNoNode) {
			t.Println(t.C(Red, "There is no one on that node."))
		} else {
			t.Println(t.C(Red, err.Error()+"."))
		}
		return nil
	}
	t.Println(t.C(Green, "Sent."))
	return nil
}
