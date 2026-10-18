package ircd

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// TestBBSServ: the service's HELP, INFO, SET of profile fields and
// terminal settings, SCAN (by /BS too), KEYS, TOKENS, PASSWD, unknown
// commands and CTCP; WHOIS and ISON know it; a NOTICE to it is not
// answered.
func TestBBSServ(t *testing.T) {
	e := start(t)
	gen, _ := e.b.Store.CreateArea("General", "", false)
	e.b.Store.CreateArea("Other Area", "", false)
	c := e.dial(t)
	c.login("alice", "alice")
	from := ":BBSServ!BBSServ@irc.test NOTICE alice :"
	user := func() *store.User {
		u, _ := e.b.Store.UserByName("alice")
		return u
	}

	c.send("PRIVMSG BBSServ :HELP")
	c.expect(from + "*** \x02BBSServ HELP\x02 ***")
	c.expect(from + "    \x02SET\x02 changes one of your settings.")
	c.expect("End of BBSServ HELP")
	c.send("PRIVMSG bbsserv :help set")
	c.expect(from + "Syntax: \x02SET <setting> [value]\x02")

	c.send("PRIVMSG BBSServ :INFO")
	c.expect(from + "Account: alice (level 20)")
	c.expect(from + "Location:  Here")
	c.expect(from + "New scan: 2 of 2 areas")

	// the profile's fields
	c.send("PRIVMSG BBSServ :SET location   Far away town")
	c.expect(from + "Your location is now: Far away town")
	c.send("PRIVMSG BBSServ :SET about Likes \x02bold\x02 things")
	c.expect(from + "Your about is now: Likes bold things")
	c.send("PRIVMSG BBSServ :SET signature Alice | alice@example.org")
	c.expect(from + "Your signature is now: Alice | alice@example.org")
	c.send("PRIVMSG BBSServ :SET alias bob")
	c.expect(from + "That is someone's name or alias.")
	c.send("PRIVMSG BBSServ :SET alias Wonder")
	c.expect(from + "Your alias is now: Wonder")
	u := user()
	if u.Location != "Far away town" || u.About != "Likes bold things" || u.Signature != "Alice\nalice@example.org" ||
		u.Alias != "Wonder" {
		t.Errorf("user %+v", u)
	}
	c.send("PRIVMSG BBSServ :SET about -")
	c.expect(from + "Your about is cleared.")

	// terminal settings
	c.send("PRIVMSG BBSServ :SET ansi off", "PRIVMSG BBSServ :SET width 99", "PRIVMSG BBSServ :SET colour on")
	c.expect(from + "Your ansi is now: off")
	c.expect(from + "Width is one of 40, 80.")
	c.expect(from + "There is no setting \"colour\"")
	if p := bbs.ParsePrefs(user().Prefs); p["ansi"] != "off" {
		t.Errorf("prefs %v", p)
	}
	c.send("PRIVMSG BBSServ :SET ansi")
	c.expect(from + "Your ansi is cleared.")

	// the new scan, by the service's own command too
	c.send("BS SCAN OFF other area")
	c.expect(from + "1 areas are out of your new scan.")
	c.send("BS SCAN")
	c.expect(from + "General                  on  0 new")
	c.expect(from + "Other Area               off 0 new")
	if off, _ := e.b.Store.ScanOff(u.ID); len(off) != 1 || off[gen.ID] {
		t.Errorf("scan off %v", off)
	}
	c.send("BS SCAN ON nowhere")
	c.expect(from + "There is no area \"nowhere\"")

	// SSH keys
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sp, _ := ssh.NewPublicKey(pub)
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " laptop"
	c.send("PRIVMSG BBSServ :KEYS ADD wrongpass " + line)
	c.expect(from + "Wrong name or password.")
	c.send("PRIVMSG BBSServ :KEYS ADD password1 " + line)
	c.expect(from + "Added SHA256:")
	c.send("PRIVMSG BBSServ :KEYS")
	c.expect(from + "1. ssh-ed25519 SHA256:")
	c.send("PRIVMSG BBSServ :KEYS DEL 1")
	c.expect(from + "Deleted SHA256:")

	// API tokens
	e.b.Store.AddToken(&store.Token{UserID: u.ID, Name: "script", Hash: "h1", Scopes: "read"})
	c.send("PRIVMSG BBSServ :TOKENS")
	l := c.expect(`key "script" (read), never used`)
	id := strings.TrimSuffix(strings.Fields(strings.SplitN(l, " :", 2)[1])[0], ".")
	c.send("PRIVMSG BBSServ :TOKENS REVOKE 999", "PRIVMSG BBSServ :TOKENS REVOKE "+id)
	c.expect(from + "There is no token of yours with that number.")
	c.expect(from + "Revoked.")

	// the password
	c.send("PRIVMSG BBSServ :PASSWD nope newpassword9")
	c.expect(from + "That is not your password.")
	c.send("PRIVMSG BBSServ :PASSWORD password1 newpassword9")
	c.expect(from + "Your password is changed.")
	if _, err := e.b.Store.CheckPassword("alice", "newpassword9"); err != nil {
		t.Error("the password is not changed")
	}

	c.send("PRIVMSG BBSServ :FROB", "PRIVMSG BBSServ :PASSWD one", "PRIVMSG BBSServ :\x01VERSION\x01")
	c.expect(from + "Unknown command.")
	c.expect(from + "Invalid parameters.")
	c.expect(from + "\x01VERSION BBSServ (bbs-sec)\x01")
	c.send("WHOIS BBSServ", "ISON BBSServ nobody")
	c.expect("311 alice BBSServ BBSServ irc.test * :Your BBS account")
	c.expect("303 alice :BBSServ")
	c.send("NOTICE BBSServ :INFO", "PING :done")
	if l := c.expect("PONG"); strings.Contains(l, "Account") {
		t.Error("a NOTICE was answered")
	}
}

// TestServiceName: a service nick with a space is refused; another name
// is taken, its IRC command too.
func TestServiceName(t *testing.T) {
	e := start(t)
	if _, err := New(e.b, Settings{Address: "127.0.0.1:0", Service: "bad name"}); err == nil {
		t.Error("a bad service nick taken")
	}
	srv, err := New(e.b, Settings{Address: "127.0.0.1:0", Service: "Account"})
	if err != nil || srv.svc.Name != "Account" || !strings.Contains(strings.Join(srv.svc.CommandAliases, " "), "ACCOUNT") {
		t.Errorf("%v %v", srv.svc, err)
	}
}

// TestPermanentChannels: the sysop alone makes a room permanent; it is
// held open by the service, listed, joined with its topic, set by the
// sysop alone; seen by BBS callers; and let go, it goes.
func TestPermanentChannels(t *testing.T) {
	e := start(t)
	a := e.dial(t)
	a.login("alice", "alice")
	s := e.dial(t)
	s.login("sysop", "sysop")
	svc := ":BBSServ!BBSServ@irc.test NOTICE "

	a.send("PRIVMSG BBSServ :ROOMS", "PRIVMSG BBSServ :ROOM ADD #lobby Hi")
	a.expect(svc + "alice :There are no permanent rooms.")
	a.expect(svc + "alice :Permanent rooms are the sysop's to make.")
	s.send("PRIVMSG BBSServ :ROOM ADD #Lobby Say hello to everyone", "BS ROOM ADD #bad.name")
	s.expect(svc + "sysop :#lobby is a permanent room: BBSServ holds it open.")
	s.expect(svc + "sysop :A room's name is 1 to 20 letters")

	// held open, empty, with its topic
	a.send("LIST")
	a.expect("322 alice #lobby 1 :Say hello to everyone")
	a.send("PRIVMSG BBSServ :ROOMS")
	a.expect(svc + "alice :#lobby                0 in  Say hello to everyone")
	a.send("JOIN #lobby")
	a.expect("JOIN #lobby")
	a.expect("332 alice #lobby :Say hello to everyone")
	a.expect("353 alice = #lobby :@BBSServ alice")
	a.send("WHO #lobby")
	a.expect("352 alice #lobby BBSServ irc.test irc.test BBSServ H@")
	a.send("WHOIS BBSServ")
	a.expect("319 alice BBSServ :@#lobby")

	// topics: the sysop's, by TOPIC or by the service
	a.send("TOPIC #lobby :mine now")
	a.expect(" 482 alice #lobby :A topic is set by the sysop")
	s.send("TOPIC #lobby :Be kind")
	a.expect(":BBSServ!BBSServ@irc.test TOPIC #lobby :Be kind")
	s.send("BS ROOM TOPIC #lobby")
	a.expect(":BBSServ!BBSServ@irc.test TOPIC #lobby :")
	a.send("TOPIC #lobby")
	a.expect("331 alice #lobby :No topic is set")
	a.send("JOIN #other", "TOPIC #other :x")
	a.expect(" 482 alice #other :Only permanent rooms have topics")

	// a caller on the BBS sees it too
	bob := e.caller(t, "bob")
	if rs := bob.Rooms(); len(rs) != 2 || !rs[0].Permanent {
		t.Errorf("rooms %+v", rs)
	}

	// kept when everyone leaves; let go, it goes
	a.send("PART #lobby")
	a.expect("PART #lobby")
	if rs := e.b.PermanentRooms(); len(rs) != 1 {
		t.Errorf("permanent %v", rs)
	}
	s.send("BS ROOM DEL #lobby", "BS ROOM DEL #lobby")
	s.expect(svc + "sysop :#lobby goes when its last caller leaves.")
	s.expect(svc + "sysop :That room is not a permanent one.")
	a.send("LIST")
	if l := a.expect("323 alice"); strings.Contains(l, "lobby") {
		t.Error("still listed")
	}
	a.send("NAMES #other")
	if l := a.expect("353 alice = #other"); strings.Contains(l, "BBSServ") {
		t.Errorf("the service in a room it does not hold: %q", l)
	}
}
