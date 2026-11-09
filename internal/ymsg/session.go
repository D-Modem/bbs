package ymsg

import (
	"crypto/md5"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// login failures (AUTHRESP key 66)
const (
	loginBadName     = 3
	loginBadPassword = 13
	loginLocked      = 14
)

// Yahoo statuses (key 10)
const (
	statusAvailable = 0
	statusBRB       = 1
	statusInvisible = 12
	statusCustom    = 99
	statusIdle      = 999
)

// statusNames are the statuses a client may set, as the BBS shows them.
var statusNames = map[int]string{0: "", 1: "be right back", 2: "busy", 3: "not at home", 4: "not at desk",
	5: "not in office", 6: "on the phone", 7: "on vacation", 8: "out to lunch", 9: "stepped out",
	12: "", 99: "", 999: "idle"}

// session is a client signed in, on a node of its own.
type session struct {
	s    *Server
	k    *conn
	host string
	u    *store.User
	n    *bbs.Node
	id   string // its Yahoo ID, as it signed in
	ver  int

	todo chan func()
	done chan struct{}
	once sync.Once

	mu        sync.Mutex
	online    bool // signed in and told its list
	status    int
	msg       string // custom status message
	away      bool
	invisible bool
	friends   map[int64]store.Friend // its buddy list, as last told
	blocked   map[int64]string       // its ignore list, as last told (by id: Yahoo ID)
	told      map[int64]string       // what it was last told of each buddy ("": offline)
	chats     map[string]string      // its chat rooms: BBS room -> name as the client calls it
	confs     map[string]string      // its conferences: BBS room -> conference name
	keptSent  bool
}

// handle serves a pager connection: the sign-in, then its packets.
func (s *Server) handle(c net.Conn) {
	host, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	k := newConn(c)
	c.SetDeadline(time.Now().Add(loginTimeout))
	ss := &session{s: s, k: k, host: host, todo: make(chan func(), queueLen), done: make(chan struct{}),
		friends: map[int64]store.Friend{}, blocked: map[int64]string{}, told: map[int64]string{},
		chats: map[string]string{}, confs: map[string]string{}}
	initial, ok := ss.signIn()
	if !ok {
		return
	}
	u := ss.u
	n, err := s.B.Enter(&bbs.Caller{Via: "ymsg", Listener: "ymsg", Remote: host, Source: "ip:" + host,
		Terminal: fmt.Sprintf("YMSG%d", ss.ver)}, ss.close)
	if err != nil {
		k.send(svcAuthResp, stDisconnected, kv(0, ss.id), kv(1, ss.id), kn(66, loginLocked))
		return
	}
	ss.n = n
	n.SetUser(u)
	n.Doing("Yahoo! Messenger")
	// one session an account, as on Yahoo's servers: the older is told it
	// signed in elsewhere
	for _, o := range s.sessionsOf(u.ID) {
		o.do(func() {
			o.k.send(svcLogoff, stDisconnected)
			o.close()
		})
	}
	if initial == statusInvisible {
		ss.invisible = true
	}
	s.register(ss)
	s.B.Log.Printf("node %d: %s on Yahoo (%s, YMSG%d)", n.Num, u.Name, host, ss.ver)
	defer func() {
		ss.close()
		s.unregister(ss)
		n.Leave()
		s.B.Log.Printf("node %d: gone (ymsg)", n.Num)
		s.changed(u)
	}()
	c.SetDeadline(time.Time{})
	ss.loadLists()
	ss.sendLists()
	ss.sendStatuses()
	ss.mu.Lock()
	ss.online = true
	ss.mu.Unlock()
	ss.drain()
	ss.deliverKept()
	s.changed(u)
	go ss.events()
	ss.serve()
}

// signIn is VERIFY, AUTH and AUTHRESP, up to a user checked (false: it
// failed; the connection is closed).  The initial status is AUTHRESP's.
func (ss *session) signIn() (initial uint32, ok bool) {
	s := ss.s
	k := ss.k
	var seed string
	authType := 0
	for range 8 {
		p, err := k.read()
		if err != nil {
			return 0, false
		}
		k.version = p.version
		ss.ver = int(p.version)
		switch p.service {
		case svcVerify:
			k.send(svcVerify, stServerAck)
		case svcAuth:
			ss.id = strings.TrimSpace(p.get(1))
			if ss.id == "" {
				return 0, false
			}
			k.session = randomSession()
			if ss.ver >= 16 {
				authType = 2
				seed = randomString(24)
			} else {
				seed = randomString(24)
			}
			k.send(svcAuth, stServerAck, kv(1, ss.id), kv(94, seed), kn(13, int64(authType)))
		case svcAuthResp:
			if seed == "" {
				return 0, false
			}
			var u *store.User
			var err error
			if authType == 2 {
				u, err = ss.tokenLogin(p, seed)
			} else {
				u, err = ss.challengeLogin(p, seed)
			}
			if err == nil {
				err = s.may(u)
			}
			if err != nil {
				code := loginBadPassword
				if errors.Is(err, bbs.ErrNotAllowed) || errors.Is(err, bbs.ErrLocked) {
					code = loginLocked
				}
				k.send(svcAuthResp, stDisconnected, kv(0, ss.id), kv(1, ss.id), kn(66, int64(code)))
				return 0, false
			}
			ss.u = u
			ss.id = idOf(u)
			return p.status, true
		case svcLogoff:
			return 0, false
		}
	}
	return 0, false
}

// challengeLogin checks auth type 0's answers (keys 6 and 96) against the
// account's kept digests.
func (ss *session) challengeLogin(p *packet, seed string) (*store.User, error) {
	s := ss.s
	user := p.get(0)
	if user == "" {
		user = p.get(1)
	}
	if user == "" {
		user = ss.id
	}
	target := s.resolve(user)
	name := user
	if target != nil {
		name = target.Name
	}
	var stale bool
	u, err := s.B.AuthenticateBy("ymsg", "ip:"+ss.host, name, func() (*store.User, error) {
		if target == nil {
			return nil, store.ErrNotFound
		}
		keys, ok, err := s.B.Store.YMSGKeys(target.ID)
		if err != nil {
			return nil, err
		}
		if !ok {
			stale = true
			return nil, store.ErrNotFound
		}
		want6, want96 := challengeAnswers(keys, user, seed)
		got6, got96 := p.get(6), p.get(96)
		if got6 == "" || subtle.ConstantTimeCompare([]byte(want6), []byte(got6)) != 1 {
			return nil, store.ErrNotFound
		}
		if got96 != "" && subtle.ConstantTimeCompare([]byte(want96), []byte(got96)) != 1 {
			return nil, store.ErrNotFound
		}
		return target, nil
	})
	if stale && target != nil {
		s.B.Log.Printf("ymsg: %s: %s has no Yahoo password digests yet: log in once by another way "+
			"(telnet, IRC, AIM, the web) or set the password again", ss.host, target.Name)
	}
	return u, err
}

// challengeAnswers are auth type 0's two answers (keys 6 and 96) for an
// account's digests, the ID as the client typed it, and the challenge.
func challengeAnswers(k store.YMSGKeys, user, seed string) (string, string) {
	for len(seed) < 16 {
		seed += "0"
	}
	sv := int(seed[15]) % 8 % 5
	var check byte
	order := func(hash string) string { return "" }
	switch sv {
	case 0:
		check = seed[int(seed[7])%16]
		order = func(h string) string { return h + user + seed }
	case 1:
		check = seed[int(seed[9])%16]
		order = func(h string) string { return user + seed + h }
	case 2:
		check = seed[int(seed[15])%16]
		order = func(h string) string { return seed + h + user }
	case 3:
		check = seed[int(seed[1])%16]
		order = func(h string) string { return user + h + seed }
	case 4:
		check = seed[int(seed[3])%16]
		order = func(h string) string { return h + seed + user }
	}
	p := md5.Sum([]byte(string(check) + order(k.Pass)))
	c := md5.Sum([]byte(string(check) + order(k.Crypt)))
	return store.Y64(p[:]), store.Y64(c[:])
}

// tokenLogin checks a YMSG 16 sign-in: the T cookie pwtoken_login gave,
// and key 307, MD5(crumb + challenge) in Yahoo's base 64.
func (ss *session) tokenLogin(p *packet, seed string) (*store.User, error) {
	s := ss.s
	t := s.cookie(p.get(278))
	name := ss.id
	if t != nil {
		name = t.u.Name
	}
	return s.B.AuthenticateBy("ymsg", "ip:"+ss.host, name, func() (*store.User, error) {
		if t == nil {
			return nil, store.ErrNotFound
		}
		if u := s.resolve(ss.id); u == nil || u.ID != t.u.ID {
			return nil, store.ErrNotFound
		}
		h := md5.Sum([]byte(t.crumb + seed))
		if subtle.ConstantTimeCompare([]byte(store.Y64(h[:])), []byte(p.get(307))) != 1 {
			return nil, store.ErrNotFound
		}
		return t.u, nil
	})
}

// closed reports that the session is over.
func (ss *session) closed() bool {
	select {
	case <-ss.done:
		return true
	default:
		return false
	}
}

// close ends the session.
func (ss *session) close() {
	ss.once.Do(func() {
		close(ss.done)
		ss.k.close()
	})
}

// do has the session's goroutine do f (dropped if too much waits).
func (ss *session) do(f func()) {
	select {
	case ss.todo <- f:
	case <-ss.done:
	default:
		ss.s.B.Log.Printf("node %d: ymsg: too much waiting; dropped", ss.n.Num)
	}
}

// isOnline: signed in and told its list.
func (ss *session) isOnline() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.online
}

// events gives the client what its node is told, and does what the
// server asks of the session, until it goes.
func (ss *session) events() {
	for {
		select {
		case <-ss.done:
			return
		case f := <-ss.todo:
			f()
		case <-ss.n.Wake():
			ss.drain()
		}
	}
}

// drain gives the client the node's events waiting.
func (ss *session) drain() {
	for _, e := range ss.n.Pending() {
		ss.event(e)
	}
}

// serve reads the client's packets until it goes.
func (ss *session) serve() {
	for {
		ss.k.c.SetReadDeadline(time.Now().Add(idleTimeout))
		p, err := ss.k.read()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !ss.closed() {
				ss.s.B.Log.Printf("node %d: ymsg: %v", ss.n.Num, err)
			}
			return
		}
		if p.service == svcLogoff {
			return
		}
		done := make(chan struct{})
		ss.do(func() {
			defer close(done)
			ss.packet(p)
		})
		select {
		case <-done:
		case <-ss.done:
			return
		}
	}
}

// packet does what a packet asks (in the session's goroutine).
func (ss *session) packet(p *packet) {
	switch p.service {
	case svcPing, svcKeepAlive, svcChatPing, svcIdle:
		// keeping the connection up
	case svcList:
		ss.sendLists()
	case svcUserStat:
		ss.sendStatuses()
	case svcIsAway, svcIsBack, svcStatusUpdate:
		ss.setStatus(p)
	case svcVisibleToggle:
		ss.mu.Lock()
		ss.invisible = p.get(13) == "2"
		ss.mu.Unlock()
		ss.s.changed(ss.u)
	case svcMessage:
		ss.message(p)
	case svcNotify:
		ss.notify(p)
	case svcAddBuddy:
		ss.addBuddy(p)
	case svcRemBuddy:
		ss.remBuddy(p)
	case svcGroupRename:
		ss.renameGroup(p)
	case svcChangeGroup15:
		ss.changeGroup(p)
	case svcIgnoreContact:
		ss.ignore(p)
	case svcAuthReq15, svcAuthBuddy, svcRejectContact:
		// answers to "may I add you": every BBS user may be added
	case svcConfInvite, svcConfAddInvite:
		ss.confInvite(p)
	case svcConfLogon:
		ss.confLogon(p)
	case svcConfDecline:
		ss.confDecline(p)
	case svcConfLogoff:
		ss.confLogoff(p)
	case svcConfMsg:
		ss.confMsg(p)
	case svcChatOnline:
		ss.k.send(svcChatOnline, stServerAck, kv(1, ss.id), kv(109, ss.id))
	case svcChatJoin, svcChatGoto:
		ss.chatJoin(p)
	case svcComment:
		ss.chatSay(p)
	case svcChatLeave, svcChatExit:
		ss.chatLeave(p)
	case svcChatLogout:
		ss.chatLogout()
	case svcChatAddInvite:
		ss.chatInvite(p)
	case svcFileTransfer, svcP2PFileXfer, svcFileTrans15, svcFileTransInfo15, svcFileTransAcc15, svcPeerToPeer:
		if to := p.get(5); to != "" && p.service != svcPeerToPeer {
			ss.system("Sending files by Yahoo! Messenger isn't available here: upload the file to " +
				"the BBS's file areas instead.")
		}
	}
}

// setStatus is ISAWAY, ISBACK or Y6_STATUS_UPDATE: the client's status.
func (ss *session) setStatus(p *packet) {
	st := statusAvailable
	if v := p.get(10); v != "" {
		fmt.Sscan(v, &st)
	} else if p.service == svcIsAway {
		st = int(p.status)
	}
	if _, ok := statusNames[st]; !ok {
		st = statusCustom
	}
	msg := ""
	if st == statusCustom {
		msg = bbs.Clean(fromYahoo(p.get(19), p.get(97) == "1"), 255)
	}
	ss.mu.Lock()
	ss.status = st
	ss.msg = msg
	ss.away = p.get(47) == "1" || (st != statusAvailable && st != statusCustom && st != statusInvisible)
	switch {
	case st == statusInvisible:
		ss.invisible = true
	case p.service != svcStatusUpdate:
		ss.invisible = false // the older clients' only way back from invisible
	}
	ss.mu.Unlock()
	doing := "Yahoo! Messenger"
	if d := statusNames[st]; d != "" {
		doing += " (" + d + ")"
	} else if msg != "" {
		doing += " (" + msg + ")"
	}
	ss.n.Doing(doing)
	ss.s.changed(ss.u)
}

// system sends the client an IM from the BBS.
func (ss *session) system(text string) {
	ss.im(ss.s.systemID(), text, time.Time{})
}
