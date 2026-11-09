// Package feeds reads news feeds (RSS 0.9x to 2.0, RSS 1.0, Atom and
// JSON Feed) and posts their items to read-only message areas, where every
// front end finds them: the terminals' message areas, newsreaders (when
// the area is a newsgroup), QWK readers, the web forum, gopher and
// Gemini.  An item becomes a short message: its headline, its summary
// as plain text, and a link to the whole story, which is what feeds are
// published for.
package feeds

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

// Feed is a feed as read.
type Feed struct {
	Title string
	Link  string
	Items []Item
}

// Item is one story.
type Item struct {
	GUID      string // the item's own id; else its link, else its title
	Title     string
	Link      string    // the whole story (made absolute)
	Summary   string    // plain text
	Author    string    // "" if not said
	Published time.Time // zero if not said
}

// ErrNotFeed: the document is not a feed this package reads.
var ErrNotFeed = errors.New("not an RSS, Atom or JSON feed")

// XML namespaces.
const (
	nsAtom    = "http://www.w3.org/2005/Atom"
	nsAtom03  = "http://purl.org/atom/ns#"
	nsRSS1    = "http://purl.org/rss/1.0/"
	nsRSS09   = "http://my.netscape.com/rdf/simple/0.9/"
	nsRDF     = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	nsDC      = "http://purl.org/dc/elements/1.1/"
	nsContent = "http://purl.org/rss/1.0/modules/content/"
)

// Parse reads a feed; base is where it came from, for relative links (nil:
// links as they are).
func Parse(data []byte, base *url.URL) (*Feed, error) {
	t := bytes.TrimLeft(data, "\xef\xbb\xbf \t\r\n")
	if len(t) > 0 && t[0] == '{' {
		return parseJSON(t, base)
	}
	root, err := parseXML(data)
	if err != nil {
		return nil, err
	}
	var f *Feed
	switch {
	case root.local == "rss":
		f = parseRSS(root.child("channel", ""), root.child("channel", "").children("item", ""))
	case root.local == "RDF":
		// RSS 0.90 and 1.0: the items beside the channel
		ns := nsRSS1
		if root.child("channel", nsRSS09) != nil {
			ns = nsRSS09
		}
		f = parseRSS(root.child("channel", ns), root.children("item", ns))
	case root.local == "feed" && (root.space == nsAtom || root.space == nsAtom03 || root.space == ""):
		f = parseAtom(root)
	default:
		return nil, ErrNotFeed
	}
	for i := range f.Items {
		it := &f.Items[i]
		it.Link = resolve(base, it.Link)
		if it.GUID == "" {
			it.GUID = it.Link
		}
		if it.GUID == "" {
			it.GUID = it.Title
		}
	}
	f.Link = resolve(base, f.Link)
	return f, nil
}

func resolve(base *url.URL, link string) string {
	link = strings.TrimSpace(link)
	if strings.HasPrefix(link, "www.") {
		link = "https://" + link // a host without its scheme (the NWS's)
	}
	if base == nil || link == "" {
		return link
	}
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	return base.ResolveReference(u).String()
}

// node is an XML element, as much of one as feeds need.
type node struct {
	space, local string
	attr         []xml.Attr
	text         strings.Builder // the character data directly in it
	kids         []*node
}

// parseXML reads an XML document as a tree.  The decoder is lenient: HTML's
// entities are known, and unclosed elements are forgiven, since feeds in
// the wild are often not well formed (an end tag closes what it matches,
// and one that matches nothing open is passed over).
func parseXML(data []byte) (*node, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	d.Entity = xml.HTMLEntity
	d.CharsetReader = charset.NewReaderLabel
	var stack []*node
	var root *node
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if root != nil && len(stack) == 0 {
				break // trailing junk after the document
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{space: t.Name.Space, local: t.Name.Local, attr: t.Attr}
			if len(stack) == 0 {
				if root != nil {
					return root, nil
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.kids = append(p.kids, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].local == t.Name.Local {
					stack = stack[:i]
					break
				}
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			}
		}
	}
	if root == nil {
		return nil, ErrNotFeed
	}
	return root, nil
}

// child is n's first child of that name (space "": no namespace or RSS
// 2.0's default; "*": any).
func (n *node) child(local, space string) *node {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.local == local && (space == "*" || k.space == space) {
			return k
		}
	}
	return nil
}

func (n *node) children(local, space string) []*node {
	if n == nil {
		return nil
	}
	var out []*node
	for _, k := range n.kids {
		if k.local == local && (space == "*" || k.space == space) {
			out = append(out, k)
		}
	}
	return out
}

// str is the text of n's child, trimmed.
func (n *node) str(local, space string) string {
	c := n.child(local, space)
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.text.String())
}

func (n *node) get(name string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// parseRSS reads an RSS channel and its items.
func parseRSS(ch *node, items []*node) *Feed {
	f := &Feed{Title: Text(ch.str("title", "*")), Link: ch.str("link", "*")}
	for _, n := range items {
		it := Item{Title: Text(first(n.str("title", ""), n.str("title", "*"))),
			Link: first(n.str("link", ""), n.str("link", nsRSS1), n.str("link", nsRSS09)),
			GUID: n.str("guid", "")}
		if it.Link == "" {
			// a guid that is a permalink (the default) is the link
			if g := n.child("guid", ""); g != nil && !strings.EqualFold(g.get("isPermaLink"), "false") {
				it.Link = it.GUID
			}
		}
		if it.GUID == "" {
			it.GUID = n.get("about") // RSS 1.0's rdf:about
		}
		it.Summary = Text(first(n.str("description", ""), n.str("description", "*"), n.str("encoded", nsContent)))
		it.Author = Text(first(n.str("creator", nsDC), n.str("author", "")))
		it.Published = ParseDate(first(n.str("pubDate", ""), n.str("date", nsDC), n.str("published", "*"), n.str("updated", "*")))
		f.Items = append(f.Items, it)
	}
	return f
}

// parseAtom reads an Atom feed.
func parseAtom(root *node) *Feed {
	space := root.space
	f := &Feed{Title: Text(root.str("title", space)), Link: atomLink(root, space)}
	for _, n := range root.children("entry", space) {
		it := Item{Title: Text(n.str("title", space)), Link: atomLink(n, space), GUID: n.str("id", space)}
		it.Summary = Text(first(n.str("summary", space), n.str("content", space)))
		if a := n.child("author", space); a != nil {
			it.Author = Text(a.str("name", space))
		}
		it.Published = ParseDate(first(n.str("published", space), n.str("issued", space), n.str("updated", space), n.str("modified", space)))
		f.Items = append(f.Items, it)
	}
	return f
}

// atomLink is an element's alternate link: rel="alternate" (or none),
// else the first.
func atomLink(n *node, space string) string {
	ls := n.children("link", space)
	for _, l := range ls {
		if rel := l.get("rel"); rel == "" || rel == "alternate" {
			return l.get("href")
		}
	}
	if len(ls) > 0 {
		return ls[0].get("href")
	}
	return ""
}

// parseJSON reads a JSON Feed (1.0 and 1.1).
func parseJSON(data []byte, base *url.URL) (*Feed, error) {
	var jf struct {
		Version string `json:"version"`
		Title   string `json:"title"`
		Home    string `json:"home_page_url"`
		Items   []struct {
			ID        json.RawMessage `json:"id"`
			URL       string          `json:"url"`
			Title     string          `json:"title"`
			Summary   string          `json:"summary"`
			Text      string          `json:"content_text"`
			HTML      string          `json:"content_html"`
			Published string          `json:"date_published"`
			Modified  string          `json:"date_modified"`
			Author    struct {
				Name string `json:"name"`
			} `json:"author"`
			Authors []struct {
				Name string `json:"name"`
			} `json:"authors"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &jf); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(jf.Version, "https://jsonfeed.org/") {
		return nil, ErrNotFeed
	}
	f := &Feed{Title: Text(jf.Title), Link: resolve(base, jf.Home)}
	for _, j := range jf.Items {
		it := Item{Title: Text(j.Title), Link: resolve(base, j.URL)}
		// a string, though some feeds give a number
		if err := json.Unmarshal(j.ID, &it.GUID); err != nil {
			it.GUID = strings.TrimSpace(string(j.ID))
		}
		it.Summary = first(Text(j.Summary), strings.TrimSpace(j.Text), Text(j.HTML))
		it.Author = j.Author.Name
		if it.Author == "" && len(j.Authors) > 0 {
			it.Author = j.Authors[0].Name
		}
		it.Published = ParseDate(first(j.Published, j.Modified))
		if it.GUID == "" {
			it.GUID = first(it.Link, it.Title)
		}
		f.Items = append(f.Items, it)
	}
	return f, nil
}

func first(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// dateLayouts are the forms dates come in: RFC 822 and its variants (RSS),
// RFC 3339 (Atom, JSON Feed, Dublin Core) and some looser ones.
var dateLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC3339Nano, time.RFC3339,
	"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04 -0700", "Mon, 2 Jan 2006 15:04 MST",
	"Mon, 02 Jan 06 15:04:05 -0700", "Mon, 2 Jan 06 15:04:05 MST",
	"02 Jan 2006 15:04:05 -0700", "2 Jan 2006 15:04:05 MST",
	"Monday, 02-Jan-06 15:04:05 MST", "Mon Jan _2 15:04:05 MST 2006",
	"2006-01-02T15:04:05", "2006-01-02T15:04Z07:00", "2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05", "2006-01-02",
}

// ParseDate reads a feed's date; zero if it cannot.
func ParseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	// "EDT" and the like: Go reads them as a zone of no offset unless the
	// machine is in it, so the US ones are put as offsets first
	for _, z := range [][2]string{{" EDT", " -0400"}, {" EST", " -0500"}, {" CDT", " -0500"}, {" CST", " -0600"},
		{" MDT", " -0600"}, {" MST", " -0700"}, {" PDT", " -0700"}, {" PST", " -0800"}, {" UT", " +0000"}, {" Z", " +0000"}} {
		if strings.HasSuffix(s, z[0]) {
			s = strings.TrimSuffix(s, z[0]) + z[1]
			break
		}
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	// no day of the week, or a wrong one
	if i := strings.Index(s, ", "); i > 0 && i < 10 {
		return ParseDate(s[i+2:])
	}
	return time.Time{}
}
