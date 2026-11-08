package oscar

import (
	"encoding/binary"
	"sort"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// feedbag classes
const (
	fbBuddy  = 0x0000
	fbGroup  = 0x0001
	fbPermit = 0x0002
	fbDeny   = 0x0003
	fbPDInfo = 0x0004
)

// feedbag status codes
const (
	fbOK       = 0x0000
	fbNotFound = 0x0002
	fbExists   = 0x0003
	fbInvalid  = 0x000a
)

// maxFeedbag items a user keeps
const maxFeedbag = 1000

// readItems reads feedbag items to the end of a SNAC.
func readItems(r *rd) []store.FeedbagItem {
	var out []store.FeedbagItem
	for r.left() > 0 {
		var it store.FeedbagItem
		it.Name = r.str16()
		it.GroupID = r.u16()
		it.ItemID = r.u16()
		it.ClassID = r.u16()
		it.Attrs = append([]byte(nil), r.take(int(r.u16()))...)
		if r.bad {
			break
		}
		out = append(out, it)
	}
	return out
}

// itemBytes is an item as SNACs carry it.
func itemBytes(b *buf, it store.FeedbagItem) {
	b.str16(it.Name)
	b.u16(it.GroupID)
	b.u16(it.ItemID)
	b.u16(it.ClassID)
	b.u16(uint16(len(it.Attrs)))
	b.raw(it.Attrs)
}

// feedbag is family 0x13: the buddy list kept on the server (SSI).  Its
// buddies are the user's friends list, kept by the BBS: BBS users alone;
// its groups, permit and deny lists and preferences are kept as the
// client sent them.
func (ss *session) feedbag(h snacHeader, r *rd) {
	switch h.sub {
	case 0x0002: // rights
		var t tlvs
		var per buf
		for c := 0; c <= 0x18; c++ {
			switch c {
			case fbBuddy, fbGroup, fbPermit, fbDeny:
				per.u16(400)
			case fbPDInfo, 0x0005, 0x0011, 0x0013:
				per.u16(1)
			case 0x0009:
				per.u16(3)
			default:
				per.u16(100)
			}
		}
		t.addU16(0x0002, 200)
		t.addU16(0x0003, 200)
		t.add(0x0004, per)
		t.addU16(0x0005, 200)
		t.addU16(0x0006, 200)
		t.addU16(0x0007, 200)
		t.addU16(0x0008, 200)
		t.addU16(0x0009, 200)
		t.addU16(0x000a, 200)
		t.addU16(0x000c, 200)
		ss.snac(0x0013, 0x0003, h.req, t.bytes())
	case 0x0004, 0x0005: // the list (5: if changed since; it is sent anyway)
		items, err := ss.bag()
		if err != nil {
			ss.s.B.Log.Printf("node %d: the feedbag: %v", ss.n.Num, err)
			ss.fail(0x0013, h.req, 0x0005)
			return
		}
		ss.mu.Lock()
		ss.items, ss.bagSent = items, true
		ss.mu.Unlock()
		ss.feedbagLoaded(items)
		// a long list goes in pieces, all but the last flagged as such
		const per = 150
		for start := 0; ; start += per {
			end := min(start+per, len(items))
			var b buf
			b.u8(0)
			b.u16(uint16(end - start))
			for _, it := range items[start:end] {
				itemBytes(&b, it)
			}
			b.u32(uint32(time.Now().Unix()))
			flags := uint16(0)
			if end < len(items) {
				flags = 0x0001
			}
			if err := ss.fc.snac(0x0013, 0x0006, flags, h.req, b); err != nil {
				ss.close()
				return
			}
			if end >= len(items) {
				break
			}
		}
	case 0x0007: // the list is in use
		ss.mu.Lock()
		ss.bagOn = true
		ss.mu.Unlock()
		ss.do(ss.rewatch)
	case 0x0008, 0x0009, 0x000a: // insert, update, delete
		items := readItems(r)
		codes := ss.changeItems(h.sub, items)
		var b buf
		for _, c := range codes {
			b.u16(c)
		}
		ss.snac(0x0013, 0x000e, h.req, b)
	case 0x0011, 0x0012: // a change's start and end
	case 0x0014: // grant future authorization: all are
	case 0x0018: // authorization request (ICQ): granted at once, here
		sn := r.str8()
		if u := ss.s.resolve(sn); u != nil {
			var b buf
			b.str8(sn)
			b.u8(1)  // granted
			b.u16(0) // no reason
			ss.snac(0x0013, 0x001b, 0, b)
		}
	case 0x001a: // authorization reply: nothing waits on it
	default:
		ss.fail(0x0013, h.req, 0x0008)
	}
}

// friendsGroup is the group friends go in when they have none of their
// own (added elsewhere: by IRC, the BBS), made if the list has none.
const friendsGroup = "Friends"

// bag is the user's feedbag as the client is to have it: the items it
// keeps, and the friends list as its buddies.  Friends not yet in the
// list (added elsewhere), or moved to another group elsewhere, are put
// in their group first, made if need be.
func (ss *session) bag() ([]store.FeedbagItem, error) {
	ss.s.bagMu.Lock()
	defer ss.s.bagMu.Unlock()
	st := ss.s.B.Store
	stored, err := st.Feedbag(ss.u.ID)
	if err != nil {
		return nil, err
	}
	var items, old []store.FeedbagItem // old: buddies kept as items before the friends list was
	for _, it := range stored {
		if it.ClassID == fbBuddy {
			old = append(old, it)
		} else {
			items = append(items, it)
		}
	}
	groups := map[uint16]int{} // group id → its item's index
	for i, it := range items {
		if it.ClassID == fbGroup {
			groups[it.GroupID] = i
		}
	}
	groupName := func(gid uint16) string {
		if i, ok := groups[gid]; ok && gid != 0 {
			return items[i].Name
		}
		return ""
	}
	if len(old) > 0 {
		for _, it := range old {
			u := ss.s.resolve(it.Name)
			if u == nil || u.ID == ss.u.ID {
				continue
			}
			if ok, err := st.IsFriend(ss.u.ID, u.ID); err != nil {
				return nil, err
			} else if !ok {
				if err := st.PutFriend(ss.u.ID, store.Friend{ID: u.ID, Group: groupName(it.GroupID), Alias: alias(it.Attrs)}); err != nil {
					return nil, err
				}
				if err := st.PutOSCARBuddy(ss.u.ID, store.OSCARBuddy{FriendID: u.ID, SN: it.Name, GroupID: it.GroupID,
					ItemID: it.ItemID, Attrs: it.Attrs}); err != nil {
					return nil, err
				}
			}
		}
		if err := st.DeleteFeedbag(ss.u.ID, old); err != nil {
			return nil, err
		}
	}
	fs, err := st.Friends(ss.u.ID)
	if err != nil {
		return nil, err
	}
	places, err := st.OSCARBuddies(ss.u.ID)
	if err != nil {
		return nil, err
	}
	used := map[[2]uint16]bool{}
	for _, it := range items {
		used[[2]uint16{it.GroupID, it.ItemID}] = true
	}
	// friends with a place in their group keep it
	var unplaced []store.Friend
	for _, f := range fs {
		p, ok := places[f.ID]
		k := [2]uint16{p.GroupID, p.ItemID}
		_, group := groups[p.GroupID]
		if ok && group && p.GroupID != 0 && p.ItemID != 0 && !used[k] &&
			(f.Group == "" || strings.EqualFold(groupName(p.GroupID), f.Group)) {
			used[k] = true
		} else {
			unplaced = append(unplaced, f)
		}
	}
	changed := map[uint16]bool{} // groups whose order changed
	for _, f := range unplaced {
		name := f.Group
		if name == "" {
			name = friendsGroup
		}
		gid := uint16(0)
		for id := range groups {
			if id != 0 && strings.EqualFold(groupName(id), name) {
				gid = id
			}
		}
		if gid == 0 {
			gid = freeID(func(id uint16) bool { _, ok := groups[id]; return ok })
			items = append(items, store.FeedbagItem{Name: name, GroupID: gid, ClassID: fbGroup})
			groups[gid] = len(items) - 1
			if _, ok := groups[0]; !ok { // the root, which lists the groups
				items = append(items, store.FeedbagItem{ClassID: fbGroup})
				groups[0] = len(items) - 1
			}
			root := &items[groups[0]]
			root.Attrs = withChild(root.Attrs, gid)
			changed[0] = true
		}
		iid := freeID(func(id uint16) bool { return used[[2]uint16{gid, id}] })
		used[[2]uint16{gid, iid}] = true
		g := &items[groups[gid]]
		g.Attrs = withChild(g.Attrs, iid)
		changed[gid] = true
		p := places[f.ID]
		p.FriendID, p.GroupID, p.ItemID = f.ID, gid, iid
		if err := st.PutOSCARBuddy(ss.u.ID, p); err != nil {
			return nil, err
		}
		places[f.ID] = p
	}
	if len(changed) > 0 {
		var put []store.FeedbagItem
		for gid := range changed {
			put = append(put, items[groups[gid]])
		}
		if err := st.PutFeedbag(ss.u.ID, put); err != nil {
			return nil, err
		}
	}
	for _, f := range fs {
		p := places[f.ID]
		items = append(items, store.FeedbagItem{Name: ss.friendName(f, p.SN), GroupID: p.GroupID, ItemID: p.ItemID,
			ClassID: fbBuddy, Attrs: withAlias(p.Attrs, f.Alias)})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].GroupID != items[j].GroupID {
			return items[i].GroupID < items[j].GroupID
		}
		return items[i].ItemID < items[j].ItemID
	})
	return items, nil
}

// friendName is a friend as the client names them: as it wrote them (sn),
// or (added elsewhere, or renamed since) their ICQ number on ICQ, their
// name on AIM.
func (ss *session) friendName(f store.Friend, sn string) string {
	if sn != "" && isUIN(sn) == ss.icq && (isUIN(sn) || ident(sn) == ident(f.Name)) {
		return sn
	}
	if ss.icq {
		return ss.s.uinOf(f.ID)
	}
	return f.Name
}

// alias is a buddy's alias (TLV 0x131), "" if none.
func alias(attrs []byte) string { return parseTLVs(attrs).str(0x0131) }

// withAlias is a buddy's TLVs with its alias as the friends list has it.
func withAlias(attrs []byte, a string) []byte {
	if alias(attrs) == a {
		return attrs
	}
	var out tlvs
	for _, x := range parseTLVs(attrs) {
		if x.t != 0x0131 {
			out = append(out, x)
		}
	}
	if a != "" {
		out.addStr(0x0131, a)
	}
	return out.bytes()
}

// freeID is the least id from 1 that taken says is not.
func freeID(taken func(uint16) bool) uint16 {
	for id := uint16(1); id < 0x7fff; id++ {
		if !taken(id) {
			return id
		}
	}
	return 0x7fff
}

// withChild is a group's attributes with an item id added to its order
// (TLV 0xC8, the ids it holds).
func withChild(attrs []byte, id uint16) []byte {
	var out tlvs
	found := false
	for _, x := range parseTLVs(attrs) {
		if x.t == 0x00c8 && !found {
			found = true
			if !holds(x.v, id) {
				x = tlv{0x00c8, binary.BigEndian.AppendUint16(append([]byte(nil), x.v...), id)}
			}
		}
		out = append(out, x)
	}
	if !found {
		out = append(out, tlv{0x00c8, binary.BigEndian.AppendUint16(nil, id)})
	}
	return out.bytes()
}

// holds reports whether a list of ids has id.
func holds(ids []byte, id uint16) bool {
	for i := 0; i+1 < len(ids); i += 2 {
		if binary.BigEndian.Uint16(ids[i:]) == id {
			return true
		}
	}
	return false
}

// changeItems adds, updates or deletes feedbag items: each one's status.
// A buddy is a friend: one not a BBS user is refused.
func (ss *session) changeItems(sub uint16, items []store.FeedbagItem) []uint16 {
	st := ss.s.B.Store
	codes := make([]uint16, len(items))
	fail := func(err error) []uint16 {
		ss.s.B.Log.Printf("node %d: the feedbag: %v", ss.n.Num, err)
		for i := range codes {
			codes[i] = fbInvalid
		}
		return codes
	}
	have, err := ss.bag()
	if err != nil {
		return fail(err)
	}
	places, err := st.OSCARBuddies(ss.u.ID)
	if err != nil {
		return fail(err)
	}
	exists := map[[2]uint16]store.FeedbagItem{}
	for _, it := range have {
		exists[[2]uint16{it.GroupID, it.ItemID}] = it
	}
	friendAt := map[[2]uint16]int64{}
	friend := map[int64][2]uint16{}
	for id, p := range places {
		friendAt[[2]uint16{p.GroupID, p.ItemID}] = id
		friend[id] = [2]uint16{p.GroupID, p.ItemID}
	}
	groupName := func(gid uint16) string {
		if g, ok := exists[[2]uint16{gid, 0}]; ok && g.ClassID == fbGroup && gid != 0 {
			return g.Name
		}
		return ""
	}
	var put, del []store.FeedbagItem
	friendsChanged := false
	for i, it := range items {
		k := [2]uint16{it.GroupID, it.ItemID}
		was, ok := exists[k]
		buddy := it.ClassID == fbBuddy || ok && was.ClassID == fbBuddy
		switch {
		case sub == 0x0008 && ok:
			codes[i] = fbExists
		case sub == 0x0008 && len(have)+len(put) >= maxFeedbag:
			codes[i] = 0x000c // over the limit
		case sub != 0x0008 && !ok:
			codes[i] = fbNotFound
		case buddy && sub == 0x000a:
			if id, ok := friendAt[k]; ok {
				if err := st.RemoveFriend(ss.u.ID, id); err != nil {
					return fail(err)
				}
				delete(friend, id)
				friendsChanged = true
			}
			delete(exists, k)
		case buddy:
			u := ss.s.resolve(it.Name)
			if u == nil || u.ID == ss.u.ID || it.ClassID != fbBuddy {
				codes[i] = fbInvalid // not a BBS user: no buddy
				continue
			}
			if at, ok := friend[u.ID]; ok && at != k {
				codes[i] = fbExists // on the list already, elsewhere
				continue
			}
			if id, ok := friendAt[k]; ok && id != u.ID {
				if err := st.RemoveFriend(ss.u.ID, id); err != nil {
					return fail(err)
				}
				delete(friend, id)
			}
			if err := st.PutFriend(ss.u.ID, store.Friend{ID: u.ID, Group: groupName(it.GroupID), Alias: alias(it.Attrs)}); err != nil {
				return fail(err)
			}
			if err := st.PutOSCARBuddy(ss.u.ID, store.OSCARBuddy{FriendID: u.ID, SN: it.Name, GroupID: it.GroupID,
				ItemID: it.ItemID, Attrs: it.Attrs}); err != nil {
				return fail(err)
			}
			friend[u.ID], friendAt[k] = k, u.ID
			exists[k] = it
			friendsChanged = true
		case sub == 0x000a:
			del = append(del, it)
			delete(exists, k)
		default:
			if sub == 0x0009 && it.ClassID == fbGroup && it.GroupID != 0 && it.Name != was.Name {
				// a group renamed: its friends' group is
				for id, at := range friend {
					if at[0] == it.GroupID {
						if f, err := ss.friendOf(id); err == nil {
							f.Group = it.Name
							if err := st.PutFriend(ss.u.ID, f); err != nil {
								return fail(err)
							}
							friendsChanged = true
						}
					}
				}
			}
			put = append(put, it)
			exists[k] = it
		}
	}
	if len(put) > 0 {
		err = st.PutFeedbag(ss.u.ID, put)
	}
	if err == nil && len(del) > 0 {
		err = st.DeleteFeedbag(ss.u.ID, del)
	}
	if err != nil {
		return fail(err)
	}
	if now, err := ss.bag(); err == nil {
		ss.mu.Lock()
		ss.items = now
		ss.mu.Unlock()
		ss.feedbagLoaded(now)
	}
	if friendsChanged {
		ss.s.B.FriendsChanged(ss.u.ID) // the user's other sessions, and the rest of the BBS
	} else {
		ss.s.friendsChanged(ss.u.ID)
	}
	ss.s.changed(ss.u)
	return codes
}

// friendOf is the entry of one on the user's friends list.
func (ss *session) friendOf(id int64) (store.Friend, error) {
	fs, err := ss.s.B.Store.Friends(ss.u.ID)
	if err != nil {
		return store.Friend{}, err
	}
	for _, f := range fs {
		if f.ID == id {
			return f, nil
		}
	}
	return store.Friend{}, store.ErrNotFound
}

// friendsChanged has each session of the user bring its client's buddy
// list up to date.
func (s *Server) friendsChanged(user int64) {
	for _, o := range s.all() {
		if o.u.ID == user {
			o.do(o.syncBag)
		}
	}
}

// syncBag sends the client what changed in its feedbag since it last had
// it (deleted, added, then updated items), and watches its buddies anew.
func (ss *session) syncBag() {
	now, err := ss.bag()
	if err != nil {
		ss.s.B.Log.Printf("node %d: the feedbag: %v", ss.n.Num, err)
		return
	}
	ss.mu.Lock()
	old, sent := ss.items, ss.bagSent
	if sent {
		ss.items = now
	}
	ss.mu.Unlock()
	if sent {
		key := func(it store.FeedbagItem) [2]uint16 { return [2]uint16{it.GroupID, it.ItemID} }
		was := map[[2]uint16]store.FeedbagItem{}
		for _, it := range old {
			was[key(it)] = it
		}
		is := map[[2]uint16]bool{}
		var add, upd, del buf
		for _, it := range now {
			is[key(it)] = true
			o, ok := was[key(it)]
			switch {
			case !ok:
				itemBytes(&add, it)
			case o.Name != it.Name || o.ClassID != it.ClassID || string(o.Attrs) != string(it.Attrs):
				itemBytes(&upd, it)
			}
		}
		for _, it := range old {
			if !is[key(it)] {
				itemBytes(&del, it)
			}
		}
		if len(del)+len(add)+len(upd) > 0 {
			ss.snac(0x0013, 0x0011, 0, nil)
			for _, x := range []struct {
				sub uint16
				b   buf
			}{{0x000a, del}, {0x0008, add}, {0x0009, upd}} {
				if len(x.b) > 0 {
					ss.snac(0x0013, x.sub, 0, x.b)
				}
			}
			ss.snac(0x0013, 0x0012, 0, nil)
		}
		ss.feedbagLoaded(now)
		return
	}
	ss.rewatch() // a client with its own list: the friends watched
}

// feedbagLoaded takes the buddies and privacy of the list, and watches
// the buddies.
func (ss *session) feedbagLoaded(items []store.FeedbagItem) {
	bagged := map[string]string{}
	permit, deny := map[string]bool{}, map[string]bool{}
	mode := byte(0)
	for _, it := range items {
		switch it.ClassID {
		case fbBuddy:
			if it.Name != "" {
				bagged[ident(it.Name)] = it.Name
			}
		case fbPermit:
			permit[ident(it.Name)] = true
		case fbDeny:
			deny[ident(it.Name)] = true
		case fbPDInfo:
			if v, ok := parseTLVs(it.Attrs).get(0x00ca); ok && len(v) >= 1 {
				mode = v[0]
			}
		}
	}
	ss.mu.Lock()
	ss.bagged = bagged
	if mode != 0 || len(permit) > 0 || len(deny) > 0 {
		ss.permit, ss.deny = permit, deny
		if mode == 0 {
			mode = pdPermitAll
		}
		ss.pdMode = mode
	}
	ss.mu.Unlock()
	ss.do(ss.rewatch)
}
