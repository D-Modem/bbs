package msn

import (
	"sort"
	"strconv"
	"strings"

	"github.com/D-Modem/bbs/internal/store"
)

// The contact lists: the Forward List (FL) is the user's friends list; the
// Block List (BL) their blocked list; the Reverse List (RL) those who have
// them as a friend; the Allow List (AL), those not blocked who are on
// either.  A user who adds one is on their RL and not on their AL or BL,
// which a client asks about ("X has added you...").

const maxGroups = 30 // MSNP7's group ids are 0 to 29

// loadLists reads the lists from the store into the session.
func (ns *nsSession) loadLists() error {
	st := ns.s.B.Store
	fs, err := st.Friends(ns.u.ID)
	if err != nil {
		return err
	}
	bs, err := st.Blocked(ns.u.ID)
	if err != nil {
		return err
	}
	rs, err := st.FriendOf(ns.u.ID)
	if err != nil {
		return err
	}
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.fl = map[int64]store.Friend{}
	for _, f := range fs {
		ns.fl[f.ID] = f
		ns.groupIDLocked(f.Group)
	}
	ns.bl = map[int64]bool{}
	for _, b := range bs {
		ns.bl[b.ID] = true
	}
	ns.rl = map[int64]bool{}
	for _, r := range rs {
		ns.rl[r.ID] = true
	}
	return nil
}

// groupIDLocked is a group's id, given one if it has none.  ns.mu is
// held.
func (ns *nsSession) groupIDLocked(name string) int {
	if name == "" {
		return 0
	}
	for id, g := range ns.groups {
		if g == name {
			return id
		}
	}
	for id := 1; id < maxGroups; id++ {
		if _, used := ns.groups[id]; !used {
			ns.groups[id] = name
			return id
		}
	}
	return 0
}

// entry is one on a list, as LST gives it.
type entry struct {
	handle, name string
	group        int
}

// list is one of the lists, sorted by handle.
func (ns *nsSession) list(which string) []entry {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	var out []entry
	add := func(id int64, name string, group int) {
		out = append(out, entry{handle: ns.s.handleOfName(name), name: name, group: group})
	}
	names := ns.namesLocked()
	switch which {
	case "FL":
		for _, f := range ns.fl {
			n := f.Name
			if f.Alias != "" {
				n = f.Alias
			}
			out = append(out, entry{handle: ns.s.handleOfName(f.Name), name: n, group: ns.groupIDLocked(f.Group)})
		}
	case "AL":
		seen := map[int64]bool{}
		for id := range ns.fl {
			if !ns.bl[id] {
				seen[id] = true
			}
		}
		for id := range ns.rl {
			if !ns.bl[id] {
				seen[id] = true
			}
		}
		for id := range seen {
			add(id, names[id], 0)
		}
	case "BL":
		for id := range ns.bl {
			add(id, names[id], 0)
		}
	case "RL":
		for id := range ns.rl {
			add(id, names[id], 0)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].handle < out[j].handle })
	return out
}

// namesLocked are the names of everyone on the lists, by id.  ns.mu is
// held; the store is read for those not on the FL.
func (ns *nsSession) namesLocked() map[int64]string {
	names := map[int64]string{}
	for id, f := range ns.fl {
		names[id] = f.Name
	}
	for _, m := range []map[int64]bool{ns.bl, ns.rl} {
		for id := range m {
			if _, ok := names[id]; ok {
				continue
			}
			if u, err := ns.s.B.Store.UserByID(id); err == nil {
				names[id] = u.Name
			}
		}
	}
	return names
}

// handleOfName is the handle of a user of that name.
func (s *Server) handleOfName(name string) string {
	return strings.ReplaceAll(name, " ", "") + "@" + s.S.Domain
}

// syn sends the lists: the settings, the groups (MSNP7), each list.  The
// list version is always new, so the client takes all of it.
func (ns *nsSession) syn(c *cmd) {
	if err := ns.loadLists(); err != nil {
		ns.s.B.Log.Printf("node %d: msn: lists: %v", ns.n.Num, err)
		ns.k.sendErr(errInternal, c.trid)
		return
	}
	if ns.ver >= 8 {
		ns.syn8(c)
		return
	}
	have, _ := strconv.Atoi(c.arg(0))
	ns.mu.Lock()
	if ns.listVer <= have {
		ns.listVer = have + 1
	}
	ns.synced = true
	gtc, blp := ns.gtc, ns.blp
	ns.mu.Unlock()
	v := ns.verNow()
	ns.k.send("SYN", c.trid, v)
	ns.k.send("GTC", c.trid, v, gtc)
	ns.k.send("BLP", c.trid, v, blp)
	if ns.ver >= 7 {
		ns.sendGroups(c.trid, v)
	}
	for _, l := range []string{"FL", "AL", "BL", "RL"} {
		ns.sendList(c.trid, v, l)
	}
}

// sendList sends a list as LST lines.
func (ns *nsSession) sendList(trid, v, which string) {
	es := ns.list(which)
	if len(es) == 0 {
		ns.k.send("LST", trid, which, v, "0", "0")
		return
	}
	for i, e := range es {
		w := []string{"LST", trid, which, v, strconv.Itoa(i + 1), strconv.Itoa(len(es)), e.handle, encode(e.name)}
		if which == "FL" && ns.ver >= 7 {
			w = append(w, strconv.Itoa(e.group))
		}
		ns.k.send(w...)
	}
}

// sendGroups sends the groups as LSG lines (Other Contacts, 0, first).
func (ns *nsSession) sendGroups(trid, v string) {
	ns.mu.Lock()
	ids := make([]int, 0, len(ns.groups))
	for id := range ns.groups {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = ns.groups[id]
		if id == 0 {
			names[i] = "Other Contacts"
		}
	}
	ns.mu.Unlock()
	for i, id := range ids {
		ns.k.send("LSG", trid, v, strconv.Itoa(i+1), strconv.Itoa(len(ids)), strconv.Itoa(id), encode(names[i]), "0")
	}
}

// lst answers LST: one list.
func (ns *nsSession) lst(c *cmd) {
	which := strings.ToUpper(c.arg(0))
	switch which {
	case "FL", "AL", "BL", "RL":
		ns.sendList(c.trid, ns.verNow(), which)
	default:
		ns.k.sendErr(errBadParam, c.trid)
	}
}

// lsg answers LSG: the groups.
func (ns *nsSession) lsg(c *cmd) {
	if ns.ver < 7 {
		ns.k.sendErr(errSyntax, c.trid)
		return
	}
	ns.sendGroups(c.trid, ns.verNow())
}

// add is ADD: a friend (FL), one allowed (AL), one blocked (BL).
func (ns *nsSession) add(c *cmd) {
	which, handle := strings.ToUpper(c.arg(0)), c.arg(1)
	k := ns.k
	t := ns.s.resolve(handle)
	switch {
	case which != "FL" && which != "AL" && which != "BL":
		k.sendErr(errBadParam, c.trid)
		return
	case !strings.Contains(handle, "@"):
		k.sendErr(errBadHandle, c.trid)
		return
	case t == nil:
		k.sendErr(errNoSuchUser, c.trid)
		return
	case t.ID == ns.u.ID:
		k.sendErr(errBadParam, c.trid)
		return
	}
	h := ns.s.handleOf(t)
	ns.mu.Lock()
	f, isFriend := ns.fl[t.ID]
	blocked := ns.bl[t.ID]
	ns.mu.Unlock()
	switch which {
	case "FL":
		group := ""
		gid := 0
		if ns.ver >= 7 && c.arg(3) != "" {
			var err error
			if gid, err = strconv.Atoi(c.arg(3)); err != nil {
				k.sendErr(errBadGroup, c.trid)
				return
			}
			ns.mu.Lock()
			g, ok := ns.groups[gid]
			ns.mu.Unlock()
			if !ok {
				k.sendErr(errBadGroup, c.trid)
				return
			}
			group = g
		}
		if isFriend && (ns.ver < 7 || f.Group == group) {
			k.sendErr(errAlreadyThere, c.trid)
			return
		}
		nf := store.Friend{ID: t.ID, Name: t.Name, Group: group, Alias: f.Alias}
		ns.mu.Lock()
		ns.fl[t.ID] = nf // before the store tells of it, so it is not told twice
		ns.mu.Unlock()
		if err := ns.s.B.Store.PutFriend(ns.u.ID, nf); err != nil {
			ns.s.B.Log.Printf("node %d: msn: adding a friend: %v", ns.n.Num, err)
			k.sendErr(errInternal, c.trid)
			return
		}
		w := []string{"ADD", c.trid, "FL", ns.bumpVer(), h, encode(t.Name)}
		if ns.ver >= 7 {
			w = append(w, strconv.Itoa(gid))
		}
		k.send(w...)
		ns.s.B.FriendsChanged(ns.u.ID)
		ns.tellNow(c.trid, t)
	case "AL":
		if blocked {
			k.sendErr(errInOpposite, c.trid)
			return
		}
		k.send("ADD", c.trid, "AL", ns.bumpVer(), h, encode(t.Name))
	case "BL":
		if blocked {
			k.sendErr(errAlreadyThere, c.trid)
			return
		}
		ns.mu.Lock()
		ns.bl[t.ID] = true
		ns.mu.Unlock()
		if err := ns.s.B.Store.Block(ns.u.ID, t.ID); err != nil {
			k.sendErr(errInternal, c.trid)
			return
		}
		k.send("ADD", c.trid, "BL", ns.bumpVer(), h, encode(t.Name))
		ns.s.B.FriendsChanged(ns.u.ID)
		ns.s.changed(ns.u)
	}
}

// rem is REM: off a list (or, MSNP7, out of a group).
func (ns *nsSession) rem(c *cmd) {
	if ns.ver >= 10 {
		ns.rem10(c)
		return
	}
	which, handle := strings.ToUpper(c.arg(0)), c.arg(1)
	k := ns.k
	t := ns.s.resolve(handle)
	if which != "FL" && which != "AL" && which != "BL" {
		k.sendErr(errBadParam, c.trid)
		return
	}
	if t == nil {
		k.sendErr(errNotOnList, c.trid)
		return
	}
	h := ns.s.handleOf(t)
	ns.mu.Lock()
	f, isFriend := ns.fl[t.ID]
	blocked := ns.bl[t.ID]
	ns.mu.Unlock()
	switch which {
	case "FL":
		if !isFriend {
			k.sendErr(errNotOnList, c.trid)
			return
		}
		if ns.ver >= 7 && c.arg(2) != "" {
			gid, err := strconv.Atoi(c.arg(2))
			ns.mu.Lock()
			g, ok := ns.groups[gid]
			ns.mu.Unlock()
			if err != nil || !ok {
				k.sendErr(errBadGroup, c.trid)
				return
			}
			if f.Group != g {
				k.sendErr(errNotInGroup, c.trid)
				return
			}
			if g != "" { // out of the group: into Other Contacts
				f.Group = ""
				ns.mu.Lock()
				ns.fl[t.ID] = f
				ns.mu.Unlock()
				ns.s.B.Store.PutFriend(ns.u.ID, f)
				ns.s.B.FriendsChanged(ns.u.ID)
			}
			k.send("REM", c.trid, "FL", ns.bumpVer(), h, c.arg(2))
			return
		}
		ns.mu.Lock()
		delete(ns.fl, t.ID)
		delete(ns.told, t.ID)
		ns.mu.Unlock()
		if err := ns.s.B.Store.RemoveFriend(ns.u.ID, t.ID); err != nil {
			k.sendErr(errInternal, c.trid)
			return
		}
		k.send("REM", c.trid, "FL", ns.bumpVer(), h)
		ns.s.B.FriendsChanged(ns.u.ID)
	case "AL":
		k.send("REM", c.trid, "AL", ns.bumpVer(), h)
	case "BL":
		if !blocked {
			k.sendErr(errNotOnList, c.trid)
			return
		}
		ns.mu.Lock()
		delete(ns.bl, t.ID)
		ns.mu.Unlock()
		ns.s.B.Store.Unblock(ns.u.ID, t.ID)
		k.send("REM", c.trid, "BL", ns.bumpVer(), h)
		ns.s.B.FriendsChanged(ns.u.ID)
		ns.s.changed(ns.u)
	}
}

// rea is REA: its own display name, or one it keeps for a friend (the
// friend's alias on the friends list).
func (ns *nsSession) rea(c *cmd) {
	handle, name := c.arg(0), strings.TrimSpace(decode(c.arg(1)))
	k := ns.k
	if name == "" || len(name) > 129 {
		k.sendErr(errBadFriendly, c.trid)
		return
	}
	t := ns.s.resolve(handle)
	if t == nil {
		k.sendErr(errNotOnList, c.trid)
		return
	}
	if t.ID == ns.u.ID {
		ns.mu.Lock()
		ns.friendly = name
		ns.mu.Unlock()
		k.send("REA", c.trid, ns.bumpVer(), handle, encode(name))
		ns.s.renamed(ns.u)
		return
	}
	ns.mu.Lock()
	f, ok := ns.fl[t.ID]
	if ok {
		f.Alias = name
		if name == t.Name {
			f.Alias = ""
		}
		ns.fl[t.ID] = f
	}
	ns.mu.Unlock()
	if !ok {
		k.sendErr(errNotOnList, c.trid)
		return
	}
	ns.s.B.Store.PutFriend(ns.u.ID, f)
	k.send("REA", c.trid, ns.bumpVer(), handle, encode(name))
}

// group is ADG, RMG and REG (MSNP7): groups made, removed, renamed.  A
// group is its friends': one with none is forgotten at the next sign-in.
func (ns *nsSession) group(c *cmd) {
	k := ns.k
	if ns.ver < 7 {
		k.sendErr(errSyntax, c.trid)
		return
	}
	if ns.ver >= 10 {
		ns.group10(c)
		return
	}
	switch c.name {
	case "ADG":
		name := strings.TrimSpace(decode(c.arg(0)))
		if name == "" || len(name) > 61 || strings.EqualFold(name, "Other Contacts") {
			k.sendErr(errGroupName, c.trid)
			return
		}
		ns.mu.Lock()
		for _, g := range ns.groups {
			if strings.EqualFold(g, name) {
				ns.mu.Unlock()
				k.sendErr(errGroupName, c.trid)
				return
			}
		}
		n := len(ns.groups)
		id := ns.groupIDLocked(name)
		ns.mu.Unlock()
		if n >= maxGroups || id == 0 {
			k.sendErr(errListFull, c.trid)
			return
		}
		k.send("ADG", c.trid, ns.bumpVer(), encode(name), strconv.Itoa(id), "0")
	case "RMG", "REG":
		id, err := strconv.Atoi(c.arg(0))
		ns.mu.Lock()
		_, ok := ns.groups[id]
		ns.mu.Unlock()
		if err != nil || !ok || id == 0 {
			k.sendErr(errBadGroup, c.trid)
			return
		}
		if code := ns.regroup(c.name, id, c.arg(1)); code != 0 {
			k.sendErr(code, c.trid)
			return
		}
		if c.name == "RMG" {
			k.send("RMG", c.trid, ns.bumpVer(), strconv.Itoa(id))
		} else {
			k.send("REG", c.trid, ns.bumpVer(), strconv.Itoa(id), c.arg(1), "0")
		}
	}
}

// regroup removes a group (RMG: its friends into none) or renames it
// (REG: to, URL-encoded); an error's number, or 0.
func (ns *nsSession) regroup(op string, id int, to string) int {
	to = strings.TrimSpace(decode(to))
	if op == "REG" && (to == "" || len(to) > 61) {
		return errGroupName
	}
	if op == "RMG" {
		to = ""
	}
	ns.mu.Lock()
	old := ns.groups[id]
	var moved []store.Friend
	for fid, f := range ns.fl {
		if f.Group == old {
			f.Group = to
			ns.fl[fid] = f
			moved = append(moved, f)
		}
	}
	if op == "RMG" {
		delete(ns.groups, id)
		delete(ns.gguid, id)
	} else {
		ns.groups[id] = to
	}
	ns.mu.Unlock()
	for _, f := range moved {
		ns.s.B.Store.PutFriend(ns.u.ID, f)
	}
	if len(moved) > 0 {
		ns.s.B.FriendsChanged(ns.u.ID)
	}
	return 0
}

// syncLists has the client told of changes made to its friends and
// blocked lists elsewhere (by AIM, IRC, the BBS).
func (ns *nsSession) syncLists() {
	ns.mu.Lock()
	synced := ns.synced
	oldFL, oldBL := ns.fl, ns.bl
	ns.mu.Unlock()
	if !synced {
		return
	}
	if err := ns.loadLists(); err != nil {
		return
	}
	ns.mu.Lock()
	newFL, newBL := ns.fl, ns.bl
	ns.mu.Unlock()
	for id, f := range newFL {
		if _, ok := oldFL[id]; !ok {
			if u, err := ns.s.B.Store.UserByID(id); err == nil {
				ns.asyncAdd("FL", u, f.Group)
				ns.contactChanged(u)
			}
		}
	}
	for id := range oldFL {
		if _, ok := newFL[id]; !ok {
			if u, err := ns.s.B.Store.UserByID(id); err == nil {
				ns.asyncRem("FL", u)
			}
			ns.mu.Lock()
			delete(ns.told, id)
			ns.mu.Unlock()
		}
	}
	for id := range newBL {
		if !oldBL[id] {
			if u, err := ns.s.B.Store.UserByID(id); err == nil {
				ns.asyncAdd("BL", u, "")
			}
		}
	}
	for id := range oldBL {
		if !newBL[id] {
			if u, err := ns.s.B.Store.UserByID(id); err == nil {
				ns.asyncRem("BL", u)
			}
		}
	}
}

// reverseChanged: another user's lists changed: whether they have this
// one as a friend (its RL), and what it sees of them.
func (ns *nsSession) reverseChanged(u *store.User) {
	ns.mu.Lock()
	synced := ns.synced
	had := ns.rl[u.ID]
	ns.mu.Unlock()
	if synced {
		has, err := ns.s.B.Store.IsFriend(u.ID, ns.u.ID)
		if err == nil && has != had {
			ns.mu.Lock()
			if has {
				ns.rl[u.ID] = true
			} else {
				delete(ns.rl, u.ID)
			}
			ns.mu.Unlock()
			if has {
				ns.asyncAdd("RL", u, "")
			} else {
				ns.asyncRem("RL", u)
			}
		}
	}
	ns.contactChanged(u)
}
