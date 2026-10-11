package bbs

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Stats count what the BBS has done since it started (for metrics).
type Stats struct {
	Logins, LoginFailures, Posts, Mail, Pages, ChatLines atomic.Int64
}

// Agent is a node that is no line: what an API client does by one request
// (post, send mail) goes through it, as a caller's would, without its
// taking a node.
func (b *BBS) Agent(u *store.User, via string) *Node {
	return &Node{Num: 0, Caller: &Caller{Via: via}, Since: time.Now(), b: b, user: u, activity: via}
}

// Kick throws the caller on a node off.
func (b *BBS) Kick(num int) error {
	b.mu.Lock()
	n := b.nodes[num]
	b.mu.Unlock()
	if n == nil {
		return ErrNoNode
	}
	if n.kick == nil {
		return errors.New("that node cannot be thrown off")
	}
	b.Log.Printf("node %d: thrown off", num)
	n.kick()
	return nil
}

// NodeOf finds the node of a number.
func (b *BBS) NodeOf(num int) *Node { return b.node(num) }

// tellMail tells the nodes of a user of a new mail.
func (b *BBS) tellMail(to int64, from, subject string) {
	b.mu.Lock()
	var ns []*Node
	for _, n := range b.nodes {
		if n.user != nil && n.user.ID == to {
			ns = append(ns, n)
		}
	}
	b.mu.Unlock()
	for _, n := range ns {
		n.deliver(Event{Kind: Mail, From: from, Text: subject, Time: time.Now()})
	}
}
