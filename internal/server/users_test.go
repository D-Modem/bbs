package server

import (
	"strings"
	"testing"
)

// TestExpertMenus: an expert's menu is its prompt; ? shows the rest.
func TestExpertMenus(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	d := login(e, "alice", "alicepass1")
	d.send("P")
	d.expect("Menus:     in full")
	d.expect("[X] Expert menus on/off")
	d.send("X")
	d.expect("Menus:     expert (the prompt alone)")
	d.send("Q")
	d.expect("Command (? for the menu): ")
	if strings.Contains(string(d.got), "[M] Message areas") {
		t.Errorf("an expert got the menu: %q", d.got)
	}
	d.send("?")
	d.expect("[M] Message areas")
	d.expect("[L] Last callers")
	d.expect("Command (? for the menu): ")
	d.send("L")
	d.expect("Last callers")
	d.expect("alice")
}
