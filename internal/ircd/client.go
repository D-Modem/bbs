package ircd

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircfmt"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/ergochat/irc-go/ircreader"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// timeouts
const (
	registerTimeout = time.Minute      // to log in
	idleTimeout     = 3 * time.Minute  // without a line: then a PING
	pingTimeout     = 90 * time.Second // for its answer
	writeTimeout    = 30 * time.Second // a line written
	maxLine         = 512              // bytes of a line, tags aside
	maxSASL         = 400 * 3          // bytes of AUTHENTICATE data
	maxJoins        = 20               // channels at once
)

// caps are the IRCv3 capabilities offered (and sasl's value).
var caps = map[string]string{
	"sasl":              "PLAIN",
	"server-time":       "",
	"echo-message":      "",
	"multi-prefix":      "",
	"userhost-in-names": "",
}

// client is a connection.
type client struct {
	s    *Server
	conn net.Conn
	host string

	wmu sync.Mutex
	w   *bufio.Writer

	// registering
	nick     string
	gotUser  bool
	realname string
	pass     string
	capWait  bool // CAP LS or REQ came: registration waits for CAP END
	caps     map[string]bool
	sasl     strings.Builder
	saslOn   bool
	u        *store.User

	// registered
	n    *bbs.Node
	away string
	done chan struct{}
	once sync.Once
}

func (s *Server) handle(conn net.Conn) {
	h, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	c := &client{s: s, conn: conn, host: h, w: bufio.NewWriter(conn), caps: map[string]bool{},
		done: make(chan struct{})}
	s.mu.Lock()
	s.clients[c] = true
	s.mu.Unlock()
	defer func() {
		c.close()
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
		if c.n != nil {
			c.n.Leave()
			s.B.Log.Printf("node %d: gone (irc)", c.n.Num)
		}
	}()
	c.serve()
}

func (c *client) close() {
	c.once.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

// source is how this client is named in what it is sent.
func (c *client) source() string {
	num := 0
	if c.n != nil {
		num = c.n.Num
	}
	return fmt.Sprintf("%s!irc@node%d", c.nick, num)
}

// send writes a message (tags: nil, or ones its caps take).
func (c *client) send(tags map[string]string, source, command string, params ...string) {
	if source == "" {
		source = c.s.S.ServerName
	}
	if !c.caps["server-time"] {
		delete(tags, "time")
	}
	m := ircmsg.MakeMessage(tags, source, command, params...)
	switch command {
	case "PRIVMSG", "NOTICE", "ERROR", "TOPIC":
		m.ForceTrailing() // text is trailing, as clients look for it
	default:
		if len(command) == 3 && command[0] >= '0' && command[0] <= '9' && len(params) > 1 {
			m.ForceTrailing() // a numeric's last parameter: its text, or a list
		}
	}
	line, err := m.LineBytesStrict(false, maxLine)
	if err != nil && !errors.Is(err, ircmsg.ErrorBodyTooLong) {
		c.s.B.Log.Printf("irc: %s %s: %v", command, strings.Join(params, " "), err)
		return
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	c.w.Write(line)
	if err := c.w.Flush(); err != nil {
		c.close()
	}
}

// reply sends a numeric to the client.
func (c *client) reply(numeric string, params ...string) {
	nick := c.nick
	if nick == "" {
		nick = "*"
	}
	c.send(nil, "", numeric, append([]string{nick}, params...)...)
}

func (c *client) serve() {
	var r ircreader.Reader
	r.Initialize(c.conn, 512, ircmsg.MaxlenTagsFromClient+maxLine)
	registerBy := time.Now().Add(registerTimeout) // the whole registration, however slow its lines
	deadline := registerTimeout
	pinged := false
	for {
		if c.n == nil {
			c.conn.SetReadDeadline(registerBy)
		} else {
			c.conn.SetReadDeadline(time.Now().Add(deadline))
		}
		raw, err := r.ReadLine()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() && c.n != nil && !pinged {
				c.send(nil, "", "PING", c.s.S.ServerName)
				pinged, deadline = true, pingTimeout
				continue
			}
			if errors.Is(err, ircreader.ErrReadQ) {
				c.send(nil, "", "ERROR", "Line too long")
			} else if ne != nil && ne.Timeout() {
				c.send(nil, "", "ERROR", "Ping timeout")
			}
			return
		}
		pinged = false
		if c.n != nil {
			deadline = idleTimeout
		}
		m, err := ircmsg.ParseLineStrict(string(raw), true, maxLine)
		if errors.Is(err, ircmsg.ErrorLineIsEmpty) {
			continue
		}
		if err != nil && !errors.Is(err, ircmsg.ErrorBodyTooLong) {
			c.reply(ERR_UNKNOWNERROR, "*", "That line could not be read")
			continue
		}
		m.Command = strings.ToUpper(m.Command)
		if m.Command == "QUIT" {
			c.send(nil, "", "ERROR", "Closing link: "+c.host+" (Quit)")
			return
		}
		if c.n == nil {
			if !c.register(&m) {
				return
			}
			if c.n != nil {
				deadline = idleTimeout
				go c.events()
			}
			continue
		}
		c.command(&m)
	}
}

// param is a message's i'th parameter, or "".
func param(m *ircmsg.Message, i int) string {
	if i < len(m.Params) {
		return m.Params[i]
	}
	return ""
}

// register handles a line before the login; false ends the connection.
func (c *client) register(m *ircmsg.Message) bool {
	switch m.Command {
	case "CAP":
		c.cap(m)
	case "AUTHENTICATE":
		if !c.authenticate(m) {
			return false
		}
	case "PASS":
		// "PASS Carol Ann:password" from a client that sends it as it is
		c.pass = strings.Join(m.Params, " ")
	case "NICK":
		nick := param(m, 0)
		if nick == "" {
			c.reply(ERR_NONICKNAMEGIVEN, "No nickname given")
			break
		}
		if strings.ContainsAny(nick, " ,*?!@:#$") || len(nick) > 64 {
			c.reply(ERR_ERRONEUSNICKNAME, "*", "Erroneous nickname")
			break
		}
		c.nick = nick
	case "USER":
		if len(m.Params) < 4 {
			c.reply(ERR_NEEDMOREPARAMS, "USER", "Not enough parameters")
			break
		}
		c.gotUser, c.realname = true, m.Params[3]
	case "PING":
		c.send(nil, "", "PONG", c.s.S.ServerName, param(m, 0))
	case "PONG":
	default:
		c.reply(ERR_NOTREGISTERED, "You have not registered")
	}
	if c.nick == "" || !c.gotUser || c.capWait {
		return true
	}
	return c.login()
}

func (c *client) cap(m *ircmsg.Message) {
	switch strings.ToUpper(param(m, 0)) {
	case "LS":
		if c.n == nil {
			c.capWait = true
		}
		v302 := false
		if v, err := strconv.Atoi(param(m, 1)); err == nil && v >= 302 {
			v302 = true
		}
		var list []string
		for name, v := range caps {
			if v302 && v != "" {
				name += "=" + v
			}
			list = append(list, name)
		}
		c.send(nil, "", "CAP", c.capNick(), "LS", strings.Join(sorted(list), " "))
	case "LIST":
		var on []string
		for name := range c.caps {
			on = append(on, name)
		}
		c.send(nil, "", "CAP", c.capNick(), "LIST", strings.Join(sorted(on), " "))
	case "REQ":
		if c.n == nil {
			c.capWait = true
		}
		want := strings.Fields(param(m, 1))
		ok := len(want) > 0
		for _, w := range want {
			if _, known := caps[strings.TrimPrefix(w, "-")]; !known {
				ok = false
			}
		}
		if !ok {
			c.send(nil, "", "CAP", c.capNick(), "NAK", param(m, 1))
			return
		}
		for _, w := range want {
			if name, off := strings.CutPrefix(w, "-"); off {
				delete(c.caps, name)
			} else {
				c.caps[w] = true
			}
		}
		c.send(nil, "", "CAP", c.capNick(), "ACK", param(m, 1))
	case "END":
		c.capWait = false
	default:
		c.reply(ERR_INVALIDCAPCMD, param(m, 0), "Invalid CAP subcommand")
	}
}

func (c *client) capNick() string {
	if c.nick == "" {
		return "*"
	}
	return c.nick
}

// authenticate is SASL PLAIN; false: too many failures, the end.
func (c *client) authenticate(m *ircmsg.Message) bool {
	arg := param(m, 0)
	switch {
	case !c.caps["sasl"]:
		c.reply(ERR_SASLFAIL, "SASL authentication failed: request the sasl capability first")
		return true
	case c.u != nil:
		c.reply(ERR_SASLALREADY, "You have already authenticated")
		return true
	case arg == "*":
		c.saslOn = false
		c.sasl.Reset()
		c.reply(ERR_SASLABORTED, "SASL authentication aborted")
		return true
	case !c.saslOn:
		if strings.ToUpper(arg) != "PLAIN" {
			c.reply(RPL_SASLMECHS, "PLAIN", "are available SASL mechanisms")
			c.reply(ERR_SASLFAIL, "SASL authentication failed")
			return true
		}
		c.saslOn = true
		c.send(nil, "", "AUTHENTICATE", "+")
		return true
	}
	if arg != "+" {
		c.sasl.WriteString(arg)
	}
	if c.sasl.Len() > maxSASL {
		c.saslOn = false
		c.sasl.Reset()
		c.reply(ERR_SASLTOOLONG, "SASL message too long")
		return true
	}
	if len(arg) == 400 {
		return true // more is coming
	}
	data, err := base64.StdEncoding.DecodeString(c.sasl.String())
	c.saslOn = false
	c.sasl.Reset()
	parts := strings.Split(string(data), "\x00")
	if err != nil || len(parts) != 3 {
		c.reply(ERR_SASLFAIL, "SASL authentication failed: bad PLAIN data")
		return true
	}
	name := parts[1]
	if parts[0] != "" && !strings.EqualFold(parts[0], parts[1]) {
		c.reply(ERR_SASLFAIL, "SASL authentication failed: you log in as yourself")
		return true
	}
	u, err := c.s.B.Authenticate("irc", "ip:"+c.host, name, parts[2])
	if err != nil {
		c.reply(ERR_SASLFAIL, "SASL authentication failed: "+err.Error())
		return !errors.Is(err, bbs.ErrThrottled)
	}
	c.u = u
	nick := Nick(u.Name)
	c.reply(RPL_LOGGEDIN, nick+"!irc@"+c.s.S.ServerName, u.Name, "You are now logged in as "+u.Name)
	c.reply(RPL_SASLSUCCESS, "SASL authentication successful")
	return true
}

// login ends the registration: the user known by SASL, or by PASS
// ("name:password", or the password with the nick as the name), given a
// node.  False ends the connection.
func (c *client) login() bool {
	b := c.s.B
	if c.u == nil {
		if c.pass == "" {
			c.reply(ERR_PASSWDMISMATCH, "Log in with your BBS name and password (SASL PLAIN, or PASS name:password)")
			c.send(nil, "", "ERROR", "Closing link: no login")
			return false
		}
		name, pw, ok := strings.Cut(c.pass, ":")
		if !ok {
			name, pw = c.nick, c.pass
		}
		u, err := b.Authenticate("irc", "ip:"+c.host, name, pw)
		if err != nil {
			c.reply(ERR_PASSWDMISMATCH, err.Error())
			c.send(nil, "", "ERROR", "Closing link: "+err.Error())
			return false
		}
		c.u = u
	}
	if !b.LevelOf(c.u).Chat {
		c.reply(ERR_YOUREBANNEDCREEP, "Your access does not allow chat")
		c.send(nil, "", "ERROR", "Closing link: no chat for your access")
		return false
	}
	n, err := b.Enter(&bbs.Caller{Via: "irc", Listener: "irc", Remote: c.host, Source: "ip:" + c.host},
		func() { c.close() })
	if err != nil {
		c.send(nil, "", "ERROR", "Closing link: "+err.Error())
		return false
	}
	n.SetUser(c.u)
	n.Doing("IRC")
	c.n = n
	b.Log.Printf("node %d: %s on IRC (%s)", n.Num, c.u.Name, c.host)
	asked := c.nick
	c.nick = Nick(c.u.Name)
	if fold(asked) != fold(c.nick) {
		c.send(nil, asked+"!irc@"+c.s.S.ServerName, "NICK", c.nick)
	}
	c.welcome()
	return true
}

func (c *client) welcome() {
	s := c.s
	c.reply(RPL_WELCOME, fmt.Sprintf("Welcome to %s, %s", s.B.Name, c.source()))
	c.reply(RPL_YOURHOST, fmt.Sprintf("Your host is %s, the chat of %s", s.S.ServerName, s.B.Name))
	c.reply(RPL_CREATED, "This server was started "+s.created.Format(time.RFC1123))
	c.reply(RPL_MYINFO, s.S.ServerName, "bbs-sec", "io", "nt")
	for _, line := range s.support.CachedReply {
		c.reply(RPL_ISUPPORT, append(line, "are supported by this server")...)
	}
	c.lusers()
	c.motd()
	c.send(nil, "", "MODE", c.nick, "+i")
}

func (c *client) lusers() {
	nodes := c.s.B.Nodes()
	c.reply(RPL_LUSERCLIENT, fmt.Sprintf("There are %d users and 0 invisible on 1 server", len(nodes)))
	c.reply(RPL_LUSERCHANNELS, strconv.Itoa(len(c.n.Rooms())), "channels formed")
	c.reply(RPL_LUSERME, fmt.Sprintf("I have %d clients and 0 servers", c.s.Clients()))
}

func (c *client) motd() {
	text := strings.TrimRight(c.s.B.MOTDText(), "\n")
	if text == "" {
		c.reply(ERR_NOMOTD, "There is no message of the day")
		return
	}
	c.reply(RPL_MOTDSTART, "- "+c.s.S.ServerName+" Message of the day - ")
	for _, l := range strings.Split(text, "\n") {
		c.reply(RPL_MOTD, "- "+strings.TrimRight(l, "\r"))
	}
	c.reply(RPL_ENDOFMOTD, "End of /MOTD command")
}

func sorted(s []string) []string {
	slices.Sort(s)
	return s
}

// stripped is a line from IRC as the BBS takes it: no colors or other
// formatting.
func stripped(text string) string { return ircfmt.Strip(text) }
