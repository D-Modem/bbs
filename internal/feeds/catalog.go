package feeds

import (
	"fmt"
	"strings"
	"time"
)

// Catalog is the feeds the BBS knows of, for [feeds] catalog: news
// outlets' own feeds (shown as headline, summary and link, which is what
// they publish them for), and the US government's, which are in the public
// domain.  Each was tried and read when it was added (November 2026).
var Catalog = []Source{
	// world and national news
	{Name: "bbc-world", Title: "BBC News", URL: "https://feeds.bbci.co.uk/news/world/rss.xml", Area: "BBC World News", Group: "News"},
	{Name: "npr-news", Title: "NPR News", URL: "https://feeds.npr.org/1001/rss.xml", Area: "NPR News", Group: "News"},
	{Name: "ap-news", Title: "AP News", URL: "https://news.google.com/rss/search?q=site:apnews.com&hl=en-US&gl=US&ceid=US:en",
		Area: "AP News", Group: "News", Description: "The Associated Press's stories, as Google News lists them"},
	{Name: "guardian-world", Title: "The Guardian", URL: "https://www.theguardian.com/world/rss", Area: "Guardian World News", Group: "News"},
	{Name: "aljazeera", Title: "Al Jazeera", URL: "https://www.aljazeera.com/xml/rss/all.xml", Area: "Al Jazeera", Group: "News"},
	{Name: "cbc-world", Title: "CBC News", URL: "https://www.cbc.ca/webfeed/rss/rss-world", Area: "CBC World News", Group: "News"},
	{Name: "dw", Title: "Deutsche Welle", URL: "https://rss.dw.com/rdf/rss-en-all", Area: "Deutsche Welle", Group: "News"},
	{Name: "globalvoices", Title: "Global Voices", URL: "https://globalvoices.org/feed/", Area: "Global Voices", Group: "News",
		Description: "Global Voices (CC BY): citizen media from around the world"},

	// business
	{Name: "bbc-business", Title: "BBC News", URL: "https://feeds.bbci.co.uk/news/business/rss.xml", Area: "BBC Business News", Group: "Business"},
	{Name: "npr-business", Title: "NPR Business", URL: "https://feeds.npr.org/1006/rss.xml", Area: "NPR Business", Group: "Business"},
	{Name: "sec-press", Title: "SEC", URL: "https://www.sec.gov/news/pressreleases.rss", Area: "SEC Press Releases", Group: "Business"},
	{Name: "sec-8k", Title: "SEC EDGAR", URL: "https://www.sec.gov/cgi-bin/browse-edgar?action=getcurrent&type=8-K&count=40&output=atom",
		Area: "SEC 8-K Filings", Group: "Business", Description: "Companies' latest 8-K reports to the SEC", Every: 30 * time.Minute, KeepDays: 7},

	// technology
	{Name: "bbc-tech", Title: "BBC News", URL: "https://feeds.bbci.co.uk/news/technology/rss.xml", Area: "BBC Technology", Group: "Technology"},
	{Name: "ars", Title: "Ars Technica", URL: "https://feeds.arstechnica.com/arstechnica/index", Area: "Ars Technica", Group: "Technology"},
	{Name: "register", Title: "The Register", URL: "https://www.theregister.com/headlines.atom", Area: "The Register", Group: "Technology"},
	{Name: "hackernews", Title: "Hacker News", URL: "https://news.ycombinator.com/rss", Area: "Hacker News", Group: "Technology"},

	// science
	{Name: "nasa", Title: "NASA", URL: "https://www.nasa.gov/feed/", Area: "NASA News", Group: "Science"},
	{Name: "usgs-quakes", Title: "USGS", URL: "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/4.5_day.atom",
		Area: "Earthquakes", Group: "Science", Description: "Earthquakes of magnitude 4.5 and more, from the USGS", Every: 15 * time.Minute, KeepDays: 14},

	// government
	{Name: "fedreg", Title: "Federal Register", URL: "https://www.federalregister.gov/api/v1/documents.rss?conditions%5Bsignificant%5D=1",
		Area: "Federal Register", Group: "Government", Description: "Significant rules and notices in the Federal Register"},
	{Name: "congress-bills", Title: "Congress.gov", URL: "https://www.congress.gov/rss/most-viewed-bills.xml",
		Area: "Most-Viewed Bills", Group: "Government", Description: "The week's most-viewed bills on Congress.gov", Every: 6 * time.Hour, KeepDays: 90},
	{Name: "congress-president", Title: "Congress.gov", URL: "https://www.congress.gov/rss/presented-to-president.xml",
		Area: "Bills to the President", Group: "Government", Description: "Bills presented to the President", Every: 2 * time.Hour, KeepDays: 90},
	{Name: "house-floor", Title: "Congress.gov", URL: "https://www.congress.gov/rss/house-floor-today.xml",
		Area: "House Floor", Group: "Government", Description: "On the House floor today"},
	{Name: "senate-floor", Title: "Congress.gov", URL: "https://www.congress.gov/rss/senate-floor-today.xml",
		Area: "Senate Floor", Group: "Government", Description: "On the Senate floor today"},
	{Name: "whitehouse", Title: "The White House", URL: "https://www.whitehouse.gov/news/feed/", Area: "White House News", Group: "Government"},
	{Name: "nws", Title: "National Weather Service", URL: "https://www.weather.gov/rss_page.php?site_name=nws",
		Area: "Weather Service News", Group: "Government"},
}

// Defaults for a feed that does not say.
const (
	DefaultEvery = 30 * time.Minute
	DefaultKeep  = 30
)

// FromCatalog is the catalogue's feeds that names asks for: "all", feeds
// by their names, or every feed of a group ("news", "government").
func FromCatalog(names string) ([]*Source, error) {
	var out []*Source
	seen := map[string]bool{}
	add := func(c Source) {
		if seen[c.Name] {
			return
		}
		seen[c.Name] = true
		if c.Every == 0 {
			c.Every = DefaultEvery
		}
		if c.KeepDays == 0 {
			c.KeepDays = DefaultKeep
		}
		out = append(out, &c)
	}
	for _, w := range strings.FieldsFunc(names, func(r rune) bool { return r == ',' || r == ' ' }) {
		found := false
		for _, c := range Catalog {
			if strings.EqualFold(w, "all") || strings.EqualFold(w, c.Name) || strings.EqualFold(w, c.Group) {
				add(c)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("the catalogue has no feed or group %q", w)
		}
	}
	return out, nil
}
