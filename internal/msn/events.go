package msn

import (
	"fmt"

	"github.com/D-Modem/bbs/internal/bbs"
)

// event is something the node is told: a private message, as an IM from
// its sender; anything else as an IM from the BBS.
func (ns *nsSession) event(e bbs.Event) {
	if plain := bbs.Plain(e); plain != nil { // ops and kicks: as notices and leaving
		for _, x := range plain {
			ns.event(x)
		}
		return
	}
	switch e.Kind {
	case bbs.Private:
		text := e.Text
		if e.Kept {
			text = "(" + e.Time.Format("Jan 2 15:04") + ", while you were away) " + text
		}
		u, err := ns.s.B.Store.UserByName(e.From)
		if err != nil {
			ns.s.deliverFrom(ns, nil, e.From+": "+text)
			return
		}
		if ns.s.B.Blocks(ns.u, u) {
			return
		}
		ns.s.deliverFrom(ns, u, text)
		return
	case bbs.Said, bbs.Emoted, bbs.Joined, bbs.Left, bbs.Topic:
		return // no rooms on MSN
	case bbs.Paged:
		text := fmt.Sprintf("%s (node %d) is paging you", e.From, e.Node)
		if e.Text != "" {
			text += ": " + e.Text
		}
		ns.s.deliverFrom(ns, nil, text+fmt.Sprintf(" (join the chat room sysop-%d to answer)", e.Node))
		return
	}
	ns.s.deliverFrom(ns, nil, e.String())
}

// deliverKept gives the client the private messages kept for its user
// while they were away.
func (ns *nsSession) deliverKept() {
	ns.mu.Lock()
	sent := ns.keptSent
	ns.keptSent = true
	ns.mu.Unlock()
	if sent {
		return
	}
	for _, e := range ns.n.Kept() {
		ns.event(e)
	}
}
