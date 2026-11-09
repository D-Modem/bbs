package irclink

import (
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
)

// eventNotice is a notice for a BBS user from the network.
func eventNotice(text string) bbs.Event {
	return bbs.Event{Kind: bbs.Private, From: "IRC", Text: text}
}

// newUser is UID or EUID: a user behind the link.  A nick of a BBS user
// is theirs no more (the BBS user is renamed Nick|Tag); one of a user
// elsewhere goes by the TS6 rules: the newer is killed (both, if as old).
func (h *Hub) newUser(l *link, src, nick, ts, umodes, ident, host, ip, uid, account, real, raw string) {
	s := h.servers[src]
	if s == nil || s.link != l || h.users[uid] != nil || len(uid) != 9 || uid[:3] != s.sid {
		return
	}
	t, _ := strconv.ParseInt(ts, 10, 64)
	u := &user{uid: uid, nick: nick, ts: t, umodes: umodes, ident: ident, host: host, ip: ip, real: real,
		account: account, srv: s, since: time.Now()}
	if o := h.nicks[fold(nick)]; o != nil {
		switch {
		case o.local != "":
			h.renameLocal(o)
		case t < o.ts:
			h.killUser(o, nil, "Nick collision (older nick overruled)")
		case t > o.ts:
			l.send(h.me.sid, "KILL", uid, h.me.name+" (Nick collision (newer killed))")
			return
		default:
			h.killUser(o, nil, "Nick collision (both killed)")
			l.send(h.me.sid, "KILL", uid, h.me.name+" (Nick collision (both killed))")
			return
		}
	}
	h.addUser(u)
	h.forward(l, raw)
}

// nickChange is NICK from a user, by the same rules.
func (h *Hub) nickChange(l *link, src, nick, ts, raw string) {
	u := h.users[src]
	if u == nil || u.local != "" || nick == "" {
		return
	}
	t, _ := strconv.ParseInt(ts, 10, 64)
	if o := h.nicks[fold(nick)]; o != nil && o != u {
		switch {
		case o.local != "":
			h.renameLocal(o)
		case t < o.ts:
			h.killUser(o, nil, "Nick collision (older nick overruled)")
		default:
			h.killUser(u, nil, "Nick collision (newer killed)")
			return
		}
	}
	h.renick(u, nick, t)
	h.forward(l, raw)
}

// renick changes a user's nick, telling the rooms they are in.
func (h *Hub) renick(u *user, nick string, ts int64) {
	was := h.display(u)
	if h.nicks[fold(u.nick)] == u {
		delete(h.nicks, fold(u.nick))
	}
	u.nick, u.ts = nick, ts
	h.nicks[fold(nick)] = u
	if u.local != "" {
		return
	}
	now := h.display(u)
	if fold(now) == fold(was) {
		return
	}
	for ch := range u.chans {
		if r := h.room(ch.name); r != "" {
			h.B.RelayEvent(bbs.Event{Kind: bbs.Notice, Room: r, Text: was + " is now known as " + now})
		}
	}
}

// renameLocal renames a BBS user, whose nick someone elsewhere has, to
// Nick|Tag, and tells the network.
func (h *Hub) renameLocal(u *user) {
	old := u.nick
	nick := h.tagged(nickOf(u.local), h.me, u, old)
	if o := h.nicks[fold(nick)]; o != nil && o != u {
		return
	}
	now := time.Now().Unix()
	h.renick(u, nick, now)
	if h.nicks[fold(old)] == u {
		delete(h.nicks, fold(old))
	}
	h.broadcast(line(u.uid, "NICK", nick, strconv.FormatInt(now, 10)))
	h.B.Log.Printf("irclink: %s is %s on the network (%s was taken)", u.local, nick, old)
}

// killUser takes a user off the network, telling every link but from.
func (h *Hub) killUser(u *user, from *link, why string) {
	h.removeUser(u, why)
	h.forward(from, line(h.me.sid, "KILL", u.uid, h.me.name+" ("+why+")"))
}

// kill is KILL: one elsewhere is gone; one of ours is off the network
// until they come on the BBS again.
func (h *Hub) kill(l *link, src, target, why, raw string) {
	u := h.users[target]
	if u == nil {
		return
	}
	if u.local != "" {
		h.killed[fold(u.local)] = true
		h.B.RelayPrivate(u.local, eventNotice("You were removed from the IRC network: "+why))
		h.B.Log.Printf("irclink: %s killed by %s: %s", u.local, src, why)
	}
	h.removeUser(u, "Killed: "+why)
	h.forward(l, raw)
}

// sjoin is SJOIN: a channel and members behind the link, by the TS rules
// (an older channel's modes and ops win; a newer one's are dropped).
func (h *Hub) sjoin(l *link, ts, name, modes string, rest []string, raw string) {
	if !strings.HasPrefix(name, "#") || len(rest) == 0 {
		return
	}
	t, _ := strconv.ParseInt(ts, 10, 64)
	ch := h.channel(name, t)
	keepTheirs := true
	switch {
	case t < ch.ts:
		ch.ts, ch.modes = t, modes
		for u := range ch.members {
			u.chans[ch] = "" // ours lose their ops
		}
	case t > ch.ts:
		keepTheirs = false
	}
	if len(ch.members) == 0 {
		ch.modes = modes
	}
	for _, m := range strings.Fields(rest[len(rest)-1]) {
		uid := strings.TrimLeft(m, "@+%~&!")
		u := h.users[uid]
		if u == nil || u.srv.link != l {
			continue
		}
		prefix := ""
		if keepTheirs {
			prefix = m[:len(m)-len(uid)]
		}
		h.join(u, ch, prefix)
	}
	h.forward(l, raw)
}

// userJoin is JOIN from a user: "0" parts all, else one channel by the
// simple TS rules.
func (h *Hub) userJoin(l *link, src, ts, name, raw string) {
	u := h.users[src]
	if u == nil {
		return
	}
	if ts == "0" {
		for ch := range u.chans {
			h.part(u, ch, h.display(u), "")
		}
		h.forward(l, raw)
		return
	}
	t, _ := strconv.ParseInt(ts, 10, 64)
	ch := h.channel(name, t)
	if t < ch.ts {
		ch.ts = t
		for o := range ch.members {
			o.chans[ch] = ""
		}
	}
	h.join(u, ch, "")
	h.forward(l, raw)
}

// topic is TOPIC, TB or ETB: a channel's topic, told to its room.
func (h *Hub) topic(l *link, command string, params []string, raw string) {
	if len(params) < 2 {
		return
	}
	ch := h.chans[fold(params[0])]
	if ch != nil {
		text := params[len(params)-1]
		if command == "TB" && len(params) >= 3 {
			// TB keeps the older topic
			if t, _ := strconv.ParseInt(params[1], 10, 64); ch.topic != "" && t >= ch.ts && t != 0 {
				h.forward(l, raw)
				return
			}
		}
		ch.topic = text
		if r := h.room(ch.name); r != "" {
			h.B.RelayEvent(bbs.Event{Kind: bbs.Notice, Room: r, Text: "Topic of " + ch.name + ": " + text})
		}
	}
	h.forward(l, raw)
}

// message is PRIVMSG or NOTICE: to a channel (its room told, and on to
// the others), or to a user (one of ours told; else on toward them).
func (h *Hub) message(l *link, src, command, target, text, raw string) {
	from := h.users[src]
	switch {
	case target == "":
	case strings.HasPrefix(target, "$"): // to the servers matching: everyone
		h.forward(l, raw)
	case strings.ContainsRune("#@+=", rune(target[0])):
		name := strings.TrimLeft(target, "@+=")
		h.forward(l, raw)
		ch := h.chans[fold(name)]
		if ch == nil || from == nil || name != target {
			return // to a channel's ops (or voiced): the BBS's users are neither
		}
		r := h.room(ch.name)
		if r == "" {
			return
		}
		nick := h.display(from)
		e := bbs.Event{Kind: bbs.Said, Room: r, From: nick, Mask: from.mask(nick), Text: text}
		if body, ok := ctcp(text); ok {
			action, isAction := strings.CutPrefix(body, "ACTION ")
			if !isAction {
				return
			}
			e.Kind, e.Text = bbs.Emoted, action
		} else if command == "NOTICE" {
			e.Kind, e.Text = bbs.Notice, "-"+nick+"- "+text
		}
		h.B.RelayEvent(e)
	default:
		u := h.users[target]
		if u == nil {
			if nick, _, ok := strings.Cut(target, "@"); ok {
				u = h.nicks[fold(nick)]
			} else {
				u = h.nicks[fold(target)]
			}
		}
		if u == nil {
			return
		}
		if u.local == "" {
			h.route(u.uid, l, raw)
			return
		}
		if from == nil {
			return // from a server: nothing to answer
		}
		nick := h.display(from)
		if body, ok := ctcp(text); ok {
			action, isAction := strings.CutPrefix(body, "ACTION ")
			if !isAction {
				if command == "PRIVMSG" && body == "VERSION" {
					l.send(u.uid, "NOTICE", from.uid, "\x01VERSION bbs-sec, the chat of "+h.B.Conf().Name+"\x01")
				}
				return
			}
			text = "* " + nick + " " + action
		}
		h.B.RelayPrivate(u.local, bbs.Event{From: nick, Mask: from.mask(nick), Text: text})
	}
}

// ctcp is a CTCP's body (\x01...\x01).
func ctcp(text string) (string, bool) {
	if !strings.HasPrefix(text, "\x01") {
		return "", false
	}
	return strings.TrimSuffix(text[1:], "\x01"), true
}

// whois is WHOIS for a user: answered for one of ours, else sent on.
func (h *Hub) whois(l *link, src, target, nick, raw string) {
	if !h.isMe(target) {
		if u := h.users[target]; u == nil || u.local == "" {
			h.route(target, l, raw)
			return
		}
	}
	from := h.users[src]
	if from == nil {
		return
	}
	if nick == "" {
		nick = target
	}
	reply := func(numeric string, params ...string) {
		l.send(h.me.sid, numeric, append([]string{from.uid}, params...)...)
	}
	u := h.nicks[fold(nick)]
	if u == nil || u.local == "" {
		reply("401", nick, "No such nick/channel")
		reply("318", nick, "End of /WHOIS list.")
		return
	}
	real := u.real
	if bu, err := h.B.Store.UserByName(u.local); err == nil && bu.Location != "" {
		real += ", " + bu.Location
	}
	reply("311", u.nick, u.ident, u.host, "*", real)
	var chans []string
	for ch, prefix := range u.chans {
		chans = append(chans, prefix+ch.name)
	}
	if len(chans) > 0 {
		reply("319", u.nick, strings.Join(chans, " "))
	}
	reply("312", u.nick, h.me.name, h.me.desc)
	for _, n := range h.B.Nodes() {
		if strings.EqualFold(n.User, u.local) {
			reply("320", u.nick, "is on node "+strconv.Itoa(n.Num)+" of "+h.B.Conf().Name+" by "+n.Via+": "+n.Activity)
		}
	}
	if u.away != "" {
		reply("301", u.nick, u.away)
	}
	reply("330", u.nick, u.account, "is logged in as")
	reply("317", u.nick, "0", strconv.FormatInt(u.since.Unix(), 10), "seconds idle, signon time")
	reply("318", u.nick, "End of /WHOIS list.")
}

// query is a query of a server (VERSION, MOTD, ...): answered if it is
// the BBS's, else sent on toward it.
func (h *Hub) query(l *link, src, command string, params []string, raw string) {
	target := ""
	switch command {
	case "STATS", "TRACE":
		if len(params) > 1 {
			target = params[1]
		}
	case "LINKS":
		if len(params) > 1 {
			target = params[0] // LINKS server :mask
		}
	default:
		if len(params) > 0 {
			target = params[len(params)-1]
		}
	}
	if target != "" && !h.isMe(target) {
		h.route(target, l, raw)
		return
	}
	from := h.users[src]
	if from == nil {
		return
	}
	reply := func(numeric string, params ...string) {
		l.send(h.me.sid, numeric, append([]string{from.uid}, params...)...)
	}
	switch command {
	case "VERSION":
		reply("351", "bbs-sec.", h.me.name, "TS6 and P10 hub for the chat of "+h.B.Conf().Name)
	case "MOTD":
		text := strings.TrimRight(h.B.MOTDText(), "\n")
		if text == "" {
			reply("422", "MOTD File is missing")
			return
		}
		reply("375", "- "+h.me.name+" Message of the Day - ")
		for _, s := range strings.Split(text, "\n") {
			reply("372", "- "+strings.TrimRight(s, "\r"))
		}
		reply("376", "End of /MOTD command.")
	case "ADMIN":
		reply("256", h.me.name, "Administrative info")
		reply("257", h.B.Conf().Name)
		reply("258", "Sysop: "+h.B.Conf().Sysop)
	case "INFO":
		reply("371", h.B.Conf().Name+": its chat rooms, as a TS6 and P10 server")
		reply("374", "End of /INFO list.")
	case "TIME":
		reply("391", h.me.name, time.Now().Format(time.RFC1123))
	case "LINKS":
		for _, s := range h.servers {
			up := s
			if s.uplink != nil {
				up = s.uplink
			}
			reply("364", s.name, up.name, strconv.Itoa(s.hops)+" "+s.desc)
		}
		reply("365", "*", "End of /LINKS list.")
	case "STATS":
		reply("219", strings.Join(params[:min(len(params), 1)], ""), "End of /STATS report")
	case "TRACE":
		reply("262", h.me.name, "End of TRACE")
	default:
		reply("265", strconv.Itoa(len(h.local)), "Local users")
	}
}
