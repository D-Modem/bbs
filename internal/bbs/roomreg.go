package bbs

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// Registered rooms: a room a user owns (ChanServ's REGISTER), with an
// access list of who has which flags there.  In a registered room ops and
// voices are given out (by the flags, as people come in, or by those who
// may), and ops may put people out.  An unregistered room has no ops but
// the sysops, as before.  All of it is the room's, so it holds for every
// way in: IRC, the text BBS, AOL, Yahoo and the rest.

// The access flags, as Atheme's ChanServ has them.
const (
	FlagVoice     = 'v' // VOICE, DEVOICE through ChanServ
	FlagAutoVoice = 'V' // voiced on coming in
	FlagOp        = 'o' // OP, DEOP through ChanServ
	FlagAutoOp    = 'O' // opped on coming in
	FlagTopic     = 't' // TOPIC through ChanServ, and in a TOPICLOCK room
	FlagInvite    = 'i' // INVITE, GETKEY (with the modes to come)
	FlagRemove    = 'r' // KICK, BAN, UNBAN
	FlagRecover   = 'R' // CLEAR
	FlagAccess    = 'f' // changes the access list
	FlagSet       = 's' // SET
	FlagView      = 'A' // sees the access list
	FlagExempt    = 'e' // not kept out by a ban
	FlagBanned    = 'b' // kept out
	FlagFounder   = 'F' // everything
)

// AllRoomFlags are the flags, in the order they are shown.
const AllRoomFlags = "AFORefiorstvVb"

// founderFlags are a founder's: everything but b.
const founderFlags = "AFORefiorstv"

// RoomTemplates are the access lists' shorthands.
var RoomTemplates = map[string]string{
	"VOP":     "AV",
	"AOP":     "AOiortv",
	"SOP":     "AORefiorstv",
	"FOUNDER": founderFlags,
}

// Room status: in a registered room, a user's.
const (
	stOp uint8 = 1 << iota
	stVoice
)

// ChanServName is the nick of the rooms' service, as events name it.
const defaultChanServ = "ChanServ"

// RoomExpiry is how long a registered room may go unused (no one on its
// access list coming in) before it is dropped; 0: never.
const defaultRoomExpiry = 90 * 24 * time.Hour

// Refusals of the rooms' service and of ops' commands.
var (
	ErrBanned        = errors.New("you are banned from that room")
	ErrNotRegistered = errors.New("that room is not registered")
	ErrNoPrivs       = errors.New("you are not an operator in that room")
	ErrNotThere      = errors.New("they are not in that room")
	ErrNoOps         = errors.New("only registered rooms have operators (/cs REGISTER #room)")
)

// SetChanServ names the rooms' service (as events from it name it), and
// how long a registered room may go unused (0: for ever).
func (b *BBS) SetChanServ(name string, expiry time.Duration) {
	b.mu.Lock()
	b.chanServ, b.roomExpiry, b.roomExpirySet = name, expiry, true
	b.mu.Unlock()
}

// ChanServ is the rooms' service's nick.
func (b *BBS) ChanServ() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.chanServ == "" {
		return defaultChanServ
	}
	return b.chanServ
}

func (b *BBS) expiry() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.roomExpirySet {
		return defaultRoomExpiry
	}
	return b.roomExpiry
}

// RoomReg is a registered room (nil: not registered).
func (b *BBS) RoomReg(name string) *store.RoomReg {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.regs[name]; r != nil {
		c := *r
		return &c
	}
	return nil
}

// RoomGuarded: ChanServ holds the room open (its GUARD setting).
func (b *BBS) RoomGuarded(name string) bool {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.regs[name]
	return r != nil && r.Has("guard")
}

// GuardedRooms are the rooms ChanServ holds open.
func (b *BBS) GuardedRooms() []string {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for name, r := range h.regs {
		if r.Has("guard") {
			out = append(out, name)
		}
	}
	return out
}

// kept: the room stays when empty.  The hub's lock is held.
func (h *hub) kept(name string, r *room) bool {
	return r.permanent || h.regs[name] != nil && h.regs[name].Has("guard")
}

// topicOf is a room's topic: a registered room's own, else a permanent
// room's.  The hub's lock is held.
func (h *hub) topicOf(name string, r *room) string {
	if reg := h.regs[name]; reg != nil {
		return reg.Topic
	}
	if r != nil {
		return r.topic
	}
	return ""
}

// RoomFlags are a user's flags in a registered room as they act: the
// founder's (or one with F) everything, a sysop's everything; "" in a
// room not registered.
func (b *BBS) RoomFlags(name string, u *store.User) string {
	reg := b.RoomReg(name)
	if reg == nil || u == nil {
		return ""
	}
	if u.Sysop || reg.FounderID == u.ID {
		return founderFlags
	}
	flags, err := b.Store.RoomFlags(name, u.ID)
	if err != nil {
		b.Log.Printf("chat: flags in #%s: %v", name, err)
		return ""
	}
	if strings.ContainsRune(flags, FlagFounder) {
		return founderFlags
	}
	return flags
}

// ownFlags are a user's flags on the list itself (the founder's all of
// them): what they get on coming in.
func (b *BBS) ownFlags(reg *store.RoomReg, u *store.User) string {
	flags, err := b.Store.RoomFlags(reg.Name, u.ID)
	if err != nil {
		b.Log.Printf("chat: flags in #%s: %v", reg.Name, err)
	}
	if reg.FounderID == u.ID || strings.ContainsRune(flags, FlagFounder) {
		return founderFlags
	}
	return flags
}

func has(flags string, f rune) bool { return strings.ContainsRune(flags, f) }

// banned: u is kept out of the room by its access list.
func (b *BBS) banned(name string, u *store.User) bool {
	reg := b.RoomReg(name)
	if reg == nil || u.Sysop || reg.FounderID == u.ID {
		return false
	}
	flags, _ := b.Store.RoomFlags(name, u.ID)
	return has(flags, FlagBanned) && !has(flags, FlagExempt) && !has(flags, FlagFounder)
}

// statusOf is u's status in a room: in a registered one as given, in
// another op for a sysop.  The hub's lock is held.
func (h *hub) statusOf(name string, r *room, u *store.User) uint8 {
	if u == nil {
		return 0
	}
	if h.regs[name] == nil {
		if u.Sysop {
			return stOp
		}
		return 0
	}
	if r == nil {
		return 0
	}
	return r.status[u.ID]
}

// IsOp: u is an operator in the room.
func (b *BBS) IsOp(name string, u *store.User) bool {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.statusOf(name, h.rooms[name], u)&stOp != 0
}

// arrive gives one coming into a registered room what their flags give
// (ops, voice) and notes the room used; the events are the joiner's to see
// (the others are told).  The hub's lock is not held.
func (n *Node) arrive(name string, u *store.User) []Event {
	b := n.b
	reg := b.RoomReg(name)
	if reg == nil || u == nil {
		return nil
	}
	flags := b.ownFlags(reg, u)
	if flags != "" && time.Since(reg.LastUsed) > time.Hour {
		if err := b.Store.RoomUsed(name); err == nil {
			h := b.hub()
			h.mu.Lock()
			if r := h.regs[name]; r != nil {
				r.LastUsed = time.Now()
			}
			h.mu.Unlock()
		}
	}
	var out []Event
	if has(flags, FlagAutoOp) {
		out = append(out, b.give(name, b.ChanServ(), 0, u, stOp, true, true)...)
	}
	if has(flags, FlagAutoVoice) {
		out = append(out, b.give(name, b.ChanServ(), 0, u, stVoice, true, true)...)
	}
	if reg.EntryMsg != "" {
		out = append(out, Event{Kind: Notice, From: b.ChanServ(), Text: "[#" + name + "] " + reg.EntryMsg, Time: time.Now()})
	}
	return out
}

// give gives (on) or takes a status from u in a registered room, telling
// those in it; arriving, u's own nodes are not told, the event is returned
// instead (for the one coming in to see after the room).  Nothing changes:
// no events.
func (b *BBS) give(name, by string, byNode int, u *store.User, st uint8, on, arriving bool) []Event {
	h := b.hub()
	h.mu.Lock()
	r := h.rooms[name]
	if r == nil || h.regs[name] == nil {
		h.mu.Unlock()
		return nil
	}
	if r.status == nil {
		r.status = map[int64]uint8{}
	}
	was := r.status[u.ID]
	now := was &^ st
	if on {
		now = was | st
	}
	if now == was {
		h.mu.Unlock()
		return nil
	}
	if now == 0 {
		delete(r.status, u.ID)
	} else {
		r.status[u.ID] = now
	}
	members := r.membersList()
	h.mu.Unlock()
	mode := "o"
	if st == stVoice {
		mode = "v"
	}
	sign := "-"
	if on {
		sign = "+"
	}
	e := Event{Kind: Mode, Room: name, From: by, Node: byNode, Text: sign + mode, Target: u.Name, Time: time.Now()}
	var own []Event
	for _, m := range members {
		if mu := m.User(); arriving && mu != nil && mu.ID == u.ID {
			own = []Event{e}
			continue
		}
		m.deliver(e)
	}
	return own
}

// SetStatus is an op's MODE +o, -o, +v or -v on one in the room: by an
// op there, or a sysop.
func (n *Node) SetStatus(name, who string, op, on bool) error {
	u := n.User()
	if u == nil {
		return ErrNoUser
	}
	if n.b.RoomReg(name) == nil {
		return ErrNoOps
	}
	if !u.Sysop && !n.b.IsOp(name, u) {
		return ErrNoPrivs
	}
	return n.setStatus(name, who, op, on)
}

// setStatus is SetStatus once allowed.
func (n *Node) setStatus(name, who string, op, on bool) error {
	t, err := n.b.UserByNick(who)
	if err != nil {
		return err
	}
	if !n.b.inRoom(name, t.ID) {
		return ErrNotThere
	}
	st := stVoice
	if op {
		st = stOp
	}
	n.b.give(name, n.name(), n.Num, t, st, on, false)
	return nil
}

// inRoom: one of the user's nodes is in the room.
func (b *BBS) inRoom(name string, id int64) bool {
	for _, m := range b.roomNodes(name) {
		if u := m.User(); u != nil && u.ID == id {
			return true
		}
	}
	return false
}

// roomNodes are the nodes in a room.
func (b *BBS) roomNodes(name string) []*Node {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[name]; r != nil {
		return r.membersList()
	}
	return nil
}

// Kick is an op's putting one out of a room: by an op there (one with r,
// through the service) or a sysop.  A sysop is put out only by a sysop.
func (n *Node) Kick(name, who, reason string) error {
	u := n.User()
	if u == nil {
		return ErrNoUser
	}
	if !u.Sysop && !n.b.IsOp(name, u) {
		return ErrNoPrivs
	}
	t, err := n.b.UserByNick(who)
	if err != nil {
		return err
	}
	if t.Sysop && !u.Sysop {
		return errors.New("the sysop can't be put out")
	}
	if reg := n.b.RoomReg(name); reg != nil && reg.FounderID == t.ID && !u.Sysop && u.ID != t.ID {
		return errors.New("the founder can't be put out")
	}
	if n.b.kick(name, n.name(), n.Num, t, reason) == 0 {
		return ErrNotThere
	}
	return nil
}

// kick puts a user's nodes out of a room, telling those in it and them;
// it gives how many nodes went.
func (b *BBS) kick(name, by string, byNode int, t *store.User, reason string) int {
	reason = Clean(reason, MaxTopic)
	var gone []*Node
	for _, m := range b.roomNodes(name) {
		if mu := m.User(); mu != nil && mu.ID == t.ID {
			gone = append(gone, m)
		}
	}
	if len(gone) == 0 {
		return 0
	}
	for _, m := range gone {
		cs := m.chat()
		cs.mu.Lock()
		if cs.room == name {
			cs.room = ""
		}
		delete(cs.also, name)
		cs.mu.Unlock()
	}
	h := b.hub()
	h.mu.Lock()
	r := h.rooms[name]
	var others []*Node
	if r != nil {
		for _, m := range gone {
			delete(r.members, m)
		}
		delete(r.status, t.ID)
		others = r.membersList()
		if len(r.members) == 0 && !h.kept(name, r) {
			delete(h.rooms, name)
		}
	}
	h.mu.Unlock()
	e := Event{Kind: Kicked, Room: name, From: by, Node: byNode, Target: t.Name, TargetNode: gone[0].Num, Text: reason, Time: time.Now()}
	for _, m := range append(others, gone...) {
		m.deliver(e)
	}
	if rl := b.relayFor(name); rl != nil {
		rl.Left(name, t.Name)
	}
	b.Log.Printf("chat: %s put %s out of #%s: %s", by, t.Name, name, reason)
	return len(gone)
}

// SetTopic sets a room's topic: a registered room's by its ops (or those
// with t; with TOPICLOCK, only those), a permanent room's by the sysop.
func (n *Node) SetTopic(name, topic string) error {
	u := n.User()
	if u == nil {
		return ErrNoUser
	}
	if reg := n.b.RoomReg(name); reg != nil {
		flags := n.b.RoomFlags(name, u)
		// t (the flag) always; with TOPICLOCK only it; else ops, or anyone
		// in a -t room
		anyone := !strings.ContainsRune(reg.Modes, 't') && n.InRoom(name)
		if !has(flags, FlagTopic) && (reg.Has("topiclock") || !n.b.IsOp(name, u) && !anyone) {
			return errors.New("you may not set that room's topic")
		}
	} else if _, permanent := n.b.RoomTopic(name); !permanent {
		return errors.New("only permanent and registered rooms have topics")
	} else if !u.Sysop {
		return errors.New("a topic is set by the sysop")
	}
	return n.b.SetRoomTopic(name, topic, u.Name)
}

// UserByNick is the BBS user of a name, or of a nick (IRC's: Carol_Ann for
// Carol Ann).
func (b *BBS) UserByNick(nick string) (*store.User, error) {
	nick = strings.TrimSpace(nick)
	if nick == "" {
		return nil, ErrNoUser
	}
	if u, err := b.Store.UserByName(nick); err == nil {
		return u, nil
	}
	us, err := b.Store.Users()
	if err != nil {
		return nil, err
	}
	for _, u := range us {
		if strings.EqualFold(Nick(u.Name), nick) {
			return u, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNoUser, nick)
}

// Nick is a BBS name as an IRC nick: what nicks may not have (spaces,
// dots) as underscores.
func Nick(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, name)
}

// expire drops the registered rooms unused too long, at most once an hour.
func (b *BBS) expire() {
	ex := b.expiry()
	if ex <= 0 {
		return
	}
	h := b.hub()
	h.mu.Lock()
	if time.Since(h.swept) < time.Hour {
		h.mu.Unlock()
		return
	}
	h.swept = time.Now()
	var old []string
	for name, r := range h.regs {
		if time.Since(r.LastUsed) > ex {
			old = append(old, name)
		}
	}
	h.mu.Unlock()
	for _, name := range old {
		if err := b.dropRoom(name); err == nil {
			b.Log.Printf("chat: #%s dropped, unused for %d days", name, int(ex.Hours()/24))
		}
	}
}

// registerRoom registers a room to u, who is given ops if there.
func (n *Node) registerRoom(name string, u *store.User) error {
	b := n.b
	h := b.hub()
	h.mu.Lock()
	_, taken := h.regs[name]
	topic := ""
	if r := h.rooms[name]; r != nil && r.permanent {
		topic = r.topic
	}
	h.mu.Unlock()
	if taken {
		return errors.New("that room is registered already")
	}
	if err := b.Store.RegisterRoom(name, u.ID, founderFlags, u.Name); err != nil {
		if errors.Is(err, store.ErrExists) {
			return errors.New("that room is registered already")
		}
		return err
	}
	if topic != "" {
		_ = b.Store.SetRoomRegTopic(name, topic, u.Name)
	}
	reg, err := b.Store.RoomReg(name)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.regs[name] = reg
	if r := h.rooms[name]; r != nil {
		r.status = map[int64]uint8{}
	}
	h.mu.Unlock()
	b.Log.Printf("chat: %s registered #%s", u.Name, name)
	b.give(name, b.ChanServ(), 0, u, stOp, true, false)
	return nil
}

// dropRoom unregisters a room: its ops and voices go (sysops are its ops
// again), and it goes if empty and not permanent.
func (b *BBS) dropRoom(name string) error {
	if err := b.Store.DropRoom(name); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	h := b.hub()
	h.mu.Lock()
	delete(h.regs, name)
	r := h.rooms[name]
	var members []*Node
	var status map[int64]uint8
	if r != nil {
		status, r.status = r.status, nil
		members = r.membersList()
		if len(r.members) == 0 && !r.permanent {
			delete(h.rooms, name)
		}
	}
	h.mu.Unlock()
	cs := b.ChanServ()
	seen := map[int64]bool{}
	for _, m := range members {
		u := m.User()
		if u == nil || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		var es []Event
		st := status[u.ID]
		if st&stOp != 0 && !u.Sysop {
			es = append(es, Event{Kind: Mode, Room: name, From: cs, Text: "-o", Target: u.Name, Time: time.Now()})
		}
		if st&stOp == 0 && u.Sysop {
			es = append(es, Event{Kind: Mode, Room: name, From: cs, Text: "+o", Target: u.Name, Time: time.Now()})
		}
		if st&stVoice != 0 {
			es = append(es, Event{Kind: Mode, Room: name, From: cs, Text: "-v", Target: u.Name, Time: time.Now()})
		}
		for _, e := range es {
			for _, o := range members {
				o.deliver(e)
			}
		}
	}
	return nil
}

// updateReg changes a registered room as cached, after the store.
func (b *BBS) updateReg(name string, f func(r *store.RoomReg)) {
	h := b.hub()
	h.mu.Lock()
	if r := h.regs[name]; r != nil {
		f(r)
	}
	h.mu.Unlock()
}
