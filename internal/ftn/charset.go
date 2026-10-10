package ftn

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"
)

// charsets are CHRS names (FTS-5003) as x/text knows them.
var charsets = map[string]encoding.Encoding{
	"CP437": charmap.CodePage437, "IBMPC": charmap.CodePage437, "PC-8": charmap.CodePage437,
	"CP850": charmap.CodePage850, "CP852": charmap.CodePage852, "CP855": charmap.CodePage855,
	"CP858": charmap.CodePage858, "CP860": charmap.CodePage860, "CP862": charmap.CodePage862,
	"CP863": charmap.CodePage863, "CP865": charmap.CodePage865, "CP866": charmap.CodePage866,
	"+7_FIDO": charmap.CodePage866, "RUSSIAN": charmap.CodePage866,
	"LATIN-1": charmap.ISO8859_1, "LATIN-2": charmap.ISO8859_2, "LATIN-9": charmap.ISO8859_15,
	"CP1250": charmap.Windows1250, "CP1251": charmap.Windows1251, "CP1252": charmap.Windows1252,
	"KOI8-R": charmap.KOI8R, "KOI8-U": charmap.KOI8U, "MAC": charmap.Macintosh,
}

// Charset is the encoding of a message, by its CHRS (or CODEPAGE) kludge:
// nil for UTF-8 or plain ASCII, CP437 if it says nothing (what most of
// FidoNet has written).
func (m *Message) Charset() encoding.Encoding {
	chrs := m.Kludge("CHRS")
	if chrs == "" {
		chrs = m.Kludge("CHARSET")
	}
	if chrs == "" {
		if cp := strings.TrimSpace(m.Kludge("CODEPAGE")); cp != "" {
			chrs = "CP" + cp
		}
	}
	name := strings.ToUpper(strings.Fields(chrs + " x")[0])
	switch name {
	case "UTF-8", "UTF8", "ASCII", "US-ASCII":
		return nil
	case "X":
		// nothing said: valid UTF-8 beyond ASCII is surely UTF-8
		return nil
	}
	if e, ok := charsets[name]; ok {
		return e
	}
	if e, err := htmlindex.Get(name); err == nil {
		return e
	}
	return charmap.CodePage437
}

// Decoder turns the message's texts into UTF-8.
func (m *Message) Decoder() func(string) string {
	enc := m.Charset()
	said := m.Kludge("CHRS") != "" || m.Kludge("CHARSET") != "" || m.Kludge("CODEPAGE") != ""
	return func(s string) string {
		if enc == nil {
			if said || utf8.ValidString(s) {
				return strings.ToValidUTF8(s, "?")
			}
			enc = charmap.CodePage437
		}
		out, err := enc.NewDecoder().String(s)
		if err != nil {
			return strings.ToValidUTF8(s, "?")
		}
		return out
	}
}

// Encoder is how messages are written: CP437 (CHRS "CP437 2", what every
// FidoNet reader shows), or UTF-8 ("UTF-8 4").  Characters CP437 lacks
// become '?'.
type Encoder struct{ UTF8 bool }

// CHRS is the kludge's value.
func (e Encoder) CHRS() string {
	if e.UTF8 {
		return "UTF-8 4"
	}
	return "CP437 2"
}

// String encodes s.
func (e Encoder) String(s string) string {
	if e.UTF8 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if c, ok := charmap.CodePage437.EncodeRune(r); ok && (r >= 0x20 || r == '\t') {
			b.WriteByte(c)
		} else if t, ok := nearest[r]; ok {
			b.WriteString(t)
		} else {
			b.WriteByte('?')
		}
	}
	return b.String()
}

// nearest are what CP437 lacks of common typography, as near as it comes.
var nearest = map[rune]string{
	'\u2013': "-", '\u2014': "-", '\u2018': "'", '\u2019': "'", '\u201a': ",",
	'\u201c': "\"", '\u201d': "\"", '\u201e': "\"", '\u2026': "...", '\u2022': "*",
	'\u00a9': "(c)", '\u00ae': "(R)", '\u2122': "(TM)", '\u20ac': "EUR", '\u00d7': "x",
}
