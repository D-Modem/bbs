package bbs

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// ChanServ: rooms' registration and access lists, as a command language
// that IRC's ChanServ bot and the BBS chat's /cs both speak.  Every
// command answers with lines for the one asking.

// chanServCommand is one of ChanServ's commands.
type chanServCommand struct {
	syntax string
	short  string
	help   string
	run    func(n *Node, u *store.User, args []string) ([]string, error)
}

var chanServCommands map[string]*chanServCommand

// chanServAliases are other names of commands.
var chanServAliases = map[string]string{"access": "flags", "unregister": "drop", "remove": "kick", "akick": "ban"}

func init() {
	chanServCommands = map[string]*chanServCommand{
		"register": {"REGISTER <#room>", "registers a room to you",
			`REGISTER makes a room yours: you are its founder, with every flag on
its access list, and an operator there.  Be in the room to register it.
A room no one on its access list comes into for 90 days is dropped.`, csRegister},
		"drop": {"DROP <#room>", "unregisters a room you founded",
			`DROP unregisters a room: its access list, topic and settings go.
Only its founder (or a sysop) may.`, csDrop},
		"info": {"INFO <#room>", "shows a registered room",
			`INFO shows a registered room: its founder, when it was registered and
last used, its settings and topic.`, csInfo},
		"flags": {"FLAGS <#room> [nick [+flags-flags|template]]", "shows or changes a room's access list",
			`FLAGS alone lists a room's access list (needs A).  With a nick, it shows
theirs; with changes, it changes them (needs f): +AO-v, a template
(VOP, AOP, SOP, FOUNDER), or -* to take them off the list.  You may only
give or take flags you have yourself, unless you have F.
    v  voice through ChanServ        V  voiced on coming in
    o  ops through ChanServ          O  opped on coming in
    t  sets the topic                i  invites (to come)
    r  KICK, BAN, UNBAN              R  CLEAR
    f  changes the access list       s  SET
    A  sees the access list          e  not kept out by a ban
    b  banned: kept out              F  founder: everything
Templates: VOP +AV, AOP +AOiortv, SOP +AORefiorstv, FOUNDER +AFORefiorstv.`, csFlags},
		"op": {"OP <#room> [nick ...]", "gives ops (needs o)",
			"OP makes you, or those named, operators in the room.", csStatus(true, true)},
		"deop": {"DEOP <#room> [nick ...]", "takes ops (needs o)",
			"DEOP takes operator from you, or those named.", csStatus(true, false)},
		"voice": {"VOICE <#room> [nick ...]", "gives a voice (needs v)",
			"VOICE gives you, or those named, a voice in the room.", csStatus(false, true)},
		"devoice": {"DEVOICE <#room> [nick ...]", "takes a voice (needs v)",
			"DEVOICE takes the voice from you, or those named.", csStatus(false, false)},
		"topic": {"TOPIC <#room> [text]", "sets a room's topic (needs t)",
			"TOPIC sets the room's topic; with no text, clears it.", csTopic},
		"kick": {"KICK <#room> <nick> [reason]", "puts one out of a room (needs r)",
			"KICK puts someone out of the room, on every way in: IRC, the BBS and the rest.", csKick},
		"ban": {"BAN <#room> <nick> [reason]", "bans one from a room (needs r)",
			`BAN puts b on someone's entry in the access list, keeping them out,
and puts them out if they are in.  UNBAN takes it off.`, csBan},
		"unban": {"UNBAN <#room> <nick>", "lets one banned in again (needs r)",
			"UNBAN takes b off someone's entry, so they may come in again.", csUnban},
		"clear": {"CLEAR <#room> USERS [reason]", "puts everyone out of a room (needs R)",
			"CLEAR USERS puts everyone out of the room but you.", csClear},
		"set": {"SET <#room> <setting> [value]", "changes a room's settings (needs s)",
			`SET changes a registered room's settings:
    FOUNDER <nick>         gives the room to another (the founder alone)
    GUARD ON|OFF           ChanServ holds the room open, empty or not
    TOPICLOCK ON|OFF       only those with t set the topic, ops or not
    PRIVATE ON|OFF         INFO shows little to those without A
    DESCRIPTION [text]     what the room is for, in INFO
    URL [text]             a web page, in INFO
    ENTRYMSG [text]        told to everyone coming in
With no text (or -), a text is cleared.`, csSet},
		"list": {"LIST [ALL]", "lists the rooms you are on the access list of",
			"LIST lists the rooms you have flags in.  A sysop's LIST ALL lists every registered room.", csList},
		"why": {"WHY <#room> [nick]", "says why one has their status",
			"WHY says what in the access list gives you (or someone) their flags.", csWhy},
	}
}

// ChanServHelp is ChanServ's help: its commands, or one's.
func ChanServHelp(name, command string) []string {
	if command == "" {
		out := []string{
			name + " looks after registered rooms: who founded them, and who has",
			"which flags there (ops and voice on coming in, ops' commands).",
			"",
			"The commands:",
		}
		var names []string
		for k := range chanServCommands {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			out = append(out, fmt.Sprintf("    %-9s %s", strings.ToUpper(k), chanServCommands[k].short))
		}
		return append(out, "", "For a command's details: HELP <command>")
	}
	cmd := chanServLookup(command)
	if cmd == nil {
		return []string{"There is no such command: HELP lists them."}
	}
	return append([]string{"Syntax: " + cmd.syntax, ""}, strings.Split(cmd.help, "\n")...)
}

func chanServLookup(name string) *chanServCommand {
	name = strings.ToLower(name)
	if a, ok := chanServAliases[name]; ok {
		name = a
	}
	return chanServCommands[name]
}

// ChanServ runs a ChanServ command line for the node's user, and gives the
// lines that answer it.
func (n *Node) ChanServ(line string) []string {
	f := strings.Fields(line)
	if len(f) == 0 || strings.EqualFold(f[0], "help") {
		topic := ""
		if len(f) > 1 {
			topic = f[1]
		}
		return ChanServHelp(n.b.ChanServ(), topic)
	}
	cmd := chanServLookup(f[0])
	if cmd == nil {
		return []string{"Unknown command.  HELP lists them."}
	}
	u := n.User()
	if u == nil {
		return []string{"You are not logged in."}
	}
	n.b.expire()
	out, err := cmd.run(n, u, f[1:])
	if err != nil {
		if errors.Is(err, errSyntax) {
			return []string{"Syntax: " + cmd.syntax}
		}
		s := err.Error()
		return []string{strings.ToUpper(s[:1]) + s[1:] + "."}
	}
	return out
}

var errSyntax = errors.New("syntax")

// roomArg is a #room argument, lower-cased and without its #.
func roomArg(s string) string { return strings.ToLower(strings.TrimPrefix(s, "#")) }

// regFor is a registered room the user has a flag in (any of need; ""
// needs none).
func (n *Node) regFor(arg string, u *store.User, need string) (*store.RoomReg, string, error) {
	name := roomArg(arg)
	reg := n.b.RoomReg(name)
	if reg == nil {
		return nil, "", fmt.Errorf("#%s is not registered", name)
	}
	flags := n.b.RoomFlags(name, u)
	if need != "" && !strings.ContainsAny(flags, need) {
		return nil, "", fmt.Errorf("you need the %s flag in #%s for that", need, name)
	}
	return reg, flags, nil
}

func csRegister(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) != 1 {
		return nil, errSyntax
	}
	name := roomArg(args[0])
	switch {
	case !roomName.MatchString(name):
		return nil, ErrNoRoom
	case strings.HasPrefix(name, "sysop-"):
		return nil, errors.New("page rooms can't be registered")
	case n.b.relayFor(name) != nil:
		return nil, errors.New("that room is a linked network's: its own services look after it")
	case !u.Sysop && !n.b.LevelOf(u).Chat:
		return nil, ErrNotAllowed
	case !u.Sysop && !n.InRoom(name):
		return nil, fmt.Errorf("come into #%s to register it", name)
	}
	if _, permanent := n.b.RoomTopic(name); permanent && !u.Sysop {
		return nil, errors.New("that room is the sysop's, a permanent one")
	}
	if err := n.registerRoom(name, u); err != nil {
		return nil, err
	}
	return []string{fmt.Sprintf("#%s is registered to you.  FLAGS #%s NICK +flags gives others theirs.", name, name)}, nil
}

func csDrop(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) != 1 {
		return nil, errSyntax
	}
	reg, _, err := n.regFor(args[0], u, "")
	if err != nil {
		return nil, err
	}
	if reg.FounderID != u.ID && !u.Sysop {
		return nil, errors.New("only the founder may drop a room")
	}
	if err := n.b.dropRoom(reg.Name); err != nil {
		return nil, err
	}
	n.b.Log.Printf("chat: %s dropped #%s", u.Name, reg.Name)
	return []string{fmt.Sprintf("#%s is unregistered.", reg.Name)}, nil
}

func when(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("Jan 2 2006 15:04")
}

func onOffWord(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func csInfo(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) != 1 {
		return nil, errSyntax
	}
	reg, flags, err := n.regFor(args[0], u, "")
	if err != nil {
		return nil, err
	}
	founder := reg.Founder
	if founder == "" {
		founder = "(gone)"
	} else {
		founder = Nick(founder)
	}
	out := []string{"Information on #" + reg.Name + ":", "Founder     : " + founder,
		"Registered  : " + when(reg.Registered)}
	if reg.Has("private") && !has(flags, FlagView) {
		return append(out, "(The room is private.)"), nil
	}
	out = append(out, "Last used   : "+when(reg.LastUsed))
	if reg.Descr != "" {
		out = append(out, "Description : "+reg.Descr)
	}
	if reg.URL != "" {
		out = append(out, "URL         : "+reg.URL)
	}
	if reg.Topic != "" {
		out = append(out, "Topic       : "+reg.Topic, fmt.Sprintf("Topic set   : by %s, %s", Nick(reg.TopicBy), when(reg.TopicAt)))
	}
	if reg.EntryMsg != "" {
		out = append(out, "Entry msg   : "+reg.EntryMsg)
	}
	var set []string
	for _, s := range []string{"guard", "topiclock", "private"} {
		if reg.Has(s) {
			set = append(set, strings.ToUpper(s))
		}
	}
	if len(set) > 0 {
		out = append(out, "Settings    : "+strings.Join(set, " "))
	}
	return out, nil
}

// sortedFlags are flags, each once, in AllRoomFlags' order.
func sortedFlags(flags string) string {
	var b strings.Builder
	for _, f := range AllRoomFlags {
		if strings.ContainsRune(flags, f) {
			b.WriteRune(f)
		}
	}
	return b.String()
}

// applyFlags changes flags by a change (+AO-v, a template, -*); it gives
// the new flags, and those added and taken.
func applyFlags(flags, change string) (string, string, string, error) {
	if t, ok := RoomTemplates[strings.ToUpper(change)]; ok {
		var added, taken string
		for _, f := range AllRoomFlags {
			in, want := has(flags, f), has(t, f)
			if want && !in {
				added += string(f)
			}
			if in && !want && f != FlagBanned {
				taken += string(f)
			}
		}
		if has(flags, FlagBanned) {
			t += "b"
		}
		return sortedFlags(t), added, taken, nil
	}
	if change == "-*" {
		return "", "", flags, nil
	}
	if change == "" || change[0] != '+' && change[0] != '-' {
		return "", "", "", errors.New("flags are +flags-flags, a template (VOP, AOP, SOP, FOUNDER) or -*")
	}
	add := true
	var added, taken string
	for _, f := range change {
		switch {
		case f == '+':
			add = true
		case f == '-':
			add = false
		case !strings.ContainsRune(AllRoomFlags, f):
			return "", "", "", fmt.Errorf("there is no flag %c (HELP FLAGS)", f)
		case add && !has(flags, f):
			flags += string(f)
			added += string(f)
		case !add && has(flags, f):
			flags = strings.ReplaceAll(flags, string(f), "")
			taken += string(f)
		}
	}
	return sortedFlags(flags), added, taken, nil
}

func plusFlags(f string) string {
	if f == "" {
		return "(none)"
	}
	return "+" + f
}

func csFlags(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, errSyntax
	}
	reg, mine, err := n.regFor(args[0], u, "")
	if err != nil {
		return nil, err
	}
	if len(args) < 3 {
		// shown: the list, or one entry (anyone may see their own)
		if len(args) == 2 {
			t, err := n.b.UserByNick(args[1])
			if err != nil {
				return nil, err
			}
			if t.ID != u.ID && !has(mine, FlagView) {
				return nil, fmt.Errorf("you need the A flag in #%s for that", reg.Name)
			}
			flags, err := n.b.Store.RoomFlags(reg.Name, t.ID)
			if err != nil {
				return nil, err
			}
			return []string{fmt.Sprintf("%s has %s in #%s.", Nick(t.Name), plusFlags(flags), reg.Name)}, nil
		}
		if !has(mine, FlagView) {
			return nil, fmt.Errorf("you need the A flag in #%s for that", reg.Name)
		}
		list, err := n.b.Store.RoomAccessList(reg.Name)
		if err != nil {
			return nil, err
		}
		out := []string{"Access list of #" + reg.Name + ":"}
		for i, a := range list {
			who := Nick(a.Name)
			if a.UserID == reg.FounderID {
				who += " (founder)"
			}
			out = append(out, fmt.Sprintf("%3d  %-24s +%-14s set by %s, %s", i+1, who, a.Flags, Nick(a.SetBy), a.SetAt.Format("Jan 2 2006")))
		}
		return append(out, fmt.Sprintf("End of the list: %d entries.", len(list))), nil
	}
	if !has(mine, FlagAccess) {
		return nil, fmt.Errorf("you need the f flag in #%s for that", reg.Name)
	}
	t, err := n.b.UserByNick(args[1])
	if err != nil {
		return nil, err
	}
	was, err := n.b.Store.RoomFlags(reg.Name, t.ID)
	if err != nil {
		return nil, err
	}
	now, added, taken, err := applyFlags(was, args[2])
	if err != nil {
		return nil, err
	}
	founder := has(mine, FlagFounder)
	switch {
	case t.ID == reg.FounderID && t.ID != u.ID && !u.Sysop:
		return nil, errors.New("the founder's flags are the founder's to change")
	case !founder && has(was, FlagFounder) && t.ID != u.ID:
		return nil, errors.New("you need F to change a founder's flags")
	case has(added+taken, FlagFounder) && !founder:
		return nil, errors.New("only a founder gives or takes F")
	}
	if !founder && t.ID != u.ID {
		for _, f := range added + taken {
			if !has(mine, f) {
				return nil, fmt.Errorf("you may only give or take flags you have (not %c)", f)
			}
		}
	}
	if t.ID == u.ID && !founder && added != "" {
		return nil, errors.New("you can't give yourself flags")
	}
	if t.ID == reg.FounderID && !has(now, FlagFounder) {
		now = sortedFlags(now + "F")
	}
	if err := n.b.Store.SetRoomAccess(reg.Name, t.ID, now, u.Name); err != nil {
		return nil, err
	}
	n.b.Log.Printf("chat: %s set the flags of %s in #%s to %q", u.Name, t.Name, reg.Name, now)
	if has(now, FlagBanned) && !has(now, FlagExempt) && !t.Sysop && t.ID != reg.FounderID {
		n.b.kick(reg.Name, n.b.ChanServ(), 0, t, "banned by "+Nick(u.Name))
	}
	if now == "" {
		return []string{fmt.Sprintf("%s is off the access list of #%s.", Nick(t.Name), reg.Name)}, nil
	}
	return []string{fmt.Sprintf("%s has %s in #%s.", Nick(t.Name), plusFlags(now), reg.Name)}, nil
}

// csStatus is OP, DEOP, VOICE and DEVOICE.
func csStatus(op, on bool) func(n *Node, u *store.User, args []string) ([]string, error) {
	return func(n *Node, u *store.User, args []string) ([]string, error) {
		if len(args) < 1 {
			return nil, errSyntax
		}
		need := string(FlagVoice)
		if op {
			need = string(FlagOp)
		}
		name := roomArg(args[0])
		who := args[1:]
		if len(who) == 0 {
			who = []string{u.Name}
		}
		self := len(args) == 1
		if !(self && !on) { // anyone may give up their own
			if _, _, err := n.regFor(args[0], u, need); err != nil {
				return nil, err
			}
		} else if n.b.RoomReg(name) == nil {
			return nil, fmt.Errorf("#%s is not registered", name)
		}
		var out []string
		for _, w := range who {
			t, err := n.b.UserByNick(w)
			if err != nil {
				out = append(out, capital(err.Error())+".")
				continue
			}
			if !n.b.inRoom(name, t.ID) {
				out = append(out, Nick(t.Name)+" is not in #"+name+".")
				continue
			}
			st := stVoice
			if op {
				st = stOp
			}
			n.b.give(name, n.b.ChanServ(), 0, t, st, on, false)
		}
		if len(out) == 0 {
			out = append(out, "Done.")
		}
		return out, nil
	}
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func csTopic(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) < 1 {
		return nil, errSyntax
	}
	reg, _, err := n.regFor(args[0], u, string(FlagTopic))
	if err != nil {
		return nil, err
	}
	if err := n.b.SetRoomTopic(reg.Name, strings.Join(args[1:], " "), u.Name); err != nil {
		return nil, err
	}
	return []string{"The topic of #" + reg.Name + " is set."}, nil
}

// victim is one an op would put out: not a sysop (but by a sysop), not the
// founder (but by a sysop or themselves).
func (n *Node) victim(reg *store.RoomReg, u *store.User, nick string) (*store.User, error) {
	t, err := n.b.UserByNick(nick)
	if err != nil {
		return nil, err
	}
	if t.Sysop && !u.Sysop {
		return nil, errors.New("the sysop can't be put out")
	}
	if t.ID == reg.FounderID && !u.Sysop && t.ID != u.ID {
		return nil, errors.New("the founder can't be put out")
	}
	return t, nil
}

func csKick(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) < 2 {
		return nil, errSyntax
	}
	reg, _, err := n.regFor(args[0], u, string(FlagRemove))
	if err != nil {
		return nil, err
	}
	t, err := n.victim(reg, u, args[1])
	if err != nil {
		return nil, err
	}
	reason := strings.Join(args[2:], " ")
	if reason == "" {
		reason = "requested by " + Nick(u.Name)
	} else {
		reason += " (" + Nick(u.Name) + ")"
	}
	if n.b.kick(reg.Name, n.b.ChanServ(), 0, t, reason) == 0 {
		return nil, fmt.Errorf("%s is not in #%s", Nick(t.Name), reg.Name)
	}
	return []string{Nick(t.Name) + " is put out of #" + reg.Name + "."}, nil
}

func csBan(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) < 2 {
		return nil, errSyntax
	}
	reg, mine, err := n.regFor(args[0], u, string(FlagRemove))
	if err != nil {
		return nil, err
	}
	t, err := n.victim(reg, u, args[1])
	if err != nil {
		return nil, err
	}
	if t.ID == reg.FounderID {
		return nil, errors.New("the founder can't be banned")
	}
	flags, err := n.b.Store.RoomFlags(reg.Name, t.ID)
	if err != nil {
		return nil, err
	}
	if has(flags, FlagFounder) && !has(mine, FlagFounder) {
		return nil, errors.New("you need F to ban a founder")
	}
	if err := n.b.Store.SetRoomAccess(reg.Name, t.ID, sortedFlags(flags+"b"), u.Name); err != nil {
		return nil, err
	}
	reason := strings.Join(args[2:], " ")
	if reason == "" {
		reason = "banned by " + Nick(u.Name)
	} else {
		reason += " (" + Nick(u.Name) + ")"
	}
	n.b.kick(reg.Name, n.b.ChanServ(), 0, t, reason)
	n.b.Log.Printf("chat: %s banned %s from #%s", u.Name, t.Name, reg.Name)
	return []string{Nick(t.Name) + " is banned from #" + reg.Name + "."}, nil
}

func csUnban(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) != 2 {
		return nil, errSyntax
	}
	reg, _, err := n.regFor(args[0], u, string(FlagRemove))
	if err != nil {
		return nil, err
	}
	t, err := n.b.UserByNick(args[1])
	if err != nil {
		return nil, err
	}
	flags, err := n.b.Store.RoomFlags(reg.Name, t.ID)
	if err != nil {
		return nil, err
	}
	if !has(flags, FlagBanned) {
		return nil, fmt.Errorf("%s is not banned from #%s", Nick(t.Name), reg.Name)
	}
	if err := n.b.Store.SetRoomAccess(reg.Name, t.ID, strings.ReplaceAll(flags, "b", ""), u.Name); err != nil {
		return nil, err
	}
	return []string{Nick(t.Name) + " may come into #" + reg.Name + " again."}, nil
}

func csClear(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) < 2 || !strings.EqualFold(args[1], "users") {
		return nil, errSyntax
	}
	reg, _, err := n.regFor(args[0], u, string(FlagRecover))
	if err != nil {
		return nil, err
	}
	reason := strings.Join(args[2:], " ")
	if reason == "" {
		reason = "CLEAR USERS by " + Nick(u.Name)
	} else {
		reason += " (" + Nick(u.Name) + ")"
	}
	seen := map[int64]bool{u.ID: true}
	count := 0
	for _, m := range n.b.roomNodes(reg.Name) {
		t := m.User()
		if t == nil || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		if n.b.kick(reg.Name, n.b.ChanServ(), 0, t, reason) > 0 {
			count++
		}
	}
	n.b.Log.Printf("chat: %s cleared #%s (%d put out)", u.Name, reg.Name, count)
	return []string{fmt.Sprintf("#%s is cleared: %d put out.", reg.Name, count)}, nil
}

func csSet(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) < 2 {
		return nil, errSyntax
	}
	reg, _, err := n.regFor(args[0], u, string(FlagSet))
	if err != nil {
		return nil, err
	}
	name := reg.Name
	value := strings.Join(args[2:], " ")
	b := n.b
	switch setting := strings.ToLower(args[1]); setting {
	case "founder":
		if reg.FounderID != u.ID && !u.Sysop {
			return nil, errors.New("only the founder gives the room away")
		}
		t, err := b.UserByNick(value)
		if err != nil {
			return nil, err
		}
		if err := b.Store.SetRoomFounder(name, t.ID); err != nil {
			return nil, err
		}
		flags, _ := b.Store.RoomFlags(name, t.ID)
		if err := b.Store.SetRoomAccess(name, t.ID, sortedFlags(strings.ReplaceAll(flags, "b", "")+founderFlags), u.Name); err != nil {
			return nil, err
		}
		b.updateReg(name, func(r *store.RoomReg) { r.FounderID, r.Founder = t.ID, t.Name })
		b.Log.Printf("chat: %s gave #%s to %s", u.Name, name, t.Name)
		return []string{fmt.Sprintf("#%s is %s's now.", name, Nick(t.Name))}, nil
	case "guard", "topiclock", "private":
		var on bool
		switch strings.ToLower(value) {
		case "on":
			on = true
		case "off":
		default:
			return nil, fmt.Errorf("SET #%s %s ON or OFF", name, strings.ToUpper(setting))
		}
		words := map[string]bool{}
		for _, w := range strings.Fields(reg.Settings) {
			words[w] = true
		}
		words[setting] = on
		var list []string
		for w, ok := range words {
			if ok {
				list = append(list, w)
			}
		}
		sort.Strings(list)
		settings := strings.Join(list, " ")
		if err := b.Store.SetRoomSettings(name, settings); err != nil {
			return nil, err
		}
		h := b.hub()
		h.mu.Lock()
		if r := h.regs[name]; r != nil {
			r.Settings = settings
		}
		if setting == "guard" {
			r := h.rooms[name]
			if on && r == nil {
				h.rooms[name] = &room{members: map[*Node]bool{}}
			}
			if !on && r != nil && len(r.members) == 0 && !h.kept(name, r) {
				delete(h.rooms, name)
			}
		}
		h.mu.Unlock()
		return []string{fmt.Sprintf("%s is %s for #%s.", strings.ToUpper(setting), onOffWord(on), name)}, nil
	case "description", "desc", "url", "entrymsg":
		field := map[string]string{"description": "descr", "desc": "descr", "url": "url", "entrymsg": "entrymsg"}[setting]
		if value == "-" {
			value = ""
		}
		value = Clean(value, MaxTopic)
		if err := b.Store.SetRoomText(name, field, value); err != nil {
			return nil, err
		}
		b.updateReg(name, func(r *store.RoomReg) {
			switch field {
			case "descr":
				r.Descr = value
			case "url":
				r.URL = value
			case "entrymsg":
				r.EntryMsg = value
			}
		})
		if value == "" {
			return []string{fmt.Sprintf("The %s of #%s is cleared.", strings.ToUpper(setting), name)}, nil
		}
		return []string{fmt.Sprintf("The %s of #%s is set.", strings.ToUpper(setting), name)}, nil
	}
	return nil, errors.New("SET's settings are FOUNDER, GUARD, TOPICLOCK, PRIVATE, DESCRIPTION, URL and ENTRYMSG")
}

func csList(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) == 1 && strings.EqualFold(args[0], "all") {
		if !u.Sysop {
			return nil, errors.New("LIST ALL is the sysop's")
		}
		regs, err := n.b.Store.RoomRegs()
		if err != nil {
			return nil, err
		}
		out := []string{"The registered rooms:"}
		for _, r := range regs {
			out = append(out, fmt.Sprintf("  #%-20s %-20s last used %s", r.Name, Nick(r.Founder), when(r.LastUsed)))
		}
		return append(out, fmt.Sprintf("%d rooms.", len(regs))), nil
	}
	if len(args) > 0 {
		return nil, errSyntax
	}
	list, err := n.b.Store.RoomsOf(u.ID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return []string{"You are on no room's access list."}, nil
	}
	out := []string{"Your rooms:"}
	for _, a := range list {
		out = append(out, fmt.Sprintf("  #%-20s +%s", a.Room, a.Flags))
	}
	return out, nil
}

func csWhy(n *Node, u *store.User, args []string) ([]string, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, errSyntax
	}
	reg, mine, err := n.regFor(args[0], u, "")
	if err != nil {
		return nil, err
	}
	t := u
	if len(args) == 2 {
		if t, err = n.b.UserByNick(args[1]); err != nil {
			return nil, err
		}
		if t.ID != u.ID && !has(mine, FlagView) {
			return nil, fmt.Errorf("you need the A flag in #%s for that", reg.Name)
		}
	}
	flags, err := n.b.Store.RoomFlags(reg.Name, t.ID)
	if err != nil {
		return nil, err
	}
	var why []string
	if t.ID == reg.FounderID {
		why = append(why, "the founder")
	}
	if flags != "" {
		why = append(why, "on the access list with +"+flags)
	}
	if t.Sysop {
		why = append(why, "a sysop (every flag)")
	}
	if len(why) == 0 {
		return []string{fmt.Sprintf("%s has no flags in #%s.", Nick(t.Name), reg.Name)}, nil
	}
	return []string{fmt.Sprintf("%s in #%s: %s.", Nick(t.Name), reg.Name, strings.Join(why, "; "))}, nil
}
