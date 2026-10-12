package textui

import (
	"fmt"
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

// Chat is the chat rooms, from room (the main one if ""), the note first.
func (s *session) Chat(room, note string) error {
	t := s.t
	if note != "" {
		t.Println(t.C(Green, note))
	}
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
