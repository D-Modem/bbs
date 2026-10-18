// Package guard keeps callers out: addresses the sysop refuses (a file of
// IPs and networks), addresses banned for a while after too many failed
// logins, names no one may take, and a log of it all for the sysop (the
// hack log).
//
// A nil *Guard refuses nothing and logs nothing.
package guard

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

// Guard is the BBS's filters, bans and hack log.
type Guard struct {
	DenyFile     string        // addresses refused: IPs, CIDR networks, # comments
	NameDenyFile string        // names no new account may have: patterns with *, any case
	HackLog      string        // the hack log ("": none)
	BanTime      time.Duration // how long an address that is throttled is banned (0: never)

	mu     sync.Mutex
	deny   list[*net.IPNet]
	names  list[string]
	bans   map[string]time.Time
	logMu  sync.Mutex
	now    func() time.Time
	Reload time.Duration // how often the files are looked at for changes (0: 2s)
}

// list is a file's entries, read again when the file changes.
type list[T any] struct {
	items   []T
	mod     time.Time
	size    int64
	checked time.Time
}

// clock is the time now (a test's own clock, if it has set one).
func (g *Guard) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

// lines are a file's lines without comments and blanks.
func lines(p string) ([]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if i := strings.IndexAny(l, "#;"); i >= 0 {
			l = l[:i]
		}
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, sc.Err()
}

// ParseNet reads an address or a network: 192.0.2.7, 2001:db8::1,
// 198.51.100.0/24.
func ParseNet(s string) (*net.IPNet, error) {
	if strings.Contains(s, "/") {
		_, n, err := net.ParseCIDR(s)
		return n, err
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("%q is not an address or a network", s)
	}
	bits := 128
	if ip4 := ip.To4(); ip4 != nil {
		ip, bits = ip4, 32
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}, nil
}

// load reads a file into l again if it changed (looked at every few
// seconds at most).
func load[T any](g *Guard, l *list[T], p string, parse func(string) (T, error)) []T {
	if p == "" {
		return nil
	}
	now := g.clock()
	every := g.Reload
	if every == 0 {
		every = 2 * time.Second
	}
	if now.Sub(l.checked) < every && !l.checked.IsZero() {
		return l.items
	}
	l.checked = now
	fi, err := os.Stat(p)
	if err != nil {
		return l.items // gone for a moment: keep what was read
	}
	if fi.ModTime().Equal(l.mod) && fi.Size() == l.size {
		return l.items
	}
	ls, err := lines(p)
	if err != nil {
		return l.items
	}
	var items []T
	for _, s := range ls {
		if t, err := parse(s); err == nil {
			items = append(items, t)
		}
	}
	l.items, l.mod, l.size = items, fi.ModTime(), fi.Size()
	return items
}

// Check reads the files as the guard will, saying what is wrong with them.
func (g *Guard) Check() []string {
	if g == nil {
		return nil
	}
	var out []string
	for _, f := range []struct{ name, path string }{{"deny_file", g.DenyFile}, {"name_deny_file", g.NameDenyFile}} {
		if f.path == "" {
			continue
		}
		ls, err := lines(f.path)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: %v", f.name, err))
			continue
		}
		if f.name == "deny_file" {
			for _, l := range ls {
				if _, err := ParseNet(l); err != nil {
					out = append(out, fmt.Sprintf("%s %s: %v", f.name, f.path, err))
				}
			}
		}
	}
	return out
}

// Refused says whether a caller from ip (an address without a port; ""
// for local callers, never refused) is kept out, and why.
func (g *Guard) Refused(ip string) (bool, string) {
	if g == nil || ip == "" {
		return false, ""
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return false, ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if until, ok := g.bans[addr.String()]; ok {
		if g.clock().Before(until) {
			return true, "banned until " + until.Format("15:04")
		}
		delete(g.bans, addr.String())
	}
	for _, n := range load(g, &g.deny, g.DenyFile, ParseNet) {
		if n.Contains(addr) {
			return true, "in deny_file (" + n.String() + ")"
		}
	}
	return false, ""
}

// Ban keeps ip out for BanTime (if there is one); false if it does not.
func (g *Guard) Ban(ip, why string) bool {
	if g == nil || g.BanTime <= 0 || net.ParseIP(ip) == nil {
		return false
	}
	g.mu.Lock()
	if g.bans == nil {
		g.bans = map[string]time.Time{}
	}
	until := g.clock().Add(g.BanTime)
	_, already := g.bans[ip]
	g.bans[ip] = until
	g.mu.Unlock()
	if !already {
		g.Hack("%s banned until %s: %s", ip, until.Format("2006-01-02 15:04"), why)
	}
	return true
}

// Bans are the addresses banned now, and until when.
func (g *Guard) Bans() map[string]time.Time {
	out := map[string]time.Time{}
	if g == nil {
		return out
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock()
	for ip, until := range g.bans {
		if now.Before(until) {
			out[ip] = until
		}
	}
	return out
}

// Unban lets an address in again.
func (g *Guard) Unban(ip string) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.bans[ip]
	delete(g.bans, ip)
	return ok
}

// NameDenied says whether a new account may not have the name.
func (g *Guard) NameDenied(name string) bool {
	if g == nil || g.NameDenyFile == "" {
		return false
	}
	g.mu.Lock()
	pats := load(g, &g.names, g.NameDenyFile, func(s string) (string, error) { return strings.ToLower(s), nil })
	g.mu.Unlock()
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range pats {
		// path.Match's * is any run of characters but /; names have no /
		if ok, _ := path.Match(p, n); ok {
			return true
		}
	}
	return false
}

// Hack writes a line to the hack log.
func (g *Guard) Hack(format string, a ...any) {
	if g == nil || g.HackLog == "" {
		return
	}
	line := g.clock().Format("2006-01-02 15:04:05") + " " + fmt.Sprintf(format, a...)
	line = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return '?'
		}
		return r
	}, line)
	g.logMu.Lock()
	defer g.logMu.Unlock()
	// opened each time: a log rotated away is followed by a new one
	f, err := os.OpenFile(g.HackLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	f.WriteString(line + "\n")
	f.Close()
}

// Tail is the hack log's last n lines.
func (g *Guard) Tail(n int) []string {
	if g == nil || g.HackLog == "" {
		return nil
	}
	g.logMu.Lock()
	defer g.logMu.Unlock()
	f, err := os.Open(g.HackLog)
	if err != nil {
		return nil
	}
	defer f.Close()
	// the end of a long log is enough
	partial := false
	if fi, err := f.Stat(); err == nil && fi.Size() > 256<<10 {
		f.Seek(fi.Size()-256<<10, 0)
		partial = true
	}
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if partial {
			partial = false // the first line, cut
			continue
		}
		out = append(out, sc.Text())
		if len(out) > n {
			out = out[1:]
		}
	}
	return out
}
