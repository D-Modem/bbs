package ircd

import (
	"errors"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
)

// ChanServ: the rooms' registration and access lists.  The commands are
// the BBS's own (bbs.Node.ChanServ, which the BBS chat's /cs runs too);
// here they come as messages to the bot, or its own IRC command (/CS).

// newChanServ is the rooms' service under nick name.
func newChanServ(name string) *service {
	short := "CS"
	if fold(name) != "chanserv" {
		short = strings.ToUpper(name)
	}
	return &service{Name: name, ShortName: short, CommandAliases: []string{strings.ToUpper(name), short},
		raw: csLine}
}

// csLine runs a line sent to ChanServ, its answers as notices.
func csLine(svc *service, c *client, line string) {
	for _, l := range c.n.ChanServ(line) {
		if l == "" {
			l = " "
		}
		svc.notice(c, l)
	}
}

// serviceReal is a service's real name, as WHO and WHOIS give it.
func (s *Server) serviceReal(svc *service) string {
	if svc == s.cs {
		return "The registered rooms and their access lists"
	}
	return "Your BBS account and the permanent rooms"
}

// eventSource is who a Mode or Kicked came from: a service, or someone on
// a node.
func (c *client) eventSource(e bbs.Event) string {
	if svc := c.s.service(e.From); svc != nil && e.Node == 0 {
		return svc.source(c)
	}
	return c.sourceOf(e.From, e.Node)
}

// nodeOfUser is a node a user is on (0: none).
func (c *client) nodeOfUser(name string) int {
	for _, n := range c.s.B.Nodes() {
		if n.User == name {
			return n.Num
		}
	}
	return 0
}

// chanMode is MODE #room with changes: +o, -o, +v and -v (up to MODES of
// them) by the room's ops; the rest are not the BBS's yet.
func (c *client) chanMode(name, modes string, args []string) {
	on, changes := true, 0
	for _, m := range modes {
		switch m {
		case '+':
			on = true
		case '-':
			on = false
		case 'o', 'v':
			if len(args) == 0 {
				continue
			}
			nick := args[0]
			args = args[1:]
			if changes++; changes > 4 {
				return
			}
			err := c.n.SetStatus(name, nick, m == 'o', on)
			switch {
			case err == nil:
			case errors.Is(err, bbs.ErrNoUser):
				c.reply(ERR_NOSUCHNICK, nick, "No such nick")
			case errors.Is(err, bbs.ErrNotThere):
				c.reply(ERR_USERNOTINCHANNEL, nick, "#"+name, "They aren't on that channel")
			default:
				c.reply(ERR_CHANOPRIVSNEEDED, "#"+name, capital(err.Error()))
			}
		case 'n', 't':
			if !on {
				c.reply(ERR_CHANOPRIVSNEEDED, "#"+name, "The BBS's rooms are always +nt")
			}
		default:
			c.reply(ERR_UNKNOWNMODE, string(m), "is not a mode the BBS's rooms have")
		}
	}
}
