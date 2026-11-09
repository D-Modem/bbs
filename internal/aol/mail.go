package aol

import (
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/D-Modem/bbs/internal/aol/fdo"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// Mail: the BBS's private mail.  The client's own Write Mail form sends
// m4 with a MIP stream: the addressees (mip_addressee), the subject
// (mip_subject) and the text (mip_text, or mip_data in pieces; HTML from
// 4.0), to mip_end_message.  A client may send the header in a stream of
// its own and wait for mip_header_accepted before it sends the text; the
// host answers a whole message with mip_message_accepted.
//
// The mailbox and each mail are the host's own windows (mailForm), whose
// buttons send keywords: "mail", "read mail N", "delete mail N".

// The host's mail windows, by global id.
const (
	mailboxWindow = "50-1-1"
	letterWindow  = "50-1-2"
)

// mailboxMax is the most mail the mailbox lists.
const mailboxMax = 20

// draft is a mail the client has sent the header of.
type draft struct {
	to      []string
	subject string
	text    strings.Builder
}

// sendMail is m4: (part of) a mail from the client's Write Mail form.
func (ss *session) sendMail(atoms []byte) {
	frames, err := fdo.Decode(atoms)
	if err != nil {
		ss.logf("m4: %v", err)
		ss.notice("Your mail could not be read, and was not sent.")
		return
	}
	ss.mu.Lock()
	d := ss.draft
	if d == nil {
		d = &draft{}
		ss.draft = d
	}
	ss.mu.Unlock()
	header, end := false, false
	for _, f := range frames {
		switch f.Name() {
		case "mip_addressee":
			for _, to := range strings.FieldsFunc(fdo.FromLatin1(trimNul(f.Data)), func(r rune) bool { return r == ',' }) {
				if to = strings.TrimSpace(to); to != "" {
					d.to = append(d.to, to)
				}
			}
			header = true
		case "mip_subject":
			d.subject = strings.TrimSpace(fdo.FromLatin1(trimNul(f.Data)))
			header = true
		case "mip_text", "mip_data":
			d.text.WriteString(fdo.FromLatin1(trimNul(f.Data)))
		case "mip_end_message":
			end = true
		}
	}
	if !end {
		if d.text.Len() > 64<<10 {
			ss.mu.Lock()
			ss.draft = nil
			ss.mu.Unlock()
			ss.notice("Your mail is too long, and was not sent.")
			return
		}
		if header {
			ss.send("uni_start_stream\n  mip_header_accepted\n  uni_wait_off\nuni_end_stream\n")
		} else {
			ss.send(waitOff)
		}
		return
	}
	ss.mu.Lock()
	ss.draft = nil
	ss.mu.Unlock()
	ss.deliver(d)
}

// deliver sends a mail the client has sent all of, to each addressee.
func (ss *session) deliver(d *draft) {
	if len(d.to) == 0 {
		ss.notice("Your mail has no one to send it to.")
		return
	}
	subject := d.subject
	if subject == "" {
		subject = "(no subject)"
	}
	body := bbs.WrapBody(mailText(d.text.String()))
	var sent, failed []string
	for _, to := range d.to {
		name := to
		if u := ss.member(to); u != nil {
			name = u.Name
		}
		if err := ss.n.SendMail(name, subject, body, 0); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v.", to, err))
			continue
		}
		sent = append(sent, name)
	}
	accepted := ""
	if len(sent) > 0 {
		accepted = "  mip_message_accepted\n"
	}
	ss.send("uni_start_stream\n" + accepted + "  uni_wait_off\nuni_end_stream\n")
	if len(failed) > 0 {
		msg := "Your mail was not sent to:\r\n" + strings.Join(failed, "\r\n")
		if len(sent) > 0 {
			msg = "Your mail was sent to " + strings.Join(sent, ", ") + ", but not to:\r\n" + strings.Join(failed, "\r\n")
		}
		ss.alert(msg)
	}
}

var (
	htmlBreak = regexp.MustCompile(`(?i)<\s*(br|/p|p|/div)\b[^>]*>`)
	htmlTag   = regexp.MustCompile(`<[^>]*>`)
)

// mailText is a mail's text as the BBS keeps it: lines ended by LF, and
// 4.0's HTML made plain.
func mailText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if strings.Contains(s, "<") && strings.Contains(s, ">") {
		s = htmlBreak.ReplaceAllString(s, "\n")
		s = htmlTag.ReplaceAllString(s, "")
		s = html.UnescapeString(s)
		s = strings.ReplaceAll(s, " ", " ")
	}
	return strings.Trim(s, "\n")
}

// mailbox is the user's mail, newest first, each a button that reads it.
func (ss *session) mailbox() {
	all, err := ss.s.B.Store.Inbox(ss.u.ID)
	if err != nil {
		ss.logf("inbox: %v", err)
		ss.notice("Your mailbox can't be opened just now.")
		return
	}
	unread := 0
	for _, m := range all {
		if !m.Read {
			unread++
		}
	}
	var text string
	switch {
	case len(all) == 0:
		text = "You have no mail."
	case unread == 0:
		text = fmt.Sprintf("You have %s, all read.", count(len(all), "message", "messages"))
	default:
		text = fmt.Sprintf("You have %s, %d new (marked *).", count(len(all), "message", "messages"), unread)
	}
	if len(all) > mailboxMax {
		text += fmt.Sprintf("  The newest %d are here.", mailboxMax)
	}
	text += "\r\n\r\nTo write one, use Compose Mail."
	var buttons []button
	for i := len(all) - 1; i >= 0 && len(buttons) < mailboxMax; i-- {
		m := all[i]
		mark := " "
		if !m.Read {
			mark = "*"
		}
		buttons = append(buttons, button{
			label:   fmt.Sprintf("%s %s  %s: %s", mark, m.Created.Format("Jan 2"), m.From, clip(m.Subject, 40)),
			keyword: fmt.Sprintf("read mail %d", m.ID)})
	}
	ss.send(closeForm(mailboxWindow))
	ss.send(windowForm(mailboxWindow, "Mailbox", text, 4, buttons))
}

// readMail shows a mail of the user's, and marks it read.
func (ss *session) readMail(id int64) {
	m, err := ss.s.B.Store.Mail(ss.u.ID, id)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			ss.logf("mail %d: %v", id, err)
		}
		ss.notice("That mail is not in your mailbox.")
		return
	}
	if !m.Read {
		ss.s.B.Store.MarkMailRead(ss.u.ID, m.ID)
	}
	text := strings.Join([]string{
		"From: " + m.From,
		"To: " + m.To,
		"Date: " + m.Created.Format("Jan 2, 2006 15:04"),
		"Subject: " + m.Subject,
		"",
		strings.ReplaceAll(m.Body, "\n", "\r\n"),
	}, "\r\n")
	ss.send(closeForm(letterWindow))
	ss.send(windowForm(letterWindow, m.Subject, text, 16, []button{
		{"Delete", fmt.Sprintf("delete mail %d", m.ID)},
		{"Mailbox", "mail"},
	}))
}

// deleteMail deletes a mail of the user's, and shows what is left.
func (ss *session) deleteMail(id int64) {
	if err := ss.s.B.Store.DeleteMail(ss.u.ID, id); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			ss.logf("delete mail %d: %v", id, err)
		}
		ss.notice("That mail is not in your mailbox.")
		return
	}
	ss.send(closeForm(letterWindow))
	ss.mailbox()
}

// count is n and a noun, singular or plural.
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// clip is s cut to n characters, with "..." if it was longer.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}
