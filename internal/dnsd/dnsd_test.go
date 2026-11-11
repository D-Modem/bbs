package dnsd

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// fakeUpstream is a DNS server answering every A query with 192.0.2.7
// (TTL 300), and nx.example. with NXDOMAIN; it counts the queries.
func fakeUpstream(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var n atomic.Int32
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
		n.Add(1)
		r := new(dns.Msg)
		r.SetReply(req)
		q := req.Question[0]
		switch {
		case q.Name == "nx.example.":
			r.Rcode = dns.RcodeNameError
			r.Ns = append(r.Ns, &dns.SOA{Hdr: dns.RR_Header{Name: "example.", Rrtype: dns.TypeSOA,
				Class: dns.ClassINET, Ttl: 600}, Ns: "ns.example.", Mbox: "h.example.", Minttl: 120})
		case q.Qtype == dns.TypeA:
			r.Answer = append(r.Answer, &dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA,
				Class: dns.ClassINET, Ttl: 300}, A: net.IPv4(192, 0, 2, 7)})
		}
		w.WriteMsg(r)
	})}
	go srv.ActivateAndServe()
	t.Cleanup(func() { srv.Shutdown() })
	return pc.LocalAddr().String(), &n
}

func query(name string, qtype uint16) *dns.Msg {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	return m
}

func newTest(t *testing.T, ups ...string) *Server {
	t.Helper()
	allow, err := ParseNetworks(DefaultAllow)
	if err != nil {
		t.Fatal(err)
	}
	return New(Settings{Address: "127.0.0.1:0", Upstreams: ups, Services: ServiceNames(),
		Names: []string{"*.retro.example"}, Allow: allow, CacheSize: 100, Timeout: 500 * time.Millisecond}, nil)
}

var local = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53}

func TestLegacyNames(t *testing.T) {
	up, n := fakeUpstream(t)
	s := newTest(t, up)
	for _, name := range []string{"messenger.hotmail.com", "Login.Live.COM", "login.oscar.aol.com", "scs.msg.yahoo.com",
		"AmericaOnline.aol.com", "gateway.moswest.msn.net", "compuserve.com", "bbs.retro.example"} {
		r := s.Answer(query(name, dns.TypeA), net.IPv4(192, 168, 77, 2), local)
		if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 1 {
			t.Fatalf("%s: %v", name, r)
		}
		if a := r.Answer[0].(*dns.A).A; !a.Equal(net.IPv4(127, 0, 0, 1)) {
			t.Errorf("%s: %v, want the address the query came to", name, a)
		}
		if !r.Authoritative {
			t.Errorf("%s: not authoritative", name)
		}
	}
	// HTTPS records and the like: none, and not asked upstream
	r := s.Answer(query("login.live.com", dns.TypeHTTPS), net.IPv4(192, 168, 77, 2), local)
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 0 {
		t.Errorf("HTTPS: %v", r)
	}
	if n.Load() != 0 {
		t.Errorf("upstream asked %d times for names answered here", n.Load())
	}
	// a name beside them is not ours
	if s.Table().Match("www.hotmail.com") != "" || s.Table().Match("retro.example") != "" {
		t.Error("matched too much")
	}
}

func TestAnswerAddress(t *testing.T) {
	s := New(Settings{Address: ":53", Services: []string{"msn"}, Answer: net.IPv4(192, 168, 77, 1)}, nil)
	r := s.Answer(query("messenger.hotmail.com", dns.TypeA), net.IPv4(203, 0, 113, 9), local)
	if len(r.Answer) != 1 || !r.Answer[0].(*dns.A).A.Equal(net.IPv4(192, 168, 77, 1)) {
		t.Fatalf("A: %v", r)
	}
	r = s.Answer(query("messenger.hotmail.com", dns.TypeAAAA), net.IPv4(203, 0, 113, 9), local)
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 0 {
		t.Fatalf("AAAA with only answer set: %v", r)
	}
	// not a service that is on: not answered here, and not allowed to resolve
	r = s.Answer(query("scs.msg.yahoo.com", dns.TypeA), net.IPv4(203, 0, 113, 9), local)
	if r.Rcode != dns.RcodeRefused {
		t.Fatalf("outsider: %v", r)
	}
	// the socket on every address: the address the system answers from
	ip := reachedAt(net.IPv4(127, 0, 0, 1), &net.UDPAddr{IP: net.IPv4zero, Port: 53})
	if !ip.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("reachedAt: %v", ip)
	}
}

func TestForwardAndCache(t *testing.T) {
	up, n := fakeUpstream(t)
	s := newTest(t, up)
	client := net.IPv4(192, 168, 77, 2)
	t0 := time.Now()
	s.cache.now = func() time.Time { return t0 }
	r := s.Answer(query("www.example.org", dns.TypeA), client, local)
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 1 || !r.RecursionAvailable {
		t.Fatalf("forwarded: %v", r)
	}
	s.cache.now = func() time.Time { return t0.Add(100 * time.Second) }
	q := query("WWW.example.org", dns.TypeA)
	r = s.Answer(q, client, local)
	if n.Load() != 1 {
		t.Fatalf("upstream asked %d times, want 1 (the second from the cache)", n.Load())
	}
	if r.Id != q.Id || r.Question[0].Name != "WWW.example.org." {
		t.Errorf("cached answer not for the query: %v", r)
	}
	if ttl := r.Answer[0].Header().Ttl; ttl != 200 {
		t.Errorf("TTL %d, want 200", ttl)
	}
	s.cache.now = func() time.Time { return t0.Add(301 * time.Second) }
	s.Answer(query("www.example.org", dns.TypeA), client, local)
	if n.Load() != 2 {
		t.Fatalf("expired answer kept: upstream asked %d times", n.Load())
	}
	// NXDOMAIN is kept for the SOA's minimum
	s.cache.now = func() time.Time { return t0 }
	for range 2 {
		r = s.Answer(query("nx.example", dns.TypeA), client, local)
		if r.Rcode != dns.RcodeNameError {
			t.Fatalf("nx: %v", r)
		}
	}
	if n.Load() != 3 {
		t.Fatalf("NXDOMAIN not kept: upstream asked %d times", n.Load())
	}
	// an outsider may not resolve
	r = s.Answer(query("www.example.org", dns.TypeA), net.IPv4(203, 0, 113, 9), local)
	if r.Rcode != dns.RcodeRefused {
		t.Fatalf("outsider: %v", r)
	}
}

func TestBackup(t *testing.T) {
	// the primary: a port nothing answers on
	dead, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.LocalAddr().String()
	dead.Close()
	up, n := fakeUpstream(t)
	s := newTest(t, deadAddr, up)
	r := s.Answer(query("www.example.org", dns.TypeA), net.IPv4(127, 0, 0, 1), local)
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 1 || n.Load() != 1 {
		t.Fatalf("backup not used: %v", r)
	}
	// none answering: SERVFAIL
	s = newTest(t, deadAddr)
	r = s.Answer(query("www.example.org", dns.TypeA), net.IPv4(127, 0, 0, 1), local)
	if r.Rcode != dns.RcodeServerFailure {
		t.Fatalf("no upstream: %v", r)
	}
}

func TestServe(t *testing.T) {
	up, _ := fakeUpstream(t)
	s := newTest(t, up)
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- s.Serve(ctx, pc, ln) }()
	defer func() {
		cancel()
		<-done
	}()
	time.Sleep(50 * time.Millisecond)
	for _, netw := range []string{"udp", "tcp"} {
		c := &dns.Client{Net: netw, Timeout: time.Second}
		r, _, err := c.Exchange(query("login.yahoo.com", dns.TypeA), pc.LocalAddr().String())
		if err != nil {
			t.Fatalf("%s: %v", netw, err)
		}
		if len(r.Answer) != 1 || !r.Answer[0].(*dns.A).A.Equal(net.IPv4(127, 0, 0, 1)) {
			t.Fatalf("%s: %v", netw, r)
		}
		r, _, err = c.Exchange(query("www.example.org", dns.TypeA), pc.LocalAddr().String())
		if err != nil || len(r.Answer) != 1 {
			t.Fatalf("%s forwarded: %v %v", netw, r, err)
		}
	}
}

func TestHostServers(t *testing.T) {
	dir := t.TempDir()
	defer func(a, b string) { ResolvConf, ResolvedResolvConf = a, b }(ResolvConf, ResolvedResolvConf)
	ResolvConf = filepath.Join(dir, "resolv.conf")
	ResolvedResolvConf = filepath.Join(dir, "resolved.conf")
	os.WriteFile(ResolvConf, []byte("nameserver 127.0.0.53\noptions edns0\n"), 0o644)
	os.WriteFile(ResolvedResolvConf, []byte("nameserver 9.9.9.9\nnameserver 1.1.1.1\n"), 0o644)
	file, servers, _, err := HostServers([]string{":53"})
	if err != nil || file != ResolvedResolvConf || len(servers) != 2 || servers[0] != "9.9.9.9:53" {
		t.Fatalf("stub: %s %v %v", file, servers, err)
	}
	os.WriteFile(ResolvConf, []byte("nameserver 127.0.0.1\nnameserver 8.8.8.8\n"), 0o644)
	_, servers, _, err = HostServers([]string{":53"})
	if err != nil || len(servers) != 1 || servers[0] != "8.8.8.8:53" {
		t.Fatalf("ourselves not left out: %v %v", servers, err)
	}
	os.WriteFile(ResolvConf, []byte("nameserver 127.0.0.1\n"), 0o644)
	if _, _, _, err := HostServers([]string{"0.0.0.0:53"}); err == nil {
		t.Fatal("only ourselves: no error")
	}
	if _, _, _, err := HostServers([]string{"192.168.77.1:53"}); err != nil {
		t.Fatalf("another address's server: %v", err)
	}
}

func TestServerAddress(t *testing.T) {
	for in, want := range map[string]string{"1.1.1.1": "1.1.1.1:53", "1.1.1.1:5353": "1.1.1.1:5353",
		"2606:4700::1111": "[2606:4700::1111]:53", "[2606:4700::1111]:53": "[2606:4700::1111]:53"} {
		if got, err := ServerAddress(in); err != nil || got != want {
			t.Errorf("%s: %s %v", in, got, err)
		}
	}
	if _, err := ServerAddress("dns.google"); err == nil {
		t.Error("a name taken")
	}
}

func TestServiceAnswers(t *testing.T) {
	s := New(Settings{Address: ":53", Services: []string{"msn", "ymsg"}, Answer: net.IPv4(192, 168, 77, 1),
		ServiceAnswers: map[string][]net.IP{"msn": {net.IPv4(192, 168, 77, 11), net.ParseIP("fd00::11")}}}, nil)
	for name, want := range map[string]string{"login.live.com": "192.168.77.11", "messenger.hotmail.com": "192.168.77.11",
		"login.yahoo.com": "192.168.77.1"} {
		r := s.Answer(query(name, dns.TypeA), net.IPv4(192, 168, 77, 2), local)
		if len(r.Answer) != 1 || r.Answer[0].(*dns.A).A.String() != want {
			t.Errorf("%s: %v, want %s", name, r, want)
		}
	}
	r := s.Answer(query("login.live.com", dns.TypeAAAA), net.IPv4(192, 168, 77, 2), local)
	if len(r.Answer) != 1 || r.Answer[0].(*dns.AAAA).AAAA.String() != "fd00::11" {
		t.Errorf("AAAA: %v", r)
	}
}
