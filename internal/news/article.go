// Package news is Usenet articles (RFC 5536, on RFC 5322): reading one
// that came in, checking it, making one of a BBS message, and what the
// BBS shows of one.
package news

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"
)

// Limits on articles.
const (
	MaxHeaderFields = 200
	MaxHeaderBytes  = 32 * 1024
	MaxLineBytes    = 4096 // longer than RFC 5322's 998, for what is out there
)

// Field is a header field.
type Field struct {
	Name  string
	Value string // unfolded
}

// Article is an article: its header, in order, and its body (CRLF lines).
type Article struct {
	Header []Field
	Body   []byte
}

// Errors of articles.
var (
	ErrMalformed = errors.New("malformed article")
	ErrTooLarge  = errors.New("article too large")
)

// Parse reads an article: header, empty line, body; lines may end in CRLF
// or LF.  The body is kept as it is, with CRLF line ends.
func Parse(raw []byte, max int) (*Article, error) {
	if len(raw) > max {
		return nil, ErrTooLarge
	}
	a := &Article{}
	rest := raw
	var cur *Field
	hdrBytes := 0
	for {
		i := bytes.IndexByte(rest, '\n')
		var line []byte
		if i < 0 {
			line, rest = rest, nil
		} else {
			line, rest = rest[:i], rest[i+1:]
		}
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) > MaxLineBytes {
			return nil, fmt.Errorf("%w: header line too long", ErrMalformed)
		}
		if hdrBytes += len(line); hdrBytes > MaxHeaderBytes {
			return nil, fmt.Errorf("%w: header too long", ErrMalformed)
		}
		if len(line) == 0 {
			break
		}
		if line[0] == ' ' || line[0] == '\t' {
			if cur == nil {
				return nil, fmt.Errorf("%w: continuation before a field", ErrMalformed)
			}
			cur.Value += " " + strings.TrimSpace(string(line))
			continue
		}
		name, value, ok := bytes.Cut(line, []byte(":"))
		if !ok || len(name) == 0 || bytes.ContainsAny(name, " \t") {
			return nil, fmt.Errorf("%w: bad header line", ErrMalformed)
		}
		if len(a.Header) >= MaxHeaderFields {
			return nil, fmt.Errorf("%w: too many header fields", ErrMalformed)
		}
		a.Header = append(a.Header, Field{Name: string(name), Value: strings.TrimSpace(string(value))})
		cur = &a.Header[len(a.Header)-1]
		if rest == nil {
			break
		}
	}
	// the body, with CRLF line ends
	var b bytes.Buffer
	for len(rest) > 0 {
		i := bytes.IndexByte(rest, '\n')
		var line []byte
		if i < 0 {
			line, rest = rest, nil
		} else {
			line, rest = rest[:i], rest[i+1:]
		}
		b.Write(bytes.TrimSuffix(line, []byte("\r")))
		b.WriteString("\r\n")
	}
	a.Body = b.Bytes()
	return a, nil
}

// Get is the first field of that name (any case), "" if none.
func (a *Article) Get(name string) string {
	for _, f := range a.Header {
		if strings.EqualFold(f.Name, name) {
			return f.Value
		}
	}
	return ""
}

// Set replaces the first field of that name, or adds it.
func (a *Article) Set(name, value string) {
	for i, f := range a.Header {
		if strings.EqualFold(f.Name, name) {
			a.Header[i].Value = value
			return
		}
	}
	a.Header = append(a.Header, Field{name, value})
}

// Bytes is the article as it is sent and kept: CRLF lines.
func (a *Article) Bytes() []byte {
	var b bytes.Buffer
	for _, f := range a.Header {
		b.WriteString(f.Name + ": " + f.Value + "\r\n")
	}
	b.WriteString("\r\n")
	b.Write(a.Body)
	return b.Bytes()
}

// HeaderBytes is the header alone (for HEAD).
func (a *Article) HeaderBytes() []byte {
	var b bytes.Buffer
	for _, f := range a.Header {
		b.WriteString(f.Name + ": " + f.Value + "\r\n")
	}
	return b.Bytes()
}

var msgidPattern = regexp.MustCompile(`^<[\x21-\x3b\x3d\x3f-\x7e]+@[\x21-\x3b\x3d\x3f-\x7e]+>$`)

// ValidMessageID checks a Message-ID: <left@right>, printable, no spaces,
// at most 250 characters.
func ValidMessageID(id string) bool {
	return len(id) <= 250 && msgidPattern.MatchString(id)
}

var groupPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+_-]*(\.[a-z0-9][a-z0-9+_-]*)+$`)

// ValidGroup checks a newsgroup name: lower case components with dots,
// at least two, at most 80 characters.
func ValidGroup(name string) bool {
	return len(name) <= 80 && groupPattern.MatchString(name)
}

// NewMessageID makes a Message-ID at domain.
func NewMessageID(domain string) string {
	var r [9]byte
	rand.Read(r[:])
	return fmt.Sprintf("<%s.%s@%s>", strings.ToLower(base32(time.Now().UnixNano())), hex.EncodeToString(r[:]), domain)
}

// base32 is n in base 32, lower case digits ("" for 0 or less): short, for
// Message-IDs.
func base32(n int64) string {
	const digits = "0123456789abcdefghijklmnopqrstuv"
	var b []byte
	for n > 0 {
		b = append([]byte{digits[n%32]}, b...)
		n /= 32
	}
	return string(b)
}

// Groups splits a Newsgroups field.
func Groups(field string) []string {
	var out []string
	for _, g := range strings.Split(field, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// Path elements of an article, first (newest) first.
func Path(a *Article) []string {
	var out []string
	for _, p := range strings.Split(a.Get("Path"), "!") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Check checks an article that came in: the fields RFC 5536 wants, a good
// Message-ID (and, if id is given, the same), no control messages.
func Check(a *Article, id string) error {
	for _, f := range []string{"From", "Newsgroups", "Subject", "Message-ID", "Date"} {
		if a.Get(f) == "" {
			return fmt.Errorf("%w: no %s", ErrMalformed, f)
		}
	}
	mid := a.Get("Message-ID")
	if !ValidMessageID(mid) {
		return fmt.Errorf("%w: bad Message-ID", ErrMalformed)
	}
	if id != "" && id != mid {
		return fmt.Errorf("%w: Message-ID %s, offered as %s", ErrMalformed, mid, id)
	}
	if _, err := mail.ParseDate(a.Get("Date")); err != nil {
		return fmt.Errorf("%w: bad Date", ErrMalformed)
	}
	if a.Get("Control") != "" || strings.HasPrefix(strings.ToLower(a.Get("Subject")), "cmsg ") {
		return fmt.Errorf("%w: control messages are not taken", ErrMalformed)
	}
	for _, g := range Groups(a.Get("Newsgroups")) {
		if !ValidGroup(g) {
			return fmt.Errorf("%w: bad newsgroup %q", ErrMalformed, g)
		}
	}
	return nil
}

// Local is what a local article is made of.
type Local struct {
	Name         string // the poster's name
	Address      string // and address
	Group        string
	Subject      string
	Body         string // lines separated by \n
	References   []string
	MessageID    string
	PathHost     string
	Organization string
}

// New makes an article of a BBS message.
func New(l Local) *Article {
	a := &Article{}
	from := (&mail.Address{Name: l.Name, Address: l.Address}).String()
	a.Header = []Field{
		{"Path", l.PathHost + "!not-for-mail"},
		{"From", from},
		{"Newsgroups", l.Group},
		{"Subject", mime.QEncoding.Encode("utf-8", l.Subject)},
		{"Date", time.Now().UTC().Format(time.RFC1123Z)},
		{"Message-ID", l.MessageID},
	}
	if len(l.References) > 0 {
		a.Header = append(a.Header, Field{"References", strings.Join(l.References, " ")})
	}
	if l.Organization != "" {
		a.Header = append(a.Header, Field{"Organization", mime.QEncoding.Encode("utf-8", l.Organization)})
	}
	a.Header = append(a.Header,
		Field{"MIME-Version", "1.0"},
		Field{"Content-Type", "text/plain; charset=UTF-8"},
		Field{"Content-Transfer-Encoding", "8bit"},
		Field{"User-Agent", "bbs-sec"})
	var b bytes.Buffer
	for _, line := range strings.Split(strings.TrimRight(l.Body, "\n"), "\n") {
		b.WriteString(strings.TrimRight(line, "\r") + "\r\n")
	}
	a.Body = b.Bytes()
	return a
}

var decoder = &mime.WordDecoder{CharsetReader: charsetReader}

// charsetReader reads r in charset as UTF-8 (r itself for UTF-8, ASCII or
// none); an error for a charset it does not know.
func charsetReader(charset string, r io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "utf-8", "us-ascii", "ascii", "":
		return r, nil
	}
	enc, err := htmlindex.Get(charset)
	if err != nil {
		return nil, err
	}
	return enc.NewDecoder().Reader(r), nil
}

// decodeWords undoes RFC 2047 encoded words, as best it can.
func decodeWords(s string) string {
	if out, err := decoder.DecodeHeader(s); err == nil {
		return out
	}
	return s
}

// Poster is who wrote it, for showing: the name, else the address.
func Poster(a *Article) string {
	from := a.Get("From")
	if addr, err := (&mail.AddressParser{WordDecoder: decoder}).Parse(from); err == nil {
		if addr.Name != "" {
			return addr.Name
		}
		return addr.Address
	}
	return decodeWords(from)
}

// Subject is the subject, decoded.
func Subject(a *Article) string { return decodeWords(a.Get("Subject")) }

// Text is the body as text: transfer encoding undone, in UTF-8, the first
// text/plain part of a multipart article.
func Text(a *Article) string {
	ct := a.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	return text(ct, a.Get("Content-Transfer-Encoding"), a.Body, 0)
}

// text is Text of a body with its content type and transfer encoding: a
// multipart's first text/plain part (or multipart part, as deep as 3), or a
// line saying it is not text.  At most 1 MiB is decoded.
func text(ctype, cte string, body []byte, depth int) string {
	media, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		media, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(media, "multipart/") && depth < 3 {
		mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			p, err := mr.NextRawPart()
			if err != nil {
				return "[a MIME message without a text part]"
			}
			pct := p.Header.Get("Content-Type")
			if pct == "" {
				pct = "text/plain"
			}
			if m, _, _ := mime.ParseMediaType(pct); m == "text/plain" || strings.HasPrefix(m, "multipart/") {
				data, _ := io.ReadAll(io.LimitReader(p, int64(len(body))))
				return text(pct, p.Header.Get("Content-Transfer-Encoding"), data, depth+1)
			}
		}
	}
	if media != "text/plain" {
		return fmt.Sprintf("[a %s message: read it with a newsreader]", media)
	}
	var r io.Reader = bytes.NewReader(body)
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, &stripNL{r: r})
	}
	if cr, err := charsetReader(params["charset"], r); err == nil {
		r = cr
	}
	data, _ := io.ReadAll(io.LimitReader(r, 1<<20))
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "?")
	}
	return s
}

// stripNL drops line ends, for base64.
type stripNL struct{ r io.Reader }

// Read reads from the reader with CR and LF taken out.
func (s *stripNL) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	j := 0
	for _, c := range p[:n] {
		if c != '\r' && c != '\n' {
			p[j] = c
			j++
		}
	}
	return j, err
}

// Wildmat matches a name against an RFC 3977 wildmat: patterns separated
// by commas, * and ? wild, a leading ! excluding; the last that matches
// decides.
func Wildmat(pattern, name string) bool {
	match := false
	for _, p := range strings.Split(pattern, ",") {
		p = strings.TrimSpace(p)
		neg := strings.HasPrefix(p, "!")
		if neg {
			p = p[1:]
		}
		if glob(p, name) {
			match = !neg
		}
	}
	return match
}

// glob matches * and ?, without the exponential cost of a pattern such
// as *a*a*a*a*b: one backtrack point, the last star.
func glob(p, s string) bool {
	pr, sr := []rune(p), []rune(s)
	pi, si := 0, 0
	star, mark := -1, 0
	for si < len(sr) {
		switch {
		case pi < len(pr) && (pr[pi] == '?' || pr[pi] == sr[si]):
			pi++
			si++
		case pi < len(pr) && pr[pi] == '*':
			star, mark = pi, si
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(pr) && pr[pi] == '*' {
		pi++
	}
	return pi == len(pr)
}
