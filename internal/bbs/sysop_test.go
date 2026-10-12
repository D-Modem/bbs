package bbs

import (
	"strings"
	"testing"
)

func TestWatchSummonAvailable(t *testing.T) {
	b, ns := chatBBS(t)
	alice, bob, sysop := ns["alice"], ns["bob"], ns["sysop"]
	if _, _, err := bob.Watch(alice.Num); err != ErrNotSysop {
		t.Errorf("bob watches: %v", err)
	}
	if _, _, err := sysop.Watch(alice.Num); err != ErrNoEcho {
		t.Errorf("no echo yet: %v", err)
	}
	if _, _, err := sysop.Watch(sysop.Num); err != ErrSelf {
		t.Errorf("oneself: %v", err)
	}
	alice.SetEchoing()
	alice.Echo([]byte("before"))
	ch, stop, err := sysop.Watch(alice.Num)
	if err != nil {
		t.Fatal(err)
	}
	alice.Echo([]byte("hello"))
	if got := string(<-ch); got != "hello" {
		t.Errorf("watched %q", got)
	}
	stop()
	alice.Echo([]byte("after"))
	select {
	case p := <-ch:
		t.Errorf("after stop: %q", p)
	default:
	}

	room, err := sysop.Summon(alice.Num)
	if err != nil || room != "sysop-1" {
		t.Fatalf("summon %q %v", room, err)
	}
	if _, err := bob.Summon(alice.Num); err != ErrNotSysop {
		t.Errorf("bob summons: %v", err)
	}
	if ev := alice.Pending(); len(ev) != 1 || !strings.Contains(ev[0].String(), "would like a word") {
		t.Errorf("told %v", ev)
	}
	if alice.Summoned() != "sysop-1" || alice.Summoned() != "" {
		t.Error("summoned once")
	}
	if _, err := alice.JoinRoom(room); err != nil {
		t.Errorf("alice into the room: %v", err)
	}
	if _, err := sysop.JoinRoom(room); err != nil {
		t.Errorf("the sysop into the room: %v", err)
	}

	// pages reach no one while the sysop is away
	b.SetSysopAvailable(false)
	if _, told, err := bob.PageSysop("help"); err != nil || told != 0 {
		t.Errorf("away: %d %v", told, err)
	}
	b.SetSysopAvailable(true)
	bob.CancelPage()
	if !b.SysopAvailable() {
		t.Error("available")
	}
}
