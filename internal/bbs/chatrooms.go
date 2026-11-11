package bbs

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// Permanent chat rooms: kept open when no one is in them, each with a
// topic; made and taken away by the sysop.

// ErrNotPermanent: the room is not a permanent one.
var ErrNotPermanent = errors.New("that room is not a permanent one")

// MaxTopic is the longest topic, in runes.
const MaxTopic = 200

// AddPermanentRoom makes a room permanent (by: who did, for the log).
func (b *BBS) AddPermanentRoom(name, topic, by string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if !roomName.MatchString(name) || strings.HasPrefix(name, "sysop-") {
		return ErrNoRoom
	}
	topic = Clean(topic, MaxTopic)
	if err := b.Store.AddChatRoom(name, topic, by); err != nil {
		if errors.Is(err, store.ErrExists) {
			return errors.New("that room is a permanent one already")
		}
		return err
	}
	h := b.hub()
	h.mu.Lock()
	r := h.rooms[name]
	if r == nil {
		r = &room{members: map[*Node]bool{}}
		h.rooms[name] = r
	}
	r.permanent, r.topic = true, topic
	h.mu.Unlock()
	b.Log.Printf("chat: %s made #%s permanent", by, name)
	return nil
}

// RemovePermanentRoom makes a room permanent no more: it goes when its
// last caller leaves.
func (b *BBS) RemovePermanentRoom(name, by string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := b.Store.DeleteChatRoom(name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotPermanent
		}
		return err
	}
	h := b.hub()
	h.mu.Lock()
	if r := h.rooms[name]; r != nil {
		r.permanent, r.topic = false, ""
		if len(r.members) == 0 && !h.kept(name, r) {
			delete(h.rooms, name)
		}
	}
	h.mu.Unlock()
	b.Log.Printf("chat: %s made #%s permanent no more", by, name)
	return nil
}

// SetRoomTopic sets a permanent or registered room's topic ("": none),
// and tells those in it.
func (b *BBS) SetRoomTopic(name, topic, by string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	topic = Clean(topic, MaxTopic)
	h := b.hub()
	h.mu.Lock()
	r := h.rooms[name]
	reg := h.regs[name]
	permanent := r != nil && r.permanent
	if reg == nil && !permanent {
		h.mu.Unlock()
		return ErrNotPermanent
	}
	h.mu.Unlock()
	if permanent {
		if err := b.Store.SetChatTopic(name, topic); err != nil {
			return err
		}
	}
	if reg != nil {
		if err := b.Store.SetRoomRegTopic(name, topic, by); err != nil {
			return err
		}
	}
	h.mu.Lock()
	if permanent {
		r.topic = topic
	}
	if reg := h.regs[name]; reg != nil {
		reg.Topic, reg.TopicBy, reg.TopicAt = topic, by, time.Now()
	}
	var members []*Node
	if r != nil {
		members = r.membersList()
	}
	h.mu.Unlock()
	e := Event{Kind: Topic, Room: name, From: by, Text: topic, Time: time.Now()}
	for _, m := range members {
		m.deliver(e)
	}
	b.Log.Printf("chat: %s set the topic of #%s", by, name)
	return nil
}

// RoomTopic is a room's topic (a registered room's own, else a permanent
// room's), and whether the room is permanent.
func (b *BBS) RoomTopic(name string) (topic string, permanent bool) {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[name]
	return h.topicOf(name, r), r != nil && r.permanent
}

// PermanentRooms are the permanent rooms, by name.
func (b *BBS) PermanentRooms() []RoomInfo {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []RoomInfo
	for name, r := range h.rooms {
		if r.permanent {
			ri := RoomInfo{Name: name, Permanent: true, Topic: h.topicOf(name, r), Registered: h.regs[name] != nil}
			for m := range r.members {
				ri.Users = append(ri.Users, m.name())
			}
			sort.Strings(ri.Users)
			out = append(out, ri)
		}
	}
	sortRooms(out)
	return out
}
