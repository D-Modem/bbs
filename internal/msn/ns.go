package msn

import (
	"crypto/subtle"
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

// statuses a client may set
var statuses = map[string]string{"NLN": "online", "BSY": "busy", "IDL": "idle", "BRB": "be right back",
	"AWY": "away", "PHN": "on the phone", "LUN": "out to lunch", "HDN": "appearing offline"}

// nsSession is a client signed in to the notification server, on a node
// of its own.
type nsSession struct {
	s      *Server
	k      *conn
	host   string
	ver    int
	u      *store.User
	n      *bbs.Node
	handle string // as signed in

	loginTrid string // its USR S's

	todo chan func()
	done chan struct{}
	once sync.Once

	mu       sync.Mutex
	friendly string // its display name
	status   string // "" until its first CHG
	blp      string // AL: all may see it; BL: only its friends
	gtc      string
	listVer  int
	synced   bool                   // has had its lists (SYN)
	fl       map[int64]store.Friend // its friends, as last told
	rl       map[int64]bool         // who has it as a friend, as last told
	bl       map[int64]bool         // its blocked list, as last told
	groups   map[int]string         // MSNP7 group ids: names ("" is 0, Other Contacts)
	told     map[int64]string       // what it was last told of each friend's status ("": offline)
	noted    map[int64]bool         // told that these are away (their messages kept)
	keptSent bool
}

// notification serves a notification session: the sign-in (begun with
// first, a VER), then its commands until it goes.
func (s *Server) notification(k *conn, host string, first *cmd) {
	ns := &nsSession{s: s, k: k, host: host, todo: make(chan func(), queueLen), done: make(chan struct{}),
		blp: "AL", gtc: "A", fl: map[int64]store.Friend{}, rl: map[int64]bool{}, bl: map[int64]bool{},
		groups: map[int]string{0: ""}, told: map[int64]string{}, noted: map[int64]bool{}}
	if !ns.signIn(first) {
		return
	}
	u := ns.u
	n, err := s.B.Enter(&bbs.Caller{Via: "msn", Listener: "msn", Remote: host, Source: "ip:" + host,
		Terminal: fmt.Sprintf("MSNP%d", ns.ver)}, ns.close)
	if err != nil {
		k.sendErr(errBusy, ns.loginTrid)
		return
	}
	ns.n = n
	n.SetUser(u)
	n.Doing("MSN")
	// one session an account (MSNP2-7 had no more): the older is told it
	// signed in elsewhere
	for _, o := range s.sessionsOf(u.ID) {
		o.do(func() {
			o.k.send("OUT", "OTH")
			o.close()
		})
	}
	if err := ns.loadLists(); err != nil {
		s.B.Log.Printf("node %d: msn: lists: %v", n.Num, err)
	}
	s.register(ns)
	s.B.Log.Printf("node %d: %s on MSN (%s, MSNP%d)", n.Num, u.Name, host, ns.ver)
	defer func() {
		ns.close()
		s.unregister(ns)
		s.leaveSwitchboards(ns)
		n.Leave()
		s.B.Log.Printf("node %d: gone (msn)", n.Num)
		s.changed(u)
	}()
	ok := []string{"USR", ns.loginTrid, "OK", ns.handle, encode(ns.friendly)}
	if ns.ver >= 6 {
		ok = append(ok, "1") // its e-mail address verified
	}
	ns.k.send(ok...)
	ns.profile()
	go ns.events()
	ns.serve()
}

// signIn is VER, INF, CVR and USR, up to a user checked (false: it failed;
// the connection is closed).
func (ns *nsSession) signIn(c *cmd) bool {
	s := ns.s
	var salt string
	var target *store.User
	for range 12 {
		switch c.name {
		case "VER":
			ns.ver = 0
			for _, a := range c.args {
				if v, ok := strings.CutPrefix(strings.ToUpper(a), "MSNP"); ok {
					if n, err := strconv.Atoi(v); err == nil && n >= minVersion && n <= maxVersion && n > ns.ver {
						ns.ver = n
					}
				}
			}
			if ns.ver == 0 {
				ns.k.send("VER", c.trid, "0")
				s.B.Log.Printf("msn: %s: a client for %s, which is not served (MSNP%d to %d are)",
					ns.host, strings.Join(c.args, " "), minVersion, maxVersion)
				return false
			}
			ns.k.send("VER", c.trid, fmt.Sprintf("MSNP%d", ns.ver))
		case "INF":
			ns.k.send("INF", c.trid, "MD5")
		case "CVR", "CVQ":
			ns.cvr(c)
		case "USR":
			pkg, stage := strings.ToUpper(c.arg(0)), strings.ToUpper(c.arg(1))
			switch {
			case pkg == "MD5" && stage == "I":
				ns.handle = c.arg(2)
				target = s.resolve(ns.handle)
				if target != nil {
					var err error
					if salt, err = s.B.Store.MSNSalt(target.ID); err != nil {
						s.B.Log.Printf("msn: the salt of %s: %v", target.Name, err)
					}
				}
				if salt == "" {
					salt = randomHex(10) // an unknown handle is challenged too; its sign-in fails
				}
				ns.k.send("USR", c.trid, "MD5", "S", salt)
			case pkg == "MD5" && stage == "S" && salt != "":
				ns.loginTrid = c.trid
				u, err := ns.md5Login(target, salt, c.arg(2))
				return ns.signedIn(c.trid, u, err)
			case pkg == "CTP" && stage == "I" && ns.ver == 2:
				ns.handle = c.arg(2)
				ns.loginTrid = c.trid
				u, err := ns.passwordLogin(c.arg(2), decode(c.arg(3)))
				return ns.signedIn(c.trid, u, err)
			default:
				ns.k.sendErr(errAuth, c.trid)
				return false
			}
		case "OUT":
			return false
		default:
			ns.k.sendErr(errNotLoggedIn, c.trid)
		}
		var err error
		if c, err = ns.k.read(); err != nil {
			return false
		}
	}
	return false
}

// signedIn finishes a sign-in: the user, or the error told (false).
func (ns *nsSession) signedIn(trid string, u *store.User, err error) bool {
	if err == nil {
		err = ns.s.may(u)
	}
	if err != nil {
		code := errAuth
		if errors.Is(err, bbs.ErrNotAllowed) {
			code = errNotAllowed
		}
		ns.k.sendErr(code, trid)
		return false
	}
	ns.u = u
	ns.friendly = u.Name
	if !strings.Contains(ns.handle, "@") {
		ns.handle = ns.s.handleOf(u)
	}
	return true
}

// md5Login checks an MD5 sign-in's digest against the account's.
func (ns *nsSession) md5Login(target *store.User, salt, digest string) (*store.User, error) {
	s := ns.s
	name := ns.handle
	if target != nil {
		name = target.Name
	}
	var stale bool
	u, err := s.B.AuthenticateBy("msn", "ip:"+ns.host, name, func() (*store.User, error) {
		if target == nil {
			return nil, store.ErrNotFound
		}
		ksalt, d, ok, err := s.B.Store.MSNKey(target.ID)
		if err != nil {
			return nil, err
		}
		if !ok || ksalt != salt {
			stale = true
			return nil, store.ErrNotFound
		}
		if subtle.ConstantTimeCompare([]byte(d), []byte(strings.ToLower(digest))) != 1 {
			return nil, store.ErrNotFound
		}
		return target, nil
	})
	if stale && target != nil {
		s.B.Log.Printf("msn: %s: %s has no MSN password digest yet: log in once by another way "+
			"(telnet, IRC, AIM, the web) or set the password again", ns.host, target.Name)
	}
	return u, err
}

// passwordLogin checks a password sent in the clear (CTP, MSN 1.0).
func (ns *nsSession) passwordLogin(handle, pw string) (*store.User, error) {
	name := handle
	if u := ns.s.resolve(handle); u != nil {
		name = u.Name
	}
	return ns.s.B.Authenticate("msn", "ip:"+ns.host, name, pw)
}

// cvr answers a client's version: whatever it is, it is current.
func (ns *nsSession) cvr(c *cmd) {
	v := c.arg(5)
	if v == "" {
		v = "1.0.0863"
	}
	site := ns.s.S.Site
	if site == "" {
		site = "http://" + ns.s.S.Domain + "/"
	}
	ns.k.send("CVR", c.trid, v, v, v, site, site)
}

// profile is the initial profile message after sign-in.
func (ns *nsSession) profile() {
	p := fmt.Sprintf("MIME-Version: 1.0\r\nContent-Type: text/x-msmsgsprofile; charset=UTF-8\r\n"+
		"LoginTime: %d\r\nEmailEnabled: 0\r\nMemberIdHigh: 0\r\nMemberIdLow: %d\r\n"+
		"lang_preference: 1033\r\npreferredEmail: \r\ncountry: \r\nPostalCode: \r\nGender: \r\n"+
		"Kid: 0\r\nAge: \r\nsid: 507\r\nkv: 2\r\nMSPAuth: %s\r\nClientIP: %s\r\nClientPort: 0\r\n\r\n",
		time.Now().Unix(), ns.u.ID, randomHex(16), ns.host)
	ns.k.sendPayload([]byte(p), "MSG", "Hotmail", "Hotmail")
}

// closed reports that the session is over.
func (ns *nsSession) closed() bool {
	select {
	case <-ns.done:
		return true
	default:
		return false
	}
}

// close ends the session.
func (ns *nsSession) close() {
	ns.once.Do(func() {
		close(ns.done)
		ns.k.close()
	})
}

// do has the session's goroutine do f (dropped if too much waits).
func (ns *nsSession) do(f func()) {
	select {
	case ns.todo <- f:
	case <-ns.done:
	default:
		ns.s.B.Log.Printf("node %d: msn: too much waiting; dropped", ns.n.Num)
	}
}

// online: it has set a status (its first CHG).
func (ns *nsSession) online() bool {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	return ns.status != ""
}

// statusNow is its status ("" before its first CHG).
func (ns *nsSession) statusNow() string {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	return ns.status
}

// friendlyName is its display name.
func (ns *nsSession) friendlyName() string {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	return ns.friendly
}

// events gives the client what its node is told, and does what the
// server asks of the session, until it goes.
func (ns *nsSession) events() {
	for {
		select {
		case <-ns.done:
			return
		case f := <-ns.todo:
			f()
		case <-ns.n.Wake():
			if !ns.online() {
				continue // held until it sets a status (its first CHG drains them)
			}
			ns.drain()
		}
	}
}

// drain gives the client the node's events waiting.
func (ns *nsSession) drain() {
	for _, e := range ns.n.Pending() {
		ns.event(e)
	}
}

// serve reads the client's commands until it goes.
func (ns *nsSession) serve() {
	for {
		ns.k.c.SetDeadline(time.Now().Add(idleTimeout))
		c, err := ns.k.read()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !ns.closed() {
				ns.s.B.Log.Printf("node %d: msn: %v", ns.n.Num, err)
			}
			return
		}
		if c.name == "" {
			continue
		}
		if c.name == "OUT" {
			ns.k.send("OUT")
			return
		}
		done := make(chan struct{})
		ns.do(func() {
			defer close(done)
			ns.command(c)
		})
		select {
		case <-done:
		case <-ns.done:
			return
		}
	}
}

// command does a command (in the session's goroutine).
func (ns *nsSession) command(c *cmd) {
	k := ns.k
	switch c.name {
	case "PNG":
		k.send("QNG")
	case "CVR", "CVQ":
		ns.cvr(c)
	case "INF":
		k.send("INF", c.trid, "MD5")
	case "SYN":
		ns.syn(c)
	case "LST":
		ns.lst(c)
	case "LSG":
		ns.lsg(c)
	case "CHG":
		ns.chg(c)
	case "ADD":
		ns.add(c)
	case "REM":
		ns.rem(c)
	case "REA":
		ns.rea(c)
	case "ADG", "RMG", "REG":
		ns.group(c)
	case "BLP":
		if m := strings.ToUpper(c.arg(0)); m == "AL" || m == "BL" {
			ns.mu.Lock()
			ns.blp = m
			ns.mu.Unlock()
			k.send("BLP", c.trid, ns.bumpVer(), m)
			ns.s.changed(ns.u)
		} else {
			k.sendErr(errBadParam, c.trid)
		}
	case "GTC":
		if m := strings.ToUpper(c.arg(0)); m == "A" || m == "N" {
			ns.mu.Lock()
			ns.gtc = m
			ns.mu.Unlock()
			k.send("GTC", c.trid, ns.bumpVer(), m)
		} else {
			k.sendErr(errBadParam, c.trid)
		}
	case "XFR":
		if strings.ToUpper(c.arg(0)) != "SB" {
			k.sendErr(errBadParam, c.trid)
			return
		}
		if st := ns.statusNow(); st == "" || st == "HDN" {
			k.sendErr(errNoSB, c.trid) // offline or hidden: no conversations
			return
		}
		sb := ns.s.newSwitchboard()
		ck := ns.s.newCookie(sb, ns, "")
		k.send("XFR", c.trid, "SB", ns.s.reachHost(k.c), "CKI", ck)
	case "QRY":
		k.send("QRY", c.trid)
	case "URL":
		if ns.s.S.Site == "" {
			k.sendErr(errDisabled, c.trid)
			return
		}
		k.send("URL", c.trid, ns.s.S.Site, ns.s.S.Site, "2")
	case "SND", "SDC", "FND", "PAG", "IMS":
		k.sendErr(errDisabled, c.trid)
	case "USR", "VER":
		k.sendErr(errSyntax, c.trid)
	default:
		k.sendErr(errSyntax, c.trid)
	}
}

// chg sets its status; the first also tells it who of its friends is on,
// and gives it what was kept for it.
func (ns *nsSession) chg(c *cmd) {
	st := strings.ToUpper(c.arg(0))
	if st == "FLN" {
		st = "HDN"
	}
	if _, ok := statuses[st]; !ok {
		ns.k.sendErr(errBadParam, c.trid)
		return
	}
	ns.mu.Lock()
	first := ns.status == ""
	ns.status = st
	ns.mu.Unlock()
	ns.k.send("CHG", c.trid, c.arg(0))
	doing := "MSN"
	if st != "NLN" {
		doing += " (" + statuses[st] + ")"
	}
	ns.n.Doing(doing)
	if first {
		ns.allContacts(c.trid)
		ns.drain()
		ns.deliverKept()
	}
	ns.s.changed(ns.u)
}

// bumpVer is the list version after a change.
func (ns *nsSession) bumpVer() string {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.listVer++
	return strconv.Itoa(ns.listVer)
}

// ver is the list version now.
func (ns *nsSession) verNow() string {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	return strconv.Itoa(ns.listVer)
}
