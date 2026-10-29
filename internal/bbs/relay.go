package bbs

import (
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

// SetRelay links the relay's rooms (nil: none).
func (b *BBS) SetRelay(r Relay) {
	b.mu.Lock()
	b.relay = r
	b.mu.Unlock()
}

// relayFor is the relay if it carries room (nil if not, or no relay).
func (b *BBS) relayFor(room string) Relay {
	b.mu.Lock()
	r := b.relay
	b.mu.Unlock()
	if r != nil && r.Carries(room) {
		return r
	}
	return nil
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
