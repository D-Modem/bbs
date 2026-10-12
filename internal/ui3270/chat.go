package ui3270

import (
	"fmt"
	"strings"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
)

// notice is the next line other nodes sent, for a screen's message line
// ("" if none); the rest wait for the screens after.
func (u *ui) notice() string {
	if u.user == nil || u.chatting {
		return ""
	}
	for _, e := range u.n.Pending() {
		u.backlog = append(u.backlog, e.String())
	}
	if len(u.backlog) == 0 {
		return ""
	}
	s := u.backlog[0]
	u.backlog = u.backlog[1:]
	if len(u.backlog) > 0 {
		s += fmt.Sprintf("  (+%d)", len(u.backlog))
	}
	return s
}

// chat: a chat room; new lines show at each Enter (or PF5): a 3270 shows
// nothing it was not sent, and is sent nothing while it is typed on
func (u *ui) chat(room, msg string) error {
	if room == "" {
		room = bbs.MainRoom
	}
	hist, err := u.n.JoinRoom(room)
	if err != nil {
		u.message(err.Error() + ".")
		return nil
	}
	u.chatting = true
	defer func() {
		u.chatting = false
		u.n.LeaveRoom()
	}()
	var log []string
	add := func(ls ...string) {
		for _, l := range ls {
			for len(l) > 0 {
				w := bbs.Trunc(l, u.cols-2)
				log = append(log, w)
				l = strings.TrimPrefix(l, w)
				if l != "" {
					l = "  " + strings.TrimLeft(l, " ")
				}
			}
		}
		if len(log) > 200 {
			log = log[len(log)-200:]
		}
	}
	for _, e := range hist {
		add(e.String())
	}
	add("You are in " + u.n.Room() + ".  /help for commands.")
	for {
		u.n.Doing("Chatting in " + u.n.Room())
		for _, e := range u.n.Pending() {
			add(e.String())
		}
		scr := u.frame("Chat: "+u.n.Room(), "Enter=Send  PF5=Refresh  PF3=Leave", msg)
		msg = ""
		room := u.rows - 5
		start := max(0, len(log)-room)
		for i, l := range log[start:] {
			color := go3270.Green
			if strings.HasPrefix(l, "->") || strings.HasPrefix(l, "<-") || strings.HasPrefix(l, "You ") {
				color = go3270.Blue
			}
			scr = append(scr, u.text(2+i, 1, l, color, false))
		}
		scr = append(scr, u.text(u.rows-3, 0, "===>", go3270.Turquoise, false))
		scr = append(scr, u.input(u.rows-3, 5, u.cols-7, "line", false)...)
		resp, v, err := u.show(scr, nil)
		if err != nil {
			return err
		}
		switch resp.AID {
		case go3270.AIDPF3:
			if u.n.Room() == fmt.Sprintf("sysop-%d", u.n.Num) {
				u.n.CancelPage()
			}
			return nil
		case go3270.AIDEnter:
			out, leave, err := u.n.ChatCommand(v["line"])
			add(out...)
			if err != nil {
				msg = err.Error() + "."
			}
			if leave {
				if u.n.Room() == fmt.Sprintf("sysop-%d", u.n.Num) {
					u.n.CancelPage()
				}
				return nil
			}
		}
	}
}

// message shows a line until Enter.
func (u *ui) message(s string) {
	scr := u.frame("", "Enter=Continue", "")
	scr = append(scr, u.text(3, 2, s, go3270.White, true))
	u.show(scr, nil)
}
