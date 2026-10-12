package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGuard(t *testing.T) {
	dir := t.TempDir()
	deny := filepath.Join(dir, "deny")
	names := filepath.Join(dir, "names")
	hack := filepath.Join(dir, "hack.log")
	os.WriteFile(deny, []byte("# spammers\n192.0.2.7\n198.51.100.0/24  ; a whole network\n2001:db8::/32\n"), 0644)
	os.WriteFile(names, []byte("root\n*sysop*\nadmin*\n"), 0644)
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	g := &Guard{DenyFile: deny, NameDenyFile: names, HackLog: hack, BanTime: time.Hour,
		now: func() time.Time { return now }, Reload: time.Nanosecond}

	for ip, want := range map[string]bool{"192.0.2.7": true, "192.0.2.8": false, "198.51.100.200": true,
		"2001:db8::5": true, "2001:db9::5": false, "": false, "not an ip": false} {
		if got, why := g.Refused(ip); got != want {
			t.Errorf("%q: %v %s", ip, got, why)
		}
	}
	// the file read again when it changes
	os.WriteFile(deny, []byte("192.0.2.8\n"), 0644)
	os.Chtimes(deny, now.Add(time.Minute), now.Add(time.Minute))
	now = now.Add(time.Second)
	if r, _ := g.Refused("192.0.2.8"); !r {
		t.Error("not reloaded")
	}
	if r, _ := g.Refused("192.0.2.7"); r {
		t.Error("an address no longer listed")
	}

	if !g.Ban("203.0.113.9", "too many failed logins") || g.Ban("nope", "x") {
		t.Error("ban")
	}
	if r, why := g.Refused("203.0.113.9"); !r || !strings.Contains(why, "banned until 13:00") {
		t.Errorf("banned: %v %s", r, why)
	}
	if len(g.Bans()) != 1 {
		t.Errorf("bans %v", g.Bans())
	}
	now = now.Add(2 * time.Hour)
	if r, _ := g.Refused("203.0.113.9"); r {
		t.Error("a ban that ended")
	}

	for name, want := range map[string]bool{"root": true, "Root": true, "the SysOp": true, "administrator": true,
		"alice": false, "rooted": false} {
		if g.NameDenied(name) != want {
			t.Errorf("name %q", name)
		}
	}

	g.Hack("node 1: failed login as %q from %s", "x\x1b[2J", "192.0.2.1")
	g.Hack("a raw \x1b[2J escape")
	tail := g.Tail(10)
	if len(tail) != 3 || tail[2][20:] != "a raw ?[2J escape" || !strings.Contains(tail[0], "banned until 2026-10-12 13:00") ||
		!strings.Contains(tail[1], `failed login as "x\x1b[2J"`) {
		t.Errorf("hack log %q", tail)
	}
	os.WriteFile(deny, []byte("300.1.1.1\n"), 0644)
	if p := g.Check(); len(p) != 1 {
		t.Errorf("check %v", p)
	}
	var none *Guard
	if r, _ := none.Refused("192.0.2.7"); r || none.NameDenied("root") || none.Ban("192.0.2.7", "") {
		t.Error("nil guard")
	}
	none.Hack("nothing")
}
