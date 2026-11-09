package irclink

import (
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// presence puts a BBS user on the network as they come on, and takes
// them off as they go.  It is the BBS's WatchPresence; it does not block.
func (h *Hub) presence(name string, online bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	u := h.local[fold(name)]
	if !online {
		delete(h.killed, fold(name))
		if u != nil {
			h.removeUser(u, "Signed off")
			h.broadcast(line(u.uid, "QUIT", "Signed off"))
		}
		return
	}
	if u != nil {
		return
	}
	h.introduceLocal(name)
}

// introduceLocal puts a BBS user on the network (Nick|Tag if their nick
// is taken), in the linked rooms they are in.  The hub's lock is held.
func (h *Hub) introduceLocal(name string) *user {
	if h.killed[fold(name)] {
		return nil
	}
	nick := nickOf(name)
	if len(nick) > h.S.NickLen {
		nick = nick[:h.S.NickLen]
	}
	if h.nicks[fold(nick)] != nil {
		nick = h.tagged(nick, h.me, nil, "")
	}
	u := &user{uid: h.uid(), nick: nick, ts: time.Now().Unix(), umodes: "+i", ident: "bbs", host: h.S.Host,
		ip: "0", real: name, account: accountOf(name), srv: h.me, local: name, since: time.Now()}
	h.addUser(u)
	for l := range h.links {
		if l.bursted {
			l.sendRaw(h.introduce(u, l))
		}
	}
	return u
}

// accountOf is a BBS name as a services account (no spaces).
func accountOf(name string) string { return strings.ReplaceAll(name, " ", "_") }

// ours is the network user of a BBS user online (put on now if they came
// before the hub knew; nil if killed off).  The hub's lock is held.
func (h *Hub) ours(name string) *user {
	if u := h.local[fold(name)]; u != nil {
		return u
	}
	return h.introduceLocal(name)
}

// Carries: the room is a channel.
func (h *Hub) Carries(room string) bool {
	return strings.HasPrefix(room, h.S.Prefix) && len(room) > len(h.S.Prefix) && !strings.HasPrefix(room, "sysop-")
}

// Allows: anyone who may chat may be in a channel.
func (h *Hub) Allows(room string, u *store.User) bool { return true }

// Joined: a BBS user came into a channel's room.
func (h *Hub) Joined(room, name string) {
	ch := h.channelOf(room)
	if ch == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	u := h.ours(name)
	if u == nil {
		return
	}
	c := h.channel(ch, time.Now().Unix())
	if c.members[u] {
		return
	}
	h.join(u, c, "")
	h.broadcast(line(u.uid, "JOIN", strconv.FormatInt(c.ts, 10), c.name, "+"))
}

// Left: a BBS user left a channel's room (off the channel once no node of
// theirs is in it).
func (h *Hub) Left(room, name string) {
	ch := h.channelOf(room)
	if ch == "" {
		return
	}
	for _, m := range h.B.RoomMembers(room) {
		if m.Node != 0 && strings.EqualFold(m.Name, name) {
			return // still in it on another node
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	u := h.local[fold(name)]
	c := h.chans[fold(ch)]
	if u == nil || c == nil || !c.members[u] {
		return
	}
	h.part(u, c, u.nick, "")
	h.broadcast(line(u.uid, "PART", c.name))
}

// Said: a BBS user said a line in a channel's room.
func (h *Hub) Said(room, name, text string, emote bool) {
	ch := h.channelOf(room)
	if ch == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	u := h.local[fold(name)]
	c := h.chans[fold(ch)]
	if u == nil || c == nil || !c.members[u] {
		return
	}
	if emote {
		text = "\x01ACTION " + text + "\x01"
	}
	h.broadcast(line(u.uid, "PRIVMSG", c.name, text))
}

// Command: a line beginning ! is only talk on IRC.
func (h *Hub) Command(room, name, line string) { h.Said(room, name, "!"+line, false) }

// Members are those of elsewhere in a channel's room.
func (h *Hub) Members(room string) []bbs.RoomMember {
	ch := h.channelOf(room)
	if ch == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.chans[fold(ch)]
	if c == nil {
		return nil
	}
	var out []bbs.RoomMember
	for u := range c.members {
		if u.local == "" {
			out = append(out, bbs.RoomMember{Name: h.display(u), Via: u.ident, Host: u.host})
		}
	}
	return out
}

// Lookup finds someone on the network (not a BBS user) by nick, or as the
// BBS names them.
func (h *Hub) Lookup(nick string) (bbs.RemoteUser, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	u := h.byDisplay(nick)
	if u == nil {
		return bbs.RemoteUser{}, false
	}
	return h.remote(u), true
}

// SendPrivate sends a BBS user's private line to someone on the network.
func (h *Hub) SendPrivate(from, to, text string, notice, emote bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	t := h.byDisplay(to)
	if t == nil {
		return bbs.ErrNoSuchNick
	}
	u := h.ours(from)
	if u == nil {
		return errKilled
	}
	if emote {
		text = "\x01ACTION " + text + "\x01"
	}
	command := "PRIVMSG"
	if notice {
		command = "NOTICE"
	}
	if t.srv.link != nil && t.srv.link.bursted {
		t.srv.link.send(u.uid, command, t.uid, text)
	}
	return nil
}

// errKilled: the user was killed off the network.
var errKilled = errorString("you were removed from the IRC network; it is yours again when you next log in")

type errorString string

func (e errorString) Error() string { return string(e) }
