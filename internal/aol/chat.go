package aol

import (
	"fmt"
	"strings"

	"github.com/D-Modem/bbs/internal/aol/fdo"
	"github.com/D-Modem/bbs/internal/bbs"
)

// People Connection: the BBS's chat rooms in the client's own room window
// (32-117 in 3.0, 43-22783 in 4.0), whose chat tool takes the room's
// lines and comings and goings as tokens of their own:
//
//	AB  name (10 bytes, padded), 0, text, 0   a line
//	CA  slot, name, 0                         someone came in
//	CB  slot                                  someone left
//
// The client sends CJ for People Connection, and Aa with each line typed.

// chatWindow is the room window's global id, so that a room opened again
// takes the place of the last.
const chatWindow = "32-60001"

// system is the name the host's own lines come from.
const system = "OnlineHost"

// enterChat is CJ: into the BBS's main room.
func (ss *session) enterChat() {
	ss.joinRoom(bbs.MainRoom)
}

// joinRoom takes the client into a room and opens its window there.
func (ss *session) joinRoom(name string) {
	hist, err := ss.n.JoinRoom(name)
	if err != nil {
		ss.alert(fmt.Sprintf("You can't go into %s: %v.", name, err))
		ss.send(waitOff)
		return
	}
	name = ss.n.Room()
	members := ss.s.B.RoomMembers(name)
	ss.mu.Lock()
	ss.room = name
	ss.slots = map[string]byte{}
	ss.nextSlot = 1
	var users []string
	for _, m := range members {
		users = append(users, m.Name)
		ss.slotOf(m.Name)
	}
	ss.mu.Unlock()
	ss.send(closeForm(chatWindow))
	ss.send(roomForm(ss.roomFormID(), name, users))
	ss.send(waitOff)
	ss.chatLine(system, fmt.Sprintf(`*** You are in "%s". ***`, name))
	for _, e := range hist {
		ss.event(e)
	}
}

// roomFormID is the client's own room window: 4.0's or 3.0's.
func (ss *session) roomFormID() string {
	if ss.l.NoCRC() {
		return "43-22783"
	}
	return "32-117"
}

// slotOf is the slot the client's room window has a name in, given it
// the first time.  ss.mu is held.
func (ss *session) slotOf(name string) byte {
	k := strings.ToLower(name)
	if s, ok := ss.slots[k]; ok {
		return s
	}
	s := ss.nextSlot
	if ss.nextSlot < 255 {
		ss.nextSlot++
	}
	ss.slots[k] = s
	return s
}

// leaveChat is CL, the room window closed.
func (ss *session) leaveChat() {
	ss.n.LeaveRoom()
	ss.mu.Lock()
	ss.room = ""
	ss.mu.Unlock()
}

// say is Aa: a line typed in the room window.
func (ss *session) say(atoms []byte) {
	text := strings.TrimSpace(firstText(atoms))
	if text == "" {
		return
	}
	emote := false
	if rest, ok := strings.CutPrefix(text, "/me "); ok {
		text, emote = rest, true
	}
	if err := ss.n.Say(text, emote); err != nil {
		ss.chatLine(system, err.Error())
	}
}

// firstText is the first text a form sent: its first de_data, or the
// atoms themselves when they are plain text.
func firstText(atoms []byte) string {
	if frames, err := fdo.Decode(atoms); err == nil {
		if de := fdo.FindAll(frames, "de_data"); len(de) > 0 {
			return fdo.FromLatin1(trimNul(de[0].Data))
		}
	}
	for _, c := range atoms {
		if c < 0x20 && c != 0 && c != '\r' && c != '\n' && c != '\t' {
			return ""
		}
	}
	return fdo.FromLatin1(trimNul(atoms))
}

func trimNul(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == 0 || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// chatLine is AB: a line in the room window.
func (ss *session) chatLine(from, text string) {
	name := fdo.Latin1(from)
	if len(name) > 10 {
		name = name[:10]
	}
	d := make([]byte, 0, 12+len(text))
	d = append(d, name...)
	for len(d) < 10 {
		d = append(d, ' ')
	}
	d = append(d, 0)
	d = append(d, fdo.Latin1(text)...)
	d = append(d, 0)
	ss.l.WriteToken("AB", d)
}

// roomEvent shows a room's event in the room window, if it is the
// client's room.
func (ss *session) roomEvent(e bbs.Event) {
	ss.mu.Lock()
	in := ss.room != "" && e.Room == ss.room
	var slot byte
	if in && (e.Kind == bbs.Joined || e.Kind == bbs.Left) {
		slot = ss.slotOf(e.From)
		if e.Kind == bbs.Left {
			delete(ss.slots, strings.ToLower(e.From))
		}
	}
	ss.mu.Unlock()
	if !in {
		return
	}
	switch e.Kind {
	case bbs.Said:
		ss.chatLine(e.From, e.Text)
	case bbs.Emoted:
		ss.chatLine(e.From, "* "+e.Text)
	case bbs.Topic:
		ss.chatLine(system, bbs.Event{Kind: bbs.Topic, From: e.From, Text: e.Text}.String())
	case bbs.Joined:
		ss.l.WriteToken("CA", append(append([]byte{slot}, fdo.Latin1(e.From)...), 0))
	case bbs.Left:
		ss.l.WriteToken("CB", []byte{slot})
	}
}

// The room list's window.
const roomsWindow = "50-3-1"

// rooms is LP, the room window's List Rooms: the BBS's rooms, each a
// button that goes in.
func (ss *session) rooms() {
	seen := map[string]bool{}
	var list []bbs.RoomInfo
	for _, r := range ss.n.Rooms() {
		seen[r.Name] = true
		list = append(list, r)
	}
	for _, r := range ss.s.B.PermanentRooms() {
		if !seen[r.Name] {
			seen[r.Name] = true
			list = append(list, r)
		}
	}
	if !seen[bbs.MainRoom] {
		list = append([]bbs.RoomInfo{{Name: bbs.MainRoom, Permanent: true}}, list...)
	}
	var buttons []button
	for _, r := range list {
		label := fmt.Sprintf("%s (%d)", r.Name, len(r.Users))
		if r.Topic != "" {
			label += ": " + clip(r.Topic, 40)
		}
		buttons = append(buttons, button{label, "room " + r.Name})
		if len(buttons) == 30 {
			break
		}
	}
	text := fmt.Sprintf("%s, with how many are in each.  To start a room of your own, "+
		"use the keyword Room and its name (\"room tea\").", count(len(list), "room", "rooms"))
	ss.send(closeForm(roomsWindow))
	ss.send(windowForm(roomsWindow, "Chat Rooms", text, 3, buttons))
}
