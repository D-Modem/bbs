package imageboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
)

// tripcode is a password's tripcode, as 2channel made them and every
// image board since: the password in Shift JIS, HTML-escaped, its salt
// its second and third characters, crypt(3)'s last ten.
func tripcode(pw string) string {
	b, err := encoding.ReplaceUnsupported(japanese.ShiftJIS.NewEncoder()).Bytes([]byte(pw))
	if err != nil {
		b = []byte(pw)
	}
	b = []byte(strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;").Replace(string(b)))
	salt := []byte(string(b) + "H..")[1:3]
	for i, c := range salt {
		switch {
		case c < '.' || c > 'z':
			salt[i] = '.'
		case strings.IndexByte(":;<=>?@[\\]^_`", c) >= 0:
			salt[i] = "ABCDEFGabcdef"[strings.IndexByte(":;<=>?@[\\]^_`", c)]
		}
	}
	c := desCrypt(b, string(salt))
	return c[len(c)-10:]
}

// secureTrip is a secure tripcode: the password and the board's secret,
// so that it cannot be found by trying passwords elsewhere.
func secureTrip(pw string, secret []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(pw))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))[:10]
}

// splitName splits what was typed as a name into the name and its
// tripcode: "Name#password" (!tripcode), "Name##password" (!!secure),
// "Name#password##secret" (both).
func splitName(typed string, secret []byte) (name, trip string) {
	name, rest, ok := strings.Cut(typed, "#")
	if !ok {
		return strings.TrimSpace(typed), ""
	}
	pw, sec := rest, ""
	if strings.HasPrefix(rest, "#") {
		pw, sec = "", rest[1:]
	} else if i := strings.Index(rest, "##"); i >= 0 {
		pw, sec = rest[:i], rest[i+2:]
	}
	if pw != "" {
		trip = "!" + tripcode(pw)
	}
	if sec != "" {
		trip += "!!" + secureTrip(sec, secret)
	}
	return strings.TrimSpace(name), trip
}
