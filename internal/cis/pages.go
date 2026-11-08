package cis

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Page is a page of the service: a menu (items leading to other pages) or
// an article (text), named by its page ID (TOP, CIS-162, RATES...) and
// reached by GO with that or one of its GO words.
type Page struct {
	ID    string
	Title string // shown at the left of the page's first line
	Go    []string
	Text  []string // an article's text, or a menu's lines before its items
	Items []Item
}

// Item is a menu's choice.
type Item struct {
	Text   string
	Target string // a page ID
}

// Menu reports whether p is a menu.
func (p *Page) Menu() bool { return len(p.Items) > 0 }

// Pages are the service's pages, by ID, with the GO words that lead to
// each.
type Pages struct {
	byID  map[string]*Page
	words map[string]*Page
	order []*Page
}

//go:embed pages.txt
var pagesText string

// DefaultPages are the pages the service comes with.
func DefaultPages() *Pages {
	p, err := ParsePages(pagesText)
	if err != nil {
		panic("cis: pages.txt: " + err.Error())
	}
	return p
}

var itemPattern = regexp.MustCompile(`^\s*(\d+)\s+(.*?)\s*>\s*(\S+)\s*$`)

// ParsePages reads pages from text:
//
//	:ID GOWORD GOWORD...   a page and the GO words for it
//	=Title                 its title
//	 1 Some service > ID   a menu item leading to page ID
//	anything else          its text
//
// Lines starting with "#" are comments.  Every item must lead to a page
// there is.
func ParsePages(text string) (*Pages, error) {
	ps := &Pages{byID: map[string]*Page{}, words: map[string]*Page{}}
	var cur *Page
	for n, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \r")
		switch {
		case strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, ":"):
			f := strings.Fields(line[1:])
			if len(f) == 0 {
				return nil, fmt.Errorf("line %d: a page needs an ID", n+1)
			}
			id := strings.ToUpper(f[0])
			if ps.byID[id] != nil {
				return nil, fmt.Errorf("line %d: page %s again", n+1, id)
			}
			cur = &Page{ID: id, Title: "CompuServe"}
			ps.byID[id] = cur
			ps.order = append(ps.order, cur)
			for _, w := range append([]string{id}, f[1:]...) {
				w = strings.ToUpper(w)
				if ps.words[w] != nil && ps.words[w] != cur {
					return nil, fmt.Errorf("line %d: GO %s is page %s's", n+1, w, ps.words[w].ID)
				}
				ps.words[w] = cur
				if w != id {
					cur.Go = append(cur.Go, w)
				}
			}
		case cur == nil:
			if strings.TrimSpace(line) != "" {
				return nil, fmt.Errorf("line %d: text before the first page", n+1)
			}
		case strings.HasPrefix(line, "="):
			cur.Title = strings.TrimSpace(line[1:])
		default:
			if m := itemPattern.FindStringSubmatch(line); m != nil {
				if k, _ := strconv.Atoi(m[1]); k != len(cur.Items)+1 {
					return nil, fmt.Errorf("line %d: item %s of %s is not number %d", n+1, m[1], cur.ID, len(cur.Items)+1)
				}
				cur.Items = append(cur.Items, Item{m[2], strings.ToUpper(m[3])})
				continue
			}
			if len(cur.Items) > 0 && strings.TrimSpace(line) != "" {
				return nil, fmt.Errorf("line %d: text after %s's items", n+1, cur.ID)
			}
			cur.Text = append(cur.Text, line)
		}
	}
	for _, p := range ps.order {
		// blank lines at the ends are the file's, not the page's
		for len(p.Text) > 0 && strings.TrimSpace(p.Text[len(p.Text)-1]) == "" {
			p.Text = p.Text[:len(p.Text)-1]
		}
		for len(p.Text) > 0 && strings.TrimSpace(p.Text[0]) == "" {
			p.Text = p.Text[1:]
		}
		for _, it := range p.Items {
			if ps.byID[it.Target] == nil {
				return nil, fmt.Errorf("page %s: item %q leads to %s, which is not a page", p.ID, it.Text, it.Target)
			}
		}
	}
	if ps.byID["TOP"] == nil {
		return nil, fmt.Errorf("there is no TOP page")
	}
	return ps, nil
}

// Get is a page by its ID or a GO word.
func (ps *Pages) Get(word string) *Page {
	word = strings.ToUpper(strings.TrimSpace(word))
	if p := ps.byID[word]; p != nil {
		return p
	}
	return ps.words[word]
}

// Find is the pages whose title, text, items or GO words mention topic.
func (ps *Pages) Find(topic string) []*Page {
	topic = strings.ToUpper(strings.TrimSpace(topic))
	if topic == "" {
		return nil
	}
	var out []*Page
	for _, p := range ps.order {
		hay := []string{p.ID, p.Title}
		hay = append(hay, p.Go...)
		hay = append(hay, p.Text...)
		for _, it := range p.Items {
			hay = append(hay, it.Text)
		}
		for _, h := range hay {
			if strings.Contains(strings.ToUpper(h), topic) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// Words are the GO words, sorted.
func (ps *Pages) Words() []string {
	var out []string
	for w := range ps.words {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// Parent is the first menu with an item leading to p, or nil.
func (ps *Pages) Parent(p *Page) *Page {
	for _, m := range ps.order {
		for _, it := range m.Items {
			if it.Target == p.ID {
				return m
			}
		}
	}
	return nil
}

// heading is a page's first line: its title, and its ID at the right
// margin.
func heading(p *Page, width int) string {
	pad := width - len(p.Title) - len(p.ID)
	if pad < 1 {
		pad = 1
	}
	return p.Title + strings.Repeat(" ", pad) + p.ID
}
