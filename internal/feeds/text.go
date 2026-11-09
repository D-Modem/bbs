package feeds

import (
	"html"
	"strings"

	xhtml "golang.org/x/net/html"
)

// blocks are the HTML elements that end a paragraph.
var blocks = map[string]bool{"p": true, "div": true, "br": true, "li": true, "tr": true, "h1": true,
	"h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "blockquote": true, "pre": true,
	"ul": true, "ol": true, "table": true, "hr": true, "figure": true, "section": true, "article": true,
	"dl": true, "dt": true, "dd": true}

// Text is a feed's text (often HTML, sometimes HTML escaped twice) as
// plain text: paragraphs kept, a blank line between them, spaces runs made
// one, pictures, scripts and styles left out.
func Text(s string) string { return text(s, 2) }

func text(s string, depth int) string {
	if !strings.ContainsAny(s, "<&") {
		return tidy(s)
	}
	z := xhtml.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case xhtml.ErrorToken:
			out := tidy(b.String())
			// text that was HTML escaped twice: once more
			if depth > 0 && strings.Contains(out, "<") && strings.Contains(out, ">") && out != s {
				return text(out, depth-1)
			}
			return out
		case xhtml.TextToken:
			if skip == 0 {
				// the tokenizer has unescaped it: escaped again for tidy
				b.WriteString(html.EscapeString(string(z.Text())))
			}
		case xhtml.StartTagToken, xhtml.EndTagToken, xhtml.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			switch tag {
			case "script", "style", "noscript":
				if tt == xhtml.StartTagToken {
					skip++
				} else if tt == xhtml.EndTagToken && skip > 0 {
					skip--
				}
			}
			if blocks[tag] {
				b.WriteString("\n\n")
			}
		}
	}
}

// tidy makes runs of spaces one and keeps at most one blank line between
// paragraphs.
func tidy(s string) string {
	s = html.UnescapeString(s)
	var paras []string
	for _, p := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n\n") {
		if p = strings.Join(strings.Fields(p), " "); p != "" {
			paras = append(paras, p)
		}
	}
	return strings.Join(paras, "\n\n")
}

// Wrap breaks text into lines of at most width characters, at spaces
// where it can; paragraphs keep a blank line between them.
func Wrap(s string, width int) []string {
	var out []string
	for i, p := range strings.Split(s, "\n\n") {
		if i > 0 {
			out = append(out, "")
		}
		line := ""
		for _, w := range strings.Fields(p) {
			for len([]rune(w)) > width {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				r := []rune(w)
				out = append(out, string(r[:width]))
				w = string(r[width:])
			}
			switch {
			case line == "":
				line = w
			case len([]rune(line))+1+len([]rune(w)) <= width:
				line += " " + w
			default:
				out = append(out, line)
				line = w
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
