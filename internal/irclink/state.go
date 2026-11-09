package irclink

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
)

// server is a server on the network.
type server struct {
	sid, name, desc string
	hops            int
	uplink          *server // nil: the BBS itself
	link            *link   // the link it is behind (nil: the BBS itself)
	tag             string  // its users' tag ("": its name's)
}

// user is someone on the network.
type user struct {
	uid, nick string
	ts        int64
	umodes    string
	ident     string
	host      string
	ip        string
	real      string
	account   string
	away      string
	srv       *server
	local     string // the BBS user's name, for one of ours ("": elsewhere)
	since     time.Time
	chans     map[*channel]string // the channels they are in, their prefixes (@, +)
}

// channel is a channel on the network.
type channel struct {
	name    string // as it was first given (#Retro)
	ts      int64
	modes   string
	topic   string
	members map[*user]bool
}

// mask is how a user is named: nick!ident@host.
func (u *user) mask(nick string) string { return nick + "!" + u.ident + "@" + u.host }

// uid makes the next UID of ours: our SID, a letter and five letters or
// digits.
func (h *Hub) uid() string {
	for {
		n := h.nextUID
		h.nextUID++
		const digits = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		b := []byte(h.me.sid + "AAAAAA")
		for i := 8; i > 3; i-- {
			b[i] = digits[n%36]
			n /= 36
		}
		b[3] = digits[n%26]
		if id := string(b); h.users[id] == nil {
			return id
		}
	}
}

// tagFor is the tag of a server's users.
func (h *Hub) tagFor(s *server) string {
	if s.tag != "" {
		return s.tag
	}
	if p := h.peer(s.name); p != nil {
		return p.Tag
	}
	return defaultTag(s.name)
}

// tagged is a nick with a server's tag (Nick|Tag), cut to the network's
// nick length, and numbered if that too is taken (by another than self,
// or is avoid).
func (h *Hub) tagged(nick string, s *server, self *user, avoid string) string {
	tag := h.tagFor(s)
	for i := 1; ; i++ {
		suffix := "|" + tag
		if i > 1 {
			suffix += strconv.Itoa(i)
		}
		base := nick
		if max := h.S.NickLen - len(suffix); len(base) > max {
			if max < 1 {
				max = 1
			}
			base = base[:max]
		}
		n := base + suffix
		if o := h.nicks[fold(n)]; (o == nil || o == self) && fold(n) != fold(avoid) {
			return n
		}
	}
}

// display is how a user of elsewhere is named on the BBS: their nick, or
// Nick|Tag if a BBS user online goes by it.
func (h *Hub) display(u *user) string {
	if u.local != "" {
		return u.local
	}
	if h.localNick(u.nick) {
		return h.tagged(u.nick, u.srv, u, "")
	}
	return u.nick
}

// localNick: a BBS user online is called that, as the BBS's IRC server
// names them.
func (h *Hub) localNick(nick string) bool {
	for _, l := range h.local {
		if fold(nickOf(l.local)) == fold(nick) {
			return true
		}
	}
	return false
}

// byDisplay finds a user of elsewhere by their nick or as the BBS names
// them.
func (h *Hub) byDisplay(nick string) *user {
	if u := h.nicks[fold(nick)]; u != nil && u.local == "" && fold(h.display(u)) == fold(nick) {
		return u
	}
	for _, u := range h.users {
		if u.local == "" && fold(h.display(u)) == fold(nick) {
			return u
		}
	}
	return nil
}

// room is a channel's BBS room ("": not one carried).
func (h *Hub) room(ch string) string {
	name, ok := strings.CutPrefix(fold(ch), "#")
	if !ok {
		return ""
	}
	r := h.S.Prefix + name
	if !roomRE.MatchString(r) || strings.HasPrefix(r, "sysop-") {
		return ""
	}
	return r
}

// channelOf is a room's channel name ("": not carried).
func (h *Hub) channelOf(room string) string {
	if !h.Carries(room) {
		return ""
	}
	return "#" + strings.TrimPrefix(room, h.S.Prefix)
}

// addUser puts a user on the network.
func (h *Hub) addUser(u *user) {
	if u.chans == nil {
		u.chans = map[*channel]string{}
	}
	h.users[u.uid] = u
	h.nicks[fold(u.nick)] = u
	if u.local != "" {
		h.local[fold(u.local)] = u
	}
}

// removeUser takes a user off the network, out of their channels (the
// rooms told).
func (h *Hub) removeUser(u *user, why string) {
	name := h.display(u)
	for ch := range u.chans {
		h.part(u, ch, name, why)
	}
	delete(h.users, u.uid)
	if h.nicks[fold(u.nick)] == u {
		delete(h.nicks, fold(u.nick))
	}
	if u.local != "" && h.local[fold(u.local)] == u {
		delete(h.local, fold(u.local))
	}
}

// channel finds a channel, made with ts if new.
func (h *Hub) channel(name string, ts int64) *channel {
	ch := h.chans[fold(name)]
	if ch == nil {
		ch = &channel{name: name, ts: ts, modes: "+nt", members: map[*user]bool{}}
		h.chans[fold(name)] = ch
	}
	return ch
}

// join puts a user in a channel, its room told of one of elsewhere.
func (h *Hub) join(u *user, ch *channel, prefix string) {
	if ch.members[u] {
		return
	}
	ch.members[u] = true
	u.chans[ch] = prefix
	if r := h.room(ch.name); r != "" && u.local == "" {
		h.B.RelayEvent(bbs.Event{Kind: bbs.Joined, Room: r, From: h.display(u), Mask: u.mask(h.display(u))})
	}
}

// part takes a user out of a channel, its room told of one of elsewhere
// (name: as they were named).
func (h *Hub) part(u *user, ch *channel, name, why string) {
	if !ch.members[u] {
		return
	}
	delete(ch.members, u)
	delete(u.chans, ch)
	if len(ch.members) == 0 {
		delete(h.chans, fold(ch.name))
	}
	if r := h.room(ch.name); r != "" && u.local == "" {
		h.B.RelayEvent(bbs.Event{Kind: bbs.Left, Room: r, From: name, Mask: u.mask(name), Text: why})
	}
}

// behind are the servers behind a server (it too), farthest first.
func (h *Hub) behind(s *server) []*server {
	var out []*server
	for _, o := range h.servers {
		for p := o; p != nil; p = p.uplink {
			if p == s {
				out = append(out, o)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].hops > out[j].hops })
	return out
}

// removeServer takes a server and those behind it off the network, with
// their users.
func (h *Hub) removeServer(s *server, why string) {
	gone := h.behind(s)
	set := map[*server]bool{}
	for _, o := range gone {
		set[o] = true
	}
	for _, u := range h.users {
		if set[u.srv] {
			h.removeUser(u, why)
		}
	}
	for _, o := range gone {
		delete(h.servers, o.sid)
		delete(h.names, fold(o.name))
	}
}

// introduce is the line that puts a user on the network, for a link.
func (h *Hub) introduce(u *user, l *link) string {
	modes := u.umodes
	if modes == "" {
		modes = "+"
	}
	ip := u.ip
	if ip == "" {
		ip = "0"
	}
	if l.hybrid {
		account := u.account
		if account == "" {
			account = "*"
		}
		return line(u.srv.sid, "UID", u.nick, strconv.Itoa(u.srv.hops+1), strconv.FormatInt(u.ts, 10), modes,
			u.ident, u.host, u.host, ip, u.uid, account, u.real)
	}
	if l.caps["EUID"] {
		account := u.account
		if account == "" {
			account = "*"
		}
		return line(u.srv.sid, "EUID", u.nick, strconv.Itoa(u.srv.hops+1), strconv.FormatInt(u.ts, 10), modes,
			u.ident, u.host, ip, u.uid, "*", account, u.real)
	}
	return line(u.srv.sid, "UID", u.nick, strconv.Itoa(u.srv.hops+1), strconv.FormatInt(u.ts, 10), modes,
		u.ident, u.host, ip, u.uid, u.real)
}

// remote is a user of elsewhere as the BBS shows them.
func (h *Hub) remote(u *user) bbs.RemoteUser {
	return bbs.RemoteUser{Nick: h.display(u), User: u.ident, Host: u.host, Server: u.srv.name,
		Network: h.S.Network, Real: u.real, Account: u.account, Away: u.away, Since: u.since}
}

// String is the user, for the log.
func (u *user) String() string { return fmt.Sprintf("%s(%s)", u.nick, u.uid) }
