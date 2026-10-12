package ircd

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
)

// command handles a registered client's line.
func (c *client) command(m *ircmsg.Message) {
	switch m.Command {
	case "PING":
		c.send(nil, "", "PONG", c.s.S.ServerName, param(m, 0))
	case "PONG":
	case "CAP":
		c.cap(m)
	case "PASS", "USER":
		c.reply(ERR_ALREADYREGISTRED, "You may not reregister")
	case "AUTHENTICATE":
		c.reply(ERR_SASLALREADY, "You have already authenticated")
	case "NICK":
		if nick := param(m, 0); fold(nick) != fold(c.nick) {
			c.reply(ERR_ERRONEUSNICKNAME, nick, "Your nick is your BBS name")
		}
	case "JOIN":
		c.join(param(m, 0))
	case "PART":
		for _, ch := range strings.Split(param(m, 0), ",") {
			c.part(ch)
		}
	case "PRIVMSG", "NOTICE":
		c.privmsg(m.Command, param(m, 0), param(m, 1))
	case "NAMES":
		for _, ch := range strings.Split(param(m, 0), ",") {
			if ch != "" {
				c.names(ch)
			}
		}
	case "WHO":
		c.who(param(m, 0))
	case "WHOIS":
		c.whois(param(m, len(m.Params)-1))
	case "WHOWAS":
		c.reply(ERR_WASNOSUCHNICK, param(m, 0), "There was no such nickname")
		c.reply(RPL_ENDOFWHOWAS, param(m, 0), "End of WHOWAS")
	case "LIST":
		c.list()
	case "TOPIC":
		c.topic(param(m, 0), param(m, 1), len(m.Params) > 1)
	case "MODE":
		c.mode(m)
	case "AWAY":
		c.away = param(m, 0)
		if c.away == "" {
			c.reply(RPL_UNAWAY, "You are no longer marked as being away")
			c.n.Doing("IRC")
		} else {
			c.reply(RPL_NOWAWAY, "You have been marked as being away")
			c.n.Doing("IRC (away)")
		}
	case "ISON":
		var on []string
		for _, nick := range m.Params {
			for _, w := range strings.Fields(nick) {
				if len(c.nodesOf(w)) > 0 || fold(w) == fold(c.s.svc.Name) {
					on = append(on, w)
				}
			}
		}
		c.reply(RPL_ISON, strings.Join(on, " "))
	case "USERHOST":
		var out []string
		for _, nick := range m.Params {
			if ns := c.nodesOf(nick); len(ns) > 0 {
				out = append(out, fmt.Sprintf("%s=+%s@node%d", Nick(ns[0].User), ns[0].Via, ns[0].Num))
			}
		}
		c.reply(RPL_USERHOST, strings.Join(out, " "))
	case "MOTD":
		c.motd()
	case "LUSERS":
		c.lusers()
	case "VERSION":
		c.reply(RPL_VERSION, "bbs-sec", c.s.S.ServerName, "the chat of "+c.s.B.Name)
		for _, line := range c.s.support.CachedReply {
			c.reply(RPL_ISUPPORT, append(line, "are supported by this server")...)
		}
	case "TIME":
		c.reply(RPL_TIME, c.s.S.ServerName, time.Now().Format(time.RFC1123))
	case "INFO":
		c.reply(RPL_INFO, c.s.B.Name+": the BBS's chat rooms are its channels")
		c.reply(RPL_ENDOFINFO, "End of INFO")
	case "KICK", "INVITE", "OPER", "KILL", "KNOCK":
		c.reply(ERR_NOPRIVILEGES, "Permission denied: the BBS's rooms are everyone's")
	default:
		if slices.Contains(c.s.svc.CommandAliases, m.Command) {
			c.s.svc.command(c, m.Params)
			return
		}
		c.reply(ERR_UNKNOWNCOMMAND, m.Command, "Unknown command")
	}
}

// room is a channel's room ("": not one of ours).
func room(channel string) string {
	if !strings.HasPrefix(channel, "#") {
		return ""
	}
	return fold(channel[1:])
}

// joined lists the rooms this client is in.
func (c *client) joined() []string {
	var out []string
	for _, r := range c.n.Rooms() {
		if c.n.InRoom(r.Name) {
			out = append(out, r.Name)
		}
	}
	return out
}

func (c *client) join(list string) {
	if list == "0" {
		for _, r := range c.joined() {
			c.part("#" + r)
		}
		return
	}
	for _, ch := range strings.Split(list, ",") {
		if ch == "" {
			continue
		}
		name := room(ch)
		if name == "" {
			c.reply(ERR_NOSUCHCHANNEL, ch, "No such channel (channels begin with #)")
			continue
		}
		if c.n.InRoom(name) {
			continue
		}
		if len(c.joined()) >= maxJoins {
			c.reply(ERR_TOOMANYCHANNELS, ch, "You have joined too many channels")
			continue
		}
		var hist []bbs.Event
		var err error
		if pager, ok := strings.CutPrefix(name, "sysop-"); ok && c.u.Sysop {
			// a sysop going into a paging caller's room answers the page
			num, _ := strconv.Atoi(pager)
			if hist, _, err = c.n.AnswerAlso(num); err != nil {
				hist, err = c.n.JoinAlso(name)
			}
		} else {
			hist, err = c.n.JoinAlso(name)
		}
		switch {
		case errors.Is(err, bbs.ErrNoRoom):
			c.reply(ERR_BADCHANMASK, ch, "A channel is # and 1 to 20 letters, digits or -")
			continue
		case errors.Is(err, bbs.ErrNotYours), errors.Is(err, bbs.ErrNotAllowed):
			c.reply(ERR_INVITEONLYCHAN, ch, "Cannot join channel: "+err.Error())
			continue
		case err != nil:
			c.reply(ERR_UNKNOWNERROR, "JOIN", err.Error())
			continue
		}
		ch = "#" + name
		c.send(nil, c.source(), "JOIN", ch)
		c.sendTopic(ch, name)
		c.names(ch)
		for _, e := range hist {
			if e.Kind != bbs.Topic { // shown above
				c.event(e)
			}
		}
	}
}

func (c *client) part(ch string) {
	name := room(ch)
	if name == "" || !c.n.InRoom(name) {
		c.reply(ERR_NOTONCHANNEL, ch, "You're not on that channel")
		return
	}
	c.n.Part(name)
	c.send(nil, c.source(), "PART", "#"+name)
}

// prefix is a member's name in NAMES and WHO: @ for sysops.
func (c *client) member(m bbs.RoomMember) (nick, user, host string, op bool) {
	nick, user, host = Nick(m.Name), m.Via, fmt.Sprintf("node%d", m.Node)
	if u, err := c.s.B.Store.UserByName(m.Name); err == nil && u.Sysop {
		op = true
	}
	return
}

func (c *client) names(ch string) {
	name := room(ch)
	if name != "" && (c.n.InRoom(name) || !strings.HasPrefix(name, "sysop-")) {
		var line []string
		seen := map[string]bool{}
		if _, permanent := c.s.B.RoomTopic(name); permanent {
			// the service holds the permanent rooms open
			entry := c.s.svc.Name
			if c.caps["userhost-in-names"] {
				entry = c.s.svc.source(c)
			}
			line = append(line, "@"+entry)
		}
		for _, m := range c.s.B.RoomMembers(name) {
			nick, user, host, op := c.member(m)
			entry := nick
			if c.caps["userhost-in-names"] {
				entry = nick + "!" + user + "@" + host
			}
			if seen[entry] {
				continue
			}
			seen[entry] = true
			if op {
				entry = "@" + entry
			}
			line = append(line, entry)
			if len(strings.Join(line, " ")) > 350 {
				c.reply(RPL_NAMREPLY, "=", "#"+name, strings.Join(line, " "))
				line = nil
			}
		}
		if len(line) > 0 {
			c.reply(RPL_NAMREPLY, "=", "#"+name, strings.Join(line, " "))
		}
	}
	c.reply(RPL_ENDOFNAMES, ch, "End of /NAMES list")
}

// nodesOf are the nodes online of a nick's user (none for the service's
// nick, which is the service's).
func (c *client) nodesOf(nick string) []bbs.NodeInfo {
	if fold(nick) == fold(c.s.svc.Name) {
		return nil
	}
	var out []bbs.NodeInfo
	for _, n := range c.s.B.Nodes() {
		if n.User != "" && fold(Nick(n.User)) == fold(nick) {
			out = append(out, n)
		}
	}
	return out
}

func (c *client) who(mask string) {
	if name := room(mask); name != "" {
		if c.n.InRoom(name) || !strings.HasPrefix(name, "sysop-") {
			if _, permanent := c.s.B.RoomTopic(name); permanent {
				svc := c.s.svc
				c.reply(RPL_WHOREPLY, "#"+name, svc.Name, c.s.S.ServerName, c.s.S.ServerName, svc.Name, "H@",
					"0 Your BBS account and the permanent rooms")
			}
			for _, m := range c.s.B.RoomMembers(name) {
				nick, user, host, op := c.member(m)
				flags := "H"
				if op {
					flags += "@"
				}
				c.reply(RPL_WHOREPLY, "#"+name, user, host, c.s.S.ServerName, nick, flags, "0 "+m.Name)
			}
		}
	} else {
		for _, n := range c.nodesOf(mask) {
			c.reply(RPL_WHOREPLY, "*", n.Via, fmt.Sprintf("node%d", n.Num), c.s.S.ServerName, Nick(n.User), "H",
				"0 "+n.User)
		}
	}
	c.reply(RPL_ENDOFWHO, mask, "End of WHO list")
}

func (c *client) whois(nick string) {
	if svc := c.s.svc; fold(nick) == fold(svc.Name) {
		c.reply(RPL_WHOISUSER, svc.Name, svc.Name, c.s.S.ServerName, "*",
			fmt.Sprintf("Your BBS account: /msg %s HELP", svc.Name))
		var chans []string
		for _, r := range c.s.B.PermanentRooms() {
			chans = append(chans, "@#"+r.Name)
		}
		if len(chans) > 0 {
			c.reply(RPL_WHOISCHANNELS, svc.Name, strings.Join(chans, " "))
		}
		c.reply(RPL_WHOISSERVER, svc.Name, c.s.S.ServerName, c.s.B.Name)
		c.reply(RPL_ENDOFWHOIS, svc.Name, "End of /WHOIS list")
		return
	}
	ns := c.nodesOf(nick)
	if len(ns) == 0 {
		c.reply(ERR_NOSUCHNICK, nick, "No such nick")
		c.reply(RPL_ENDOFWHOIS, nick, "End of /WHOIS list")
		return
	}
	n := ns[0]
	nick = Nick(n.User)
	u, err := c.s.B.Store.UserByName(n.User)
	real := n.User
	if err == nil && u.Location != "" {
		real += ", " + u.Location
	}
	c.reply(RPL_WHOISUSER, nick, n.Via, fmt.Sprintf("node%d", n.Num), "*", real)
	c.reply(RPL_WHOISSERVER, nick, c.s.S.ServerName, c.s.B.Name)
	for _, o := range ns {
		c.reply(RPL_WHOISSPECIAL, nick, fmt.Sprintf("is on node %d by %s: %s", o.Num, o.Via, o.Activity))
	}
	if err == nil && u.Sysop {
		c.reply(RPL_WHOISOPERATOR, nick, "is the sysop")
	}
	if err == nil {
		c.reply(RPL_WHOISACCOUNT, nick, u.Name, "is logged in as")
	}
	c.reply(RPL_WHOISIDLE, nick, strconv.Itoa(int(time.Since(n.Since).Seconds())), strconv.FormatInt(n.Since.Unix(), 10),
		"seconds idle, signon time")
	c.reply(RPL_ENDOFWHOIS, nick, "End of /WHOIS list")
}

func (c *client) list() {
	c.reply(RPL_LISTSTART, "Channel", "Users  Name")
	for _, r := range c.n.Rooms() {
		users := len(r.Users)
		if r.Permanent {
			users++ // the service
		}
		c.reply(RPL_LIST, "#"+r.Name, strconv.Itoa(users), r.Topic)
	}
	c.reply(RPL_LISTEND, "End of /LIST")
}

func (c *client) topic(ch, text string, set bool) {
	name := room(ch)
	_, permanent := c.s.B.RoomTopic(name)
	switch {
	case name == "" || !c.n.InRoom(name) && !permanent:
		c.reply(ERR_NOTONCHANNEL, ch, "You're not on that channel")
	case !set:
		c.sendTopic("#"+name, name)
	case !permanent:
		c.reply(ERR_CHANOPRIVSNEEDED, ch, fmt.Sprintf("Only permanent rooms have topics (/msg %s HELP ROOM)", c.s.svc.Name))
	case !c.u.Sysop:
		c.reply(ERR_CHANOPRIVSNEEDED, ch, "A topic is set by the sysop")
	default:
		if err := c.s.B.SetRoomTopic(name, text, c.u.Name); err != nil {
			c.reply(ERR_UNKNOWNERROR, "TOPIC", err.Error())
		}
	}
}

// sendTopic tells the client a room's topic.
func (c *client) sendTopic(ch, name string) {
	if topic, _ := c.s.B.RoomTopic(name); topic != "" {
		c.reply(RPL_TOPIC, ch, topic)
	} else {
		c.reply(RPL_NOTOPIC, ch, "No topic is set")
	}
}

func (c *client) mode(m *ircmsg.Message) {
	target := param(m, 0)
	if name := room(target); name != "" {
		switch {
		case len(m.Params) == 1:
			c.reply(RPL_CHANNELMODEIS, "#"+name, "+nt")
		case strings.Trim(param(m, 1), "+") == "b":
			c.reply(RPL_ENDOFBANLIST, "#"+name, "End of channel ban list")
		default:
			c.reply(ERR_CHANOPRIVSNEEDED, target, "The BBS's rooms have no modes to change")
		}
		return
	}
	if fold(target) != fold(c.nick) {
		c.reply(ERR_USERSDONTMATCH, "Can't change mode for other users")
		return
	}
	if len(m.Params) == 1 {
		c.reply(RPL_UMODEIS, "+i")
	}
}

func (c *client) privmsg(command, target, text string) {
	notice := command == "NOTICE"
	if target == "" || text == "" {
		if !notice {
			c.reply(ERR_NOTEXTTOSEND, "No text to send")
		}
		return
	}
	if fold(target) == fold(c.s.svc.Name) {
		if !notice { // a NOTICE to a bot is never answered
			c.s.svc.privmsg(c, stripped(text))
		}
		return
	}
	emote := false
	if strings.HasPrefix(text, "\x01") {
		body := strings.TrimSuffix(text[1:], "\x01")
		action, ok := strings.CutPrefix(body, "ACTION ")
		if !ok {
			return // other CTCPs go nowhere
		}
		text, emote = action, true
	}
	text = stripped(text)
	if name := room(target); name != "" {
		if !c.n.InRoom(name) {
			if !notice {
				c.reply(ERR_CANNOTSENDTOCHAN, target, "Cannot send to channel (join it first)")
			}
			return
		}
		if err := c.n.SayIn(name, text, emote); err != nil && !notice {
			c.reply(ERR_CANNOTSENDTOCHAN, target, "Cannot send to channel: "+err.Error())
		}
		return
	}
	ns := c.nodesOf(target)
	if len(ns) == 0 {
		if !notice {
			c.reply(ERR_NOSUCHNICK, target, "No such nick")
		}
		return
	}
	if emote {
		text = "* " + c.u.Name + " " + text
	}
	sent := false
	var lastErr error
	for _, o := range ns {
		if o.Num == c.n.Num {
			continue
		}
		if err := c.n.Message(o.Num, text); err != nil {
			lastErr = err
		} else {
			sent = true
		}
	}
	switch {
	case sent && c.caps["echo-message"]:
		c.send(map[string]string{"time": stamp(time.Now())}, c.source(), command, target, text)
	case !sent && lastErr != nil && !notice:
		c.reply(ERR_CANNOTSENDTOCHAN, target, "Cannot send: "+lastErr.Error())
	}
}

// stamp is a time as server-time has it.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// events gives the client what its node is told, until it goes.
func (c *client) events() {
	for {
		select {
		case <-c.done:
			return
		case <-c.n.Wake():
			for _, e := range c.n.Pending() {
				c.event(e)
			}
		}
	}
}

// sourceOf is how one on a node is named.
func (c *client) sourceOf(name string, node int) string {
	via := "bbs"
	for _, n := range c.s.B.Nodes() {
		if n.Num == node {
			via = n.Via
		}
	}
	return fmt.Sprintf("%s!%s@node%d", Nick(name), via, node)
}

// event is something the node is told, as IRC.
func (c *client) event(e bbs.Event) {
	tags := map[string]string{"time": stamp(e.Time)}
	switch e.Kind {
	case bbs.Said, bbs.Emoted:
		if e.Node == c.n.Num && !c.caps["echo-message"] {
			return
		}
		text := e.Text
		if e.Kind == bbs.Emoted {
			text = "\x01ACTION " + text + "\x01"
		}
		c.send(tags, c.sourceOf(e.From, e.Node), "PRIVMSG", "#"+e.Room, text)
	case bbs.Joined:
		if e.Node != c.n.Num {
			c.send(tags, c.sourceOf(e.From, e.Node), "JOIN", "#"+e.Room)
		}
	case bbs.Left:
		if e.Node != c.n.Num {
			c.send(tags, c.sourceOf(e.From, e.Node), "PART", "#"+e.Room)
		}
	case bbs.Topic:
		c.send(tags, c.s.svc.source(c), "TOPIC", "#"+e.Room, e.Text)
	case bbs.Private:
		c.send(tags, c.sourceOf(e.From, e.Node), "PRIVMSG", c.nick, e.Text)
	case bbs.Paged:
		text := fmt.Sprintf("%s (node %d) is paging you", e.From, e.Node)
		if e.Text != "" {
			text += ": " + e.Text
		}
		c.send(tags, "", "NOTICE", c.nick, text+fmt.Sprintf(" (/join #sysop-%d to answer)", e.Node))
	default:
		c.send(tags, "", "NOTICE", c.nick, e.String())
	}
}
