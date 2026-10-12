package bbs

import (
	"fmt"
	"sync"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// minCallTime is the least a call is given, however little of the day is
// left: to read what came, and log off.
var minCallTime = time.Minute

// clock keeps a call's time against the user's day: it warns as the end
// nears, and hangs up at it.
type clock struct {
	mu     sync.Mutex
	start  time.Time
	userID int64
	end    time.Time // zero: no limit
	timers []*time.Timer
}

// startClock starts counting the call against u's day, with the limit of
// their level.
func (n *Node) startClock(u *store.User) {
	c := &clock{start: time.Now(), userID: u.ID}
	if limit := n.b.LevelOf(u).Time; limit > 0 {
		left := limit - time.Duration(u.TimeToday())*time.Second
		left = max(left, minCallTime)
		c.end = c.start.Add(left)
		for _, w := range []time.Duration{5 * time.Minute, time.Minute} {
			if left > w {
				w := w
				c.timers = append(c.timers, time.AfterFunc(left-w, func() {
					n.deliver(Event{Kind: Notice, Text: fmt.Sprintf("%d minute(s) left today.", int(w.Minutes())), Time: time.Now()})
				}))
			}
		}
		c.timers = append(c.timers, time.AfterFunc(left, func() {
			n.deliver(Event{Kind: Notice, Text: "Your time is up for today.", Time: time.Now()})
			n.b.Log.Printf("node %d: %s's time is up", n.Num, u.Name)
			time.AfterFunc(2*time.Second, func() {
				if n.kick != nil {
					n.kick()
				}
			})
		}))
	}
	n.b.mu.Lock()
	n.clock = c
	n.b.mu.Unlock()
}

// stopClock adds the call's time to the user's day.
func (n *Node) stopClock() {
	n.b.mu.Lock()
	c := n.clock
	n.clock = nil
	n.b.mu.Unlock()
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.timers {
		t.Stop()
	}
	n.b.Store.AddTime(c.userID, int(time.Since(c.start).Seconds()))
}

// TimeLeft is the time the caller has left today; ok false if there is no
// limit.
func (n *Node) TimeLeft() (left time.Duration, ok bool) {
	n.b.mu.Lock()
	c := n.clock
	n.b.mu.Unlock()
	if c == nil || c.end.IsZero() {
		return 0, false
	}
	return max(time.Until(c.end), 0), true
}
