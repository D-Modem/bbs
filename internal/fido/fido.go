// Package fido puts the BBS on FidoNet (or any FTN network): echomail in
// the message areas that are echoes, netmail as private mail, packets
// tossed and scanned here, and binkp to move them to and from the links.
package fido

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/archive"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Settings are the system's on FidoNet.
type Settings struct {
	Addrs      []ftn.Addr // ours: the first is the main one, the rest AKAs
	System     string     // the system's name, for binkp
	Sysop      string
	Location   string
	Origin     string   // the origin line's text
	Dir        string   // inbound, outbound and bad packets under it
	UTF8       bool     // write UTF-8, not CP437
	MaxMessage int      // bytes of a message's text
	Nodelists  []string // nodelist files (globs: the newest of each is read)
	Links      []*Link
}

// Link is a system we exchange mail with.
type Link struct {
	Name            string
	Addr            ftn.Addr
	Host            string       // host:port to call it ("": it calls us)
	Password        string       // binkp session password
	PlainOK         bool         // the password may be sent in the clear (no CRAM-MD5)
	PktPassword     string       // packets' password ("": not checked, none sent)
	Echoes          string       // wildmat of the echo tags exchanged
	FileEchoes      string       // wildmat of the file echoes exchanged (TIC; "": none)
	AreaFix         bool         // it manages its echoes and file echoes here, by AreaFix and FileFix
	AreafixPassword string       // between us for AreaFix and FileFix, its to ours and ours to its
	TicPassword     string       // the TIC files' password ("": not checked, none sent)
	Create          bool         // echoes (and file echoes) it sends that are not here are made
	Route           bool         // netmail for nodes that are not links goes here
	Packer          archive.Kind // bundles' format (ARCmail): ZIP, ARC, ARJ, LHA ("": bare packets)
	Poll            time.Duration
	AKA             ftn.Addr // ours to use with it (zero: by its zone)
}

// Fido is the BBS on FidoNet.
type Fido struct {
	B *bbs.BBS
	S Settings

	locks sync.Map // link name: *sync.Mutex, one session or toss at a time
	kick  chan struct{}

	nl      atomic.Pointer[ftn.Nodelist]
	nlMu    sync.Mutex
	nlStamp string // the nodelist files read, and their times
}

// New makes it, its directories too.
func New(b *bbs.BBS, s Settings) (*Fido, error) {
	if len(s.Addrs) == 0 {
		return nil, errors.New("no address")
	}
	if s.MaxMessage == 0 {
		s.MaxMessage = 64 << 10
	}
	f := &Fido{B: b, S: s, kick: make(chan struct{}, 1)}
	for _, d := range []string{"in", "out", "bad", "tmp"} {
		if err := os.MkdirAll(filepath.Join(s.Dir, d), 0700); err != nil {
			return nil, err
		}
	}
	for _, l := range s.Links {
		for _, d := range []string{f.inbound(l), f.outbound(l)} {
			if err := os.MkdirAll(d, 0700); err != nil {
				return nil, err
			}
		}
	}
	f.loadNodelists()
	return f, nil
}

// inbound is the directory a link's files come into.
func (f *Fido) inbound(l *Link) string { return filepath.Join(f.S.Dir, "in", l.Name) }

// outbound is the directory of what waits to be sent to a link.
func (f *Fido) outbound(l *Link) string { return filepath.Join(f.S.Dir, "out", l.Name) }

// linkLock keeps a link to one session, scan or toss at a time: in this
// process, and with a lock file, against bbs fido commands run meanwhile.
type linkLock struct {
	mu   sync.Mutex
	path string
	fh   *os.File
}

// flock locks the lock file as how says (false: it is held elsewhere). With no
// lock file to be had it is true: the mutex alone keeps the link.
func (k *linkLock) flock(how int) bool {
	fh, err := os.OpenFile(k.path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return true // no lock file to be had: the mutex will do
	}
	if err := syscall.Flock(int(fh.Fd()), how); err != nil {
		fh.Close()
		return false
	}
	k.fh = fh
	return true
}

// Lock waits for the link.
func (k *linkLock) Lock() {
	k.mu.Lock()
	k.flock(syscall.LOCK_EX)
}

// TryLock takes the link if it is free.
func (k *linkLock) TryLock() bool {
	if !k.mu.TryLock() {
		return false
	}
	if !k.flock(syscall.LOCK_EX | syscall.LOCK_NB) {
		k.mu.Unlock()
		return false
	}
	return true
}

// Unlock lets it go.
func (k *linkLock) Unlock() {
	if k.fh != nil {
		k.fh.Close()
		k.fh = nil
	}
	k.mu.Unlock()
}

// lock is a link's lock.
func (f *Fido) lock(l *Link) *linkLock {
	m, _ := f.locks.LoadOrStore(l.Name, &linkLock{path: filepath.Join(f.outbound(l), ".lock")})
	return m.(*linkLock)
}

// Main is our main address.
func (f *Fido) Main() ftn.Addr { return f.S.Addrs[0] }

// akaFor is our address to show a link: its own choice, or the first of
// ours in its zone, or the main one.
func (f *Fido) akaFor(l *Link) ftn.Addr {
	if l.AKA.Zone != 0 {
		return l.AKA
	}
	for _, a := range f.S.Addrs {
		if a.Zone == l.Addr.Zone {
			return a
		}
	}
	return f.Main()
}

// ours reports whether an address is one of ours.
func (f *Fido) ours(a ftn.Addr) bool {
	for _, o := range f.S.Addrs {
		if o.Same(a) {
			return true
		}
	}
	return false
}

// Link finds a link by name.
func (f *Fido) Link(name string) *Link {
	for _, l := range f.S.Links {
		if strings.EqualFold(l.Name, name) {
			return l
		}
	}
	return nil
}

// linkByAddr finds the link of an address.
func (f *Fido) linkByAddr(a ftn.Addr) *Link {
	for _, l := range f.S.Links {
		if l.Addr.Same(a) {
			return l
		}
	}
	return nil
}

// origin is how messages from a link are marked in the store.
func (l *Link) origin() string { return "ftn:" + l.Name }

// Netmail queues a netmail from a user: the BBS's NetmailFunc.
func (f *Fido) Netmail(u *store.User, to string, addr ftn.Addr, subject, body string, reply *store.Message) error {
	if err := f.checkNodelist(addr); err != nil {
		return err
	}
	if f.route(addr) == nil {
		return fmt.Errorf("no route to %s: it is not a link, and no link takes netmail on", addr)
	}
	n := &store.Netmail{FromID: u.ID, From: bbs.Clean(u.Name, bbs.MaxFTNNameLen), To: to,
		ToAddr: addr.String4D(), Subject: subject, Body: body, MsgID: ftn.NewMSGID(f.Main())}
	if reply != nil {
		n.Reply = reply.FTNMsgID
	}
	if _, err := f.B.Store.QueueNetmail(n); err != nil {
		return err
	}
	f.Kick()
	return nil
}

// route is the link netmail to a goes through: a itself, a's boss, or the
// default route.
func (f *Fido) route(a ftn.Addr) *Link {
	if l := f.linkByAddr(a); l != nil {
		return l
	}
	if l := f.linkByAddr(a.Boss()); l != nil {
		return l
	}
	for _, l := range f.S.Links {
		if l.Route {
			return l
		}
	}
	return nil
}

// Kick asks Run to scan (and call) now.
func (f *Fido) Kick() {
	select {
	case f.kick <- struct{}{}:
	default:
	}
}

// Run scans, and calls the links that have mail waiting (at once when
// there is new mail, else every few minutes while calls fail) or are due
// a poll, until ctx is done.
func (f *Fido) Run(ctx context.Context) {
	lastPoll := map[string]time.Time{}
	lastTry := map[string]time.Time{}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		f.loadNodelists()
		fresh, err := f.ScanAll()
		if err != nil {
			f.B.Log.Printf("fido: scan: %v", err)
		}
		for _, l := range f.S.Links {
			if l.Host == "" {
				continue
			}
			poll := l.Poll > 0 && time.Since(lastPoll[l.Name]) >= l.Poll
			mail := f.waiting(l) && (fresh || time.Since(lastTry[l.Name]) >= retryWait)
			if poll || mail {
				lastTry[l.Name] = time.Now()
				if poll {
					lastPoll[l.Name] = time.Now()
				}
				go func(l *Link) {
					if err := f.Call(ctx, l); err != nil {
						f.B.Log.Printf("fido: calling %s: %v", l.Name, err)
					}
				}(l)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-f.kick:
		}
	}
}

// retryWait is how long a failed call waits to be tried again.
const retryWait = 5 * time.Minute

// waiting reports whether a link has files waiting in its outbound.
func (f *Fido) waiting(l *Link) bool {
	ents, _ := os.ReadDir(f.outbound(l))
	for _, e := range ents {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}

// Outbound counts the files waiting for a link.
func (f *Fido) Outbound(l *Link) int {
	n := 0
	ents, _ := os.ReadDir(f.outbound(l))
	for _, e := range ents {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			n++
		}
	}
	return n
}
