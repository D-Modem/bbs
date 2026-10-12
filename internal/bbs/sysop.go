package bbs

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// What the sysop does to and for other nodes: watching one, calling its
// caller into chat; and whether they are to be paged.

// tap copies a node's output to the sysops watching it.
type tap struct {
	mu       sync.Mutex
	echoing  bool // the node's front end gives its output (a text terminal's)
	watchers map[chan []byte]bool
	summon   string // the room the caller is called into ("": none)
}

// Errors of watching.
var (
	ErrNoEcho   = errors.New("that node's screen cannot be watched (a 3270's, or no caller yet)")
	ErrSelf     = errors.New("that is your own node")
	ErrNotSysop = errors.New("only a sysop does that")
)

// SetEchoing says the node's front end gives Echo what it sends.
func (n *Node) SetEchoing() {
	n.sysopTap.mu.Lock()
	n.sysopTap.echoing = true
	n.sysopTap.mu.Unlock()
}

// Echo gives what was sent to the node's caller to those watching it; a
// watcher too slow to keep up misses some.
func (n *Node) Echo(p []byte) {
	t := &n.sysopTap
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.watchers) == 0 {
		return
	}
	cp := append([]byte(nil), p...)
	for ch := range t.watchers {
		select {
		case ch <- cp:
		default:
		}
	}
}

// Watch gives a sysop on node by what node num's caller is sent, until
// stop is called.
func (by *Node) Watch(num int) (<-chan []byte, func(), error) {
	if u := by.User(); u == nil || !u.Sysop {
		return nil, nil, ErrNotSysop
	}
	if num == by.Num {
		return nil, nil, ErrSelf
	}
	o := by.b.node(num)
	if o == nil {
		return nil, nil, ErrNoNode
	}
	t := &o.sysopTap
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.echoing {
		return nil, nil, ErrNoEcho
	}
	ch := make(chan []byte, 256)
	if t.watchers == nil {
		t.watchers = map[chan []byte]bool{}
	}
	t.watchers[ch] = true
	by.b.Log.Printf("node %d: %s watches node %d", by.Num, by.name(), num)
	stop := func() {
		t.mu.Lock()
		delete(t.watchers, ch)
		t.mu.Unlock()
	}
	return ch, stop, nil
}

// Summon calls node num's caller into a private chat with the sysop on
// node by: they are told now, and taken there at their next menu.  The
// room is the one by joins.
func (by *Node) Summon(num int) (string, error) {
	u := by.User()
	if u == nil || !u.Sysop {
		return "", ErrNotSysop
	}
	if num == by.Num {
		return "", ErrSelf
	}
	o := by.b.node(num)
	if o == nil || o.User() == nil {
		return "", ErrNoNode
	}
	room := pageRoom(num)
	o.sysopTap.mu.Lock()
	o.sysopTap.summon = room
	o.sysopTap.mu.Unlock()
	o.deliver(Event{Kind: Notice, Text: fmt.Sprintf("%s would like a word with you: you are going to chat.", u.Name),
		Time: time.Now()})
	by.b.Log.Printf("node %d: %s calls node %d into chat", by.Num, u.Name, num)
	return room, nil
}

// Summoned is the room the caller has been called into ("": none), once.
func (n *Node) Summoned() string {
	n.sysopTap.mu.Lock()
	defer n.sysopTap.mu.Unlock()
	r := n.sysopTap.summon
	n.sysopTap.summon = ""
	return r
}

// SysopAvailable: pages reach the sysop.
func (b *BBS) SysopAvailable() bool { return !b.sysopAway.Load() }

// SetSysopAvailable says whether pages reach the sysop.
func (b *BBS) SetSysopAvailable(on bool) {
	b.sysopAway.Store(!on)
	b.Log.Printf("the sysop is %s", map[bool]string{true: "available for chat", false: "not available for chat"}[on])
}

// PurgeAreas takes away the messages each area keeps no more (its
// max_msgs, max_days); how many went.
func (b *BBS) PurgeAreas() (int, error) {
	as, err := b.Store.Areas()
	if err != nil {
		return 0, err
	}
	// what REPs brought, kept a while against uploads twice over
	if err := b.Store.ForgetREPs(time.Now().AddDate(0, 0, -90)); err != nil {
		return 0, err
	}
	total := 0
	for _, a := range as {
		if a.MaxMsgs == 0 && a.MaxDays == 0 {
			continue
		}
		n, err := b.Store.Purge(a)
		if err != nil {
			return total, fmt.Errorf("%s: %w", a.Name, err)
		}
		if n > 0 {
			b.Log.Printf("purged %d messages from %s", n, a.Name)
		}
		total += n
	}
	return total, nil
}
