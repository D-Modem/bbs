package ymsg

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/D-Modem/bbs/internal/bbs"
	"golang.org/x/text/encoding/charmap"
)

// event is something the node is told: a private message as an IM from
// its sender, a room's line to the chat room or conference it is, anything
// else as an IM from the BBS.
func (ss *session) event(e bbs.Event) {
	switch e.Kind {
	case bbs.Private:
		text := e.Text
		if e.Kept {
			text = "(" + e.Time.Format("Jan 2 15:04") + ", while you were away) " + text
		}
		if u, err := ss.s.B.Store.UserByName(e.From); err == nil {
			if ss.s.B.Blocks(ss.u, u) {
				return
			}
			ss.im(idOf(u), text, e.Time)
			return
		}
		ss.im(ss.s.systemID(), e.From+": "+text, e.Time)
		return
	case bbs.Said, bbs.Emoted, bbs.Joined, bbs.Left, bbs.Topic:
		ss.roomEvent(e, false)
		return
	case bbs.Notice:
		if e.Room != "" && ss.roomEvent(e, false) {
			return
		}
	case bbs.Paged:
		text := fmt.Sprintf("%s (node %d) is paging you", e.From, e.Node)
		if e.Text != "" {
			text += ": " + e.Text
		}
		ss.system(text + fmt.Sprintf(" (join the chat room sysop-%d to answer)", e.Node))
		return
	}
	ss.system(e.String())
}

// deliverKept gives the client the private messages kept for its user
// while they were away.
func (ss *session) deliverKept() {
	ss.mu.Lock()
	sent := ss.keptSent
	ss.keptSent = true
	ss.mu.Unlock()
	if sent {
		return
	}
	for _, e := range ss.n.Kept() {
		ss.event(e)
	}
}

// utf8OK: the client takes UTF-8 (key 97), Y!M 6 and later.
func (ss *session) utf8OK() bool { return ss.ver >= 11 }

// toClient is text for the client: UTF-8 if it takes it, else Windows-1252.
func (ss *session) toClient(text string) string {
	if ss.utf8OK() {
		return text
	}
	out, err := charmap.Windows1252.NewEncoder().String(strings.Map(func(r rune) rune {
		if _, ok := charmap.Windows1252.EncodeRune(r); ok {
			return r
		}
		return '?'
	}, text))
	if err != nil {
		return text
	}
	return out
}

// im sends the client an IM.
func (ss *session) im(from, text string, at time.Time) {
	p := &packet{service: svcMessage, status: stServerAck}
	p.add(5, ss.id).add(4, from).add(14, ss.toClient(text))
	if !at.IsZero() {
		p.addInt(15, at.Unix())
	}
	if ss.utf8OK() {
		p.add(97, "1")
	}
	ss.k.write(p)
}

var (
	ansiCode = regexp.MustCompile(`\x1b\[[^m]{0,16}m`)
	tags     = regexp.MustCompile(`(?i)</?(font|fade|alt|b|i|u|s)(\s[^>]*)?>`)
)

// fromYahoo is a line from a client as plain text: UTF-8 (utf8 says it
// is; else it is Windows-1252 unless it reads as UTF-8), without its
// colour codes and font tags.
func fromYahoo(s string, isUTF8 bool) string {
	if !isUTF8 && !utf8.ValidString(s) {
		if d, err := charmap.Windows1252.NewDecoder().String(s); err == nil {
			s = d
		}
	}
	s = ansiCode.ReplaceAllString(s, "")
	s = tags.ReplaceAllString(s, "")
	return s
}

// message is MESSAGE: an IM to a BBS user.
func (ss *session) message(p *packet) {
	to := p.get(5)
	text := bbs.Clean(fromYahoo(p.get(14), p.get(97) == "1"), bbs.MaxChatLine*4)
	if to == "" || text == "" {
		return
	}
	t := ss.s.resolve(to)
	if t == nil {
		ss.system(to + " is not a user here.")
		return
	}
	if t.ID == ss.u.ID {
		return
	}
	for _, line := range splitLines(text) {
		if _, err := ss.n.TellUser(t, line); err != nil {
			ss.system("Your message to " + idOf(t) + " was not sent: " + err.Error())
			return
		}
	}
}

// splitLines cuts text into lines the chat takes (at most MaxChatLine
// runes each).
func splitLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		r := []rune(strings.TrimSpace(l))
		for len(r) > bbs.MaxChatLine {
			cut := bbs.MaxChatLine
			for i := cut; i > cut/2; i-- {
				if r[i] == ' ' {
					cut = i
					break
				}
			}
			out = append(out, string(r[:cut]))
			r = []rune(strings.TrimSpace(string(r[cut:])))
		}
		if len(r) > 0 {
			out = append(out, string(r))
		}
	}
	return out
}

// notify is NOTIFY: typing (only Yahoo clients are told).
func (ss *session) notify(p *packet) {
	if !strings.EqualFold(p.get(49), "TYPING") {
		return
	}
	t := ss.s.resolve(p.get(5))
	if t == nil || ss.s.B.Blocks(t, ss.u) {
		return
	}
	on := p.get(13)
	if on != "1" {
		on = "0"
	}
	for _, x := range ss.s.sessionsOf(t.ID) {
		x.do(func() {
			if x.isOnline() {
				x.k.send(svcNotify, stNotify, kv(5, x.id), kv(4, ss.id), kv(14, " "), kv(13, on), kv(49, "TYPING"))
			}
		})
	}
}
