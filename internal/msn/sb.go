package msn

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// A switchboard is a conversation: the MSN clients joined to it, each by
// a connection of its own, and the BBS users in it by other routes (IRC,
// AIM, a terminal), who are "virtual": what is said goes to them as
// private messages, and theirs comes back into it.

// switchboard is one conversation.
type switchboard struct {
	id string
	s  *Server

	mu      sync.Mutex
	joined  []*sbConn
	virtual []*virtual
	invited map[int64]bool     // users rung and not yet answered
	queue   map[int64][][]byte // MSG lines waiting for a user to answer
	closed  bool
}

// virtual is one in a conversation by another route (u nil: the BBS's
// notices).
type virtual struct {
	u        *store.User
	handle   string
	friendly string
}

// sbConn is an MSN client's connection to a switchboard.
type sbConn struct {
	k      *conn
	sb     *switchboard
	ns     *nsSession
	handle string
	once   sync.Once
}

// sbCookie lets a client join a switchboard: by USR (the one that asked
// for it) or ANS (one rung).
type sbCookie struct {
	sb      *switchboard
	ns      *nsSession
	answer  bool
	expires time.Time
}

// newSwitchboard makes a conversation.
func (s *Server) newSwitchboard() *switchboard {
	s.mu.Lock()
	defer s.mu.Unlock()
	sb := &switchboard{id: strconv.Itoa(s.nextSB), s: s, invited: map[int64]bool{}, queue: map[int64][][]byte{}}
	s.nextSB++
	s.sbs[sb.id] = sb
	return sb
}

// newCookie is a cookie to join sb, for ns's client.
func (s *Server) newCookie(sb *switchboard, ns *nsSession, kind string) string {
	ck := fmt.Sprintf("%d.%d", time.Now().UnixNano()%1000000000, randomNum())
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, v := range s.cookies {
		if time.Now().After(v.expires) {
			delete(s.cookies, key)
		}
	}
	s.cookies[ck] = &sbCookie{sb: sb, ns: ns, answer: kind == "ans", expires: time.Now().Add(cookieLife)}
	return ck
}

// randomNum is a random decimal number of 10 digits.
func randomNum() uint64 {
	h := randomHex(8)
	n, _ := strconv.ParseUint(h, 16, 64)
	return n%9000000000 + 1000000000
}

// takeCookie spends a cookie (nil: none such, or expired).
func (s *Server) takeCookie(ck string) *sbCookie {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.cookies[ck]
	delete(s.cookies, ck)
	if k == nil || time.Now().After(k.expires) {
		return nil
	}
	return k
}

// switchboardConn serves a client's switchboard connection, begun with
// first (USR or ANS).
func (s *Server) switchboardConn(k *conn, host string, first *cmd) {
	var ck *sbCookie
	var handle string
	switch first.name {
	case "USR":
		handle = first.arg(0)
		ck = s.takeCookie(first.arg(1))
		if ck != nil && ck.answer {
			ck = nil
		}
	case "ANS":
		handle = first.arg(0)
		ck = s.takeCookie(first.arg(1))
		if ck != nil && (!ck.answer || ck.sb.id != first.arg(2)) {
			ck = nil
		}
	}
	if ck == nil || ck.ns.closed() {
		k.sendErr(errAuth, first.trid)
		return
	}
	if u := s.resolve(handle); u == nil || u.ID != ck.ns.u.ID {
		k.sendErr(errAuth, first.trid)
		return
	}
	sc := &sbConn{k: k, sb: ck.sb, ns: ck.ns, handle: ck.ns.handle}
	if !ck.sb.join(sc, first) {
		k.sendErr(errNoSB, first.trid)
		return
	}
	defer sc.leave(false)
	for {
		k.c.SetDeadline(time.Now().Add(sbIdle))
		c, err := k.read()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				sc.leave(true)
			} else if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.B.Log.Printf("node %d: msn switchboard: %v", sc.ns.n.Num, err)
			}
			return
		}
		switch c.name {
		case "":
		case "OUT":
			return
		case "CAL":
			sc.call(c)
		case "MSG":
			sc.msg(c)
		default:
			k.sendErr(errSyntax, c.trid)
		}
		if sc.ns.closed() {
			return
		}
	}
}

// join puts a client in the conversation: USR, the one that asked for
// it; ANS, one rung, who is told who is there (IRO) and the others that
// it came (JOI).
func (sb *switchboard) join(sc *sbConn, first *cmd) bool {
	sb.mu.Lock()
	if sb.closed {
		sb.mu.Unlock()
		return false
	}
	friendly := encode(sc.ns.friendlyName())
	uid := sc.ns.u.ID
	if first.name == "USR" {
		sb.joined = append(sb.joined, sc)
		sb.mu.Unlock()
		sc.k.send("USR", first.trid, "OK", sc.handle, friendly)
		return true
	}
	var others [][2]string
	for _, o := range sb.joined {
		others = append(others, [2]string{o.handle, encode(o.ns.friendlyName())})
	}
	for _, v := range sb.virtual {
		others = append(others, [2]string{v.handle, encode(v.friendly)})
	}
	tell := append([]*sbConn(nil), sb.joined...)
	sb.joined = append(sb.joined, sc)
	delete(sb.invited, uid)
	queued := sb.queue[uid]
	delete(sb.queue, uid)
	sb.mu.Unlock()
	for i, o := range others {
		sc.k.send("IRO", first.trid, strconv.Itoa(i+1), strconv.Itoa(len(others)), o[0], o[1])
	}
	sc.k.send("ANS", first.trid, "OK")
	for _, o := range tell {
		o.k.send("JOI", sc.handle, friendly)
	}
	for _, m := range queued {
		sc.k.write(m)
	}
	return true
}

// leave takes a client out (timeout: for idling); the others are told,
// and a conversation with no client left ends.
func (sc *sbConn) leave(timeout bool) {
	sc.once.Do(func() {
		sb := sc.sb
		sb.mu.Lock()
		for i, o := range sb.joined {
			if o == sc {
				sb.joined = append(sb.joined[:i], sb.joined[i+1:]...)
				break
			}
		}
		rest := append([]*sbConn(nil), sb.joined...)
		empty := len(sb.joined) == 0 && len(sb.invited) == 0
		if empty {
			sb.closed = true
		}
		sb.mu.Unlock()
		for _, o := range rest {
			if timeout {
				o.k.send("BYE", sc.handle, "1")
			} else {
				o.k.send("BYE", sc.handle)
			}
		}
		if empty {
			sb.s.dropSwitchboard(sb)
		}
		sc.k.close()
	})
}

// dropSwitchboard forgets a conversation.
func (s *Server) dropSwitchboard(sb *switchboard) {
	s.mu.Lock()
	delete(s.sbs, sb.id)
	for key, v := range s.cookies {
		if v.sb == sb {
			delete(s.cookies, key)
		}
	}
	s.mu.Unlock()
}

// leaveSwitchboards takes a client that signed out out of its
// conversations.
func (s *Server) leaveSwitchboards(ns *nsSession) {
	s.mu.Lock()
	sbs := make([]*switchboard, 0, len(s.sbs))
	for _, sb := range s.sbs {
		sbs = append(sbs, sb)
	}
	s.mu.Unlock()
	for _, sb := range sbs {
		sb.mu.Lock()
		var mine []*sbConn
		for _, o := range sb.joined {
			if o.ns == ns {
				mine = append(mine, o)
			}
		}
		delete(sb.invited, ns.u.ID)
		delete(sb.queue, ns.u.ID)
		sb.mu.Unlock()
		for _, o := range mine {
			o.leave(false)
		}
	}
}

// has reports whether the user is in the conversation, joined, rung or
// by another route.
func (sb *switchboard) has(id int64) bool {
	for _, o := range sb.joined {
		if o.ns.u.ID == id {
			return true
		}
	}
	for _, v := range sb.virtual {
		if v.u != nil && v.u.ID == id {
			return true
		}
	}
	return sb.invited[id]
}

// call is CAL: a user asked into the conversation.  One on MSN is rung
// (RNG) at each of their sessions; one on the BBS another way is in it at
// once, by that route.
func (sc *sbConn) call(c *cmd) {
	s := sc.sb.s
	t := s.resolve(c.arg(0))
	switch {
	case t == nil:
		sc.k.sendErr(errBadHandle, c.trid)
		return
	case t.ID == sc.ns.u.ID:
		sc.k.sendErr(errBadParam, c.trid)
		return
	}
	sb := sc.sb
	sb.mu.Lock()
	in := sb.has(t.ID)
	sb.mu.Unlock()
	if in {
		sc.k.sendErr(errAlreadyThere, c.trid)
		return
	}
	if s.statusFor(t, sc.ns.u) == "" {
		sc.k.sendErr(errNotOnline, c.trid)
		return
	}
	var ring []*nsSession
	for _, o := range s.sessionsOf(t.ID) {
		if st := o.statusNow(); st != "" && st != "HDN" {
			ring = append(ring, o)
		}
	}
	sc.k.send("CAL", c.trid, "RINGING", sb.id)
	if len(ring) > 0 {
		sb.mu.Lock()
		sb.invited[t.ID] = true
		sb.mu.Unlock()
		for _, o := range ring {
			sb.ring(o, sc.handle, sc.ns.friendlyName())
		}
		return
	}
	v := &virtual{u: t, handle: s.handleOf(t), friendly: t.Name}
	sb.mu.Lock()
	sb.virtual = append(sb.virtual, v)
	sb.mu.Unlock()
	sc.k.send("JOI", v.handle, encode(v.friendly))
}

// ring has a client asked to answer the conversation (RNG), as from one
// in it.
func (sb *switchboard) ring(o *nsSession, fromHandle, fromFriendly string) {
	sb.expireInvite(o.u.ID)
	ck := sb.s.newCookie(sb, o, "ans")
	host := sb.s.reachHost(o.k.c)
	o.do(func() { o.k.send("RNG", sb.id, host, "CKI", ck, fromHandle, encode(fromFriendly)) })
}

// msg is MSG: a message to the others.  Text goes to each client in the
// conversation and, as a private message, to those in it by other routes;
// anything else (typing, invitations) only to the clients.
func (sc *sbConn) msg(c *cmd) {
	ack := strings.ToUpper(c.arg(0))
	m := parseMIME(c.payload)
	text := ""
	if strings.HasPrefix(m.contentType(), "text/plain") {
		text = strings.Join(strings.Fields(strings.ReplaceAll(m.body, "\r\n", "\n")), " ")
	}
	sb := sc.sb
	sb.mu.Lock()
	var to []*sbConn
	for _, o := range sb.joined {
		if o != sc {
			to = append(to, o)
		}
	}
	vs := append([]*virtual(nil), sb.virtual...)
	sb.mu.Unlock()
	fail := func() {
		if ack == "A" || ack == "N" || ack == "D" {
			sc.k.send("NAK", c.trid)
		}
	}
	if len(to) == 0 && (text == "" || len(vs) == 0) {
		fail()
		return
	}
	n := sc.ns.n
	if text != "" {
		if err := n.Throttle(); err != nil {
			fail()
			return
		}
	}
	line := append([]byte(fmt.Sprintf("MSG %s %s %d\r\n", sc.handle, encode(sc.ns.friendlyName()), len(c.payload))),
		c.payload...)
	kept := map[int64]bool{}
	for _, o := range to {
		if text != "" && !kept[o.ns.u.ID] {
			kept[o.ns.u.ID] = true
			n.Keep(o.ns.u, text, true) // kept before it is seen
		}
		o.k.write(line)
	}
	ok := len(to) > 0
	for _, v := range vs {
		if v.u == nil || text == "" {
			continue
		}
		online, err := n.TellUser(v.u, text)
		switch {
		case errors.Is(err, bbs.ErrBlocked):
			continue
		case err != nil:
			sc.ns.s.B.Log.Printf("node %d: msn: to %s: %v", n.Num, v.u.Name, err)
			continue
		}
		ok = true
		if !online {
			sc.ns.noteAway(sc, v)
		}
	}
	if !ok {
		fail()
		return
	}
	if ack == "A" || ack == "D" {
		sc.k.send("ACK", c.trid)
	}
}

// noteAway tells a client (once) that one it talks with went, and will
// have its messages when they are next on.
func (ns *nsSession) noteAway(sc *sbConn, v *virtual) {
	ns.mu.Lock()
	noted := ns.noted[v.u.ID]
	ns.noted[v.u.ID] = true
	ns.mu.Unlock()
	if noted {
		return
	}
	text := v.u.Name + " is not on now; they will have your messages when they next sign on."
	p := textPayload(text)
	sc.k.write(append([]byte(fmt.Sprintf("MSG %s %s %d\r\n", ns.s.systemHandle(), encode(ns.s.S.System), len(p))), p...))
}

// fromOther gives a client a line from one in the conversation by another
// route (or the BBS's notices): now if it has joined, else when it
// answers.
func (sb *switchboard) fromOther(ns *nsSession, v *virtual, text string) bool {
	p := textPayload(text)
	line := append([]byte(fmt.Sprintf("MSG %s %s %d\r\n", v.handle, encode(v.friendly), len(p))), p...)
	sb.mu.Lock()
	if sb.closed {
		sb.mu.Unlock()
		return false
	}
	var to []*sbConn
	for _, o := range sb.joined {
		if o.ns == ns {
			to = append(to, o)
		}
	}
	if len(to) == 0 {
		if !sb.invited[ns.u.ID] {
			sb.mu.Unlock()
			return false
		}
		sb.queue[ns.u.ID] = append(sb.queue[ns.u.ID], line)
		sb.mu.Unlock()
		return true
	}
	sb.mu.Unlock()
	for _, o := range to {
		o.k.write(line)
	}
	return true
}

// virtualOf is the one by another route (handle) in the conversation
// (nil: none).
func (sb *switchboard) virtualOf(handle string) *virtual {
	for _, v := range sb.virtual {
		if strings.EqualFold(v.handle, handle) {
			return v
		}
	}
	return nil
}

// deliverFrom gives a client a line from one not on MSN (u nil: the BBS's
// notices): into a conversation it has with them, or a new one it is rung
// to.
func (s *Server) deliverFrom(ns *nsSession, u *store.User, text string) {
	v := &virtual{handle: s.systemHandle(), friendly: s.S.System}
	if u != nil {
		v = &virtual{u: u, handle: s.handleOf(u), friendly: u.Name}
	}
	s.mu.Lock()
	var best *switchboard
	for _, sb := range s.sbs {
		sb.mu.Lock()
		mine := sb.invited[ns.u.ID]
		for _, o := range sb.joined {
			if o.ns == ns {
				mine = true
			}
		}
		if mine && sb.virtualOf(v.handle) != nil && !sb.closed {
			if best == nil || sbNewer(sb, best) {
				best = sb
			}
		}
		sb.mu.Unlock()
	}
	s.mu.Unlock()
	if best != nil {
		best.mu.Lock()
		if x := best.virtualOf(v.handle); x != nil {
			v = x
		}
		best.mu.Unlock()
		if best.fromOther(ns, v, text) {
			return
		}
	}
	sb := s.newSwitchboard()
	sb.mu.Lock()
	sb.virtual = append(sb.virtual, v)
	sb.invited[ns.u.ID] = true
	sb.mu.Unlock()
	sb.fromOther(ns, v, text)
	sb.expireInvite(ns.u.ID)
	ck := s.newCookie(sb, ns, "ans")
	ns.k.send("RNG", sb.id, s.reachHost(ns.k.c), "CKI", ck, v.handle, encode(v.friendly))
}

// sbNewer: a was made after b.
func sbNewer(a, b *switchboard) bool {
	x, _ := strconv.Atoi(a.id)
	y, _ := strconv.Atoi(b.id)
	return x > y
}

// expireInvite forgets a user's invitation if they have not answered once
// their cookie is spent; a conversation left with no one in it ends.
func (sb *switchboard) expireInvite(id int64) {
	time.AfterFunc(cookieLife+time.Second, func() {
		sb.mu.Lock()
		if !sb.invited[id] || sb.closed {
			sb.mu.Unlock()
			return
		}
		delete(sb.invited, id)
		delete(sb.queue, id)
		empty := len(sb.joined) == 0 && len(sb.invited) == 0
		if empty {
			sb.closed = true
		}
		sb.mu.Unlock()
		if empty {
			sb.s.dropSwitchboard(sb)
		}
	})
}
