package irclink

import (
	"bufio"
	"context"
	"errors"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/ergochat/irc-go/ircreader"
)

// limits of a link
const (
	maxLine       = 512
	sendQ         = 8192 // lines waiting to be written; more drops the link
	registerLimit = time.Minute
	idleLimit     = 3 * time.Minute // without a line: a PING
	pingLimit     = 2 * time.Minute // for its answer
	writeLimit    = 60 * time.Second
)

// capab is what the BBS tells its peers it speaks.
const capab = "QS EX CHW IE KNOCK TB ENCAP SAVE EUID EOPMOD"

var roomRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,19}$`)

// link is a connection to a server.
type link struct {
	h       *Hub
	conn    net.Conn
	peer    *Peer // as configured (nil until its SERVER, for one linking in)
	out     chan string
	done    chan struct{}
	written chan struct{} // the writer is done
	once    sync.Once

	// registering
	pass, sid string
	caps      map[string]bool
	srv       *server // its server, once linked

	hybrid  bool // it speaks ircd-hybrid 8's TS6 (SERVER with a SID, its UID, TBURST, EOB)
	pinged  bool // our PING after the handshake is sent
	bursted bool // our burst is sent: lines may go to it
}

// line makes a line: source (a SID or UID; "": none), command and
// parameters, the last as trailing.
func line(source, command string, params ...string) string {
	m := ircmsg.MakeMessage(nil, source, command, params...)
	switch command {
	case "JOIN", "PART", "NICK", "MODE", "TMODE":
	default:
		if len(params) > 0 {
			m.ForceTrailing() // text, a list or a description
		}
	}
	b, err := m.LineBytesStrict(false, maxLine)
	if err != nil && !errors.Is(err, ircmsg.ErrorBodyTooLong) {
		return ""
	}
	return strings.TrimRight(string(b), "\r\n")
}

// trace is told every line, when set (tests).
var trace func(string)

// sendRaw queues a line to the link; a full queue drops it.
func (l *link) sendRaw(s string) {
	if s == "" {
		return
	}
	if trace != nil && s != flushClose {
		trace("-> " + l.name() + " " + s)
	}
	select {
	case <-l.done:
	case l.out <- s:
	default:
		l.h.B.Log.Printf("irclink: %s: send queue full", l.name())
		l.close()
	}
}

// send queues a line made of its parts.
func (l *link) send(source, command string, params ...string) {
	l.sendRaw(line(source, command, params...))
}

// close ends the link (once).
func (l *link) close() {
	l.once.Do(func() {
		close(l.done)
		l.conn.Close()
	})
}

// name is the link's server name, or its address before it says.
func (l *link) name() string {
	if l.srv != nil {
		return l.srv.name
	}
	if l.peer != nil {
		return l.peer.Name
	}
	return l.conn.RemoteAddr().String()
}

// flushClose is the line that has the writer write what is queued
// before it, then end the link.
const flushClose = "\x00close"

// finish ends the link once what is queued (an ERROR) is written, or in
// a few seconds.
func (l *link) finish() {
	l.sendRaw(flushClose)
	select {
	case <-l.written:
	case <-time.After(3 * time.Second):
	}
	l.close()
}

// writer writes the queued lines until the link ends.
func (l *link) writer() {
	defer close(l.written)
	w := bufio.NewWriter(l.conn)
	for {
		select {
		case <-l.done:
			return
		case s := <-l.out:
			last := s == flushClose
			if !last {
				w.WriteString(s + "\r\n")
			}
			for more := !last; more; {
				select {
				case s := <-l.out:
					if s == flushClose {
						last, more = true, false
						break
					}
					w.WriteString(s + "\r\n")
				default:
					more = false
				}
			}
			l.conn.SetWriteDeadline(time.Now().Add(writeLimit))
			if err := w.Flush(); err != nil || last {
				l.close()
				return
			}
		}
	}
}

// serve runs a link (peer: the one linked out to; nil for one linking in)
// until it drops; then the servers behind it are gone from the network.
func (h *Hub) serve(ctx context.Context, conn net.Conn, peer *Peer) {
	l := &link{h: h, conn: conn, peer: peer, out: make(chan string, sendQ), done: make(chan struct{}),
		written: make(chan struct{}), caps: map[string]bool{}}
	h.mu.Lock()
	h.links[l] = true
	h.mu.Unlock()
	go l.writer()
	stop := context.AfterFunc(ctx, l.close)
	defer stop()
	if peer != nil {
		l.handshake()
	}
	why := l.read()
	l.finish()
	h.mu.Lock()
	delete(h.links, l)
	if l.srv != nil && h.servers[l.srv.sid] == l.srv {
		h.B.Log.Printf("irclink: %s split: %s", l.srv.name, why)
		h.squit(l.srv, l, h.me.name+" "+l.srv.name)
	}
	h.mu.Unlock()
}

// handshake sends the BBS's PASS, CAPAB, SERVER and SVINFO, and then a
// PING: its answer comes after the peer's burst.
func (l *link) handshake() {
	h := l.h
	if l.peer.Hybrid {
		l.hybrid = true
	}
	if l.hybrid {
		l.send("", "PASS", l.peer.SendPassword)
		l.send("", "CAPAB", "KNOCK TBURST ENCAP EOB")
		l.send("", "SERVER", h.me.name, "1", h.me.sid, "+", h.me.desc)
	} else {
		l.send("", "PASS", l.peer.SendPassword, "TS", "6", h.me.sid)
		l.send("", "CAPAB", capab)
		l.send("", "SERVER", h.me.name, "1", h.me.desc)
	}
	l.send("", "SVINFO", "6", "6", "0", strconv.FormatInt(time.Now().Unix(), 10))
	l.send("", "PING", h.me.name)
	l.pinged = true
}

// read reads the link's lines until it ends; it is why.
func (l *link) read() string {
	var r ircreader.Reader
	r.Initialize(l.conn, 1024, 8192)
	registerBy := time.Now().Add(registerLimit)
	pinged := false
	for {
		if l.srv == nil {
			l.conn.SetReadDeadline(registerBy)
		} else if pinged {
			l.conn.SetReadDeadline(time.Now().Add(pingLimit))
		} else {
			l.conn.SetReadDeadline(time.Now().Add(idleLimit))
		}
		raw, err := r.ReadLine()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() && l.srv != nil && !pinged {
				pinged = true
				l.send(l.h.me.sid, "PING", l.h.me.name, l.srv.sid)
				continue
			}
			if ne != nil && ne.Timeout() {
				l.sendRaw("ERROR :Ping timeout")
				return "ping timeout"
			}
			select {
			case <-l.done:
				return "link closed"
			default:
			}
			return err.Error()
		}
		pinged = false
		m, err := ircmsg.ParseLine(string(raw))
		if err != nil {
			continue
		}
		m.Command = strings.ToUpper(m.Command)
		if trace != nil {
			trace("<- " + l.name() + " " + string(raw))
		}
		h := l.h
		h.mu.Lock()
		why, ok := h.handle(l, &m, string(raw))
		h.mu.Unlock()
		if !ok {
			l.sendRaw("ERROR :Closing link: " + why)
			return why
		}
	}
}

// handle is a line from a link; false drops the link, for why. The hub's
// lock is held.
func (h *Hub) handle(l *link, m *ircmsg.Message, raw string) (why string, ok bool) {
	if l.srv == nil {
		return h.register(l, m)
	}
	p := func(i int) string {
		if i < len(m.Params) {
			return m.Params[i]
		}
		return ""
	}
	src := m.Source
	if src == "" {
		src = l.srv.sid
	}
	if i := strings.IndexByte(src, '!'); i >= 0 {
		src = src[:i]
	}
	// a source behind another link: a loop, or a server gone
	if s := h.sourceServer(src); s != nil && s != l.srv && s.link != l {
		return "", true
	}
	switch m.Command {
	case "PING":
		if dest := p(1); dest != "" && !h.isMe(dest) {
			h.route(dest, l, raw)
			return "", true
		}
		l.send(h.me.sid, "PONG", h.me.name, p(0))
	case "PONG":
		if dest := p(1); dest != "" && !h.isMe(dest) {
			h.route(dest, l, raw)
			return "", true
		}
		if l.pinged && !l.bursted {
			h.burst(l)
		}
	case "ERROR":
		return "ERROR from " + l.srv.name + ": " + p(0), false
	case "SID":
		return h.sid(l, src, p(0), p(1), p(2), p(3), raw)
	case "SERVER":
		return "a server without a SID: not TS6", false
	case "SQUIT":
		target := h.servers[p(0)]
		if target == nil {
			target = h.names[fold(p(0))]
		}
		if target == nil {
			return "", true
		}
		if target == h.me || target == l.srv {
			return "SQUIT: " + p(1), false
		}
		if target.link == l {
			h.squit(target, l, p(1))
		}
	case "UID":
		switch {
		case len(m.Params) >= 11: // hybrid 8: nick hops ts umodes user host realhost ip uid account :gecos
			account := p(9)
			if account == "*" || account == "0" {
				account = ""
			}
			h.newUser(l, src, p(0), p(2), p(3), p(4), p(5), p(7), p(8), account, p(10), raw)
		case len(m.Params) >= 9:
			h.newUser(l, src, p(0), p(2), p(3), p(4), p(5), p(6), p(7), "", p(8), raw)
		}
	case "EUID":
		if len(m.Params) < 11 {
			return "", true
		}
		account := p(9)
		if account == "*" || account == "0" {
			account = ""
		}
		h.newUser(l, src, p(0), p(2), p(3), p(4), p(5), p(6), p(7), account, p(10), raw)
	case "NICK":
		h.nickChange(l, src, p(0), p(1), raw)
	case "QUIT":
		if u := h.users[src]; u != nil {
			h.removeUser(u, p(0))
			h.forward(l, raw)
		}
	case "KILL":
		h.kill(l, src, p(0), p(1), raw)
	case "SAVE":
		if u := h.users[p(0)]; u != nil {
			h.renick(u, u.uid, time.Now().Unix())
			h.forward(l, raw)
		}
	case "SJOIN":
		h.sjoin(l, p(0), p(1), p(2), m.Params[min(len(m.Params), 3):], raw)
	case "JOIN":
		h.userJoin(l, src, p(0), p(1), raw)
	case "PART":
		if u := h.users[src]; u != nil {
			for _, name := range strings.Split(p(0), ",") {
				if ch := h.chans[fold(name)]; ch != nil {
					h.part(u, ch, h.display(u), p(1))
				}
			}
			h.forward(l, raw)
		}
	case "KICK":
		if ch, u := h.chans[fold(p(0))], h.users[p(1)]; ch != nil && u != nil {
			h.part(u, ch, h.display(u), "kicked: "+p(2))
			if u.local != "" {
				h.B.RelayPrivate(u.local, eventNotice("You were kicked from "+ch.name+" on the IRC network: "+p(2)))
			}
		}
		h.forward(l, raw)
	case "EOB":
		h.forward(l, raw)
	case "TBURST":
		// TBURST channel-ts channel topic-ts setter :topic (hybrid)
		if len(m.Params) >= 5 {
			h.topic(l, "TB", []string{p(1), p(2), p(3), p(4)}, raw)
		}
	case "TOPIC", "TB", "ETB":
		h.topic(l, m.Command, m.Params, raw)
	case "AWAY":
		if u := h.users[src]; u != nil {
			u.away = p(0)
		}
		h.forward(l, raw)
	case "CHGHOST":
		if u := h.users[p(0)]; u != nil && p(1) != "" {
			u.host = p(1)
		}
		h.forward(l, raw)
	case "ENCAP":
		if p(1) == "CHGHOST" {
			if u := h.users[p(2)]; u != nil && p(3) != "" {
				u.host = p(3)
			}
		}
		if p(1) == "LOGIN" {
			if u := h.users[src]; u != nil {
				u.account = p(2)
			}
		}
		h.forward(l, raw)
	case "PRIVMSG", "NOTICE":
		h.message(l, src, m.Command, p(0), p(1), raw)
	case "WHOIS":
		h.whois(l, src, p(0), p(1), raw)
	case "VERSION", "MOTD", "ADMIN", "INFO", "TIME", "LUSERS", "STATS", "LINKS", "TRACE", "USERS":
		h.query(l, src, m.Command, m.Params, raw)
	case "SVINFO", "CAPAB", "PASS":
	default:
		if len(m.Command) == 3 && m.Command[0] >= '0' && m.Command[0] <= '9' {
			// a numeric: to the user it answers
			h.route(p(0), l, raw)
			return "", true
		}
		h.forward(l, raw) // TMODE, BMASK, MODE, OPERWALL, BAN, ...: on to the others
	}
	return "", true
}

// register is a line before the link's SERVER is taken.
func (h *Hub) register(l *link, m *ircmsg.Message) (string, bool) {
	p := func(i int) string {
		if i < len(m.Params) {
			return m.Params[i]
		}
		return ""
	}
	switch m.Command {
	case "PASS":
		// PASS password TS 6 :SID; hybrid 8's SERVER gives the SID instead
		l.pass = p(0)
		if strings.ToUpper(p(1)) == "TS" {
			if p(2) != "6" || !sidRE.MatchString(p(3)) {
				return "TS6 only (PASS password TS 6 :SID)", false
			}
			l.sid = p(3)
		}
	case "CAPAB":
		for _, c := range strings.Fields(strings.ToUpper(p(0))) {
			l.caps[c] = true
		}
	case "SERVER":
		name := p(0)
		peer := h.peer(name)
		desc := m.Params[len(m.Params)-1]
		if len(m.Params) >= 4 && sidRE.MatchString(p(2)) {
			l.sid = p(2) // SERVER name hops SID +flags :description (hybrid 8)
			l.hybrid = true
		}
		switch {
		case l.sid == "" || len(m.Params) < 3:
			return "no SID (PASS password TS 6 :SID)", false
		case peer == nil:
			return "no link for " + name, false
		case l.peer != nil && fold(l.peer.Name) != fold(name):
			return "linked to " + l.peer.Name + ", not " + name, false
		case l.pass != peer.AcceptPassword:
			return "bad password", false
		case !l.caps["ENCAP"]:
			return "the BBS needs ENCAP", false
		case h.names[fold(name)] != nil:
			return name + " is on the network already", false
		case h.servers[l.sid] != nil:
			return "SID " + l.sid + " is in use", false
		}
		if l.peer == nil {
			l.peer = peer
			l.handshake()
		}
		s := &server{sid: l.sid, name: name, desc: desc, hops: 1, uplink: h.me, link: l, tag: peer.Tag}
		h.servers[s.sid] = s
		h.names[fold(name)] = s
		l.srv = s
		h.B.Log.Printf("irclink: %s linked (%s)", name, l.conn.RemoteAddr())
		// the others are told of it at once
		for o := range h.links {
			if o != l && o.bursted {
				o.send(h.me.sid, "SID", s.name, "2", s.sid, s.desc)
			}
		}
	case "ERROR":
		return "ERROR: " + p(0), false
	case "PING":
		l.send("", "PONG", h.me.name, p(0))
	case "SVINFO", "PONG", "NOTICE":
	default:
		return "not registered: " + m.Command, false
	}
	return "", true
}

// isMe: a name or SID is the BBS's.
func (h *Hub) isMe(s string) bool { return s == h.me.sid || fold(s) == fold(h.me.name) }

// sourceServer is the server a source (a SID or UID) is on (nil: not
// known).
func (h *Hub) sourceServer(src string) *server {
	if s := h.servers[src]; s != nil {
		return s
	}
	if u := h.users[src]; u != nil {
		return u.srv
	}
	return nil
}

// forward sends a line on to every link but the one it came by.
func (h *Hub) forward(from *link, raw string) {
	for l := range h.links {
		if l != from && l.bursted && l.srv != nil {
			l.sendRaw(raw)
		}
	}
}

// broadcast sends a line to every link.
func (h *Hub) broadcast(raw string) { h.forward(nil, raw) }

// route sends a line toward a server or user (a SID, UID, server name or
// nick), but never back by the link it came by.
func (h *Hub) route(target string, from *link, raw string) {
	var s *server
	if u := h.users[target]; u != nil {
		s = u.srv
	} else if u := h.nicks[fold(target)]; u != nil {
		s = u.srv
	} else if s = h.servers[target]; s == nil {
		s = h.names[fold(target)]
	}
	if s == nil || s.link == nil || s.link == from || !s.link.bursted {
		return
	}
	s.link.sendRaw(raw)
}

// sid is SID: a server behind the link.
func (h *Hub) sid(l *link, src, name, hops, sid, desc, raw string) (string, bool) {
	up := h.servers[src]
	if up == nil || up.link != l && up != l.srv {
		return "", true
	}
	if h.names[fold(name)] != nil || h.servers[sid] != nil {
		// on the network by another way: a loop
		return "server " + name + " (" + sid + ") is on the network already", false
	}
	n, _ := strconv.Atoi(hops)
	s := &server{sid: sid, name: name, desc: desc, hops: max(n, up.hops+1), uplink: up, link: l}
	h.servers[sid] = s
	h.names[fold(name)] = s
	h.forward(l, raw)
	return "", true
}

// squit takes a server behind a link off the network (the link itself:
// all behind it), and tells the other links.
func (h *Hub) squit(s *server, from *link, why string) {
	h.removeServer(s, why)
	for l := range h.links {
		if l != from && l.bursted && l.srv != nil && l.srv != s {
			l.send(h.me.sid, "SQUIT", s.sid, why)
		}
	}
}

// burst gives a link the network as the BBS has it: the servers, users
// and channels not behind it.  Lines may go to it from now on.
func (h *Hub) burst(l *link) {
	// collisions with what it brought are settled first: ours renamed
	for _, u := range h.local {
		if o := h.nicks[fold(u.nick)]; o != nil && o != u {
			h.renameLocal(u)
		}
	}
	var srvs []*server
	for _, s := range h.servers {
		if s != h.me && s.link != l {
			srvs = append(srvs, s)
		}
	}
	sort.Slice(srvs, func(i, j int) bool { return srvs[i].hops < srvs[j].hops })
	for _, s := range srvs {
		l.send(s.uplink.sid, "SID", s.name, strconv.Itoa(s.hops+1), s.sid, s.desc)
	}
	var us []*user
	for _, u := range h.users {
		if u.srv.link != l {
			us = append(us, u)
		}
	}
	sort.Slice(us, func(i, j int) bool { return us[i].uid < us[j].uid })
	for _, u := range us {
		l.sendRaw(h.introduce(u, l))
		if u.away != "" {
			l.send(u.uid, "AWAY", u.away)
		}
	}
	for _, ch := range h.chans {
		var members []string
		for u := range ch.members {
			if u.srv.link != l {
				members = append(members, u.chans[ch]+u.uid)
			}
		}
		if len(members) == 0 {
			continue
		}
		sort.Strings(members)
		ts := strconv.FormatInt(ch.ts, 10)
		for len(members) > 0 {
			k, size := 0, 0
			for k < len(members) && size+len(members[k])+1 < 400 {
				size += len(members[k]) + 1
				k++
			}
			l.send(h.me.sid, "SJOIN", ts, ch.name, ch.modes, strings.Join(members[:k], " "))
			members = members[k:]
		}
		if ch.topic != "" {
			if l.hybrid {
				l.send(h.me.sid, "TBURST", ts, ch.name, ts, h.me.name, ch.topic)
			} else {
				l.send(h.me.sid, "TB", ch.name, ts, h.me.name, ch.topic)
			}
		}
	}
	if l.hybrid {
		l.send(h.me.sid, "EOB")
	}
	l.bursted = true
	l.send(h.me.sid, "PING", h.me.name, l.srv.sid)
}
