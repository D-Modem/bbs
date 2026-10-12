// Package mrc joins the BBS to Multi Relay Chat, the BBSes' chat network:
// MRC's rooms are the BBS's chat rooms of a prefix (mrc-lobby is MRC's
// lobby), so callers on terminals, the web and IRC talk with other
// systems' users there.  The BBS is its own MRC connector: one connection
// to the MRC server carries all its users.
//
// The protocol is as Synchronet's mrc-connector.js and mrc-session.js
// speak it: lines of FromUser~FromSite~FromRoom~ToUser~MsgExt~ToRoom~Body~,
// the server's PING answered IMALIVE, each user's IAMHERE every minute,
// NEWROOM:old:new to move, LOGOFF to go; text in Mystic's |NN colors.
package mrc

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Protocol facts.
const (
	ProtocolVersion = "1.3.5"
	DefaultAddress  = "mrc.bottomlessabyss.net:5000" // 5001 with TLS
	maxLine         = 512
	maxBody         = 250
)

// Settings are the [mrc] section's.
type Settings struct {
	Address    string // the MRC server
	TLS        bool
	Prefix     string // the BBS's rooms that are MRC's ("mrc-")
	SystemName string // the BBS as MRC shows it (pipe colors allowed; default its name)
	// what /info shows of the BBS
	Web, Telnet, SSH, Sysop, Description string
	Require                              string // who may chat on MRC ("": anyone who chats)

	ReconnectDelay time.Duration // between tries (default 30s)
	PingInterval   time.Duration // each user's IAMHERE (default 60s)
	TLSConfig      *tls.Config   // nil: the system's roots
}

// Message is a line of the protocol.
type Message struct {
	FromUser, FromSite, FromRoom, ToUser, MsgExt, ToRoom, Body string
}

// Parse reads a line; ok false if it is no message.
func Parse(line string) (Message, bool) {
	f := strings.Split(strings.TrimRight(line, "\r\n"), "~")
	if len(f) < 7 {
		return Message{}, false
	}
	return Message{f[0], f[1], f[2], f[3], f[4], f[5], f[6]}, true
}

// Line is the message as it is sent.
func (m Message) Line() string {
	return strings.Join([]string{m.FromUser, m.FromSite, m.FromRoom, m.ToUser, m.MsgExt, m.ToRoom, m.Body}, "~") + "~"
}

var pipeCode = regexp.MustCompile(`\|[0-9A-Za-z]{2}`)

// SanitizeName is a user's or site's name as MRC takes it: no spaces
// (underscores), ASCII 33-125, no pipe codes, at most 30.
func SanitizeName(s string) string {
	s = pipeCode.ReplaceAllString(strings.ReplaceAll(s, " ", "_"), "")
	return cut(keep(s, 0x21, 0x7d), 30)
}

func sanitizeRoom(s string) string {
	return cut(keep(pipeCode.ReplaceAllString(s, ""), 0x21, 0x7d), 30)
}

// sanitizeText is a message's text: ASCII 32-125 (no ~).
func sanitizeText(s string) string { return keep(s, 0x20, 0x7d) }

func keep(s string, lo, hi rune) string {
	return strings.Map(func(r rune) rune {
		if r < lo || r > hi {
			return -1
		}
		return r
	}, s)
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Strip is MRC text as the BBS shows it: its pipe colors taken out.
func Strip(s string) string { return pipeCode.ReplaceAllString(s, "") }

// user is a BBS user on MRC.
type user struct {
	name    string         // as MRC knows them
	local   string         // the BBS's name
	rooms   map[string]int // the carried rooms they are in, and on how many nodes
	current string         // the MRC room they are in
}

// Client is the BBS's MRC connection.
type Client struct {
	B *bbs.BBS
	S Settings

	site string
	req  bbs.Require

	mu    sync.Mutex
	conn  net.Conn
	users map[string]*user // by MRC name, folded
}

// New checks the settings and makes the client.
func New(b *bbs.BBS, s Settings) (*Client, error) {
	if s.Address == "" {
		s.Address = DefaultAddress
	}
	if _, _, err := net.SplitHostPort(s.Address); err != nil {
		return nil, fmt.Errorf("mrc: address: %v", err)
	}
	if s.Prefix == "" {
		s.Prefix = "mrc-"
	}
	if s.SystemName == "" {
		s.SystemName = b.Name
	}
	if s.ReconnectDelay <= 0 {
		s.ReconnectDelay = 30 * time.Second
	}
	if s.PingInterval <= 0 {
		s.PingInterval = time.Minute
	}
	req, err := bbs.ParseRequire(s.Require)
	if err != nil {
		return nil, fmt.Errorf("mrc: require: %v", err)
	}
	return &Client{B: b, S: s, site: SanitizeName(Strip(b.Name)), req: req, users: map[string]*user{}}, nil
}

// Site is the BBS's name on MRC.
func (c *Client) Site() string { return c.site }

// the MRC room of a carried room, and back
func (c *Client) mrcRoom(room string) string { return room[len(c.S.Prefix):] }

func (c *Client) localRoom(mrcRoom string) string {
	return c.S.Prefix + strings.ToLower(sanitizeRoom(mrcRoom))
}

// Carries: the room is one of MRC's.
func (c *Client) Carries(room string) bool {
	return strings.HasPrefix(room, c.S.Prefix) && len(room) > len(c.S.Prefix)
}

// Allows: the user may chat on MRC.
func (c *Client) Allows(_ string, u *store.User) bool { return c.req.Allows(u) }

// send sends a message from the BBS (site filled in); false if it could
// not go.
func (c *Client) send(m Message) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sendLocked(m)
}

func (c *Client) sendLocked(m Message) bool {
	if c.conn == nil {
		return false
	}
	m.FromSite = c.site
	m.FromRoom, m.ToRoom = sanitizeRoom(m.FromRoom), sanitizeRoom(m.ToRoom)
	m.ToUser, m.MsgExt = SanitizeName(m.ToUser), SanitizeName(m.MsgExt)
	m.Body = sanitizeText(m.Body)
	line := m.Line()
	if len(line) > maxLine-1 {
		line = line[:maxLine-2] + "~"
	}
	c.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := c.conn.Write([]byte(line + "\n")); err != nil {
		c.conn.Close()
		return false
	}
	return true
}

func (c *Client) command(from, room, body string) {
	c.send(Message{FromUser: from, FromRoom: room, ToUser: "SERVER", ToRoom: room, Body: body})
}

// Joined: a user came into a carried room.
func (c *Client) Joined(room, local string) {
	name := SanitizeName(local)
	if name == "" {
		return
	}
	c.mu.Lock()
	u := c.users[strings.ToLower(name)]
	if u == nil {
		u = &user{name: name, local: local, rooms: map[string]int{}}
		c.users[strings.ToLower(name)] = u
	}
	u.rooms[room]++
	if u.rooms[room] > 1 {
		c.mu.Unlock()
		return
	}
	old, now := u.current, c.mrcRoom(room)
	u.current = now
	if old == "" {
		c.sendLocked(Message{FromUser: name, ToUser: "SERVER", Body: "IAMHERE"})
		c.sendLocked(Message{FromUser: name, ToUser: "NOTME", Body: "|07- |11" + name + " |03has arrived."})
	}
	c.sendLocked(Message{FromUser: name, FromRoom: old, ToUser: "SERVER", ToRoom: old, Body: "NEWROOM:" + old + ":" + now})
	c.mu.Unlock()
}

// Left: a user left a carried room.
func (c *Client) Left(room, local string) {
	name := SanitizeName(local)
	c.mu.Lock()
	defer c.mu.Unlock()
	u := c.users[strings.ToLower(name)]
	if u == nil || u.rooms[room] == 0 {
		return
	}
	if u.rooms[room]--; u.rooms[room] > 0 {
		return
	}
	delete(u.rooms, room)
	if u.current != c.mrcRoom(room) {
		return
	}
	if len(u.rooms) > 0 {
		// still in another of MRC's rooms here: there now
		var rest []string
		for r := range u.rooms {
			rest = append(rest, r)
		}
		sort.Strings(rest)
		old, now := u.current, c.mrcRoom(rest[0])
		u.current = now
		c.sendLocked(Message{FromUser: name, FromRoom: old, ToUser: "SERVER", ToRoom: old, Body: "NEWROOM:" + old + ":" + now})
		return
	}
	c.sendLocked(Message{FromUser: name, ToUser: "NOTME", Body: "|07- |12" + name + " |04has left chat|08."})
	c.sendLocked(Message{FromUser: name, ToUser: "SERVER", Body: "LOGOFF"})
	delete(c.users, strings.ToLower(name))
}

// present makes sure the user is in the room's MRC room (they may be in
// several of MRC's rooms here; MRC has them in one).
func (c *Client) present(room, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	u := c.users[strings.ToLower(name)]
	now := c.mrcRoom(room)
	if u == nil || u.current == now {
		return
	}
	old := u.current
	u.current = now
	c.sendLocked(Message{FromUser: name, FromRoom: old, ToUser: "SERVER", ToRoom: old, Body: "NEWROOM:" + old + ":" + now})
}

// Said: a user said a line in a carried room.
func (c *Client) Said(room, local, text string, emote bool) {
	name := SanitizeName(local)
	c.present(room, name)
	r := c.mrcRoom(room)
	body := "|03<|11" + name + "|03> |07" + text
	if emote {
		body = "|15* |13" + name + " " + text
	}
	c.send(Message{FromUser: name, FromRoom: r, ToRoom: r, Body: cut(body, maxBody)})
}

// Command: a user's !COMMAND, for the MRC server.
func (c *Client) Command(room, local, line string) {
	name := SanitizeName(local)
	c.present(room, name)
	c.command(name, c.mrcRoom(room), line)
}

// Run keeps the connection up until ctx is done.
func (c *Client) Run(ctx context.Context) {
	for {
		err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		c.B.Log.Printf("mrc: %v; again in %v", err, c.S.ReconnectDelay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.S.ReconnectDelay):
		}
	}
}

// Connected: the connection to the MRC server is up.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

func (c *Client) dial() (net.Conn, error) {
	d := &net.Dialer{Timeout: 30 * time.Second}
	if !c.S.TLS {
		return d.Dial("tcp", c.S.Address)
	}
	cfg := c.S.TLSConfig
	if cfg == nil {
		host, _, _ := net.SplitHostPort(c.S.Address)
		cfg = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	return tls.DialWithDialer(d, "tcp", c.S.Address, cfg)
}

// session is one connection: the handshake, then lines until it goes.
func (c *Client) session(ctx context.Context) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() {
		c.mu.Lock()
		if c.conn != nil {
			c.sendLocked(Message{FromUser: "CLIENT", ToUser: "SERVER", Body: "SHUTDOWN"})
		}
		c.mu.Unlock()
		conn.Close()
	})
	defer stop()

	// SYSTEM~PLATFORM, then the server's HELLO
	platform := strings.ReplaceAll(fmt.Sprintf("BBSSEC/%s_%s/%s", runtime.GOOS, runtime.GOARCH, ProtocolVersion), " ", "_")
	sys := strings.NewReplacer("~", "", "\n", "", "\r", "").Replace(c.S.SystemName)
	conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := fmt.Fprintf(conn, "%s~%s\n", sys, platform); err != nil {
		return err
	}
	r := bufio.NewReaderSize(conn, 4096)
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	hello, err := r.ReadString('\n')
	if err != nil {
		return fmt.Errorf("no hello: %w", err)
	}
	c.B.Log.Printf("mrc: connected to %s: %s", c.S.Address, strings.TrimSpace(Strip(hello)))

	c.mu.Lock()
	c.conn = conn
	for _, f := range []struct{ k, v string }{{"WEB", c.S.Web}, {"TEL", c.S.Telnet}, {"SSH", c.S.SSH},
		{"SYS", c.S.Sysop}, {"DSC", c.S.Description}} {
		if f.v != "" {
			c.sendLocked(Message{FromUser: "CLIENT", ToUser: "SERVER", Body: "INFO" + f.k + ":" + f.v})
		}
	}
	c.sendLocked(Message{FromUser: "CLIENT", ToUser: "SERVER", Body: "IMALIVE:" + sys})
	caps := "CAPABILITIES: MCI"
	if c.S.TLS {
		caps += " SSL"
	}
	c.sendLocked(Message{FromUser: "CLIENT", ToUser: "SERVER", Body: caps})
	// back after a break: those here are still here
	for _, u := range c.users {
		c.sendLocked(Message{FromUser: u.name, ToUser: "SERVER", Body: "IAMHERE"})
		c.sendLocked(Message{FromUser: u.name, ToUser: "SERVER", Body: "NEWROOM::" + u.current})
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()

	pings := time.NewTicker(c.S.PingInterval)
	defer pings.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-pings.C:
				if !ok {
					return
				}
				c.mu.Lock()
				if c.conn != conn {
					c.mu.Unlock()
					return
				}
				for _, u := range c.users {
					c.sendLocked(Message{FromUser: u.name, FromRoom: u.current, ToUser: "SERVER", ToRoom: u.current,
						Body: "IAMHERE"})
				}
				c.mu.Unlock()
			}
		}
	}()

	for {
		// the server pings now and then: silence this long is a dead line
		conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, net.ErrClosed) && ctx.Err() != nil {
				return nil
			}
			return err
		}
		if len(line) > 4*maxLine {
			continue
		}
		if m, ok := Parse(line); ok {
			c.handle(m)
		}
	}
}

// localName is the BBS's name of an MRC user here ("": not one).
func (c *Client) localName(mrcName string) (*user, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	u, ok := c.users[strings.ToLower(mrcName)]
	if !ok {
		return nil, false
	}
	cp := *u
	return &cp, true
}

// roomsInUse are the carried rooms someone here is in.
func (c *Client) roomsInUse() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	set := map[string]bool{}
	for _, u := range c.users {
		for r := range u.rooms {
			set[r] = true
		}
	}
	var out []string
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// rooms are the carried rooms a message is for: its room's, or all.
func (c *Client) rooms(mrcRoom string) []string {
	if mrcRoom == "" {
		return c.roomsInUse()
	}
	room := c.localRoom(mrcRoom)
	for _, r := range c.roomsInUse() {
		if r == room {
			return []string{room}
		}
	}
	return nil
}

// handle is a line from the MRC server.
func (c *Client) handle(m Message) {
	if m.FromUser == "SERVER" && strings.EqualFold(strings.TrimSpace(m.Body), "PING") {
		c.send(Message{FromUser: "CLIENT", ToUser: "SERVER", Body: "IMALIVE:" + c.S.SystemName})
		return
	}
	if m.ToUser == "SERVER" {
		return // a site's command, a user's LOGOFF
	}
	if strings.EqualFold(m.FromSite, c.site) && m.FromUser != "SERVER" {
		return // ours, come back: those here saw it as it was said
	}
	text := strings.TrimSpace(Strip(m.Body))
	if text == "" {
		return
	}
	// to one of ours: a private message, or the server's answer
	if u, ok := c.localName(m.ToUser); ok {
		from := m.FromUser
		if m.FromSite != "" {
			from += "@" + m.FromSite
		}
		if m.FromUser == "SERVER" {
			from = "MRC"
		}
		for r := range u.rooms {
			if c.mrcRoom(r) == u.current {
				c.B.RelayTo(r, u.local, bbs.Event{Kind: bbs.Notice, Room: r, Text: from + ": " + text})
			}
		}
		return
	}
	switch strings.ToUpper(m.ToUser) {
	case "", "CLIENT", "NOTME":
	default:
		return // to someone elsewhere
	}
	if m.FromUser == "SERVER" {
		cmd, rest, _ := strings.Cut(text, ":")
		switch strings.ToUpper(cmd) {
		case "STATS", "LATENCY", "PROTOCOLVERSION", "USERLIST", "HELLO":
			return
		case "BANNER":
			text = strings.TrimSpace(rest)
		case "ROOMTOPIC":
			room, topic, _ := strings.Cut(rest, ":")
			for _, r := range c.rooms(room) {
				c.B.RelayEvent(bbs.Event{Kind: bbs.Notice, Room: r, Text: "MRC topic: " + strings.TrimSpace(topic)})
			}
			return
		}
		for _, r := range c.rooms(m.ToRoom) {
			c.B.RelayEvent(bbs.Event{Kind: bbs.Notice, Room: r, Text: text})
		}
		return
	}
	room := m.ToRoom
	if room == "" {
		room = m.FromRoom
	}
	if m.ToUser == "NOTME" || m.ToUser == "CLIENT" {
		for _, r := range c.rooms(room) {
			c.B.RelayEvent(bbs.Event{Kind: bbs.Notice, Room: r, Text: text})
		}
		return
	}
	from := m.FromUser + "@" + m.FromSite
	e := bbs.Event{Kind: bbs.Said, From: from, Text: text}
	if rest, ok := strings.CutPrefix(text, "* "); ok {
		// |15* |13name does something
		e.Kind = bbs.Emoted
		if who, what, ok := strings.Cut(rest, " "); ok && strings.EqualFold(who, m.FromUser) {
			rest = what
		}
		e.Text = rest
	} else if who, what, ok := strings.Cut(text, " "); ok && strings.Contains(strings.ToLower(who), strings.ToLower(m.FromUser)) {
		e.Text = what // the sender's colored alias first: <name>
	}
	for _, r := range c.rooms(room) {
		e.Room = r
		c.B.RelayEvent(e)
	}
}
