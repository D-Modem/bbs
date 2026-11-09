package email

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// Errors of a mail refused.
var (
	ErrNotYours   = errors.New("a mail sent here must be from your own address")
	ErrNoInternet = errors.New("this BBS sends no mail elsewhere")
	ErrSysopOnly  = errors.New("your access allows mail only to the sysop")
	ErrNoFidoNet  = errors.New("this BBS is not on FidoNet")
	ErrTooBig     = errors.New("the mail is too big")
)

// shownAddress is a name and address as the terminals show who a mail is
// from, and take it again as the address of a reply.
func shownAddress(name, addr string) string {
	switch {
	case name == "" || strings.EqualFold(name, addr):
		return addr
	case strings.ContainsAny(name, "\"\\,<>@;:()[]."):
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `" <` + addr + `>`
	}
	return name + " <" + addr + ">"
}

// checkRecipient checks that u may send to r.
func (s *Service) checkRecipient(u *store.User, r *recipient) error {
	if !s.mayMail(u) && (r.kind != toLocal || !r.user.Sysop) {
		return ErrSysopOnly
	}
	switch r.kind {
	case toInternet:
		if s.Set.Outbound == OutboundNone {
			return ErrNoInternet
		}
	case toFido:
		if s.B.Netmail == nil {
			return ErrNoFidoNet
		}
	}
	return nil
}

// Submit sends a mail a user here wrote (by SMTP submission, or on a
// terminal) to its recipients: users here, FidoNet netmail, and the queue
// for addresses elsewhere; the user keeps it in Sent.  Its From must be
// the user's own address.
func (s *Service) Submit(u *store.User, rcpts []string, raw []byte) error {
	if int64(len(raw)) > s.Set.MaxMessage {
		return ErrTooBig
	}
	raw = crlf(raw)
	p, err := parse(raw)
	if err != nil {
		return fmt.Errorf("the mail cannot be read: %w", err)
	}
	own := s.Address(u)
	if p.fromAddr == "" {
		raw = append([]byte("From: "+s.Mailbox(u.Name)+"\r\n"), raw...)
	} else if !strings.EqualFold(p.fromAddr, own) {
		return ErrNotYours
	}
	var rs []*recipient
	for _, a := range rcpts {
		r, err := s.resolve(a)
		if err != nil {
			return fmt.Errorf("%s: %w", a, err)
		}
		if err := s.checkRecipient(u, r); err != nil {
			return fmt.Errorf("%s: %w", a, err)
		}
		rs = append(rs, r)
	}
	if len(rs) == 0 {
		return errors.New("a mail for no one")
	}
	// the header made sound: Bcc is not shown, a Date and Message-ID
	raw = withoutFields(raw, "Bcc", "Return-Path", "DKIM-Signature")
	var extra bytes.Buffer
	if p.date.IsZero() {
		fmt.Fprintf(&extra, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	}
	if p.messageID == "" {
		p.messageID = s.newMessageID()
		fmt.Fprintf(&extra, "Message-ID: %s\r\n", p.messageID)
	}
	raw = append(extra.Bytes(), raw...)
	if s.Set.DKIM != nil {
		if signed, err := s.sign(raw); err == nil {
			raw = signed
		} else {
			s.B.Log.Printf("mail: DKIM: %v", err)
		}
	}
	subject := bbs.Clean(p.subject, bbs.MaxSubjectLen)
	if subject == "" {
		subject = "(no subject)"
	}
	text := terminalBody(p.text)
	m := &store.Message{FromID: u.ID, From: u.Name, Subject: subject, Body: text, Raw: raw,
		MessageID: p.messageID, ToAddr: addressList(append(p.to, p.cc...))}
	d := store.Delivery{Sent: true}
	var shown []string
	for _, r := range rs {
		switch r.kind {
		case toLocal:
			d.To = append(d.To, r.user.ID)
			shown = append(shown, r.user.Name)
		case toFido:
			if err := s.B.Netmail(u, bbs.Clean(r.name, bbs.MaxFTNNameLen), r.ftn, subject, text, nil); err != nil {
				return fmt.Errorf("%s: %w", r.addr, err)
			}
			shown = append(shown, r.name+"@"+r.ftn.String())
		case toInternet:
			d.Queue = append(d.Queue, &store.Outbound{UserID: u.ID, Sender: own, Rcpt: r.addr})
			shown = append(shown, r.addr)
		}
	}
	m.To = bbs.Clean(strings.Join(shown, ", "), bbs.MaxMailToLen)
	if _, err := s.B.Store.DeliverMail(m, d); err != nil {
		return err
	}
	for _, id := range d.To {
		s.B.TellMail(id, u.Name, subject)
	}
	if len(d.Queue) > 0 {
		s.Kick()
	}
	s.B.Log.Printf("mail: %s sent %q to %s", u.Name, subject, strings.Join(shown, ", "))
	return nil
}

// fromTerminal sends a mail written on a terminal to an e-mail address
// (bbs.EmailFunc).
func (s *Service) fromTerminal(u *store.User, to, subject, body string, reply *store.Message) error {
	a, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("%q is not an e-mail address", to)
	}
	var b bytes.Buffer
	field := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	field("From", s.Mailbox(u.Name))
	field("To", a.String())
	field("Subject", mime.QEncoding.Encode("utf-8", subject))
	field("Date", time.Now().Format(time.RFC1123Z))
	field("Message-ID", s.newMessageID())
	if reply != nil {
		id := s.messageID(reply)
		field("In-Reply-To", id)
		field("References", id)
	}
	writeTextBody(&b, body)
	return s.Submit(u, []string{a.Address}, b.Bytes())
}

// Receive keeps a mail from elsewhere for users here: trace is the header
// fields put before it (Received, Authentication-Results).
func (s *Service) Receive(to []*store.User, raw []byte, trace string) error {
	if int64(len(raw)) > s.Set.MaxMessage {
		return ErrTooBig
	}
	raw = append([]byte(trace), crlf(raw)...)
	p, err := parse(raw)
	if err != nil {
		return fmt.Errorf("the mail cannot be read: %w", err)
	}
	subject := bbs.Clean(p.subject, bbs.MaxSubjectLen)
	if subject == "" {
		subject = "(no subject)"
	}
	from := bbs.Clean(shownAddress(p.fromName, p.fromAddr), 200)
	if from == "" {
		from = "(unknown)"
	}
	var ids []int64
	var names []string
	for _, u := range to {
		ids = append(ids, u.ID)
		names = append(names, u.Name)
	}
	created := time.Now()
	m := &store.Message{From: from, FromAddr: p.fromAddr, To: bbs.Clean(strings.Join(names, ", "), bbs.MaxMailToLen),
		Subject: subject, Body: terminalBody(p.text), Raw: raw, MessageID: p.messageID,
		ToAddr: addressList(append(p.to, p.cc...)), Created: created}
	if len(ids) > 0 {
		m.ToID = ids[0]
	}
	if _, err := s.B.Store.DeliverMail(m, store.Delivery{To: ids}); err != nil {
		return err
	}
	for _, id := range ids {
		s.B.TellMail(id, from, subject)
	}
	s.B.Log.Printf("mail: from %s for %s: %q", p.fromAddr, strings.Join(names, ", "), subject)
	return nil
}

// bounce tells a mail's sender here it could not be sent.
func (s *Service) bounce(o *store.Outbound, why string) {
	if o.UserID <= 0 {
		s.B.Log.Printf("mail: %d for %s given up (%s): no one here to tell", o.MailID, o.Rcpt, why)
		return
	}
	u, err := s.B.Store.UserByID(o.UserID)
	if err != nil {
		return
	}
	subject := "(no subject)"
	var head string
	if raw, err := s.B.Store.MailRaw(o.MailID); err == nil {
		if p, err := parse(raw); err == nil && p.subject != "" {
			subject = p.subject
		}
		if end := headerEnd(raw); end > 0 {
			head = string(raw[:end])
		}
	}
	body := fmt.Sprintf("Your mail could not be delivered to %s.\n\n%s\n\nIts header was:\n\n%s",
		o.Rcpt, why, strings.ReplaceAll(head, "\r\n", "\n"))
	m := &store.Message{From: "Mail Delivery System", FromAddr: "mailer-daemon@" + s.Set.Domain, ToID: u.ID, To: u.Name,
		Subject: bbs.Clean("Undelivered: "+subject, bbs.MaxSubjectLen), Body: terminalBody(body)}
	if _, err := s.B.Store.SendMail(m); err != nil {
		s.B.Log.Printf("mail: bounce for %s: %v", u.Name, err)
		return
	}
	s.B.TellMail(u.ID, m.From, m.Subject)
	s.B.Log.Printf("mail: %d for %s given up (%s): %s told", o.MailID, o.Rcpt, why, u.Name)
}
