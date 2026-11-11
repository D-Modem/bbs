package dnsd

import (
	"container/list"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// cache keeps upstream answers until their TTLs run out, the least
// recently used going first when it is full.
type cache struct {
	mu      sync.Mutex
	max     int
	maxTTL  time.Duration
	negTTL  time.Duration // the longest a "no such name" or "no such record" is kept
	entries map[cacheKey]*list.Element
	lru     *list.List // of *cacheEntry, most recent first
	now     func() time.Time
}

type cacheKey struct {
	name   string
	qtype  uint16
	qclass uint16
	do, cd bool // DNSSEC records wanted; checking disabled
}

type cacheEntry struct {
	key     cacheKey
	msg     *dns.Msg
	stored  time.Time
	expires time.Time
}

func newCache(max int, maxTTL, negTTL time.Duration) *cache {
	return &cache{max: max, maxTTL: maxTTL, negTTL: negTTL, entries: map[cacheKey]*list.Element{},
		lru: list.New(), now: time.Now}
}

func keyOf(q *dns.Msg) cacheKey {
	k := cacheKey{name: strings.ToLower(q.Question[0].Name), qtype: q.Question[0].Qtype,
		qclass: q.Question[0].Qclass, cd: q.CheckingDisabled}
	if o := q.IsEdns0(); o != nil {
		k.do = o.Do()
	}
	return k
}

// get is a copy of the answer kept for q, its TTLs counted down, or nil.
func (c *cache) get(q *dns.Msg) *dns.Msg {
	if c == nil || c.max == 0 {
		return nil
	}
	k := keyOf(q)
	c.mu.Lock()
	defer c.mu.Unlock()
	el := c.entries[k]
	if el == nil {
		return nil
	}
	e := el.Value.(*cacheEntry)
	now := c.now()
	if !now.Before(e.expires) {
		c.lru.Remove(el)
		delete(c.entries, k)
		return nil
	}
	c.lru.MoveToFront(el)
	m := e.msg.Copy()
	age := uint32(now.Sub(e.stored) / time.Second)
	for _, rrs := range [][]dns.RR{m.Answer, m.Ns, m.Extra} {
		for _, rr := range rrs {
			h := rr.Header()
			if h.Rrtype == dns.TypeOPT {
				continue
			}
			if h.Ttl > age {
				h.Ttl -= age
			} else {
				h.Ttl = 0
			}
		}
	}
	return m
}

// put keeps r, the answer to q, for as long as its TTLs say (a SERVFAIL
// or a truncated answer is not kept).
func (c *cache) put(q, r *dns.Msg) {
	if c == nil || c.max == 0 || r.Truncated {
		return
	}
	ttl, ok := c.ttl(r)
	if !ok || ttl <= 0 {
		return
	}
	k := keyOf(q)
	now := c.now()
	e := &cacheEntry{key: k, msg: r.Copy(), stored: now, expires: now.Add(ttl)}
	e.msg.Id = 0
	c.mu.Lock()
	defer c.mu.Unlock()
	if el := c.entries[k]; el != nil {
		el.Value = e
		c.lru.MoveToFront(el)
		return
	}
	c.entries[k] = c.lru.PushFront(e)
	for c.lru.Len() > c.max {
		last := c.lru.Back()
		c.lru.Remove(last)
		delete(c.entries, last.Value.(*cacheEntry).key)
	}
}

// ttl is how long r may be kept: its records' shortest TTL, or for "no
// such name" and "no such record", the SOA's (RFC 2308).
func (c *cache) ttl(r *dns.Msg) (time.Duration, bool) {
	switch r.Rcode {
	case dns.RcodeSuccess, dns.RcodeNameError:
	default:
		return 0, false
	}
	min := uint32(1<<32 - 1)
	if r.Rcode == dns.RcodeSuccess && len(r.Answer) > 0 {
		for _, rr := range r.Answer {
			min = minU(min, rr.Header().Ttl)
		}
		d := time.Duration(min) * time.Second
		return minD(d, c.maxTTL), true
	}
	// negative: the SOA in the authority section says
	for _, rr := range r.Ns {
		if soa, ok := rr.(*dns.SOA); ok {
			min = minU(min, minU(soa.Hdr.Ttl, soa.Minttl))
		}
	}
	if min == 1<<32-1 {
		return 0, false // no SOA: not kept (RFC 2308 5)
	}
	d := time.Duration(min) * time.Second
	return minD(d, minD(c.negTTL, c.maxTTL)), true
}

// len is how many answers are kept.
func (c *cache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}

func minU(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func minD(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
