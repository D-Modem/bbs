package ircd

// The services' framework: a service's commands, their parameters, HELP
// and CTCP.  Adapted from ergo's irc/services.go (github.com/ergochat/ergo),
// under its MIT license: see LICENSE.ergo.

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ergochat/irc-go/ircfmt"
)

// service is a services bot: messages to its nick, or its own commands
// (/BBSSERV, /BS), run its commands.
type service struct {
	Name           string
	ShortName      string
	CommandAliases []string // IRC commands that talk to it
	Commands       map[string]*serviceCommand
	HelpBanner     string
}

// serviceCommand is one of a service's commands.
type serviceCommand struct {
	aliasOf           string // this command is another's other name
	handler           func(svc *service, c *client, command string, params []string)
	help              string
	helpShort         string
	minParams         int
	maxParams         int  // if set, more parameters are an error
	unsplitFinalParam bool // split into at most maxParams, the last with its spaces
}

// lookup finds a command, through an alias.
func (svc *service) lookup(command string) *serviceCommand {
	for depth := 0; depth <= 1; depth++ {
		cmd, ok := svc.Commands[command]
		if !ok {
			return nil
		}
		if cmd.aliasOf == "" {
			return cmd
		}
		command = cmd.aliasOf
	}
	return nil
}

// source is the service's nick!user@host.
func (svc *service) source(c *client) string {
	return svc.Name + "!" + svc.Name + "@" + c.s.S.ServerName
}

// notice is a line from the service to the client.
func (svc *service) notice(c *client, text string) {
	c.send(nil, svc.source(c), "NOTICE", c.nick, text)
}

// fieldsN is strings.Fields, into at most n fields: the last keeps the
// rest of the line.
func fieldsN(s string, n int) []string {
	var out []string
	for len(out) < n-1 {
		s = strings.TrimLeft(s, " ")
		if s == "" {
			return out
		}
		i := strings.IndexByte(s, ' ')
		if i < 0 {
			return append(out, s)
		}
		out = append(out, s[:i])
		s = s[i+1:]
	}
	if s = strings.TrimLeft(s, " "); s != "" {
		out = append(out, s)
	}
	return out
}

// privmsg runs what was sent to the service (/msg BBSServ INFO).
func (svc *service) privmsg(c *client, message string) {
	if strings.HasPrefix(message, "\x01") {
		svc.ctcp(c, message)
		return
	}
	params := strings.Fields(message)
	if len(params) == 0 {
		return
	}
	name := strings.ToLower(params[0])
	cmd := svc.lookup(name)
	if cmd != nil && cmd.unsplitFinalParam {
		params = fieldsN(message, cmd.maxParams+1)[1:]
	} else {
		params = params[1:]
	}
	svc.run(c, cmd, name, params)
}

// command runs the service's own IRC command (/BS INFO), as a message to
// it: the parameters as one line, however the client split them.
func (svc *service) command(c *client, params []string) {
	line := strings.Join(params, " ")
	if strings.TrimSpace(line) == "" {
		line = "help"
	}
	svc.privmsg(c, line)
}

// ctcp answers a CTCP VERSION, PING or TIME sent to the service; others
// get nothing.
func (svc *service) ctcp(c *client, message string) {
	ctcp := fieldsN(strings.TrimSuffix(message[1:], "\x01"), 2)
	if len(ctcp) == 0 {
		return
	}
	out := ""
	switch strings.ToUpper(ctcp[0]) {
	case "VERSION":
		out = svc.Name + " (bbs-sec)"
	case "PING":
		if len(ctcp) > 1 {
			out = ctcp[1]
		}
	case "TIME":
		out = time.Now().UTC().Format(time.RFC1123)
	}
	if out != "" {
		c.send(nil, svc.source(c), "NOTICE", c.nick, fmt.Sprintf("\x01%s %s\x01", strings.ToUpper(ctcp[0]), out))
	}
}

// run runs cmd (nil: unknown) with params, or says why not: too few or
// too many parameters. A command with no handler is HELP.
func (svc *service) run(c *client, cmd *serviceCommand, name string, params []string) {
	if cmd == nil {
		svc.notice(c, fmt.Sprintf("Unknown command. To see the commands, /msg %s HELP", svc.Name))
		return
	}
	if len(params) < cmd.minParams || 0 < cmd.maxParams && cmd.maxParams < len(params) {
		svc.notice(c, fmt.Sprintf("Invalid parameters. For how to use it, /msg %s HELP %s", svc.Name, strings.ToUpper(name)))
		return
	}
	if cmd.handler == nil { // HELP
		svc.helpHandler(c, params)
		return
	}
	cmd.handler(svc, c, name, params)
}

// helpHandler shows the service's commands, or one command's help.
func (svc *service) helpHandler(c *client, params []string) {
	svc.notice(c, ircfmt.Unescape(fmt.Sprintf("*** $b%s HELP$b ***", svc.Name)))
	if len(params) == 0 {
		lines := append(strings.Split(ircfmt.Unescape(svc.HelpBanner), "\n"),
			"",
			"For a command's details:",
			ircfmt.Unescape(fmt.Sprintf("    $b/msg %s HELP <command>$b", svc.Name)),
			"",
			"The commands:")
		var short []string
		for _, cmd := range svc.Commands {
			if cmd.aliasOf == "" {
				short = append(short, "    "+ircfmt.Unescape(cmd.helpShort))
			}
		}
		slices.Sort(short)
		for _, l := range append(lines, short...) {
			svc.notice(c, l)
		}
	} else if cmd := svc.lookup(strings.ToLower(params[0])); cmd == nil {
		svc.notice(c, fmt.Sprintf("Unknown command. To see the commands, /msg %s HELP", svc.Name))
	} else {
		for _, l := range strings.Split(ircfmt.Unescape(cmd.help), "\n") {
			svc.notice(c, l)
		}
	}
	svc.notice(c, ircfmt.Unescape(fmt.Sprintf("*** $bEnd of %s HELP$b ***", svc.Name)))
}
