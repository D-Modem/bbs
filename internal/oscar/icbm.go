package oscar

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// ICBM errors
const (
	icbmNotLoggedOn  = 0x0004
	icbmNotSupported = 0x0009
	icbmRefused      = 0x000a
	icbmRateLimited  = 0x0003
)

// icbm is family 4: instant messages.
func (ss *session) icbm(h snacHeader, r *rd) {
	switch h.sub {
	case 0x0002: // the client's parameters: taken
	case 0x0004:
		var b buf
		b.u16(100) // slots
		b.u32(3)   // flags
		b.u16(512) // longest message the client takes
		b.u16(999) // warning levels
		b.u16(999)
		b.u32(0) // between messages
		ss.snac(0x0004, 0x0005, h.req, b)
	case 0x0006:
		ss.sendICBM(h, r)
	case 0x0008: // warnings: none here
		ss.fail(0x0004, h.req, icbmRefused)
	case 0x000b: // the client's error or auto answer, for the sender
		ss.relayToSender(h, r, 0x000b)
	case 0x0010: // offline messages: none kept
		ss.snac(0x0004, 0x0017, h.req, nil)
	case 0x0014: // typing
		ss.relayToSender(h, r, 0x0014)
	default:
		ss.fail(0x0004, h.req, 0x0008)
	}
}

// relayToSender passes a cookie, channel, name and the rest on to the
// named user's AIM and ICQ sessions, named as the sender to them.
func (ss *session) relayToSender(h snacHeader, r *rd, sub uint16) {
	ck := r.take(8)
	ch := r.u16()
	sn := r.str8()
	rest := r.rest()
	if r.bad {
		return
	}
	u := ss.s.resolve(sn)
	if u == nil {
		return
	}
	for _, o := range ss.s.sessionsOf(u.ID) {
		if o == ss || o.blocks(ss.u) {
			continue
		}
		var b buf
		b.raw(ck)
		b.u16(ch)
		b.str8(o.nameFor(ss.u))
		b.raw(rest)
		o.snac(0x0004, sub, 0, b)
	}
}

// imText is a channel 1 message's text, as sent (HTML, for AIM's).
func imText(data []byte) string {
	r := &rd{p: data}
	var out strings.Builder
	for r.left() >= 4 {
		id := r.u8()
		r.u8() // version
		frag := r.take(int(r.u16()))
		if r.bad {
			break
		}
		if id == 0x01 && len(frag) >= 4 {
			cs := binary.BigEndian.Uint16(frag)
			out.WriteString(decodeText(frag[4:], cs))
		}
	}
	return out.String()
}

// imData is a channel 1 message's data for text.
func imData(text string) []byte {
	p, cs := encodeText(text)
	var b buf
	b.u8(0x05) // features
	b.u8(0x01)
	b.u16(1)
	b.u8(0x01)
	b.u8(0x01) // the text
	b.u8(0x01)
	b.u16(uint16(4 + len(p)))
	b.u16(cs)
	b.u16(0)
	b.raw(p)
	return b
}

// sendICBM is SNAC 4/6: an IM (channel 1), a rendezvous (2: chat
// invitations, files and direct connections between AIM clients), or an
// ICQ message (4).
func (ss *session) sendICBM(h snacHeader, r *rd) {
	ck := r.take(8)
	ch := r.u16()
	sn := r.str8()
	t := parseTLVs(r.rest())
	if r.bad {
		ss.fail(0x0004, h.req, 0x000e)
		return
	}
	u := ss.s.resolve(sn)
	if u == nil {
		ss.fail(0x0004, h.req, icbmNotLoggedOn)
		return
	}
	var err error
	switch ch {
	case 1:
		data, _ := t.get(0x0002)
		raw := imText(data)
		err = ss.deliver(u, ck, raw, !ss.icq, t.has(0x0004))
	case 2:
		data, _ := t.get(0x0005)
		err = ss.rendezvous(u, ck, data)
	case 4:
		data, _ := t.get(0x0005)
		text, ok := icqText(data)
		if !ok {
			return // not text: nothing to pass on
		}
		err = ss.deliver(u, ck, text, false, false)
	default:
		err = errNotSupported
	}
	switch err {
	case nil:
		if t.has(0x0003) {
			var b buf
			b.raw(ck)
			b.u16(ch)
			b.str8(sn)
			ss.snac(0x0004, 0x000c, h.req, b)
		}
	case errNotSupported:
		ss.fail(0x0004, h.req, icbmNotSupported)
	case bbs.ErrFlood:
		ss.fail(0x0004, h.req, icbmRateLimited)
	default:
		ss.fail(0x0004, h.req, icbmNotLoggedOn)
	}
}

var (
	errNotSupported = fmt.Errorf("not supported")
	errOffline      = fmt.Errorf("not online")
)

// deliver gives an IM to each of u's nodes: to an AIM or ICQ session as
// an IM (its HTML kept, from AIM to AIM), to any other as a BBS private
// message (a PRIVMSG on IRC).
func (ss *session) deliver(u *store.User, ck []byte, raw string, isHTML, auto bool) error {
	plain := plainText(raw)
	if plain == "" {
		return nil
	}
	for _, o := range ss.s.sessionsOf(u.ID) {
		if o.blocks(ss.u) {
			return errOffline
		}
	}
	nodes := ss.s.nodesOf(u.Name)
	if len(nodes) == 0 {
		return errOffline
	}
	if auto {
		plain = "(auto reply) " + plain
	}
	native := false
	var lastErr error
	sent := false
	for _, n := range nodes {
		if o := ss.s.onNode(n.Num); o != nil {
			if !native {
				if err := ss.n.Throttle(); err != nil {
					return err
				}
				native = true
			}
			text := raw
			switch {
			case o.icq:
				text = plain
			case !isHTML:
				text = htmlText(plain)
			}
			info, _ := ss.s.userInfo(o, ss.u, false)
			from := ss
			o.do(func() { o.im(ck, info, text, auto, from) })
			sent = true
			continue
		}
		if n.Num == ss.n.Num {
			continue
		}
		if err := ss.n.Message(n.Num, plain); err != nil {
			lastErr = err
		} else {
			sent = true
		}
	}
	if native {
		ss.s.B.Log.Printf("node %d: %s sent an IM to %s", ss.n.Num, ss.u.Name, u.Name)
	}
	if !sent && lastErr != nil {
		return lastErr
	}
	return nil
}

// im gives the client an IM (channel 1).
func (ss *session) im(ck, info []byte, text string, auto bool, from *session) {
	if from != nil && ss.blocks(from.u) {
		return
	}
	var t tlvs
	t.add(0x0002, imData(text))
	if auto {
		t.add(0x0004, nil)
	}
	var b buf
	b.raw(ck)
	b.u16(1)
	b.raw(info)
	b.raw(t.bytes())
	ss.snac(0x0004, 0x0007, 0, b)
}

// imFrom gives the client a line from a BBS user (or the system), as an
// IM.
func (ss *session) imFrom(name string, text string) {
	var info []byte
	if u, err := ss.s.B.Store.UserByName(name); err == nil {
		if ss.blocks(u) {
			return
		}
		info, _ = ss.s.userInfo(ss, u, false)
	} else {
		info = plainInfo(name)
	}
	if !ss.icq {
		text = htmlText(text)
	}
	ss.im(newICBMCookie(), info, text, false, nil)
}

// rendezvous is a channel 2 message: to AIM sessions as it is (the
// sender's address added, for direct connections), a chat invitation
// to anyone else as a line, anything else refused.
func (ss *session) rendezvous(u *store.User, ck, data []byte) error {
	r := &rd{p: data}
	kind := r.u16()
	r.take(8)
	capb := r.take(16)
	inner := parseTLVs(r.rest())
	if r.bad {
		return errNotSupported
	}
	others := ss.s.sessionsOf(u.ID)
	for _, o := range others {
		if o.blocks(ss.u) {
			return errOffline
		}
	}
	if len(others) > 0 {
		if kind == 0 && !inner.has(0x0004) {
			if a := net.ParseIP(ss.host).To4(); a != nil {
				// the sender's address as the server sees it
				var b buf
				b.raw(data[:26])
				inner.add(0x0004, a)
				b.raw(inner.bytes())
				data = b
			}
		}
		if err := ss.n.Throttle(); err != nil {
			return err
		}
		for _, o := range others {
			info, _ := ss.s.userInfo(o, ss.u, false)
			var t tlvs
			t.add(0x0005, data)
			var b buf
			b.raw(ck)
			b.u16(2)
			b.raw(info)
			b.raw(t.bytes())
			o.do(func() { o.snac(0x0004, 0x0007, 0, b) })
		}
		return nil
	}
	if kind != 0 || string(capb) != string(capChat) {
		return errNotSupported
	}
	v, ok := inner.get(0x2711)
	if !ok {
		return errNotSupported
	}
	rr := &rd{p: v}
	rr.u16()
	room := roomOfCookie(rr.str8())
	if rr.bad || room == "" {
		return errNotSupported
	}
	msg := plainText(inner.str(0x000c))
	line := fmt.Sprintf("invites you to the chat room %s", room)
	if msg != "" {
		line += ": " + msg
	}
	nodes := ss.s.nodesOf(u.Name)
	if len(nodes) == 0 {
		return errOffline
	}
	for _, n := range nodes {
		if n.Num != ss.n.Num {
			ss.n.Message(n.Num, line)
		}
	}
	return nil
}

// icqText is an ICQ (channel 4) message's text, if it is one of text.
func icqText(data []byte) (string, bool) {
	if len(data) < 8 {
		return "", false
	}
	kind := data[4]
	n := int(binary.LittleEndian.Uint16(data[6:]))
	if 8+n > len(data) {
		n = len(data) - 8
	}
	text := strings.TrimRight(string(data[8:8+n]), "\x00")
	switch kind {
	case 0x01: // text
	case 0x04: // a URL: its description, FE, the URL
		d, url, _ := strings.Cut(text, "\xfe")
		text = strings.TrimSpace(d + " " + url)
	default:
		return "", false
	}
	return decodeText([]byte(text), csASCII), text != ""
}
