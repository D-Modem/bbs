package bbs

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
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
		if len(r.members) == 0 {
			delete(h.rooms, name)
		}
	}
	h.mu.Unlock()
	b.Log.Printf("chat: %s made #%s permanent no more", by, name)
	return nil
}

// SetRoomTopic sets a permanent room's topic ("": none), and tells those
// in it.
func (b *BBS) SetRoomTopic(name, topic, by string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	topic = Clean(topic, MaxTopic)
	h := b.hub()
	h.mu.Lock()
	r := h.rooms[name]
	if r == nil || !r.permanent {
		h.mu.Unlock()
		return ErrNotPermanent
	}
	h.mu.Unlock()
	if err := b.Store.SetChatTopic(name, topic); err != nil {
		return err
	}
	h.mu.Lock()
	r.topic = topic
	members := r.membersList()
	h.mu.Unlock()
	e := Event{Kind: Topic, Room: name, From: by, Text: topic, Time: time.Now()}
	for _, m := range members {
		m.deliver(e)
	}
	b.Log.Printf("chat: %s set the topic of #%s", by, name)
	return nil
}

// RoomTopic is a room's topic, and whether the room is permanent.
func (b *BBS) RoomTopic(name string) (topic string, permanent bool) {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	if r := h.rooms[name]; r != nil {
		return r.topic, r.permanent
	}
	return "", false
}

// PermanentRooms are the permanent rooms, by name.
func (b *BBS) PermanentRooms() []RoomInfo {
	h := b.hub()
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []RoomInfo
	for name, r := range h.rooms {
		if r.permanent {
			ri := RoomInfo{Name: name, Permanent: true, Topic: r.topic}
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
