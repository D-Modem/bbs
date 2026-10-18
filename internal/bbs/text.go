package bbs

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on what users write.
const (
	MaxNameLen     = 24
	MaxLocationLen = 40
	MaxSubjectLen  = 60
	MaxLineLen     = 79
	MaxBodyLines   = 200
	MaxBodyBytes   = 16 * 1024
	MinPasswordLen = 8
	MaxPasswordLen = 72 // bcrypt's limit
)

var namePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 ._-]*[A-Za-z0-9]$`)

// reserved names: what the menus take as commands, and names that could
// pass for the system
var reserved = map[string]bool{
	"new": true, "all": true, "sysop": true, "guest": true, "root": true,
	"admin": true, "system": true, "bbs": true, "off": true, "bye": true,
}

// Reserved reports whether name is kept from callers (the sysop may take
// one).
func Reserved(name string) bool { return reserved[strings.ToLower(name)] }

// ValidName checks a new user name: letters, digits and a few separators,
// starting with a letter, 2 to MaxNameLen long, no doubled spaces.
func ValidName(name string) string {
	switch {
	case len(name) < 2 || len(name) > MaxNameLen:
		return "A name is 2 to 24 characters long."
	case !namePattern.MatchString(name) || strings.Contains(name, "  "):
		return "Use letters, digits, spaces, dots, dashes and underscores; start with a letter."
	case reserved[strings.ToLower(name)]:
		return "That name is reserved."
	}
	return ""
}

// ValidPassword checks a new password.
func ValidPassword(pw, name string) string {
	switch {
	case len(pw) < MinPasswordLen:
		return "A password is at least 8 characters long."
	case len(pw) > MaxPasswordLen:
		return "A password is at most 72 characters long."
	case strings.EqualFold(pw, name):
		return "The password cannot be your name."
	}
	for _, r := range pw {
		if r < 0x20 || r == 0x7f {
			return "The password has a control character in it."
		}
	}
	return ""
}

// Clean makes one line of user text safe to show on any terminal: no
// control characters (escape sequences, 3270 orders once in EBCDIC, bell),
// no C1 controls, no bidirectional overrides, no invalid UTF-8; tabs become
// spaces.  The result is at most max runes, spaces trimmed.
func Clean(s string, max int) string { return strings.TrimSpace(CleanSpaced(s, max)) }

// CleanSpaced is Clean with the spaces kept: a line of a screen, its
// indentation and the spaces about its tags as they were.
func CleanSpaced(s string, max int) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(s) && n < max; {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == utf8.RuneError:
			// invalid UTF-8, or U+FFFD itself (go3270 stops encoding at it)
			r = '?'
		case r == '\t':
			r = ' '
		case !safeRune(r):
			continue
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// safeRune reports whether r may be shown as typed: not a control character,
// bidirectional override or mark, BOM, interlinear annotation, private use or
// surrogate.
func safeRune(r rune) bool {
	switch {
	case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0:
		return false
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069,
		r == 0x200e, r == 0x200f, r == 0x061c:
		// bidirectional overrides and marks: text that reads otherwise
		// than it is
		return false
	case r == 0xfeff, r >= 0xfff9 && r <= 0xfffb:
		return false
	case unicode.Is(unicode.Co, r), unicode.Is(unicode.Cs, r):
		return false
	}
	return true
}

// CleanBody cleans a message body line by line: each line as Clean (but
// keeping leading spaces), at most MaxBodyLines lines and MaxBodyBytes,
// trailing empty lines dropped.
func CleanBody(lines []string) string {
	var out []string
	size := 0
	for _, l := range lines {
		if len(out) == MaxBodyLines {
			break
		}
		indent := len(l) - len(strings.TrimLeft(l, " "))
		c := strings.Repeat(" ", min(indent, MaxLineLen)) + Clean(l, MaxLineLen)
		if size += len(c) + 1; size > MaxBodyBytes {
			break
		}
		out = append(out, strings.TrimRight(c, " "))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// Wrap breaks text (lines separated by \n) into lines of at most width
// runes, at spaces where it can.
func Wrap(text string, width int) []string {
	if width < 10 {
		width = 10
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		r := []rune(para)
		for len(r) > width {
			cut := width
			for i := width; i > width/2; i-- {
				if r[i] == ' ' {
					cut = i
					break
				}
			}
			out = append(out, strings.TrimRight(string(r[:cut]), " "))
			r = r[cut:]
			if len(r) > 0 && r[0] == ' ' {
				r = r[1:]
			}
		}
		out = append(out, string(r))
	}
	return out
}

// quotePrefix is a quoted line's mark ("> ", " XX> "), to carry on to the
// lines it wraps into.
var quotePrefix = regexp.MustCompile(`^ ?[A-Za-z]{0,3}> ?`)

// WrapBody is text typed in paragraphs (as on the web) as a message body's
// lines: long lines wrapped to MaxLineLen, rather than cut, each piece of
// a quoted line quoted again.
func WrapBody(text string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		l = strings.TrimRight(l, " \t\r")
		if utf8.RuneCountInString(l) <= MaxLineLen {
			out = append(out, l)
			continue
		}
		prefix := quotePrefix.FindString(l)
		rest := l[len(prefix):]
		if prefix != "" {
			prefix = strings.TrimRight(prefix, " ") + " "
			rest = strings.TrimLeft(rest, " ")
		}
		for _, w := range Wrap(rest, MaxLineLen-len(prefix)) {
			out = append(out, prefix+w)
		}
	}
	return out
}

// Fit pads or cuts s to exactly width runes.
func Fit(s string, width int) string {
	r := []rune(s)
	if len(r) > width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}

// Trunc cuts s to at most width runes.
func Trunc(s string, width int) string {
	r := []rune(s)
	if len(r) > width {
		return string(r[:width])
	}
	return s
}

// ReplySubject is "Re: subject", without piling up Re:s.
func ReplySubject(s string) string {
	if len(s) >= 3 && strings.EqualFold(s[:3], "re:") {
		return Trunc(s, MaxSubjectLen)
	}
	return Trunc("Re: "+s, MaxSubjectLen)
}

// Quote quotes a message for a reply: "> " lines, at most n of them.  The
// lines of a paragraph are joined before they are wrapped again, so that
// lines the editor wrapped do not leave a word on a line of its own.
func Quote(from, body string, n int) []string {
	var paras []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			paras = append(paras, strings.Join(cur, " "))
			cur = nil
		}
	}
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			flush()
			paras = append(paras, "")
		case strings.HasPrefix(t, ">") || strings.HasPrefix(l, " "):
			// quotes and indented lines keep their lines
			flush()
			paras = append(paras, l)
		default:
			cur = append(cur, t)
		}
	}
	flush()
	out := []string{from + " wrote:"}
	for _, l := range Wrap(strings.Join(paras, "\n"), MaxLineLen-2) {
		if len(out) > n {
			break
		}
		out = append(out, Trunc(">"+strings.TrimRight(" "+l, " "), MaxLineLen))
	}
	return append(out, "")
}
