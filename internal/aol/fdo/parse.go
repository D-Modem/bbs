package fdo

import (
	"fmt"
	"strconv"
	"strings"
)

// The source language is the one AOL's tools and the FDO91 manual use, as
// Ada32.dll compiles it and disassembles it:
//
//	uni_start_stream <00x>
//	  man_start_object <ind_group, "Exit Gateway">
//	    mat_orientation <vff>
//	    mat_object_id <32-105>
//	    act_replace_select_action
//	      <
//	      uni_start_stream
//	      sm_send_token_raw <"pE">
//	      uni_end_stream
//	      >
//	  man_end_object
//	uni_end_stream
//
// One atom a line; its argument in angle brackets: numbers (123), hex bytes
// (0dx), global ids (32-105, 1-0-1329), "strings" (with \r \n \t \\ \"
// \xNN), words (yes, vff, select, A, an atom's name), words or numbers or'd
// together (left | center), or several of these separated by commas.  An
// argument of a < alone on its line, then atoms, then a > alone on its line,
// is a nested stream.  A ; starts a comment.  Indentation doesn't matter.
// "atom$" before a name, as the manual writes them, is allowed.
// the_unknown <proto, atom, bytes...> is an atom by number.

// Kind is the kind of an argument value.
type Kind uint8

const (
	KindNum    Kind = iota + 1 // Num
	KindHex                    // Bytes, written 0dx or 0d0ax
	KindString                 // Bytes
	KindWord                   // Text
	KindGID                    // GID
	KindPipe                   // Parts, or'd together
	KindStream                 // Stream
)

// Value is one argument value.
type Value struct {
	Kind   Kind
	Num    int64
	Bytes  []byte
	Text   string
	GID    []int64
	Parts  []Value
	Stream []*Node

	streamFrames []Frame // a decompiled nested stream
}

// Node is one atom of source: its name and its arguments.
type Node struct {
	Name string
	Args []Value
	Line int
}

// ParseError is a line the parser couldn't read.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("fdo: line %d: %s", e.Line, e.Msg) }

type tokKind uint8

const (
	tEOF tokKind = iota
	tNL
	tLT
	tGT
	tComma
	tPipe
	tWord
	tNum
	tHex
	tGID
	tString
)

type token struct {
	kind tokKind
	text string
	line int
}

type lexer struct {
	src  string
	pos  int
	line int
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isHex(c byte) bool {
	return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
func isWord(c byte) bool {
	return c == '_' || c == '$' || isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func (l *lexer) next() (token, error) {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == ' ' || c == '\t' || c == '\f' || c == '\v' {
			l.pos++
			continue
		}
		if c == ';' {
			for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
				l.pos++
			}
			continue
		}
		break
	}
	if l.pos >= len(l.src) {
		return token{tEOF, "", l.line}, nil
	}
	start, line := l.pos, l.line
	c := l.src[l.pos]
	l.pos++
	switch c {
	case '\r':
		if l.pos < len(l.src) && l.src[l.pos] == '\n' {
			l.pos++
		}
		l.line++
		return token{tNL, "", line}, nil
	case '\n':
		l.line++
		return token{tNL, "", line}, nil
	case '<':
		return token{tLT, "<", line}, nil
	case '>':
		return token{tGT, ">", line}, nil
	case ',':
		return token{tComma, ",", line}, nil
	case '|':
		return token{tPipe, "|", line}, nil
	case '"':
		var sb strings.Builder
		for {
			if l.pos >= len(l.src) || l.src[l.pos] == '\n' || l.src[l.pos] == '\r' {
				return token{}, &ParseError{line, "string not closed"}
			}
			c := l.src[l.pos]
			l.pos++
			if c == '"' {
				break
			}
			sb.WriteByte(c)
			if c == '\\' && l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
				sb.WriteByte(l.src[l.pos])
				l.pos++
			}
		}
		return token{tString, sb.String(), line}, nil
	}
	if !isWord(c) && c != '-' {
		return token{}, &ParseError{line, fmt.Sprintf("unexpected %q", c)}
	}
	for l.pos < len(l.src) && (isWord(l.src[l.pos]) || l.src[l.pos] == '-') {
		l.pos++
	}
	text := l.src[start:l.pos]
	return token{classify(text), text, line}, nil
}

// classify sorts a run of word characters into a number, hex bytes, a
// global id or a word.
func classify(s string) tokKind {
	if n := len(s); n > 1 && (s[n-1] == 'x' || s[n-1] == 'X') && allHex(s[:n-1]) {
		return tHex
	}
	digits := strings.TrimPrefix(s, "-")
	if digits != "" && allDigits(digits) {
		return tNum
	}
	if parts := strings.Split(s, "-"); len(parts) == 2 || len(parts) == 3 {
		ok := true
		for _, p := range parts {
			ok = ok && p != "" && allDigits(p)
		}
		if ok {
			return tGID
		}
	}
	return tWord
}

func allHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isHex(s[i]) {
			return false
		}
	}
	return s != ""
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return s != ""
}

type parser struct {
	lx   lexer
	tok  token
	peek *token
}

func (p *parser) advance() error {
	if p.peek != nil {
		p.tok, p.peek = *p.peek, nil
		return nil
	}
	t, err := p.lx.next()
	p.tok = t
	return err
}

func (p *parser) lookahead() (token, error) {
	if p.peek == nil {
		t, err := p.lx.next()
		if err != nil {
			return t, err
		}
		p.peek = &t
	}
	return *p.peek, nil
}

// Parse reads FDO91 source into its atoms.  It checks the syntax only:
// Compile checks the names and arguments.
func Parse(src string) ([]*Node, error) {
	p := &parser{lx: lexer{src: src, line: 1}}
	if err := p.advance(); err != nil {
		return nil, err
	}
	nodes, err := p.atoms(false)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// atoms reads atoms to the end, or to a > alone on its line if nested.
func (p *parser) atoms(nested bool) ([]*Node, error) {
	var out []*Node
	for {
		switch p.tok.kind {
		case tNL:
			if err := p.advance(); err != nil {
				return nil, err
			}
			continue
		case tEOF:
			if nested {
				return nil, &ParseError{p.tok.line, "nested stream not closed with >"}
			}
			return out, nil
		case tGT:
			if !nested {
				return nil, &ParseError{p.tok.line, "> without a nested stream"}
			}
			return out, p.advance()
		case tWord:
		default:
			return nil, &ParseError{p.tok.line, fmt.Sprintf("expected an atom name, found %q", p.tok.text)}
		}
		n := &Node{Name: strings.TrimPrefix(strings.ToLower(p.tok.text), "atom$"), Line: p.tok.line}
		if err := p.advance(); err != nil {
			return nil, err
		}
		// the argument may be on the next line, as a nested stream's < is
		if p.tok.kind == tNL {
			if t, err := p.lookahead(); err != nil {
				return nil, err
			} else if t.kind == tLT {
				if err := p.advance(); err != nil {
					return nil, err
				}
			}
		}
		if p.tok.kind == tLT {
			args, err := p.args()
			if err != nil {
				return nil, err
			}
			n.Args = args
		}
		switch p.tok.kind {
		case tNL, tEOF, tGT:
		default:
			return nil, &ParseError{p.tok.line, fmt.Sprintf("unexpected %q after %s", p.tok.text, n.Name)}
		}
		out = append(out, n)
	}
}

// args reads <...>, p.tok being the <.
func (p *parser) args() ([]Value, error) {
	line := p.tok.line
	if err := p.advance(); err != nil {
		return nil, err
	}
	if p.tok.kind == tNL {
		// a nested stream
		if err := p.advance(); err != nil {
			return nil, err
		}
		s, err := p.atoms(true)
		if err != nil {
			return nil, err
		}
		return []Value{{Kind: KindStream, Stream: s}}, nil
	}
	var out []Value
	if p.tok.kind == tGT {
		return out, p.advance()
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		switch p.tok.kind {
		case tComma:
			if err := p.advance(); err != nil {
				return nil, err
			}
		case tGT:
			return out, p.advance()
		default:
			return nil, &ParseError{line, fmt.Sprintf("expected , or > in the argument, found %q", p.tok.text)}
		}
	}
}

// value reads one value, or values or'd together.
func (p *parser) value() (Value, error) {
	v, err := p.term()
	if err != nil || p.tok.kind != tPipe {
		return v, err
	}
	pipe := Value{Kind: KindPipe, Parts: []Value{v}}
	for p.tok.kind == tPipe {
		if err := p.advance(); err != nil {
			return v, err
		}
		t, err := p.term()
		if err != nil {
			return v, err
		}
		pipe.Parts = append(pipe.Parts, t)
	}
	return pipe, nil
}

func (p *parser) term() (Value, error) {
	t := p.tok
	var v Value
	switch t.kind {
	case tNum:
		n, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return v, &ParseError{t.line, "number out of range: " + t.text}
		}
		v = Value{Kind: KindNum, Num: n}
	case tHex:
		h := t.text[:len(t.text)-1]
		if len(h)%2 == 1 {
			h = "0" + h
		}
		b := make([]byte, len(h)/2)
		for i := range b {
			x, _ := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
			b[i] = byte(x)
		}
		v = Value{Kind: KindHex, Bytes: b}
	case tGID:
		var g []int64
		for _, s := range strings.Split(t.text, "-") {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return v, &ParseError{t.line, "bad global id " + t.text}
			}
			g = append(g, n)
		}
		v = Value{Kind: KindGID, GID: g}
	case tString:
		v = Value{Kind: KindString, Bytes: unescape(t.text)}
	case tWord:
		v = Value{Kind: KindWord, Text: t.text}
	default:
		return v, &ParseError{t.line, fmt.Sprintf("expected a value, found %q", t.text)}
	}
	return v, p.advance()
}

// unescape turns a string's escapes into bytes.  The rest of the string
// is taken byte for byte, as Ada32 does: write Latin-1 text with \xNN, or
// give the compiler the bytes the client should get.
func unescape(s string) []byte {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b = append(b, c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b = append(b, '\n')
		case 'r':
			b = append(b, '\r')
		case 't':
			b = append(b, '\t')
		case '\\':
			b = append(b, '\\')
		case '"':
			b = append(b, '"')
		case 'x':
			if i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
				x, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
				b = append(b, byte(x))
				i += 2
			} else {
				b = append(b, '\\', 'x')
			}
		default:
			b = append(b, '\\', s[i])
		}
	}
	return b
}
