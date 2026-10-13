package archive

// The static Huffman coding of LHA's -lh4- to -lh7- and ARJ's methods 1
// to 3 (Haruhiko Okumura's, as both use it): blocks, each with its own
// code tables, of literals and (length, distance) pairs.

// bitReader reads bits from the first (most significant) down, as LHA and
// ARJ write them; past the end it reads zeros (the output's size, known,
// ends the decoding).
type bitReader struct {
	b   []byte
	pos int
	buf uint32 // the next bits, from bit 31 down
	n   uint   // how many of them are good
}

func (r *bitReader) fill() {
	for r.n <= 24 {
		var c byte
		if r.pos < len(r.b) {
			c = r.b[r.pos]
		}
		r.pos++
		r.buf |= uint32(c) << (24 - r.n)
		r.n += 8
	}
}

// peek is the next 16 bits.
func (r *bitReader) peek() uint16 {
	r.fill()
	return uint16(r.buf >> 16)
}

func (r *bitReader) skip(n uint) {
	r.fill()
	r.buf <<= n
	r.n -= n
}

// bits reads n (at most 16) bits.
func (r *bitReader) bits(n uint) uint16 {
	if n == 0 {
		return 0
	}
	v := r.peek() >> (16 - n)
	r.skip(n)
	return v
}

// overrun reports whether far more was read than there is: a corrupt
// stream decoding zeros.
func (r *bitReader) overrun() bool { return r.pos > len(r.b)+8 }

const (
	hufThreshold = 3
	hufMaxMatch  = 256
	hufNC        = 255 + hufMaxMatch + 2 - hufThreshold // 510: literals and lengths
	hufCBit      = 9
	hufNT        = 16 + 3 // the code lengths' code
	hufTBit      = 5
	hufMaxNP     = 0x11
)

// huf decodes one stream.
type huf struct {
	r         bitReader
	np        int  // positions' codes: the dictionary's bits + 1
	pbit      uint // bits that say how many there are
	blocksize int
	cLen      [hufNC]byte
	ptLen     [hufNT + hufMaxNP]byte
	cTable    [4096]uint16
	ptTable   [256]uint16
	left      [2*hufNC - 1]uint16
	right     [2*hufNC - 1]uint16
}

// makeTable builds a decoding table of tablebits from code lengths, with
// a tree beside it for codes longer than that; false if the lengths do
// not make a code.
func (h *huf) makeTable(nchar int, bitlen []byte, tablebits uint, table []uint16) bool {
	var count [17]uint32
	var start, weight [18]uint32
	for i := 0; i < nchar; i++ {
		if bitlen[i] > 16 {
			return false
		}
		count[bitlen[i]]++
	}
	for i := 1; i <= 16; i++ {
		start[i+1] = start[i] + count[i]<<(16-i)
	}
	if start[17] != 1<<16 {
		return false
	}
	jut := 16 - tablebits
	for i := uint(1); i <= tablebits; i++ {
		start[i] >>= jut
		weight[i] = 1 << (tablebits - i)
	}
	for i := tablebits + 1; i <= 16; i++ {
		weight[i] = 1 << (16 - i)
	}
	if i := start[tablebits+1] >> jut; i != 1<<16>>jut {
		for k := uint32(1) << tablebits; i < k; i++ {
			table[i] = 0
		}
	}
	avail := nchar
	mask := uint32(1) << (15 - tablebits)
	for ch := 0; ch < nchar; ch++ {
		l := uint(bitlen[ch])
		if l == 0 {
			continue
		}
		next := start[l] + weight[l]
		if l <= tablebits {
			if next > 1<<tablebits {
				return false
			}
			for i := start[l]; i < next; i++ {
				table[i] = uint16(ch)
			}
		} else {
			k := start[l]
			p := &table[k>>jut]
			for i := l - tablebits; i != 0; i-- {
				if *p == 0 {
					if avail >= len(h.left) {
						return false
					}
					h.left[avail], h.right[avail] = 0, 0
					*p = uint16(avail)
					avail++
				}
				if int(*p) >= len(h.left) {
					return false
				}
				if k&mask != 0 {
					p = &h.right[*p]
				} else {
					p = &h.left[*p]
				}
				k <<= 1
			}
			*p = uint16(ch)
		}
		start[l] = next
	}
	return true
}

// readPtLen reads the lengths of a pt code: nn codes, nbit bits for how
// many; after the special'th (if any) a 2-bit count of zeros.
func (h *huf) readPtLen(nn int, nbit uint, special int) bool {
	n := int(h.r.bits(nbit))
	if n == 0 {
		c := h.r.bits(nbit)
		for i := 0; i < nn; i++ {
			h.ptLen[i] = 0
		}
		for i := range h.ptTable {
			h.ptTable[i] = c
		}
		return true
	}
	if n > nn {
		return false
	}
	i := 0
	for i < n {
		c := int(h.r.peek() >> 13)
		if c == 7 {
			for mask := uint16(1) << 12; mask&h.r.peek() != 0; mask >>= 1 {
				c++
				if c > 16 {
					return false
				}
			}
		}
		if c < 7 {
			h.r.skip(3)
		} else {
			h.r.skip(uint(c - 3))
		}
		h.ptLen[i] = byte(c)
		i++
		if i == special {
			for z := int(h.r.bits(2)); z > 0 && i < nn; z-- {
				h.ptLen[i] = 0
				i++
			}
		}
	}
	for ; i < nn; i++ {
		h.ptLen[i] = 0
	}
	return h.makeTable(nn, h.ptLen[:], 8, h.ptTable[:])
}

// readCLen reads the literals' and lengths' code lengths.
func (h *huf) readCLen() bool {
	n := int(h.r.bits(hufCBit))
	if n == 0 {
		c := h.r.bits(hufCBit)
		for i := range h.cLen {
			h.cLen[i] = 0
		}
		for i := range h.cTable {
			h.cTable[i] = c
		}
		return true
	}
	if n > hufNC {
		return false
	}
	i := 0
	for i < n {
		c := int(h.ptTable[h.r.peek()>>8])
		if c >= hufNT {
			for mask := uint16(1) << 7; c >= hufNT; mask >>= 1 {
				if mask == 0 || c >= len(h.left) {
					return false
				}
				if h.r.peek()&mask != 0 {
					c = int(h.right[c])
				} else {
					c = int(h.left[c])
				}
			}
		}
		h.r.skip(uint(h.ptLen[c]))
		if c <= 2 {
			switch c {
			case 0:
				c = 1
			case 1:
				c = int(h.r.bits(4)) + 3
			default:
				c = int(h.r.bits(hufCBit)) + 20
			}
			for ; c > 0 && i < hufNC; c-- {
				h.cLen[i] = 0
				i++
			}
		} else {
			h.cLen[i] = byte(c - 2)
			i++
		}
	}
	for ; i < hufNC; i++ {
		h.cLen[i] = 0
	}
	return h.makeTable(hufNC, h.cLen[:], 12, h.cTable[:])
}

// decodeC is the next literal (below 256) or length code; -1 if the
// stream is broken.
func (h *huf) decodeC() int {
	if h.blocksize == 0 {
		h.blocksize = int(h.r.bits(16))
		if !h.readPtLen(hufNT, hufTBit, 3) || !h.readCLen() || !h.readPtLen(h.np, h.pbit, -1) {
			return -1
		}
	}
	h.blocksize--
	j := int(h.cTable[h.r.peek()>>4])
	if j >= hufNC {
		for mask := uint16(1) << 3; j >= hufNC; mask >>= 1 {
			if mask == 0 || j >= len(h.left) {
				return -1
			}
			if h.r.peek()&mask != 0 {
				j = int(h.right[j])
			} else {
				j = int(h.left[j])
			}
		}
	}
	h.r.skip(uint(h.cLen[j]))
	return j
}

// decodeP is the next distance (less one); -1 if the stream is broken.
func (h *huf) decodeP() int {
	j := int(h.ptTable[h.r.peek()>>8])
	if j >= h.np {
		for mask := uint16(1) << 7; j >= h.np; mask >>= 1 {
			if mask == 0 || j >= len(h.left) {
				return -1
			}
			if h.r.peek()&mask != 0 {
				j = int(h.right[j])
			} else {
				j = int(h.left[j])
			}
		}
	}
	h.r.skip(uint(h.ptLen[j]))
	if j != 0 {
		j = 1<<(j-1) + int(h.r.bits(uint(j-1)))
	}
	return j
}

// unhuf unpacks size bytes coded so, with distances of up to dicbit bits.
// Before the start the dictionary is spaces, as LHA's is.
func unhuf(src []byte, size int64, dicbit uint) ([]byte, error) {
	h := &huf{r: bitReader{b: src}, np: int(dicbit) + 1, pbit: 4}
	if dicbit > 13 {
		h.pbit = 5
	}
	out := make([]byte, 0, size)
	for int64(len(out)) < size {
		if h.r.overrun() {
			return nil, ErrCorrupt
		}
		c := h.decodeC()
		switch {
		case c < 0:
			return nil, ErrCorrupt
		case c < 256:
			out = append(out, byte(c))
		default:
			n := c - 256 + hufThreshold
			p := h.decodeP()
			if p < 0 {
				return nil, ErrCorrupt
			}
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
	}
	return out, nil
}
