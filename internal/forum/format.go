package forum

import (
	"fmt"
	"hash/fnv"
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

var funcs = template.FuncMap{
	"body":      renderBody,
	"when":      when,
	"date":      func(t time.Time) string { return t.Format("02 January 2006") },
	"longdate":  longDate,
	"odd":       func(n int) bool { return n%2 == 1 },
	"shortdate": func(t time.Time) string { return t.Format("Jan. 02, 2006") },
	"clock":     func(t time.Time) string { return t.Format("03:04 PM") },
	"initial":   initial,
	"avatar":    avatarClass,
	"group":     groupTitle,
	"urlq":      url.QueryEscape,
	"path":      url.PathEscape,
	"add":       func(a, b int) int { return a + b },
	"plural":    plural,
	"num":       func(n int) string { return groupDigits(n) },
	"isSysop":   func(u *store.User) bool { return u != nil && u.Sysop },
	"truncate":  truncate,
	"snippet":   snippet,
	"av":        func(name, size string) map[string]string { return map[string]string{"Name": name, "Size": size} },
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out
	},
}

// whenTime is a time as boards show it: today, yesterday, or the date.
func when(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	now := time.Now()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today):
		return "Today, " + t.Format("03:04 PM")
	case !t.Before(today.AddDate(0, 0, -1)):
		return "Yesterday, " + t.Format("03:04 PM")
	case t.Year() == now.Year():
		return t.Format("02 January - 03:04 PM")
	}
	return t.Format("02 January 2006 - 03:04 PM")
}

// longDate is a time in full, as "12th October 2026 - 11:21 PM".
func longDate(t time.Time) string {
	d := t.Day()
	suffix := "th"
	if d < 11 || d > 13 {
		switch d % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(d) + suffix + t.Format(" January 2006 - 03:04 PM")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return groupDigits(n) + " " + one
	}
	return groupDigits(n) + " " + many
}

func groupDigits(n int) string {
	s := strconv.Itoa(n)
	if n < 0 || len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n])) + "…"
}

// snippet is the start of a message's own words: what it quotes left out.
func snippet(body string, n int) string {
	var words []string
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || quoteLine.MatchString(l) || wroteLine.MatchString(t) {
			continue
		}
		words = append(words, t)
	}
	return truncate(strings.Join(words, " "), n)
}

// initial is the letter a name's picture shows.
func initial(name string) string {
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return "?"
}

// avatarClass is a name's picture's color (one of a palette, by the name).
func avatarClass(name string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	return fmt.Sprintf("av%d", h.Sum32()%12)
}

// groupTitle is what a user is called under their name.
func groupTitle(u *store.User) string {
	switch {
	case u == nil:
		return "Guests"
	case u.Sysop:
		return "Administrators"
	case u.Locked:
		return "Banned"
	}
	return "Members"
}

var (
	quoteLine = regexp.MustCompile(`^ ?[A-Za-z]{0,3}>`)
	link      = regexp.MustCompile(`\b(?:https?://|www\.)[^\s<>"']+[^\s<>"'.,;:!?)\]]`)
	wroteLine = regexp.MustCompile(`^(.{1,40}) wrote:$`)
)

// renderBody is a message's text as HTML: its quoted lines (> or XX>, as
// BBSes quote) in quote boxes, links made links, the rest as it is.
func renderBody(body string) template.HTML {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if quoteLine.MatchString(l) {
			if !inQuote {
				b.WriteString(`<blockquote class="quote">`)
				inQuote = true
			}
			text := strings.TrimPrefix(strings.TrimSpace(l[strings.Index(l, ">")+1:]), " ")
			b.WriteString(linkify(text))
			b.WriteString("<br>")
			continue
		}
		if inQuote {
			b.WriteString(`</blockquote>`)
			inQuote = false
			if strings.TrimSpace(l) == "" {
				continue // the blank line after a quote: the box is gap enough
			}
		}
		// "Name wrote:" before a quote is its title
		if m := wroteLine.FindStringSubmatch(strings.TrimSpace(l)); m != nil && i+1 < len(lines) && quoteLine.MatchString(lines[i+1]) {
			b.WriteString(`<p class="citation">` + html.EscapeString(m[1]) + ` said:</p>`)
			continue
		}
		b.WriteString(linkify(l))
		if i < len(lines)-1 {
			b.WriteString("<br>")
		}
	}
	if inQuote {
		b.WriteString(`</blockquote>`)
	}
	return template.HTML(b.String())
}

// linkify escapes text, its web addresses made links.
func linkify(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range link.FindAllStringIndex(s, -1) {
		b.WriteString(html.EscapeString(s[last:m[0]]))
		u := s[m[0]:m[1]]
		href := u
		if strings.HasPrefix(href, "www.") {
			href = "http://" + href
		}
		fmt.Fprintf(&b, `<a href="%s" rel="nofollow ugc noopener" class="bbc_url">%s</a>`,
			html.EscapeString(href), html.EscapeString(u))
		last = m[1]
	}
	b.WriteString(html.EscapeString(s[last:]))
	return b.String()
}

// pages is a pagination's links.
type pages struct {
	Cur, Total int
	Base       string // the URL, to which page=N is put
}

func (p pages) URL(n int) string {
	sep := "?"
	if strings.Contains(p.Base, "?") {
		sep = "&"
	}
	if n <= 1 {
		return p.Base
	}
	return p.Base + sep + "page=" + strconv.Itoa(n)
}

// Shown are the page numbers to show (0: a gap).
func (p pages) Shown() []int {
	var out []int
	for i := 1; i <= p.Total; i++ {
		if i == 1 || i == p.Total || i >= p.Cur-2 && i <= p.Cur+2 {
			out = append(out, i)
		} else if len(out) > 0 && out[len(out)-1] != 0 {
			out = append(out, 0)
		}
	}
	return out
}

func (p pages) Prev() int { return p.Cur - 1 }
func (p pages) Next() int {
	if p.Cur < p.Total {
		return p.Cur + 1
	}
	return 0
}

func newPages(base string, count, cur int) pages {
	total := (count + perPage - 1) / perPage
	if total < 1 {
		total = 1
	}
	if cur < 1 {
		cur = 1
	}
	if cur > total {
		cur = total
	}
	return pages{Cur: cur, Total: total, Base: base}
}

func urlEscape(s string) string { return url.QueryEscape(s) }
