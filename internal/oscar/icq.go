package oscar

import (
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/D-Modem/bbs/internal/store"
)

// ICQ's server requests (family 0x15) are little-endian, in TLV 1: a
// length, the client's UIN, a type, a sequence number, the request.
const (
	icqOfflineReq  = 0x003c
	icqOfflineMsg  = 0x0041
	icqOfflineDone = 0x0042
	icqOfflineAck  = 0x003e
	icqMetaReq     = 0x07d0
	icqMetaReply   = 0x07da

	icqOK   = 0x0a
	icqFail = 0x32
)

// le builds a little-endian body.
type le []byte

func (b *le) u8(v byte)    { *b = append(*b, v) }
func (b *le) u16(v uint16) { *b = binary.LittleEndian.AppendUint16(*b, v) }
func (b *le) u32(v uint32) { *b = binary.LittleEndian.AppendUint32(*b, v) }

// str is a string as ICQ sends them: its length with its NUL, the NUL.
func (b *le) str(s string) {
	p, _ := encodeText(s)
	if len(p) > 1000 {
		p = p[:1000]
	}
	b.u16(uint16(len(p) + 1))
	*b = append(*b, p...)
	b.u8(0)
}

// lrd reads a little-endian body.
type lrd struct{ rd }

func (r *lrd) u16() uint16 { return binary.LittleEndian.Uint16(r.take(2)) }
func (r *lrd) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }
func (r *lrd) str() string {
	return strings.TrimRight(decodeText(r.take(int(r.u16())), csASCII), "\x00")
}

// icqMeta is family 0x15: offline messages (those kept), and the
// directory: users' info and searches.
func (ss *session) icqMeta(h snacHeader, r *rd) {
	if h.sub != 0x0002 {
		ss.fail(0x0015, h.req, 0x0008)
		return
	}
	v, ok := parseTLVs(r.rest()).get(0x0001)
	if !ok {
		return
	}
	q := &lrd{rd{p: v}}
	q.u16() // length
	q.u32() // the client's UIN
	kind := q.u16()
	seq := q.u16()
	if q.bad {
		return
	}
	switch kind {
	case icqOfflineReq:
		ss.icqOffline(h.req, seq)
	case icqOfflineAck:
	case icqMetaReq:
		sub := q.u16()
		ss.icqDirectory(h, seq, sub, q)
	}
}

// icqOffline sends the messages kept while the user was away, each with
// its sender and time (GMT), then that there are no more.
func (ss *session) icqOffline(req uint32, seq uint16) {
	st := ss.s.B.Store
	cs, err := st.KeptChats(ss.u.ID)
	if err != nil {
		ss.s.B.Log.Printf("node %d: kept messages: %v", ss.n.Num, err)
	}
	ids := make([]int64, 0, len(cs))
	for _, c := range cs {
		if o, err := st.UserByID(c.FromID); err == nil && ss.blocks(o) {
			continue // kept back: they are blocked
		}
		t := c.Sent.UTC()
		var b le
		b.u32(uint32(ss.s.S.UINBase) + uint32(c.FromID))
		b.u16(uint16(t.Year()))
		b.u8(byte(t.Month()))
		b.u8(byte(t.Day()))
		b.u8(byte(t.Hour()))
		b.u8(byte(t.Minute()))
		b.u8(0x01) // a text message
		b.u8(0)    // flags
		b.str(c.Text)
		ss.icqReply(req, icqOfflineMsg, seq, b, true)
		ids = append(ids, c.ID)
	}
	if err := st.ChatsDelivered(ids); err != nil {
		ss.s.B.Log.Printf("node %d: kept messages: %v", ss.n.Num, err)
	}
	var b le
	b.u8(0) // none dropped
	ss.icqReply(req, icqOfflineDone, seq, b, false)
}

// icqReply sends a reply of kind (with its sequence number) to the
// client; more: more replies follow.
func (ss *session) icqReply(req uint32, kind, seq uint16, body []byte, more bool) {
	var b le
	b.u32(uint32(ss.s.accountUIN(ss.u)))
	b.u16(kind)
	b.u16(seq)
	b = append(b, body...)
	var env le
	env.u16(uint16(len(b)))
	env = append(env, b...)
	var t tlvs
	t.add(0x0001, env)
	flags := uint16(0)
	if more {
		flags = 0x0001
	}
	if err := ss.fc.snac(0x0015, 0x0003, flags, req, t.bytes()); err != nil {
		ss.close()
	}
}

// metaReply is a directory reply: its subtype, success, the rest.
func (ss *session) metaReply(req uint32, seq, sub uint16, status byte, body []byte, more bool) {
	var b le
	b.u16(sub)
	b.u8(status)
	b = append(b, body...)
	ss.icqReply(req, icqMetaReply, seq, b, more)
}

// accountUIN is an account's ICQ number.
func (s *Server) accountUIN(u *store.User) uint32 { return s.S.UINBase + uint32(u.ID) }

// icqDirectory answers a directory request.
func (ss *session) icqDirectory(h snacHeader, seq, sub uint16, q *lrd) {
	switch sub {
	case 0x04ba: // short info
		u := ss.icqUser(q.u32())
		if u == nil {
			ss.metaReply(h.req, seq, 0x0104, icqFail, nil, false)
			return
		}
		var b le
		b.str(u.Name)
		b.str("")
		b.str("")
		b.str("")
		b.u8(0) // no authorization needed
		b.u8(0)
		b.u8(0)
		ss.metaReply(h.req, seq, 0x0104, icqOK, b, false)
	case 0x04b2, 0x04d0: // full info, in eight replies
		u := ss.icqUser(q.u32())
		if u == nil {
			ss.metaReply(h.req, seq, 0x00c8, icqFail, nil, false)
			return
		}
		ss.icqFullInfo(h.req, seq, u)
	case 0x051f: // search by UIN
		ss.icqFound(h.req, seq, ss.icqUsers(ss.icqUser(q.u32())))
	case 0x0569: // search by UIN, in a TLV
		t := q.rest()
		var found *store.User
		for len(t) >= 4 {
			tag, n := binary.LittleEndian.Uint16(t), int(binary.LittleEndian.Uint16(t[2:]))
			if 4+n > len(t) {
				break
			}
			if tag == 0x0136 && n >= 4 {
				found = ss.icqUser(binary.LittleEndian.Uint32(t[4:]))
			}
			t = t[4+n:]
		}
		ss.icqFound(h.req, seq, ss.icqUsers(found))
	case 0x0515, 0x0533, 0x055f: // by name: first, last, nick (and more)
		first, last, nick := q.str(), q.str(), q.str()
		ss.icqFound(h.req, seq, ss.searchNames(strings.TrimSpace(nick+" "+first+" "+last)))
	case 0x0529, 0x0573: // by e-mail: there are none
		ss.metaReply(h.req, seq, 0x01ae, icqFail, nil, false)
	case 0x03ea, 0x03f3, 0x03fd, 0x0406, 0x040b, 0x0410, 0x041a, 0x0424, 0x0c3a, 0x0654:
		// the user's info: kept in the BBS, not set from here; acked
		acks := map[uint16]uint16{0x03ea: 0x0064, 0x03f3: 0x006e, 0x03fd: 0x0078, 0x0406: 0x0082, 0x040b: 0x0087,
			0x0410: 0x008c, 0x041a: 0x0096, 0x0424: 0x00a0, 0x0c3a: 0x0c3f, 0x0654: 0x031e}
		ss.metaReply(h.req, seq, acks[sub], icqOK, nil, false)
	default:
		ss.metaReply(h.req, seq, sub+0x0a, icqFail, nil, false)
	}
}

// icqUser is the account of an ICQ number (nil: none).
func (ss *session) icqUser(uin uint32) *store.User {
	id := ss.s.accountOfUIN(strconv.FormatUint(uint64(uin), 10))
	if id == 0 {
		return nil
	}
	u, err := ss.s.B.Store.UserByID(id)
	if err != nil {
		return nil
	}
	return u
}

func (ss *session) icqUsers(u *store.User) []*store.User {
	if u == nil {
		return nil
	}
	return []*store.User{u}
}

// searchNames are the accounts whose names hold what was asked, at most
// 20.
func (ss *session) searchNames(q string) []*store.User {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	us, err := ss.s.B.Store.Users()
	if err != nil {
		return nil
	}
	var out []*store.User
	for _, u := range us {
		if strings.Contains(strings.ToLower(u.Name), q) && !u.Locked {
			out = append(out, u)
			if len(out) == 20 {
				break
			}
		}
	}
	return out
}

// icqFound sends search results, the last flagged as last.
func (ss *session) icqFound(req uint32, seq uint16, us []*store.User) {
	if len(us) == 0 {
		ss.metaReply(req, seq, 0x01ae, icqFail, nil, false)
		return
	}
	for i, u := range us {
		var rec le
		rec.u32(ss.s.accountUIN(u))
		rec.str(u.Name)
		rec.str("")
		rec.str("")
		rec.str("")
		rec.u8(0) // no authorization
		online := uint16(0)
		if len(ss.s.nodesOf(u.Name)) > 0 {
			online = 1
		}
		rec.u16(online)
		rec.u8(0)  // gender
		rec.u16(0) // age
		var b le
		b.u16(uint16(len(rec)))
		b = append(b, rec...)
		if i == len(us)-1 {
			b.u32(0) // none left
			b = append(b, make([]byte, 8)...)
			ss.metaReply(req, seq, 0x01ae, icqOK, b, false)
		} else {
			b = append(b, make([]byte, 12)...)
			ss.metaReply(req, seq, 0x01a4, icqOK, b, true)
		}
	}
}

// icqFullInfo sends a user's full info: basic, more, e-mails, home page,
// work, notes (their line about themselves), interests, affiliations.
func (ss *session) icqFullInfo(req uint32, seq uint16, u *store.User) {
	var basic le
	basic.str(u.Name)
	basic.str("")
	basic.str("")
	basic.str("")
	basic.str(u.Location) // city
	for range 6 {         // state, phone, fax, address, cell, zip
		basic.str("")
	}
	basic.u16(0) // country
	basic.u8(0)  // GMT offset
	basic.u8(0)  // no authorization
	basic.u8(0)  // web aware
	basic.u8(0)  // direct connections
	basic.u8(0)  // publish e-mail
	ss.metaReply(req, seq, 0x00c8, icqOK, basic, true)

	var more le
	more.u16(0) // age
	more.u8(0)  // gender
	more.str("")
	more.u16(0) // birth
	more.u8(0)
	more.u8(0)
	more.u8(0) // languages
	more.u8(0)
	more.u8(0)
	more.u16(0)
	more.str("")
	more.str("")
	more.u16(0)
	more.u8(0)
	more = append(more, 0, 0, 0, 0)
	ss.metaReply(req, seq, 0x00dc, icqOK, more, true)

	ss.metaReply(req, seq, 0x00eb, icqOK, le{0}, true) // no e-mails

	var home le
	home.u8(0)
	home.u16(0)
	home.str("")
	home.u8(0)
	ss.metaReply(req, seq, 0x010e, icqOK, home, true)

	var work le
	for range 6 {
		work.str("")
	}
	work.u16(0)
	work.str("")
	work.str("")
	work.str("")
	work.u16(0)
	work.str("")
	ss.metaReply(req, seq, 0x00d2, icqOK, work, true)

	var notes le
	notes.str(u.About)
	ss.metaReply(req, seq, 0x00e6, icqOK, notes, true)

	ss.metaReply(req, seq, 0x00f0, icqOK, le{0}, true)     // no interests
	ss.metaReply(req, seq, 0x00fa, icqOK, le{0, 0}, false) // no affiliations
}
