package bbs

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestPermanentRooms(t *testing.T) {
	b, ns := chatBBS(t)
	alice, sysop := ns["alice"], ns["sysop"]
	if err := b.AddPermanentRoom("Lobby", "Say hello", "sysop"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"no way", "sysop-1"} {
		if err := b.AddPermanentRoom(bad, "", "sysop"); err != ErrNoRoom {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if err := b.AddPermanentRoom("lobby", "", "sysop"); err == nil {
		t.Error("made permanent twice")
	}
	// open with no one in it
	if rs := alice.Rooms(); len(rs) != 1 || rs[0].Name != "lobby" || !rs[0].Permanent || rs[0].Topic != "Say hello" {
		t.Errorf("rooms %+v", rs)
	}
	hist, err := alice.JoinRoom("lobby")
	if err != nil || len(hist) != 1 || hist[0].String() != "Topic: Say hello" {
		t.Fatalf("join: %v %v", hist, err)
	}
	alice.LeaveRoom()
	if rs := b.PermanentRooms(); len(rs) != 1 {
		t.Errorf("gone when empty: %v", rs)
	}

	// the topic, from chat
	alice.JoinRoom("lobby")
	if _, _, err := alice.ChatCommand("/topic nope"); err == nil {
		t.Error("alice set the topic")
	}
	if out, _, _ := alice.ChatCommand("/topic"); strings.Join(out, "") != "Topic: Say hello" {
		t.Errorf("/topic: %q", out)
	}
	sysop.JoinRoom("lobby")
	alice.Pending()
	if _, _, err := sysop.ChatCommand("/topic Be kind"); err != nil {
		t.Fatal(err)
	}
	if ev := alice.Pending(); len(ev) != 1 || ev[0].String() != "sysop set the topic: Be kind" {
		t.Errorf("told %v", ev)
	}
	if out, _, _ := alice.ChatCommand("/rooms"); !strings.Contains(strings.Join(out, "\n"), "Be kind") {
		t.Errorf("/rooms: %q", out)
	}
	if err := b.SetRoomTopic("elsewhere", "x", "sysop"); err != ErrNotPermanent {
		t.Errorf("a room not permanent: %v", err)
	}

	// kept across a restart: a new BBS on the same store
	b2 := New(Defaults(), b.Store, log.New(&bytes.Buffer{}, "", 0))
	if rs := b2.PermanentRooms(); len(rs) != 1 || rs[0].Topic != "Be kind" {
		t.Errorf("after a restart %+v", rs)
	}

	// no longer permanent: it goes with its last caller
	if err := b.RemovePermanentRoom("lobby", "sysop"); err != nil {
		t.Fatal(err)
	}
	if err := b.RemovePermanentRoom("lobby", "sysop"); err != ErrNotPermanent {
		t.Errorf("again: %v", err)
	}
	alice.LeaveRoom()
	sysop.LeaveRoom()
	if rs := alice.Rooms(); len(rs) != 0 {
		t.Errorf("rooms %v", rs)
	}
}
