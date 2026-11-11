package bbs

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// Channel modes, as Solanum's (Libera.Chat's ircd): a registered room's,
// set by its ops.  They are the room's, so they hold however one comes in.
//
//	n  no lines from outside (IRC's PRIVMSG to a room one is not in)
//	t  only ops (and those with t) set the topic
//	s  secret: not in the room lists, nor its members to those outside
//	p  private: no KNOCK
//	m  moderated: only ops and voices talk
//	i  invite only: by INVITE, an I mask, or the i flag
//	z  op moderated: lines m, b or q stop go to the ops
//	g  anyone in the room may INVITE
//	T  no IRC NOTICEs to the room
//	k  a key to come in    l  how many accounts may be in
//	b  bans  q  quiets  e  exceptions to both  I  exceptions to i
//
// c (formatting stripped) and C (no CTCPs) are always on: the rooms are so.

// RoomFlagModes are the modes on or off, without a parameter.
const RoomFlagModes = "Tgimnpstz"

// RoomFixedModes are always on.
const RoomFixedModes = "Cc"

// RoomListModes are the lists.
const RoomListModes = "Ibeq"

// MaxRoomList is the most entries on each of a room's lists.
const MaxRoomList = 100

// MaxRoomKey is the longest key.
const MaxRoomKey = 23

// Refusals of the modes.
var (
	ErrInviteOnly = errors.New("that room is invite only")
	ErrBadKey     = errors.New("that room needs a key")
	ErrFull       = errors.New("that room is full")
	ErrModerated  = errors.New("that room is moderated: only its ops and voices talk")
	ErrQuieted    = errors.New("you may not talk in that room")
	ErrNoModes    = errors.New("only registered rooms have modes (/cs REGISTER #room)")
	ErrNoNotices  = errors.New("that room takes no notices")
)

// ModeChange is one mode given (On) or taken: its letter, and its
// parameter (a nick for o and v, a mask for the lists, the key, the
// limit).
type ModeChange struct {
	On   bool
	Mode rune
	Arg  string
}

// String is the change as IRC writes it: "+b mask".
func (m ModeChange) String() string {
	s := "-"
	if m.On {
		s = "+"
	}
	s += string(m.Mode)
	if m.Arg != "" {
		s += " " + m.Arg
	}
	return s
}

// ModeTakesArg: the mode has a parameter when given (on) or taken.
func ModeTakesArg(m rune, on bool) bool {
	switch m {
	case 'o', 'v', 'b', 'q', 'e', 'I', 'k':
		return true
	case 'l':
		return on
	}
	return false
}

// RoomModes are a room's modes as IRC shows them ("+Cnt", with "k", "l"),
// and their parameters (the key, shown only if showKey, and the limit).
// A room not registered: "+Cnt" (the BBS's rooms are so).
func (b *BBS) RoomModes(name string, showKey bool) (string, []string) {
	reg := b.RoomReg(name)
	modes := "nt"
	var args []string
	if reg != nil {
		modes = reg.Modes
	}
	letters := []rune(modes + RoomFixedModes)
	sort.Slice(letters, func(i, j int) bool { return letters[i] < letters[j] })
	s := "+" + string(letters)
	if reg != nil && reg.Key != "" {
		s += "k"
		if showKey {
			args = append(args, reg.Key)
		} else {
			args = append(args, "*")
		}
	}
	if reg != nil && reg.Limit > 0 {
		s += "l"
		args = append(args, strconv.Itoa(reg.Limit))
	}
	return s, args
}

// RoomHas: a registered room has a mode on (a room not registered: n and t
// alone).
func (b *BBS) RoomHas(name string, mode rune) bool {
	reg := b.RoomReg(name)
	if reg == nil {
		return mode == 'n' || mode == 't'
	}
	return strings.ContainsRune(reg.Modes, mode)
}

// RoomList is a registered room's entries of a list (nil: none).
func (b *BBS) RoomList(name string, kind rune) []*store.RoomListEntry {
	if reg := b.RoomReg(name); reg != nil {
		return reg.List(string(kind))
	}
	return nil
}

// subject is one a mask is matched against: nick!via@nodeN, and their
// account.
type subject struct {
	nick, user, host, account string
	sysop                     bool
}

// subjectOf is a node's user as masks see them.
func (n *Node) subjectOf() subject {
	n.b.mu.Lock()
	u, via := n.user, n.Caller.Via
	n.b.mu.Unlock()
	s := subject{user: via, host: fmt.Sprintf("node%d", n.Num)}
	if u != nil {
		s.nick, s.account, s.sysop = Nick(u.Name), u.Name, u.Sysop
	}
	return s
}

// NormalMask is a mask as a list keeps it: nick!user@host filled in with
// *s ("alice" is alice!*@*), or an extban ($a, $a:mask, $o, negated with
// ~).  "": not a mask.
func NormalMask(m string) string {
	m = strings.TrimSpace(m)
	if m == "" || len(m) > 100 || strings.ContainsAny(m, " ,") {
		return ""
	}
	if strings.HasPrefix(m, "$") {
		body := m[1:]
		neg := strings.HasPrefix(body, "~")
		body = strings.TrimPrefix(body, "~")
		if body == "" {
			return ""
		}
		kind, data, hasData := strings.Cut(body, ":")
		kind = strings.ToLower(kind)
		switch {
		case kind == "a" && (!hasData || data != ""):
		case kind == "o" && !hasData:
		default:
			return ""
		}
		out := "$"
		if neg {
			out += "~"
		}
		out += kind
		if hasData {
			out += ":" + data
		}
		return out
	}
	if !strings.Contains(m, "!") && !strings.Contains(m, "@") {
		return m + "!*@*"
	}
	if !strings.Contains(m, "@") {
		return m + "@*"
	}
	if !strings.Contains(m, "!") {
		return "*!" + m
	}
	return m
}

// glob matches s with a mask's * and ? (case aside).
func glob(mask, s string) bool {
	mask = strings.NewReplacer("[", "\\[", "]", "\\]", "\\", "\\\\").Replace(strings.ToLower(mask))
	ok, err := path.Match(mask, strings.ToLower(s))
	return err == nil && ok
}

// matches: a mask matches the subject.
func (s subject) matches(mask string) bool {
	if strings.HasPrefix(mask, "$") {
		body := mask[1:]
		neg := strings.HasPrefix(body, "~")
		body = strings.TrimPrefix(body, "~")
		kind, data, hasData := strings.Cut(body, ":")
		var hit bool
		switch kind {
		case "a":
			hit = s.account != "" && (!hasData || glob(data, s.nick) || glob(data, s.account))
		case "o":
			hit = s.sysop
		default:
			return false
		}
		return hit != neg
	}
	return glob(mask, s.nick+"!"+s.user+"@"+s.host)
}

// matchesAny: one of a room's entries of a kind matches.
func (s subject) matchesAny(reg *store.RoomReg, kind string) bool {
	for _, e := range reg.Lists {
		if e.Kind == kind && s.matches(e.Mask) {
			return true
		}
	}
	return false
}

// mayEnter checks a registered room's modes for one coming in: bans (but
// exceptions), invite only, the key and the limit.  Sysops and the
// founder come in always; the i flag gets past i, k and l, as an invite
// does.  It gives whether an invite was used.
func (n *Node) mayEnter(name string, u *store.User, key string) (bool, error) {
	b := n.b
	reg := b.RoomReg(name)
	if reg == nil || u.Sysop || reg.FounderID == u.ID {
		return false, nil
	}
	if b.inRoom(name, u.ID) {
		return false, nil // another node of theirs is in already
	}
	flags := b.ownFlags(reg, u)
	s := n.subjectOf()
	if s.matchesAny(reg, "b") && !s.matchesAny(reg, "e") && !has(flags, FlagExempt) {
		return false, ErrBanned
	}
	invited := b.invited(name, u.ID) || has(flags, FlagInvite)
	if strings.ContainsRune(reg.Modes, 'i') && !invited && !s.matchesAny(reg, "I") {
		return false, ErrInviteOnly
	}
	if reg.Key != "" && key != reg.Key && !invited {
		return false, ErrBadKey
	}
	if reg.Limit > 0 && !invited && b.accountsIn(name) >= reg.Limit {
		return false, ErrFull
	}
	return invited, nil
}

// accountsIn counts the users in a room (one on two nodes once).
func (b *BBS) accountsIn(name string) int {
	seen := map[int64]bool{}
	for _, m := range b.roomNodes(name) {
		if u := m.User(); u != nil {
			seen[u.ID] = true
		}
	}
	return len(seen)
}

// inviteFor is how long an invite lasts.
const inviteFor = time.Hour

// invited: the user has an invite to the room, not yet used.
func (b *BBS) invited(name string, id int64) bool {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[name]
	return r != nil && time.Since(r.invites[id]) < inviteFor
}

// useInvite takes the user's invite to a room: they came in.
func (b *BBS) useInvite(name string, id int64) {
	h := b.hub()
	h.mu.Lock()
	if r := h.rooms[name]; r != nil {
		delete(r.invites, id)
	}
	h.mu.Unlock()
}

// invite gives a user an invite to a room, kept (open) for the hour.
func (b *BBS) invite(name string, id int64) {
	h := b.hub()
	h.mu.Lock()
	r := h.rooms[name]
	if r == nil {
		r = &room{members: map[*Node]bool{}}
		h.rooms[name] = r
	}
	if r.invites == nil {
		r.invites = map[int64]time.Time{}
	}
	r.invites[id] = time.Now()
	h.mu.Unlock()
}

// mayTalk checks a registered room's modes for a line: m, and bans and
// quiets (but exceptions).  Ops, voices and sysops talk always.  It gives
// nil, or why not.
func (n *Node) mayTalk(name string, u *store.User) error {
	b := n.b
	reg := b.RoomReg(name)
	if reg == nil || u == nil || u.Sysop {
		return nil
	}
	h := b.hub()
	h.mu.Lock()
	st := h.statusOf(name, h.rooms[name], u)
	h.mu.Unlock()
	if st != 0 {
		return nil
	}
	if strings.ContainsRune(reg.Modes, 'm') {
		return ErrModerated
	}
	s := n.subjectOf()
	if (s.matchesAny(reg, "b") || s.matchesAny(reg, "q")) && !s.matchesAny(reg, "e") && !has(b.ownFlags(reg, u), FlagExempt) {
		return ErrQuieted
	}
	return nil
}

// opsOf are the nodes in a room of its ops (and sysops).
func (b *BBS) opsOf(name string) []*Node {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[name]
	if r == nil {
		return nil
	}
	var out []*Node
	for m := range r.members {
		if h.statusOf(name, r, m.user)&stOp != 0 || m.user != nil && m.user.Sysop {
			out = append(out, m)
		}
	}
	return out
}

// SetModes is MODE on a registered room by one of its ops (or a sysop):
// each change made, told to those in the room.  It gives the changes made
// and why any were not.
func (n *Node) SetModes(name string, changes []ModeChange) ([]ModeChange, []error) {
	u := n.User()
	if u == nil {
		return nil, []error{ErrNoUser}
	}
	if n.b.RoomReg(name) == nil {
		return nil, []error{ErrNoModes}
	}
	if !u.Sysop && !n.b.IsOp(name, u) {
		return nil, []error{ErrNoPrivs}
	}
	var done []ModeChange
	var errs []error
	for _, c := range changes {
		applied, err := n.setMode(name, u, c)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if applied {
			done = append(done, c)
		}
	}
	return done, errs
}

// ModeError is a refused change, with its mode.
type ModeError struct {
	Mode rune
	Err  error
}

func (e *ModeError) Error() string { return e.Err.Error() }
func (e *ModeError) Unwrap() error { return e.Err }

// setMode makes one change (allowed): whether anything changed.
func (n *Node) setMode(name string, u *store.User, c ModeChange) (bool, error) {
	b := n.b
	modeErr := func(err error) (bool, error) { return false, &ModeError{c.Mode, err} }
	switch {
	case c.Mode == 'o' || c.Mode == 'v':
		t, err := b.UserByNick(c.Arg)
		if err != nil {
			return false, err
		}
		if !b.inRoom(name, t.ID) {
			return false, ErrNotThere
		}
		st := stVoice
		if c.Mode == 'o' {
			st = stOp
		}
		b.give(name, n.name(), n.Num, t, st, c.On, false)
		return false, nil // told by give
	case strings.ContainsRune(RoomFixedModes, c.Mode):
		if !c.On {
			return modeErr(fmt.Errorf("+%c is always on in the BBS's rooms", c.Mode))
		}
		return false, nil
	case strings.ContainsRune(RoomFlagModes, c.Mode):
		reg := b.RoomReg(name)
		on := strings.ContainsRune(reg.Modes, c.Mode)
		if on == c.On {
			return false, nil
		}
		modes := strings.ReplaceAll(reg.Modes, string(c.Mode), "")
		if c.On {
			modes += string(c.Mode)
		}
		if err := b.saveModes(name, &modes, nil, nil); err != nil {
			return false, err
		}
		c.Arg = ""
	case c.Mode == 'k':
		key := ""
		if c.On {
			key = c.Arg
			if key == "" || len(key) > MaxRoomKey || strings.ContainsAny(key, " ,:") {
				return modeErr(fmt.Errorf("a key is 1 to %d letters, with no spaces, commas or colons", MaxRoomKey))
			}
		}
		if b.RoomReg(name).Key == key {
			return false, nil
		}
		if err := b.saveModes(name, nil, &key, nil); err != nil {
			return false, err
		}
		c.Arg = key
		if !c.On {
			c.Arg = "*"
		}
	case c.Mode == 'l':
		limit := 0
		if c.On {
			l, err := strconv.Atoi(c.Arg)
			if err != nil || l < 1 || l > 10000 {
				return modeErr(errors.New("a limit is 1 to 10000"))
			}
			limit = l
			c.Arg = strconv.Itoa(l)
		}
		if b.RoomReg(name).Limit == limit {
			return false, nil
		}
		if err := b.saveModes(name, nil, nil, &limit); err != nil {
			return false, err
		}
	case strings.ContainsRune(RoomListModes, c.Mode):
		mask := NormalMask(c.Arg)
		if mask == "" {
			return modeErr(fmt.Errorf("%q is not a mask (nick!user@host, or $a:account, $o)", c.Arg))
		}
		c.Arg = mask
		kind := string(c.Mode)
		if c.On {
			if len(b.RoomList(name, c.Mode)) >= MaxRoomList {
				return modeErr(fmt.Errorf("the +%c list is full (%d)", c.Mode, MaxRoomList))
			}
			err := b.Store.AddRoomList(name, kind, mask, Nick(u.Name))
			if errors.Is(err, store.ErrExists) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			b.updateReg(name, func(r *store.RoomReg) {
				r.Lists = append(r.Lists, &store.RoomListEntry{Room: name, Kind: kind, Mask: mask, SetBy: Nick(u.Name), SetAt: time.Now()})
			})
		} else {
			// as given, or as normalised
			found := ""
			for _, e := range b.RoomList(name, c.Mode) {
				if strings.EqualFold(e.Mask, mask) || strings.EqualFold(e.Mask, strings.TrimSpace(c.Arg)) {
					found = e.Mask
				}
			}
			if found == "" {
				return false, nil
			}
			if err := b.Store.DelRoomList(name, kind, found); err != nil && !errors.Is(err, store.ErrNotFound) {
				return false, err
			}
			c.Arg = found
			b.updateReg(name, func(r *store.RoomReg) {
				keep := r.Lists[:0]
				for _, e := range r.Lists {
					if !(e.Kind == kind && e.Mask == found) {
						keep = append(keep, e)
					}
				}
				r.Lists = keep
			})
		}
	default:
		return modeErr(fmt.Errorf("%c is not a mode the BBS's rooms have", c.Mode))
	}
	b.tellMode(name, n.name(), n.Num, c)
	return true, nil
}

// saveModes changes a registered room's modes, key or limit (nil: as it
// is), stored and cached.
func (b *BBS) saveModes(name string, modes, key *string, limit *int) error {
	reg := b.RoomReg(name)
	if reg == nil {
		return ErrNoModes
	}
	if modes != nil {
		letters := []rune(*modes)
		sort.Slice(letters, func(i, j int) bool { return letters[i] < letters[j] })
		reg.Modes = string(letters)
	}
	if key != nil {
		reg.Key = *key
	}
	if limit != nil {
		reg.Limit = *limit
	}
	if err := b.Store.SetRoomModes(name, reg.Modes, reg.Key, reg.Limit); err != nil {
		return err
	}
	b.updateReg(name, func(r *store.RoomReg) { r.Modes, r.Key, r.Limit = reg.Modes, reg.Key, reg.Limit })
	return nil
}

// tellMode tells those in a room of a change.
func (b *BBS) tellMode(name, by string, byNode int, c ModeChange) {
	sign := "-"
	if c.On {
		sign = "+"
	}
	e := Event{Kind: Mode, Room: name, From: by, Node: byNode, Text: sign + string(c.Mode), Target: c.Arg, Time: time.Now()}
	for _, m := range b.roomNodes(name) {
		m.deliver(e)
	}
}

// Invite is INVITE: one in a room (an op there if it is invite only, but
// with g; a sysop anywhere) asks another in.  Invited, they get past i,
// k and l for the hour; they are told.
func (n *Node) Invite(name, who string) (*store.User, error) {
	u := n.User()
	if u == nil {
		return nil, ErrNoUser
	}
	if !n.InRoom(name) && !u.Sysop {
		return nil, ErrNotInRoom
	}
	reg := n.b.RoomReg(name)
	if reg != nil && strings.ContainsRune(reg.Modes, 'i') && !strings.ContainsRune(reg.Modes, 'g') &&
		!u.Sysop && !n.b.IsOp(name, u) && !has(n.b.RoomFlags(name, u), FlagInvite) {
		return nil, ErrNoPrivs
	}
	t, err := n.b.UserByNick(who)
	if err != nil {
		return nil, err
	}
	if n.b.inRoom(name, t.ID) {
		return t, ErrAlreadyIn
	}
	nodes := n.b.nodesOfUser(t.ID)
	if len(nodes) == 0 {
		return t, ErrNotOn
	}
	n.b.invite(name, t.ID)
	e := Event{Kind: Invited, Room: name, From: n.name(), Node: n.Num, Target: t.Name, Time: time.Now()}
	for _, o := range nodes {
		o.deliver(e)
	}
	n.b.Log.Printf("chat: %s invited %s into #%s", u.Name, t.Name, name)
	return t, nil
}

// More refusals: of INVITE and KNOCK.
var (
	ErrAlreadyIn = errors.New("they are in that room already")
	ErrNotOn     = errors.New("they are not on")
	ErrRoomOpen  = errors.New("that room is open: just come in")
	ErrNoKnock   = errors.New("that room takes no knocks")
	ErrKnocked   = errors.New("you knocked a moment ago")
)

// Knock is KNOCK: one kept out of a registered room (invite only, keyed
// or full) asks its ops in, once a minute.  A +p room takes none.
func (n *Node) Knock(name, text string) error {
	u := n.User()
	if u == nil {
		return ErrNoUser
	}
	reg := n.b.RoomReg(name)
	switch {
	case n.InRoom(name):
		return ErrAlreadyIn
	case reg == nil || !strings.ContainsRune(reg.Modes, 'i') && reg.Key == "" && (reg.Limit == 0 || n.b.accountsIn(name) < reg.Limit):
		return ErrRoomOpen
	case strings.ContainsRune(reg.Modes, 'p'):
		return ErrNoKnock
	}
	cs := n.chat()
	cs.mu.Lock()
	if time.Since(cs.knocked[name]) < time.Minute {
		cs.mu.Unlock()
		return ErrKnocked
	}
	if cs.knocked == nil {
		cs.knocked = map[string]time.Time{}
	}
	cs.knocked[name] = time.Now()
	cs.mu.Unlock()
	line := fmt.Sprintf("%s (node %d) knocks on %s, asking to come in", Nick(u.Name), n.Num, name)
	if text = Clean(text, MaxTopic); text != "" {
		line += ": " + text
	}
	e := Event{Kind: Notice, Room: "", From: n.name(), Node: n.Num, Text: line + fmt.Sprintf(" (INVITE %s #%s)", Nick(u.Name), name), Time: time.Now()}
	for _, o := range n.b.opsOf(name) {
		o.deliver(e)
	}
	return nil
}

// modeWords are modes given and taken, in words for the text front ends
// ("%s" the room; the mode's parameter follows).
var modeWords = map[string]string{
	"+m": "made %s moderated", "-m": "made %s unmoderated",
	"+i": "made %s invite only", "-i": "opened %s to all",
	"+k": "gave %s a key", "-k": "took the key from %s",
	"+l": "limited %s to", "-l": "took the limit from %s",
	"+b": "banned from %s:", "-b": "lifted a ban in %s:",
	"+q": "quieted in %s:", "-q": "lifted a quiet in %s:",
	"+s": "made %s secret", "-s": "made %s public",
}

// RoomSecret: a registered room is +s.
func (b *BBS) RoomSecret(name string) bool { return b.RoomReg(name) != nil && b.RoomHas(name, 's') }

// ParseModes reads changes as MODE has them ("+mk-i", then their
// parameters); b, q, e and I with no mask left are not changes (they ask
// for the list).
func ParseModes(modes string, args []string) []ModeChange {
	on := true
	var out []ModeChange
	for _, m := range modes {
		switch m {
		case '+':
			on = true
			continue
		case '-':
			on = false
			continue
		}
		c := ModeChange{On: on, Mode: m}
		if ModeTakesArg(m, on) {
			if len(args) == 0 {
				if m == 'k' && !on {
					out = append(out, c)
				}
				continue
			}
			c.Arg, args = args[0], args[1:]
		}
		out = append(out, c)
	}
	return out
}
