package oscar

import (
	"encoding/hex"
	"fmt"
	"html"
	"net"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// capabilities (16-byte GUIDs)
var (
	capChat        = mustCap("748F2420628711D18222444553540000")
	capICQRelay    = mustCap("094613494C7F11D18222444553540000")
	capUTF8        = mustCap("0946134E4C7F11D18222444553540000")
	capTypingNotes = mustCap("563FC8090B6F41BD9F79422609DFA2F3")
)

func mustCap(h string) []byte {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 16 {
		panic(h)
	}
	return b
}

// user classes
const (
	classUnconfirmed = 0x0001
	classAdmin       = 0x0002
	classFree        = 0x0010
	classAway        = 0x0020
	classICQ         = 0x0040
)

// userInfo is an account's user info block as viewer sees it (self: its
// own), and whether it is online to viewer at all.
func (s *Server) userInfo(viewer *session, u *store.User, self bool) ([]byte, bool) {
	name := u.Name
	if viewer != nil {
		name = viewer.nameFor(u)
	}
	if self {
		name = viewer.sn
	}
	nodes := s.nodesOf(u.Name)
	others := s.sessionsOf(u.ID)
	online := len(nodes) > 0 || self
	if !self && viewer != nil {
		for _, o := range others {
			if o.blocks(viewer.u) {
				online = false
			}
		}
	}
	var t tlvs
	class := uint16(classFree)
	icq := viewer != nil && viewer.icq
	if icq {
		class |= classICQ
	}
	if !online {
		t.addU16(0x0001, class)
		return infoBlock(name, t), false
	}
	signon := time.Now()
	for _, n := range nodes {
		if n.Since.Before(signon) {
			signon = n.Since
		}
	}
	if self {
		signon = viewer.signon
	}
	away := false
	var status uint32
	var idle time.Duration
	idleAll := len(others) > 0
	var caps [][]byte
	seen := map[string]bool{}
	for _, o := range others {
		o.mu.Lock()
		if o.away != "" {
			away = true
		}
		if o.status&(icqAway|icqNA|icqDND|icqOccupied) != 0 {
			away = true
			status = o.status &^ icqInvisible
		}
		if o.idle.IsZero() {
			idleAll = false
		} else if d := time.Since(o.idle); idle == 0 || d < idle {
			idle = d
		}
		for _, c := range o.caps {
			if !seen[string(c)] {
				seen[string(c)] = true
				caps = append(caps, c)
			}
		}
		o.mu.Unlock()
	}
	if len(others) == 0 {
		// on by telnet, IRC, the web: they may be asked into rooms
		caps = [][]byte{capChat}
		for _, n := range nodes {
			if strings.HasSuffix(n.Activity, "(away)") {
				away = true
			}
		}
		if away {
			status = icqAway
		}
	}
	if away {
		class |= classAway
	}
	t.addU16(0x0001, class)
	t.addU32(0x0003, uint32(signon.Unix()))
	if idleAll && idle >= time.Minute {
		t.addU16(0x0004, uint16(min(idle/time.Minute, 0xffff)))
	}
	t.addU32(0x0005, uint32(u.Created.Unix()))
	if icq || self && viewer.icq {
		t.addU32(0x0006, status)
	}
	if self {
		if a := net.ParseIP(viewer.host).To4(); a != nil {
			t.add(0x000a, a)
		}
	}
	if len(caps) > 0 {
		var c []byte
		for _, x := range caps {
			c = append(c, x...)
		}
		t.add(0x000d, c)
	}
	t.addU32(0x000f, uint32(time.Since(signon).Seconds()))
	return infoBlock(name, t), true
}

// infoBlock is a user info block: a screen name, a warning level, TLVs.
func infoBlock(name string, t tlvs) []byte {
	var b buf
	b.str8(name)
	b.u16(0)
	b.raw(t.block())
	return b
}

// plainInfo is the user info of one with no account (a chat network's).
func plainInfo(name string) []byte {
	var t tlvs
	t.addU16(0x0001, classFree)
	return infoBlock(name, t)
}

// blocks reports that the session's privacy keeps who from it: from
// seeing it online, and from messaging it.
func (ss *session) blocks(who *store.User) bool {
	if who == nil || who.ID == ss.u.ID {
		return false
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	in := func(m map[string]bool) bool {
		return m[ident(who.Name)] || m[ss.s.uinOf(who.ID)]
	}
	if ss.status&icqInvisible != 0 && !in(ss.permit) {
		return true
	}
	switch ss.pdMode {
	case pdDenyAll:
		return true
	case pdPermitSome:
		return !in(ss.permit)
	case pdDenySome:
		return in(ss.deny)
	case pdBuddies:
		_, ok := ss.watch[who.ID]
		return !ok
	}
	return false
}

// blocksName is blocks for one known by name only.
func (ss *session) blocksName(name string) bool {
	if u, err := ss.s.B.Store.UserByName(name); err == nil {
		return ss.blocks(u)
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.pdMode == pdDenyAll || ss.pdMode == pdPermitSome && !ss.permit[ident(name)] ||
		ss.pdMode == pdDenySome && ss.deny[ident(name)]
}

// buddyChanged tells the client of a buddy's state, if it watches them.
func (ss *session) buddyChanged(u *store.User) {
	ss.mu.Lock()
	_, watched := ss.watch[u.ID]
	ready := ss.isReady
	ss.mu.Unlock()
	if !watched || !ready {
		return
	}
	info, online := ss.s.userInfo(ss, u, false)
	if online {
		ss.snac(0x0003, 0x000b, 0, info)
	} else {
		ss.snac(0x0003, 0x000c, 0, info)
	}
}

// allArrived tells the client of each buddy online.
func (ss *session) allArrived() {
	ss.mu.Lock()
	ids := make([]int64, 0, len(ss.watch))
	for id := range ss.watch {
		ids = append(ids, id)
	}
	ss.mu.Unlock()
	for _, id := range ids {
		u, err := ss.s.B.Store.UserByID(id)
		if err != nil {
			continue
		}
		if info, online := ss.s.userInfo(ss, u, false); online {
			ss.snac(0x0003, 0x000b, 0, info)
		}
	}
}

// rewatch makes the buddies watched those of both lists, telling the
// client of each new one online.
func (ss *session) rewatch() {
	ss.mu.Lock()
	names := map[string]string{}
	for k, v := range ss.listed {
		names[k] = v
	}
	for k, v := range ss.bagged {
		names[k] = v
	}
	ss.mu.Unlock()
	watch := map[int64]string{}
	for _, sn := range names {
		if u := ss.s.resolve(sn); u != nil {
			watch[u.ID] = sn
		}
	}
	ss.mu.Lock()
	var added []int64
	for id := range watch {
		if _, ok := ss.watch[id]; !ok {
			added = append(added, id)
		}
	}
	ss.watch = watch
	ss.mu.Unlock()
	for _, id := range added {
		if u, err := ss.s.B.Store.UserByID(id); err == nil {
			ss.buddyChanged(u)
		}
	}
}

// buddy is family 3: the client-side buddy list.
func (ss *session) buddy(h snacHeader, r *rd) {
	switch h.sub {
	case 0x0002:
		var t tlvs
		t.addU16(0x0001, 500)
		t.addU16(0x0002, 1000)
		t.addU16(0x0004, 160)
		ss.snac(0x0003, 0x0003, h.req, t.bytes())
	case 0x0004, 0x0005:
		ss.mu.Lock()
		for r.left() > 0 {
			sn := r.str8()
			if r.bad || sn == "" {
				break
			}
			if h.sub == 0x0004 {
				ss.listed[ident(sn)] = sn
			} else {
				delete(ss.listed, ident(sn))
			}
		}
		ss.mu.Unlock()
		ss.do(ss.rewatch)
	default:
		ss.fail(0x0003, h.req, 0x0008)
	}
}

// permitDeny is family 9: the client-side permit and deny lists.
func (ss *session) permitDeny(h snacHeader, r *rd) {
	switch h.sub {
	case 0x0002:
		var t tlvs
		t.addU16(0x0001, 200)
		t.addU16(0x0002, 200)
		t.addU16(0x0003, 200)
		ss.snac(0x0009, 0x0003, h.req, t.bytes())
		return
	case 0x0004: // the permitted classes: all of ours are one
		return
	case 0x0005, 0x0006, 0x0007, 0x0008:
	default:
		ss.fail(0x0009, h.req, 0x0008)
		return
	}
	ss.mu.Lock()
	list := ss.permit
	mode := byte(pdPermitSome)
	if h.sub >= 0x0007 {
		list, mode = ss.deny, pdDenySome
	}
	if h.sub == 0x0005 || h.sub == 0x0007 {
		ss.pdMode = mode
	}
	for r.left() > 0 {
		sn := r.str8()
		if r.bad || sn == "" {
			break
		}
		if h.sub == 0x0005 || h.sub == 0x0007 {
			list[ident(sn)] = true
		} else {
			delete(list, ident(sn))
		}
	}
	ss.mu.Unlock()
	ss.s.changed(ss.u)
}

// locate is family 2: profiles, away messages, capabilities.
func (ss *session) locate(h snacHeader, r *rd) {
	switch h.sub {
	case 0x0002:
		var t tlvs
		t.addU16(0x0001, 4096)
		t.addU16(0x0002, 128)
		t.addU16(0x0003, 10)
		t.addU16(0x0004, 4096)
		t.addU16(0x0005, 128)
		ss.snac(0x0002, 0x0003, h.req, t.bytes())
	case 0x0004:
		t := parseTLVs(r.rest())
		ss.mu.Lock()
		if v, ok := t.get(0x0002); ok {
			ss.profile, ss.profMIME = string(v), t.str(0x0001)
		}
		if v, ok := t.get(0x0004); ok {
			ss.away, ss.awayMIME = string(v), t.str(0x0003)
		}
		if v, ok := t.get(0x0005); ok {
			ss.caps = nil
			for len(v) >= 16 {
				ss.caps = append(ss.caps, append([]byte(nil), v[:16]...))
				v = v[16:]
			}
		}
		ss.mu.Unlock()
		ss.n.Doing(ss.doing())
		if ss.ready() {
			ss.s.changed(ss.u)
		}
	case 0x0005: // user info (old): a type, a name
		kind := r.u16()
		sn := r.str8()
		var flags uint32
		switch kind {
		case 0x0001:
			flags = 0x0001
		case 0x0003:
			flags = 0x0002
		case 0x0004:
			flags = 0x0004
		}
		ss.userInfoReply(h, sn, flags)
	case 0x0015: // user info: flags, a name
		flags := r.u32()
		ss.userInfoReply(h, r.str8(), flags)
	case 0x0009, 0x000f: // directory info, interests: kept nowhere, but taken
		var b buf
		b.u16(1)
		ss.snac(0x0002, h.sub+1, h.req, b)
	case 0x000b: // directory info: none
		var b buf
		b.u16(1)
		b.u16(0)
		ss.snac(0x0002, 0x000c, h.req, b)
	default:
		ss.fail(0x0002, h.req, 0x0008)
	}
}

// userInfoReply is SNAC 2/6: a user's info, with their profile (flags
// 1), away message (2) or capabilities (4).
func (ss *session) userInfoReply(h snacHeader, sn string, flags uint32) {
	u := ss.s.resolve(sn)
	if u == nil {
		ss.fail(0x0002, h.req, 0x0004)
		return
	}
	info, online := ss.s.userInfo(ss, u, u.ID == ss.u.ID)
	if !online {
		ss.fail(0x0002, h.req, 0x0004)
		return
	}
	var t tlvs
	const mime = `text/x-aolrtf; charset="us-ascii"`
	others := ss.s.sessionsOf(u.ID)
	if flags&0x0001 != 0 {
		profile, pm := "", mime
		for _, o := range others {
			o.mu.Lock()
			if o.profile != "" {
				profile, pm = o.profile, o.profMIME
			}
			o.mu.Unlock()
		}
		if profile == "" {
			profile = ss.s.bbsProfile(u)
		}
		t.addStr(0x0001, pm)
		t.addStr(0x0002, profile)
	}
	if flags&0x0002 != 0 {
		for _, o := range others {
			o.mu.Lock()
			if o.away != "" {
				t.addStr(0x0003, o.awayMIME)
				t.addStr(0x0004, o.away)
				o.mu.Unlock()
				break
			}
			o.mu.Unlock()
		}
	}
	if flags&0x0004 != 0 {
		var c []byte
		for _, o := range others {
			o.mu.Lock()
			for _, x := range o.caps {
				c = append(c, x...)
			}
			o.mu.Unlock()
		}
		if len(others) == 0 {
			c = capChat
		}
		t.add(0x0005, c)
	}
	ss.snac(0x0002, 0x0006, h.req, append(info, t.bytes()...))
}

// bbsProfile is the profile of a user known by the BBS alone: their line
// about themselves, where they are, and what they are doing.
func (s *Server) bbsProfile(u *store.User) string {
	var b strings.Builder
	b.WriteString("<HTML><BODY BGCOLOR=\"#ffffff\">")
	if u.About != "" {
		b.WriteString(html.EscapeString(u.About) + "<BR><BR>")
	}
	if u.Location != "" {
		b.WriteString("<B>Location:</B> " + html.EscapeString(u.Location) + "<BR>")
	}
	for _, n := range s.nodesOf(u.Name) {
		fmt.Fprintf(&b, "<B>On %s</B> (node %d): %s<BR>", html.EscapeString(n.Via), n.Num, html.EscapeString(n.Activity))
	}
	fmt.Fprintf(&b, "<BR>A user of %s since %s.", html.EscapeString(s.B.Conf().Name), u.Created.Format("January 2006"))
	b.WriteString("</BODY></HTML>")
	return b.String()
}
