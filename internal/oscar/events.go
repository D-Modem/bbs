package oscar

import (
	"fmt"

	"github.com/D-Modem/bbs/internal/bbs"
)

// event is something the node is told: a private message as an IM, a
// room's line to its chat connection, anything else as an IM from the
// system.
func (ss *session) event(e bbs.Event) {
	switch e.Kind {
	case bbs.Private:
		text := e.Text
		if e.Kept {
			text = "(" + e.Time.Format("Jan 2 15:04") + ", while you were away) " + text
		}
		ss.imFrom(e.From, text)
		return
	case bbs.Said, bbs.Emoted, bbs.Joined, bbs.Left, bbs.Topic:
		if c := ss.chatOf(e.Room); c != nil {
			c.event(e, false)
		}
		return
	case bbs.Notice:
		if e.Room != "" {
			if c := ss.chatOf(e.Room); c != nil {
				c.event(e, false)
				return
			}
		}
	case bbs.Paged:
		text := fmt.Sprintf("%s (node %d) is paging you", e.From, e.Node)
		if e.Text != "" {
			text += ": " + e.Text
		}
		ss.imFrom(ss.systemName(), text+fmt.Sprintf(" (join the chat room sysop-%d to answer)", e.Node))
		return
	}
	ss.imFrom(ss.systemName(), e.String())
}

// chatOf is the chat connection in a room (nil: none).
func (ss *session) chatOf(room string) *chatConn {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	for c := range ss.chats {
		if c.room == room && c.joined {
			return c
		}
	}
	return nil
}
