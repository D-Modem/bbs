package oscar

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ident is a screen name as compared: lower case, no spaces.
func ident(sn string) string { return strings.ToLower(strings.ReplaceAll(sn, " ", "")) }

// isUIN: the screen name is an ICQ number.
func isUIN(sn string) bool {
	if len(sn) < 4 || len(sn) > 10 {
		return false
	}
	for _, r := range sn {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// IM charsets (ICBM's and chat's).
const (
	csASCII  = 0x0000
	csUCS2   = 0x0002
	csLatin1 = 0x0003
)

// decodeText is a message's bytes in its charset as UTF-8.
func decodeText(p []byte, charset uint16) string {
	switch charset {
	case csUCS2:
		u := make([]uint16, len(p)/2)
		for i := range u {
			u[i] = uint16(p[2*i])<<8 | uint16(p[2*i+1])
		}
		return string(utf16.Decode(u))
	case csLatin1:
		return latin1(p)
	}
	if utf8.Valid(p) {
		return string(p)
	}
	return latin1(p)
}

// latin1 is ISO 8859-1 as UTF-8.
func latin1(p []byte) string {
	r := make([]rune, len(p))
	for i, c := range p {
		r[i] = rune(c)
	}
	return string(r)
}

// encodeText is UTF-8 text in the plainest charset that holds it.
func encodeText(s string) ([]byte, uint16) {
	ascii, latin := true, true
	for _, r := range s {
		if r >= 0x80 {
			ascii = false
		}
		if r >= 0x100 {
			latin = false
		}
	}
	switch {
	case ascii:
		return []byte(s), csASCII
	case latin:
		p := make([]byte, 0, len(s))
		for _, r := range s {
			p = append(p, byte(r))
		}
		return p, csLatin1
	}
	var p []byte
	for _, u := range utf16.Encode([]rune(s)) {
		p = append(p, byte(u>>8), byte(u))
	}
	return p, csUCS2
}

var (
	breakTag = regexp.MustCompile(`(?i)<\s*(br|/p|p|/div|div|hr)\b[^>]*>`)
	anyTag   = regexp.MustCompile(`<[^>]*>`)
	looksTag = regexp.MustCompile(`(?i)<\s*/?\s*(html|body|font|b|i|u|a|br|p|div|span|s|sub|sup|img|hr|pre)\b`)
)

// plainText is an IM's text as the BBS takes it: its HTML (AIM's) made
// one line of text, entities decoded; plain text (ICQ's) as it is.
func plainText(s string) string {
	if looksTag.MatchString(s) {
		s = breakTag.ReplaceAllString(s, " ")
		s = anyTag.ReplaceAllString(s, "")
		s = html.UnescapeString(s)
	}
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	return strings.TrimSpace(s)
}

// htmlText is plain text as an AIM client shows it.
func htmlText(s string) string {
	return "<HTML><BODY BGCOLOR=\"#ffffff\">" + html.EscapeString(s) + "</BODY></HTML>"
}

// uinOf is an account's ICQ number.
func (s *Server) uinOf(id int64) string { return strconv.FormatInt(int64(s.S.UINBase)+id, 10) }

// accountOfUIN is the account id of an ICQ number (0: none).
func (s *Server) accountOfUIN(uin string) int64 {
	n, err := strconv.ParseInt(uin, 10, 64)
	if err != nil || n <= int64(s.S.UINBase) {
		return 0
	}
	return n - int64(s.S.UINBase)
}
