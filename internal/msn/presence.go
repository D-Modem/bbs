package msn

import (
	"github.com/D-Modem/bbs/internal/store"
)

// statusFor is the status viewer sees of u ("": offline).  A user blocking
// the viewer, appearing offline, or (BLP BL) not having the viewer as a
// friend is offline to them; one on MSN has the status they set there;
// one on the BBS by any other route is online.
func (s *Server) statusFor(u, viewer *store.User) string {
	if s.B.Blocks(u, viewer) {
		return ""
	}
	best := ""
	for _, ns := range s.sessionsOf(u.ID) {
		ns.mu.Lock()
		st, blp := ns.status, ns.blp
		ns.mu.Unlock()
		if st == "" {
			continue
		}
		if st == "HDN" {
			return ""
		}
		if blp == "BL" {
			if ok, _ := s.B.Store.IsFriend(u.ID, viewer.ID); !ok {
				return ""
			}
		}
		if best == "" || st == "NLN" {
			best = st
		}
	}
	if best != "" {
		return best
	}
	for _, n := range s.nodesOf(u.Name) {
		if s.onNode(n.Num) == nil {
			return "NLN"
		}
	}
	return ""
}

// friendlyFor is u's display name: as set on MSN, or their name.
func (s *Server) friendlyFor(u *store.User) string {
	for _, ns := range s.sessionsOf(u.ID) {
		if f := ns.friendlyName(); f != "" {
			return f
		}
	}
	return u.Name
}

// renamed: a user's display name changed: those watching them are told.
func (s *Server) renamed(u *store.User) {
	for _, x := range s.all() {
		x.do(func() {
			x.mu.Lock()
			_, ok := x.fl[u.ID]
			st := x.told[u.ID]
			x.mu.Unlock()
			if ok && st != "" && x.online() {
				x.k.send("NLN", st, s.handleOf(u), encode(s.friendlyFor(u)))
			}
		})
	}
}

// allContacts tells a client signing in which of its friends are on (ILN,
// on its first CHG's transaction).
func (ns *nsSession) allContacts(trid string) {
	ns.mu.Lock()
	fs := make([]store.Friend, 0, len(ns.fl))
	for _, f := range ns.fl {
		fs = append(fs, f)
	}
	ns.mu.Unlock()
	for _, f := range fs {
		u, err := ns.s.B.Store.UserByID(f.ID)
		if err != nil {
			continue
		}
		st := ns.s.statusFor(u, ns.u)
		ns.mu.Lock()
		ns.told[f.ID] = st
		ns.mu.Unlock()
		if st != "" {
			ns.k.send("ILN", trid, st, ns.s.handleOf(u), encode(ns.s.friendlyFor(u)))
		}
	}
}

// contactChanged tells the client a friend's status, if it differs from
// what it was last told.
func (ns *nsSession) contactChanged(u *store.User) {
	if u.ID == ns.u.ID || !ns.online() {
		return
	}
	ns.mu.Lock()
	_, ok := ns.fl[u.ID]
	was := ns.told[u.ID]
	ns.mu.Unlock()
	if !ok {
		return
	}
	st := ns.s.statusFor(u, ns.u)
	if st == was {
		return
	}
	ns.mu.Lock()
	ns.told[u.ID] = st
	ns.mu.Unlock()
	if st == "" {
		ns.k.send("FLN", ns.s.handleOf(u))
		return
	}
	ns.k.send("NLN", st, ns.s.handleOf(u), encode(ns.s.friendlyFor(u)))
}
