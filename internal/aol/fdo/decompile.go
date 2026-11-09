package fdo

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Decompile turns an atom stream back into FDO91 source.  Compiling the
// text gives the same bytes back: where an argument has no nicer form that
// does, it is shown as hex.
func Decompile(b []byte) (string, error) {
	frames, err := Decode(b)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	writeFrames(&sb, frames, 0)
	return sb.String(), nil
}

// DecompileFrames is Decompile for frames already decoded.
func DecompileFrames(frames []Frame) string {
	var sb strings.Builder
	writeFrames(&sb, frames, 0)
	return sb.String()
}

func writeFrames(sb *strings.Builder, frames []Frame, depth int) {
	writeStream(sb, frames, depth, false)
}

// writeStream writes atoms a line each, indented by objects and streams as
// Ada32 lays them out; a nested stream goes in < and >, four spaces in.
func writeStream(sb *strings.Builder, frames []Frame, depth int, nested bool) {
	for _, f := range frames {
		a, known := f.Atom()
		var fl Flags
		if known && a.Type != TypeStream && (a.Proto == ProtoUNI || a.Flags&FlagEOS == 0) &&
			!(nested && a.Proto == ProtoUNI) {
			fl = a.Flags
		}
		if fl&FlagOutdent != 0 && depth > 0 {
			depth--
		}
		indent := strings.Repeat("  ", depth)
		sb.WriteString(indent)
		vals := frameValues(f)
		if !known {
			sb.WriteString("the_unknown")
		} else {
			sb.WriteString(a.Name)
		}
		if len(vals) == 1 && vals[0].Kind == KindStream {
			inner := strings.Repeat("  ", depth+2)
			sb.WriteString("\n" + inner + "<\n")
			writeStream(sb, vals[0].streamFrames, depth+2, true)
			sb.WriteString(inner + ">\n")
		} else {
			if len(vals) > 0 {
				sb.WriteString(" <")
				if known && a.Type == TypeVarString && len(vals) == 2 {
					// as Ada32 writes it: <A,"text">
					writeValue(sb, vals[0])
					sb.WriteString(",")
					writeValue(sb, vals[1])
				} else {
					writeValues(sb, vals)
				}
				sb.WriteString(">")
			}
			sb.WriteString("\n")
		}
		if fl&FlagIndent != 0 {
			depth++
		}
	}
}

func writeValues(sb *strings.Builder, vals []Value) {
	for i, v := range vals {
		if i > 0 {
			sb.WriteString(", ")
		}
		writeValue(sb, v)
	}
}

func writeValue(sb *strings.Builder, v Value) {
	switch v.Kind {
	case KindNum:
		sb.WriteString(strconv.FormatInt(v.Num, 10))
	case KindHex:
		for i, c := range v.Bytes {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(sb, "%02xx", c)
		}
	case KindString:
		sb.WriteString(quote(v.Bytes))
	case KindWord:
		sb.WriteString(v.Text)
	case KindGID:
		for i, n := range v.GID {
			if i > 0 {
				sb.WriteByte('-')
			}
			sb.WriteString(strconv.FormatInt(n, 10))
		}
	case KindPipe:
		for i, p := range v.Parts {
			if i > 0 {
				sb.WriteString(" | ")
			}
			writeValue(sb, p)
		}
	}
}

// quote writes b as a string: printable ASCII as it is, the rest escaped.
func quote(b []byte) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, c := range b {
		switch {
		case c == '\\':
			sb.WriteString(`\\`)
		case c == '"':
			sb.WriteString(`\"`)
		case c == '\r':
			sb.WriteString(`\r`)
		case c == '\n':
			sb.WriteString(`\n`)
		case c == '\t':
			sb.WriteString(`\t`)
		case c >= 0x20 && c < 0x7F:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, `\x%02x`, c)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// hexValue is data as hex bytes, which always compile back to themselves.
func hexValue(d []byte) []Value { return []Value{{Kind: KindHex, Bytes: d}} }

// frameValues is the argument the decompiler shows for f: its nicest form
// that compiles back to f's data.
func frameValues(f Frame) []Value {
	a, ok := f.Atom()
	if !ok {
		vals := []Value{{Kind: KindNum, Num: int64(f.Proto)}, {Kind: KindNum, Num: int64(f.Num)}}
		if len(f.Data) > 0 {
			vals = append(vals, Value{Kind: KindHex, Bytes: f.Data})
		}
		return vals
	}
	if len(f.Data) == 0 {
		return nil
	}
	for _, vals := range candidates(a, f.Data) {
		if len(vals) == 1 && vals[0].Kind == KindStream {
			return vals
		}
		if b, err := encodeArgs(a, vals); err == nil && bytes.Equal(b, f.Data) {
			return vals
		}
	}
	return hexValue(f.Data)
}
