package bbs

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func chatBBS(t *testing.T) (*BBS, map[string]*Node) {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := New(Defaults(), st, log.New(&bytes.Buffer{}, "", 0))
	nodes := map[string]*Node{}
	for _, u := range []struct {
		name  string
		sysop bool
	}{{"alice", false}, {"bob", false}, {"sysop", true}} {
		us, err := st.CreateUser(u.name, "password1", "", u.sysop)
		if err != nil {
			t.Fatal(err)
		}
		n, err := b.Enter(&Caller{Via: "test"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		n.SetUser(us)
		nodes[u.name] = n
	}
	return b, nodes
}

func lines(es []Event) string {
	var out []string
	for _, e := range es {
		out = append(out, e.String())
	}
	return strings.Join(out, "\n")
}

func TestChatRooms(t *testing.T) {
	_, ns := chatBBS(t)
	alice, bob, sysop := ns["alice"], ns["bob"], ns["sysop"]
	if _, err := alice.JoinRoom("Main"); err != nil {
		t.Fatal(err)
	}
	if err := alice.Say("hello \x1b[2J world", false); err != nil {
		t.Fatal(err)
	}
	hist, err := bob.JoinRoom("main")
	if err != nil || lines(hist) != "alice: hello [2J world" {
		t.Fatalf("history %q %v", lines(hist), err)
	}
	bob.Say("waves", true)
	if got := lines(alice.Pending()); got != "alice: hello [2J world\n-> bob came in\n* bob waves" {
		t.Errorf("alice saw %q", got)
	}
	if rs := sysop.Rooms(); len(rs) != 1 || rs[0].Name != "main" || len(rs[0].Users) != 2 {
		t.Errorf("rooms %+v", rs)
	}
	bob.LeaveRoom()
	if got := lines(alice.Pending()); got != "<- bob left" {
		t.Errorf("alice saw %q", got)
	}
	if err := bob.Say("x", false); err != ErrNotInRoom {
		t.Error(err)
	}
	for _, bad := range []string{"", "a b", "x/y", strings.Repeat("a", 21)} {
		if _, err := alice.JoinRoom(bad); err != ErrNoRoom {
			t.Errorf("%q: %v", bad, err)
		}
	}
	// flooding
	var err2 error
	for i := 0; i < floodLines+1 && err2 == nil; i++ {
		err2 = alice.Say("spam", false)
	}
	if err2 != ErrFlood {
		t.Errorf("flood: %v", err2)
	}

	// node messages
	if err := bob.Message(sysop.Num, "psst"); err != nil {
		t.Fatal(err)
	}
	if got := lines(sysop.Pending()); got != "Message from bob (node 2): psst" {
		t.Errorf("%q", got)
	}
	if err := bob.Message(99, "x"); err != ErrNoNode {
		t.Error(err)
	}
	if err := bob.Message(bob.Num, "x"); err != ErrNoNode {
		t.Error("a message to oneself")
	}
}

func TestPaging(t *testing.T) {
	b, ns := chatBBS(t)
	alice, bob, sysop := ns["alice"], ns["bob"], ns["sysop"]
	room, told, err := bob.PageSysop("help with files")
	if err != nil || told != 1 || room != "sysop-2" {
		t.Fatal(room, told, err)
	}
	if got := lines(sysop.Pending()); !strings.Contains(got, "bob (node 2) is paging you: help with files") {
		t.Errorf("%q", got)
	}
	if _, _, err := bob.PageSysop("again"); err == nil {
		t.Error("paged twice in a minute")
	}
	// the private room: bob's and sysops' only
	if _, err := alice.JoinRoom(room); err != ErrNotYours {
		t.Errorf("alice in %s: %v", room, err)
	}
	if _, err := bob.JoinRoom(room); err != nil {
		t.Fatal(err)
	}
	if rs := alice.Rooms(); len(rs) != 0 {
		t.Errorf("alice sees %+v", rs)
	}
	if ps := b.Pages(); len(ps) != 1 || ps[0].User != "bob" {
		t.Errorf("pages %+v", ps)
	}
	if _, _, err := alice.Answer(0); err == nil {
		t.Error("alice answered")
	}
	if _, node, err := sysop.Answer(0); err != nil || node != 2 || sysop.Room() != room {
		t.Fatal(node, err)
	}
	if got := lines(bob.Pending()); got != "-> sysop came in\nsysop answered your page." {
		t.Errorf("bob saw %q", got)
	}
	if len(b.Pages()) != 0 {
		t.Error("the page is still waiting")
	}

	// a busy sysop is not told; with no sysop, the page command runs
	sysop.SetBusy(true)
	mark := filepath.Join(t.TempDir(), "paged")
	b.Settings.PageCommand = []string{"sh", "-c", `printf '%s|%s|%s' "$1" "$2" "$BBS_PAGE_REASON" > ` + mark, "sh", "{user}", "{node}"}
	if _, told, err := alice.PageSysop("are you there? $(rm -rf /)"); err != nil || told != 0 {
		t.Fatal(told, err)
	}
	var got []byte
	for i := 0; i < 50; i++ {
		if got, _ = os.ReadFile(mark); len(got) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if string(got) != "alice|1|are you there? $(rm -rf /)" {
		t.Errorf("page command got %q", got)
	}
	// leaving: out of the room, the page gone
	bob.Leave()
	if got := lines(sysop.Pending()); got != "<- bob left" {
		t.Errorf("%q", got)
	}
	alice.Leave()
	if len(b.Pages()) != 0 {
		t.Error("a page of a caller gone")
	}
}
