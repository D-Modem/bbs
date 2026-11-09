package marvel

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"time"
	"unicode/utf16"
)

// MAPI properties, as Exchange's MSN providers (the mail transport and the
// address book) carry them.  A property is a tag, its low 16 bits the
// value's type.

const (
	ptI2      = 0x0002
	ptLong    = 0x0003
	ptR4      = 0x0004
	ptDouble  = 0x0005
	ptCurr    = 0x0006
	ptAppTime = 0x0007
	ptBool    = 0x000B
	ptI8      = 0x0014
	ptString8 = 0x001E
	ptUnicode = 0x001F
	ptSysTime = 0x0040
	ptCLSID   = 0x0048
	ptBinary  = 0x0102
)

// the tags this server reads or writes
const (
	tagObjectType              = 0x0FFE0003
	tagMsgStatus               = 0x0E170003
	tagMessageFlags            = 0x0E070003
	tagMessageClass            = 0x001A001E
	tagImportance              = 0x00170003
	tagSentReprName            = 0x0042001E
	tagSentReprAddrType        = 0x0064001E
	tagSentReprEmail           = 0x0065001E
	tagDisplayTo               = 0x0E04001E
	tagSensitivity             = 0x00360003
	tagHasAttach               = 0x0E1B000B
	tagSubject                 = 0x0037001E
	tagSubjectW                = 0x0037001F
	tagDeliveryTime            = 0x0E060040
	tagMessageSize             = 0x0E080003
	tagBody                    = 0x1000001E
	tagBodyW                   = 0x1000001F
	tagRTFCompressed           = 0x10090102
	tagClientSubmitTime        = 0x00390040
	tagSenderName              = 0x0C1A001E
	tagSenderAddrType          = 0x0C1E001E
	tagSenderEmail             = 0x0C1F001E
	tagSenderSearchKey         = 0x0C1D0102
	tagRecipientType           = 0x0C150003
	tagDisplayName             = 0x3001001E
	tagDisplayNameW            = 0x3001001F
	tagAddrType                = 0x3002001E
	tagEmailAddress            = 0x3003001E
	tagEmailAddressW           = 0x3003001F
	tagDisplayType             = 0x39000003
	mapiMessage                = 5
	mapiMailUser               = 6
	msgFlagRead                = 0x01
	msgFlagUnmodified          = 0x02
	mapiNotFound               = 0x8004010F
	mapiFail                   = 0x80004005
	addrTypeMSN                = "MSN"
	mapiTo                     = 1
	importanceNormal           = 1
	propertyNamedIDs    uint32 = 0x8000
)

// providerUID is the MSN providers' MAPIUID, in every entry id.
var providerUID = []byte{0x1b, 0xec, 0xba, 0x6c, 0x5f, 0x92, 0x10, 0x1b, 0xb9, 0x3d, 0x00, 0x00, 0x0b, 0x70, 0x34, 0x6a}

// memberEntryID is a member's address-book entry id (and the mail
// transport's CONNINFO): 184 bytes, keyed on the member name.
func memberEntryID(display, member string) []byte {
	b := make([]byte, 0xB8)
	copy(b[0x04:], providerUID)
	binary.LittleEndian.PutUint32(b[0x14:], 2)
	binary.LittleEndian.PutUint32(b[0x18:], 1)
	copy(b[0x1C:0x1C+0x5A], ansi(display))
	copy(b[0x77:0x77+0x40], ansi(member))
	return b
}

type mprop struct {
	tag uint32
	v   any // uint32, uint16, bool, string, []byte, uint64, time.Time
}

// putValue appends a value as the transport carries it (no length but
// a binary's; strings NUL-ended).
func putValue(b []byte, typ uint32, v any) []byte {
	switch typ {
	case ptI2:
		return binary.LittleEndian.AppendUint16(b, uint16(asUint(v)))
	case ptBool:
		x := uint16(0)
		if t, ok := v.(bool); ok && t || asUint(v) != 0 {
			x = 1
		}
		return binary.LittleEndian.AppendUint16(b, x)
	case ptLong, ptR4:
		return binary.LittleEndian.AppendUint32(b, uint32(asUint(v)))
	case ptDouble, ptCurr, ptAppTime, ptI8:
		return binary.LittleEndian.AppendUint64(b, asUint(v))
	case ptSysTime:
		if t, ok := v.(time.Time); ok {
			return binary.LittleEndian.AppendUint64(b, filetime(t))
		}
		return binary.LittleEndian.AppendUint64(b, asUint(v))
	case ptString8:
		s, _ := v.(string)
		return append(append(b, ansi(s)...), 0)
	case ptUnicode:
		s, _ := v.(string)
		for _, u := range utf16.Encode([]rune(s)) {
			b = binary.LittleEndian.AppendUint16(b, u)
		}
		return append(b, 0, 0)
	case ptBinary:
		d, _ := v.([]byte)
		return append(binary.LittleEndian.AppendUint32(b, uint32(len(d))), d...)
	case ptCLSID:
		d, _ := v.([]byte)
		return append(b, append(d, make([]byte, 16)...)[:16]...)
	}
	return b
}

func asUint(v any) uint64 {
	switch x := v.(type) {
	case uint32:
		return uint64(x)
	case uint16:
		return uint64(x)
	case int:
		return uint64(x)
	case uint64:
		return x
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

// propList is [count][tag value]...
func propList(ps []mprop) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(ps)))
	for _, p := range ps {
		b = binary.LittleEndian.AppendUint32(b, p.tag)
		b = putValue(b, p.tag&0xFFFF, p.v)
	}
	return b
}

var errMAPI = errors.New("a MAPI property list cut short or of a type it can't hold")

// mapiReader reads the transport's serialised properties.
type mapiReader struct {
	b   []byte
	pos int
}

func (r *mapiReader) take(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.b) {
		return nil, errMAPI
	}
	v := r.b[r.pos : r.pos+n]
	r.pos += n
	return v, nil
}

func (r *mapiReader) u32() (uint32, error) {
	v, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(v), nil
}

// value reads one value of a type: strings as Go strings, the rest as
// raw bytes.
func (r *mapiReader) value(typ uint32) (any, error) {
	switch typ {
	case ptI2, ptBool:
		return r.take(2)
	case ptLong, ptR4:
		return r.take(4)
	case ptDouble, ptCurr, ptAppTime, ptI8, ptSysTime:
		return r.take(8)
	case ptCLSID:
		return r.take(16)
	case ptString8:
		i := bytes.IndexByte(r.b[r.pos:], 0)
		if i < 0 {
			return nil, errMAPI
		}
		s := unansi(r.b[r.pos : r.pos+i])
		r.pos += i + 1
		return s, nil
	case ptUnicode:
		for i := r.pos; i+1 < len(r.b); i += 2 {
			if r.b[i] == 0 && r.b[i+1] == 0 {
				s := wide(r.b[r.pos : i+2])
				r.pos = i + 2
				return s, nil
			}
		}
		return nil, errMAPI
	case ptBinary:
		n, err := r.u32()
		if err != nil {
			return nil, err
		}
		return r.take(int(n))
	}
	return nil, errMAPI
}

// props reads a property list into a map by tag (a named property's by
// the type it carries, under its tag as sent).
func (r *mapiReader) props() (map[uint32]any, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	out := map[uint32]any{}
	for i := uint32(0); i < n; i++ {
		tag, err := r.u32()
		if err != nil {
			return nil, err
		}
		typ := tag & 0xFFFF
		if tag>>16 >= propertyNamedIDs {
			// reserved, the name's GUID, its kind and the name or number
			if _, err := r.take(4 + 16); err != nil {
				return nil, err
			}
			kind, err := r.u32()
			if err != nil {
				return nil, err
			}
			if kind == 0 {
				if _, err := r.take(4); err != nil {
					return nil, err
				}
			} else if _, err := r.value(ptUnicode); err != nil {
				return nil, err
			}
			t, err := r.take(2)
			if err != nil {
				return nil, err
			}
			typ = uint32(binary.LittleEndian.Uint16(t))
		}
		v, err := r.value(typ)
		if err != nil {
			return nil, err
		}
		out[tag] = v
	}
	return out, nil
}

func str(m map[uint32]any, tags ...uint32) string {
	for _, t := range tags {
		if s, ok := m[t].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func dwordOf(m map[uint32]any, tag uint32, def uint32) uint32 {
	if b, ok := m[tag].([]byte); ok && len(b) == 4 {
		return binary.LittleEndian.Uint32(b)
	}
	return def
}

// mszip compresses as MSN's MOSMUTIL does: "CK" and a raw deflate stream.
func mszip(b []byte) []byte {
	var out bytes.Buffer
	out.WriteString("CK")
	w, _ := flate.NewWriter(&out, flate.BestCompression)
	w.Write(b)
	w.Close()
	return out.Bytes()
}

// zeroValue is a value of a tag's type with nothing in it, as the address
// book must send for a property it has nothing for.
func zeroValue(tag uint32) any {
	switch tag & 0xFFFF {
	case ptString8, ptUnicode:
		return ""
	case ptBinary, ptCLSID:
		return []byte(nil)
	}
	return uint32(0)
}
