package marvel

import (
	"encoding/binary"
)

// The icons a client fetches by shabby id: drawn here, in the 16 colours
// Windows 95 always has.

// the standard 16-colour palette, as RGB
var palette16 = [16][3]byte{
	{0, 0, 0}, {0x80, 0, 0}, {0, 0x80, 0}, {0x80, 0x80, 0}, {0, 0, 0x80}, {0x80, 0, 0x80},
	{0, 0x80, 0x80}, {0xC0, 0xC0, 0xC0}, {0x80, 0x80, 0x80}, {0xFF, 0, 0}, {0, 0xFF, 0},
	{0xFF, 0xFF, 0}, {0, 0, 0xFF}, {0xFF, 0, 0xFF}, {0, 0xFF, 0xFF}, {0xFF, 0xFF, 0xFF},
}

const (
	cBlack     = 0
	cDarkRed   = 1
	cOlive     = 3
	cNavy      = 4
	cSilver    = 7
	cGray      = 8
	cRed       = 9
	cYellow    = 11
	cWhite     = 15
	cClear     = -1
	iconSize   = 32
	bannerSize = 16
)

type canvas [][]int

func newCanvas(n int) canvas {
	c := make(canvas, n)
	for y := range c {
		c[y] = make([]int, n)
		for x := range c[y] {
			c[y][x] = cClear
		}
	}
	return c
}

// box fills x0..x1, y0..y1 (inclusive) with fill and outlines it with edge.
func (c canvas) box(x0, y0, x1, y1, fill, edge int) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			v := fill
			if x == x0 || x == x1 || y == y0 || y == y1 {
				v = edge
			}
			c[y][x] = v
		}
	}
}

func (c canvas) line(x0, x1, y, col int) {
	for x := x0; x <= x1; x++ {
		c[y][x] = col
	}
}

// folderArt is a closed folder.
func folderArt(n int) canvas {
	c := newCanvas(n)
	s := func(v int) int { return v * n / iconSize }
	c.box(s(3), s(6), s(13), s(10), cYellow, cBlack)
	c.box(s(2), s(9), s(29), s(26), cYellow, cBlack)
	c.line(s(3), s(28), s(25), cOlive)
	return c
}

// boardArt is a note pinned to a board.
func boardArt(n int) canvas {
	c := newCanvas(n)
	s := func(v int) int { return v * n / iconSize }
	c.box(s(2), s(3), s(29), s(28), cOlive, cBlack)
	c.box(s(8), s(8), s(23), s(24), cWhite, cGray)
	for _, y := range []int{12, 15, 18, 21} {
		c.line(s(10), s(21), s(y), cNavy)
	}
	c.box(s(15), s(5), s(16), s(9), cRed, cDarkRed)
	return c
}

// chatArt is two speech balloons.
func chatArt(n int) canvas {
	c := newCanvas(n)
	s := func(v int) int { return v * n / iconSize }
	c.box(s(2), s(4), s(20), s(16), cWhite, cBlack)
	c.box(s(5), s(16), s(7), s(19), cWhite, cBlack)
	c.box(s(11), s(13), s(29), s(25), cYellow, cBlack)
	c.box(s(24), s(25), s(26), s(28), cYellow, cBlack)
	for _, y := range []int{8, 11} {
		c.line(s(5), s(17), s(y), cNavy)
	}
	for _, y := range []int{18, 21} {
		c.line(s(14), s(26), s(y), cNavy)
	}
	return c
}

// fileArt is a page with its corner turned down.
func fileArt(n int) canvas {
	c := newCanvas(n)
	s := func(v int) int { return v * n / iconSize }
	c.box(s(6), s(2), s(25), s(29), cWhite, cBlack)
	c.box(s(19), s(2), s(25), s(8), cSilver, cBlack)
	for _, y := range []int{12, 15, 18, 21, 24} {
		c.line(s(9), s(22), s(y), cNavy)
	}
	return c
}

// ico is the canvas as a 4-bit Windows icon.
func (c canvas) ico() []byte {
	n := len(c)
	xorRow := (n/2 + 3) &^ 3
	andRow := ((n+7)/8 + 3) &^ 3
	size := 40 + 64 + n*xorRow + n*andRow
	b := binary.LittleEndian.AppendUint16(nil, 0)
	b = binary.LittleEndian.AppendUint16(b, 1) // an icon
	b = binary.LittleEndian.AppendUint16(b, 1) // one image
	b = append(b, byte(n), byte(n), 16, 0)
	b = binary.LittleEndian.AppendUint16(b, 1) // planes
	b = binary.LittleEndian.AppendUint16(b, 4) // bits per pixel
	b = binary.LittleEndian.AppendUint32(b, uint32(size))
	b = binary.LittleEndian.AppendUint32(b, uint32(len(b)+4))
	b = c.dibHeader(b, 2*n, 4, n*xorRow+n*andRow)
	for _, p := range palette16 {
		b = append(b, p[2], p[1], p[0], 0)
	}
	for y := n - 1; y >= 0; y-- {
		row := make([]byte, xorRow)
		for x, v := range c[y] {
			if v < 0 {
				v = 0
			}
			row[x/2] |= byte(v) << (4 * (1 - x%2))
		}
		b = append(b, row...)
	}
	for y := n - 1; y >= 0; y-- {
		row := make([]byte, andRow)
		for x, v := range c[y] {
			if v < 0 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
		b = append(b, row...)
	}
	return b
}

// bmp is the canvas as a 24-bit bitmap file, clear as silver.
func (c canvas) bmp() []byte {
	n := len(c)
	row := (n*3 + 3) &^ 3
	b := []byte{'B', 'M'}
	b = binary.LittleEndian.AppendUint32(b, uint32(14+40+n*row))
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = binary.LittleEndian.AppendUint32(b, 14+40)
	b = c.dibHeader(b, n, 24, n*row)
	for y := n - 1; y >= 0; y-- {
		r := make([]byte, 0, row)
		for _, v := range c[y] {
			if v < 0 {
				v = cSilver
			}
			p := palette16[v]
			r = append(r, p[2], p[1], p[0])
		}
		b = append(b, r[:cap(r)]...)
	}
	return b
}

func (c canvas) dibHeader(b []byte, height, bpp, imageSize int) []byte {
	b = binary.LittleEndian.AppendUint32(b, 40)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(c)))
	b = binary.LittleEndian.AppendUint32(b, uint32(height))
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, uint16(bpp))
	b = binary.LittleEndian.AppendUint32(b, 0) // no compression
	b = binary.LittleEndian.AppendUint32(b, uint32(imageSize))
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = binary.LittleEndian.AppendUint32(b, 0)
	colours := 0
	if bpp <= 8 {
		colours = 1 << bpp
	}
	b = binary.LittleEndian.AppendUint32(b, uint32(colours))
	return binary.LittleEndian.AppendUint32(b, 0)
}

var shabbies = map[uint32][]byte{
	shabbyBanner: boardArt(bannerSize).bmp(),
	iconFolder:   folderArt(iconSize).ico(),
	iconBoard:    boardArt(iconSize).ico(),
	iconChat:     chatArt(iconSize).ico(),
	iconFile:     fileArt(iconSize).ico(),
}

// shabby is the file for a shabby id (empty: none; the client then shows
// its own glyph).
func shabby(id uint32) []byte { return shabbies[id] }
