package archive

import (
	"bytes"
	"io"
	"strings"
)

const arjMaxHeader = 2600 // ARJ's own limit on a basic header

// arjHeader reads the header at off: its basic header, and where what
// follows it (its data) begins; nil at the end of the archive.
func arjHeader(r io.ReaderAt, size, off int64) (h []byte, next int64, err error) {
	id, err := readAt(r, size, off, 4)
	if err != nil {
		return nil, 0, err
	}
	if id[0] != 0x60 || id[1] != 0xEA {
		return nil, 0, ErrCorrupt
	}
	n := int64(le16(id[2:]))
	if n == 0 {
		return nil, 0, nil
	}
	if n < 30 || n > arjMaxHeader {
		return nil, 0, ErrCorrupt
	}
	h, err = readAt(r, size, off+4, n+4)
	if err != nil {
		return nil, 0, err
	}
	if crc32sum(h[:n]) != le32(h[n:]) {
		return nil, 0, ErrCorrupt
	}
	pos := off + 4 + n + 4
	for { // the extended headers: skipped
		x, err := readAt(r, size, pos, 2)
		if err != nil {
			return nil, 0, err
		}
		xn := int64(le16(x))
		pos += 2
		if xn == 0 {
			break
		}
		pos += xn + 4
	}
	return h[:n], pos, nil
}

// walkARJ reads an ARJ archive: methods 0 (stored) to 4.
func walkARJ(r io.ReaderAt, size int64, fn func(e Entry, open Opener) bool) error {
	main, off, err := arjHeader(r, size, 0)
	if err != nil {
		return err
	}
	if main == nil || main[6] != 2 {
		return ErrCorrupt // the first is the archive's own header
	}
	for {
		h, data, err := arjHeader(r, size, off)
		if err != nil {
			return err
		}
		if h == nil {
			return nil
		}
		first := int(h[0])
		if first < 30 || first > len(h) {
			return ErrCorrupt
		}
		method := h[5]
		ftype := h[6]
		stamp := le32(h[8:])
		packed := int64(le32(h[12:]))
		orig := int64(le32(h[16:]))
		want := le32(h[20:])
		rest := h[first:]
		name := cString(rest)
		name = strings.ReplaceAll(name, "\\", "/")
		e := Entry{Name: name, Size: orig, Packed: packed, Method: "stored",
			Modified: dosTime(uint16(stamp>>16), uint16(stamp)), Dir: ftype == 3}
		if method != 0 {
			e.Method = "method " + string(rune('0'+method))
		}
		open := func(max int64) ([]byte, error) {
			if err := sized(orig, max); err != nil {
				return nil, err
			}
			if h[4]&1 != 0 {
				return nil, ErrMethod // garbled (a password)
			}
			src, err := readAt(r, size, data, packed)
			if err != nil {
				return nil, err
			}
			var out []byte
			switch method {
			case 0:
				if packed != orig {
					return nil, ErrCorrupt
				}
				out = src
			case 1, 2, 3:
				out, err = unhuf(src, orig, 16)
			case 4:
				out, err = unarj4(src, orig)
			default:
				return nil, ErrMethod
			}
			if err != nil {
				return nil, err
			}
			if crc32sum(out) != want {
				return nil, ErrChecksum
			}
			return out, nil
		}
		if ftype != 2 && !fn(e, open) {
			return nil
		}
		off = data + packed
	}
}

// unarj4 unpacks ARJ's method 4 ("fastest"): a literal or a match, each
// of lengths and distances in a code of growing widths.
func unarj4(src []byte, size int64) ([]byte, error) {
	r := &bitReader{b: src}
	// a unary count of more widths from start up to stop, then that many bits
	value := func(start, stop uint) int {
		plus, pwr := 0, 1<<start
		width := start
		for ; width < stop; width++ {
			if r.bits(1) == 0 {
				break
			}
			plus += pwr
			pwr <<= 1
		}
		c := 0
		if width != 0 {
			c = int(r.bits(width))
		}
		return c + plus
	}
	out := make([]byte, 0, size)
	for int64(len(out)) < size {
		if r.overrun() {
			return nil, ErrCorrupt
		}
		n := value(0, 7)
		if n == 0 {
			out = append(out, byte(r.bits(8)))
			continue
		}
		n += hufThreshold - 1
		p := value(9, 13)
		from := len(out) - p - 1
		for ; n > 0 && int64(len(out)) < size; n-- {
			if from < 0 {
				out = append(out, ' ')
			} else {
				out = append(out, out[from])
			}
			from++
		}
	}
	return out, nil
}

// arjBasic is a basic header, its CRC after it.
func arjBasic(method, ftype byte, stamp, packed, orig, crc uint32, name string) []byte {
	h := make([]byte, 30)
	h[0] = 30
	h[1] = 11 // the version that wrote it: ARJ 2.50's
	h[2] = 1  // the version to read it
	h[3] = 0  // MS-DOS
	h[5] = method
	h[6] = ftype
	put32(h[8:], stamp)
	put32(h[12:], packed)
	put32(h[16:], orig)
	put32(h[20:], crc)
	put16(h[26:], 0x20) // archive
	h = append(h, name...)
	h = append(h, 0, 0) // the name's end, an empty comment
	var b bytes.Buffer
	b.Write([]byte{0x60, 0xEA})
	var n [2]byte
	put16(n[:], uint16(len(h)))
	b.Write(n[:])
	b.Write(h)
	var c [4]byte
	put32(c[:], crc32sum(h))
	b.Write(c[:])
	b.Write([]byte{0, 0}) // no extended header
	return b.Bytes()
}

// writeARJ writes files stored (method 0) in an archive called name.
func writeARJ(w io.Writer, name string, files []File) error {
	var b bytes.Buffer
	b.Write(arjBasic(0, 2, 0, 0, 0, 0, name))
	for _, f := range files {
		date, tm := dosStamp(f.Modified)
		n := f.Name
		if len(n) > 500 {
			n = n[:500]
		}
		b.Write(arjBasic(0, 0, uint32(date)<<16|uint32(tm), uint32(len(f.Data)), uint32(len(f.Data)),
			crc32sum(f.Data), n))
		b.Write(f.Data)
	}
	b.Write([]byte{0x60, 0xEA, 0, 0})
	_, err := w.Write(b.Bytes())
	return err
}
