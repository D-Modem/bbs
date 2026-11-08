package fdo

import (
	"errors"
	"fmt"
)

// Frame is one atom as it travels: its protocol, number and argument bytes.
type Frame struct {
	Proto uint8
	Num   uint8
	Data  []byte
}

// Atom is the frame's table entry, if it has one.
func (f Frame) Atom() (*Atom, bool) { return LookupNum(f.Proto, f.Num) }

// Name is the frame's atom name, or "proto:num" for one not in the table.
func (f Frame) Name() string {
	if a, ok := f.Atom(); ok {
		return a.Name
	}
	return fmt.Sprintf("%d:%d", f.Proto, f.Num)
}

func (f Frame) String() string { return fmt.Sprintf("%s % x", f.Name(), f.Data) }

// The eight encoding styles: the top three bits of an atom's first byte.
const (
	styleFull    = 0 // [proto] [atom] [len] [data]
	styleLength  = 1 // [proto] [len<<5|atom] [data]: 0-7 bytes, atom < 32
	styleData    = 2 // [proto] [value<<5|atom]: one byte of 0-7, atom < 32
	styleAtom    = 3 // [atom]: no data, the last atom's protocol
	styleCurrent = 4 // [atom] [len] [data]: the last atom's protocol
	styleZero    = 5 // [atom]: data is one 0 byte
	styleOne     = 6 // [atom]: data is one 1 byte
	stylePrefix  = 7 // [111PPAAS] then one of the others: protocols 32-127
)

// The universal protocol's large-atom atoms: an atom too big for one
// packet goes as uni_start_large_atom <proto, atom, length>, zero or more
// uni_large_atom_segment <data>, then uni_end_large_atom <data>.
const (
	atomStartLarge   = 4
	atomLargeSegment = 5
	atomEndLarge     = 6
)

// MaxData is the most argument bytes one atom can carry (a two-byte length
// has 15 bits); a bigger atom must be sent as a large atom.
const MaxData = 0x7FFF

var (
	// ErrShort is a stream that ends inside an atom.
	ErrShort = errors.New("fdo: atom stream ends inside an atom")
	// ErrLargeAtom is a large-atom segment with no uni_start_large_atom.
	ErrLargeAtom = errors.New("fdo: large atom segment without a start")
)

// Decoder reads atom streams, keeping what carries from one packet to the
// next: the protocol the compact styles assume and a large atom being
// collected.  The zero value is ready; keep one per stream id.
type Decoder struct {
	proto  uint8
	large  bool
	lProto uint8
	lNum   uint8
	lData  []byte
}

// Decode reads one whole atom stream.
func Decode(b []byte) ([]Frame, error) {
	var d Decoder
	f, err := d.Feed(b)
	if err == nil && d.large {
		err = fmt.Errorf("fdo: large atom %d:%d not ended", d.lProto, d.lNum)
	}
	return f, err
}

// Feed decodes the atoms in b (a packet's worth: whole atoms) and returns
// them.  A large atom comes back as one frame once its end arrives.
func (d *Decoder) Feed(b []byte) ([]Frame, error) {
	var out []Frame
	for len(b) > 0 {
		f, n, err := d.next(b)
		if err != nil {
			return out, err
		}
		b = b[n:]
		if f.Proto == ProtoUNI {
			switch f.Num {
			case atomStartLarge:
				if len(f.Data) < 2 {
					return out, fmt.Errorf("fdo: uni_start_large_atom needs a protocol and an atom")
				}
				d.large, d.lProto, d.lNum, d.lData = true, f.Data[0], f.Data[1], nil
				continue
			case atomLargeSegment, atomEndLarge:
				if !d.large {
					return out, ErrLargeAtom
				}
				d.lData = append(d.lData, f.Data...)
				if f.Num == atomEndLarge {
					out = append(out, Frame{d.lProto, d.lNum, d.lData})
					d.large, d.lData = false, nil
				}
				continue
			}
		}
		out = append(out, f)
	}
	return out, nil
}

// next decodes the atom at the start of b and returns it and its size.
func (d *Decoder) next(b []byte) (Frame, int, error) {
	p := 0
	get := func() (byte, error) {
		if p >= len(b) {
			return 0, ErrShort
		}
		c := b[p]
		p++
		return c, nil
	}
	length := func() (int, error) {
		c, err := get()
		if err != nil || c&0x80 == 0 {
			return int(c), err
		}
		lo, err := get()
		return int(c&0x7F)<<8 | int(lo), err
	}
	data := func(n int) ([]byte, error) {
		if len(b)-p < n {
			return nil, ErrShort
		}
		v := append([]byte(nil), b[p:p+n]...)
		p += n
		return v, nil
	}
	first, _ := get()
	style, low := first>>5, first&0x1F
	var f Frame
	var err error
	switch style {
	case styleFull:
		f.Proto = low
		if f.Num, err = get(); err == nil {
			var n int
			if n, err = length(); err == nil {
				f.Data, err = data(n)
			}
		}
	case styleLength, styleData:
		f.Proto = low
		var c byte
		if c, err = get(); err == nil {
			f.Num = c & 0x1F
			if style == styleData {
				f.Data = []byte{c >> 5}
			} else {
				f.Data, err = data(int(c >> 5))
			}
		}
	case styleAtom, styleZero, styleOne, styleCurrent:
		f.Proto, f.Num = d.proto, low
		switch style {
		case styleZero:
			f.Data = []byte{0}
		case styleOne:
			f.Data = []byte{1}
		case styleCurrent:
			var n int
			if n, err = length(); err == nil {
				f.Data, err = data(n)
			}
		}
	case stylePrefix:
		// [111 PP AA S]: PP the protocol's bits 6-5, AA the atom's 7-6
		var c, a byte
		if c, err = get(); err != nil {
			break
		}
		if a, err = get(); err != nil {
			break
		}
		f.Proto = (first>>3&3)<<5 | c&0x1F
		hi := (first >> 1 & 3) << 6
		switch c >> 5 {
		case styleFull, styleCurrent:
			f.Num = a
			var n int
			if n, err = length(); err == nil {
				f.Data, err = data(n)
			}
		case styleLength:
			f.Num = hi | a&0x1F
			f.Data, err = data(int(a >> 5))
		case styleData:
			f.Num = hi | a&0x1F
			f.Data = []byte{a >> 5}
		case styleAtom:
			f.Num = hi | a&0x3F
		case styleZero:
			f.Num, f.Data = hi|a&0x3F, []byte{0}
		case styleOne:
			f.Num, f.Data = hi|a&0x3F, []byte{1}
		default:
			err = fmt.Errorf("fdo: prefix byte %#02x then another prefix", first)
		}
	}
	if err != nil {
		return f, p, err
	}
	if f.Data == nil {
		f.Data = []byte{}
	}
	d.proto = f.Proto
	return f, p, nil
}

// AppendFrame appends f in the full style, as AOL's compiler writes every
// atom.  Its data must be at most MaxData bytes.
func AppendFrame(b []byte, f Frame) []byte {
	if f.Proto >= 32 {
		b = append(b, stylePrefix<<5|(f.Proto&0x60)>>2)
	}
	b = append(b, f.Proto&0x1F, f.Num)
	b = appendLength(b, len(f.Data))
	return append(b, f.Data...)
}

func appendLength(b []byte, n int) []byte {
	if n < 0x80 {
		return append(b, byte(n))
	}
	return append(b, 0x80|byte(n>>8), byte(n))
}

// frameSize is f's size in the full style.
func frameSize(f Frame) int {
	n := 3 + len(f.Data)
	if len(f.Data) >= 0x80 {
		n++
	}
	if f.Proto >= 32 {
		n++
	}
	return n
}

// Encode writes frames in the full style, byte for byte as Ada32.dll does.
// A frame bigger than MaxData goes as a large atom.
func Encode(frames []Frame) []byte {
	var b []byte
	for _, f := range frames {
		if len(f.Data) > MaxData {
			for _, s := range splitLarge(f, MaxData) {
				b = AppendFrame(b, s)
			}
			continue
		}
		b = AppendFrame(b, f)
	}
	return b
}

// EncodeCompact writes frames in the shortest styles.  The client reads
// them, but AOL's tools wrote the full style; Encode is the one to match.
func EncodeCompact(frames []Frame) []byte {
	var b []byte
	cur := uint8(0)
	for _, f := range frames {
		n, d := f.Num, f.Data
		switch {
		case f.Proto >= 32 || len(d) > MaxData:
			b = append(b, Encode([]Frame{f})...)
			if len(d) > MaxData {
				cur = ProtoUNI // the decoder last saw uni_end_large_atom
				continue
			}
		case n < 32 && f.Proto == cur && len(d) == 0:
			b = append(b, styleAtom<<5|n)
		case n < 32 && f.Proto == cur && len(d) == 1 && d[0] == 0:
			b = append(b, styleZero<<5|n)
		case n < 32 && f.Proto == cur && len(d) == 1 && d[0] == 1:
			b = append(b, styleOne<<5|n)
		case n < 32 && len(d) == 1 && d[0] < 8:
			b = append(b, styleData<<5|f.Proto, d[0]<<5|n)
		case n < 32 && len(d) >= 1 && len(d) < 8:
			b = append(b, styleLength<<5|f.Proto, byte(len(d))<<5|n)
			b = append(b, d...)
		case n < 32 && f.Proto == cur:
			b = append(b, styleCurrent<<5|n)
			b = appendLength(b, len(d))
			b = append(b, d...)
		default:
			b = AppendFrame(b, f)
		}
		cur = f.Proto
	}
	return b
}

// splitLarge breaks f into uni_start_large_atom, segments and
// uni_end_large_atom, each segment's data at most seg bytes.
func splitLarge(f Frame, seg int) []Frame {
	start := []byte{f.Proto, f.Num}
	if n := len(f.Data); n < 0x80 {
		start = append(start, byte(n))
	} else {
		start = append(start, byte(n>>8), byte(n))
	}
	out := []Frame{{ProtoUNI, atomStartLarge, start}}
	d := f.Data
	for len(d) > seg {
		out = append(out, Frame{ProtoUNI, atomLargeSegment, d[:seg]})
		d = d[seg:]
	}
	return append(out, Frame{ProtoUNI, atomEndLarge, d})
}

// Packets encodes frames (full style) into pieces of at most max bytes,
// each holding whole atoms, for P3 data packets: the AOL 3.0 client's own
// limit is 119 bytes of packet data, and the stream's token and id take 4
// of them.  An atom that doesn't fit one piece goes as a large atom, its
// start and each segment in a piece of its own.  max must be at least 8.
func Packets(frames []Frame, max int) [][]byte {
	if max < 8 {
		panic("fdo: packet size too small")
	}
	var out [][]byte
	var cur []byte
	flush := func() {
		if len(cur) > 0 {
			out = append(out, cur)
			cur = nil
		}
	}
	for _, f := range frames {
		n := frameSize(f)
		if n > max {
			flush()
			segs := splitLarge(f, max-4)
			for i, s := range segs {
				cur = AppendFrame(cur, s)
				if i < len(segs)-1 {
					flush()
				}
			}
			continue
		}
		if len(cur)+n > max {
			flush()
		}
		cur = AppendFrame(cur, f)
	}
	flush()
	return out
}
