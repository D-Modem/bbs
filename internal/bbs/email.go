package bbs

import (
	"errors"
	"net/mail"
	"strings"

	"github.com/D-Modem/bbs/internal/store"
)

// EmailFunc sends a mail from u to an e-mail address; reply is the mail it
// answers (nil if none).
type EmailFunc func(u *store.User, to, subject, body string, reply *store.Message) error

// IsEmailAddress reports whether a recipient is an e-mail address
// (local@domain.tld, or Name <local@domain.tld> as a mail from one says
// who it is from), not a user's name or a FidoNet address.
func IsEmailAddress(to string) bool {
	to = strings.TrimSpace(to)
	if _, _, ok := SplitNetmail(to); ok {
		return false
	}
	a, err := mail.ParseAddress(to)
	if err != nil {
		return false
	}
	_, domain, ok := strings.Cut(a.Address, "@")
	return ok && strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

// sendEmail sends a mail to an e-mail address.
func (n *Node) sendEmail(u *store.User, to, subject string, body []string, replyTo int64) error {
	if n.b.Email == nil {
		return errors.New("this BBS does not send e-mail")
	}
	subj := Clean(subject, MaxSubjectLen)
	text := CleanBody(body)
	if subj == "" {
		return errors.New("a message needs a subject")
	}
	if text == "" {
		return errors.New("the message is empty")
	}
	var reply *store.Message
	if replyTo > 0 {
		reply, _ = n.b.Store.Mail(u.ID, replyTo)
	}
	if err := n.b.Email(u, to, subj, text, reply); err != nil {
		return err
	}
	n.b.Stats.Mail.Add(1)
	n.b.Log.Printf("node %d: %s sent e-mail to %s", n.Num, u.Name, to)
	return nil
}
