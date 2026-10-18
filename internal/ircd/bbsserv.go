package ircd

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// BBSServ: a user's own account, from IRC.  Every IRC user is logged in,
// so every command is about the one asking.

const bbsservHelp = `$b%s$b is your BBS account: what you have set, and
setting it, as your profile on the BBS does.`

// newBBSServ is the account service under nick name: its commands, and
// the IRC commands that reach it, its name upper-cased and a short one
// (/BS for BBSServ; for another name, its name again).
func newBBSServ(name string) *service {
	short := "BS"
	if fold(name) != "bbsserv" {
		short = strings.ToUpper(name)
	}
	svc := &service{Name: name, ShortName: short, CommandAliases: []string{strings.ToUpper(name), short},
		HelpBanner: fmt.Sprintf(bbsservHelp, name)}
	svc.Commands = map[string]*serviceCommand{
		"help": {
			help:      "Syntax: $bHELP [command]$b\n\nHELP shows the commands, or how to use one.",
			helpShort: "$bHELP$b shows the commands, or how to use one.",
		},
		"info": {
			handler:   bsInfo,
			help:      "Syntax: $bINFO$b\n\nINFO shows your account: where you call from, your alias and\nsignature, your settings, your mail, keys and tokens.",
			helpShort: "$bINFO$b shows your account.",
			maxParams: 0,
		},
		"set": {
			handler: bsSet,
			help: `Syntax: $bSET <setting> [value]$b

SET changes one of your settings; with no value (or -) it clears it.
    $bLOCATION$b    where you call from
    $bABOUT$b       a line about yourself
    $bALIAS$b       a name to post under where areas allow
    $bSIGNATURE$b   up to three lines under what you write, parted by |
    $bANSI$b, $bUTF8$b, $bSIXEL$b, $bRIP$b  on or off, for your terminal
    $bWIDTH$b       40 or 80, a Commodore's columns
    $bEDITOR$b      full or line
    $bEXPERT$b      on or off: menus as their prompt alone
Terminal settings cleared go back to what your terminal says.`,
			helpShort:         "$bSET$b changes one of your settings.",
			minParams:         1,
			maxParams:         2,
			unsplitFinalParam: true,
		},
		"passwd": {
			handler:   bsPasswd,
			help:      "Syntax: $bPASSWD <password now> <new password>$b\n\nPASSWD changes your password (best over TLS).",
			helpShort: "$bPASSWD$b changes your password.",
			minParams: 2,
			maxParams: 2,
		},
		"scan": {
			handler: bsScan,
			help: `Syntax: $bSCAN [ON|OFF <area>|ALL]$b

SCAN lists the message areas, and whether your new scan (and your QWK
packets) take them in.  SCAN ON or OFF puts an area in, or leaves it
out; ALL is every area.`,
			helpShort:         "$bSCAN$b shows or changes the areas of your new scan.",
			maxParams:         2,
			unsplitFinalParam: true,
		},
		"keys": {
			handler: bsKeys,
			help: `Syntax: $bKEYS [ADD <password> <key> | DEL <number>]$b

KEYS lists the SSH keys you log in with.  KEYS ADD adds one (a line
of ~/.ssh/id_ed25519.pub; your password, since a key opens your
account); KEYS DEL takes one away, by its number in the list.`,
			helpShort:         "$bKEYS$b lists, adds and deletes your SSH keys.",
			maxParams:         3,
			unsplitFinalParam: true,
		},
		"tokens": {
			handler: bsTokens,
			help: `Syntax: $bTOKENS [REVOKE <id>]$b

TOKENS lists your HTTP API tokens (keys and logins' sessions);
TOKENS REVOKE ends one.`,
			helpShort: "$bTOKENS$b lists and revokes your API tokens.",
			maxParams: 2,
		},
	}
	svc.Commands["rooms"] = &serviceCommand{
		handler:   bsRooms,
		help:      "Syntax: $bROOMS$b\n\nROOMS lists the permanent rooms: channels " + name + " keeps open\nwhen no one is in them, each with its topic.",
		helpShort: "$bROOMS$b lists the permanent rooms.",
		maxParams: 0,
	}
	svc.Commands["room"] = &serviceCommand{
		handler: bsRoom,
		help: `Syntax: $bROOM ADD <#room> [topic]$b
        $bROOM DEL <#room>$b
        $bROOM TOPIC <#room> [topic]$b

For the sysop.  ROOM ADD makes a room permanent: ` + name + ` holds it
open, empty or not, across restarts, on IRC and on the BBS alike.  ROOM
DEL lets it go when its last caller leaves.  ROOM TOPIC sets its topic
(none: clears it), as TOPIC does in the channel.`,
		helpShort:         "$bROOM$b makes rooms permanent, and sets their topics (sysops).",
		minParams:         2,
		maxParams:         3,
		unsplitFinalParam: true,
	}
	svc.Commands["channel"] = &serviceCommand{aliasOf: "room"}
	svc.Commands["channels"] = &serviceCommand{aliasOf: "rooms"}
	svc.Commands["account"] = &serviceCommand{aliasOf: "info"}
	svc.Commands["password"] = &serviceCommand{aliasOf: "passwd"}
	return svc
}

// fresh is the client's user as the database has it now.
func (c *client) fresh() *store.User {
	if u := c.n.Refresh(); u != nil {
		c.u = u
	}
	return c.u
}

// onOff is b as "on" or "off".
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// bsInfo is INFO: the caller's account, profile, settings, and counts of
// their unread mail, SSH keys, API tokens and areas in their new scan.
func bsInfo(svc *service, c *client, _ string, _ []string) {
	b := c.s.B
	u := c.fresh()
	say := func(f string, a ...any) { svc.notice(c, fmt.Sprintf(f, a...)) }
	what := fmt.Sprintf("level %d", u.Level)
	if u.Flags != "" {
		what += ", flags " + u.Flags
	}
	if u.Sysop {
		what += ", the sysop"
	}
	say("Account: %s (%s)", u.Name, what)
	say("Since %s; %d calls, %d posts", u.Created.Format("2006-01-02"), u.Logins, u.Posts)
	show := func(label, v string) {
		if v == "" {
			v = "(none)"
		}
		say("%-10s %s", label+":", v)
	}
	show("Location", u.Location)
	show("About", u.About)
	show("Alias", u.Alias)
	show("Signature", strings.ReplaceAll(u.Signature, "\n", " | "))
	prefs := bbs.ParsePrefs(u.Prefs).String()
	if prefs == "" {
		prefs = "(none: your terminal decides)"
	}
	say("Settings:  %s", prefs)
	unread, _ := b.Store.UnreadMail(u.ID)
	keys, _ := b.Store.SSHKeys(u.ID)
	tokens, _ := b.Store.Tokens(u.ID)
	say("Mail: %d unread; %d SSH keys; %d API tokens", unread, len(keys), len(tokens))
	if areas, err := b.AreasFor(u); err == nil {
		off, _ := b.Store.ScanOff(u.ID)
		in := 0
		for _, a := range areas {
			if !off[a.ID] {
				in++
			}
		}
		say("New scan: %d of %d areas (/msg %s SCAN)", in, len(areas), svc.Name)
	}
}

// cleared: a value that clears a setting.
func cleared(v string) bool { v = strings.TrimSpace(v); return v == "" || v == "-" }

// bsSet is SET: one of the profile's fields (location, about, alias,
// signature) or a terminal setting changed, or cleared by none or "-",
// checked as the BBS checks its own.
func bsSet(svc *service, c *client, _ string, params []string) {
	b := c.s.B
	u := c.fresh()
	what := strings.ToLower(params[0])
	value := ""
	if len(params) > 1 {
		value = strings.TrimSpace(params[1])
	}
	if cleared(value) {
		value = ""
	}
	var err error
	switch what {
	case "location":
		value = bbs.Clean(value, bbs.MaxLocationLen)
		err = b.Store.SetLocation(u.ID, value)
	case "about":
		value = bbs.Clean(value, 60)
		err = b.Store.SetAbout(u.ID, value)
	case "alias":
		if value != "" {
			if msg := c.n.CheckNewName(value); msg != "" {
				svc.notice(c, msg)
				return
			}
		}
		if err = b.Store.SetAlias(u.ID, value); errors.Is(err, store.ErrExists) {
			svc.notice(c, "That is someone's name or alias.")
			return
		}
	case "signature":
		var ls []string
		for _, l := range strings.Split(value, "|") {
			if l = bbs.Clean(l, 70); l != "" {
				ls = append(ls, l)
			}
		}
		if len(ls) > 3 {
			svc.notice(c, "A signature is at most three lines.")
			return
		}
		value = strings.Join(ls, "\n")
		err = b.Store.SetSignature(u.ID, value)
	default:
		if !slices.Contains(bbs.PrefKeys(), what) {
			svc.notice(c, fmt.Sprintf("There is no setting %q: /msg %s HELP SET", params[0], svc.Name))
			return
		}
		var prefs string
		if prefs, err = bbs.ChangePrefs(u.Prefs, map[string]string{what: strings.ToLower(value)}); err != nil {
			svc.notice(c, capital(err.Error())+".")
			return
		}
		err = b.Store.SetPrefs(u.ID, prefs)
	}
	if err != nil {
		svc.notice(c, "That could not be saved: "+err.Error())
		return
	}
	c.fresh()
	b.Log.Printf("node %d: %s set their %s (irc)", c.n.Num, u.Name, what)
	if value == "" {
		svc.notice(c, fmt.Sprintf("Your %s is cleared.", what))
	} else {
		svc.notice(c, fmt.Sprintf("Your %s is now: %s", what, strings.ReplaceAll(value, "\n", " | ")))
	}
}

// bsPasswd is PASSWD: the password changed, given the one now.
func bsPasswd(svc *service, c *client, _ string, params []string) {
	if err := c.n.ChangePassword(params[0], params[1]); err != nil {
		svc.notice(c, capital(err.Error())+".")
		return
	}
	svc.notice(c, "Your password is changed.")
}

// bsScan is SCAN: the caller's areas and whether each is in their new
// scan; SCAN ON or OFF with an area's name, or ALL, puts it in or out.
func bsScan(svc *service, c *client, _ string, params []string) {
	b := c.s.B
	u := c.fresh()
	areas, err := b.AreasFor(u)
	if err != nil {
		svc.notice(c, "The areas could not be read: "+err.Error())
		return
	}
	off, err := b.Store.ScanOff(u.ID)
	if err != nil {
		svc.notice(c, "The areas could not be read: "+err.Error())
		return
	}
	if len(params) == 0 {
		if len(areas) == 0 {
			svc.notice(c, "There are no message areas.")
		}
		for _, a := range areas {
			svc.notice(c, fmt.Sprintf("%-24s %-3s %d new", a.Name, onOff(!off[a.ID]), a.New))
		}
		return
	}
	on := false
	switch strings.ToLower(params[0]) {
	case "on":
		on = true
	case "off":
	default:
		svc.notice(c, fmt.Sprintf("SCAN ON or OFF: /msg %s HELP SCAN", svc.Name))
		return
	}
	if len(params) < 2 {
		svc.notice(c, "Which area (or ALL)?")
		return
	}
	n := 0
	for _, a := range areas {
		if strings.EqualFold(params[1], "all") || strings.EqualFold(a.Name, params[1]) {
			if err := b.Store.SetScan(u.ID, a.ID, on); err != nil {
				svc.notice(c, "That could not be saved: "+err.Error())
				return
			}
			n++
		}
	}
	if n == 0 {
		svc.notice(c, fmt.Sprintf("There is no area %q (/msg %s SCAN lists them).", params[1], svc.Name))
		return
	}
	svc.notice(c, fmt.Sprintf("%d areas %s your new scan.", n, map[bool]string{true: "are in", false: "are out of"}[on]))
}

// bsKeys is KEYS: the caller's SSH keys listed; ADD one, given the
// password again; DEL one by its number in the list.  An external
// account's keys are not changed here.
func bsKeys(svc *service, c *client, _ string, params []string) {
	b := c.s.B
	u := c.fresh()
	if u.External && len(params) > 0 {
		svc.notice(c, "Your account's keys are kept by the system you came from.")
		return
	}
	keys, err := b.Store.SSHKeys(u.ID)
	if err != nil {
		svc.notice(c, "Your keys could not be read: "+err.Error())
		return
	}
	if len(params) == 0 {
		if len(keys) == 0 {
			svc.notice(c, "You have no SSH keys: you log in by SSH with your password.")
		}
		for i, k := range keys {
			svc.notice(c, fmt.Sprintf("%d. %s %s %s", i+1, k.Type, k.Fingerprint, k.Comment))
		}
		return
	}
	switch strings.ToLower(params[0]) {
	case "add":
		if len(params) < 3 {
			svc.notice(c, "KEYS ADD <password> <key>")
			return
		}
		if _, err := b.Authenticate("irc", "ip:"+c.host, u.Name, params[1]); err != nil {
			svc.notice(c, capital(err.Error())+".")
			return
		}
		k, err := b.AddSSHKey(u, params[2])
		if err != nil {
			svc.notice(c, capital(err.Error())+".")
			return
		}
		svc.notice(c, "Added "+k.Fingerprint+".")
	case "del", "delete":
		i, err := strconv.Atoi(param2(params))
		if err != nil || i < 1 || i > len(keys) {
			svc.notice(c, fmt.Sprintf("Which key? Its number in /msg %s KEYS.", svc.Name))
			return
		}
		if err := b.Store.RemoveSSHKey(keys[i-1].ID, u.ID); err != nil {
			svc.notice(c, "It could not be deleted: "+err.Error())
			return
		}
		b.Log.Printf("%s: SSH key deleted: %s (irc)", u.Name, keys[i-1].Fingerprint)
		svc.notice(c, "Deleted "+keys[i-1].Fingerprint+".")
	default:
		svc.notice(c, fmt.Sprintf("KEYS, KEYS ADD or KEYS DEL: /msg %s HELP KEYS", svc.Name))
	}
}

// bsTokens is TOKENS: the caller's API tokens listed, or REVOKE one by
// its id.
func bsTokens(svc *service, c *client, _ string, params []string) {
	b := c.s.B
	u := c.fresh()
	if len(params) == 0 {
		ts, err := b.Store.Tokens(u.ID)
		if err != nil {
			svc.notice(c, "Your tokens could not be read: "+err.Error())
			return
		}
		if len(ts) == 0 {
			svc.notice(c, "You have no API tokens.")
		}
		for _, t := range ts {
			kind := "key"
			if t.Session {
				kind = "login"
			}
			used := "never used"
			if !t.LastUsed.IsZero() {
				used = "used " + t.LastUsed.Format("2006-01-02 15:04")
			}
			svc.notice(c, fmt.Sprintf("%d. %s %q (%s), %s", t.ID, kind, t.Name, t.Scopes, used))
		}
		return
	}
	if strings.ToLower(params[0]) != "revoke" {
		svc.notice(c, fmt.Sprintf("TOKENS or TOKENS REVOKE <id>: /msg %s HELP TOKENS", svc.Name))
		return
	}
	id, err := strconv.ParseInt(param2(params), 10, 64)
	if err != nil {
		svc.notice(c, "Which token? Its number in the list.")
		return
	}
	if err := b.Store.RevokeToken(id, u.ID); err != nil {
		svc.notice(c, "There is no token of yours with that number.")
		return
	}
	b.Log.Printf("%s: API token %d revoked (irc)", u.Name, id)
	svc.notice(c, "Revoked.")
}

// param2 is the second parameter trimmed, or "".
func param2(params []string) string {
	if len(params) > 1 {
		return strings.TrimSpace(params[1])
	}
	return ""
}

// capital is s with its first letter upper case (an error as a sentence).
func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// bsRooms is ROOMS: the permanent rooms, how many are in each, and its
// topic.
func bsRooms(svc *service, c *client, _ string, _ []string) {
	rs := c.s.B.PermanentRooms()
	if len(rs) == 0 {
		svc.notice(c, "There are no permanent rooms.")
		return
	}
	for _, r := range rs {
		line := fmt.Sprintf("#%-20s %d in", r.Name, len(r.Users))
		if r.Topic != "" {
			line += "  " + r.Topic
		}
		svc.notice(c, line)
	}
}

// bsRoom is ROOM, the sysop's alone: ADD makes a room permanent, DEL lets
// it go, TOPIC sets its topic.
func bsRoom(svc *service, c *client, _ string, params []string) {
	b := c.s.B
	if !c.fresh().Sysop {
		svc.notice(c, "Permanent rooms are the sysop's to make.")
		return
	}
	name := strings.TrimPrefix(params[1], "#")
	topic := ""
	if len(params) > 2 {
		topic = params[2]
	}
	var err error
	switch strings.ToLower(params[0]) {
	case "add":
		if err = b.AddPermanentRoom(name, topic, c.u.Name); err == nil {
			svc.notice(c, fmt.Sprintf("#%s is a permanent room: %s holds it open.", strings.ToLower(name), svc.Name))
		}
	case "del", "delete":
		if err = b.RemovePermanentRoom(name, c.u.Name); err == nil {
			svc.notice(c, fmt.Sprintf("#%s goes when its last caller leaves.", strings.ToLower(name)))
		}
	case "topic":
		if err = b.SetRoomTopic(name, topic, c.u.Name); err == nil {
			svc.notice(c, fmt.Sprintf("The topic of #%s is set.", strings.ToLower(name)))
		}
	default:
		svc.notice(c, fmt.Sprintf("ROOM ADD, DEL or TOPIC: /msg %s HELP ROOM", svc.Name))
		return
	}
	if err != nil {
		svc.notice(c, capital(err.Error())+".")
	}
}
