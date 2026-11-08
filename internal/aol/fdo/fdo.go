// Package fdo is FDO91, the forms language of the classic AOL client (2.5
// to 4.0 and on): the atom streams the host sends to draw windows and run
// actions, and the client sends back with what a form holds.
//
// An atom stream is a run of atoms, each a protocol (man, mat, act...), an
// atom number and an argument of up to 32K bytes.  Ada32.dll, AOL's own
// compiler, writes every atom in the "full" style; the client also reads
// seven compact ones, and atoms too big for one packet as a large atom in
// pieces.  This package reads all of them (Decode, Decoder), writes them as
// Ada32 does (Encode, Packets), compiles FDO91 source to atom streams
// byte for byte as Ada32 does (Compile), and turns streams back into
// source that compiles to the same bytes (Decompile).
//
// The atom table is ADA.BIN's, the compiler's own list, as AtomForge
// dumped it, and the compiler is checked against AtomForge's corpus of what
// Ada32 made of 1,889 forms and 74,369 single atoms, and of 44 streams
// captured from a live client (see the tests).
//
// Strings are bytes: the client is a Windows program of the 1990s and
// reads Windows-1252.  Latin1 and Quote turn Go strings into those bytes
// and into source text.
package fdo

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Latin1 is s as the client's bytes: Windows-1252, with '?' for what it
// can't show.
func Latin1(s string) []byte {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r < 0x80 || r >= 0xA0 && r <= 0xFF:
			b = append(b, byte(r))
		default:
			if c, ok := cp1252[r]; ok {
				b = append(b, c)
			} else {
				b = append(b, '?')
			}
		}
	}
	return b
}

// FromLatin1 is the client's Windows-1252 bytes as a Go string.
func FromLatin1(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c < 0x80 || c >= 0xA0:
			sb.WriteRune(rune(c))
		default:
			if r := cp1252Runes[c-0x80]; r != 0 {
				sb.WriteRune(r)
			} else {
				sb.WriteRune(utf8.RuneError)
			}
		}
	}
	return sb.String()
}

// cp1252Runes are Windows-1252's characters at 0x80-0x9F.
var cp1252Runes = [32]rune{
	'€', 0, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0, 'Ž', 0,
	0, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0, 'ž', 'Ÿ',
}

var cp1252 = func() map[rune]byte {
	m := map[rune]byte{}
	for i, r := range cp1252Runes {
		if r != 0 {
			m[r] = byte(0x80 + i)
		}
	}
	return m
}()

// Quote is s as an FDO91 string, "..." with escapes, its text in the
// client's Windows-1252: for putting text that isn't known in advance (a
// screen name, a message) into source.
func Quote(s string) string { return quote(Latin1(s)) }

// QuoteBytes is b, the client's bytes, as an FDO91 string.
func QuoteBytes(b []byte) string { return quote(b) }

// Find is the first frame of the named atom, at the top level of frames.
func Find(frames []Frame, name string) (Frame, bool) {
	a, ok := Lookup(name)
	if !ok {
		return Frame{}, false
	}
	for _, f := range frames {
		if f.Proto == a.Proto && f.Num == a.Num {
			return f, true
		}
	}
	return Frame{}, false
}

// FindAll is every frame of the named atom, at the top level of frames:
// FindAll(frames, "de_data") is the fields a form sent, in order.
func FindAll(frames []Frame, name string) []Frame {
	a, ok := Lookup(name)
	if !ok {
		return nil
	}
	var out []Frame
	for _, f := range frames {
		if f.Proto == a.Proto && f.Num == a.Num {
			out = append(out, f)
		}
	}
	return out
}

// F makes a frame from an atom's name and its argument bytes; it panics on
// a name not in the table, so it is for names known when the program is
// written.
func F(name string, data ...byte) Frame {
	a := MustLookup(name)
	if data == nil {
		data = []byte{}
	}
	return Frame{a.Proto, a.Num, data}
}

// Number is n as an atom's argument: big-endian in as few bytes as it
// needs, as the compiler writes numbers.
func Number(n int64) []byte { return number(n) }

// GID is a global id as an atom's argument: 32-105 is GID(32, 105) and
// 1-0-1329 GID(1, 0, 1329).
func GID(parts ...int64) []byte {
	if len(parts) != 2 && len(parts) != 3 {
		panic(fmt.Sprintf("fdo: a global id has 2 or 3 parts, not %d", len(parts)))
	}
	return number(gidNumber(parts))
}
