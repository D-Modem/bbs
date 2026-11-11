package ircd

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ergochat/irc-go/ircmsg"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
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
		c.join(param(m, 0), param(m, 1))
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
				if _, remote := c.s.B.LookupRemote(w); len(c.nodesOf(w)) > 0 || remote || c.s.service(w) != nil {
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
		c.reply(RPL_VERSION, "bbs-sec", c.s.S.ServerName, "the chat of "+c.s.B.Conf().Name)
		for _, line := range c.s.support.CachedReply {
			c.reply(RPL_ISUPPORT, append(line, "are supported by this server")...)
		}
	case "TIME":
		c.reply(RPL_TIME, c.s.S.ServerName, time.Now().Format(time.RFC1123))
	case "INFO":
		c.reply(RPL_INFO, c.s.B.Conf().Name+": the BBS's chat rooms are its channels")
		c.reply(RPL_ENDOFINFO, "End of INFO")
	case "KICK":
		c.kick(param(m, 0), param(m, 1), param(m, 2))
	case "INVITE":
		c.invite(param(m, 0), param(m, 1))
	case "KNOCK":
		c.knock(param(m, 0), param(m, 1))
	case "OPER", "KILL":
		c.reply(ERR_NOPRIVILEGES, "Permission denied")
	default:
		for _, svc := range []*service{c.s.svc, c.s.cs} {
			if slices.Contains(svc.CommandAliases, m.Command) {
				svc.command(c, m.Params)
				return
			}
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

// join is JOIN: each channel's room joined, with its topic, names and
// last lines; "0" parts them all. A sysop joining #sysop-N answers node
// N's page.
func (c *client) join(list, keyList string) {
	if list == "0" {
		for _, r := range c.joined() {
			c.part("#" + r)
		}
		return
	}
	keys := strings.Split(keyList, ",")
	for i, ch := range strings.Split(list, ",") {
		if ch == "" {
			continue
		}
		key := ""
		if i < len(keys) {
			key = keys[i]
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
				hist, err = c.n.JoinAlsoKey(name, key)
			}
		} else {
			hist, err = c.n.JoinAlsoKey(name, key)
		}
		switch {
		case errors.Is(err, bbs.ErrNoRoom):
			c.reply(ERR_BADCHANMASK, ch, "A channel is # and 1 to 20 letters, digits or -")
			continue
		case errors.Is(err, bbs.ErrBanned):
			c.reply(ERR_BANNEDFROMCHAN, ch, "Cannot join channel (+b): "+err.Error())
			continue
		case errors.Is(err, bbs.ErrInviteOnly):
			c.reply(ERR_INVITEONLYCHAN, ch, "Cannot join channel (+i): "+err.Error())
			continue
		case errors.Is(err, bbs.ErrBadKey):
			c.reply(ERR_BADCHANNELKEY, ch, "Cannot join channel (+k): "+err.Error())
			continue
		case errors.Is(err, bbs.ErrFull):
			c.reply(ERR_CHANNELISFULL, ch, "Cannot join channel (+l): "+err.Error())
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

// part leaves a channel's room.
func (c *client) part(ch string) {
	name := room(ch)
	if name == "" || !c.n.InRoom(name) {
		c.reply(ERR_NOTONCHANNEL, ch, "You're not on that channel")
		return
	}
	c.n.Part(name)
	c.send(nil, c.source(), "PART", "#"+name)
}

// member is a room member as NAMES and WHO give one: nick, user (how they
// came), host (their node), and their prefixes (@ an op, + a voice; with
// multi-prefix both, else the highest).
func (c *client) member(m bbs.RoomMember) (nick, user, host, prefix string) {
	if m.Node == 0 { // on a linked network: as it names them
		return m.Name, m.Via, m.Host, ""
	}
	nick, user, host = Nick(m.Name), m.Via, fmt.Sprintf("node%d", m.Node)
	if m.Op {
		prefix = "@"
	}
	if m.Voice && (prefix == "" || c.caps["multi-prefix"]) {
		prefix += "+"
	}
	return
}

// roomServices are the services sitting in a room: BBSServ in a permanent
// one, ChanServ in a guarded one.
func (c *client) roomServices(name string) []*service {
	var out []*service
	if _, permanent := c.s.B.RoomTopic(name); permanent {
		out = append(out, c.s.svc)
	}
	if c.s.B.RoomGuarded(name) {
		out = append(out, c.s.cs)
	}
	return out
}

// names is NAMES for one channel: the service first in a permanent room,
// sysops as ops; a sysop- room's only to those in it.
func (c *client) names(ch string) {
	name := room(ch)
	if name != "" && (c.n.InRoom(name) || !strings.HasPrefix(name, "sysop-") && (!c.s.B.RoomSecret(name) || c.u.Sysop)) {
		var line []string
		seen := map[string]bool{}
		for _, svc := range c.roomServices(name) {
			// the services hold permanent and guarded rooms open
			entry := svc.Name
			if c.caps["userhost-in-names"] {
				entry = svc.source(c)
			}
			line = append(line, "@"+entry)
		}
		for _, m := range c.s.B.RoomMembers(name) {
			nick, user, host, prefix := c.member(m)
			entry := nick
			if c.caps["userhost-in-names"] {
				entry = nick + "!" + user + "@" + host
			}
			if seen[entry] {
				continue
			}
			seen[entry] = true
			entry = prefix + entry
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
	if c.s.service(nick) != nil {
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

// userOfNick is the BBS user a nick is of, on or not (nil: none).
func (c *client) userOfNick(nick string) *store.User {
	if c.s.service(nick) != nil {
		return nil
	}
	if u, err := c.s.B.Store.UserByName(nick); err == nil {
		return u
	}
	us, err := c.s.B.Store.Users()
	if err != nil {
		return nil
	}
	for _, u := range us {
		if fold(Nick(u.Name)) == fold(nick) {
			return u
		}
	}
	return nil
}

// who is WHO: a channel's members (and the service, in a permanent room),
// or a nick's nodes.
func (c *client) who(mask string) {
	if name := room(mask); name != "" {
		if c.n.InRoom(name) || !strings.HasPrefix(name, "sysop-") && (!c.s.B.RoomSecret(name) || c.u.Sysop) {
			for _, svc := range c.roomServices(name) {
				c.reply(RPL_WHOREPLY, "#"+name, svc.Name, c.s.S.ServerName, c.s.S.ServerName, svc.Name, "H@",
					"0 "+c.s.serviceReal(svc))
			}
			for _, m := range c.s.B.RoomMembers(name) {
				nick, user, host, prefix := c.member(m)
				flags := "H" + prefix
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

// whois is WHOIS: the service, or a nick's user, their nodes and what each
// is doing, whether they are the sysop, and how long since they came on.
func (c *client) whois(nick string) {
	if svc := c.s.service(nick); svc != nil {
		c.reply(RPL_WHOISUSER, svc.Name, svc.Name, c.s.S.ServerName, "*",
			fmt.Sprintf("%s: /msg %s HELP", c.s.serviceReal(svc), svc.Name))
		var chans []string
		if svc == c.s.svc {
			for _, r := range c.s.B.PermanentRooms() {
				chans = append(chans, "@#"+r.Name)
			}
		} else {
			for _, r := range c.s.B.GuardedRooms() {
				chans = append(chans, "@#"+r)
			}
			slices.Sort(chans)
		}
		if len(chans) > 0 {
			c.reply(RPL_WHOISCHANNELS, svc.Name, strings.Join(chans, " "))
		}
		c.reply(RPL_WHOISSERVER, svc.Name, c.s.S.ServerName, c.s.B.Conf().Name)
		c.reply(RPL_ENDOFWHOIS, svc.Name, "End of /WHOIS list")
		return
	}
	ns := c.nodesOf(nick)
	if r, ok := c.s.B.LookupRemote(nick); len(ns) == 0 && ok {
		c.whoisRemote(r)
		return
	}
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
	c.reply(RPL_WHOISSERVER, nick, c.s.S.ServerName, c.s.B.Conf().Name)
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

// whoisRemote is WHOIS of someone on a linked network.
func (c *client) whoisRemote(r bbs.RemoteUser) {
	c.reply(RPL_WHOISUSER, r.Nick, r.User, r.Host, "*", r.Real)
	c.reply(RPL_WHOISSERVER, r.Nick, r.Server, "on "+r.Network)
	if r.Away != "" {
		c.reply(RPL_AWAY, r.Nick, r.Away)
	}
	if r.Account != "" {
		c.reply(RPL_WHOISACCOUNT, r.Nick, r.Account, "is logged in as")
	}
	if !r.Since.IsZero() {
		c.reply(RPL_WHOISIDLE, r.Nick, "0", strconv.FormatInt(r.Since.Unix(), 10), "seconds idle, signon time")
	}
	c.reply(RPL_ENDOFWHOIS, r.Nick, "End of /WHOIS list")
}

// list is LIST: the rooms, their numbers of callers (the services counted
// in the rooms they hold) and their topics.
func (c *client) list() {
	c.reply(RPL_LISTSTART, "Channel", "Users  Name")
	for _, r := range c.n.Rooms() {
		users := len(r.Users) + len(c.roomServices(r.Name)) // the services holding it open
		c.reply(RPL_LIST, "#"+r.Name, strconv.Itoa(users), r.Topic)
	}
	c.reply(RPL_LISTEND, "End of /LIST")
}

// topic is TOPIC: a room's topic shown, or set: a registered room's by
// its ops, a permanent room's by the sysop.
func (c *client) topic(ch, text string, set bool) {
	name := room(ch)
	_, permanent := c.s.B.RoomTopic(name)
	switch {
	case name == "" || !c.n.InRoom(name) && !permanent && c.s.B.RoomReg(name) == nil:
		c.reply(ERR_NOTONCHANNEL, ch, "You're not on that channel")
	case !set:
		c.sendTopic("#"+name, name)
	default:
		if err := c.n.SetTopic(name, text); err != nil {
			c.reply(ERR_CHANOPRIVSNEEDED, "#"+name, capital(err.Error()))
		}
	}
}

// kick is KICK: ops put people out (a list of nicks, parted by commas).
func (c *client) kick(ch, nicks, reason string) {
	name := room(ch)
	if name == "" || nicks == "" {
		c.reply(ERR_NEEDMOREPARAMS, "KICK", "Not enough parameters")
		return
	}
	if !c.n.InRoom(name) && !c.u.Sysop {
		c.reply(ERR_NOTONCHANNEL, ch, "You're not on that channel")
		return
	}
	for _, nick := range strings.Split(nicks, ",") {
		err := c.n.Kick(name, nick, reason)
		switch {
		case err == nil:
		case errors.Is(err, bbs.ErrNoUser):
			c.reply(ERR_NOSUCHNICK, nick, "No such nick")
		case errors.Is(err, bbs.ErrNotThere):
			c.reply(ERR_USERNOTINCHANNEL, nick, "#"+name, "They aren't on that channel")
		default:
			c.reply(ERR_CHANOPRIVSNEEDED, "#"+name, capital(err.Error()))
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

// mode is MODE: a channel's are +nt; its ops give and take o and v (in a
// registered room).  A user's own is +i.
func (c *client) mode(m *ircmsg.Message) {
	target := param(m, 0)
	if name := room(target); name != "" {
		if len(m.Params) == 1 {
			modes, args := c.s.B.RoomModes(name, c.n.InRoom(name) || c.u.Sysop)
			c.reply(RPL_CHANNELMODEIS, append([]string{"#" + name, modes}, args...)...)
			return
		}
		c.chanMode(name, m.Params[1], m.Params[2:])
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

// privmsg is PRIVMSG or NOTICE: to the service, to a room the client is
// in, or to each node of a nick's user (CTCP ACTION as an emote, other
// CTCPs dropped, formatting stripped). A NOTICE is never answered with an
// error.
func (c *client) privmsg(command, target, text string) {
	notice := command == "NOTICE"
	if target == "" || text == "" {
		if !notice {
			c.reply(ERR_NOTEXTTOSEND, "No text to send")
		}
		return
	}
	if svc := c.s.service(target); svc != nil {
		if !notice { // a NOTICE to a bot is never answered
			svc.privmsg(c, stripped(text))
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
		if notice && c.s.B.RoomHas(name, 'T') {
			return // +T: no notices (and a NOTICE is never answered)
		}
		if !c.n.InRoom(name) {
			if err := c.n.SayOutside(name, text, emote); err != nil && !notice {
				if errors.Is(err, bbs.ErrNotInRoom) {
					c.reply(ERR_CANNOTSENDTOCHAN, target, "Cannot send to channel (join it first)")
				} else {
					c.reply(ERR_CANNOTSENDTOCHAN, target, "Cannot send to channel: "+err.Error())
				}
			}
			return
		}
		if err := c.n.SayIn(name, text, emote); err != nil && !notice {
			c.reply(ERR_CANNOTSENDTOCHAN, target, "Cannot send to channel: "+err.Error())
		}
		return
	}
	ns := c.nodesOf(target)
	if len(ns) == 0 && c.userOfNick(target) == nil {
		// someone on a linked network?
		err := c.n.MessageRemote(target, text, notice, emote)
		switch {
		case err == nil:
			if emote {
				text = "\x01ACTION " + text + "\x01"
			}
			if c.caps["echo-message"] {
				c.send(map[string]string{"time": stamp(time.Now())}, c.source(), command, target, text)
			}
		case notice:
		case errors.Is(err, bbs.ErrNoSuchNick):
			c.reply(ERR_NOSUCHNICK, target, "No such nick")
		default:
			c.reply(ERR_CANNOTSENDTOCHAN, target, "Cannot send: "+err.Error())
		}
		return
	}
	if emote {
		text = "* " + c.u.Name + " " + text
	}
	if len(ns) == 0 {
		// a user away: kept for them
		u := c.userOfNick(target)
		if u == nil {
			if !notice {
				c.reply(ERR_NOSUCHNICK, target, "No such nick")
			}
			return
		}
		if _, err := c.n.TellUser(u, text); err != nil {
			if !notice {
				c.reply(ERR_CANNOTSENDTOCHAN, target, "Cannot send: "+err.Error())
			}
			return
		}
		if c.caps["echo-message"] {
			c.send(map[string]string{"time": stamp(time.Now())}, c.source(), command, target, text)
		}
		if !notice {
			c.reply(RPL_AWAY, Nick(u.Name), "Not on now; they will have your message when they next log in")
		}
		return
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
	if sent {
		if u, err := c.s.B.Store.UserByName(ns[0].User); err == nil {
			c.n.Keep(u, text, true)
		}
	}
	switch {
	case sent && c.caps["echo-message"], !sent && lastErr == nil:
		// echoed, or to yourself with no other node: back it comes, as IRC
		// servers send a message to one's own nick
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

// sourceOfEvent is how an event's sender is named.
func (c *client) sourceOfEvent(e bbs.Event) string {
	if e.Mask != "" {
		return e.Mask // someone on a linked IRC network
	}
	return c.sourceOf(e.From, e.Node)
}

// sourceOf is how one on a node is named.
func (c *client) sourceOf(name string, node int) string {
	if _, site, ok := strings.Cut(name, "@"); ok && node == 0 {
		// someone on another system, come by a chat network (MRC)
		return fmt.Sprintf("%s!mrc@%s", Nick(name), strings.ToLower(Nick(site)))
	}
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
		c.send(tags, c.sourceOfEvent(e), "PRIVMSG", "#"+e.Room, text)
	case bbs.Joined:
		if e.Node != c.n.Num {
			c.send(tags, c.sourceOfEvent(e), "JOIN", "#"+e.Room)
		}
	case bbs.Left:
		if e.Node != c.n.Num {
			c.send(tags, c.sourceOfEvent(e), "PART", "#"+e.Room)
		}
	case bbs.Topic:
		source := c.s.svc.source(c)
		if c.s.B.RoomReg(e.Room) != nil {
			source = c.s.cs.source(c)
			if e.From != "" && e.From != c.s.cs.Name {
				source = c.sourceOf(e.From, c.nodeOfUser(e.From))
			}
		}
		c.send(tags, source, "TOPIC", "#"+e.Room, e.Text)
	case bbs.Mode:
		switch {
		case strings.HasSuffix(e.Text, "o"), strings.HasSuffix(e.Text, "v"):
			c.send(tags, c.eventSource(e), "MODE", "#"+e.Room, e.Text, Nick(e.Target))
		case e.Target != "":
			c.send(tags, c.eventSource(e), "MODE", "#"+e.Room, e.Text, e.Target)
		default:
			c.send(tags, c.eventSource(e), "MODE", "#"+e.Room, e.Text)
		}
	case bbs.Invited:
		c.send(tags, c.eventSource(e), "INVITE", Nick(e.Target), "#"+e.Room)
	case bbs.Kicked:
		c.send(tags, c.eventSource(e), "KICK", "#"+e.Room, Nick(e.Target), e.Text)
	case bbs.Private:
		text := e.Text
		if e.Kept && !c.caps["server-time"] {
			text = "[" + e.Time.Format("Jan 2 15:04") + "] " + text
		}
		c.send(tags, c.sourceOfEvent(e), "PRIVMSG", c.nick, text)
	case bbs.Paged:
		text := fmt.Sprintf("%s (node %d) is paging you", e.From, e.Node)
		if e.Text != "" {
			text += ": " + e.Text
		}
		c.send(tags, "", "NOTICE", c.nick, text+fmt.Sprintf(" (/join #sysop-%d to answer)", e.Node))
	case bbs.Notice:
		if e.Room != "" && c.n.InRoom(e.Room) {
			c.send(tags, "", "NOTICE", "#"+e.Room, e.Text) // the room's, from a chat network
			return
		}
		source := ""
		if svc := c.s.service(e.From); svc != nil {
			source = svc.source(c) // a room's entry message, from ChanServ
		}
		c.send(tags, source, "NOTICE", c.nick, e.String())
	default:
		c.send(tags, "", "NOTICE", c.nick, e.String())
	}
}
