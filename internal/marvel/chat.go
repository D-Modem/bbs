package marvel

import (
	"encoding/binary"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/D-Modem/bbs/internal/bbs"
)

// Chat: the BBS's chat rooms, listed in MSN's Chat folder, open in the
// client's chat window (TEXTCHAT).  Opening one asks CONFLOC where the
// room is, then joins it on a CONFSRV pipe; everything said in the room
// after that comes back as events on the join call, which stays open.

// app id of a chat room
const appChat = 4

// a chat room's node: (its id, roomTag)
const roomTag = 0x43484154 // "CHAT"

var keyChat = nodeKey{1, 0x12}

// roomID is a room's number, the same for the server's life.
func (s *Server) roomID(name string) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.roomIDs[name]; ok {
		return id
	}
	id := uint32(0x100 + len(s.roomIDs))
	s.roomIDs[name] = id
	s.roomNames[id] = name
	return id
}

func (s *Server) roomName(id uint32) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.roomNames[id]
	return n, ok
}

type roomEntry struct {
	id     uint32
	name   string
	topic  string
	people int
}

// rooms are the rooms the caller may see: the main one, the permanent
// ones, and those in use.
func (c *conn) rooms() []roomEntry {
	b := c.s.B
	seen := map[string]*roomEntry{bbs.MainRoom: {name: bbs.MainRoom}}
	add := func(ri bbs.RoomInfo) {
		if strings.HasPrefix(ri.Name, "sysop-") {
			return
		}
		e := seen[ri.Name]
		if e == nil {
			e = &roomEntry{name: ri.Name}
			seen[ri.Name] = e
		}
		e.topic, e.people = ri.Topic, len(ri.Users)
	}
	for _, ri := range b.PermanentRooms() {
		add(ri)
	}
	c.mu.Lock()
	n := c.node
	c.mu.Unlock()
	if n != nil {
		for _, ri := range n.Rooms() {
			add(ri)
		}
	}
	var out []roomEntry
	for _, e := range seen {
		e.id = c.s.roomID(e.name)
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// CONFLOC finds a room.
type confloc struct{ c *conn }

const (
	conflocIface     = 0x01
	conflocEditIface = 0x0C
	conflocLocate    = 0x01
	conflocTicket    = 0x05
	locateFound      = 1
	locateNotFound   = 2
)

func (l *confloc) ifaces() []iface {
	return []iface{mosIID(0x52, conflocIface), mosIID(0x72, conflocEditIface)}
}

func (l *confloc) call(h hostBlock) ([]byte, bool) {
	if h.oneWay() {
		return nil, false
	}
	ps, _ := parseParams(h.Params)
	if h.Iface == conflocEditIface {
		// rooms are made in the BBS, not from the client
		if h.Method == conflocTicket {
			return reply{}.dword(refused).blob(nil), true
		}
		return reply{}.dword(refused).dword(0).endStatic(), true
	}
	if h.Method != conflocLocate {
		l.c.logf("CONFLOC: no method %#x", h.Method)
		return nil, false
	}
	var id uint32
	for _, p := range ps {
		if p.Tag == 0x03 {
			id = p.Int
			break
		}
	}
	if _, ok := l.c.s.roomName(id); !ok {
		return reply{}.word(0).word(locateNotFound).endStatic(), true
	}
	return reply{}.word(uint16(id)).word(locateFound).endStatic(), true
}

// CONFSRV is one room, joined.
type chat struct {
	c    *conn
	pipe uint16

	mu     sync.Mutex
	joined bool
	room   string
	iface  byte
	id     uint32 // the join call's: events go back on it
	me     uint32
	ids    map[string]uint32 // participants by name
	stop   chan struct{}
}

const (
	chatSend       = 0x02
	chatJoin       = 0x03
	chatJoined     = 3
	chatRefused    = 8 // "This chat is currently full."
	roleMember     = 1
	evText         = 0
	evList         = 2
	evJoined       = 4
	evLeft         = 5
	chatMessageMax = bbs.MaxChatLine
)

func newChat(c *conn, pipe uint16) *chat {
	return &chat{c: c, pipe: pipe, ids: map[string]uint32{}}
}

func (ch *chat) ifaces() []iface { return []iface{mosIID(0xC8, 0x01)} }

func (ch *chat) call(h hostBlock) ([]byte, bool) {
	if h.oneWay() {
		return nil, false
	}
	ps, _ := parseParams(h.Params)
	switch h.Method {
	case chatJoin:
		var id uint32
		for _, p := range ps {
			if p.Tag == 0x03 {
				id = p.Int
				break
			}
		}
		ch.join(h, id)
		return nil, false // join answers for itself, before the room's news
	case chatSend:
		// always acknowledged: a failure the client reads as a lost line
		ch.c.sendPipe(ch.pipe, hostBlock{Iface: h.Iface, Method: h.Method, ID: h.ID, Params: reply{}.endStatic()}.bytes())
		for _, p := range ps {
			if p.Tag == 0x04 {
				ch.said(p.Data)
				break
			}
		}
		return nil, false
	}
	ch.c.logf("CONFSRV: no method %#x", h.Method)
	return nil, false
}

func (ch *chat) node() *bbs.Node {
	ch.c.mu.Lock()
	defer ch.c.mu.Unlock()
	return ch.c.node
}

func (ch *chat) join(h hostBlock, id uint32) {
	ch.end()
	answer := func(b []byte) {
		ch.c.sendPipe(ch.pipe, hostBlock{Iface: h.Iface, Method: h.Method, ID: h.ID,
			Params: append([]byte{tagEndStatic, tagStreamEnd}, b...)}.bytes())
	}
	name, ok := ch.c.s.roomName(id)
	n := ch.node()
	if !ok || n == nil {
		answer(u16(chatRefused))
		return
	}
	if _, err := n.JoinRoom(name); err != nil {
		ch.c.logf("chat: %s: %v", name, err)
		answer(u16(chatRefused))
		return
	}
	name = n.Room()
	n.Doing("MSN Chat")
	ch.mu.Lock()
	ch.joined, ch.room, ch.iface, ch.id = true, name, h.Iface, h.ID
	ch.me = uint32(n.Num)
	ch.ids = map[string]uint32{}
	ch.stop = make(chan struct{})
	stop := ch.stop
	ch.mu.Unlock()

	title := utf16.Encode([]rune("#" + name))
	rec := binary.LittleEndian.AppendUint16(nil, chatJoined)
	rec = binary.LittleEndian.AppendUint16(rec, 0)
	rec = binary.LittleEndian.AppendUint32(rec, ch.me)
	rec = binary.LittleEndian.AppendUint16(rec, roleMember)
	rec = binary.LittleEndian.AppendUint32(rec, chatMessageMax)
	rec = binary.LittleEndian.AppendUint32(rec, uint32(len(title)+1))
	for _, u := range title {
		rec = binary.LittleEndian.AppendUint16(rec, u)
	}
	answer(append(rec, 0, 0))

	var roster []byte
	ch.mu.Lock()
	for _, m := range ch.c.s.B.RoomMembers(name) {
		roster = append(roster, ch.participant(m.Name, m.Node)...)
	}
	ch.mu.Unlock()
	ch.event(evList, 0, roster)
	go ch.relay(n, stop)
}

// participantID is who a member is in this room: their node's number, or
// for one on a network elsewhere a number of their name's.  ch.mu is held.
func (ch *chat) participantID(name string, node int) uint32 {
	if node > 0 {
		ch.ids[strings.ToLower(name)] = uint32(node)
		return uint32(node)
	}
	if id, ok := ch.ids[strings.ToLower(name)]; ok {
		return id
	}
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	id := h.Sum32() | 0x80000000
	ch.ids[strings.ToLower(name)] = id
	return id
}

// participant is a member's record in the roster.  ch.mu is held.
func (ch *chat) participant(name string, node int) []byte {
	id := ch.participantID(name, node)
	nm := ansi(name)
	b := binary.LittleEndian.AppendUint32(nil, id)
	b = binary.LittleEndian.AppendUint16(b, roleMember)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(nm)))
	b = binary.LittleEndian.AppendUint32(b, 0)
	return append(b, nm...)
}

// event pushes an event on the join call.
func (ch *chat) event(kind uint16, from uint32, payload []byte) {
	ch.mu.Lock()
	joined, iface, id := ch.joined, ch.iface, ch.id
	ch.mu.Unlock()
	if !joined {
		return
	}
	b := binary.LittleEndian.AppendUint16([]byte{tagStreamEnd}, kind)
	b = binary.LittleEndian.AppendUint16(b, 0)
	b = binary.LittleEndian.AppendUint32(b, from)
	ch.c.sendPipe(ch.pipe, hostBlock{Iface: iface, Method: chatJoin, ID: id, Params: append(b, payload...)}.bytes())
}

func wideZ(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return append(b, 0, 0)
}

// relay passes the room's events to the client until it leaves.
func (ch *chat) relay(n *bbs.Node, stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-n.Wake():
		}
		for _, e := range n.Pending() {
			ch.mu.Lock()
			room := ch.room
			ch.mu.Unlock()
			if e.Room != room {
				continue
			}
			switch e.Kind {
			case bbs.Said, bbs.Emoted:
				text := e.Text
				if e.Kind == bbs.Emoted {
					text = "* " + text
				}
				ch.mu.Lock()
				_, known := ch.ids[strings.ToLower(e.From)]
				var rec []byte
				if !known {
					rec = ch.participant(e.From, e.Node)
				}
				from := ch.participantID(e.From, e.Node)
				ch.mu.Unlock()
				if rec != nil {
					ch.event(evJoined, from, rec)
				}
				ch.event(evText, from, wideZ(text))
			case bbs.Joined:
				ch.mu.Lock()
				rec := ch.participant(e.From, e.Node)
				from := ch.participantID(e.From, e.Node)
				ch.mu.Unlock()
				ch.event(evJoined, from, rec)
			case bbs.Left:
				ch.mu.Lock()
				from := ch.participantID(e.From, e.Node)
				delete(ch.ids, strings.ToLower(e.From))
				ch.mu.Unlock()
				ch.event(evLeft, from, nil)
			}
		}
	}
}

// said is a record the client sent: a line of text (others, such as a
// host's change of someone's role, are not taken).
func (ch *chat) said(rec []byte) {
	if len(rec) < 10 || binary.LittleEndian.Uint16(rec) != evText {
		return
	}
	text := strings.TrimSpace(wide(rec[8:]))
	n := ch.node()
	ch.mu.Lock()
	joined := ch.joined
	ch.mu.Unlock()
	if !joined || n == nil || text == "" {
		return
	}
	emote := false
	if rest, ok := strings.CutPrefix(text, "/me "); ok {
		text, emote = rest, true
	}
	if err := n.Say(text, emote); err != nil {
		ch.c.logf("chat: %v", err)
	}
}

// end leaves the room.
func (ch *chat) end() {
	ch.mu.Lock()
	joined, stop := ch.joined, ch.stop
	ch.joined, ch.stop = false, nil
	ch.mu.Unlock()
	if !joined {
		return
	}
	close(stop)
	if n := ch.node(); n != nil {
		n.LeaveRoom()
		n.Doing("MSN")
	}
}

func (ch *chat) cancel(h hostBlock) {
	ch.mu.Lock()
	ours := ch.joined && h.Method == chatJoin && h.ID == ch.id
	ch.mu.Unlock()
	if ours {
		ch.end()
	}
}
