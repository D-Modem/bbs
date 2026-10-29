package bbs

import (
	"strings"

	"github.com/D-Modem/bbs/internal/news"
	"github.com/D-Modem/bbs/internal/store"
)

// NewsSettings are what articles made here say of the system.
type NewsSettings struct {
	PathHost     string // this site in Path headers
	Domain       string // of Message-IDs and posters' addresses
	Organization string
}

// localPart is a user name as an address's local part: what is not a
// letter, digit or one of ._- becomes a dot.
func localPart(name string) string {
	out := strings.Map(func(r rune) rune {
		if r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
			return r
		}
		return '.'
	}, name)
	out = strings.Trim(out, ".")
	for strings.Contains(out, "..") {
		out = strings.ReplaceAll(out, "..", ".")
	}
	if out == "" {
		out = "user"
	}
	return strings.ToLower(out)
}

// MakeArticle makes m (by u, in newsgroup area a) an article: its
// Message-ID and its raw form, with References to what it answers.
func (b *BBS) MakeArticle(u *store.User, a *store.Area, m *store.Message) {
	ns := b.News
	if a.Newsgroup == "" || ns.PathHost == "" {
		return
	}
	var refs []string
	if m.ReplyTo > 0 {
		if parent, err := b.Store.Message(m.ReplyTo); err == nil && parent.MessageID != "" {
			if raw, err := b.Store.Raw(parent.ID); err == nil {
				if pa, err := news.Parse(raw, len(raw)); err == nil {
					refs = strings.Fields(pa.Get("References"))
				}
			}
			refs = append(refs, parent.MessageID)
			if len(refs) > 10 {
				refs = append(refs[:1], refs[len(refs)-9:]...)
			}
		}
	}
	m.MessageID = news.NewMessageID(ns.Domain)
	art := news.New(news.Local{Name: m.From, Address: localPart(m.From) + "@" + ns.Domain,
		Group: a.Newsgroup, Subject: m.Subject, Body: m.Body, References: refs,
		MessageID: m.MessageID, PathHost: ns.PathHost, Organization: ns.Organization})
	m.Raw = art.Bytes()
}
