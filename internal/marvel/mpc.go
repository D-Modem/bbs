package marvel

import (
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf16"
)

// MPC, the Marvel Protocol Client, is the RPC that runs on a pipe.  A
// call or reply is a host block:
//
//	Interface  Method  RequestID (VLI)  Parameters
//
// The interface byte is the selector this server gave the interface's
// IID in its discovery block; a reply echoes all three.  Parameters are
// tagged: the client's send tags have bit 7 clear, then its receive
// descriptors (bit 7 set, no data) say what it wants back.

// Interface bytes a one-way frame of a chunked parameter carries: more
// to come, or the last.
const (
	classChunkMore = 0xE6
	classChunkLast = 0xE7
)

// reply tags
const (
	tagByte      = 0x81
	tagWord      = 0x82
	tagDword     = 0x83
	tagVar       = 0x84
	tagPartial   = 0x85 // dynamic data, more to come
	tagComplete  = 0x86 // dynamic data to the end, for a single wait
	tagEndStatic = 0x87
	tagStreamEnd = 0x88 // dynamic data to the end, for an iterator
	tagError     = 0x8F
)

// putVLI appends a request id.  The bounds are strict, as the client's.
func putVLI(b []byte, v uint32) []byte {
	switch {
	case v < 0x3F:
		return append(b, byte(v))
	case v < 0x3FFF:
		return append(b, 0x80|byte(v>>8), byte(v))
	default:
		return binary.BigEndian.AppendUint32(b, v&0x3FFFFFFF|0xC0000000)
	}
}

// getVLI reads a request id: the value and its length (0: none).
func getVLI(b []byte) (uint32, int) {
	if len(b) == 0 {
		return 0, 0
	}
	switch b[0] & 0xC0 {
	case 0x00, 0x40:
		return uint32(b[0] & 0x3F), 1
	case 0x80:
		if len(b) < 2 {
			return 0, 0
		}
		return uint32(b[0]&0x3F)<<8 | uint32(b[1]), 2
	}
	if len(b) < 4 {
		return 0, 0
	}
	return binary.BigEndian.Uint32(b) & 0x3FFFFFFF, 4
}

// hostBlock is a call.  A one-way chunk frame has no method or request
// id: Method holds its stream id.
type hostBlock struct {
	Iface  byte
	Method byte
	ID     uint32
	Params []byte
}

func (h hostBlock) oneWay() bool { return h.Iface&0xE0 == 0xE0 }

func parseHostBlock(b []byte) (hostBlock, error) {
	if len(b) < 2 {
		return hostBlock{}, errors.New("short host block")
	}
	h := hostBlock{Iface: b[0], Method: b[1]}
	if h.oneWay() {
		h.Params = b[2:]
		return h, nil
	}
	id, n := getVLI(b[2:])
	if n == 0 {
		return h, errors.New("short host block")
	}
	h.ID, h.Params = id, b[2+n:]
	return h, nil
}

func (h hostBlock) bytes() []byte {
	b := putVLI([]byte{h.Iface, h.Method}, h.ID)
	return append(b, h.Params...)
}

// param is one of a call's send parameters.
type param struct {
	Tag    byte // 0x01 byte, 0x02 word, 0x03 dword, 0x04 bytes, 0x05 chunked reference
	Int    uint32
	Data   []byte
	Stream byte   // chunked: its stream
	Length uint32 // chunked: its length
}

// parseParams reads a call's send parameters and receive descriptors.
func parseParams(b []byte) (ps []param, recv []byte) {
	for len(b) > 0 {
		tag := b[0]
		b = b[1:]
		if tag&0x80 != 0 {
			recv = append(recv, tag)
			continue
		}
		switch tag {
		case 0x01:
			if len(b) < 1 {
				return
			}
			ps = append(ps, param{Tag: tag, Int: uint32(b[0])})
			b = b[1:]
		case 0x02:
			if len(b) < 2 {
				return
			}
			ps = append(ps, param{Tag: tag, Int: uint32(binary.LittleEndian.Uint16(b))})
			b = b[2:]
		case 0x03:
			if len(b) < 4 {
				return
			}
			ps = append(ps, param{Tag: tag, Int: binary.LittleEndian.Uint32(b)})
			b = b[4:]
		case 0x05, 0x45:
			if len(b) < 5 {
				return
			}
			ps = append(ps, param{Tag: 0x05, Stream: b[0], Length: binary.LittleEndian.Uint32(b[1:])})
			b = b[5:]
		case 0x04, 0x44:
			if len(b) < 1 {
				return
			}
			n, hl := int(b[0]&0x7F), 1
			if b[0]&0x80 == 0 {
				if len(b) < 2 {
					return
				}
				n, hl = int(b[0])<<8|int(b[1]), 2
			}
			b = b[hl:]
			if n > len(b) {
				n = len(b)
			}
			// 0x44 is compressed, which no shipped client sends
			ps = append(ps, param{Tag: 0x04, Data: b[:n]})
			b = b[n:]
		default:
			return
		}
	}
	return
}

// reply builds a reply's parameters.
type reply []byte

func (r reply) byte_(v byte) reply   { return append(r, tagByte, v) }
func (r reply) word(v uint16) reply  { return binary.LittleEndian.AppendUint16(append(r, tagWord), v) }
func (r reply) dword(v uint32) reply { return binary.LittleEndian.AppendUint32(append(r, tagDword), v) }
func (r reply) endStatic() reply     { return append(r, tagEndStatic) }
func (r reply) raw(tag byte, b []byte) reply {
	return append(append(r, tag), b...)
}

// bytes_ is a variable-length value: a one-byte length below 127 (bit 7
// set), else two bytes big-endian.
func (r reply) bytes_(b []byte) reply {
	r = append(r, tagVar)
	if len(b) < 0x7F {
		r = append(r, 0x80|byte(len(b)))
	} else {
		r = append(r, byte(len(b)>>8), byte(len(b)))
	}
	return append(r, b...)
}

// blob is the static part's end, then b as a single dynamic value.
func (r reply) blob(b []byte) reply { return r.endStatic().raw(tagComplete, b) }

// discovery is a service's first block on a new pipe: each IID it
// serves, with the interface byte it will be called by.
func discovery(ifaces []iface) []byte {
	var b []byte
	for _, i := range ifaces {
		b = append(b, i.iid[:]...)
		b = append(b, i.sel)
	}
	return hostBlock{Params: b}.bytes()
}

type iface struct {
	iid [16]byte
	sel byte
}

// mosIID is {00028Bxx-0000-0000-C000-000000000046} in a GUID's memory
// layout.
func mosIID(xx byte, sel byte) iface {
	return iface{iid: [16]byte{xx, 0x8B, 0x02, 0x00, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0, 0x46}, sel: sel}
}

// Property records (SVCPROP): the shape a node's properties travel in.
//
//	size:u32 count:u16 { type:u8 name:asciiz value }...

type prop struct {
	typ   byte
	name  string
	value []byte
}

// property value types this server sends
const (
	ptByte   = 0x01
	ptWord   = 0x02
	ptDword  = 0x03
	ptQword  = 0x04 // eight bytes
	ptAnsi   = 0x0A // flag-byte string, kept ANSI by the client
	ptWide   = 0x0B // flag-byte string, kept UTF-16
	ptQword2 = 0x0C // eight bytes inline (a FILETIME, a delegate's mnid)
	ptBlob   = 0x0E // u32 length, then bytes
	ptDwords = 0x10 // a counted array of dwords
)

func record(ps []prop) []byte {
	var body []byte
	for _, p := range ps {
		body = append(body, p.typ)
		body = append(append(body, p.name...), 0)
		body = append(body, p.value...)
	}
	out := make([]byte, 6, 6+len(body))
	binary.LittleEndian.PutUint32(out, uint32(6+len(body)))
	binary.LittleEndian.PutUint16(out[4:], uint16(len(ps)))
	return append(out, body...)
}

func u8(v byte) []byte    { return []byte{v} }
func u16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }
func u32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func u64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

// sz is a string property's value: flag 1 and the ANSI text, NUL ended.
// (The "empty" flag 2 makes the client drop the property, and every one
// after it, so an empty string goes as an empty ANSI one.)
func sz(s string) []byte {
	return append(append([]byte{1}, ansi(s)...), 0)
}

// ansi is s in Windows-1252, with what it can't hold as '?'.
func ansi(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r < 0x80:
			out = append(out, byte(r))
		case r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		default:
			if b, ok := cp1252[r]; ok {
				out = append(out, b)
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}

// unansi reads Windows-1252 text.
func unansi(b []byte) string {
	var sb strings.Builder
	for _, x := range b {
		switch {
		case x < 0x80 || x >= 0xA0:
			sb.WriteRune(rune(x))
		default:
			if r, ok := cp1252rev[x]; ok {
				sb.WriteRune(r)
			} else {
				sb.WriteRune('?')
			}
		}
	}
	return sb.String()
}

var cp1252 = map[rune]byte{'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87,
	'ˆ': 0x88, '‰': 0x89, 'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E, '‘': 0x91, '’': 0x92, '“': 0x93,
	'”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97, '˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B, 'œ': 0x9C,
	'ž': 0x9E, 'Ÿ': 0x9F}

var cp1252rev = func() map[byte]rune {
	m := map[byte]rune{}
	for r, b := range cp1252 {
		m[b] = r
	}
	return m
}()

// wide reads a NUL-ended UTF-16LE string.
func wide(b []byte) string {
	var u []uint16
	for i := 0; i+1 < len(b); i += 2 {
		c := binary.LittleEndian.Uint16(b[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// asciiz is the NUL-ended string at the start of b.
func asciiz(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	return unansi(b)
}
