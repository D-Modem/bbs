package oscar

import (
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

// feedbag is family 0x13: the buddy list kept on the server (SSI).
func (ss *session) feedbag(h snacHeader, r *rd) {
	st := ss.s.B.Store
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
		items, err := st.Feedbag(ss.u.ID)
		if err != nil {
			ss.s.B.Log.Printf("node %d: the feedbag: %v", ss.n.Num, err)
			ss.fail(0x0013, h.req, 0x0005)
			return
		}
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

// changeItems adds, updates or deletes feedbag items: each one's status.
func (ss *session) changeItems(sub uint16, items []store.FeedbagItem) []uint16 {
	st := ss.s.B.Store
	have, err := st.Feedbag(ss.u.ID)
	if err != nil {
		ss.s.B.Log.Printf("node %d: the feedbag: %v", ss.n.Num, err)
		codes := make([]uint16, len(items))
		for i := range codes {
			codes[i] = fbInvalid
		}
		return codes
	}
	exists := map[[2]uint16]bool{}
	for _, it := range have {
		exists[[2]uint16{it.GroupID, it.ItemID}] = true
	}
	codes := make([]uint16, len(items))
	var put, del []store.FeedbagItem
	for i, it := range items {
		k := [2]uint16{it.GroupID, it.ItemID}
		switch {
		case sub == 0x0008 && exists[k]:
			codes[i] = fbExists
		case sub == 0x0008 && len(have)+len(put) >= maxFeedbag:
			codes[i] = 0x000c // over the limit
		case sub != 0x0008 && !exists[k]:
			codes[i] = fbNotFound
		case sub == 0x000a:
			del = append(del, it)
			delete(exists, k)
		default:
			put = append(put, it)
			exists[k] = true
		}
	}
	if len(put) > 0 {
		err = st.PutFeedbag(ss.u.ID, put)
	}
	if err == nil && len(del) > 0 {
		err = st.DeleteFeedbag(ss.u.ID, del)
	}
	if err != nil {
		ss.s.B.Log.Printf("node %d: the feedbag: %v", ss.n.Num, err)
		for i := range codes {
			codes[i] = fbInvalid
		}
		return codes
	}
	if now, err := st.Feedbag(ss.u.ID); err == nil {
		ss.feedbagLoaded(now)
	}
	// the user's other sessions are told of the change
	for _, o := range ss.s.sessionsOf(ss.u.ID) {
		if o == ss {
			continue
		}
		var b buf
		for _, it := range items {
			itemBytes(&b, it)
		}
		o.do(func() {
			o.snac(0x0013, sub, 0, b)
			if now, err := st.Feedbag(o.u.ID); err == nil {
				o.feedbagLoaded(now)
			}
		})
	}
	ss.s.changed(ss.u)
	return codes
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
