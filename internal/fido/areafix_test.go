package fido

import (
	"strings"
	"testing"
)

// TestAreaFix: a node asks its hub's AreaFix for echoes; the hub links
// it, says so by netmail, and sends it what is posted there; the node,
// with create = yes, makes the area it lacked.
func TestAreaFix(t *testing.T) {
	down := &Link{Name: "n200", Addr: mustAddr("21:1/200"), Password: "pw", Echoes: "*",
		AreaFix: true, AreafixPassword: "FIXIT"}
	hub := newSystem(t, "21:1/100", down)
	gen, _ := hub.f.B.Store.CreateArea("Gen", "General chat", false)
	hub.f.B.Store.SetEchoTag(gen.ID, "FSX_GEN")
	up := &Link{Name: "hub", Addr: mustAddr("21:1/100"), Host: hub.host, Password: "pw", Echoes: "*",
		AreafixPassword: "FIXIT", Create: true, Route: true}
	n200 := newSystem(t, "21:1/200", up)
	for _, s := range []*system{hub, n200} {
		s.f.ScanAll()
	}
	// linked to nothing yet: nothing goes to it
	if tags, _ := hub.f.Carried(down, Echoes); len(tags) != 0 {
		t.Fatalf("carried before: %v", tags)
	}

	if err := n200.f.FixRequest(up, Echoes, []string{"%LIST", "+FSX_GEN", "+NOPE", "TEST"}); err != nil {
		t.Fatal(err)
	}
	n200.call(t, up, hub)
	if tags, _ := hub.f.Carried(down, Echoes); strings.Join(tags, " ") != "FSX_GEN TEST" {
		t.Fatalf("carried: %v", tags)
	}
	// the answer, by netmail, to the node's sysop
	n200.call(t, up, hub)
	sysop, _ := n200.f.B.Store.UserByName("sysop")
	in, _ := n200.f.B.Store.Inbox(sysop.ID)
	if len(in) != 1 || in[0].From != "AreaFix@21:1/100" || in[0].Subject != "AreaFix reply" {
		t.Fatalf("inbox: %+v", in)
	}
	for _, want := range []string{"Available (* linked):", "  FSX_GEN", "FSX_GEN: linked", "NOPE: no such echo here", "TEST: linked"} {
		if !strings.Contains(in[0].Body, want) {
			t.Errorf("no %q in the answer:\n%s", want, in[0].Body)
		}
	}

	// echomail in FSX_GEN at the hub reaches the node, which makes the area
	if _, err := hub.node(t, "alice").Post(gen, "All", "Kia ora", []string{"From the hub"}, 0); err != nil {
		t.Fatal(err)
	}
	n200.call(t, up, hub)
	a, err := n200.f.B.Store.AreaByEchoTag("FSX_GEN")
	if err != nil {
		t.Fatalf("no area made: %v\n%s", err, n200.log)
	}
	if ms, _ := n200.f.B.Store.Messages(a.ID, 0, 10); len(ms) != 1 || ms[0].Subject != "Kia ora" {
		t.Fatalf("FSX_GEN at the node: %+v", ms)
	}

	// unlinked: no more
	n200.f.FixRequest(up, Echoes, []string{"-ALL"})
	n200.call(t, up, hub)
	if tags, _ := hub.f.Carried(down, Echoes); len(tags) != 0 {
		t.Fatalf("carried after -ALL: %v", tags)
	}
	hub.node(t, "alice").Post(gen, "All", "Anyone?", []string{"x"}, 0)
	if n, _ := hub.f.Scan(down); n != 1 { // only AreaFix's answer
		t.Errorf("packed %d for the node", n)
	}

	// a wrong password does nothing
	up.AreafixPassword = "WRONG"
	n200.f.FixRequest(up, Echoes, []string{"+*"})
	n200.call(t, up, hub)
	if tags, _ := hub.f.Carried(down, Echoes); len(tags) != 0 {
		t.Fatalf("carried after a wrong password: %v", tags)
	}
	if !strings.Contains(hub.log.String(), "AreaFix request from n200: wrong password") {
		t.Error("not logged")
	}
}
