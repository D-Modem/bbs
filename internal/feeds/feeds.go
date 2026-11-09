package feeds

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/news"
	"github.com/D-Modem/bbs/internal/store"
)

// Source is a feed the BBS reads, and the area its items go to.
type Source struct {
	Name        string        // the [feed NAME], or the catalogue's
	Title       string        // who it is from, on its messages ("": the feed's own title)
	URL         string        // the feed
	Area        string        // the message area its items go to (made if there is none)
	Group       string        // the group the area is listed under, when it is made
	Description string        // the area's description, when it is made
	Every       time.Duration // how often it is read
	KeepDays    int           // how long its items are kept, when the area is made (0: for ever)
}

// Settings are [feeds]' and the [feed NAME]s'.
type Settings struct {
	// UserAgent is how the BBS names itself to the sites: some (the SEC,
	// the National Weather Service) ask for a way to reach the sysop in it
	UserAgent string
	Sources   []*Source
	// MaxItems is the most items of a feed posted at one reading (the
	// first reading's newest, or a feed that has suddenly much)
	MaxItems int
}

// Status is how a feed's reading has gone.
type Status struct {
	Source  *Source
	Last    time.Time // when it was last read (zero: not yet)
	Posted  int       // items posted since the BBS started
	Err     error     // the last reading's failure (nil: it worked)
	Next    time.Time
	etag    string
	lastMod string
}

// Service reads the feeds into their areas.
type Service struct {
	B      *bbs.BBS
	S      Settings
	Client *http.Client // nil: one with a 30 second timeout

	mu     sync.Mutex
	status map[string]*Status
}

// maxFeed is the largest feed read.
const maxFeed = 8 << 20

// Prepare makes the areas the feeds go to that are not there yet:
// read-only (only the sysop posts), listed under their group, kept as
// long as the feed says.
func (s *Service) Prepare() error {
	for _, src := range s.S.Sources {
		if _, err := s.B.Store.AreaByName(src.Area); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		desc := src.Description
		if desc == "" {
			desc = src.Title
		}
		a, err := s.B.Store.CreateArea(src.Area, bbs.Clean(desc, 60), true)
		if err != nil {
			return fmt.Errorf("feed %s: area %s: %w", src.Name, src.Area, err)
		}
		if src.Group != "" {
			if err := s.B.Store.SetAreaGroup(a.ID, src.Group); err != nil {
				return err
			}
		}
		if src.KeepDays > 0 {
			if err := s.B.Store.SetAreaKeep(a.ID, 0, src.KeepDays); err != nil {
				return err
			}
		}
		s.B.Log.Printf("feed %s: made the area %s", src.Name, src.Area)
	}
	return nil
}

// Run reads each feed now (a little apart) and then as often as it asks,
// until ctx is done.
func (s *Service) Run(ctx context.Context) {
	if err := s.Prepare(); err != nil {
		s.B.Log.Printf("feeds: %v", err)
	}
	var wg sync.WaitGroup
	for i, src := range s.S.Sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wait := time.Duration(i) * 3 * time.Second
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
				_, err := s.Read(ctx, src)
				wait = s.next(src, err)
			}
		}()
	}
	wg.Wait()
}

// retryAfter is a site's asking us to come back later.
type retryAfter time.Duration

func (r retryAfter) Error() string {
	return "the site asks us to wait " + time.Duration(r).Round(time.Second).String()
}

// next is how long until src is read again: its interval (a little more
// or less, so the feeds do not all go together), longer after failures.
func (s *Service) next(src *Source, err error) time.Duration {
	d := src.Every
	var ra retryAfter
	switch {
	case errors.As(err, &ra):
		d = max(d, time.Duration(ra))
	case err != nil:
		s.B.Log.Printf("feed %s: %v", src.Name, err)
		d = min(d*2, 6*time.Hour)
	}
	d += time.Duration(rand.Int64N(int64(d/10) + 1))
	st := s.state(src)
	s.mu.Lock()
	st.Next = time.Now().Add(d)
	s.mu.Unlock()
	return d
}

func (s *Service) state(src *Source) *Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == nil {
		s.status = map[string]*Status{}
	}
	st := s.status[src.Name]
	if st == nil {
		st = &Status{Source: src}
		s.status[src.Name] = st
	}
	return st
}

// Statuses are how the feeds have gone, in the order configured.
func (s *Service) Statuses() []Status {
	var out []Status
	for _, src := range s.S.Sources {
		st := s.state(src)
		s.mu.Lock()
		out = append(out, *st)
		s.mu.Unlock()
	}
	return out
}

// Read reads src once and posts what is new; how many were posted.
func (s *Service) Read(ctx context.Context, src *Source) (int, error) {
	st := s.state(src)
	s.mu.Lock()
	etag, lastMod := st.etag, st.lastMod
	s.mu.Unlock()
	f, etag, lastMod, err := s.fetch(ctx, src.URL, etag, lastMod)
	n := 0
	if err == nil && f != nil {
		n, err = s.post(src, f)
	}
	s.mu.Lock()
	st.Last, st.Err = time.Now(), err
	st.Posted += n
	if err == nil {
		st.etag, st.lastMod = etag, lastMod
	}
	s.mu.Unlock()
	return n, err
}

// fetch gets the feed, unless it is as it was (nil, by its ETag or date).
func (s *Service) fetch(ctx context.Context, u, etag, lastMod string) (*Feed, string, string, error) {
	base, err := url.Parse(u)
	if err != nil {
		return nil, "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", s.S.UserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/feed+json, application/xml;q=0.9, text/xml;q=0.9, */*;q=0.5")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastMod != "" {
		req.Header.Set("If-Modified-Since", lastMod)
	}
	cl := s.Client
	if cl == nil {
		cl = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil, etag, lastMod, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable:
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			return nil, "", "", retryAfter(time.Duration(min(secs, 86400)) * time.Second)
		}
		return nil, "", "", retryAfter(time.Hour)
	case resp.StatusCode != http.StatusOK:
		return nil, "", "", fmt.Errorf("%s: %s", u, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFeed+1))
	if err != nil {
		return nil, "", "", err
	}
	if len(data) > maxFeed {
		return nil, "", "", fmt.Errorf("%s: larger than %d MB", u, maxFeed>>20)
	}
	f, err := Parse(data, base)
	if err != nil {
		return nil, "", "", fmt.Errorf("%s: %w", u, err)
	}
	return f, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), nil
}

// MessageID is an item's Message-ID: the same each time it is read, so
// that it is posted once.
func (s *Service) MessageID(src *Source, it *Item) string {
	h := sha1.Sum([]byte(src.URL + "\x00" + it.GUID))
	dom := s.B.News.Domain
	if dom == "" {
		dom = "feeds.invalid"
	}
	return "<feed." + hex.EncodeToString(h[:12]) + "@" + dom + ">"
}

// post posts f's items not posted before, oldest first.
func (s *Service) post(src *Source, f *Feed) (int, error) {
	a, err := s.B.Store.AreaByName(src.Area)
	if err != nil {
		return 0, fmt.Errorf("area %s: %w", src.Area, err)
	}
	type fresh struct {
		it  *Item
		mid string
		pos int
	}
	var todo []fresh
	for i := range f.Items {
		it := &f.Items[i]
		if it.Title == "" && it.Summary == "" {
			continue
		}
		mid := s.MessageID(src, it)
		seen, err := s.B.Store.Seen(mid)
		if err != nil {
			return 0, err
		}
		if !seen {
			todo = append(todo, fresh{it, mid, i})
		}
	}
	// oldest first: by date where items have them, else as the feed has
	// them (newest at the top, mostly)
	sort.SliceStable(todo, func(i, j int) bool {
		a, b := todo[i].it.Published, todo[j].it.Published
		if !a.IsZero() && !b.IsZero() && !a.Equal(b) {
			return a.Before(b)
		}
		return todo[i].pos > todo[j].pos
	})
	maxItems := s.S.MaxItems
	if maxItems <= 0 {
		maxItems = 50
	}
	if len(todo) > maxItems {
		// the newest of them; the rest are noted, not posted
		for _, t := range todo[:len(todo)-maxItems] {
			s.B.Store.Remember(t.mid)
		}
		todo = todo[len(todo)-maxItems:]
	}
	from := src.Title
	if from == "" {
		from = f.Title
	}
	if from == "" {
		from = src.Name
	}
	from = bbs.Clean(from, 60)
	google := strings.Contains(src.URL, "://news.google.com/")
	n := 0
	for _, t := range todo {
		if google {
			// Google News puts the outlet after each title: " - AP News"
			if i := strings.LastIndex(t.it.Title, " - "); i > 0 {
				t.it.Title = t.it.Title[:i]
			}
		}
		m := s.Message(a, from, t.it)
		m.MessageID = t.mid
		m.Origin = "feed:" + src.Name
		if a.Newsgroup != "" && s.B.News.PathHost != "" {
			art := news.New(news.Local{Name: m.From, Address: "feeds@" + s.B.News.Domain, Group: a.Newsgroup,
				Subject: m.Subject, Body: m.Body, MessageID: m.MessageID, PathHost: s.B.News.PathHost,
				Organization: s.B.News.Organization})
			m.Raw = art.Bytes()
		}
		if _, err := s.B.Store.Post(m); err != nil {
			if errors.Is(err, store.ErrExists) {
				continue
			}
			return n, err
		}
		n++
	}
	return n, nil
}

// Message is an item as a message in area a: the summary, a link to the
// whole story, and where and when it is from.
func (s *Service) Message(a *store.Area, from string, it *Item) *store.Message {
	subject := bbs.Clean(it.Title, bbs.MaxSubjectLen)
	if subject == "" {
		subject = bbs.Clean(it.Summary, bbs.MaxSubjectLen)
	}
	var lines []string
	if it.Author != "" && !strings.EqualFold(it.Author, from) {
		lines = append(lines, "By "+it.Author, "")
	}
	if it.Title != "" && len([]rune(it.Title)) > bbs.MaxSubjectLen {
		lines = append(lines, Wrap(it.Title, 76)...)
		lines = append(lines, "")
	}
	summary := it.Summary
	if r := []rune(summary); len(r) > 2000 {
		summary = strings.TrimSpace(string(r[:2000])) + " ..."
	}
	if summary != "" && summary != it.Title {
		lines = append(lines, Wrap(summary, 76)...)
		lines = append(lines, "")
	}
	if it.Link != "" {
		lines = append(lines, "The whole story:", it.Link, "")
	}
	when := ""
	if !it.Published.IsZero() {
		when = ", " + it.Published.UTC().Format("Mon 2 Jan 2006 15:04 UTC")
	}
	lines = append(lines, "-- "+from+when)
	return &store.Message{AreaID: a.ID, From: from, To: "All", Subject: subject, Body: bbs.CleanBody(lines)}
}
