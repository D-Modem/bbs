package imageboard

import (
	"fmt"
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"
)

// A comment is kept as it was typed and marked up as it is shown, as
// 4chan marks it up: >>123 links to a post, >>>/b/123 to one on another
// board (>>>/b/ to the board), a line that starts with > is quoted
// (greentext), [spoiler]...[/spoiler] hides text until it is hovered,
// [code]...[/code] is set as code, and links are links.

var tokenPat = regexp.MustCompile(`>>>/([a-z0-9]{1,16})/(\d{1,12})?|>>(\d{1,12})|https?://[^\s<>"'\x60]+|\[/?spoiler\]`)

// Linker finds what a comment's quotes point at.
type linker interface {
	// thread is the thread a post of this board is in (ok false: it is
	// not there)
	thread(no int64) (int64, bool)
	// cross is the same for a post of another board; a board alone (no
	// 0) is ok if it is there
	cross(dir string, no int64) (int64, bool)
}

// quotes are the numbers of this board's posts a comment quotes, and the
// others' by board.
func quotes(text string) (nos []int64, cross map[string][]int64) {
	for _, m := range tokenPat.FindAllStringSubmatch(text, -1) {
		switch {
		case m[3] != "":
			n, _ := strconv.ParseInt(m[3], 10, 64)
			nos = append(nos, n)
		case m[1] != "":
			if cross == nil {
				cross = map[string][]int64{}
			}
			n, _ := strconv.ParseInt(m[2], 10, 64)
			cross[m[1]] = append(cross[m[1]], n)
		}
	}
	return nos, cross
}

// markup is a comment as HTML: here is the thread being shown (0: a
// board's index, where every quote links to its thread's page), dir the
// board's.
func markup(text, dir string, here int64, l linker) template.HTML {
	var b strings.Builder
	parts := strings.Split(text, "[code]")
	spoilers := 0
	for i, part := range parts {
		if i > 0 {
			code, rest, closed := strings.Cut(part, "[/code]")
			if !closed {
				code, rest = part, ""
			}
			b.WriteString(`<pre class="code">`)
			b.WriteString(html.EscapeString(strings.Trim(code, "\n")))
			b.WriteString(`</pre>`)
			part = strings.TrimPrefix(rest, "\n")
		}
		lines := strings.Split(part, "\n")
		for j, line := range lines {
			quoted := strings.HasPrefix(line, ">") && !quoteAt(line)
			if quoted {
				b.WriteString(`<span class="quote">`)
			}
			last := 0
			for _, m := range tokenPat.FindAllStringSubmatchIndex(line, -1) {
				b.WriteString(html.EscapeString(line[last:m[0]]))
				last = m[1]
				tok := line[m[0]:m[1]]
				switch {
				case tok == "[spoiler]":
					spoilers++
					b.WriteString(`<s>`)
				case tok == "[/spoiler]":
					if spoilers > 0 {
						spoilers--
						b.WriteString(`</s>`)
					} else {
						b.WriteString(html.EscapeString(tok))
					}
				case m[6] >= 0: // >>123
					n, _ := strconv.ParseInt(line[m[6]:m[7]], 10, 64)
					b.WriteString(postLink(tok, dir, n, here, l))
				case m[2] >= 0: // >>>/b/123, >>>/b/
					board := line[m[2]:m[3]]
					var n int64
					if m[4] >= 0 {
						n, _ = strconv.ParseInt(line[m[4]:m[5]], 10, 64)
					}
					t, ok := l.cross(board, n)
					switch {
					case !ok:
						fmt.Fprintf(&b, `<span class="deadlink">%s</span>`, html.EscapeString(tok))
					case n == 0:
						fmt.Fprintf(&b, `<a href="%s/%s/" class="quotelink">%s</a>`, Base, board, html.EscapeString(tok))
					default:
						fmt.Fprintf(&b, `<a href="%s/%s/thread/%d#p%d" class="quotelink">%s</a>`, Base, board, t, n,
							html.EscapeString(tok))
					}
				default: // a link
					u := strings.TrimRight(tok, ".,;:!?)")
					fmt.Fprintf(&b, `<a href="%s" rel="nofollow noreferrer noopener" target="_blank">%s</a>`,
						html.EscapeString(u), html.EscapeString(u))
					b.WriteString(html.EscapeString(tok[len(u):]))
				}
			}
			b.WriteString(html.EscapeString(line[last:]))
			if quoted {
				b.WriteString(`</span>`)
			}
			if j < len(lines)-1 {
				b.WriteString("<br>")
			}
		}
	}
	b.WriteString(strings.Repeat("</s>", spoilers))
	return template.HTML(b.String())
}

// quoteAt says whether a line starts with a post's link (>>123), which
// does not make it greentext.
func quoteAt(line string) bool {
	m := tokenPat.FindStringIndex(line)
	return m != nil && m[0] == 0 && strings.HasPrefix(line, ">>")
}

// postLink is >>123 as a link: to the post in this thread, or to its
// thread's page; struck through if it is not there.
func postLink(tok, dir string, n, here int64, l linker) string {
	t, ok := l.thread(n)
	if !ok {
		return `<span class="deadlink">` + html.EscapeString(tok) + `</span>`
	}
	text := html.EscapeString(tok)
	if n == t {
		text += " (OP)"
	}
	if here != 0 && t == here {
		return fmt.Sprintf(`<a href="#p%d" class="quotelink" data-no="%d">%s</a>`, n, n, text)
	}
	if here != 0 {
		text += " &rarr;"
	}
	return fmt.Sprintf(`<a href="%s/%s/thread/%d#p%d" class="quotelink" data-no="%d">%s</a>`, Base, dir, t, n, n, text)
}

// abbreviate cuts a comment for a board's index: its first lines, and
// whether it was cut.
func abbreviate(text string, lines int) (string, bool) {
	n := 0
	for i, c := range text {
		if c == '\n' {
			if n++; n == lines {
				return text[:i], true
			}
		}
		if i >= 1500 {
			if j := strings.LastIndexAny(text[:i], " \n"); j > 1000 {
				i = j
			}
			return text[:i], true
		}
	}
	return text, false
}

// cleanComment is a comment as kept: lines ended by \n, control
// characters gone, blank lines at its ends trimmed, runs of blank lines
// cut to two.
func cleanComment(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n', r == '\t':
			return r
		case r == '\r':
			return '\n'
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0, r == '‮', r == '‭', r == '⁦', r == '⁧', r == '⁨':
			return -1
		}
		return r
	}, s)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	s = strings.Trim(strings.Join(lines, "\n"), "\n")
	for strings.Contains(s, "\n\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n\n", "\n\n\n")
	}
	return s
}
