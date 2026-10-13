package fido

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/news"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Kinds of subscription: echomail areas (AreaFix), file echoes (FileFix).
const (
	Echoes    = "echo"
	FileEchos = "file"
)

// allows reports whether a link may have a tag at all: its echoes (or
// fileechoes) wildmat.
func (l *Link) allows(kind, tag string) bool {
	pat := l.Echoes
	if kind == FileEchos {
		pat = l.FileEchoes
	}
	return pat != "" && news.Wildmat(strings.ToUpper(pat), strings.ToUpper(tag))
}

// carries reports whether a link exchanges an echo (or a file echo):
// one it may have and, if it manages its own by AreaFix, has asked for.
func (f *Fido) carries(l *Link, kind, tag string) bool {
	if !l.allows(kind, tag) {
		return false
	}
	if !l.AreaFix {
		return true
	}
	ok, err := f.B.Store.FidoSubscribed(l.Name, kind, strings.ToUpper(tag))
	return err == nil && ok
}

// fixKind is the kind a robot's name manages ("": not a robot's name).
func fixKind(to string) string {
	switch strings.ToLower(strings.TrimSpace(to)) {
	case "areafix", "areamgr", "arealink", "areafix!":
		return Echoes
	case "filefix", "filemgr", "allfix", "raid", "ticfix":
		return FileEchos
	}
	return ""
}

// available is what a link may subscribe to of a kind: the echoes (or
// file echoes) here that it is allowed, by tag.
func (f *Fido) available(l *Link, kind string) ([]string, error) {
	var tags []string
	if kind == Echoes {
		as, err := f.B.Store.EchoAreas()
		if err != nil {
			return nil, err
		}
		for _, a := range as {
			if l.allows(kind, a.EchoTag) {
				tags = append(tags, strings.ToUpper(a.EchoTag))
			}
		}
	} else {
		fas, err := f.B.Store.FileEchoAreas()
		if err != nil {
			return nil, err
		}
		for _, a := range fas {
			if l.allows(kind, a.FileEcho) {
				tags = append(tags, strings.ToUpper(a.FileEcho))
			}
		}
	}
	sort.Strings(tags)
	return tags, nil
}

// Subscribe subscribes a link to the tags (wildmats too) it may have of
// a kind, or unsubscribes it; it says what it did, a line each.
func (f *Fido) Subscribe(l *Link, kind string, on bool, patterns []string) ([]string, error) {
	avail, err := f.available(l, kind)
	if err != nil {
		return nil, err
	}
	subs, err := f.B.Store.FidoSubs(l.Name, kind)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, s := range subs {
		have[s] = true
	}
	var out []string
	for _, p := range patterns {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if p == "ALL" {
			p = "*"
		}
		matched := 0
		// what may be had; and when unsubscribing, what is had (an area
		// gone, or no longer allowed, can still be dropped)
		cands := avail
		if !on {
			cands = append(append([]string{}, avail...), subs...)
		}
		done := map[string]bool{}
		for _, tag := range cands {
			if done[tag] || !news.Wildmat(p, tag) {
				continue
			}
			done[tag] = true
			matched++
			switch {
			case on && have[tag]:
				out = append(out, tag+": already linked")
			case on:
				if err := f.B.Store.SetFidoSub(l.Name, kind, tag, true); err != nil {
					return out, err
				}
				have[tag] = true
				out = append(out, tag+": linked")
			case have[tag]:
				if err := f.B.Store.SetFidoSub(l.Name, kind, tag, false); err != nil {
					return out, err
				}
				delete(have, tag)
				out = append(out, tag+": unlinked")
			default:
				if p == tag {
					out = append(out, tag+": was not linked")
				}
			}
		}
		if matched == 0 {
			out = append(out, p+": no such "+kindWord(kind)+" here")
		}
	}
	return out, nil
}

func kindWord(kind string) string {
	if kind == FileEchos {
		return "file echo"
	}
	return "echo"
}

// robotName is the robot's name of a kind, as replies are signed.
func robotName(kind string) string {
	if kind == FileEchos {
		return "FileFix"
	}
	return "AreaFix"
}

// fixRequest answers a request to AreaFix or FileFix from a link's
// sysop: its commands, a line each, done; the answer goes back by
// netmail.  Requests from systems that are not links are dropped.
func (f *Fido) fixRequest(orig ftn.Addr, from, robot, subject string, lines []string, msgid string) error {
	kind := fixKind(robot)
	l := f.linkByAddr(orig)
	if l == nil {
		f.B.Log.Printf("fido: %s request from %s, not a link: dropped", robotName(kind), orig)
		return nil
	}
	reply := func(body []string) error {
		n := &store.Netmail{From: robotName(kind), To: bbs.Clean(from, bbs.MaxFTNNameLen),
			ToAddr: orig.String4D(), Subject: robotName(kind) + " reply", Body: strings.Join(body, "\n"),
			MsgID: ftn.NewMSGID(f.Main()), Reply: msgid}
		if n.To == "" {
			n.To = "Sysop"
		}
		if _, err := f.B.Store.QueueNetmail(n); err != nil {
			return err
		}
		f.Kick()
		return nil
	}
	if !l.AreaFix {
		f.B.Log.Printf("fido: %s request from %s: not open to it", robotName(kind), l.Name)
		return reply([]string{robotName(kind) + " is not open to " + l.Addr.String() + ": ask the sysop."})
	}
	password, _, _ := strings.Cut(strings.TrimSpace(subject), " ")
	if l.AreafixPassword == "" || !strings.EqualFold(password, l.AreafixPassword) {
		f.B.Log.Printf("fido: %s request from %s: wrong password", robotName(kind), l.Name)
		return reply([]string{"Wrong password: nothing done."})
	}
	out := []string{fmt.Sprintf("%s, %s here: your request for %s.", robotName(kind), f.Main(), l.Addr), ""}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == 1 {
			continue
		}
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "* Origin:") {
			break
		}
		cmd := strings.ToUpper(line)
		switch {
		case cmd == "%HELP":
			out = append(out, fixHelp(kind)...)
		case cmd == "%LIST", cmd == "%QUERY", cmd == "%LINKED", cmd == "%UNLINKED":
			avail, err := f.available(l, kind)
			if err != nil {
				return err
			}
			subs, err := f.B.Store.FidoSubs(l.Name, kind)
			if err != nil {
				return err
			}
			have := map[string]bool{}
			for _, s := range subs {
				have[s] = true
			}
			title := map[string]string{"%LIST": "Available (* linked):", "%QUERY": "Linked:",
				"%LINKED": "Linked:", "%UNLINKED": "Not linked:"}[cmd]
			out = append(out, title)
			n := 0
			for _, tag := range avail {
				switch {
				case cmd == "%LIST" && have[tag]:
					out = append(out, "* "+tag)
				case cmd == "%LIST", cmd == "%UNLINKED" && !have[tag]:
					out = append(out, "  "+tag)
				case (cmd == "%QUERY" || cmd == "%LINKED") && have[tag]:
					out = append(out, "  "+tag)
				default:
					continue
				}
				n++
			}
			out = append(out, fmt.Sprintf("(%d)", n), "")
		case strings.HasPrefix(cmd, "%"):
			out = append(out, line+": not done here")
		case strings.HasPrefix(cmd, "-"):
			r, err := f.Subscribe(l, kind, false, []string{cmd[1:]})
			if err != nil {
				return err
			}
			out = append(out, r...)
		default:
			r, err := f.Subscribe(l, kind, true, []string{strings.TrimLeft(cmd, "+=")})
			if err != nil {
				return err
			}
			out = append(out, r...)
		}
	}
	f.B.Log.Printf("fido: %s request from %s done", robotName(kind), l.Name)
	return reply(out)
}

func fixHelp(kind string) []string {
	w := kindWord(kind)
	return []string{
		"Write to " + robotName(kind) + " with your password as the subject, and a",
		"command to a line:",
		"  +TAG or TAG   link the " + w + " (a pattern too: +FSX_*)",
		"  -TAG          unlink it; -ALL unlinks every one",
		"  %LIST         the " + w + "s you may have, * where linked",
		"  %QUERY        the ones linked",
		"  %UNLINKED     the ones not linked",
		"  %HELP         this",
		"",
	}
}

// FixRequest sends a request to a link's AreaFix (or FileFix): our
// commands, with the password we share.
func (f *Fido) FixRequest(l *Link, kind string, commands []string) error {
	if l.AreafixPassword == "" {
		return errors.New("[link " + l.Name + "] has no areafix_password")
	}
	n := &store.Netmail{From: bbs.Clean(f.S.Sysop, bbs.MaxFTNNameLen), To: robotName(kind), ToAddr: l.Addr.String4D(),
		Subject: l.AreafixPassword, Body: strings.Join(commands, "\n"), MsgID: ftn.NewMSGID(f.Main())}
	if n.From == "" {
		n.From = "Sysop"
	}
	if _, err := f.B.Store.QueueNetmail(n); err != nil {
		return err
	}
	f.Kick()
	return nil
}

// Carried are the tags of a kind a link has: here, allowed, and (if it
// manages its own) linked.
func (f *Fido) Carried(l *Link, kind string) ([]string, error) {
	avail, err := f.available(l, kind)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, t := range avail {
		if f.carries(l, kind, t) {
			out = append(out, t)
		}
	}
	return out, nil
}
