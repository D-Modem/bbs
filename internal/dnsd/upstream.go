package dnsd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// ResolvConf is where the host's DNS servers are read, and
// systemd-resolved's list of the servers it follows itself: used instead
// when the first lists only its stub (127.0.0.53).
var (
	ResolvConf         = "/etc/resolv.conf"
	ResolvedResolvConf = "/run/systemd/resolve/resolv.conf"
)

// upstreams are the servers asked: the sysop's primary and backup, or the
// host's, read again when its resolv.conf changes.
type upstreams struct {
	fixed []string // the sysop's; nil: the host's
	self  []string // our own addresses, never asked (a loop)

	mu      sync.Mutex
	host    []string
	file    string
	modTime time.Time
	checked time.Time
}

// servers are the servers to ask, in order.
func (u *upstreams) servers() ([]string, error) {
	if u.fixed != nil {
		return u.fixed, nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if time.Since(u.checked) < 10*time.Second && u.host != nil {
		return u.host, nil
	}
	u.checked = time.Now()
	file, servers, mod, err := HostServers(u.self)
	if err != nil {
		if u.host != nil {
			return u.host, nil // the last good list
		}
		return nil, err
	}
	if file != u.file || !mod.Equal(u.modTime) {
		u.file, u.modTime, u.host = file, mod, servers
	}
	return u.host, nil
}

// HostServers are the host's DNS servers (host:port), from resolv.conf
// (systemd-resolved's own list when resolv.conf has only its stub), less
// any of self (the DNS server's own addresses: asking itself would loop).
func HostServers(self []string) (file string, servers []string, mod time.Time, err error) {
	read := func(path string) ([]string, time.Time, error) {
		st, err := os.Stat(path)
		if err != nil {
			return nil, time.Time{}, err
		}
		cc, err := dns.ClientConfigFromFile(path)
		if err != nil {
			return nil, time.Time{}, err
		}
		var out []string
		for _, s := range cc.Servers {
			out = append(out, net.JoinHostPort(s, cc.Port))
		}
		return out, st.ModTime(), nil
	}
	file = ResolvConf
	servers, mod, err = read(file)
	if err != nil {
		return file, nil, mod, fmt.Errorf("the host's DNS servers: %w", err)
	}
	stubOnly := len(servers) > 0
	for _, s := range servers {
		h, _, _ := net.SplitHostPort(s)
		stubOnly = stubOnly && (h == "127.0.0.53" || h == "127.0.0.54")
	}
	if stubOnly {
		if rs, rmod, rerr := read(ResolvedResolvConf); rerr == nil && len(rs) > 0 {
			file, servers, mod = ResolvedResolvConf, rs, rmod
		}
	}
	var kept []string
	for _, s := range servers {
		if !isSelf(s, self) {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		return file, nil, mod, fmt.Errorf("the host's DNS servers (%s) are none but this one: "+
			"give [dns] primary (and backup)", file)
	}
	return file, kept, mod, nil
}

// isSelf: server (host:port) is one of our own addresses.
func isSelf(server string, self []string) bool {
	sh, sp, err := net.SplitHostPort(server)
	if err != nil {
		return false
	}
	sip := net.ParseIP(sh)
	for _, a := range self {
		h, p, err := net.SplitHostPort(a)
		if err != nil || p != sp {
			continue
		}
		ip := net.ParseIP(h)
		switch {
		case h == "" || ip != nil && ip.IsUnspecified():
			// all addresses: any of the machine's, loopback included
			if sip != nil && (sip.IsLoopback() || localAddress(sip)) {
				return true
			}
		case ip != nil && sip != nil && ip.Equal(sip):
			return true
		}
	}
	return false
}

// localAddress: ip is one of this machine's.
func localAddress(ip net.IP) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// ServerAddress is a server given as host, host:port or [v6]:port, with
// port 53 when none is said.
func ServerAddress(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("empty")
	}
	if ip := net.ParseIP(strings.Trim(s, "[]")); ip != nil {
		return net.JoinHostPort(ip.String(), "53"), nil
	}
	h, p, err := net.SplitHostPort(s)
	if err != nil {
		return "", fmt.Errorf("%q: an IP address, with :port if not 53", s)
	}
	if net.ParseIP(h) == nil {
		return "", fmt.Errorf("%q: an IP address (a name would need resolving itself)", s)
	}
	return net.JoinHostPort(h, p), nil
}

// exchange asks the servers in turn until one answers (by UDP, and again
// by TCP when the answer was cut short).  A server that answers SERVFAIL
// or REFUSED is passed over for the next; with none left, the last
// answer, or the last error.
func exchange(servers []string, q *dns.Msg, timeout time.Duration) (*dns.Msg, string, error) {
	udp := &dns.Client{Net: "udp", Timeout: timeout, UDPSize: dns.DefaultMsgSize}
	tcp := &dns.Client{Net: "tcp", Timeout: timeout}
	var last *dns.Msg
	var lastFrom string
	err := errors.New("no DNS servers")
	for _, s := range servers {
		r, _, e := udp.Exchange(q, s)
		if e == nil && r.Truncated {
			r, _, e = tcp.Exchange(q, s)
		}
		if e != nil {
			err = fmt.Errorf("%s: %w", s, e)
			continue
		}
		if r.Rcode == dns.RcodeServerFailure || r.Rcode == dns.RcodeRefused {
			last, lastFrom = r, s
			continue
		}
		return r, s, nil
	}
	if last != nil {
		return last, lastFrom, nil
	}
	return nil, "", err
}
