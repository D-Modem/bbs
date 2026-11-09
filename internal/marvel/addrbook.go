package marvel

import (
	"encoding/binary"
	"sort"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// MOSABP is the MSN address book: Exchange's list of members, the names
// typed into To: resolved, and the Member Properties sheet.  Its members
// are the BBS's users, and with them the caller's own saved addresses
// (store.Addresses, which CompuServe's Address Book shares and the mail
// they send from Exchange adds to).
type addrBook struct{ c *conn }

const abIface = 0x01

// MOSABP methods
const (
	abContainers    = 0x00
	abUserDetails   = 0x02
	abCloseTable    = 0x09
	abUserDetailsH  = 0x0A
	abQueryRows     = 0x0C
	abQueryRestrict = 0x0D
	abQueryMore     = 0x0F
)

// the member tags the sheet and the lists ask for
const (
	tagCity      = 0x6000001E
	tagState     = 0x6001001E
	tagFirstName = 0x600D001E
	tagLastName  = 0x600E001E
	tagInterests = 0x6007001E
)

// abRowMax is the most rows' bytes the client takes, uncompressed.
const abRowMax = 27000

func (a *addrBook) ifaces() []iface { return []iface{mosIID(0x22, abIface)} }

func (a *addrBook) call(h hostBlock) ([]byte, bool) {
	if h.oneWay() {
		return nil, false
	}
	ps, _ := parseParams(h.Params)
	if a.c.signedOn() == nil {
		return status(mapiFail), true
	}
	switch h.Method {
	case abContainers:
		users := a.entries("")
		rec := make([]byte, 0xDC)
		binary.LittleEndian.PutUint32(rec, 1)
		copy(rec[0x08:0x08+0x40], ansi(a.c.s.B.Conf().Name+" members"))
		binary.LittleEndian.PutUint32(rec[0xD0:], uint32(len(users)))
		binary.LittleEndian.PutUint32(rec[0xD8:], 0x00020000) // DT_GLOBAL
		return reply{}.dword(0).dword(1).blob(rec), true
	case abUserDetails, abUserDetailsH:
		var name string
		var tags []uint32
		for _, p := range ps {
			if p.Tag != 0x04 {
				continue
			}
			if name == "" && h.Method == abUserDetails {
				name = asciiz(p.Data)
			} else {
				tags = tagsOf(p.Data)
			}
		}
		u := a.member(name)
		if u == nil {
			return reply{}.dword(mapiNotFound).blob(nil), true
		}
		return reply{}.dword(0).blob(a.row(memberEntry(u), tags)), true
	case abCloseTable:
		return status(0), true
	case abQueryRows, abQueryRestrict, abQueryMore:
		var vars [][]byte
		var dwords []uint32
		for _, p := range ps {
			switch p.Tag {
			case 0x04:
				vars = append(vars, p.Data)
			case 0x03:
				dwords = append(dwords, p.Int)
			}
		}
		if len(vars) == 0 {
			return rowsReply(nil, 0), true
		}
		tags := tagsOf(vars[len(vars)-1])
		var name string
		if len(vars) >= 2 {
			name = asciiz(vars[len(vars)-2])
		}
		if h.Method == abQueryRestrict && name == "" && len(vars) >= 3 {
			name = anrOf(vars[0])
		}
		want := 45
		if len(dwords) >= 3 && dwords[2] > 0 && dwords[2] < 1000 {
			want = int(dwords[2])
		}
		if h.Method == abQueryMore {
			return rowsReply(nil, 0), true // every list goes in one page
		}
		users := a.entries(name)
		if h.Method == abQueryRestrict {
			// the one a name is, if there is one; else all it begins
			for _, e := range users {
				if strings.EqualFold(e.name, strings.TrimSpace(name)) {
					users = []abEntry{e}
					break
				}
			}
		}
		var rows []byte
		count := 0
		for _, u := range users {
			if count == want {
				break
			}
			r := a.row(u, tags)
			if len(rows)+len(r) > abRowMax {
				break
			}
			rows = append(rows, r...)
			count++
		}
		return rowsReply(rows, count), true
	}
	a.c.logf("MOSABP: no method %#x", h.Method)
	return nil, false
}

// rowsReply is a list's reply: five dwords, the row count fourth, and the
// rows compressed.
func rowsReply(rows []byte, count int) []byte {
	var blob []byte
	if count > 0 {
		blob = mszip(rows)
	}
	return reply{}.dword(0).dword(0).dword(0).dword(uint32(count)).dword(0).blob(blob)
}

func tagsOf(b []byte) []uint32 {
	var out []uint32
	for ; len(b) >= 4; b = b[4:] {
		out = append(out, binary.LittleEndian.Uint32(b))
	}
	return out
}

// anrOf is the name a serialised PR_ANR restriction looks for.
func anrOf(b []byte) string {
	if len(b) >= 8 && binary.LittleEndian.Uint32(b) == 0 { // RES_AND: the first node
		b = b[8:]
	}
	if len(b) < 32 {
		return ""
	}
	n := int(binary.LittleEndian.Uint32(b[28:]))
	if 32+n > len(b) {
		return ""
	}
	return unansi(b[32 : 32+n])
}

// abEntry is a row of the address book: a member, or a saved address.
type abEntry struct {
	name, email, typ string
	u                *store.User // a member's (a saved address's: the member it is, if one)
}

func memberEntry(u *store.User) abEntry {
	return abEntry{name: u.Name, email: u.Name, typ: addrTypeMSN, u: u}
}

// entries are the members and saved addresses a name begins (all, for
// none), by name.
func (a *addrBook) entries(prefix string) []abEntry {
	var out []abEntry
	for _, u := range a.members(prefix) {
		out = append(out, memberEntry(u))
	}
	p := strings.ToLower(strings.TrimSpace(prefix))
	for _, e := range a.saved() {
		if p == "" || strings.HasPrefix(strings.ToLower(e.name), p) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].name) < strings.ToLower(out[j].name) })
	return out
}

// saved is the caller's own address book, those of its addresses MSN
// mail can go to: an internet address, or a member.
func (a *addrBook) saved() []abEntry {
	u := a.c.signedOn()
	if u == nil {
		return nil
	}
	book, err := a.c.s.B.Store.Addresses(u.ID)
	if err != nil {
		a.c.logf("address book: %v", err)
	}
	var out []abEntry
	for _, b := range book {
		addr := savedAddr(b.Addr)
		switch m := a.member(addr); {
		case bbs.IsEmailAddress(addr):
			out = append(out, abEntry{name: b.Name, email: addr, typ: "INTERNET"})
		case m != nil && !m.QWKNode:
			out = append(out, abEntry{name: b.Name, email: m.Name, typ: addrTypeMSN, u: m})
		}
	}
	return out
}

// savedAddr is a saved address without CompuServe's dressing ("Jane
// >INTERNET:jane@example.com").
func savedAddr(s string) string {
	if i := strings.Index(s, ">"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSpace(s)
	if len(s) > 9 && strings.EqualFold(s[:9], "INTERNET:") {
		s = strings.TrimSpace(s[9:])
	}
	return s
}

// members are the users a name begins (all, for none), by name.
func (a *addrBook) members(prefix string) []*store.User {
	all, err := a.c.s.B.Store.Users()
	if err != nil {
		a.c.logf("address book: %v", err)
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	var out []*store.User
	for _, u := range all {
		if u.Locked || u.QWKNode {
			continue
		}
		if prefix == "" || strings.HasPrefix(strings.ToLower(u.Name), prefix) ||
			u.Alias != "" && strings.HasPrefix(strings.ToLower(u.Alias), prefix) {
			out = append(out, u)
		}
	}
	return out
}

func (a *addrBook) member(name string) *store.User {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	s := a.c.s.B.Store
	u, err := s.UserByName(name)
	if err != nil {
		if u, err = s.UserByScreenName(name); err != nil {
			return nil
		}
	}
	if u.Locked {
		return nil
	}
	return u
}

// row is a member's values for the tags, in their order: one for every
// tag, an empty one where there is nothing (the client counts them, and
// can't be told which is missing).
func (a *addrBook) row(e abEntry, tags []uint32) []byte {
	first, last, _ := strings.Cut(e.name, " ")
	var city, about string
	if e.u != nil {
		city, about = e.u.Location, e.u.About
	}
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(tags)))
	for _, t := range tags {
		var v any
		switch t {
		case tagObjectType:
			v = uint32(mapiMailUser)
		case tagDisplayType:
			v = uint32(0) // DT_MAILUSER
		case tagDisplayName, tagDisplayNameW:
			v = e.name
		case tagEmailAddress, tagEmailAddressW:
			v = e.email
		case tagAddrType:
			v = e.typ
		case tagCity:
			v = city
		case tagFirstName:
			v = first
		case tagLastName:
			v = last
		case tagInterests:
			v = about
		default:
			v = zeroValue(t)
		}
		b = abValue(b, t&0xFFFF, v)
	}
	return b
}

// abValue is a value as the address book carries it: strings and binaries
// counted (no NUL), the rest as wide as their type.
func abValue(b []byte, typ uint32, v any) []byte {
	switch typ {
	case ptString8:
		s := ansi(v.(string))
		return append(binary.LittleEndian.AppendUint32(b, uint32(len(s))), s...)
	case ptUnicode:
		w := putValue(nil, ptUnicode, v)
		w = w[:len(w)-2]
		return append(binary.LittleEndian.AppendUint32(b, uint32(len(w))), w...)
	case ptBinary:
		return putValue(b, ptBinary, v)
	}
	return putValue(b, typ, v)
}
