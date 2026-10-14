package bbs

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/door"
	"github.com/Neo-Desktop/bbs-sec/internal/guard"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func TestRequire(t *testing.T) {
	alice := &store.User{ID: 1, Level: 30, Flags: "AF"}
	low := &store.User{ID: 2, Level: 10}
	sysop := &store.User{ID: 3, Sysop: true}
	locked := &store.User{ID: 4, Level: 99, Locked: true}
	for _, c := range []struct {
		req                     string
		alice, low, sysop, lock bool
	}{
		{"", true, true, true, false}, // a locked user meets nothing
		{"20", true, false, true, false},
		{"30 F", true, false, true, false},
		{"fa", true, false, true, false},
		{"20 B", false, false, true, false},
		{"!F", false, true, true, false},
		{"10 !B", true, true, true, false},
		{"sysop", false, false, true, false},
	} {
		r, err := ParseRequire(c.req)
		if err != nil {
			t.Fatalf("%q: %v", c.req, err)
		}
		if r.Allows(alice) != c.alice || r.Allows(low) != c.low || r.Allows(sysop) != c.sysop || r.Allows(locked) != c.lock {
			t.Errorf("%q (%s): alice %v low %v sysop %v locked %v", c.req, r, r.Allows(alice), r.Allows(low), r.Allows(sysop), r.Allows(locked))
		}
		if r2, _ := ParseRequire(r.String()); r2 != r {
			t.Errorf("%q: %q reads back as %+v", c.req, r.String(), r2)
		}
	}
	for _, bad := range []string{"256", "-1", "A1", "!", "!7", "9x"} {
		if _, err := ParseRequire(bad); err == nil {
			t.Errorf("%q taken", bad)
		}
	}
	if MustRequire("bad!").Allows(alice) || !MustRequire("bad!").Allows(sysop) {
		t.Error("a bad requirement lets users in")
	}
	if (Require{}).Allows(nil) != true || MustRequire("1").Allows(nil) {
		t.Error("no user")
	}
	for _, c := range []struct{ have, edit, want string }{
		{"AC", "+B -C", "AB"}, {"AC", "zy", "YZ"}, {"", "+a+b", "AB"}, {"ABC", "-", "ABC"}, {"ABC", "", ""},
	} {
		if got, err := EditFlags(c.have, c.edit); err != nil || got != c.want {
			t.Errorf("%q %q: %q %v", c.have, c.edit, got, err)
		}
	}
	if _, err := EditFlags("", "+1"); err == nil {
		t.Error("+1 taken")
	}
}

func levelBBS(t *testing.T) (*BBS, map[string]*Node) {
	b, nodes := chatBBS(t)
	b.Conf().NewUserLevel, b.Settings.ValidatedLevel = 10, 20
	newbie := AllowAll(10, "New")
	newbie.Post, newbie.Mail, newbie.Chat = false, false, false
	newbie.Time, newbie.Downloads = 30*time.Minute, 1
	b.Settings.Levels = []Level{newbie, AllowAll(20, "Member")}
	b.Settings.Levels[1].Downloads = 3
	for name, n := range nodes {
		u := n.User()
		u.Level = map[string]int{"alice": 10, "bob": 20, "sysop": 0}[name]
		b.Store.SetLevel(u.ID, u.Level)
	}
	return b, nodes
}

func TestLevels(t *testing.T) {
	b, nodes := levelBBS(t)
	alice, bob, sysop := nodes["alice"].User(), nodes["bob"].User(), nodes["sysop"].User()
	if l := b.LevelOf(alice); l.Name != "New" || l.Post || l.Chat {
		t.Errorf("alice: %+v", l)
	}
	if l := b.LevelOf(bob); l.Name != "Member" || !l.Post || l.Downloads != 3 {
		t.Errorf("bob: %+v", l)
	}
	if l := b.LevelOf(&store.User{Level: 5}); l.Name != "Level 5" || !l.Post {
		t.Errorf("under every level: %+v", l)
	}
	if l := b.LevelOf(sysop); l.Name != "Sysop" || l.Time != 0 {
		t.Errorf("sysop: %+v", l)
	}

	gen, _ := b.Store.CreateArea("General", "", false)
	mem, _ := b.Store.CreateArea("Members", "", false)
	b.Store.SetAreaRequire(mem.ID, "20", "20 M")
	mem, _ = b.Store.Area(mem.ID)
	areas := func(u *store.User) string {
		as, _ := b.AreasFor(u)
		var names []string
		for _, a := range as {
			names = append(names, a.Name)
		}
		return strings.Join(names, ",")
	}
	if areas(alice) != "General" || areas(bob) != "General,Members" || areas(sysop) != "General,Members" {
		t.Errorf("areas: %s / %s / %s", areas(alice), areas(bob), areas(sysop))
	}
	// alice's level may not post; bob may read Members but not post there without M
	if _, err := nodes["alice"].Post(gen, "", "hi", []string{"x"}, 0); err == nil {
		t.Error("alice posted")
	}
	if _, err := nodes["bob"].Post(mem, "", "hi", []string{"x"}, 0); err == nil {
		t.Error("bob posted without M")
	}
	bob.Flags = "M"
	if _, err := nodes["bob"].Post(mem, "", "hi", []string{"x"}, 0); err != nil {
		t.Errorf("bob with M: %v", err)
	}
	// mail: alice only to the sysop
	if err := nodes["alice"].SendMail("bob", "hi", []string{"x"}, 0); err == nil {
		t.Error("alice mailed bob")
	}
	if err := nodes["alice"].SendMail("sysop", "validate me", []string{"please"}, 0); err != nil {
		t.Errorf("alice to the sysop: %v", err)
	}
	// chat and paging
	if _, err := nodes["alice"].JoinRoom("main"); err != ErrNotAllowed {
		t.Errorf("alice chatted: %v", err)
	}
	b.Settings.Levels[0].Page = false
	if _, _, err := nodes["alice"].PageSysop("hi"); err != ErrNotAllowed {
		t.Errorf("alice paged: %v", err)
	}

	// files: download limits a day
	fa, _ := b.Store.CreateFileArea("Uploads", "", false)
	b.Store.SetFileAreaRequire(fa.ID, "", "20")
	fa, _ = b.Store.FileArea(fa.ID)
	if !b.CanDownload(alice, fa) || b.CanUpload(alice, fa) || !b.CanUpload(bob, fa) {
		t.Error("file area access")
	}
	f := &store.File{ID: 99}
	if err := b.MayDownload(alice, fa); err != nil {
		t.Fatal(err)
	}
	b.Downloaded(alice, f)
	if err := b.MayDownload(alice, fa); err == nil || !strings.Contains(err.Error(), "1 downloads") {
		t.Errorf("a second download: %v", err)
	}
	if fresh, _ := b.Store.UserByID(alice.ID); fresh.DownloadsToday() != 1 {
		t.Errorf("downloads today: %d", fresh.DownloadsToday())
	}

	// doors
	d := &door.Door{Name: "lord", Require: "20"}
	if b.CanDoor(alice, d) || !b.CanDoor(bob, d) {
		t.Error("door access")
	}

	// validation
	if err := b.Validate(alice, "test"); err != nil {
		t.Fatal(err)
	}
	if fresh, _ := b.Store.UserByID(alice.ID); fresh.Level != 20 {
		t.Errorf("validated: %d", fresh.Level)
	}
	if w, _ := b.Store.UsersBelow(20); len(w) != 0 {
		t.Errorf("waiting: %v", w)
	}
}

// TestTimeLimit: a call's time counts against the day; at its end the
// caller is warned and hung up.
func TestTimeLimit(t *testing.T) {
	b, _ := levelBBS(t)
	minCallTime = 300 * time.Millisecond
	t.Cleanup(func() { minCallTime = time.Minute })
	b.Settings.Levels[0].Time = 100 * time.Millisecond
	kicked := make(chan bool, 1)
	n, err := b.Enter(&Caller{Via: "test"}, func() { kicked <- true })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Login("alice", "password1"); err != nil {
		t.Fatal(err)
	}
	if left, ok := n.TimeLeft(); !ok || left > 300*time.Millisecond {
		t.Errorf("time left: %v %v", left, ok)
	}
	select {
	case <-kicked:
	case <-time.After(5 * time.Second):
		t.Fatal("not hung up")
	}
	es := n.Pending()
	if len(es) == 0 || !strings.Contains(es[len(es)-1].Text, "time is up") {
		t.Errorf("events: %v", es)
	}
	n.Leave()
	if u, _ := b.Store.UserByName("alice"); u.TimeToday() < 2 {
		// at least the 2 seconds the hang up waits
		t.Errorf("time today: %d", u.TimeToday())
	}
	// a sysop has no limit
	s, _ := b.Enter(&Caller{Via: "test"}, nil)
	s.Login("sysop", "password1")
	if _, ok := s.TimeLeft(); ok {
		t.Error("a sysop's time is limited")
	}
	s.Leave()
}

func TestDownloadRatio(t *testing.T) {
	b, ns := chatBBS(t)
	l := AllowAll(20, "Member")
	l.Ratio, l.RatioFree, l.Downloads = 2, 3, 10
	b.Settings.Levels = []Level{l}
	u := ns["alice"].Refresh()
	u.Level = 20
	if left, _ := b.DownloadsLeft(u); left != 3 {
		t.Errorf("free %d", left)
	}
	u.Downloads, u.Uploads = 3, 0
	left, why := b.DownloadsLeft(u)
	if left != 0 || why == nil || !strings.Contains(why.Error(), "upload a file to download 2 more") {
		t.Errorf("used up: %d %v", left, why)
	}
	u.Uploads = 4 // 3 + 2*4 - 3 downloaded
	if left, _ := b.DownloadsLeft(u); left != 8 {
		t.Errorf("ratio %d", left)
	}
	u.DLCount, u.DLDay = 9, store.Today()
	if left, why := b.DownloadsLeft(u); left != 1 || !strings.Contains(why.Error(), "today") {
		t.Errorf("daily %d %v", left, why)
	}
	u.Sysop = true
	if left, _ := b.DownloadsLeft(u); left != -1 {
		t.Errorf("sysop %d", left)
	}
}

func TestGuardedLogins(t *testing.T) {
	b, ns := chatBBS(t)
	dir := t.TempDir()
	names := filepath.Join(dir, "names")
	os.WriteFile(names, []byte("*spam*\n"), 0644)
	b.Guard = &guard.Guard{HackLog: filepath.Join(dir, "hack.log"), NameDenyFile: names, BanTime: time.Hour}
	b.Conf().ThrottleFails, b.Conf().ThrottleTime, b.Conf().FailDelay, b.Settings.NewUsers = 3, time.Minute, 0, true
	addr := &net.TCPAddr{IP: net.ParseIP("192.0.2.9"), Port: 4000}
	for i := 0; i < 3; i++ {
		if _, err := b.Authenticate("node 9", "ip:192.0.2.9", "root", "x"); err != ErrBadLogin {
			t.Fatalf("try %d: %v", i, err)
		}
	}
	if !b.Refuse("[listen telnet]", addr) {
		t.Error("not banned after three failures")
	}
	if b.Refuse("[listen telnet]", &net.TCPAddr{IP: net.ParseIP("192.0.2.10")}) {
		t.Error("another address banned")
	}
	if b.Refuse("[listen modem]", &net.UnixAddr{Name: "modem.sock"}) {
		t.Error("a unix socket refused")
	}
	if msg := ns["alice"].CheckNewName("Big Spammer"); msg != "That name is not allowed here." {
		t.Errorf("a denied name: %q", msg)
	}
	if msg := ns["alice"].CheckNewName("Zed"); msg != "" {
		t.Errorf("Zed: %q", msg)
	}
	if _, err := ns["alice"].Register("Spam King", "password12", ""); err != ErrNameDenied {
		t.Errorf("register: %v", err)
	}
	log := strings.Join(b.Guard.Tail(20), "\n")
	for _, want := range []string{`ip:192.0.2.9 node 9: failed login as "root"`, "192.0.2.9 banned until",
		"192.0.2.9 [listen telnet]: refused: banned until", `new user "Spam King" refused by name_deny_file`} {
		if !strings.Contains(log, want) {
			t.Errorf("no %q in the hack log:\n%s", want, log)
		}
	}
}
