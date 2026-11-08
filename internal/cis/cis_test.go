package cis

import "testing"

func TestPPN(t *testing.T) {
	for _, c := range []struct {
		id  int64
		ppn string
	}{{1, "72000,1"}, {8, "72000,10"}, {4095, "72000,7777"}, {4096, "72001,0"}, {100000, "72030,3240"}} {
		if got := PPN(c.id); got != c.ppn {
			t.Errorf("PPN(%d) = %s, want %s", c.id, got, c.ppn)
		}
		if id, ok := ParsePPN(c.ppn); !ok || id != c.id {
			t.Errorf("ParsePPN(%s) = %d %v", c.ppn, id, ok)
		}
	}
	for _, bad := range []string{"70000,123", "72000,8", "72000,0", "alice", "72000;1", "720001"} {
		if id, ok := ParsePPN(bad); ok {
			t.Errorf("ParsePPN(%s) = %d", bad, id)
		}
	}
	if id, ok := ParsePPN("72000.1"); !ok || id != 1 {
		t.Error("72000.1")
	}
}

func TestParseUserID(t *testing.T) {
	u := ParseUserID("+72000,1/INT:60 /secure:1")
	if u.ID != "72000,1" || !u.Secure {
		t.Errorf("%+v", u)
	}
	u = ParseUserID("73237,341;TTY")
	if u.ID != "73237,341" || !u.TTY {
		t.Errorf("%+v", u)
	}
	u = ParseUserID("70000,123*NAVIG")
	if u.ID != "70000,123" || len(u.Opts) != 1 {
		t.Errorf("%+v", u)
	}
	u = ParseUserID("alice/GO:RATES")
	if u.ID != "alice" || u.Go != "RATES" {
		t.Errorf("%+v", u)
	}
}

func TestPages(t *testing.T) {
	ps := DefaultPages()
	if p := ps.Get("cis"); p == nil || p.ID != "TOP" || len(p.Items) != 12 {
		t.Fatalf("TOP: %+v", p)
	}
	if p := ps.Get("help"); p == nil || p.ID != "CIS-162" || p.Menu() {
		t.Errorf("HELP: %+v", p)
	}
	if ps.Parent(ps.Get("RATES")).ID != "MEMBER" {
		t.Error("RATES's parent")
	}
	for _, bad := range []string{
		":TOP\n 1 Nowhere > NOWHERE\n",
		":TOP\n 2 Wrong number > TOP\n",
		":X\n",
		":TOP\n:TOP\n",
		":TOP A\n:B A\n",
	} {
		if _, err := ParsePages(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if h := heading(ps.Get("TOP"), 80); len(h) != 80 {
		t.Errorf("heading %q", h)
	}
}
