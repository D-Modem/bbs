package msn

import (
	"strings"

	"github.com/D-Modem/bbs/internal/store"
)

// view is what a viewer sees of a user: status ("" offline), display
// name, and from their MSN client its capabilities, display picture and
// personal message.
type view struct {
	status, friendly string
	caps, msnobj     string
	psm              []byte
}

// key tells views apart, for telling a change once.
func (v view) key() string {
	return v.status + "\x00" + v.friendly + "\x00" + v.caps + "\x00" + v.msnobj
}

// viewOf is what viewer sees of u.  A user blocking the viewer,
// appearing offline, or (BLP BL) not having the viewer as a friend is
// offline to them; one on MSN has the status they set there; one on the
// BBS by any other route is online.
func (s *Server) viewOf(u, viewer *store.User) view {
	v := view{friendly: u.Name, caps: "0"}
	if s.B.Blocks(u, viewer) {
		return v
	}
	var best *nsSession
	for _, ns := range s.sessionsOf(u.ID) {
		ns.mu.Lock()
		st, blp := ns.status, ns.blp
		ns.mu.Unlock()
		if st == "" {
			continue
		}
		if st == "HDN" {
			return v
		}
		if blp == "BL" {
			if ok, _ := s.B.Store.IsFriend(u.ID, viewer.ID); !ok {
				return v
			}
		}
		if best == nil || st == "NLN" {
			best = ns
			v.status = st
		}
	}
	if best != nil {
		best.mu.Lock()
		v.friendly, v.caps, v.msnobj, v.psm = best.friendly, best.caps, best.msnobj, best.psm
		best.mu.Unlock()
		return v
	}
	for _, n := range s.nodesOf(u.Name) {
		if n.Via != "msn" { // on MSN: as its status, set or not yet
			v.status = "NLN"
			break
		}
	}
	return v
}

// statusFor is the status viewer sees of u ("": offline).
func (s *Server) statusFor(u, viewer *store.User) string { return s.viewOf(u, viewer).status }

// presenceWords are an ILN's or NLN's words after the status: the
// handle, the display name, and since MSNP8 the capabilities (and since
// MSNP9 the display picture).
func (ns *nsSession) presenceWords(u *store.User, v view) []string {
	w := []string{ns.s.handleOf(u), encode(v.friendly)}
	if ns.ver >= 8 {
		w = append(w, v.caps)
		if ns.ver >= 9 && v.msnobj != "" {
			w = append(w, v.msnobj)
		}
	}
	return w
}

// tell tells the client what it sees of u now, if it differs from what
// it was last told: ILN (on trid) or NLN, or FLN; and (MSNP11) their
// personal message as they come on.
func (ns *nsSession) tell(trid string, u *store.User) {
	v := ns.s.viewOf(u, ns.u)
	key := ""
	if v.status != "" {
		key = v.key()
	}
	ns.mu.Lock()
	was, told := ns.told[u.ID]
	ns.told[u.ID] = key
	ns.mu.Unlock()
	if told && was == key || !told && key == "" {
		return
	}
	switch {
	case key == "":
		ns.k.send("FLN", ns.s.handleOf(u))
		return
	case trid != "":
		ns.k.send(append([]string{"ILN", trid, v.status}, ns.presenceWords(u, v)...)...)
	default:
		ns.k.send(append([]string{"NLN", v.status}, ns.presenceWords(u, v)...)...)
	}
	if ns.ver >= 11 && was == "" && len(v.psm) > 0 {
		ns.ubx(u, v.psm)
	}
}

// tellNow tells the client of a friend it just added who is on (ILN).
func (ns *nsSession) tellNow(trid string, u *store.User) {
	ns.mu.Lock()
	delete(ns.told, u.ID)
	ns.mu.Unlock()
	ns.tell(trid, u)
}

// ubx gives the client a friend's personal message (MSNP11).
func (ns *nsSession) ubx(u *store.User, psm []byte) {
	ns.k.sendPayload(psm, "UBX", ns.s.handleOf(u))
}

// renamed: a user's display name changed: those watching them are told.
func (s *Server) renamed(u *store.User) { s.changed(u) }

// psmChanged: a user's personal message changed: those watching them on
// MSNP11 and later, who see them on, are told.
func (s *Server) psmChanged(u *store.User) {
	for _, x := range s.all() {
		if x.ver < 11 || x.u.ID == u.ID {
			continue
		}
		x.do(func() {
			x.mu.Lock()
			_, ok := x.fl[u.ID]
			on := x.told[u.ID] != ""
			x.mu.Unlock()
			if ok && on && x.online() {
				x.ubx(u, s.viewOf(u, x.u).psm)
			}
		})
	}
}

// allContacts tells a client signing in which of its friends are on (ILN,
// on its first CHG's transaction).
func (ns *nsSession) allContacts(trid string) {
	ns.mu.Lock()
	ids := make([]int64, 0, len(ns.fl))
	for id := range ns.fl {
		ids = append(ids, id)
	}
	ns.mu.Unlock()
	for _, id := range ids {
		if u, err := ns.s.B.Store.UserByID(id); err == nil {
			ns.tell(trid, u)
		}
	}
}

// contactChanged tells the client a friend's state, if it differs from
// what it was last told.
func (ns *nsSession) contactChanged(u *store.User) {
	if u.ID == ns.u.ID || !ns.online() {
		return
	}
	ns.mu.Lock()
	_, ok := ns.fl[u.ID]
	ns.mu.Unlock()
	if ok {
		ns.tell("", u)
	}
}

// uux is UUX (MSNP11): its personal message and what it plays.
func (ns *nsSession) uux(c *cmd) {
	p := c.payload
	if len(p) > 0 && !strings.HasPrefix(strings.TrimSpace(string(p)), "<Data>") {
		ns.k.sendErr(errBadParam, c.trid)
		return
	}
	ns.mu.Lock()
	same := string(ns.psm) == string(p)
	ns.psm = append([]byte(nil), p...)
	ns.mu.Unlock()
	ns.k.send("UUX", c.trid, "0")
	if !same {
		ns.s.psmChanged(ns.u)
	}
}
