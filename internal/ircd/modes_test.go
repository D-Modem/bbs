package ircd

import (
	"strings"
	"testing"
)

// TestChannelModes: a registered room's modes by its op: m and voices, i
// and INVITE, k, l, b and e, q and z, s, t, n, T, KNOCK; the lists; a
// caller on the BBS bound as an IRC user is; and the modes kept.
func TestChannelModes(t *testing.T) {
	e := start(t)
	a := e.dial(t)
	a.login("alice", "alice")
	b := e.dial(t)
	b.login("bob", "bob")
	a.send("JOIN #den", "CS REGISTER #den")
	a.expect("MODE #den +o alice")
	a.send("MODE #den")
	a.expect("324 alice #den :+Ccnt")

	// only ops set modes; unregistered rooms have none
	b.send("JOIN #den")
	b.expect("366 bob #den")
	b.send("MODE #den +m")
	b.expect("482 bob #den :You are not an operator in that room")
	b.send("JOIN #open", "MODE #open +m")
	b.expect("482 bob #open :Only registered rooms have modes")

	// m: bob is quiet until voiced; a caller on the BBS too
	carol := e.caller(t, "Carol Ann")
	if _, err := carol.JoinRoom("den"); err != nil {
		t.Fatal(err)
	}
	a.send("MODE #den +m")
	b.expect(":alice!irc@node1 MODE #den +m")
	b.send("PRIVMSG #den :hello?")
	b.expect("404 bob #den :Cannot send to channel: that room is moderated")
	if err := carol.Say("me too?", false); err == nil {
		t.Error("a caller talked in a +m room")
	}
	a.send("MODE #den +v bob")
	b.expect("MODE #den +v bob")
	b.send("PRIVMSG #den :voiced")
	a.expect(":bob!irc@node2 PRIVMSG #den :voiced")
	a.send("MODE #den -m")
	b.expect("MODE #den -m")

	// i: carol, out, is kept out until invited
	carol.LeaveRoom()
	a.send("MODE #den +i")
	b.expect("MODE #den +i")
	if _, err := carol.JoinRoom("den"); err == nil || !strings.Contains(err.Error(), "invite only") {
		t.Errorf("+i: %v", err)
	}
	b.send("INVITE Carol_Ann #den")
	b.expect("482 bob #den :You are not an operator in that room")
	a.send("INVITE Carol_Ann #den")
	a.expect("341 alice Carol_Ann :#den")
	waitEvent(t, carol, "alice invites Carol Ann into den")
	if _, err := carol.JoinRoom("den"); err != nil {
		t.Errorf("invited: %v", err)
	}
	carol.LeaveRoom()
	if _, err := carol.JoinRoom("den"); err == nil {
		t.Error("an invite used twice")
	}
	a.send("MODE #den -i")
	b.expect("MODE #den -i")

	// k and l
	a.send("MODE #den +kl sesame 2")
	b.expect("MODE #den +k sesame")
	b.expect("MODE #den +l 2")
	a.send("MODE #den")
	a.expect("324 alice #den +Ccntkl sesame :2")
	if _, err := carol.JoinRoom("den"); err == nil || !strings.Contains(err.Error(), "key") {
		t.Errorf("+k: %v", err)
	}
	if _, err := carol.JoinRoomKey("den", "sesame"); err == nil || !strings.Contains(err.Error(), "full") {
		t.Errorf("+l: %v", err)
	}
	a.send("MODE #den -l")
	b.expect("MODE #den -l")
	if _, err := carol.JoinRoomKey("den", "sesame"); err != nil {
		t.Errorf("the key: %v", err)
	}
	b.send("PART #den", "JOIN #den")
	b.expect("475 bob #den :Cannot join channel (+k)")
	b.send("JOIN #den sesame")
	b.expect("366 bob #den")
	a.send("MODE #den -k *")
	b.expect("MODE #den -k *")

	// b and e; the lists
	a.send("MODE #den +b bob", "MODE #den b")
	a.expectAll("MODE #den +b bob!*@*", "367 alice #den bob!*@* alice ", "368 alice #den")
	b.send("PRIVMSG #den :banned?")
	b.expect("404 bob #den :Cannot send to channel: you may not talk in that room")
	b.send("PART #den", "JOIN #den")
	b.expect("474 bob #den")
	a.send("MODE #den +e $a:bob")
	a.expect("MODE #den +e $a:bob")
	b.send("JOIN #den")
	b.expect("366 bob #den")
	a.send("MODE #den -be bob $a:bob", "MODE #den +q *!telnet@*")
	a.expect("MODE #den -e $a:bob")
	a.expect("MODE #den +q *!telnet@*")
	if err := carol.Say("from the terminal", false); err == nil {
		t.Error("a quieted caller talked")
	}
	// z: what q stops goes to the ops
	a.send("MODE #den +z")
	a.expect("MODE #den +z")
	if err := carol.Say("to the ops", false); err != nil {
		t.Errorf("+z: %v", err)
	}
	a.expect(":Carol_Ann!telnet@node3 PRIVMSG #den :to the ops")
	a.send("MODE #den q")
	a.expect("728 alice #den q *!telnet@* alice ")

	// t off: anyone in the room sets the topic
	b.send("TOPIC #den :bob's topic")
	b.expect("482 bob #den")
	a.send("MODE #den -t")
	b.expect("MODE #den -t")
	b.send("TOPIC #den :bob's topic")
	a.expect("TOPIC #den :bob's topic")

	// n off: lines from outside; T: no notices
	o := e.dial(t)
	o.login("sysop", "sysop")
	o.send("PRIVMSG #den :from outside")
	o.expect("404 sysop #den :Cannot send to channel (join it first)")
	a.send("MODE #den -n+T")
	a.expect("MODE #den -n")
	a.expect("MODE #den +T")
	o.send("PRIVMSG #den :from outside")
	a.expect(":sysop!irc@node4 PRIVMSG #den :from outside")
	b.send("NOTICE #den :a notice", "PRIVMSG #den :a line")
	if l := a.expect("#den :a line"); strings.Contains(l, "notice") {
		t.Error(l)
	}

	// s: not in LIST to those outside; KNOCK on an invite-only room
	a.send("MODE #den +si")
	a.expect("MODE #den +i")
	d := e.dial(t)
	d.send("PASS sysop:password1", "NICK x", "USER u 0 * :x")
	d.expect(" 376 ")
	c := e.dial(t)
	c.login("Carol Ann", "Carol_Ann")
	c.send("LIST")
	if l := c.expect("323 Carol_Ann"); strings.Contains(l, "den") {
		t.Error("a secret room listed")
	}
	carol.LeaveRoom()
	c.send("KNOCK #den :let me in")
	c.expect("711 Carol_Ann #den :Your KNOCK has been delivered")
	a.expect("Carol_Ann (node")
	c.send("KNOCK #den")
	c.expect("712 Carol_Ann #den")

	// kept: in the store
	reg, err := e.b.Store.RoomReg("den")
	if err != nil {
		t.Fatal(err)
	}
	if reg.Modes != "Tisz" || len(reg.Lists) != 1 || reg.Lists[0].Mask != "*!telnet@*" {
		t.Errorf("kept %q %+v", reg.Modes, reg.Lists)
	}
}
