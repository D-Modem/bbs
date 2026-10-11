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
	"  /msg NODE TEXT  a message to one node    /quit           leave the chat",
	"Sysops:  /pages  callers paging   /answer [NODE]  answer one   /busy  pages on or off",
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
			out = append(out, fmt.Sprintf("%-20s %s", r.Name, strings.Join(r.Users, ", ")))
		}
		return out, false, nil
	case "join", "j":
		hist, err := n.JoinRoom(arg)
		if err != nil {
			return nil, false, err
		}
		for _, e := range hist {
			out = append(out, e.String())
		}
		return append(out, "You are in "+n.Room()+"."), false, nil
	case "msg", "m":
		to, text, _ := strings.Cut(arg, " ")
		num, err := strconv.Atoi(to)
		if err != nil {
			return nil, false, fmt.Errorf("/msg NODE TEXT: a node's number first")
		}
		if err := n.Message(num, text); err != nil {
			return nil, false, err
		}
		return []string{fmt.Sprintf("Sent to node %d.", num)}, false, nil
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
