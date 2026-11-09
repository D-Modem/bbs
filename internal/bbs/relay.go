package bbs

import (
	"errors"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// Relay links chat rooms to a chat network elsewhere (MRC): it is told
// who comes into the rooms it carries, who leaves and what they say, and
// gives the rooms what is said there (RelayEvent).
type Relay interface {
	Carries(room string) bool
	Allows(room string, u *store.User) bool
	Joined(room, user string)
	Left(room, user string)
	Said(room, user, text string, emote bool)
	// Command is a line typed as !COMMAND in a carried room, for the
	// network's server
	Command(room, user, line string)
}

// MemberLister is a relay that knows who is in its rooms elsewhere (an
// IRC network's users), for the rooms' member lists.
type MemberLister interface {
	Members(room string) []RoomMember
}

// RemoteUser is someone on a chat network elsewhere, as a Messenger finds
// them.
type RemoteUser struct {
	Nick    string // the name they go by there
	User    string // their ident
	Host    string
	Server  string // the server they are on
	Network string // the network's name
	Real    string // their real name (IRC's gecos)
	Account string // their services account ("": none)
	Away    string
	Since   time.Time // when they came on ("": not known)
}

// Messenger is a relay that reaches people elsewhere by name: private
// messages to and from a network's users.
type Messenger interface {
	Lookup(nick string) (RemoteUser, bool)
	SendPrivate(from, to, text string, notice, emote bool) error
}

// SetRelay links the relay's rooms, in place of any others (nil: none).
func (b *BBS) SetRelay(r Relay) {
	b.mu.Lock()
	b.relays = nil
	if r != nil {
		b.relays = []Relay{r}
	}
	b.mu.Unlock()
}

// AddRelay links one more relay's rooms (a room is the first's that
// carries it).
func (b *BBS) AddRelay(r Relay) {
	b.mu.Lock()
	b.relays = append(b.relays, r)
	b.mu.Unlock()
}

// relaysNow are the relays.
func (b *BBS) relaysNow() []Relay {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Relay(nil), b.relays...)
}

// relayFor is the relay that carries room (nil if none does).
func (b *BBS) relayFor(room string) Relay {
	for _, r := range b.relaysNow() {
		if r.Carries(room) {
			return r
		}
	}
	return nil
}

// remoteMembers are those a relay knows in a room it carries.
func (b *BBS) remoteMembers(room string) []RoomMember {
	if ml, ok := b.relayFor(room).(MemberLister); ok {
		return ml.Members(room)
	}
	return nil
}

// LookupRemote finds someone of that name on a chat network.
func (b *BBS) LookupRemote(nick string) (RemoteUser, bool) {
	for _, r := range b.relaysNow() {
		if m, ok := r.(Messenger); ok {
			if u, ok := m.Lookup(nick); ok {
				return u, true
			}
		}
	}
	return RemoteUser{}, false
}

// ErrNoSuchNick: no one of that name is on, here or on a network.
var ErrNoSuchNick = errors.New("no one of that name is on")

// MessageRemote sends a private line to someone on a chat network (a
// notice, or an emote: /me).
func (n *Node) MessageRemote(nick, text string, notice, emote bool) error {
	text = Clean(text, MaxChatLine)
	if text == "" {
		return errors.New("say something")
	}
	u := n.User()
	if u == nil {
		return errors.New("not logged in")
	}
	for _, r := range n.b.relaysNow() {
		m, ok := r.(Messenger)
		if !ok {
			continue
		}
		if _, ok := m.Lookup(nick); !ok {
			continue
		}
		if n.flood() {
			return ErrFlood
		}
		return m.SendPrivate(u.Name, nick, text, notice, emote)
	}
	return ErrNoSuchNick
}

// RelayPrivate gives a user's nodes a private message from a network; it
// is how many nodes were told (0: the user is not on).
func (b *BBS) RelayPrivate(user string, e Event) int {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Kind = Private
	e.Text = Clean(e.Text, 2*MaxChatLine)
	b.mu.Lock()
	var ns []*Node
	for _, n := range b.nodes {
		if n.user != nil && strings.EqualFold(n.user.Name, user) {
			ns = append(ns, n)
		}
	}
	b.mu.Unlock()
	for _, n := range ns {
		n.deliver(e)
	}
	return len(ns)
}

// RelayEvent gives a carried room's members what came from the network
// (said and emoted lines go in its history too).
func (b *BBS) RelayEvent(e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Text = Clean(e.Text, 2*MaxChatLine)
	h := b.hub()
	h.mu.Lock()
	r := h.rooms[e.Room]
	var members []*Node
	if r != nil {
		members = r.membersList()
		if e.Kind == Said || e.Kind == Emoted {
			r.history = append(r.history, e)
			if len(r.history) > historyMax {
				r.history = r.history[len(r.history)-historyMax:]
			}
		}
	}
	h.mu.Unlock()
	for _, m := range members {
		m.deliver(e)
	}
}

// RelayTo tells a user's nodes in a carried room something meant for them
// alone (a network's private message, its server's answer).
func (b *BBS) RelayTo(room, user string, e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Text = Clean(e.Text, 2*MaxChatLine)
	for _, m := range b.RoomMembers(room) {
		if m.Name == user {
			if n := b.node(m.Node); n != nil {
				n.deliver(e)
			}
		}
	}
}
