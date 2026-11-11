package msn

import (
	"crypto/md5"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// The lists since MSNP8: SYN gives each contact once, its lists as bits
// (FL 1, AL 2, BL 4, RL 8); since MSNP10 contacts and groups are named by
// GUIDs (ABCHMigrated) and ADC replaces ADD.

const (
	bitFL = 1
	bitAL = 2
	bitBL = 4
	bitRL = 8
)

// contactGUID is a contact's GUID: made of their account's number.
func contactGUID(id int64) string { return fmt.Sprintf("00000000-0000-0000-0000-%012x", id) }

// contactOfGUID is the account a contact GUID is made of (0: none).
func contactOfGUID(g string) int64 {
	rest, ok := strings.CutPrefix(strings.ToLower(g), "00000000-0000-0000-0000-")
	if !ok || len(rest) != 12 {
		return 0
	}
	id, err := strconv.ParseInt(rest, 16, 64)
	if err != nil {
		return 0
	}
	return id
}

// nameGUID is a GUID made of a group's name.
func nameGUID(name string) string {
	h := md5.Sum([]byte(strings.ToLower(name)))
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

// groupGUIDLocked is a group's GUID (kept for the session, so a renamed
// group keeps its own).  ns.mu is held.
func (ns *nsSession) groupGUIDLocked(id int) string {
	if g, ok := ns.gguid[id]; ok {
		return g
	}
	g := nameGUID(ns.groups[id])
	ns.gguid[id] = g
	return g
}

// groupOfGUIDLocked is the group a GUID names.  ns.mu is held.
func (ns *nsSession) groupOfGUIDLocked(g string) (int, bool) {
	for id := range ns.groups {
		if id != 0 && strings.EqualFold(ns.groupGUIDLocked(id), g) {
			return id, true
		}
	}
	return 0, false
}

// contact is one on any of the lists, with the lists as bits.
type contact struct {
	id     int64
	handle string
	name   string // as stored for them (an alias), or their name
	bits   int
	group  string
}

// contacts are everyone on the lists, by handle.
func (ns *nsSession) contacts() []contact {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	names := ns.namesLocked()
	byID := map[int64]*contact{}
	get := func(id int64) *contact {
		c := byID[id]
		if c == nil {
			c = &contact{id: id, handle: ns.s.handleOfName(names[id]), name: names[id]}
			byID[id] = c
		}
		return c
	}
	for id, f := range ns.fl {
		c := get(id)
		c.bits |= bitFL
		c.group = f.Group
		if f.Alias != "" {
			c.name = f.Alias
		}
		if !ns.bl[id] {
			c.bits |= bitAL
		}
	}
	for id := range ns.rl {
		c := get(id)
		c.bits |= bitRL
		if !ns.bl[id] {
			c.bits |= bitAL
		}
	}
	for id := range ns.bl {
		get(id).bits |= bitBL
	}
	out := make([]contact, 0, len(byID))
	for _, c := range byID {
		if c.name != "" {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].handle < out[j].handle })
	return out
}

// listStamp is a list or settings version since MSNP10: a time.
func listStamp() string { return time.Now().Format("2006-01-02T15:04:05.0000000-07:00") }

// syn8 is SYN for MSNP8 and later: the settings, the groups, and each
// contact once.
func (ns *nsSession) syn8(c *cmd) {
	cs := ns.contacts()
	ns.mu.Lock()
	ns.synced = true
	gtc, blp := ns.gtc, ns.blp
	ids := make([]int, 0, len(ns.groups))
	for id := range ns.groups {
		if id != 0 || ns.ver < 10 { // since MSNP10, no group is no group
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	groups := make([][2]string, len(ids)) // its id (or GUID), its name
	for i, id := range ids {
		name := ns.groups[id]
		if id == 0 {
			name = "Other Contacts"
		}
		gid := strconv.Itoa(id)
		if ns.ver >= 10 {
			gid = ns.groupGUIDLocked(id)
		}
		groups[i] = [2]string{gid, encode(name)}
	}
	groupOf := map[string]string{}
	for _, c := range cs {
		if c.bits&bitFL != 0 {
			if ns.ver >= 10 {
				if c.group != "" {
					groupOf[c.group] = ns.groupGUIDLocked(ns.groupIDLocked(c.group))
				}
			} else {
				groupOf[c.group] = strconv.Itoa(ns.groupIDLocked(c.group))
			}
		}
	}
	friendly := ns.friendly
	ns.mu.Unlock()
	n, g := strconv.Itoa(len(cs)), strconv.Itoa(len(groups))
	k := ns.k
	if ns.ver >= 10 {
		v := listStamp()
		k.send("SYN", c.trid, v, v, n, g)
	} else {
		have, _ := strconv.Atoi(c.arg(0))
		ns.mu.Lock()
		if ns.listVer <= have {
			ns.listVer = have + 1
		}
		ns.mu.Unlock()
		k.send("SYN", c.trid, ns.verNow(), n, g)
	}
	k.send("GTC", gtc)
	k.send("BLP", blp)
	if ns.ver >= 10 {
		k.send("PRP", "MFN", encode(friendly))
	}
	for _, gr := range groups {
		if ns.ver >= 10 {
			k.send("LSG", gr[1], gr[0])
		} else {
			k.send("LSG", gr[0], gr[1], "0")
		}
	}
	for _, ct := range cs {
		var w []string
		if ns.ver >= 10 {
			w = []string{"LST", "N=" + ct.handle, "F=" + encode(ct.name)}
			if ct.bits&bitFL != 0 {
				w = append(w, "C="+contactGUID(ct.id))
			}
			w = append(w, strconv.Itoa(ct.bits))
			if ns.ver >= 12 {
				w = append(w, "1") // its network: MSN
			}
			if ct.bits&bitFL != 0 && ct.group != "" {
				w = append(w, groupOf[ct.group])
			}
		} else {
			w = []string{"LST", ct.handle, encode(ct.name), strconv.Itoa(ct.bits)}
			if ct.bits&bitFL != 0 {
				w = append(w, groupOf[ct.group])
			}
		}
		k.send(w...)
	}
}

// versioned is a reply's words with the list version after the
// transaction id, before MSNP10 (which has none).
func (ns *nsSession) versioned(name, trid string, rest ...string) []string {
	w := []string{name, trid}
	if ns.ver < 10 {
		w = append(w, ns.bumpVer())
	}
	return append(w, rest...)
}

// asyncAdd tells the client that u was put on a list elsewhere.
func (ns *nsSession) asyncAdd(list string, u *store.User, group string) {
	h, name := ns.s.handleOf(u), encode(u.Name)
	if ns.ver < 10 {
		w := []string{"ADD", "0", list, ns.bumpVer(), h, name}
		if list == "FL" && ns.ver >= 7 {
			ns.mu.Lock()
			w = append(w, strconv.Itoa(ns.groupIDLocked(group)))
			ns.mu.Unlock()
		}
		ns.k.send(w...)
		return
	}
	switch list {
	case "FL":
		ns.k.send("ADC", "0", "FL", "N="+h, "F="+name, "C="+contactGUID(u.ID))
		if group != "" {
			ns.mu.Lock()
			g := ns.groupGUIDLocked(ns.groupIDLocked(group))
			ns.mu.Unlock()
			ns.k.send("ADC", "0", "FL", "C="+contactGUID(u.ID), g)
		}
	case "RL":
		ns.k.send("ADC", "0", "RL", "N="+h, "F="+name)
	default:
		ns.k.send("ADC", "0", list, "N="+h)
	}
}

// asyncRem tells the client that u was taken off a list elsewhere.
func (ns *nsSession) asyncRem(list string, u *store.User) {
	switch {
	case ns.ver < 10:
		ns.k.send("REM", "0", list, ns.bumpVer(), ns.s.handleOf(u))
	case list == "FL":
		ns.k.send("REM", "0", "FL", contactGUID(u.ID))
	default:
		ns.k.send("REM", "0", list, ns.s.handleOf(u))
	}
}

// field is a "K=value" argument's value ("" if none has the prefix).
func field(args []string, k string) string {
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, k+"="); ok {
			return v
		}
	}
	return ""
}

// adc is ADC (MSNP10): onto a list (N=handle), or a friend into a group
// (C=GUID and a group's GUID).
func (ns *nsSession) adc(c *cmd) {
	k := ns.k
	which := strings.ToUpper(c.arg(0))
	if cg := field(c.args, "C"); which == "FL" && cg != "" && field(c.args, "N") == "" {
		ns.intoGroup(c, cg, c.arg(2))
		return
	}
	handle := field(c.args, "N")
	t := ns.s.resolve(handle)
	switch {
	case which != "FL" && which != "AL" && which != "BL" && which != "RL":
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
	_, isFriend := ns.fl[t.ID]
	blocked := ns.bl[t.ID]
	ns.mu.Unlock()
	switch which {
	case "FL":
		if isFriend {
			k.sendErr(errAlreadyThere, c.trid)
			return
		}
		nf := store.Friend{ID: t.ID, Name: t.Name}
		ns.mu.Lock()
		ns.fl[t.ID] = nf
		ns.mu.Unlock()
		if err := ns.s.B.Store.PutFriend(ns.u.ID, nf); err != nil {
			ns.s.B.Log.Printf("node %d: msn: adding a friend: %v", ns.n.Num, err)
			k.sendErr(errInternal, c.trid)
			return
		}
		k.send("ADC", c.trid, "FL", "N="+h, "F="+encode(t.Name), "C="+contactGUID(t.ID))
		ns.s.B.FriendsChanged(ns.u.ID)
		ns.tellNow(c.trid, t)
	case "AL", "RL":
		if which == "AL" && blocked {
			k.sendErr(errInOpposite, c.trid)
			return
		}
		k.send("ADC", c.trid, which, "N="+h)
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
		k.send("ADC", c.trid, "BL", "N="+h)
		ns.s.B.FriendsChanged(ns.u.ID)
		ns.s.changed(ns.u)
	}
}

// intoGroup puts a friend (by GUID) into a group (by GUID): their group
// on the friends list.
func (ns *nsSession) intoGroup(c *cmd, cg, gg string) {
	k := ns.k
	id := contactOfGUID(strings.TrimPrefix(cg, "C="))
	ns.mu.Lock()
	f, ok := ns.fl[id]
	gid, gok := ns.groupOfGUIDLocked(gg)
	name := ns.groups[gid]
	if ok && gok {
		f.Group = name
		ns.fl[id] = f
	}
	ns.mu.Unlock()
	switch {
	case !ok:
		k.sendErr(errNotOnList, c.trid)
		return
	case !gok:
		k.sendErr(errBadGroup, c.trid)
		return
	}
	ns.s.B.Store.PutFriend(ns.u.ID, f)
	k.send("ADC", c.trid, "FL", "C="+contactGUID(id), gg)
	ns.s.B.FriendsChanged(ns.u.ID)
}

// rem10 is REM since MSNP10: a friend by GUID (out of a group, if one is
// named), anyone else by handle.
func (ns *nsSession) rem10(c *cmd) {
	k := ns.k
	which := strings.ToUpper(c.arg(0))
	switch which {
	case "FL":
		id := contactOfGUID(c.arg(1))
		if id == 0 {
			if t := ns.s.resolve(c.arg(1)); t != nil {
				id = t.ID
			}
		}
		ns.mu.Lock()
		f, ok := ns.fl[id]
		ns.mu.Unlock()
		if !ok {
			k.sendErr(errNotOnList, c.trid)
			return
		}
		if gg := c.arg(2); gg != "" {
			ns.mu.Lock()
			gid, gok := ns.groupOfGUIDLocked(gg)
			in := gok && ns.groups[gid] == f.Group
			if in {
				f.Group = ""
				ns.fl[id] = f
			}
			ns.mu.Unlock()
			if !gok {
				k.sendErr(errBadGroup, c.trid)
				return
			}
			if !in {
				k.sendErr(errNotInGroup, c.trid)
				return
			}
			ns.s.B.Store.PutFriend(ns.u.ID, f)
			k.send("REM", c.trid, "FL", c.arg(1), gg)
			ns.s.B.FriendsChanged(ns.u.ID)
			return
		}
		ns.mu.Lock()
		delete(ns.fl, id)
		delete(ns.told, id)
		ns.mu.Unlock()
		if err := ns.s.B.Store.RemoveFriend(ns.u.ID, id); err != nil {
			k.sendErr(errInternal, c.trid)
			return
		}
		k.send("REM", c.trid, "FL", c.arg(1))
		ns.s.B.FriendsChanged(ns.u.ID)
	case "AL", "PL", "RL":
		k.send("REM", c.trid, which, c.arg(1))
	case "BL":
		t := ns.s.resolve(c.arg(1))
		ns.mu.Lock()
		blocked := t != nil && ns.bl[t.ID]
		if blocked {
			delete(ns.bl, t.ID)
		}
		ns.mu.Unlock()
		if !blocked {
			k.sendErr(errNotOnList, c.trid)
			return
		}
		ns.s.B.Store.Unblock(ns.u.ID, t.ID)
		k.send("REM", c.trid, "BL", c.arg(1))
		ns.s.B.FriendsChanged(ns.u.ID)
		ns.s.changed(ns.u)
	default:
		k.sendErr(errBadParam, c.trid)
	}
}

// prp is PRP: its own display name (MFN, MSNP10); phone numbers are
// taken and not kept.
func (ns *nsSession) prp(c *cmd) {
	prop := strings.ToUpper(c.arg(0))
	if prop == "MFN" {
		name := strings.TrimSpace(decode(c.arg(1)))
		if name == "" || len(name) > 387 {
			ns.k.sendErr(errBadFriendly, c.trid)
			return
		}
		ns.mu.Lock()
		ns.friendly = name
		ns.mu.Unlock()
		ns.s.changed(ns.u)
	}
	ns.k.send(ns.versioned("PRP", c.trid, c.args...)...)
}

// sbp is SBP (MSNP10): a friend's stored name (MFN), their alias.
func (ns *nsSession) sbp(c *cmd) {
	id := contactOfGUID(c.arg(0))
	ns.mu.Lock()
	f, ok := ns.fl[id]
	ns.mu.Unlock()
	if !ok {
		ns.k.sendErr(errNotOnList, c.trid)
		return
	}
	if strings.ToUpper(c.arg(1)) == "MFN" {
		name := strings.TrimSpace(decode(c.arg(2)))
		if name == f.Name {
			name = ""
		}
		f.Alias = name
		ns.mu.Lock()
		ns.fl[id] = f
		ns.mu.Unlock()
		ns.s.B.Store.PutFriend(ns.u.ID, f)
	}
	ns.k.send(append([]string{"SBP", c.trid}, c.args...)...)
}

// group10 is ADG, RMG and REG since MSNP10: groups by GUID, no versions.
func (ns *nsSession) group10(c *cmd) {
	k := ns.k
	switch c.name {
	case "ADG":
		name := strings.TrimSpace(decode(c.arg(0)))
		if name == "" || len(name) > 61 {
			k.sendErr(errGroupName, c.trid)
			return
		}
		ns.mu.Lock()
		for id, g := range ns.groups {
			if id != 0 && strings.EqualFold(g, name) {
				ns.mu.Unlock()
				k.sendErr(errGroupName, c.trid)
				return
			}
		}
		id := ns.groupIDLocked(name)
		g := ""
		if id != 0 {
			g = ns.groupGUIDLocked(id)
		}
		ns.mu.Unlock()
		if id == 0 {
			k.sendErr(errListFull, c.trid)
			return
		}
		k.send("ADG", c.trid, encode(name), g)
	case "RMG", "REG":
		ns.mu.Lock()
		id, ok := ns.groupOfGUIDLocked(c.arg(0))
		ns.mu.Unlock()
		if !ok {
			k.sendErr(errBadGroup, c.trid)
			return
		}
		if code := ns.regroup(c.name, id, c.arg(1)); code != 0 {
			k.sendErr(code, c.trid)
			return
		}
		if c.name == "RMG" {
			k.send("RMG", c.trid, c.arg(0))
		} else {
			k.send("REG", c.trid, c.arg(0), c.arg(1))
		}
	}
}
