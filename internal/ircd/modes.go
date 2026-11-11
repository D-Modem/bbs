package ircd

import (
	"errors"
	"strconv"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
)

// The channel modes on IRC: MODE's changes and lists, INVITE and KNOCK.
// The modes themselves are the rooms' (bbs.Node.SetModes).

// numerics of Solanum's that ergo's list lacks
const (
	RPL_QUIETLIST      = "728"
	RPL_ENDOFQUIETLIST = "729"
	RPL_KNOCK          = "710"
	RPL_KNOCKDLVR      = "711"
	ERR_TOOMANYKNOCK   = "712"
	ERR_CHANOPEN       = "713"
	ERR_KNOCKONCHAN    = "714"
	ERR_CANNOTKNOCK    = "480"
)

// chanMode is MODE #room with changes (up to 4 with parameters) by the
// room's ops; b, q, e or I with no mask lists that list.
func (c *client) chanMode(name, modes string, args []string) {
	on := true
	var changes []bbs.ModeChange
	withArg := 0
	for _, m := range modes {
		switch {
		case m == '+':
			on = true
			continue
		case m == '-':
			on = false
			continue
		case strings.ContainsRune(bbs.RoomListModes, m) && len(args) == 0:
			c.sendList(name, m)
			continue
		}
		ch := bbs.ModeChange{On: on, Mode: m}
		if bbs.ModeTakesArg(m, on) {
			if len(args) == 0 {
				if m == 'k' && !on {
					changes = append(changes, ch) // -k needs no key here
				}
				continue
			}
			ch.Arg, args = args[0], args[1:]
			if withArg++; withArg > 4 {
				break
			}
		}
		changes = append(changes, ch)
	}
	if len(changes) == 0 {
		return
	}
	_, errs := c.n.SetModes(name, changes)
	for _, err := range errs {
		var me *bbs.ModeError
		switch {
		case errors.Is(err, bbs.ErrNoUser):
			c.reply(ERR_NOSUCHNICK, "*", "No such nick")
		case errors.Is(err, bbs.ErrNotThere):
			c.reply(ERR_USERNOTINCHANNEL, "*", "#"+name, "They aren't on that channel")
		case errors.As(err, &me) && strings.Contains(me.Error(), "not a mode"):
			c.reply(ERR_UNKNOWNMODE, string(me.Mode), "is not a mode the BBS's rooms have")
		case errors.As(err, &me) && strings.Contains(me.Error(), "list is full"):
			c.reply(ERR_BANLISTFULL, "#"+name, string(me.Mode), capital(me.Error()))
		default:
			c.reply(ERR_CHANOPRIVSNEEDED, "#"+name, capital(err.Error()))
		}
	}
}

// sendList is a room's list of a kind: bans (367), quiets (728),
// exceptions (348), invite exceptions (346).
func (c *client) sendList(name string, kind rune) {
	ch := "#" + name
	if c.s.B.RoomSecret(name) && !c.n.InRoom(name) && !c.u.Sysop {
		kind = 0 // a secret room's lists are its own
	}
	entries := c.s.B.RoomList(name, kind)
	row := func(e string, by string, at int64) []string {
		return []string{ch, e, by, strconv.FormatInt(at, 10)}
	}
	switch kind {
	case 'q':
		for _, e := range entries {
			c.reply(RPL_QUIETLIST, ch, "q", e.Mask, e.SetBy, strconv.FormatInt(e.SetAt.Unix(), 10))
		}
		c.reply(RPL_ENDOFQUIETLIST, ch, "q", "End of channel quiet list")
	case 'e':
		for _, e := range entries {
			c.reply(RPL_EXCEPTLIST, row(e.Mask, e.SetBy, e.SetAt.Unix())...)
		}
		c.reply(RPL_ENDOFEXCEPTLIST, ch, "End of channel exception list")
	case 'I':
		for _, e := range entries {
			c.reply(RPL_INVITELIST, row(e.Mask, e.SetBy, e.SetAt.Unix())...)
		}
		c.reply(RPL_ENDOFINVITELIST, ch, "End of channel invite exception list")
	default:
		for _, e := range entries {
			c.reply(RPL_BANLIST, row(e.Mask, e.SetBy, e.SetAt.Unix())...)
		}
		c.reply(RPL_ENDOFBANLIST, ch, "End of channel ban list")
	}
}

// invite is INVITE nick #room.
func (c *client) invite(nick, ch string) {
	name := room(ch)
	if nick == "" || name == "" {
		c.reply(ERR_NEEDMOREPARAMS, "INVITE", "Not enough parameters")
		return
	}
	t, err := c.n.Invite(name, nick)
	switch {
	case err == nil:
		c.reply(RPL_INVITING, Nick(t.Name), "#"+name)
	case errors.Is(err, bbs.ErrNoUser), errors.Is(err, bbs.ErrNotOn):
		c.reply(ERR_NOSUCHNICK, nick, "No such nick")
	case errors.Is(err, bbs.ErrNotInRoom):
		c.reply(ERR_NOTONCHANNEL, "#"+name, "You're not on that channel")
	case errors.Is(err, bbs.ErrAlreadyIn):
		c.reply(ERR_USERONCHANNEL, nick, "#"+name, "is already on channel")
	default:
		c.reply(ERR_CHANOPRIVSNEEDED, "#"+name, capital(err.Error()))
	}
}

// knock is KNOCK #room [why].
func (c *client) knock(ch, text string) {
	name := room(ch)
	if name == "" {
		c.reply(ERR_NEEDMOREPARAMS, "KNOCK", "Not enough parameters")
		return
	}
	err := c.n.Knock(name, text)
	switch {
	case err == nil:
		c.reply(RPL_KNOCKDLVR, "#"+name, "Your KNOCK has been delivered")
	case errors.Is(err, bbs.ErrAlreadyIn):
		c.reply(ERR_KNOCKONCHAN, "#"+name, "You're already on that channel")
	case errors.Is(err, bbs.ErrRoomOpen):
		c.reply(ERR_CHANOPEN, "#"+name, "Channel is open")
	case errors.Is(err, bbs.ErrKnocked):
		c.reply(ERR_TOOMANYKNOCK, "#"+name, "Too many KNOCKs (channel)")
	default:
		c.reply(ERR_CANNOTKNOCK, "#"+name, "Cannot knock: "+err.Error())
	}
}
