package marvel

import (
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
)

// The Compose window may send a post's body as rich text, compressed or
// not; the BBS keeps plain text.  The compression is MAPI's ("LZFu",
// [MS-OXRTFCP]).

// lzfuDict is the dictionary's starting text.
const lzfuDict = `{\rtf1\ansi\mac\deff0\deftab720{\fonttbl;}{\f0\fnil \froman \fswiss \fmodern \fscript \fdecor MS Sans SerifSymbolArialTimes New RomanCourier{\colortbl\red0\green0\blue0` +
	"\r\n" + `\par \pard\plain\f0\fs20\b\i\u\tab\tx`

const (
	lzfuCompressed   = 0x75465A4C // "LZFu"
	lzfuUncompressed = 0x414C454D // "MELA"
)

var errRTFComp = errors.New("not compressed RTF")

// unlzfu decompresses compressed RTF.
func unlzfu(b []byte) ([]byte, error) {
	if len(b) < 16 {
		return nil, errRTFComp
	}
	size := int(binary.LittleEndian.Uint32(b))
	raw := int(binary.LittleEndian.Uint32(b[4:]))
	kind := binary.LittleEndian.Uint32(b[8:])
	data := b[16:]
	if size-12 < len(data) && size >= 12 {
		data = data[:size-12]
	}
	switch kind {
	case lzfuUncompressed:
		return data[:min(raw, len(data))], nil
	case lzfuCompressed:
	default:
		return nil, errRTFComp
	}
	var dict [4096]byte
	copy(dict[:], lzfuDict)
	w := len(lzfuDict)
	out := make([]byte, 0, raw)
	for i := 0; i < len(data); {
		flags := data[i]
		i++
		for bit := 0; bit < 8 && i < len(data); bit++ {
			if flags&(1<<bit) == 0 {
				out = append(out, data[i])
				dict[w] = data[i]
				w = (w + 1) % len(dict)
				i++
				continue
			}
			if i+1 >= len(data) {
				return out, nil
			}
			ref := int(data[i])<<8 | int(data[i+1])
			i += 2
			off, n := ref>>4, ref&0x0F+2
			if off == w {
				return out, nil // the end
			}
			for k := 0; k < n; k++ {
				c := dict[(off+k)%len(dict)]
				out = append(out, c)
				dict[w] = c
				w = (w + 1) % len(dict)
			}
		}
	}
	return out, nil
}

// rtfDestinations are the groups whose text isn't the document's.
var rtfDestinations = map[string]bool{
	"fonttbl": true, "colortbl": true, "stylesheet": true, "info": true, "pict": true,
	"object": true, "header": true, "footer": true, "headerl": true, "headerr": true,
	"footerl": true, "footerr": true, "field": false, "fldinst": true, "listtable": true,
	"listoverridetable": true, "rsidtbl": true, "generator": true, "xmlnstbl": true,
	"themedata": true, "datastore": true, "latentstyles": true, "filetbl": true,
	"revtbl": true, "pn": true,
}

// rtfText is the plain text of an RTF document.
func rtfText(b []byte) string {
	var sb strings.Builder
	type state struct {
		skip bool
		uc   int
	}
	st := state{uc: 1}
	var stack []state
	skipChars := 0
	emit := func(r rune) {
		if st.skip {
			return
		}
		if skipChars > 0 {
			skipChars--
			return
		}
		sb.WriteRune(r)
	}
	for i := 0; i < len(b); {
		c := b[i]
		switch c {
		case '{':
			stack = append(stack, st)
			i++
		case '}':
			if len(stack) > 0 {
				st = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
			i++
		case '\r', '\n':
			i++
		case '\\':
			i++
			if i >= len(b) {
				break
			}
			c = b[i]
			switch {
			case c == '\\' || c == '{' || c == '}':
				emit(rune(c))
				i++
			case c == '\'':
				if i+2 < len(b) {
					if v, err := strconv.ParseUint(string(b[i+1:i+3]), 16, 8); err == nil {
						for _, r := range unansi([]byte{byte(v)}) {
							emit(r)
						}
					}
				}
				i += 3
			case c == '*':
				st.skip = true
				i++
			case c == '~':
				emit(0xA0)
				i++
			case c == '\r' || c == '\n':
				emit('\n')
				i++
			case isAlpha(c):
				j := i
				for j < len(b) && isAlpha(b[j]) {
					j++
				}
				word := string(b[i:j])
				k := j
				if k < len(b) && (b[k] == '-' || isDigit(b[k])) {
					k++
					for k < len(b) && isDigit(b[k]) {
						k++
					}
				}
				arg, hasArg := 0, k > j
				if hasArg {
					arg, _ = strconv.Atoi(string(b[j:k]))
				}
				if k < len(b) && b[k] == ' ' {
					k++
				}
				i = k
				switch {
				case rtfDestinations[word]:
					st.skip = true
				case word == "par" || word == "line" || word == "sect" || word == "page":
					emit('\n')
				case word == "tab":
					emit('\t')
				case word == "emdash":
					emit('—')
				case word == "endash":
					emit('–')
				case word == "lquote":
					emit('‘')
				case word == "rquote":
					emit('’')
				case word == "ldblquote":
					emit('“')
				case word == "rdblquote":
					emit('”')
				case word == "bullet":
					emit('•')
				case word == "uc" && hasArg:
					st.uc = arg
				case word == "u" && hasArg:
					if arg < 0 {
						arg += 65536
					}
					emit(rune(arg))
					if !st.skip {
						skipChars = st.uc
					}
				}
			default:
				i++
			}
		default:
			emit([]rune(unansi([]byte{c}))[0])
			i++
		}
	}
	return sb.String()
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
