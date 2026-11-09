package irclink

import (
	"net"
	"strings"
	"testing"
	"time"
)

// startP10 is a BBS whose hub has numeric 1 (AB), linking out to its
// peers, those named in p10 by P10.
func startP10(t *testing.T, p10 map[int]bool, peers ...*peerServer) *env {
	t.Helper()
	return startWith(t, func(s *Settings) {
		s.Numeric = 1
		for i := range s.Peers {
			s.Peers[i].P10 = p10[i]
		}
	}, peers...)
}

// linkP10 is the P10 handshake from the peer's side (numeric AF), with
// its burst and EB.
func (p *peerServer) linkP10(name string, burst ...string) {
	p.t.Helper()
	p.accept()
	p.expect("PASS :sendpw")
	p.expect("SERVER irc.test.example 1 ", " J10 AB]]] +h :Test BBS")
	p.say("PASS :acceptpw", "SERVER "+name+" 1 1700000000 1700000100 J10 AF]]] +h :a P10 server")
	p.say(burst...)
	p.say("AF EB")
}

// TestP10: a link with ircu: its burst taken (servers, users with their
// accounts and IPs, a channel with ops and bans), the BBS's sent after its
// EB, with a nick it has renamed Nick|Tag; then talk, private messages,
// WHOIS, PING, nick changes, topics, quits and a split, as P10.
func TestP10(t *testing.T) {
	p := newPeer(t)
	e := startP10(t, map[int]bool{0: true}, p)
	alice := e.node(t, "alice")
	e.run(t)
	p.linkP10("far.example",
		"AF S leaf.example 2 0 1700000100 P10 AG]]] +h :behind far",
		"AF N alice 1 100 al far.host +i B]AAAB AFAAA :Alice of far",
		"AF N carol 1 100 ca far.host +ir carol_acct:1700000000 AAAAAA AFAAB :Carol",
		"AG N dave 2 100 da leaf.host AAAAAA AGAAA :Dave",
		"AF B #retro 100 +nt AFAAB,AFAAA:o,AGAAA :%*!*@bad.example")
	p.expect("AB N alice|Test 1 ", " bbs irc.test.example +ir alice AAAAAA ABAAA :alice")
	p.expect("AB EB")
	p.expect("AB EA")
	p.expect("AB G :irc.test.example")
	if r, ok := e.b.LookupRemote("carol"); !ok || r.Account != "carol_acct" || r.Host != "far.host" {
		t.Errorf("carol %+v %v", r, ok)
	}
	if r, ok := e.b.LookupRemote("dave"); !ok || r.Server != "leaf.example" {
		t.Errorf("dave %+v %v", r, ok)
	}
	e.hub.mu.Lock()
	if u := e.hub.users[e.hub.nums.knownUser("AFAAA")]; u == nil || u.ip != "127.0.0.1" || u.chans[e.hub.chans["#retro"]] != "@" {
		t.Errorf("alice of far %+v", u)
	}
	e.hub.mu.Unlock()

	// the room is the channel
	if _, err := alice.JoinRoom("irc-retro"); err != nil {
		t.Fatal(err)
	}
	p.expect("ABAAA J #retro 100")
	names := map[string]bool{}
	for _, m := range e.b.RoomMembers("irc-retro") {
		names[m.Name] = true
	}
	if !names["alice"] || !names["alice|far"] || !names["carol"] || !names["dave"] {
		t.Errorf("members %v", names)
	}
	p.say("AFAAB P #retro :hi there", "AGAAA P #retro :\x01ACTION waves\x01")
	if ev := wait(t, alice, "carol: hi there"); ev.Mask != "carol!ca@far.host" {
		t.Errorf("mask %q", ev.Mask)
	}
	wait(t, alice, "* dave waves")
	if err := alice.Say("hello all", false); err != nil {
		t.Fatal(err)
	}
	p.expect("ABAAA P #retro :hello all")

	// private messages
	p.say("AFAAB P ABAAA :psst")
	wait(t, alice, "psst")
	if err := alice.MessageRemote("carol", "hi carol", false, false); err != nil {
		t.Fatal(err)
	}
	p.expect("ABAAA P AFAAB :hi carol")
	if err := alice.MessageRemote("alice|far", "hello me", true, false); err != nil {
		t.Fatal(err)
	}
	p.expect("ABAAA O AFAAA :hello me")

	// WHOIS, and PING, from the network
	p.say("AFAAB W AB :alice|Test")
	p.expect("AB 311 AFAAB alice|Test bbs irc.test.example * :alice")
	p.expect("AB 319 AFAAB alice|Test :#retro")
	p.expect("AB 318 AFAAB alice|Test :End of /WHOIS list.")
	p.say("AF G !1700000000.5 irc.test.example 1700000000.5")
	p.expect("AB Z AB !1700000000.5 1700000000.5 0 1700000000.5")

	// a nick change to one of ours: ours is renamed
	bob := e.node(t, "bob")
	p.expect("AB N bob 1 ", "ABAAB :bob")
	p.say("AFAAB N bob 200")
	p.expect("ABAAB N bob|Test ")
	bob.Leave()
	p.expect("ABAAB Q :Signed off")

	// a topic, a quit, a split
	p.say("AFAAB T #retro 100 150 bob :new topic")
	wait(t, alice, "Topic of #retro: new topic")
	p.say("AFAAA Q :bye")
	wait(t, alice, "alice|far left")
	p.say("AF SQ leaf.example 0 :gone")
	wait(t, alice, "dave left")
	if _, ok := e.b.LookupRemote("dave"); ok {
		t.Error("dave is still on")
	}
	alice.Leave()
	p.expect("ABAAA L #retro")
}

// TestP10Hub: a TS6 server and a P10 one through the hub: each is given
// the other's servers, users and channels in its own protocol, and talk
// and private messages go across, numerics mapped.
func TestP10Hub(t *testing.T) {
	p1, p2 := newPeer(t), newPeer(t)
	e := startP10(t, map[int]bool{1: true}, p1, p2)
	e.run(t)
	p1.link("far.example", "42X",
		":42X UID erin 1 100 +i er far.host 192.0.2.7 42XAAAAAA :Erin",
		":42X SJOIN 100 #retro +nt :@42XAAAAAA")
	p1.expect(":1BB PING irc.test.example :42X")
	p2.linkP10("other.example",
		"AF N frank 1 100 fr other.host +i AAAAAA AFAAA :Frank",
		"AF B #retro 90 AFAAA:o")
	// the P10 server gets the TS6 one's, in P10
	far := e.hub.nums.server("42X")
	p2.expect("AB S far.example 2 0 ", " P10 "+far+"]]] + :a test server")
	erin := e.hub.nums.user("42XAAAAAA")
	p2.expect(far + " N erin 2 100 er far.host +i DAAAIH " + erin + " :Erin")
	p2.expect("AB B #retro 90 " + erin) // the P10 one's is older: erin is no op
	p2.expect("AB EB")
	// the TS6 server gets the P10 one's, in TS6
	sid := e.hub.nums.knownServer("AF")
	frank := e.hub.nums.knownUser("AFAAA")
	p1.expect(":1BB SID other.example 2 " + sid + " :a P10 server")
	p1.expect(":"+sid+" EUID frank 1 100 +i fr other.host 0 "+frank, ":Frank")
	p1.expect(":" + sid + " SJOIN 90 #retro + :@" + frank)
	// talk and private messages go across
	p2.say("AFAAA P #retro :hi all")
	p1.expect(":" + frank + " PRIVMSG #retro :hi all")
	p1.say(":42XAAAAAA PRIVMSG " + frank + " :hi frank")
	p2.expect(erin + " P AFAAA :hi frank")
	p1.say(":42XAAAAAA JOIN 100 #other +")
	p2.expect(erin + " J #other 100")
	p2.say("AFAAA L #retro :later")
	p1.expect(":" + frank + " PART #retro :later")
	// a split of the TS6 one: the P10 one is told
	p1.conn.Close()
	p2.expect("AB SQ " + far + " 0 :irc.test.example far.example")
}

// TestP10In: an ircu linking in is told apart by its SERVER, and given
// the P10 handshake; one with the BBS's numeric is refused.
func TestP10In(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	e := startWith(t, func(s *Settings) {
		s.Numeric = 1
		s.Address = addr
		s.Peers = []Peer{{Name: "far.example", SendPassword: "sendpw", AcceptPassword: "acceptpw"}}
	})
	e.run(t)
	dial := func() *peerServer {
		var c net.Conn
		for i := 0; ; i++ {
			if c, err = net.Dial("tcp", addr); err == nil {
				break
			}
			if i > 50 {
				t.Fatal(err)
			}
			time.Sleep(20 * time.Millisecond)
		}
		p := &peerServer{t: t, conn: c, lines: make(chan string, 1000)}
		t.Cleanup(func() { c.Close() })
		go func() {
			buf := make([]byte, 4096)
			var rest string
			for {
				n, err := c.Read(buf)
				rest += string(buf[:n])
				for {
					i := strings.Index(rest, "\r\n")
					if i < 0 {
						break
					}
					p.lines <- rest[:i]
					rest = rest[i+2:]
				}
				if err != nil {
					close(p.lines)
					return
				}
			}
		}()
		return p
	}
	p := dial()
	p.say("PASS :acceptpw", "SERVER far.example 1 1700000000 1700000100 J10 AB]]] +h :mine")
	p.expect("ERROR", "numeric AB is the BBS's")
	p = dial()
	p.say("PASS :acceptpw", "SERVER far.example 1 1700000000 1700000100 J10 AF]]] +h :a P10 server",
		"AF N carol 1 100 ca far.host AAAAAA AFAAA :Carol", "AF EB")
	p.expect("PASS :sendpw")
	p.expect("SERVER irc.test.example 1 ", " J10 AB]]] +h")
	p.expect("AB EB")
	p.expect("AB EA")
	if _, ok := e.b.LookupRemote("carol"); !ok {
		t.Error("carol is not on")
	}
}

// TestNumerics: P10's base64, and its IP addresses.
func TestNumerics(t *testing.T) {
	if toB64(0, 2) != "AA" || toB64(4095, 2) != "]]" || toB64(63, 3) != "AA]" || fromB64("AC") != 2 {
		t.Error("base64")
	}
	if b64ToIP("B]AAAB") != "127.0.0.1" || ipToB64("127.0.0.1") != "B]AAAB" || b64ToIP("AAAAAA") != "0" {
		t.Errorf("IPv4 %s %s", b64ToIP("B]AAAB"), ipToB64("127.0.0.1"))
	}
	// 2001:db8::1: 2001 is CAB, db8 is A24, then the zeros, then 1
	if ip := b64ToIP("CABA24_AAB"); ip != "2001:db8::1" {
		t.Errorf("IPv6 %s", ip)
	}
	src, tok, params := parseP10("AFAAB P #retro :hi there")
	if src != "AFAAB" || tok != "P" || len(params) != 2 || params[1] != "hi there" {
		t.Errorf("parse %q %q %q", src, tok, params)
	}
	if l := p10Line("AB", "N", false, "bob", "200"); l != "AB N bob 200" {
		t.Errorf("line %q", l)
	}
}
