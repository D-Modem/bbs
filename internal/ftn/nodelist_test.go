package ftn

import (
	"strings"
	"testing"
)

const sampleNodelist = ";A fsxNet Nodelist for Friday, October 10, 2026 -- Day number 283 : 12345\r\n" +
	";S This list is a test\r\n" +
	"Zone,21,fsxNet_Zone_21,Wellington_NZ,Paul_Hayton,-Unpublished-,300,CM,IBN,INA:agency.bbs.nz\r\n" +
	"Host,1,fsxNet_Net_1,Wellington_NZ,Paul_Hayton,-Unpublished-,300,CM\r\n" +
	",100,Agency_HUB,Wellington_NZ,Paul_Hayton,-Unpublished-,300,CM,IBN:agency.bbs.nz:24556\r\n" +
	",101,Black_Flag,Sydney_AU,Nick_Andre,-Unpublished-,300,CM,INA:blackflag.acid.org,IBN\r\n" +
	"Hold,102,Quiet_Place,Nowhere,Some_One,-Unpublished-,300,CM\r\n" +
	"Down,103,Gone_BBS,Nowhere,Some_Body,-Unpublished-,300,IBN:gone.example\r\n" +
	"Pvt,104,Private_BBS,Somewhere,Pri_Vate,-Unpublished-,300,MO\r\n" +
	"Boss,21:1/100\r\n" +
	"Host,2,fsxNet_Net_2,Auckland_NZ,Other_Host,-Unpublished-,300\r\n" +
	",1,Two_One,Auckland_NZ,Two_Sysop,64-9-555-1212,33600,V34,IBN:192.0.2.7\r\n" +
	"junk line without enough fields\r\n" +
	"\x1a"

func TestNodelist(t *testing.T) {
	l := NewNodelist()
	if err := l.Read(strings.NewReader(sampleNodelist)); err != nil {
		t.Fatal(err)
	}
	if l.Len() != 9 || !l.Covers(21) || l.Covers(1) {
		t.Fatalf("len %d, zones %v", l.Len(), l.Zones())
	}
	n, ok := l.Lookup(Addr{Zone: 21, Net: 1, Node: 100, Point: 3})
	if !ok || n.Name != "Agency HUB" || n.Sysop != "Paul Hayton" || n.Kind != "" {
		t.Fatalf("1/100: %+v", n)
	}
	for _, c := range []struct {
		node int
		net  int
		want string
	}{
		{100, 1, "agency.bbs.nz:24556"},
		{101, 1, "blackflag.acid.org:24554"},
		{102, 1, ""},
		{1, 2, "192.0.2.7:24554"},
	} {
		n, _ := l.Lookup(Addr{Zone: 21, Net: c.net, Node: c.node})
		got, _ := n.Binkp()
		if got != c.want {
			t.Errorf("%d/%d: binkp %q, want %q", c.net, c.node, got, c.want)
		}
	}
	if n, _ := l.Lookup(Addr{Zone: 21, Net: 1, Node: 103}); !n.Down() {
		t.Error("103 is down")
	}
	if n, _ := l.Lookup(Addr{Zone: 21, Net: 1, Node: 102}); n.Kind != "Hold" || n.Down() {
		t.Errorf("102: %+v", n)
	}
	if _, ok := l.Lookup(Addr{Zone: 21, Net: 1, Node: 999}); ok {
		t.Error("999 found")
	}
	if got := l.Search("nz hub", 10); len(got) != 1 || got[0].Addr.Node != 100 {
		t.Errorf("search: %+v", got)
	}
	if got := l.Search("21:2/1", 10); len(got) != 1 || got[0].Name != "Two One" {
		t.Errorf("search by address: %+v", got)
	}
	if got := l.Search("", 3); len(got) != 3 {
		t.Errorf("search all, 3: %d", len(got))
	}
}
