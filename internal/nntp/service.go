// Package nntp is the BBS's news: an NNTP server (RFC 3977) for
// newsreaders and peers, and a client that keeps newsgroups in step with
// peer servers (pulling by article number, pushing by IHAVE).  Newsgroups
// are the message areas that have a newsgroup name; an article is a
// message.
package nntp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/news"
	"github.com/D-Modem/bbs/internal/store"
)

// Results of offering an article.
var (
	ErrHave     = errors.New("already have it")
	ErrNotHere  = errors.New("no newsgroup of it is carried here")
	ErrLoop     = errors.New("it has been here before (Path)")
	ErrRejected = errors.New("rejected")
)

// Service takes and gives articles for the server and the peers.
type Service struct {
	B          *bbs.BBS
	MaxArticle int // bytes
}

// Wanted reports whether an article of that Message-ID would be taken (by
// its Message-ID alone: IHAVE's question).
func (s *Service) Wanted(id string) (bool, error) {
	if !news.ValidMessageID(id) {
		return false, nil
	}
	seen, err := s.B.Store.Seen(id)
	return !seen, err
}

// Accept takes an article from a peer (origin: its name), offered as id
// ("" if not said): checked, stored in the first of its newsgroups carried
// here with this site at the head of its Path.  A refused article is
// remembered, so it is not offered again.
func (s *Service) Accept(raw []byte, id, origin string) (*store.Message, error) {
	a, err := news.Parse(raw, s.MaxArticle)
	if err != nil {
		if id != "" {
			s.B.Store.Remember(id)
		}
		return nil, err
	}
	if err := news.Check(a, id); err != nil {
		if id != "" {
			s.B.Store.Remember(id)
		}
		return nil, err
	}
	mid := a.Get("Message-ID")
	if seen, err := s.B.Store.Seen(mid); err != nil {
		return nil, err
	} else if seen {
		return nil, ErrHave
	}
	host := s.B.News.PathHost
	for _, p := range news.Path(a) {
		if strings.EqualFold(p, host) {
			s.B.Store.Remember(mid)
			return nil, ErrLoop
		}
	}
	var area *store.Area
	for _, g := range news.Groups(a.Get("Newsgroups")) {
		if ar, err := s.B.Store.AreaByNewsgroup(g); err == nil {
			area = ar
			break
		}
	}
	if area == nil {
		s.B.Store.Remember(mid)
		return nil, ErrNotHere
	}
	path := a.Get("Path")
	if path == "" {
		path = "not-for-mail"
	}
	a.Set("Path", host+"!"+path)
	m := s.message(a, area)
	m.Origin = origin
	m.Raw = a.Bytes()
	if _, err := s.B.Store.Post(m); err != nil {
		if errors.Is(err, store.ErrExists) {
			return nil, ErrHave
		}
		return nil, err
	}
	return m, nil
}

// message is what the BBS shows of an article.
func (s *Service) message(a *news.Article, area *store.Area) *store.Message {
	m := &store.Message{AreaID: area.ID, From: bbs.Clean(news.Poster(a), 60), To: "All",
		Subject: bbs.Clean(news.Subject(a), bbs.MaxSubjectLen), MessageID: a.Get("Message-ID")}
	if m.Subject == "" {
		m.Subject = "(no subject)"
	}
	if m.From == "" {
		m.From = "(unknown)"
	}
	lines := strings.Split(news.Text(a), "\n")
	body := bbs.CleanBody(lines)
	if strings.Count(body, "\n")+1 < len(lines)-1 || len(body) >= bbs.MaxBodyBytes-200 {
		body += "\n\n[The article goes on: read it whole with a newsreader.]"
	}
	if body == "" {
		body = "(empty)"
	}
	m.Body = body
	// a reply: to the last of its References that is here
	refs := strings.Fields(a.Get("References"))
	for i := len(refs) - 1; i >= 0; i-- {
		if p, err := s.B.Store.MessageByMessageID(refs[i]); err == nil {
			m.ReplyTo = p.ID
			if p.FromID > 0 {
				if u, err := s.B.Store.UserByID(p.FromID); err == nil {
					m.To = u.Name
				}
			} else {
				m.To = p.From
			}
			break
		}
	}
	return m
}

// PostLocal takes an article a newsreader posted, for user u: the poster
// is u, whatever its From says; Message-ID, Date and Path are this site's.
func (s *Service) PostLocal(raw []byte, u *store.User) (*store.Message, error) {
	a, err := news.Parse(raw, s.MaxArticle)
	if err != nil {
		return nil, err
	}
	if a.Get("Control") != "" {
		return nil, fmt.Errorf("%w: control messages are not taken", news.ErrMalformed)
	}
	var area *store.Area
	for _, g := range news.Groups(a.Get("Newsgroups")) {
		if ar, err := s.B.Store.AreaByNewsgroup(g); err == nil {
			area = ar
			break
		}
	}
	if area == nil {
		return nil, ErrNotHere
	}
	if !s.B.CanPost(u, area) {
		return nil, fmt.Errorf("%w: you may not post in %s", ErrRejected, area.Newsgroup)
	}
	m := s.message(a, area)
	if strings.TrimSpace(news.Subject(a)) == "" {
		return nil, fmt.Errorf("%w: no Subject", news.ErrMalformed)
	}
	m.FromID, m.From = u.ID, u.Name
	// made again as ours: the poster, the ids and Path are not the
	// newsreader's to choose; the text and the References (if sound) are
	s.B.MakeArticle(u, area, m)
	if refs := strings.Fields(a.Get("References")); len(refs) > 0 && len(refs) <= 20 {
		ok := true
		for _, r := range refs {
			ok = ok && news.ValidMessageID(r)
		}
		if na, err := news.Parse(m.Raw, len(m.Raw)); ok && err == nil {
			na.Set("References", strings.Join(refs, " "))
			m.Raw = na.Bytes()
		}
	}
	if _, err := s.B.Store.Post(m); err != nil {
		return nil, err
	}
	return m, nil
}
