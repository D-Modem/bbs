package ircd

import (
	"strings"
	"testing"
	"time"
)

// TestChanServ: a room registered by one in it, who is opped; the access
// list (FLAGS), auto-op and auto-voice on coming in, by IRC and from the
// BBS; ops' MODE and KICK; topics; BAN keeping one out; CLEAR USERS
// putting everyone out; DROP.
func TestChanServ(t *testing.T) {
	e := start(t)
	cs := ":ChanServ!ChanServ@irc.test NOTICE "
	a := e.dial(t)
	a.send("CAP REQ :multi-prefix")
	a.expect("CAP * ACK")
	a.send("CAP END")
	a.login("alice", "alice")
	b := e.dial(t)
	b.login("bob", "bob")

	a.send("PRIVMSG ChanServ :REGISTER #den")
	a.expect(cs + "alice :Come into #den to register it.")
	a.send("JOIN #den")
	a.expect("353 alice = #den :alice")
	a.send("CS REGISTER #den")
	a.expectAll(":ChanServ!ChanServ@irc.test MODE #den +o alice", cs+"alice :#den is registered to you.")
	a.send("CS REGISTER #den")
	a.expect(cs + "alice :That room is registered already.")

	// bob comes in with nothing; is opped by alice's MODE, and kicked
	b.send("JOIN #den")
	b.expect("353 bob = #den :")
	a.expect(":bob!irc@node2 JOIN #den")
	b.send("MODE #den +o bob")
	b.expect("482 bob #den :You are not an operator in that room")
	b.send("TOPIC #den :mine")
	b.expect("482 bob #den :You may not set that room's topic")
	a.send("MODE #den +v bob")
	b.expect(":alice!irc@node1 MODE #den +v bob")
	a.send("TOPIC #den :Welcome")
	b.expect(":alice!irc@node1 TOPIC #den :Welcome")
	a.send("KICK #den bob :out you go")
	b.expect(":alice!irc@node1 KICK #den bob :out you go")
	b.send("PRIVMSG #den :still here?")
	b.expect(" 404 ")

	// the access list: bob as AOP is opped on coming in
	a.send("CS FLAGS #den bob AOP")
	a.expect(cs + "alice :bob has +AOiortv in #den.")
	b.send("CS FLAGS #den bob +F")
	b.expect(cs + "bob :You need the f flag in #den for that.")
	b.send("JOIN #den")
	b.expect("353 bob = #den :")
	b.expect(":ChanServ!ChanServ@irc.test MODE #den +o bob")
	a.expect(":ChanServ!ChanServ@irc.test MODE #den +o bob")
	b.send("CS FLAGS #den")
	b.expect(cs + "bob :Access list of #den:")
	b.expect("alice (founder)")
	b.expect(cs + "bob :End of the list: 2 entries.")
	b.send("CS FLAGS #den bob +s")
	b.expect(cs + "bob :You need the f flag in #den for that.")

	// a caller on the BBS: voiced on coming in, and sees ops as lines
	carol := e.caller(t, "Carol Ann")
	a.send("CS FLAGS #den Carol_Ann VOP")
	a.expect(cs + "alice :Carol_Ann has +AV in #den.")
	hist, err := carol.JoinRoom("den")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, ev := range hist {
		lines = append(lines, ev.String())
	}
	if !strings.Contains(strings.Join(lines, "|"), "ChanServ gave Carol Ann a voice in den") {
		t.Errorf("joining: %q", lines)
	}
	a.expect(":ChanServ!ChanServ@irc.test MODE #den +v Carol_Ann")
	a.send("NAMES #den")
	l := a.expect("353 alice = #den")
	for _, want := range []string{"@alice", "@bob", "+Carol_Ann"} {
		if !strings.Contains(l, want) {
			t.Errorf("names %q: no %s", l, want)
		}
	}
	if _, _, err := carol.ChatCommand("/kick bob"); err == nil || !strings.Contains(err.Error(), "not an operator") {
		t.Errorf("a voice kicked: %v", err)
	}
	if out := carol.ChanServ("WHY #den"); len(out) != 1 || !strings.Contains(out[0], "+AV") {
		t.Errorf("why: %q", out)
	}

	// BAN keeps one out; UNBAN lets them in again
	a.send("CS BAN #den Carol_Ann spam")
	a.expectAll(":ChanServ!ChanServ@irc.test KICK #den Carol_Ann :spam (alice)", cs+"alice :Carol_Ann is banned from #den.")
	waitEvent(t, carol, "ChanServ put Carol Ann out of den: spam (alice)")
	if carol.InRoom("den") {
		t.Error("banned, still in")
	}
	if _, err := carol.JoinRoom("den"); err == nil {
		t.Error("banned, came in")
	}
	a.send("CS UNBAN #den Carol_Ann")
	a.expect(cs + "alice :Carol_Ann may come into #den again.")
	if _, err := carol.JoinRoom("den"); err != nil {
		t.Errorf("unbanned: %v", err)
	}

	// CLEAR USERS: everyone out but the one asking
	b.send("CS CLEAR #den USERS")
	b.expect(cs + "bob :You need the R flag in #den for that.")
	a.send("CS CLEAR #den USERS closing time")
	a.expectAll(":ChanServ!ChanServ@irc.test KICK #den bob :closing time (alice)", cs+"alice :#den is cleared: 2 put out.")
	waitEvent(t, carol, "put Carol Ann out of den: closing time")
	if ms := e.b.RoomMembers("den"); len(ms) != 1 || ms[0].Name != "alice" || carol.InRoom("den") {
		t.Errorf("cleared, left %+v", ms)
	}

	// settings, INFO, LIST; DROP by the founder alone
	a.send("CS SET #den ENTRYMSG Be nice", "CS SET #den GUARD ON", "CS INFO #den")
	a.expect(cs + "alice :The ENTRYMSG of #den is set.")
	a.expect(cs + "alice :GUARD is on for #den.")
	a.expect(cs + "alice :Founder     : alice")
	a.expect(cs + "alice :Topic       : Welcome")
	a.send("PART #den", "LIST")
	a.expect("322 alice #den 1 :Welcome")
	a.send("JOIN #den")
	a.expect("353 alice = #den :@ChanServ @alice")
	a.expect(cs + "alice :[#den] Be nice")
	b.send("CS LIST", "CS DROP #den")
	b.expect(cs + "bob :  #den                  +AOiortv")
	b.expect(cs + "bob :Only the founder may drop a room.")
	a.send("CS DROP #den")
	a.expectAll(":ChanServ!ChanServ@irc.test MODE #den -o alice", cs+"alice :#den is unregistered.")
	a.send("CS INFO #den")
	a.expect(cs + "alice :#den is not registered.")
}

// TestChanServRules: page and linked rooms can't be registered; sysops
// act as founders; a deleted founder's room passes on.
func TestChanServRules(t *testing.T) {
	e := start(t)
	cs := ":ChanServ!ChanServ@irc.test NOTICE "
	a := e.dial(t)
	a.login("alice", "alice")
	s := e.dial(t)
	s.login("sysop", "sysop")
	a.send("JOIN #sysop-1", "CS REGISTER #sysop-1")
	a.expect(cs + "alice :Page rooms can't be registered.")
	a.send("JOIN #room", "CS REGISTER #room", "CS FLAGS #room bob SOP")
	a.expect(cs + "alice :bob has +AORefiorstv in #room.")
	s.send("CS FLAGS #room", "CS SET #room TOPICLOCK ON")
	s.expect(cs + "sysop :Access list of #room:")
	s.expect(cs + "sysop :TOPICLOCK is on for #room.")

	// alice's account goes: bob, with the most flags, founds the room
	alice, _ := e.b.Store.UserByName("alice")
	if err := e.b.Store.DeleteUser(alice.ID); err != nil {
		t.Fatal(err)
	}
	reg, err := e.b.Store.RoomReg("room")
	if err != nil {
		t.Fatal(err)
	}
	if reg.Founder != "bob" {
		t.Errorf("founder %q", reg.Founder)
	}
}

// expectAll reads lines until each of wants has been in one, in any order
// (a service's notices and the room's events come by different ways).
func (c *conn) expectAll(wants ...string) {
	c.t.Helper()
	left := append([]string(nil), wants...)
	var seen []string
	for len(left) > 0 {
		c.c.SetReadDeadline(time.Now().Add(3 * time.Second))
		l, err := c.r.ReadString('\n')
		if err != nil {
			c.t.Fatalf("waiting for %q: %v; got %q", left, err, seen)
		}
		l = strings.TrimRight(l, "\r\n")
		seen = append(seen, l)
		for i, w := range left {
			if strings.Contains(l, w) {
				left = append(left[:i], left[i+1:]...)
				break
			}
		}
	}
}
