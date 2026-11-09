package irclink

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLive links to a real ircd, TS6 or P10, when one is set up (it is
// skipped otherwise):
//
//	IRCLINK_LIVE_PEER     its server name, linking in to 127.0.0.1:17000
//	IRCLINK_LIVE_SEND     the password we send it (its accept_password)
//	IRCLINK_LIVE_ACCEPT   the password it sends us (its send_password)
//	IRCLINK_LIVE_CLIENT   its address for clients (127.0.0.1:16667)
//	IRCLINK_LIVE_NICKLEN  its NICKLEN, if not 30 (hybrid's is 9)
//	IRCLINK_LIVE_LINKOUT  its address for servers, if the BBS links out
//	IRCLINK_LIVE_PROTOCOL hybrid or p10, if not TS6 (the BBS's P10 numeric is 1)
//
// A client on it sees the BBS's alice (and the network's carol beside her
// as Nick|Tag), talks with her in #retro, messages her, and WHOISes her.
func TestLive(t *testing.T) {
	peer := os.Getenv("IRCLINK_LIVE_PEER")
	if peer == "" {
		t.Skip("no IRCLINK_LIVE_PEER")
	}
	trace = func(s string) { t.Log(s) }
	t.Cleanup(func() { trace = nil })
	e := startWith(t, func(s *Settings) { s.Numeric = 1 })
	e.hub.S.Address = "127.0.0.1:17000"
	e.hub.S.NickLen = 30
	if n, err := strconv.Atoi(os.Getenv("IRCLINK_LIVE_NICKLEN")); err == nil {
		e.hub.S.NickLen = n // hybrid's is 9
	}
	ours := e.hub.tagged("alice", e.hub.me, nil, "")
	e.hub.S.Peers = []Peer{{Name: peer, SendPassword: os.Getenv("IRCLINK_LIVE_SEND"),
		AcceptPassword: os.Getenv("IRCLINK_LIVE_ACCEPT"), Tag: "far", Address: os.Getenv("IRCLINK_LIVE_LINKOUT"),
		Hybrid: os.Getenv("IRCLINK_LIVE_PROTOCOL") == "hybrid", P10: os.Getenv("IRCLINK_LIVE_PROTOCOL") == "p10"}}
	// a user there who has alice's nick
	c := dialIRC(t, os.Getenv("IRCLINK_LIVE_CLIENT"), "alice")
	alice := e.node(t, "alice")
	e.run(t)
	deadline := time.Now().Add(20 * time.Second)
	for !e.hub.linked(peer) {
		if time.Now().After(deadline) {
			t.Fatal("never linked")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := alice.JoinRoom("irc-retro"); err != nil {
		t.Fatal(err)
	}
	c.send("JOIN #retro")
	c.expect("366")
	wait(t, alice, "-> alice|far came in")
	c.send("WHOIS " + ours)
	c.expect(" 311 alice " + ours + " bbs irc.test.example")
	c.expect(" 319 alice " + ours + " :#retro")
	c.send("PRIVMSG #retro :hello from " + peer)
	wait(t, alice, "alice|far: hello from "+peer)
	alice.Say("hi there", false)
	c.expect(":" + ours + "!bbs@irc.test.example PRIVMSG #retro :hi there")
	c.send("PRIVMSG " + ours + " :psst")
	wait(t, alice, "Message from alice|far")
	if err := alice.MessageRemote("alice|far", "hello back", false, false); err != nil {
		t.Fatal(err)
	}
	c.expect("PRIVMSG alice :hello back")
	alice.Leave()
	c.expect(":" + ours + "!bbs@irc.test.example PART #retro")
}

// ircClient is a client of the live ircd.
type ircClient struct {
	t     *testing.T
	conn  net.Conn
	lines chan string
	seen  []string
}

// dialIRC connects and registers as nick.
func dialIRC(t *testing.T, addr, nick string) *ircClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	c := &ircClient{t: t, conn: conn, lines: make(chan string, 1000)}
	go func() {
		s := bufio.NewScanner(conn)
		for s.Scan() {
			l := strings.TrimRight(s.Text(), "\r")
			if strings.HasPrefix(l, "PING ") {
				fmt.Fprintf(conn, "PONG %s\r\n", l[5:])
			}
			c.lines <- l
		}
		close(c.lines)
	}()
	c.send("NICK "+nick, "USER "+nick+" 0 * :"+nick)
	c.expect(" 001 ")
	return c
}

func (c *ircClient) send(lines ...string) {
	for _, l := range lines {
		fmt.Fprintf(c.conn, "%s\r\n", l)
	}
}

func (c *ircClient) expect(want string) {
	c.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				c.t.Fatalf("closed; no %q among %q", want, c.seen)
			}
			c.seen = append(c.seen, l)
			if strings.Contains(l, want) {
				return
			}
		case <-deadline:
			c.t.Fatalf("no %q among %q", want, c.seen)
		}
	}
}
