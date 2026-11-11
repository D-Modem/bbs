package dnsd

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

// TestRecords: the sysop's records are answered as they are, to anyone,
// before the services' names and before anything upstream; a CNAME is
// followed to a target here; a wildcard takes the name asked.
func TestRecords(t *testing.T) {
	up, n := fakeUpstream(t)
	s := newTest(t, up)
	var rrs []dns.RR
	for _, r := range []struct{ name, typ, value string }{
		{"games.bbs.example", "", "192.168.77.20"},
		{"games.bbs.example", "", "fd00::20"},
		{"games.bbs.example", "TXT", `Play "LORD" here`},
		{"bbs.example", "MX", "10 mail.bbs.example"},
		{"www.bbs.example", "CNAME", "games.bbs.example"},
		{"*.lan.example", "A", "10.0.0.9"},
		{"messenger.hotmail.com", "", "192.168.77.30"}, // over the service's own answer
		{"login.live.com", "TXT", "hello"},             // a type the service has not: its A stays
	} {
		rr, err := NewRecord(r.name, r.typ, r.value, 300)
		if err != nil {
			t.Fatalf("%v: %v", r, err)
		}
		rrs = append(rrs, rr)
	}
	s.S.Records = rrs
	s.recs = NewRecords(rrs)
	outsider := net.IPv4(203, 0, 113, 9) // not allowed to recurse
	ask := func(name string, qtype uint16) *dns.Msg {
		t.Helper()
		r := s.Answer(query(name, qtype), outsider, local)
		if r.Rcode != dns.RcodeSuccess || !r.Authoritative {
			t.Fatalf("%s %s: %v", name, dns.TypeToString[qtype], r)
		}
		return r
	}
	if r := ask("Games.BBS.example", dns.TypeA); len(r.Answer) != 1 || !r.Answer[0].(*dns.A).A.Equal(net.IPv4(192, 168, 77, 20)) ||
		r.Answer[0].Header().Name != "Games.BBS.example." {
		t.Errorf("games A: %v", r.Answer)
	}
	if r := ask("games.bbs.example", dns.TypeAAAA); len(r.Answer) != 1 {
		t.Errorf("games AAAA: %v", r.Answer)
	}
	if r := ask("games.bbs.example", dns.TypeTXT); len(r.Answer) != 1 || r.Answer[0].(*dns.TXT).Txt[0] != `Play \"LORD\" here` {
		t.Errorf("games TXT: %v", r.Answer)
	}
	if r := ask("bbs.example", dns.TypeMX); len(r.Answer) != 1 || r.Answer[0].(*dns.MX).Mx != "mail.bbs.example." {
		t.Errorf("MX: %v", r.Answer)
	}
	if r := ask("bbs.example", dns.TypeA); len(r.Answer) != 0 {
		t.Errorf("bbs.example A: %v, want none", r.Answer)
	}
	if r := ask("www.bbs.example", dns.TypeA); len(r.Answer) != 2 || r.Answer[0].Header().Rrtype != dns.TypeCNAME {
		t.Errorf("www A: %v, want the CNAME and its A", r.Answer)
	}
	if r := ask("pc1.lan.example", dns.TypeA); len(r.Answer) != 1 || r.Answer[0].Header().Name != "pc1.lan.example." {
		t.Errorf("wildcard: %v", r.Answer)
	}
	if r := ask("messenger.hotmail.com", dns.TypeA); !r.Answer[0].(*dns.A).A.Equal(net.IPv4(192, 168, 77, 30)) {
		t.Errorf("messenger: %v, want the record's", r.Answer)
	}
	if r := ask("login.live.com", dns.TypeA); len(r.Answer) != 1 || !r.Answer[0].(*dns.A).A.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("login.live.com A: %v, want the service's", r.Answer)
	}
	if n.Load() != 0 {
		t.Errorf("%d asked upstream", n.Load())
	}
	for _, bad := range []struct{ name, typ, value string }{
		{"x.example", "", "not-an-ip"}, {"x.example", "BOGUS", "1"}, {"x.example", "MX", "mail.example"},
		{"a b.example", "A", "10.0.0.1"}, {"x.example", "A", ""}, {"x.example", "SOA", "a b 1 2 3 4 5"},
	} {
		if _, err := NewRecord(bad.name, bad.typ, bad.value, 60); err == nil {
			t.Errorf("%v: taken", bad)
		}
	}
}
