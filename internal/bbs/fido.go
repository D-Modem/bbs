package bbs

import (
	"errors"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// MaxMailToLen is the longest a mail's recipient is typed: a user, or a
// FidoNet name and address.
const MaxMailToLen = 60

// MaxFTNNameLen is the longest name a FidoNet message carries.
const MaxFTNNameLen = 35

// NetmailFunc sends a FidoNet netmail from u; reply is the mail it
// answers (nil if none).
type NetmailFunc func(u *store.User, to string, addr ftn.Addr, subject, body string, reply *store.Message) error

// SplitNetmail reads a netmail recipient: "Name@zone:net/node[.point]"
// (spaces around the @ allowed).
func SplitNetmail(to string) (string, ftn.Addr, bool) {
	i := strings.LastIndex(to, "@")
	if i <= 0 {
		return "", ftn.Addr{}, false
	}
	a, err := ftn.ParseAddr(to[i+1:])
	if err != nil {
		return "", ftn.Addr{}, false
	}
	name := Clean(strings.TrimSpace(to[:i]), MaxFTNNameLen)
	if name == "" {
		return "", ftn.Addr{}, false
	}
	return name, a, true
}

// sendNetmail sends a mail to a FidoNet address.
func (n *Node) sendNetmail(u *store.User, name string, addr ftn.Addr, subject string, body []string, replyTo int64) error {
	if n.b.Netmail == nil {
		return errors.New("this BBS is not on FidoNet")
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
	if err := n.b.Netmail(u, name, addr, subj, text, reply); err != nil {
		return err
	}
	n.b.Log.Printf("node %d: %s sent netmail to %s at %s", n.Num, u.Name, name, addr)
	return nil
}
