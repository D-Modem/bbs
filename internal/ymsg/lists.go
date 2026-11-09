package ymsg

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// defaultGroup holds friends in no group.
const defaultGroup = "Friends"

// loadLists reads the user's friends and blocked lists.
func (ss *session) loadLists() {
	st := ss.s.B.Store
	fs, err := st.Friends(ss.u.ID)
	if err != nil {
		ss.s.B.Log.Printf("node %d: ymsg: friends: %v", ss.n.Num, err)
	}
	bs, err := st.Blocked(ss.u.ID)
	if err != nil {
		ss.s.B.Log.Printf("node %d: ymsg: blocked: %v", ss.n.Num, err)
	}
	ss.mu.Lock()
	ss.friends = map[int64]store.Friend{}
	for _, f := range fs {
		ss.friends[f.ID] = f
	}
	ss.blocked = map[int64]string{}
	for _, b := range bs {
		ss.blocked[b.ID] = idOfName(b.Name)
	}
	ss.mu.Unlock()
}

// groupOf is a friend's group, as the client shows it.
func groupOf(f store.Friend) string {
	if f.Group == "" {
		return defaultGroup
	}
	return f.Group
}

// grouped are the friends by group, groups and friends in order.
func (ss *session) grouped() ([]string, map[string][]string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	by := map[string][]string{}
	for _, f := range ss.friends {
		g := groupOf(f)
		by[g] = append(by[g], idOfName(f.Name))
	}
	var groups []string
	for g := range by {
		groups = append(groups, g)
		sort.Strings(by[g])
	}
	sort.Strings(groups)
	return groups, by
}

// ignored are the blocked list's IDs, in order.
func (ss *session) ignored() []string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	var out []string
	for _, id := range ss.blocked {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// cookies are the Y and T cookies the client is given (only what the
// client keeps, for its web links; nothing checks them).
func (ss *session) cookies() (string, string) {
	y := "v=1&n=" + randomString(26) + "&l=" + ss.id + "/o&p=" + randomString(20) + "&r=" + randomString(3) +
		"&lg=us&intl=us&np=1"
	t := "z=" + randomString(24) + "&a=" + randomString(12) + "&sk=" + randomString(24) + "&d=" + randomString(30)
	return y, t
}

// sendLists sends the buddy list and ignore list: in LIST, and for
// YMSG 15 and later in LIST_15 too.
func (ss *session) sendLists() {
	groups, by := ss.grouped()
	ign := ss.ignored()
	p := &packet{service: svcList, status: stOK}
	if ss.ver < 15 {
		var b strings.Builder
		for _, g := range groups {
			b.WriteString(g + ":" + strings.Join(by[g], ",") + "\n")
		}
		if b.Len() > 0 {
			p.add(87, b.String())
		}
		if len(ign) > 0 {
			p.add(88, strings.Join(ign, ","))
		}
	}
	y, t := ss.cookies()
	p.add(89, ss.id).add(59, "Y\t"+y+"; expires=Thu, 15 Apr 2030 20:00:00 GMT; path=/; domain=.yahoo.com").
		add(59, "T\t"+t+"; expires=Thu, 15 Apr 2030 20:00:00 GMT; path=/; domain=.yahoo.com").
		add(90, "1").add(100, "0").add(101, "").add(102, "").add(93, "86400").add(3, ss.id)
	ss.k.write(p)
	if ss.ver < 15 {
		return
	}
	l := &packet{service: svcList15, status: stOK}
	l.add(302, "318")
	for _, g := range groups {
		l.add(300, "318").add(65, g).add(302, "319")
		for _, id := range by[g] {
			l.add(300, "319").add(7, id).add(301, "319")
		}
		l.add(303, "319").add(301, "318")
	}
	if len(ign) > 0 {
		l.add(302, "320")
		for _, id := range ign {
			l.add(300, "320").add(7, id).add(301, "320")
		}
		l.add(303, "320")
	}
	l.add(303, "318")
	ss.k.write(l)
}

// buddyState is how a buddy shows to the client: its status, custom
// message, and away flag; "" key: offline.
type buddyState struct {
	status int
	msg    string
	away   bool
}

func (b buddyState) key() string {
	a := "0"
	if b.away {
		a = "1"
	}
	return strconv.Itoa(b.status) + "|" + a + "|" + b.msg
}

// stateFor is what viewer sees of u (ok false: offline).  A user blocking
// the viewer, or invisible, is offline to them; one on Yahoo has the
// status they set there; one on the BBS by any other route is available,
// with what they are doing as a custom message.
func (s *Server) stateFor(u, viewer *store.User) (buddyState, bool) {
	if s.B.Blocks(u, viewer) {
		return buddyState{}, false
	}
	var best *buddyState
	for _, x := range s.sessionsOf(u.ID) {
		x.mu.Lock()
		st := buddyState{status: x.status, msg: x.msg, away: x.away}
		inv, on := x.invisible, x.online
		x.mu.Unlock()
		if inv {
			return buddyState{}, false
		}
		if on && (best == nil || st.status == statusAvailable) {
			best = &st
		}
	}
	if best != nil {
		return *best, true
	}
	for _, n := range s.nodesOf(u.Name) {
		if s.onNode(n.Num) == nil {
			msg := ""
			if n.Via != "" {
				msg = "on the BBS by " + n.Via
			}
			return buddyState{status: statusCustom, msg: msg}, true
		}
	}
	return buddyState{}, false
}

// buddyPairs are a buddy's entry in a status packet.
func buddyPairs(id string, b buddyState) []pair {
	ps := []pair{kv(7, id), kn(10, int64(b.status))}
	if b.status == statusCustom {
		ps = append(ps, kv(19, b.msg))
	}
	away := "0"
	if b.away {
		away = "1"
	}
	return append(ps, kv(47, away), kv(11, "0"), kv(17, "0"), kv(13, "1"), kv(97, "1"))
}

// sendStatuses tells the client which of its buddies are on: LOGON (with
// itself in keys 0 and 1, which completes an older client's sign-in), and
// STATUS_15 for YMSG 15 and later.
func (ss *session) sendStatuses() {
	ss.mu.Lock()
	ids := make([]int64, 0, len(ss.friends))
	for id := range ss.friends {
		ids = append(ids, id)
	}
	ss.mu.Unlock()
	type on struct {
		id string
		b  buddyState
	}
	var ons []on
	for _, fid := range ids {
		u, err := ss.s.B.Store.UserByID(fid)
		if err != nil {
			continue
		}
		b, ok := ss.s.stateFor(u, ss.u)
		ss.mu.Lock()
		if ok {
			ss.told[fid] = b.key()
		} else {
			ss.told[fid] = ""
		}
		ss.mu.Unlock()
		if ok {
			ons = append(ons, on{idOf(u), b})
		}
	}
	sort.Slice(ons, func(i, j int) bool { return ons[i].id < ons[j].id })
	if ss.ver >= 15 {
		p := &packet{service: svcStatus15, status: stOK}
		p.add(0, ss.id).add(1, ss.id)
		if len(ons) > 0 {
			p.add(302, "315")
			for _, o := range ons {
				p.add(300, "315")
				p.pairs = append(p.pairs, buddyPairs(o.id, o.b)...)
				p.add(301, "315")
			}
			p.add(303, "315")
		}
		ss.k.write(p)
		return
	}
	p := &packet{service: svcLogon, status: stOK}
	p.add(0, ss.id).add(1, ss.id).addInt(8, int64(len(ons)))
	for _, o := range ons {
		p.pairs = append(p.pairs, buddyPairs(o.id, o.b)...)
	}
	ss.k.write(p)
}

// buddyChanged tells the client a buddy's state, if it differs from what
// it was last told.
func (ss *session) buddyChanged(u *store.User) {
	if u.ID == ss.u.ID || !ss.isOnline() {
		return
	}
	ss.mu.Lock()
	_, ok := ss.friends[u.ID]
	was := ss.told[u.ID]
	ss.mu.Unlock()
	if !ok {
		return
	}
	b, on := ss.s.stateFor(u, ss.u)
	now := ""
	if on {
		now = b.key()
	}
	if now == was {
		return
	}
	ss.mu.Lock()
	ss.told[u.ID] = now
	ss.mu.Unlock()
	id := idOf(u)
	switch {
	case !on:
		ss.k.send(svcLogoff, stOK, kv(7, id), kv(10, "0"), kv(13, "0"))
	case was == "":
		svc := uint16(svcLogon)
		if ss.ver >= 15 {
			svc = svcStatus15
		}
		ss.k.send(svc, stOK, buddyPairs(id, b)...)
	default:
		svc := uint16(svcIsAway)
		switch {
		case ss.ver >= 15:
			svc = svcStatus15
		case ss.ver >= 13:
			svc = svcStatusUpdate
		case b.status == statusAvailable:
			svc = svcIsBack
		}
		ss.k.send(svc, stOK, buddyPairs(id, b)...)
	}
}

// syncLists has the client's lists match the store's: friends added or
// moved elsewhere are added (ADDBUDDY), those gone removed (REMBUDDY),
// the ignore list likewise.
func (ss *session) syncLists() {
	if !ss.isOnline() {
		return
	}
	ss.mu.Lock()
	oldF := ss.friends
	oldB := ss.blocked
	ss.mu.Unlock()
	ss.loadLists()
	ss.mu.Lock()
	newF, newB := ss.friends, ss.blocked
	ss.mu.Unlock()
	for id, f := range oldF {
		if g, ok := newF[id]; !ok || groupOf(g) != groupOf(f) {
			ss.k.send(svcRemBuddy, stOK, kv(1, ss.id), kv(7, idOfName(f.Name)), kv(65, groupOf(f)), kv(66, "0"))
			ss.mu.Lock()
			delete(ss.told, id)
			ss.mu.Unlock()
		}
	}
	for id, f := range newF {
		if o, ok := oldF[id]; !ok || groupOf(o) != groupOf(f) {
			ss.k.send(svcAddBuddy, stOK, kv(1, ss.id), kv(7, idOfName(f.Name)), kv(65, groupOf(f)), kv(66, "0"))
			if u, err := ss.s.B.Store.UserByID(id); err == nil {
				ss.buddyChanged(u)
			}
		}
	}
	for id, name := range oldB {
		if _, ok := newB[id]; !ok {
			ss.k.send(svcIgnoreContact, stOK, kv(0, ss.id), kv(7, name), kv(13, "2"), kv(66, "0"))
		}
	}
	for id, name := range newB {
		if _, ok := oldB[id]; !ok {
			ss.k.send(svcIgnoreContact, stOK, kv(0, ss.id), kv(7, name), kv(13, "1"), kv(66, "0"))
		}
	}
}

// addBuddy is ADDBUDDY: a friend added (or, already one, moved to the
// group).
func (ss *session) addBuddy(p *packet) {
	who, group := p.get(7), strings.TrimSpace(p.get(65))
	if group == "" {
		group = defaultGroup
	}
	t := ss.s.resolve(who)
	if t == nil || t.ID == ss.u.ID {
		ss.k.send(svcAddBuddy, stOK, kv(1, ss.id), kv(7, who), kv(65, group), kv(66, "3"))
		ss.k.send(svcRemBuddy, stOK, kv(1, ss.id), kv(7, who), kv(65, group), kv(66, "0"))
		ss.system(who + " is not a user here: only the BBS's users can be on your list.")
		return
	}
	g := group
	if g == defaultGroup {
		g = ""
	}
	ss.mu.Lock()
	f := ss.friends[t.ID]
	ss.mu.Unlock()
	f.ID, f.Name, f.Group = t.ID, t.Name, g
	if err := ss.s.B.Store.PutFriend(ss.u.ID, f); err != nil {
		ss.s.B.Log.Printf("node %d: ymsg: adding a friend: %v", ss.n.Num, err)
		ss.k.send(svcAddBuddy, stOK, kv(1, ss.id), kv(7, who), kv(65, group), kv(66, "1"))
		return
	}
	ss.mu.Lock()
	ss.friends[t.ID] = store.Friend{ID: t.ID, Name: t.Name, Group: g, Added: time.Now()}
	delete(ss.told, t.ID)
	ss.mu.Unlock()
	id := idOf(t)
	ss.k.send(svcAddBuddy, stOK, kv(1, ss.id), kv(7, id), kv(65, group), kv(66, "0"))
	if ss.ver >= 13 {
		// "may I add you": every BBS user may be, so it is granted
		ss.k.send(svcAuthReq15, stOK, kv(4, id), kv(5, ss.id), kv(13, "1"))
	}
	ss.buddyChanged(t)
	ss.s.B.FriendsChanged(ss.u.ID)
}

// remBuddy is REMBUDDY: a friend taken off.
func (ss *session) remBuddy(p *packet) {
	who, group := p.get(7), p.get(65)
	t := ss.s.resolve(who)
	if t != nil {
		if err := ss.s.B.Store.RemoveFriend(ss.u.ID, t.ID); err != nil {
			ss.s.B.Log.Printf("node %d: ymsg: removing a friend: %v", ss.n.Num, err)
		}
		ss.mu.Lock()
		delete(ss.friends, t.ID)
		delete(ss.told, t.ID)
		ss.mu.Unlock()
		who = idOf(t)
	}
	ss.k.send(svcRemBuddy, stOK, kv(1, ss.id), kv(7, who), kv(65, group), kv(66, "0"))
	if t != nil {
		ss.s.B.FriendsChanged(ss.u.ID)
	}
}

// renameGroup is GROUPRENAME: 65 the old name, 67 the new (some clients
// the other way about: the old is the one that exists).
func (ss *session) renameGroup(p *packet) {
	a, b := p.get(65), p.get(67)
	groups, _ := ss.grouped()
	has := func(g string) bool {
		for _, x := range groups {
			if x == g {
				return true
			}
		}
		return false
	}
	from, to := a, b
	if !has(a) && has(b) {
		from, to = b, a
	}
	to = strings.TrimSpace(to)
	if to == "" || !has(from) {
		return
	}
	g := to
	if g == defaultGroup {
		g = ""
	}
	ss.mu.Lock()
	var fs []store.Friend
	for id, f := range ss.friends {
		if groupOf(f) == from {
			f.Group = g
			ss.friends[id] = f
			fs = append(fs, f)
		}
	}
	ss.mu.Unlock()
	for _, f := range fs {
		if err := ss.s.B.Store.PutFriend(ss.u.ID, f); err != nil {
			ss.s.B.Log.Printf("node %d: ymsg: renaming a group: %v", ss.n.Num, err)
		}
	}
	ss.k.send(svcGroupRename, stOK, kv(1, ss.id), kv(65, to), kv(66, "0"), kv(67, from))
	ss.s.B.FriendsChanged(ss.u.ID)
}

// changeGroup is YMSG 15's move of a buddy: 7 the buddy, 224 the old
// group, 264 the new.
func (ss *session) changeGroup(p *packet) {
	t := ss.s.resolve(p.get(7))
	to := strings.TrimSpace(p.get(264))
	if t == nil || to == "" {
		return
	}
	ss.mu.Lock()
	f, ok := ss.friends[t.ID]
	ss.mu.Unlock()
	if !ok {
		return
	}
	f.Group = to
	if to == defaultGroup {
		f.Group = ""
	}
	if err := ss.s.B.Store.PutFriend(ss.u.ID, f); err != nil {
		ss.s.B.Log.Printf("node %d: ymsg: moving a friend: %v", ss.n.Num, err)
		return
	}
	ss.mu.Lock()
	ss.friends[t.ID] = f
	ss.mu.Unlock()
	ss.k.send(svcChangeGroup15, stOK, kv(0, ss.id), kv(302, "240"), kv(300, "240"), kv(7, idOf(t)),
		kv(224, p.get(224)), kv(264, to), kv(301, "240"), kv(303, "240"))
	ss.s.B.FriendsChanged(ss.u.ID)
}

// ignore is IGNORECONTACT: 13 is 1 to ignore one, 2 to stop.
func (ss *session) ignore(p *packet) {
	who := p.get(7)
	t := ss.s.resolve(who)
	if t == nil || t.ID == ss.u.ID {
		ss.k.send(svcIgnoreContact, stOK, kv(0, ss.id), kv(7, who), kv(13, p.get(13)), kv(66, "3"))
		return
	}
	var err error
	if p.get(13) == "2" {
		err = ss.s.B.Store.Unblock(ss.u.ID, t.ID)
	} else {
		err = ss.s.B.Store.Block(ss.u.ID, t.ID)
	}
	if err != nil {
		ss.s.B.Log.Printf("node %d: ymsg: ignore list: %v", ss.n.Num, err)
		ss.k.send(svcIgnoreContact, stOK, kv(0, ss.id), kv(7, who), kv(13, p.get(13)), kv(66, "1"))
		return
	}
	ss.mu.Lock()
	if p.get(13) == "2" {
		delete(ss.blocked, t.ID)
	} else {
		ss.blocked[t.ID] = idOf(t)
	}
	ss.mu.Unlock()
	ss.k.send(svcIgnoreContact, stOK, kv(0, ss.id), kv(7, idOf(t)), kv(13, p.get(13)), kv(66, "0"))
	ss.s.B.FriendsChanged(ss.u.ID)
	ss.s.changed(ss.u)
}
