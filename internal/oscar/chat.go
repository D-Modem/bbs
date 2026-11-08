package oscar

import (
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
)

// exchanges: 4 holds the rooms made by users, 5 the public ones (the
// BBS's permanent rooms); both are the BBS's one set of rooms.
const (
	exchangeUser   = 4
	exchangePublic = 5
)

// roomName is a name an AIM user gave a room as a BBS room's: lower
// case, spaces as dashes, other marks dropped, at most 20 ("": none).
func roomName(name string) string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "#")
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_', r == '.':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > 20 {
		s = strings.TrimRight(s[:20], "-")
	}
	return s
}

// roomCookie is a room's cookie: exchange, instance and name, as AOL's
// were (Pidgin reads the name from the third part).
func roomCookie(exchange uint16, room string) string { return itoa(int(exchange)) + "-0-" + room }

// roomOfCookie is the room of a cookie ("": none).
func roomOfCookie(c string) string {
	parts := strings.SplitN(c, "-", 3)
	if len(parts) == 3 {
		return roomName(parts[2])
	}
	return roomName(c)
}

// roomInfo is a room's info block (chat navigation's, and a chat
// connection's first).
func roomInfo(exchange uint16, room string) []byte {
	var t tlvs
	t.addU16(0x00c9, 15) // flags: evilable, nav only, instancing, occupant peek
	t.addU32(0x00ca, uint32(time.Now().Unix()))
	t.addU16(0x00d1, uint16(bbs.MaxChatLine*4))
	t.addU16(0x00d2, 100)
	t.addU8(0x00d5, 2)
	t.addStr(0x006a, room)
	t.addStr(0x00d3, room)
	t.addU16(0x00da, uint16(bbs.MaxChatLine))
	var b buf
	b.u16(exchange)
	b.str8(roomCookie(exchange, room))
	b.u16(0)
	b.u8(2)
	b.raw(t.block())
	return b
}

// exchangeInfo is an exchange's, for chat navigation.
func exchangeInfo(exchange uint16) []byte {
	var t tlvs
	t.addU8(0x0003, 10)
	t.addU16(0x0002, 0x0010)
	t.addU16(0x0004, 20)
	t.addU16(0x00c9, 15)
	t.addU8(0x00d5, 2)
	t.addStr(0x00d6, "us-ascii")
	t.addStr(0x00d7, "en")
	t.addStr(0x00d8, "us-ascii")
	t.addStr(0x00d9, "en")
	var b buf
	b.u16(exchange)
	b.raw(t.block())
	return b
}

// chatConn is a chat room's connection.
type chatConn struct {
	ss       *session
	fc       *flapConn
	room     string
	exchange uint16
	joined   bool
	reflect  bool // the client wants its own lines back
}

// service serves a service's connection for the session: chat
// navigation, or a chat room.
func (ss *session) service(fc *flapConn, k *cookie) {
	fc.c.SetDeadline(time.Time{})
	var c *chatConn
	ss.mu.Lock()
	if k.service == 0x000e {
		c = &chatConn{ss: ss, fc: fc, room: k.room, exchange: k.exchange}
		ss.chats[c] = true
	} else {
		ss.nav = append(ss.nav, fc)
	}
	ss.mu.Unlock()
	if ss.closed() {
		fc.c.Close()
	}
	defer func() {
		fc.c.Close()
		ss.mu.Lock()
		if c != nil {
			delete(ss.chats, c)
		}
		for i, f := range ss.nav {
			if f == fc {
				ss.nav = append(ss.nav[:i], ss.nav[i+1:]...)
				break
			}
		}
		ss.mu.Unlock()
		if c != nil {
			ss.do(func() {
				if c.joined {
					ss.n.Part(c.room)
				}
			})
		}
	}()
	var b buf
	b.u16(0x0001)
	b.u16(k.service)
	fc.snac(0x0001, 0x0003, 0, 0, b)
	for {
		fc.c.SetReadDeadline(time.Now().Add(idleTimeout))
		f, err := fc.read()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !ss.closed() {
				ss.s.B.Log.Printf("node %d: oscar %04x: %v", ss.n.Num, k.service, err)
			}
			return
		}
		if f.ch == chanSignoff {
			return
		}
		if f.ch != chanData {
			continue
		}
		h, body, err := parseSNAC(f.data)
		if err != nil {
			continue
		}
		r := &rd{p: body}
		switch {
		case h.fam == 0x0001 && h.sub == 0x0002 && c != nil:
			done := make(chan bool, 1)
			ss.do(func() { done <- c.join() })
			select {
			case ok := <-done:
				if !ok {
					return
				}
			case <-ss.done:
				return
			}
		case h.fam == 0x0001:
			ss.oservice(h, r, fc, k.service)
		case h.fam == 0x000d && k.service == 0x000d:
			ss.chatNav(fc, h, r)
		case h.fam == 0x000e && c != nil:
			c.command(h, r)
		default:
			var e buf
			e.u16(0x0008)
			fc.snac(h.fam, 0x0001, 0, h.req, e)
		}
	}
}

// chatNav is family 0x0D: finding and making rooms.
func (ss *session) chatNav(fc *flapConn, h snacHeader, r *rd) {
	reply := func(t tlvs) { fc.snac(0x000d, 0x0009, 0, h.req, t.bytes()) }
	fail := func(code uint16) {
		var b buf
		b.u16(code)
		fc.snac(0x000d, 0x0001, 0, h.req, b)
	}
	switch h.sub {
	case 0x0002: // rights
		var t tlvs
		t.addU8(0x0002, 10)
		t.add(0x0003, exchangeInfo(exchangeUser))
		t.add(0x0003, exchangeInfo(exchangePublic))
		reply(t)
	case 0x0003: // an exchange
		ex := r.u16()
		if ex != exchangeUser && ex != exchangePublic {
			fail(0x0008)
			return
		}
		var t tlvs
		t.addU8(0x0002, 10)
		t.add(0x0003, exchangeInfo(ex))
		reply(t)
	case 0x0004: // a room's info
		ex := r.u16()
		room := roomOfCookie(r.str8())
		if room == "" {
			fail(0x0014)
			return
		}
		var t tlvs
		t.add(0x0004, roomInfo(ex, room))
		reply(t)
	case 0x0008: // make (or find) a room
		ex := r.u16()
		r.str8() // "create"
		r.u16()  // instance
		r.u8()   // detail
		t := r.tlvBlock()
		room := roomName(t.str(0x00d3))
		if room == "" {
			fail(0x0014)
			return
		}
		if ex != exchangeUser && ex != exchangePublic {
			ex = exchangeUser
		}
		var o tlvs
		o.add(0x0004, roomInfo(ex, room))
		reply(o)
	default:
		fail(0x0008)
	}
}

// join takes the session into the room, once its client is ready: the
// room's info, who is in it and its last lines.  False: refused.
func (c *chatConn) join() bool {
	ss := c.ss
	hist, err := ss.n.JoinAlso(c.room)
	if err != nil {
		ss.s.B.Log.Printf("node %d: oscar: room %s: %v", ss.n.Num, c.room, err)
		ss.imFrom(ss.systemName(), "You cannot go into the room "+c.room+": "+err.Error())
		return false
	}
	c.joined = true
	c.send(0x0002, roomInfo(c.exchange, c.room))
	var b buf
	seen := map[string]bool{}
	for _, m := range ss.s.B.RoomMembers(c.room) {
		if seen[strings.ToLower(m.Name)] {
			continue
		}
		seen[strings.ToLower(m.Name)] = true
		b.raw(c.memberInfo(m.Name))
	}
	c.send(0x0003, b)
	for _, e := range hist {
		c.event(e, true)
	}
	return true
}

// send sends a chat SNAC.
func (c *chatConn) send(sub uint16, body []byte) {
	if err := c.fc.snac(0x000e, sub, 0, 0, body); err != nil {
		c.fc.c.Close()
	}
}

// memberInfo is a member's user info, as the chat shows them.
func (c *chatConn) memberInfo(name string) []byte {
	if u, err := c.ss.s.B.Store.UserByName(name); err == nil {
		info, _ := c.ss.s.userInfo(nil, u, false)
		return info
	}
	return plainInfo(name)
}

// command is family 0x0E from the client: a line to the room.
func (c *chatConn) command(h snacHeader, r *rd) {
	if h.sub != 0x0005 {
		var e buf
		e.u16(0x0008)
		c.fc.snac(0x000e, 0x0001, 0, h.req, e)
		return
	}
	r.take(8)
	r.u16()
	t := parseTLVs(r.rest())
	v, _ := t.get(0x0005)
	info := parseTLVs(v)
	text := info.str(0x0001)
	cs := uint16(csASCII)
	switch strings.ToLower(info.str(0x0002)) {
	case "unicode-2-0", "utf-16be":
		cs = csUCS2
	case "iso-8859-1", "iso 8859", "iso-8859-1 ":
		cs = csLatin1
	}
	line := plainText(decodeText([]byte(text), cs))
	c.reflect = t.has(0x0006)
	emote := false
	if rest, ok := strings.CutPrefix(line, "/me "); ok {
		line, emote = rest, true
	}
	if line == "" {
		return
	}
	ss := c.ss
	ss.do(func() {
		if err := ss.n.SayIn(c.room, line, emote); err != nil {
			ss.imFrom(ss.systemName(), "Room "+c.room+": "+err.Error())
		}
	})
}

// line sends the room a line from one in it.
func (c *chatConn) line(from string, text string) {
	p, cs := encodeText(htmlText(text))
	charset := map[uint16]string{csASCII: "us-ascii", csLatin1: "iso-8859-1", csUCS2: "unicode-2-0"}[cs]
	var m tlvs
	m.add(0x0001, p)
	m.addStr(0x0002, charset)
	m.addStr(0x0003, "en")
	var t tlvs
	t.add(0x0003, c.memberInfo(from))
	t.add(0x0001, nil)
	t.add(0x0005, m.bytes())
	var b buf
	b.raw(newICBMCookie())
	b.u16(3)
	b.raw(t.bytes())
	c.send(0x0006, b)
}

// event is a room's event, for its chat connection (hist: a line from
// before the client came in).
func (c *chatConn) event(e bbs.Event, hist bool) {
	ss := c.ss
	switch e.Kind {
	case bbs.Said, bbs.Emoted:
		if e.Node == ss.n.Num && !c.reflect && !hist {
			return
		}
		text := e.Text
		if e.Kind == bbs.Emoted {
			text = "/me " + text
		}
		c.line(e.From, text)
	case bbs.Joined, bbs.Left:
		if e.Node == ss.n.Num {
			return
		}
		for _, m := range ss.s.B.RoomMembers(c.room) {
			if strings.EqualFold(m.Name, e.From) && m.Node != e.Node {
				return // on in the room by another node too
			}
		}
		sub := uint16(0x0003)
		if e.Kind == bbs.Left {
			sub = 0x0004
		}
		c.send(sub, c.memberInfo(e.From))
	case bbs.Topic:
		c.line(ss.s.S.System, "Topic: "+e.Text)
	default:
		c.line(ss.s.S.System, e.String())
	}
}
