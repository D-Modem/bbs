package bbs

import (
	"errors"
	"strings"

	"github.com/D-Modem/bbs/internal/store"
)

// Private messages between users are kept, whatever way they come (IRC,
// AIM, ICQ, the BBS's chat): one away gets them on their next login, and
// either may read their conversation back.

// historyLines are the lines of a conversation History gives at most.
const historyLines = 500

// ErrNoUser: there is no user of that name.
var ErrNoUser = errors.New("there is no user of that name")

// Keep keeps a private line from the node's user to another (delivered:
// they had it, on some node).
func (n *Node) Keep(to *store.User, text string, delivered bool) {
	from := n.User()
	text = Clean(text, MaxChatLine)
	if from == nil || to == nil || text == "" {
		return
	}
	if _, err := n.b.Store.AddChat(&store.Chat{FromID: from.ID, From: from.Name, ToID: to.ID, To: to.Name,
		Text: text, Delivered: delivered}); err != nil {
		n.b.Log.Printf("node %d: keeping a message: %v", n.Num, err)
	}
}

// nodesOfUser are the nodes a user is on.
func (b *BBS) nodesOfUser(id int64) []*Node {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*Node
	for _, o := range b.nodes {
		if o.user != nil && o.user.ID == id {
			out = append(out, o)
		}
	}
	return out
}

// TellUser sends a private line to a user: to each node they are on
// (besides this one), or, if they are on none, kept for them.  Online: it
// went to them now.
func (n *Node) TellUser(to *store.User, text string) (online bool, err error) {
	text = Clean(text, MaxChatLine)
	if text == "" {
		return false, errors.New("say something")
	}
	sent := false
	var lastErr error
	others := n.b.nodesOfUser(to.ID)
	for _, o := range others {
		if o == n {
			continue
		}
		if err := n.Message(o.Num, text); err != nil {
			lastErr = err
		} else {
			sent = true
		}
	}
	if !sent && lastErr != nil {
		return false, lastErr
	}
	if len(others) == 0 {
		if n.flood() {
			return false, ErrFlood
		}
	}
	n.Keep(to, text, len(others) > 0)
	return len(others) > 0, nil
}

// Kept takes the private lines kept for the node's user while they were
// away, as events, and marks them had.
func (n *Node) Kept() []Event {
	u := n.User()
	if u == nil {
		return nil
	}
	cs, err := n.b.Store.KeptChats(u.ID)
	if err != nil {
		n.b.Log.Printf("node %d: kept messages: %v", n.Num, err)
		return nil
	}
	if len(cs) == 0 {
		return nil
	}
	ids := make([]int64, len(cs))
	out := make([]Event, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
		out[i] = Event{Kind: Private, From: c.From, Text: c.Text, Time: c.Sent, Kept: true}
	}
	if err := n.b.Store.ChatsDelivered(ids); err != nil {
		n.b.Log.Printf("node %d: kept messages: %v", n.Num, err)
	}
	return out
}

// DeliverKept gives the node the private lines kept for its user, as its
// events.
func (n *Node) DeliverKept() {
	for _, e := range n.Kept() {
		n.deliver(e)
	}
}

// History is the node's user's conversation with another user, oldest
// first (at most limit lines; 0: as many as History gives).
func (n *Node) History(with *store.User, limit int) ([]store.Chat, error) {
	u := n.User()
	if u == nil {
		return nil, ErrNoUser
	}
	if limit <= 0 || limit > historyLines {
		limit = historyLines
	}
	return n.b.Store.ChatsBetween(u.ID, with.ID, limit)
}

// Conversations are those the node's user has talked with, the latest
// first.
func (n *Node) Conversations() ([]store.ChatPartner, error) {
	u := n.User()
	if u == nil {
		return nil, ErrNoUser
	}
	return n.b.Store.ChatPartners(u.ID, 50)
}

// HistoryLines is a conversation as lines to show.
func HistoryLines(cs []store.Chat) []string {
	out := make([]string, 0, len(cs))
	day := ""
	for _, c := range cs {
		if d := c.Sent.Format("Monday, January 2, 2006"); d != day {
			day = d
			out = append(out, "-- "+d+" --")
		}
		out = append(out, c.Sent.Format("15:04")+" <"+c.From+"> "+c.Text)
	}
	return out
}

// Friends is a user's friends list.
func (b *BBS) Friends(u *store.User) ([]store.Friend, error) { return b.Store.Friends(u.ID) }

// AddFriend puts the user of that name on u's friends list; that user.
func (b *BBS) AddFriend(u *store.User, name string) (*store.User, error) {
	f, err := b.Store.UserByName(strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNoUser
		}
		return nil, err
	}
	if f.ID == u.ID {
		return nil, errors.New("you are always your own friend")
	}
	if ok, err := b.Store.IsFriend(u.ID, f.ID); err != nil {
		return nil, err
	} else if ok {
		return f, nil
	}
	if err := b.Store.PutFriend(u.ID, store.Friend{ID: f.ID}); err != nil {
		return nil, err
	}
	b.FriendsChanged(u.ID)
	return f, nil
}

// RemoveFriend takes the user of that name off u's friends list; that
// user.
func (b *BBS) RemoveFriend(u *store.User, name string) (*store.User, error) {
	f, err := b.Store.UserByName(strings.TrimSpace(name))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNoUser
		}
		return nil, err
	}
	if ok, err := b.Store.IsFriend(u.ID, f.ID); err != nil {
		return nil, err
	} else if !ok {
		return nil, errors.New(f.Name + " is not on your friends list")
	}
	if err := b.Store.RemoveFriend(u.ID, f.ID); err != nil {
		return nil, err
	}
	b.FriendsChanged(u.ID)
	return f, nil
}

// WatchFriends has fn told when a user's friends list changes, until the
// returned func is called.  fn must not block.
func (b *BBS) WatchFriends(fn func(user int64)) (stop func()) {
	b.presMu.Lock()
	defer b.presMu.Unlock()
	if b.friendFns == nil {
		b.friendFns = map[int]func(int64){}
	}
	b.friendNext++
	id := b.friendNext
	b.friendFns[id] = fn
	return func() {
		b.presMu.Lock()
		delete(b.friendFns, id)
		b.presMu.Unlock()
	}
}

// FriendsChanged tells the watchers that a user's friends list changed.
func (b *BBS) FriendsChanged(user int64) {
	b.presMu.Lock()
	fns := make([]func(int64), 0, len(b.friendFns))
	for _, fn := range b.friendFns {
		fns = append(fns, fn)
	}
	b.presMu.Unlock()
	for _, fn := range fns {
		fn(user)
	}
}

// FriendLines are a user's friends as lines to show: who is on, and how.
func (b *BBS) FriendLines(u *store.User) ([]string, error) {
	fs, err := b.Friends(u)
	if err != nil {
		return nil, err
	}
	if len(fs) == 0 {
		return []string{"Your friends list is empty."}, nil
	}
	out := []string{"Your friends:"}
	for _, f := range fs {
		var on []string
		for _, n := range b.Nodes() {
			if strings.EqualFold(n.User, f.Name) {
				on = append(on, n.Via)
			}
		}
		state := "away"
		if len(on) > 0 {
			state = "on (" + strings.Join(on, ", ") + ")"
		}
		out = append(out, "  "+f.Name+"  "+state)
	}
	return out, nil
}
