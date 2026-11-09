package marvel

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// FindSvc is Find > The Microsoft Network: the dialog sends what was
// typed as a query, and gets back the ids of the directory's nodes that
// match (folders, boards, rooms and files; never the messages in a
// board).  It reads each one's row from DIRSRV after.
//
// A query is fragments joined by AND, each in parentheses:
//
//	(SUBJ_NAME contains 'red' & 'shoe%') AND (APPID <> 2 or BBS_FOLDER_FLAGS <> 0)
//
// The text after "contains" is quoted terms joined by & (and), | (or) and
// ~ (not).  A term matches whole words: % is any run of a word's letters,
// _ any one, a backslash makes the next character itself, and a term of
// several words matches them in a row.
type findSvc struct {
	c    *conn
	pipe uint16
}

const (
	findSearch = 0x01
	findMax    = 200 // results at most
)

func (f *findSvc) ifaces() []iface {
	// the dialog's channel asks for six; it calls the first
	out := make([]iface, 6)
	for i := range out {
		out[i] = mosIID(0xB0+byte(i), byte(i+1))
	}
	return out
}

func (f *findSvc) call(h hostBlock) ([]byte, bool) {
	if h.oneWay() {
		return nil, false
	}
	if h.Method != findSearch {
		f.c.logf("FindSvc: no method %#x", h.Method)
		return nil, false
	}
	ps, _ := parseParams(h.Params)
	var q string
	for _, p := range ps {
		if p.Tag == 0x04 {
			q = asciiz(p.Data)
			break
		}
	}
	var ids []byte
	n := 0
	if match, err := parseQuery(q); err != nil {
		f.c.logf("FindSvc: %q: %v", q, err)
	} else if f.c.signedOn() != nil {
		for _, k := range f.c.tree().search(match) {
			ids = append(ids, k.bytes()...)
			n++
		}
	}
	// the ids end a message of their own (0x88), and a bare 0x86 then ends
	// the request: the dialog waits for both
	r := append(reply{}.dword(0).dword(uint32(n)).endStatic(), tagStreamEnd)
	f.c.sendPipe(f.pipe, hostBlock{Iface: h.Iface, Method: h.Method, ID: h.ID, Params: append(r, ids...)}.bytes())
	f.c.sendPipe(f.pipe, hostBlock{Iface: h.Iface, Method: h.Method, ID: h.ID, Params: []byte{tagComplete}}.bytes())
	return nil, false
}

// search is the nodes a query matches, by name, up to findMax.
func (t *tree) search(match func(*node) bool) []nodeKey {
	var out []*node
	for k, n := range t.nodes {
		switch k {
		case keyNowhere, keyWorldwide, keyCentral:
			continue
		}
		if match(n) {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].name), strings.ToLower(out[j].name)
		if a != b {
			return a < b
		}
		return out[i].key.a < out[j].key.a || out[i].key.a == out[j].key.a && out[i].key.b < out[j].key.b
	})
	var keys []nodeKey
	for i, n := range out {
		if i == findMax {
			break
		}
		keys = append(keys, n.key)
	}
	return keys
}

// the text a field names, of a node
func fieldText(field string, n *node) (string, bool) {
	switch field {
	case "NAME":
		return n.name, true
	case "SUBJECT":
		return "", true // nodes here have no topics
	case "DESCRIPTION":
		return n.desc, true
	case "SUBJ_NAME":
		return n.name, true
	case "NAME_DESC", "SEARCH_PROPS":
		return n.name + " " + n.desc, true
	case "SUBJ_DESC":
		return n.desc, true
	case "PLACES":
		return "", true // nothing here is anywhere in particular
	}
	return "", false
}

// the number a field names, of a node
func fieldNumber(field string, n *node) (int, bool) {
	switch field {
	case "APPID":
		return int(n.app), true
	case "BBS_FOLDER_FLAGS":
		// a board is an MSN board (1), or a newsgroup's (0)
		if n.board != nil && n.board.Newsgroup == "" {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

var errQuery = errors.New("a query this server can't read")

type qtoken struct {
	kind byte // 'w' a word, 'n' a number, 's' a quoted term, or the operator itself ('<' is <>)
	text string
}

func lexQuery(q string) ([]qtoken, error) {
	var out []qtoken
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case strings.IndexByte("()&|~=,", c) >= 0:
			out = append(out, qtoken{kind: c})
			i++
		case c == '<' && i+1 < len(q) && q[i+1] == '>':
			out = append(out, qtoken{kind: '<'})
			i += 2
		case c == '\'':
			var sb strings.Builder
			i++
			for {
				if i >= len(q) {
					return nil, errQuery
				}
				if q[i] == '\\' && i+1 < len(q) {
					sb.WriteByte(q[i])
					sb.WriteByte(q[i+1])
					i += 2
					continue
				}
				if q[i] == '\'' {
					if i+1 < len(q) && q[i+1] == '\'' {
						sb.WriteByte('\'')
						i += 2
						continue
					}
					i++
					break
				}
				sb.WriteByte(q[i])
				i++
			}
			out = append(out, qtoken{kind: 's', text: sb.String()})
		case c >= '0' && c <= '9':
			j := i
			for j < len(q) && q[j] >= '0' && q[j] <= '9' {
				j++
			}
			out = append(out, qtoken{kind: 'n', text: q[i:j]})
			i = j
		case c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
			j := i
			for j < len(q) && (q[j] == '_' || q[j] >= 'A' && q[j] <= 'Z' || q[j] >= 'a' && q[j] <= 'z' || q[j] >= '0' && q[j] <= '9') {
				j++
			}
			out = append(out, qtoken{kind: 'w', text: strings.ToUpper(q[i:j])})
			i = j
		default:
			return nil, errQuery
		}
	}
	return out, nil
}

type qparser struct {
	toks []qtoken
	pos  int
}

func (p *qparser) peek() qtoken {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return qtoken{}
}

func (p *qparser) is(kind byte, word string) bool {
	t := p.peek()
	return t.kind == kind && (word == "" || t.text == word)
}

func (p *qparser) next() qtoken {
	t := p.peek()
	p.pos++
	return t
}

// parseQuery compiles a query into a test of a node.
func parseQuery(q string) (func(*node) bool, error) {
	toks, err := lexQuery(q)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return func(*node) bool { return true }, nil
	}
	p := &qparser{toks: toks}
	m, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, errQuery
	}
	return m, nil
}

func (p *qparser) or() (func(*node) bool, error) {
	l, err := p.and()
	for err == nil && (p.is('w', "OR") || p.is('|', "")) {
		p.next()
		var r func(*node) bool
		if r, err = p.and(); err == nil {
			a := l
			l = func(n *node) bool { return a(n) || r(n) }
		}
	}
	return l, err
}

func (p *qparser) and() (func(*node) bool, error) {
	l, err := p.not()
	for err == nil && (p.is('w', "AND") || p.is('&', "")) {
		p.next()
		var r func(*node) bool
		if r, err = p.not(); err == nil {
			a := l
			l = func(n *node) bool { return a(n) && r(n) }
		}
	}
	return l, err
}

func (p *qparser) not() (func(*node) bool, error) {
	switch {
	case p.is('w', "NOT") || p.is('~', ""):
		p.next()
		m, err := p.not()
		if err != nil {
			return nil, err
		}
		return func(n *node) bool { return !m(n) }, nil
	case p.is('(', ""):
		p.next()
		m, err := p.or()
		if err != nil || !p.is(')', "") {
			return nil, errQuery
		}
		p.next()
		return m, nil
	case p.is('w', ""):
		return p.comparison()
	}
	return nil, errQuery
}

// comparison is FIELD contains text, FIELD = n, FIELD <> n or FIELD in (n, ...).
func (p *qparser) comparison() (func(*node) bool, error) {
	field := p.next().text
	switch t := p.next(); {
	case t.kind == 'w' && t.text == "CONTAINS":
		m, err := p.textOr()
		if err != nil {
			return nil, err
		}
		if field == "LOCALES" {
			return func(*node) bool { return true }, nil // everything here is in every language
		}
		return func(n *node) bool {
			s, ok := fieldText(field, n)
			return ok && m(words(s))
		}, nil
	case t.kind == '=' || t.kind == '<':
		v, err := p.number()
		if err != nil {
			return nil, err
		}
		eq := t.kind == '='
		return func(n *node) bool {
			x, ok := fieldNumber(field, n)
			return ok && (x == v) == eq
		}, nil
	case t.kind == 'w' && t.text == "IN":
		if !p.is('(', "") {
			return nil, errQuery
		}
		p.next()
		var set []int
		for {
			v, err := p.number()
			if err != nil {
				return nil, err
			}
			set = append(set, v)
			if p.is(')', "") {
				p.next()
				break
			}
			if !p.is(',', "") {
				return nil, errQuery
			}
			p.next()
		}
		return func(n *node) bool {
			x, ok := fieldNumber(field, n)
			return ok && contains(set, x)
		}, nil
	}
	return nil, errQuery
}

func contains(set []int, x int) bool {
	for _, v := range set {
		if v == x {
			return true
		}
	}
	return false
}

func (p *qparser) number() (int, error) {
	if !p.is('n', "") {
		return 0, errQuery
	}
	return strconv.Atoi(p.next().text)
}

func (p *qparser) textOr() (func([]string) bool, error) {
	l, err := p.textAnd()
	for err == nil && (p.is('|', "") || p.is(',', "")) {
		p.next()
		var r func([]string) bool
		if r, err = p.textAnd(); err == nil {
			a := l
			l = func(w []string) bool { return a(w) || r(w) }
		}
	}
	return l, err
}

func (p *qparser) textAnd() (func([]string) bool, error) {
	l, err := p.textNot()
	for err == nil && p.is('&', "") {
		p.next()
		var r func([]string) bool
		if r, err = p.textNot(); err == nil {
			a := l
			l = func(w []string) bool { return a(w) && r(w) }
		}
	}
	return l, err
}

func (p *qparser) textNot() (func([]string) bool, error) {
	switch {
	case p.is('~', ""):
		p.next()
		m, err := p.textNot()
		if err != nil {
			return nil, err
		}
		return func(w []string) bool { return !m(w) }, nil
	case p.is('(', ""):
		p.next()
		m, err := p.textOr()
		if err != nil || !p.is(')', "") {
			return nil, errQuery
		}
		p.next()
		return m, nil
	case p.is('s', ""):
		phrase := termWords(p.next().text)
		if len(phrase) == 0 {
			return func([]string) bool { return false }, nil
		}
		return func(w []string) bool { return hasPhrase(w, phrase) }, nil
	}
	return nil, errQuery
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// words are a text's words, in lower case.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !isWordRune(r) })
}

// a pattern's rune: a letter or digit, or a wildcard
type prune struct {
	r    rune
	wild byte // 0, '%' or '_'
}

// termWords is a term's words as patterns: wildcards inside a word, and
// anything else (an escaped wildcard too) between them.
func termWords(t string) [][]prune {
	var out [][]prune
	var cur []prune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, cur)
			cur = nil
		}
	}
	rs := []rune(strings.ToLower(t))
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\\' && i+1 < len(rs):
			i++
			if isWordRune(rs[i]) {
				cur = append(cur, prune{r: rs[i]})
			} else {
				flush()
			}
		case r == '%' || r == '_':
			cur = append(cur, prune{wild: byte(r)})
		case isWordRune(r):
			cur = append(cur, prune{r: r})
		default:
			flush()
		}
	}
	flush()
	return out
}

func hasPhrase(ws []string, phrase [][]prune) bool {
	for i := 0; i+len(phrase) <= len(ws); i++ {
		ok := true
		for j, p := range phrase {
			if !globMatch(p, []rune(ws[i+j])) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// globMatch matches a whole word.
func globMatch(p []prune, w []rune) bool {
	if len(p) == 0 {
		return len(w) == 0
	}
	switch p[0].wild {
	case '%':
		for i := 0; i <= len(w); i++ {
			if globMatch(p[1:], w[i:]) {
				return true
			}
		}
		return false
	case '_':
		return len(w) > 0 && globMatch(p[1:], w[1:])
	}
	return len(w) > 0 && w[0] == p[0].r && globMatch(p[1:], w[1:])
}
