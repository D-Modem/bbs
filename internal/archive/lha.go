package archive

import (
	"bytes"
	"io"
	"strings"
	"time"
)

// lhaDicbit is each LHA method's dictionary's bits (0: stored).
var lhaDicbit = map[string]uint{"-lh0-": 0, "-lz4-": 0, "-lhd-": 0, "-lh4-": 12, "-lh5-": 13, "-lh6-": 15, "-lh7-": 16}

// walkLHA reads an LHA (LZH) archive: headers of levels 0, 1 and 2.
func walkLHA(r io.ReaderAt, size int64, fn func(e Entry, open Opener) bool) error {
	off := int64(0)
	for off < size {
		first, err := readAt(r, size, off, 1)
		if err != nil || first[0] == 0 {
			return nil // the end (a 0, or the file ends)
		}
		base, err := readAt(r, size, off, 22)
		if err != nil {
			return ErrCorrupt
		}
		method := string(base[2:7])
		packed := int64(le32(base[7:]))
		orig := int64(le32(base[11:]))
		level := base[20]
		var name, dir string
		var mod time.Time
		var headerLen int64 // to the data
		var want uint16
		switch level {
		case 0, 1:
			hsize := int64(base[0])
			h, err := readAt(r, size, off, hsize+2)
			if err != nil || hsize < 22 {
				return ErrCorrupt
			}
			var sum byte
			for _, c := range h[2:] {
				sum += c
			}
			if sum != h[1] {
				return ErrCorrupt
			}
			mod = dosTime(le16(h[17:]), le16(h[15:]))
			nl := int64(h[21])
			if 22+nl+2 > hsize+2 {
				return ErrCorrupt
			}
			name = string(h[22 : 22+nl])
			want = le16(h[22+nl:])
			headerLen = hsize + 2
			if level == 1 {
				// the extended headers, whose size the packed size counts
				next := int64(le16(h[hsize:]))
				for next != 0 {
					x, err := readAt(r, size, off+headerLen, next)
					if err != nil || next < 3 {
						return ErrCorrupt
					}
					name, dir = lhaExt(x[:next-2], name, dir)
					headerLen += next
					packed -= next
					next = int64(le16(x[next-2:]))
				}
			}
		case 2:
			total := int64(le16(base[0:]))
			h, err := readAt(r, size, off, total)
			if err != nil || total < 26 {
				return ErrCorrupt
			}
			mod = time.Unix(int64(le32(h[15:])), 0)
			want = le16(h[21:])
			pos := int64(24)
			next := int64(le16(h[pos:]))
			pos += 2
			for next != 0 {
				if next < 3 || pos+next > total {
					return ErrCorrupt
				}
				x := h[pos : pos+next]
				name, dir = lhaExt(x[:next-2], name, dir)
				pos += next
				next = int64(le16(x[next-2:]))
			}
			headerLen = total
		default:
			return ErrMethod
		}
		if packed < 0 || orig < 0 {
			return ErrCorrupt
		}
		if dir != "" {
			name = dir + "/" + name
		}
		name = strings.ReplaceAll(name, "\\", "/")
		dataOff := off + headerLen
		dicbit, known := lhaDicbit[method]
		e := Entry{Name: name, Size: orig, Packed: packed, Method: method, Modified: mod, Dir: method == "-lhd-"}
		open := func(max int64) ([]byte, error) {
			if err := sized(orig, max); err != nil {
				return nil, err
			}
			if !known {
				return nil, ErrMethod
			}
			src, err := readAt(r, size, dataOff, packed)
			if err != nil {
				return nil, err
			}
			var out []byte
			if dicbit == 0 {
				if packed != orig {
					return nil, ErrCorrupt
				}
				out = src
			} else if out, err = unhuf(src, orig, dicbit); err != nil {
				return nil, err
			}
			if crc16(out) != want {
				return nil, ErrChecksum
			}
			return out, nil
		}
		if !fn(e, open) {
			return nil
		}
		off = dataOff + packed
	}
	return nil
}

// lhaExt reads an extended header: the file's name, its directory.
func lhaExt(x []byte, name, dir string) (string, string) {
	if len(x) == 0 {
		return name, dir
	}
	switch x[0] {
	case 0x01:
		name = string(x[1:])
	case 0x02:
		dir = strings.Trim(strings.ReplaceAll(string(x[1:]), "\xff", "/"), "/")
	}
	return name, dir
}

// writeLHA writes files stored (-lh0-), with level 0 headers.
func writeLHA(w io.Writer, files []File) error {
	var b bytes.Buffer
	for _, f := range files {
		name := []byte(f.Name)
		if len(name) > 200 {
			name = name[:200]
		}
		hsize := 22 + len(name)
		h := make([]byte, hsize+2)
		h[0] = byte(hsize)
		copy(h[2:], "-lh0-")
		put32(h[7:], uint32(len(f.Data)))
		put32(h[11:], uint32(len(f.Data)))
		date, tm := dosStamp(f.Modified)
		put16(h[15:], tm)
		put16(h[17:], date)
		h[19] = 0x20 // archive
		h[20] = 0    // level 0
		h[21] = byte(len(name))
		copy(h[22:], name)
		put16(h[22+len(name):], crc16(f.Data))
		var sum byte
		for _, c := range h[2:] {
			sum += c
		}
		h[1] = sum
		b.Write(h)
		b.Write(f.Data)
	}
	b.WriteByte(0)
	_, err := w.Write(b.Bytes())
	return err
}
