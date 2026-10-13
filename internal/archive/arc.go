package archive

import (
	"bytes"
	"io"
	"strings"
)

var arcMethods = map[byte]string{1: "stored", 2: "stored", 3: "packed", 4: "squeezed", 8: "crunched", 9: "squashed"}

// walkARC reads an ARC archive (SEA's, and PKPAK's methods it shares).
func walkARC(r io.ReaderAt, size int64, fn func(e Entry, open Opener) bool) error {
	off := int64(0)
	for {
		h, err := readAt(r, size, off, 2)
		if err != nil {
			return nil // the file ends: as good as an end mark
		}
		if h[0] != 0x1A {
			return ErrCorrupt
		}
		method := h[1]
		if method == 0 {
			return nil
		}
		hlen := int64(29)
		if method == 1 {
			hlen = 25 // the oldest: no unpacked size
		}
		h, err = readAt(r, size, off, hlen)
		if err != nil {
			return ErrCorrupt
		}
		name := cString(h[2:15])
		packed := int64(le32(h[15:]))
		mod := dosTime(le16(h[19:]), le16(h[21:]))
		want := le16(h[23:])
		orig := packed
		if method != 1 {
			orig = int64(le32(h[25:]))
		}
		dataOff := off + hlen
		mname, known := arcMethods[method]
		if !known {
			mname = "method " + string(rune('0'+method%10))
		}
		e := Entry{Name: strings.ReplaceAll(name, "\\", "/"), Size: orig, Packed: packed, Method: mname, Modified: mod}
		open := func(max int64) ([]byte, error) {
			if err := sized(orig, max); err != nil {
				return nil, err
			}
			src, err := readAt(r, size, dataOff, packed)
			if err != nil {
				return nil, err
			}
			var out []byte
			switch method {
			case 1, 2:
				out = src
			case 3:
				out, err = unRLE90(src, orig)
			case 4:
				var sq []byte
				if sq, err = unsqueeze(src, max); err == nil {
					out, err = unRLE90(sq, orig)
				}
			case 8:
				if len(src) == 0 || src[0] != 12 {
					return nil, ErrCorrupt // the bits it was crunched to: always 12
				}
				var lz []byte
				if lz, err = unLZW(src[1:], 12, max); err == nil {
					out, err = unRLE90(lz, orig)
				}
			case 9:
				out, err = unLZW(src, 13, max)
			default:
				return nil, ErrMethod
			}
			if err != nil {
				return nil, err
			}
			if int64(len(out)) != orig {
				return nil, ErrCorrupt
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
}

// unRLE90 undoes ARC's run-length coding: 0x90 n repeats the byte before
// it to n times in all, 0x90 0 is a 0x90.
func unRLE90(src []byte, size int64) ([]byte, error) {
	out := make([]byte, 0, size)
	var last byte
	rep := false
	for _, c := range src {
		switch {
		case rep:
			if c == 0 {
				out = append(out, 0x90)
			} else {
				for i := 1; i < int(c); i++ {
					out = append(out, last)
				}
			}
			rep = false
		case c == 0x90:
			rep = true
		default:
			out = append(out, c)
			last = c
		}
		if int64(len(out)) > size {
			return nil, ErrCorrupt
		}
	}
	return out, nil
}

// unsqueeze undoes ARC's squeezing: a Huffman tree (its node count, then
// pairs of children: a leaf is -(byte+1), 256 the end), then the codes,
// low bit first.
func unsqueeze(src []byte, max int64) ([]byte, error) {
	if len(src) < 2 {
		return nil, ErrCorrupt
	}
	n := int(le16(src))
	if n > 256 || 2+4*n > len(src) {
		return nil, ErrCorrupt
	}
	if n == 0 {
		return nil, nil
	}
	tree := make([][2]int16, n)
	for i := range tree {
		tree[i] = [2]int16{int16(le16(src[2+4*i:])), int16(le16(src[4+4*i:]))}
	}
	var out []byte
	node := int16(0)
	for _, c := range src[2+4*n:] {
		for bit := 0; bit < 8; bit++ {
			node = tree[node][c>>bit&1]
			if node < 0 {
				v := -(int(node) + 1)
				if v == 256 {
					return out, nil
				}
				out = append(out, byte(v))
				if int64(len(out)) > max {
					return nil, ErrTooBig
				}
				node = 0
			} else if int(node) >= n {
				return nil, ErrCorrupt
			}
		}
	}
	return out, nil // no end mark: what there was
}

// unLZW undoes ARC's crunching and squashing: LZW of 9 bits growing to
// maxbits, code 256 clearing the table, codes low bit first and read in
// groups of their width's bytes, as Unix's compress reads them.
func unLZW(src []byte, maxbits uint, max int64) ([]byte, error) {
	const clear, first = 256, 257
	maxmax := 1 << maxbits
	prefix := make([]uint16, maxmax)
	suffix := make([]byte, maxmax)
	for i := 0; i < 256; i++ {
		suffix[i] = byte(i)
	}
	nbits := uint(9)
	maxcode := 1<<nbits - 1
	free := first
	clearFlag := false
	pos := 0
	var group []byte
	gbit, gsize := 0, 0 // the bit at in the group, the group's usable bits
	getcode := func() int {
		if clearFlag || gbit >= gsize || free > maxcode {
			if free > maxcode {
				nbits++
				if nbits >= maxbits {
					nbits = maxbits
					maxcode = maxmax
				} else {
					maxcode = 1<<nbits - 1
				}
			}
			if clearFlag {
				nbits = 9
				maxcode = 1<<nbits - 1
				clearFlag = false
			}
			if pos >= len(src) {
				return -1
			}
			end := pos + int(nbits)
			if end > len(src) {
				end = len(src)
			}
			group = src[pos:end]
			pos = end
			gbit = 0
			gsize = len(group)*8 - int(nbits-1)
		}
		code := 0
		for i := uint(0); i < nbits; i++ {
			b := gbit + int(i)
			if b/8 < len(group) && group[b/8]>>(b%8)&1 != 0 {
				code |= 1 << i
			}
		}
		gbit += int(nbits)
		return code
	}
	var out []byte
	stack := make([]byte, 0, maxmax)
	old := getcode()
	if old < 0 {
		return nil, nil
	}
	if old > 255 {
		return nil, ErrCorrupt
	}
	fin := byte(old)
	out = append(out, fin)
	for {
		code := getcode()
		if code < 0 {
			return out, nil
		}
		if code == clear {
			free = first - 1
			clearFlag = true
			if code = getcode(); code < 0 {
				return out, nil
			}
		}
		in := code
		stack = stack[:0]
		if code >= free {
			if code > free {
				return nil, ErrCorrupt
			}
			stack = append(stack, fin)
			code = old
		}
		for code >= 256 {
			if code >= maxmax || len(stack) >= maxmax {
				return nil, ErrCorrupt
			}
			stack = append(stack, suffix[code])
			code = int(prefix[code])
		}
		fin = suffix[code]
		stack = append(stack, fin)
		for i := len(stack) - 1; i >= 0; i-- {
			out = append(out, stack[i])
		}
		if int64(len(out)) > max {
			return nil, ErrTooBig
		}
		if free < maxmax {
			prefix[free] = uint16(old)
			suffix[free] = fin
			free++
		}
		old = in
	}
}

// arcName is a name as ARC keeps it: 8.3, in capitals.
func arcName(n string) string {
	n = strings.ToUpper(n)
	if len(n) > 12 {
		base, ext, _ := strings.Cut(n, ".")
		if len(base) > 8 {
			base = base[:8]
		}
		if len(ext) > 3 {
			ext = ext[:3]
		}
		n = base
		if ext != "" {
			n += "." + ext
		}
	}
	return n
}

// writeARC writes files stored (method 2).
func writeARC(w io.Writer, files []File) error {
	var b bytes.Buffer
	for _, f := range files {
		h := make([]byte, 29)
		h[0], h[1] = 0x1A, 2
		copy(h[2:14], arcName(f.Name))
		put32(h[15:], uint32(len(f.Data)))
		date, tm := dosStamp(f.Modified)
		put16(h[19:], date)
		put16(h[21:], tm)
		put16(h[23:], crc16(f.Data))
		put32(h[25:], uint32(len(f.Data)))
		b.Write(h)
		b.Write(f.Data)
	}
	b.Write([]byte{0x1A, 0})
	_, err := w.Write(b.Bytes())
	return err
}
