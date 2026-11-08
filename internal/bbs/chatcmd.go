package bbs

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ChatHelp is what the chat commands are.
var ChatHelp = []string{
	"Type to talk.  Commands:",
	"  /who            who is in this room      /rooms          the rooms in use",
	"  /join ROOM      go to another room       /me ACTION      * you do something",
	"  /msg WHO TEXT   to a user, kept if away  /topic          this room's topic",
	"  /history [WHO]  your talks with a user   /friends        your friends list",
	"  /friend WHO     put one on your list     /unfriend WHO   take one off it",
	"  /quit           leave the chat",
	"Sysops:  /pages  callers paging   /answer [NODE]  answer one   /busy  pages on or off",
	"         /topic TEXT  set a permanent room's topic",
}

// ChatCommand does a line typed in chat: talk, or a /command.  It gives
// the lines to show (besides the room's events), and whether to leave.
func (n *Node) ChatCommand(line string) (out []string, leave bool, err error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false, nil
	}
	if !strings.HasPrefix(line, "/") {
		return nil, false, n.Say(line, false)
	}
	cmd, arg, _ := strings.Cut(line[1:], " ")
	arg = strings.TrimSpace(arg)
	switch strings.ToLower(cmd) {
	case "q", "quit", "leave", "exit", "x":
		return nil, true, nil
	case "?", "h", "help":
		return ChatHelp, false, nil
	case "me":
		return nil, false, n.Say(arg, true)
	case "who":
		for _, r := range n.Rooms() {
			if r.Name == n.Room() {
				return []string{"In " + r.Name + ": " + strings.Join(r.Users, ", ")}, false, nil
			}
		}
		return []string{"You are in no room."}, false, nil
	case "rooms":
		rs := n.Rooms()
		if len(rs) == 0 {
			return []string{"No one is chatting."}, false, nil
		}
		for _, r := range rs {
			who := strings.Join(r.Users, ", ")
			if r.Permanent {
				if who == "" {
					who = "(no one yet)"
				}
				if r.Topic != "" {
					who += "  - " + r.Topic
				}
			}
			out = append(out, fmt.Sprintf("%-20s %s", r.Name, who))
		}
		return out, false, nil
	case "topic":
		room := n.Room()
		if room == "" {
			return nil, false, ErrNotInRoom
		}
		if arg == "" {
			topic, _ := n.b.RoomTopic(room)
			if topic == "" {
				return []string{"No topic is set."}, false, nil
			}
			return []string{"Topic: " + topic}, false, nil
		}
		if u := n.User(); u == nil || !u.Sysop {
			return nil, false, fmt.Errorf("a topic is set by the sysop")
		}
		return nil, false, n.b.SetRoomTopic(room, arg, n.name())
	case "join", "j":
		hist, err := n.JoinRoom(arg)
		if err != nil {
			return nil, false, err
		}
		for _, e := range hist {
			out = append(out, e.String())
		}
		return append(out, "You are in "+n.Room()+"."), false, nil
	case "msg", "m", "tell":
		to, text, _ := strings.Cut(arg, " ")
		if to == "" {
			return nil, false, fmt.Errorf("/msg WHO TEXT: a user's name (or a node's number), then the message")
		}
		if num, err := strconv.Atoi(to); err == nil {
			if err := n.Message(num, text); err != nil {
				return nil, false, err
			}
			if o := n.b.node(num); o != nil {
				n.Keep(o.User(), text, true)
			}
			return []string{fmt.Sprintf("Sent to node %d.", num)}, false, nil
		}
		u, err := n.b.Store.UserByName(to)
		if err != nil {
			return nil, false, ErrNoUser
		}
		online, err := n.TellUser(u, text)
		if err != nil {
			return nil, false, err
		}
		if !online {
			return []string{u.Name + " is away; they will have it when they next log in."}, false, nil
		}
		return []string{"Sent to " + u.Name + "."}, false, nil
	case "history":
		if arg == "" {
			ps, err := n.Conversations()
			if err != nil {
				return nil, false, err
			}
			if len(ps) == 0 {
				return []string{"You have no conversations yet (/msg WHO TEXT starts one)."}, false, nil
			}
			out = append(out, "Your conversations (/history WHO to read one):")
			for _, p := range ps {
				out = append(out, fmt.Sprintf("  %-20s %d lines, the last %s", p.Name, p.Lines, p.Last.Format("Jan 2 15:04")))
			}
			return out, false, nil
		}
		u, err := n.b.Store.UserByName(arg)
		if err != nil {
			return nil, false, ErrNoUser
		}
		cs, err := n.History(u, 50)
		if err != nil {
			return nil, false, err
		}
		if len(cs) == 0 {
			return []string{"You and " + u.Name + " have not talked yet."}, false, nil
		}
		return HistoryLines(cs), false, nil
	case "friends":
		u := n.User()
		if u == nil {
			return nil, false, ErrNoUser
		}
		lines, err := n.b.FriendLines(u)
		return lines, false, err
	case "friend", "unfriend":
		u := n.User()
		if u == nil || arg == "" {
			return nil, false, fmt.Errorf("/%s WHO: a user's name", strings.ToLower(cmd))
		}
		if strings.ToLower(cmd) == "friend" {
			f, err := n.b.AddFriend(u, arg)
			if err != nil {
				return nil, false, err
			}
			return []string{f.Name + " is on your friends list."}, false, nil
		}
		f, err := n.b.RemoveFriend(u, arg)
		if err != nil {
			return nil, false, err
		}
		return []string{f.Name + " is off your friends list."}, false, nil
	case "pages":
		ps := n.b.Pages()
		if len(ps) == 0 {
			return []string{"No one is paging."}, false, nil
		}
		for _, p := range ps {
			out = append(out, fmt.Sprintf("node %d  %s  %s ago: %s", p.Node, p.User,
				Since(p.Since), p.Reason))
		}
		return out, false, nil
	case "answer", "a":
		num := 0
		if arg != "" {
			if num, err = strconv.Atoi(arg); err != nil {
				return nil, false, fmt.Errorf("/answer NODE: a node's number")
			}
		}
		hist, node, err := n.Answer(num)
		if err != nil {
			return nil, false, err
		}
		for _, e := range hist {
			out = append(out, e.String())
		}
		return append(out, fmt.Sprintf("You are with the caller on node %d (room %s).", node, n.Room())), false, nil
	case "busy":
		if u := n.User(); u == nil || !u.Sysop {
			return nil, false, fmt.Errorf("/busy is for sysops")
		}
		n.SetBusy(!n.Busy())
		if n.Busy() {
			return []string{"You will not be paged."}, false, nil
		}
		return []string{"You may be paged."}, false, nil
	}
	return nil, false, fmt.Errorf("no such command: /%s (/help)", cmd)
}

// Since is how long ago t was, roughly: 5s, 3m, 2h.
func Since(t time.Time) string {
	d := int64(time.Since(t) / time.Second)
	switch {
	case d < 60:
		return fmt.Sprintf("%ds", d)
	case d < 3600:
		return fmt.Sprintf("%dm", d/60)
	}
	return fmt.Sprintf("%dh", d/3600)
}
