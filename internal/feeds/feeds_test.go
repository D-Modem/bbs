package feeds

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

const rss2 = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:atom="http://www.w3.org/2005/Atom">
<channel><title>Example News</title><link>https://example.org/</link>
<atom:link rel="self" href="https://example.org/rss"/>
<item><title>Second story</title><link>/news/2</link><guid isPermaLink="false">id-2</guid>
<description><![CDATA[<p>A <b>bold</b> start.</p><p>Then more &amp; more.</p><script>bad()</script>]]></description>
<pubDate>Mon, 09 Nov 2026 08:00:00 GMT</pubDate><dc:creator>A. Writer</dc:creator></item>
<item><title>First story</title><guid>https://example.org/news/1</guid>
<description>Plain &lt;i&gt;escaped&lt;/i&gt; text&nbsp;here</description>
<pubDate>Sun, 8 Nov 2026 20:15:00 EST</pubDate></item>
</channel></rss>`

const rdf = `<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/" xmlns:dc="http://purl.org/dc/elements/1.1/">
<channel rdf:about="https://example.de/"><title>Welle</title><link>https://example.de/</link></channel>
<item rdf:about="https://example.de/a-1"><title>Eins</title><link>https://example.de/a-1</link>
<description>Erste.</description><dc:date>2026-11-09T07:30:00Z</dc:date></item>
</rdf:RDF>`

const atom = `<?xml version="1.0" encoding="ISO-8859-1"?>
<feed xmlns="http://www.w3.org/2005/Atom"><title>Filings</title>
<link rel="self" href="/feed"/><link rel="alternate" href="/"/>
<entry><title>8-K - Caf` + "\xe9" + ` Inc.</title><link rel="alternate" type="text/html" href="/Archives/1.htm"/>
<id>urn:tag:x,2008:1</id><updated>2026-11-06T17:30:10-05:00</updated>
<summary type="html">&lt;b&gt;Filed:&lt;/b&gt; 2026-11-06</summary><author><name>EDGAR</name></author></entry>
</feed>`

const jsonFeed = `{"version":"https://jsonfeed.org/version/1.1","title":"JSON News","home_page_url":"https://j.example/",
"items":[{"id":42,"url":"https://j.example/42","title":"Numbered","content_text":"Body text.","date_published":"2026-11-01T10:00:00Z"}]}`

func TestParse(t *testing.T) {
	base, _ := url.Parse("https://example.org/rss")
	f, err := Parse([]byte(rss2), base)
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Example News" || len(f.Items) != 2 {
		t.Fatalf("feed %+v", f)
	}
	a, b := f.Items[0], f.Items[1]
	if a.Link != "https://example.org/news/2" || a.GUID != "id-2" || a.Author != "A. Writer" {
		t.Errorf("first item %+v", a)
	}
	if a.Summary != "A bold start.\n\nThen more & more." {
		t.Errorf("summary %q", a.Summary)
	}
	if !a.Published.Equal(time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("date %v", a.Published)
	}
	// a permalink guid is the link; EST is five hours behind
	if b.Link != "https://example.org/news/1" || b.Summary != "Plain escaped text here" {
		t.Errorf("second item %+v", b)
	}
	if !b.Published.Equal(time.Date(2026, 11, 9, 1, 15, 0, 0, time.UTC)) {
		t.Errorf("EST date %v", b.Published)
	}

	f, err = Parse([]byte(rdf), nil)
	if err != nil || len(f.Items) != 1 || f.Items[0].Title != "Eins" || f.Items[0].GUID != "https://example.de/a-1" ||
		f.Items[0].Published.IsZero() {
		t.Errorf("RSS 1.0: %+v %v", f, err)
	}

	base, _ = url.Parse("https://www.sec.gov/cgi-bin/browse-edgar")
	f, err = Parse([]byte(atom), base)
	if err != nil || len(f.Items) != 1 {
		t.Fatalf("Atom: %+v %v", f, err)
	}
	e := f.Items[0]
	if e.Title != "8-K - Café Inc." || e.Link != "https://www.sec.gov/Archives/1.htm" || e.Summary != "Filed: 2026-11-06" ||
		e.Author != "EDGAR" || e.GUID != "urn:tag:x,2008:1" || f.Link != "https://www.sec.gov/" {
		t.Errorf("Atom entry %+v (feed link %s)", e, f.Link)
	}

	f, err = Parse([]byte(jsonFeed), nil)
	if err != nil || len(f.Items) != 1 || f.Items[0].GUID != "42" || f.Items[0].Summary != "Body text." {
		t.Errorf("JSON Feed: %+v %v", f, err)
	}

	if _, err := Parse([]byte("<html><body>no</body></html>"), nil); err != ErrNotFeed {
		t.Errorf("HTML: %v", err)
	}
}

func TestParseDate(t *testing.T) {
	want := time.Date(2026, 11, 9, 13, 0, 0, 0, time.UTC)
	for _, s := range []string{"Mon, 09 Nov 2026 13:00:00 GMT", "Mon, 9 Nov 2026 08:00:00 EST", "2026-11-09T13:00:00Z",
		"Mon, 09 Nov 2026 14:00:00 +0100", "09 Nov 2026 13:00:00 +0000", "Tue, 09 Nov 2026 13:00:00 +0000"} {
		if got := ParseDate(s); !got.Equal(want) {
			t.Errorf("%q: %v", s, got)
		}
	}
	if !ParseDate("soon").IsZero() {
		t.Error("nonsense has a date")
	}
}

func TestWrap(t *testing.T) {
	got := Wrap("one two three four\n\nfive", 9)
	want := []string{"one two", "three", "four", "", "five"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%q", got)
	}
}

// TestService: a feed read into its area once, however often it is read;
// a feed unchanged (304) posts nothing.
func TestService(t *testing.T) {
	var hits, conditional atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.HasPrefix(r.UserAgent(), "test-agent") {
			t.Errorf("User-Agent %q", r.UserAgent())
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			conditional.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write([]byte(rss2))
	}))
	defer srv.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := bbs.New(bbs.Defaults(), st, log.New(&bytes.Buffer{}, "", 0))
	b.News = bbs.NewsSettings{PathHost: "news.test", Domain: "news.test"}
	src := &Source{Name: "ex", Title: "Example", URL: srv.URL + "/rss", Area: "Example News", Group: "News", Every: time.Hour, KeepDays: 7}
	s := &Service{B: b, S: Settings{UserAgent: "test-agent", Sources: []*Source{src}}}
	if err := s.Prepare(); err != nil {
		t.Fatal(err)
	}
	a, err := st.AreaByName("Example News")
	if err != nil || !a.SysopOnly || a.Group != "News" || a.MaxDays != 7 {
		t.Fatalf("area %+v %v", a, err)
	}
	if n, err := s.Read(context.Background(), src); err != nil || n != 2 {
		t.Fatalf("first reading: %d %v", n, err)
	}
	if n, err := s.Read(context.Background(), src); err != nil || n != 0 || conditional.Load() != 1 {
		t.Fatalf("second reading: %d %v (conditional %d)", n, err, conditional.Load())
	}
	if st := s.Statuses(); len(st) != 1 || st[0].Posted != 2 || st[0].Err != nil || st[0].Last.IsZero() {
		t.Errorf("status %+v", st)
	}
	// the ETag forgotten: read whole, but nothing posted twice
	s.status = nil
	if n, err := s.Read(context.Background(), src); err != nil || n != 0 {
		t.Fatalf("third reading: %d %v", n, err)
	}
	ms, err := st.Messages(a.ID, 0, 10)
	if err != nil || len(ms) != 2 {
		t.Fatalf("messages %v %v", ms, err)
	}
	// oldest first
	if ms[0].Subject != "First story" || ms[1].Subject != "Second story" || ms[1].From != "Example" {
		t.Errorf("messages %q %q from %q", ms[0].Subject, ms[1].Subject, ms[1].From)
	}
	body := ms[1].Body
	for _, want := range []string{"By A. Writer", "A bold start.", "The whole story:\n" + srv.URL + "/news/2", "-- Example, Mon 9 Nov 2026 08:00 UTC"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	if !strings.HasPrefix(ms[0].MessageID, "<feed.") || ms[0].Origin != "feed:ex" {
		t.Errorf("Message-ID %q origin %q", ms[0].MessageID, ms[0].Origin)
	}
}

func TestFromCatalog(t *testing.T) {
	all, err := FromCatalog("all")
	if err != nil || len(all) != len(Catalog) {
		t.Fatalf("all: %d %v", len(all), err)
	}
	gov, err := FromCatalog("government, nasa")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range gov {
		names[s.Name] = true
		if s.Every == 0 || s.KeepDays == 0 {
			t.Errorf("%s: no defaults", s.Name)
		}
	}
	if !names["fedreg"] || !names["congress-bills"] || !names["nasa"] || names["bbc-world"] {
		t.Errorf("government and nasa: %v", names)
	}
	if _, err := FromCatalog("nosuch"); err == nil {
		t.Error("an unknown name taken")
	}
	areas := map[string]bool{}
	for _, c := range Catalog {
		if areas[c.Area] || len(c.Area) > 30 {
			t.Errorf("catalogue area %q twice or too long", c.Area)
		}
		areas[c.Area] = true
	}
}
