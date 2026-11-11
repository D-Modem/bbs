// Package dnsd is the BBS's DNS server, for its callers (dial-up users
// above all, whose PPP link names it as their DNS server).  The names of
// the old services' servers whose front ends the BBS has (MSN Messenger's
// messenger.hotmail.com and Passport, AIM's login.oscar.aol.com, Yahoo!
// Messenger's scs.msg.yahoo.com, ...) are answered with the BBS's own
// address, so the clients of the time find the BBS unchanged, with no
// hosts file or registry edits.  Every other name is resolved by the
// host's DNS servers, or a primary and backup of the sysop's, and kept in
// a cache for as long as its TTL says.
package dnsd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"

	"github.com/D-Modem/bbs/internal/sockets"
)

// Settings are [dns]'s.
type Settings struct {
	Address   string
	Upstreams []string // host:port, in order (primary, backup); nil: the host's
	// Answer is the address the services' names are given (nil: the
	// BBS's address as the asker reaches it); Answer6 their IPv6 one (nil:
	// likewise, when the asker came by IPv6; none otherwise)
	Answer, Answer6 net.IP
	// ServiceAnswers are services' own addresses (an IPv4 and an IPv6 one
	// at most), given for their names instead of Answer and Answer6: for a
	// service whose HTTPS server is on an IP of its own, with a
	// certificate of its own
	ServiceAnswers map[string][]net.IP
	Services       []string // whose names are answered (keys of Services)
	Names          []string // more names answered with the BBS's address
	// Records are the sysop's own ([dnsrecord NAME]), answered as they
	// are before all else
	Records []dns.RR
	// Allow are the networks whose clients may resolve other names (the
	// services' names are answered to anyone)
	Allow     []*net.IPNet
	CacheSize int           // answers kept (0: none)
	MaxTTL    time.Duration // the longest one is kept
	NegTTL    time.Duration // the longest "no such name" is kept
	TTL       uint32        // the TTL of the answers given for the services' names
	Timeout   time.Duration // for each upstream server
	Rate      int           // UDP queries a second one address may make (0: no limit)
	Burst     int
}

// DefaultAllow are the networks that may resolve names by default: this
// machine and private networks (dial-up users' PPP addresses among them).
var DefaultAllow = []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"100.64.0.0/10", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"}

// Server answers DNS queries.
type Server struct {
	S     Settings
	Log   *log.Logger
	table *Table
	recs  *Records
	cache *cache
	up    *upstreams
	group singleflight.Group
	lim   *limiter

	logMu   sync.Mutex
	lastLog time.Time
}

// New is a server for s.
func New(s Settings, lg *log.Logger) *Server {
	if lg == nil {
		lg = log.New(nopWriter{}, "", 0)
	}
	if s.MaxTTL == 0 {
		s.MaxTTL = 24 * time.Hour
	}
	if s.NegTTL == 0 {
		s.NegTTL = 5 * time.Minute
	}
	if s.TTL == 0 {
		s.TTL = 60
	}
	if s.Timeout == 0 {
		s.Timeout = 2 * time.Second
	}
	srv := &Server{S: s, Log: lg, table: NewTable(s.Services, s.Names), recs: NewRecords(s.Records),
		cache: newCache(s.CacheSize, s.MaxTTL, s.NegTTL),
		up:    &upstreams{fixed: s.Upstreams, self: []string{s.Address}}}
	if s.Rate > 0 {
		srv.lim = &limiter{rate: float64(s.Rate), burst: float64(max(s.Burst, 1)), buckets: map[string]*bucket{}}
	}
	return srv
}

// Table is the names answered with the BBS's address.
func (s *Server) Table() *Table { return s.table }

// Run answers on Address, by UDP and TCP, until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	pc, err := sockets.ListenUDP(s.S.Address)
	if err != nil {
		return fmt.Errorf("dns: %w", err)
	}
	ln, err := sockets.Listen("tcp", s.S.Address)
	if err != nil {
		pc.Close()
		return fmt.Errorf("dns: %w", err)
	}
	return s.Serve(ctx, pc, ln)
}

// Serve answers on pc (UDP) and ln (TCP) until ctx is done.
func (s *Server) Serve(ctx context.Context, pc net.PacketConn, ln net.Listener) error {
	if _, err := s.up.servers(); err != nil {
		s.Log.Printf("dns: %v", err)
	}
	udp := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(s.serveDNS), UDPSize: dns.DefaultMsgSize}
	tcp := &dns.Server{Listener: ln, Handler: dns.HandlerFunc(s.serveDNS),
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: func() time.Duration { return 10 * time.Second }}
	errs := make(chan error, 2)
	go func() { errs <- udp.ActivateAndServe() }()
	go func() { errs <- tcp.ActivateAndServe() }()
	var err error
	select {
	case <-ctx.Done():
	case err = <-errs:
	}
	udp.Shutdown()
	tcp.Shutdown()
	if err != nil {
		return fmt.Errorf("dns: %w", err)
	}
	return nil
}

// serveDNS answers one query.
func (s *Server) serveDNS(w dns.ResponseWriter, req *dns.Msg) {
	tcp := isTCP(w.RemoteAddr())
	client := hostIP(w.RemoteAddr())
	if !tcp && s.lim != nil && client != nil && !s.lim.allow(client.String()) {
		return // dropped: a flood, or someone else's address forged
	}
	r := s.Answer(req, client, w.LocalAddr())
	if r == nil {
		return
	}
	size := dns.MinMsgSize
	if tcp {
		size = dns.MaxMsgSize
	} else if o := req.IsEdns0(); o != nil {
		size = max(int(o.UDPSize()), dns.MinMsgSize)
	}
	r.Truncate(size)
	w.WriteMsg(r)
}

// Answer is the reply to req from client, which came to local.
func (s *Server) Answer(req *dns.Msg, client net.IP, local net.Addr) *dns.Msg {
	r := new(dns.Msg)
	if req.Response {
		return nil
	}
	if req.Opcode != dns.OpcodeQuery {
		return r.SetRcode(req, dns.RcodeNotImplemented)
	}
	if len(req.Question) != 1 {
		return r.SetRcode(req, dns.RcodeFormatError)
	}
	q := req.Question[0]
	allowed := s.allowed(client)
	if ans, has := s.recs.answer(q.Name, q.Qtype); has && q.Qclass == dns.ClassINET &&
		(len(ans) > 0 || s.table.Match(q.Name) == "") {
		// the sysop's own: as they are (none of the type: no records,
		// NOERROR, so nothing upstream answers for the name)
		r.SetReply(req)
		r.Authoritative = true
		r.RecursionAvailable = allowed
		r.Answer = fixName(ans, q.Name)
		return r
	}
	if who := s.table.Match(q.Name); who != "" && q.Qclass == dns.ClassINET {
		r.SetReply(req)
		r.Authoritative = true
		r.RecursionAvailable = allowed
		if ip := s.answerFor(who, q.Qtype, client, local); ip != nil {
			hdr := dns.RR_Header{Name: q.Name, Rrtype: q.Qtype, Class: dns.ClassINET, Ttl: s.S.TTL}
			if q.Qtype == dns.TypeA {
				r.Answer = append(r.Answer, &dns.A{Hdr: hdr, A: ip.To4()})
			} else {
				r.Answer = append(r.Answer, &dns.AAAA{Hdr: hdr, AAAA: ip})
			}
		}
		// any other type (HTTPS, MX ...): there is none (NOERROR, no
		// records), so nothing upstream sends the client elsewhere
		return r
	}
	if !allowed {
		r.SetRcode(req, dns.RcodeRefused)
		return r
	}
	if c := s.cache.get(req); c != nil {
		c.Id, c.Question, c.Response = req.Id, req.Question, true
		c.RecursionAvailable = true
		return c
	}
	k := keyOf(req)
	v, err, _ := s.group.Do(fmt.Sprintf("%s|%d|%d|%t|%t", k.name, k.qtype, k.qclass, k.do, k.cd), func() (any, error) {
		return s.resolve(req)
	})
	if err != nil {
		s.logf("dns: %s %s: %v", q.Name, dns.TypeToString[q.Qtype], err)
		return r.SetRcode(req, dns.RcodeServerFailure)
	}
	out := v.(*dns.Msg).Copy()
	out.Id, out.Question = req.Id, req.Question
	return out
}

// resolve asks the upstream servers, keeping what they say.
func (s *Server) resolve(req *dns.Msg) (*dns.Msg, error) {
	servers, err := s.up.servers()
	if err != nil {
		return nil, err
	}
	q := req.Copy()
	q.Id = dns.Id()
	q.RecursionDesired = true
	q.Extra = nil
	if o := req.IsEdns0(); o != nil {
		q.SetEdns0(dns.DefaultMsgSize, o.Do())
	}
	r, _, err := exchange(servers, q, s.S.Timeout)
	if err != nil {
		return nil, err
	}
	if r.Rcode == dns.RcodeRefused {
		// the upstreams refused us: SERVFAIL to the client
		return nil, errors.New("refused by every DNS server")
	}
	r.RecursionAvailable = true
	r.Authoritative = false
	s.cache.put(req, r)
	return r, nil
}

// answerFor is the address the services' names are given, for an A or
// AAAA query (nil: none).
func (s *Server) answerFor(who string, qtype uint16, client net.IP, local net.Addr) net.IP {
	if ips := s.S.ServiceAnswers[who]; len(ips) > 0 {
		// the service's own address (its HTTPS server on an IP of its own)
		for _, ip := range ips {
			if qtype == dns.TypeA && ip.To4() != nil {
				return ip.To4()
			}
			if qtype == dns.TypeAAAA && ip.To4() == nil {
				return ip
			}
		}
		return nil
	}
	switch qtype {
	case dns.TypeA:
		if s.S.Answer != nil {
			return s.S.Answer
		}
	case dns.TypeAAAA:
		if s.S.Answer6 != nil {
			return s.S.Answer6
		}
		if s.S.Answer != nil {
			return nil // the sysop's IPv4 address only
		}
	default:
		return nil
	}
	ip := reachedAt(client, local)
	if qtype == dns.TypeA {
		if ip != nil && ip.To4() != nil {
			return ip.To4()
		}
		return firstAddress(true)
	}
	if ip != nil && ip.To4() == nil && !ip.IsLinkLocalUnicast() {
		return ip
	}
	return nil
}

// reachedAt is this machine's address as client reaches it: the address
// the query came to, or, when the socket is on every address, the one
// the system sends to client from.
func reachedAt(client net.IP, local net.Addr) net.IP {
	if ip := hostIP(local); ip != nil && !ip.IsUnspecified() {
		return ip
	}
	if client == nil {
		return nil
	}
	if v4 := client.To4(); v4 != nil {
		client = v4
	}
	c, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: client, Port: 53})
	if err != nil {
		return nil
	}
	defer c.Close()
	return hostIP(c.LocalAddr())
}

// firstAddress is this machine's first IPv4 (v4) or IPv6 address that
// is not loopback or link-local.
func firstAddress(v4 bool) net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.IsLoopback() || n.IP.IsLinkLocalUnicast() || (n.IP.To4() != nil) != v4 {
			continue
		}
		return n.IP
	}
	return nil
}

// allowed: client may resolve names other than the services'.
func (s *Server) allowed(client net.IP) bool {
	if client == nil {
		return false
	}
	for _, n := range s.S.Allow {
		if n.Contains(client) {
			return true
		}
	}
	return false
}

// logf logs, at most once a minute (a dead upstream would fill the log).
func (s *Server) logf(format string, args ...any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if time.Since(s.lastLog) < time.Minute {
		return
	}
	s.lastLog = time.Now()
	s.Log.Printf(format, args...)
}

// Cached is how many answers are kept.
func (s *Server) Cached() int { return s.cache.len() }

func isTCP(a net.Addr) bool {
	_, ok := a.(*net.TCPAddr)
	return ok
}

// hostIP is an address's IP (nil: none).
func hostIP(a net.Addr) net.IP {
	switch a := a.(type) {
	case *net.UDPAddr:
		return a.IP
	case *net.TCPAddr:
		return a.IP
	case nil:
		return nil
	}
	h, _, err := net.SplitHostPort(a.String())
	if err != nil {
		return nil
	}
	return net.ParseIP(h)
}

// ParseNetworks reads networks (CIDR, or one address) given by commas or
// spaces.
func ParseNetworks(list []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, s := range list {
		if ip := net.ParseIP(s); ip != nil {
			bits := 128
			if ip.To4() != nil {
				ip, bits = ip.To4(), 32
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("%q: a network (192.168.77.0/24) or an address", s)
		}
		out = append(out, n)
	}
	return out, nil
}

// limiter limits each address to rate queries a second, burst at once.
type limiter struct {
	rate, burst float64
	mu          sync.Mutex
	buckets     map[string]*bucket
	swept       time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func (l *limiter) allow(addr string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) > time.Minute {
		full := time.Duration(l.burst / l.rate * float64(time.Second))
		for k, b := range l.buckets {
			if now.Sub(b.at) > full {
				delete(l.buckets, k)
			}
		}
		l.swept = now
	}
	b := l.buckets[addr]
	if b == nil {
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[addr] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.at).Seconds()*l.rate)
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// String describes the settings, for bbs check.
func (s Settings) String() string {
	up := "the host's DNS servers"
	if s.Upstreams != nil {
		up = fmt.Sprint(s.Upstreams)
	}
	ans := "the address each caller reaches"
	if s.Answer != nil {
		ans = s.Answer.String()
	}
	return "dns: " + s.Address + ", " + strconv.Itoa(len(NewTable(s.Services, s.Names).exact)) +
		" names (" + fmt.Sprint(s.Services) + ") answered with " + ans + perService(s.ServiceAnswers) + ", the rest from " + up +
		records(len(s.Records)) + ", " + strconv.Itoa(s.CacheSize) + " kept"
}

// records is how many records of the sysop's there are, for String.
func records(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return ", 1 record of your own"
	}
	return ", " + strconv.Itoa(n) + " records of your own"
}

// perService describes the services' own addresses, for String.
func perService(m map[string][]net.IP) string {
	out := ""
	for _, k := range ServiceNames() {
		if ips := m[k]; len(ips) > 0 {
			out += fmt.Sprintf(" (%s: %v)", k, ips)
		}
	}
	return out
}
