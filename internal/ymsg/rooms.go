package ymsg

import (
	"regexp"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// A Yahoo chat room is a BBS chat room (an IRC channel, an AIM chat room):
// "Linux, FreeBSD, Solaris:1" is the room linux-freebsd-solar.  A
// conference, Yahoo's invited group IM, is a BBS room of its own, yc- and
// the conference's name: those invited are asked to it, and anyone on the
// BBS who is told its name may join it.

var notRoom = regexp.MustCompile(`[^a-z0-9]+`)

// roomFor is the BBS room a Yahoo room name is.
func roomFor(name string) string {
	name = strings.ToLower(name)
	if i := strings.LastIndexByte(name, ':'); i > 0 {
		name = name[:i] // "Lobby:3": the room's numbered lobbies are one room
	}
	r := strings.Trim(notRoom.ReplaceAllString(name, "-"), "-")
	if len(r) > 20 {
		r = strings.TrimRight(r[:20], "-")
	}
	if r == "" {
		r = "lobby"
	}
	return r
}

// confRoom is the BBS room a conference is.
func confRoom(name string) string {
	r := strings.Trim(notRoom.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if r == "" {
		r = "conf"
	}
	r = "yc-" + r
	if len(r) > 20 {
		r = strings.TrimRight(r[:20], "-")
	}
	return r
}

// roomEvent gives the client a line, a coming or a going in a room it is
// in (hist: from the room's history, on coming in).  False: it is in no
// such room.
func (ss *session) roomEvent(e bbs.Event, hist bool) bool {
	ss.mu.Lock()
	chat, isChat := ss.chats[e.Room]
	conf, isConf := ss.confs[e.Room]
	ss.mu.Unlock()
	if !isChat && !isConf {
		return false
	}
	mine := e.Node == ss.n.Num
	from := idOfName(e.From)
	if e.Node == 0 && e.Kind != bbs.Said && e.Kind != bbs.Emoted {
		from = ss.s.systemID()
	}
	text := e.Text
	switch e.Kind {
	case bbs.Said, bbs.Emoted:
		if mine && !hist {
			return true // the client shows its own lines
		}
	case bbs.Topic:
		from, text = ss.s.systemID(), "Topic: "+e.Text
	case bbs.Notice:
		from = ss.s.systemID()
	case bbs.Joined, bbs.Left:
		if mine || hist {
			return true
		}
		if ss.inRoomElsewhere(e) {
			return true
		}
	}
	if isConf {
		switch e.Kind {
		case bbs.Joined:
			ss.k.send(svcConfLogon, stOK, kv(1, ss.id), kv(53, from), kv(57, conf))
		case bbs.Left:
			ss.k.send(svcConfLogoff, stOK, kv(1, ss.id), kv(56, from), kv(57, conf))
		default:
			if e.Kind == bbs.Emoted {
				text = "* " + e.From + " " + text
			}
			ss.k.send(svcConfMsg, stOK, kv(1, ss.id), kv(3, from), kv(57, conf), kv(14, ss.toClient(text)), kv(97, "1"))
		}
		return true
	}
	switch e.Kind {
	case bbs.Joined:
		ss.k.send(svcChatJoin, stServerAck, append([]pair{kv(1, ss.id), kv(104, chat)}, memberPairs(from, e.From)...)...)
	case bbs.Left:
		ss.k.send(svcChatExit, stServerAck, kv(1, ss.id), kv(104, chat), kv(109, from))
	default:
		kind := "1"
		if e.Kind == bbs.Emoted {
			kind = "2"
		}
		ss.k.send(svcComment, stServerAck, kv(1, ss.id), kv(104, chat), kv(109, from),
			kv(117, ss.toClient(text)), kv(124, kind), kv(97, "1"))
	}
	return true
}

// inRoomElsewhere: the one coming or going is in the room by another node
// as well (so has not come or gone).
func (ss *session) inRoomElsewhere(e bbs.Event) bool {
	for _, m := range ss.s.B.RoomMembers(e.Room) {
		if strings.EqualFold(m.Name, e.From) && m.Node != e.Node {
			return true
		}
	}
	return false
}

// memberPairs are a chat room member's entry (CHATJOIN).
func memberPairs(id, name string) []pair {
	return []pair{kv(109, id), kv(110, "0"), kv(141, name), kv(113, "0"), kv(142, "")}
}

// chatJoin is CHATJOIN (or CHATGOTO): into a chat room, told who is in it
// and its last lines.
func (ss *session) chatJoin(p *packet) {
	name := strings.TrimSpace(p.get(104))
	if name == "" {
		return
	}
	room := roomFor(name)
	ss.mu.Lock()
	_, in := ss.chats[room]
	ss.mu.Unlock()
	if in {
		return
	}
	hist, err := ss.n.JoinAlso(room)
	if err != nil {
		ss.k.send(svcChatJoin, stDisconnected, kv(1, ss.id), kv(104, name), kv(114, "-1"))
		ss.system("You cannot go into the room " + room + ": " + err.Error())
		return
	}
	ss.mu.Lock()
	ss.chats[room] = name
	ss.mu.Unlock()
	topic := ""
	for _, r := range ss.n.Rooms() {
		if r.Name == room {
			topic = r.Topic
		}
	}
	reply := &packet{service: svcChatJoin, status: stServerAck}
	reply.add(1, ss.id).add(104, name).add(105, ss.toClient(topic)).add(128, "0").add(129, "0").add(126, "328704")
	seen := map[string]bool{}
	var members []pair
	for _, m := range ss.s.B.RoomMembers(room) {
		id := idOfName(m.Name)
		if seen[id] {
			continue
		}
		seen[id] = true
		members = append(members, memberPairs(id, m.Name)...)
	}
	if !seen[ss.id] {
		members = append(members, memberPairs(ss.id, ss.u.Name)...)
		seen[ss.id] = true
	}
	reply.addInt(108, int64(len(seen)))
	reply.pairs = append(reply.pairs, members...)
	reply.add(130, "1")
	ss.k.write(reply)
	for _, e := range hist {
		ss.roomEvent(e, true)
	}
}

// chatSay is COMMENT: a line in a chat room.
func (ss *session) chatSay(p *packet) {
	room := roomFor(p.get(104))
	ss.mu.Lock()
	_, in := ss.chats[room]
	ss.mu.Unlock()
	if !in {
		ss.k.send(svcComment, stDisconnected, kv(1, ss.id), kv(114, "-1"))
		return
	}
	text := fromYahoo(p.get(117), p.get(97) == "1")
	if err := ss.n.SayIn(room, text, p.get(124) == "2"); err != nil {
		ss.system("Room " + room + ": " + err.Error())
	}
}

// chatLeave is CHATLEAVE or CHATEXIT: out of a chat room.
func (ss *session) chatLeave(p *packet) {
	room := roomFor(p.get(104))
	ss.mu.Lock()
	name, in := ss.chats[room]
	delete(ss.chats, room)
	ss.mu.Unlock()
	if !in {
		return
	}
	ss.n.Part(room)
	ss.k.send(svcChatExit, stServerAck, kv(1, ss.id), kv(104, name), kv(109, ss.id))
}

// chatLogout is CHATLOGOUT: out of every chat room.
func (ss *session) chatLogout() {
	ss.mu.Lock()
	rooms := make([]string, 0, len(ss.chats))
	for r := range ss.chats {
		rooms = append(rooms, r)
	}
	ss.chats = map[string]string{}
	ss.mu.Unlock()
	for _, r := range rooms {
		ss.n.Part(r)
	}
	ss.k.send(svcChatLogout, stServerAck, kv(1, ss.id))
}

// chatInvite is CHATADDINVITE: a user asked to the chat room, as a
// private message (whatever way they are on).
func (ss *session) chatInvite(p *packet) {
	t := ss.s.resolve(p.get(118))
	room := roomFor(p.get(104))
	if t == nil {
		return
	}
	text := ss.u.Name + " invites you to the chat room " + room
	if m := fromYahoo(p.get(117), p.get(97) == "1"); strings.TrimSpace(m) != "" {
		text += ": " + m
	}
	if _, err := ss.n.TellUser(t, text); err != nil {
		ss.system("Your invitation to " + idOf(t) + " was not sent: " + err.Error())
	}
}

// confInvite is CONFINVITE or CONFADDINVITE: a conference begun (or more
// asked to it).  Its host goes into its room; Yahoo users invited are
// asked by their clients, the others by a private message.
func (ss *session) confInvite(p *packet) {
	name := strings.TrimSpace(p.get(57))
	if name == "" {
		return
	}
	room := confRoom(name)
	ss.mu.Lock()
	_, in := ss.confs[room]
	ss.mu.Unlock()
	if !in {
		if _, err := ss.n.JoinAlso(room); err != nil {
			ss.system("You cannot begin a conference: " + err.Error())
			return
		}
		ss.mu.Lock()
		ss.confs[room] = name
		ss.mu.Unlock()
	}
	invitees := p.all(52)
	if p.service == svcConfAddInvite {
		invitees = p.all(51)
	}
	msg := fromYahoo(p.get(58), p.get(97) == "1")
	var asked []*store.User
	for _, id := range invitees {
		if t := ss.s.resolve(id); t != nil && t.ID != ss.u.ID && !ss.s.B.Blocks(t, ss.u) {
			asked = append(asked, t)
		}
	}
	for _, t := range asked {
		xs := ss.s.sessionsOf(t.ID)
		if len(xs) == 0 {
			text := ss.u.Name + " invites you to a conference: join the chat room " + room
			if strings.TrimSpace(msg) != "" {
				text += " (" + msg + ")"
			}
			ss.n.TellUser(t, text)
			continue
		}
		// the others it is with, as Yahoo IDs
		var with []pair
		for _, m := range ss.s.B.RoomMembers(room) {
			if id := idOfName(m.Name); id != idOf(t) && id != ss.id {
				with = append(with, kv(52, id))
			}
		}
		for _, o := range asked {
			if o.ID != t.ID {
				with = append(with, kv(52, idOf(o)))
			}
		}
		for _, x := range xs {
			x.do(func() {
				x.mu.Lock()
				x.confs[room] = name // so its answer finds the room
				x.mu.Unlock()
				ps := append([]pair{kv(1, x.id), kv(50, ss.id)}, with...)
				ps = append(ps, kv(57, name), kv(58, x.toClient(msg)), kv(13, "0"), kv(97, "1"))
				x.k.send(svcConfInvite, stOK, ps...)
			})
		}
	}
}

// confLogon is CONFLOGON: into a conference the client was asked to.
func (ss *session) confLogon(p *packet) {
	name := strings.TrimSpace(p.get(57))
	if name == "" {
		return
	}
	room := confRoom(name)
	if _, err := ss.n.JoinAlso(room); err != nil {
		ss.system("You cannot join the conference: " + err.Error())
		return
	}
	ss.mu.Lock()
	ss.confs[room] = name
	ss.mu.Unlock()
}

// confDecline is CONFDECLINE: those in it (key 3) are told.
func (ss *session) confDecline(p *packet) {
	name := p.get(57)
	room := confRoom(name)
	ss.mu.Lock()
	delete(ss.confs, room)
	ss.mu.Unlock()
	msg := fromYahoo(p.get(14), p.get(97) == "1")
	for _, m := range ss.s.B.RoomMembers(room) {
		x := ss.s.onNode(m.Node)
		if x == nil {
			continue
		}
		x.do(func() {
			x.mu.Lock()
			conf, ok := x.confs[room]
			x.mu.Unlock()
			if ok {
				x.k.send(svcConfDecline, stOK, kv(1, x.id), kv(54, ss.id), kv(57, conf), kv(14, x.toClient(msg)))
			}
		})
	}
}

// confLogoff is CONFLOGOFF: out of a conference.
func (ss *session) confLogoff(p *packet) {
	room := confRoom(p.get(57))
	ss.mu.Lock()
	_, in := ss.confs[room]
	delete(ss.confs, room)
	ss.mu.Unlock()
	if in {
		ss.n.Part(room)
	}
}

// confMsg is CONFMSG: a line in a conference.
func (ss *session) confMsg(p *packet) {
	room := confRoom(p.get(57))
	ss.mu.Lock()
	_, in := ss.confs[room]
	ss.mu.Unlock()
	if !in || !ss.n.InRoom(room) {
		return
	}
	text := fromYahoo(p.get(14), p.get(97) == "1")
	for _, line := range splitLines(text) {
		if err := ss.n.SayIn(room, line, false); err != nil {
			ss.system("Conference: " + err.Error())
			return
		}
	}
}
