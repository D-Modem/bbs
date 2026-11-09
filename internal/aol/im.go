package aol

import (
	"fmt"
	"strings"

	"github.com/D-Modem/bbs/internal/aol/fdo"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// Instant Messages: the BBS's private messages, to and from users on any
// route (terminals, IRC, AIM, ICQ, MSN, AOL).  The client sends iS (4.0:
// is) from its Send Instant Message window with the name and the message
// as de_data, and iO to ask whether someone is on; an IM to it opens the
// client's own Instant Message window (32-223, 4.0's 32-6736), one for
// each partner.

// sendIM is iS: an IM to a BBS user.
func (ss *session) sendIM(atoms []byte) {
	f := texts(atoms)
	if len(f) < 2 {
		ss.logf("an IM without a name and a message: % x", atoms)
		ss.send(waitOff)
		return
	}
	to, text := strings.TrimSpace(f[0]), strings.TrimSpace(f[len(f)-1])
	u := ss.member(to)
	if u == nil {
		ss.alert(fmt.Sprintf("%s is not a member here.", to))
		return
	}
	online, err := ss.n.TellUser(u, plain(text))
	if err != nil {
		ss.alert(fmt.Sprintf("Your message to %s was not sent: %v.", u.Name, err))
		return
	}
	if !online {
		ss.alert(fmt.Sprintf("%s is not signed on now, and will have your message when they next sign on.", u.Name))
	}
	// the sender's own line, in their window with the partner, as AOL did
	ss.send(ss.imForm(u.Name, ss.u.Name, text))
}

// available is iO: is someone on?
func (ss *session) available(atoms []byte) {
	f := texts(atoms)
	who := ""
	if len(f) > 0 {
		who = strings.TrimSpace(f[0])
	}
	u := ss.member(who)
	switch {
	case u == nil:
		ss.alert(fmt.Sprintf("%s is not a member here.", who))
	case ss.online(u):
		ss.alert(fmt.Sprintf("%s is online and able to receive Instant Messages.", u.Name))
	default:
		ss.alert(fmt.Sprintf("%s is not currently signed on.", u.Name))
	}
}

// member is the BBS user named, or nil: AOL names are compared without
// their spaces, so "CarolAnn" finds "Carol Ann".
func (ss *session) member(name string) *store.User {
	if name == "" {
		return nil
	}
	u, err := ss.s.B.Store.UserByScreenName(name)
	if err != nil {
		return nil
	}
	return u
}

// online is whether u is on any node.
func (ss *session) online(u *store.User) bool {
	for _, n := range ss.s.B.Nodes() {
		if strings.EqualFold(n.User, u.Name) {
			return true
		}
	}
	return false
}

// texts are the text fields a form sent, its de_data in order; or, sent
// as plain text, its lines.
func texts(atoms []byte) []string {
	if frames, err := fdo.Decode(atoms); err == nil {
		if de := fdo.FindAll(frames, "de_data"); len(de) > 0 {
			out := make([]string, len(de))
			for i, d := range de {
				out[i] = fdo.FromLatin1(trimNul(d.Data))
			}
			return out
		}
	}
	var out []string
	for _, p := range strings.FieldsFunc(fdo.FromLatin1(atoms), func(r rune) bool { return r == 0 || r == '\r' }) {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// plain is an IM's text without 4.0's HTML.
func plain(s string) string {
	var sb strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>' && in:
			in = false
		case !in:
			sb.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// privateEvent shows a private message as an IM.
func (ss *session) privateEvent(e bbs.Event) {
	text := e.Text
	if e.Kept {
		text = fmt.Sprintf("(%s, while you were away) %s", e.Time.Format("Jan 2 15:04"), text)
	}
	ss.send(ss.imForm(e.From, e.From, text))
}

// imWindow is the global id of the IM window for a partner, one each.
// ss.mu is held.
func (ss *session) imWindow(partner string) string {
	k := strings.ToLower(partner)
	n, ok := ss.ims[k]
	if !ok {
		n = len(ss.ims) + 1
		ss.ims[k] = n
	}
	return fmt.Sprintf("50-0-%d", n)
}

// imForm is a line from sender in the IM window with partner: the
// client's own window, opened the first time, appended to after.
func (ss *session) imForm(partner, sender, text string) string {
	ss.mu.Lock()
	gid := ss.imWindow(partner)
	ss.mu.Unlock()
	if ss.l.NoCRC() {
		return fmt.Sprintf(im40Form, gid, fdo.Quote("Instant Message From: "+partner), gid, gid,
			fdo.Quote(sender), fdo.Quote(text), fdo.Quote(partner))
	}
	return fmt.Sprintf(im30Form, gid, gid, fdo.Quote("Instant Message From: "+partner), fdo.Quote(partner),
		gid, fdo.Quote(sender+":  "+text+"\r"))
}

// The IM windows: if one for the partner is open, append to it; if not,
// open the client's own and name it.
const im30Form = `uni_start_stream
  man_preset_gid <%s>
  if_last_return_true_then <1, 2>
    man_end_context
  uni_sync_skip <1>
    uni_invoke_local <32-223>
    man_set_context_globalid <0-223>
    mat_object_id <%s>
    mat_title <%s>
    man_set_context_relative <1>
    man_append_data <%s>
    man_end_context
    man_end_context
  uni_sync_skip <2>
  man_set_context_globalid <%s>
  man_set_context_relative <3>
  man_append_data <%s>
  man_end_context
  man_end_context
  man_update_display
  uni_wait_off
uni_end_stream
`

const im40Form = `uni_start_stream
  man_preset_gid <%s>
  if_last_return_true_then <1, 2>
    man_end_context
  uni_sync_skip <1>
    man_preset_title <%s>
    man_preset_gid <%s>
    uni_invoke_local <32-6736>
  uni_sync_skip <2>
  man_set_context_globalid <%s>
  man_set_context_relative <1>
  man_replace_data <%s>
  man_end_context
  man_set_context_relative <7>
  man_replace_data <" ">
  man_clear_object
  man_append_data <%s>
  man_end_data
  man_end_context
  act_do_action <351>
  man_set_context_relative <1>
  man_replace_data <%s>
  man_end_context
  man_end_context
  man_update_display
  uni_wait_off
uni_end_stream
`
