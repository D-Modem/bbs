// Package sixel encodes pictures as DEC sixel graphics, which xterm
// (-ti vt340), mlterm, foot, WezTerm, Windows Terminal, iTerm2, SyncTERM
// and others show inline.
//
// A sixel picture is a DCS string: ESC P 0;1;0 q, the raster attributes,
// the palette, then bands six pels high, each a run of characters per color
// whose six low bits are that color's pels in the column, and ESC \.
package sixel

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
)

// MaxColors is the most colors a picture is given, register 0 aside: a
// VT340 had 16 registers; terminals today have 256.
const MaxColors = 255

// Encode encodes img.  Transparent pels (alpha < 128) are left as the
// terminal's background.  A picture with more than MaxColors colors is
// dithered to the web-safe palette.
func Encode(img image.Image) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("sixel: empty picture")
	}
	if w > 4096 || h > 4096 {
		return nil, errors.New("sixel: picture too large")
	}
	pal, idx := quantize(img)

	var out bytes.Buffer
	// P2 = 1: pels with no sixel keep the background
	out.WriteString("\x1bP0;1;0q")
	fmt.Fprintf(&out, "\"1;1;%d;%d", w, h)
	// register 0 is black and unused: decoders that fill the pels no
	// sixel sets (rather than leave them, as P2 = 1 asks) fill them with
	// register 0.  The picture's colors are 1 on.
	out.WriteString("#0;2;0;0;0")
	for i, c := range pal {
		r, g, bl, _ := c.RGBA()
		fmt.Fprintf(&out, "#%d;2;%d;%d;%d", i+1, pct(r), pct(g), pct(bl))
	}
	for band := 0; band < h; band += 6 {
		// the colors in this band
		used := make([]bool, len(pal))
		for y := band; y < band+6 && y < h; y++ {
			for x := 0; x < w; x++ {
				if c := idx[y*w+x]; c >= 0 {
					used[c] = true
				}
			}
		}
		first := true
		for c := range pal {
			if !used[c] {
				continue
			}
			if !first {
				out.WriteByte('$') // back to the band's start for the next color
			}
			first = false
			fmt.Fprintf(&out, "#%d", c+1)
			run, last := 0, byte(0)
			flush := func() {
				switch {
				case run == 0:
				case run > 3:
					fmt.Fprintf(&out, "!%d%c", run, last)
				default:
					for i := 0; i < run; i++ {
						out.WriteByte(last)
					}
				}
			}
			for x := 0; x < w; x++ {
				var bits byte
				for i := 0; i < 6 && band+i < h; i++ {
					if idx[(band+i)*w+x] == c {
						bits |= 1 << i
					}
				}
				ch := 63 + bits
				if ch == last && run > 0 {
					run++
					continue
				}
				flush()
				run, last = 1, ch
			}
			// trailing empty columns need not be sent
			if last != 63 {
				flush()
			}
		}
		out.WriteByte('-') // next band
	}
	out.WriteString("\x1b\\")
	return out.Bytes(), nil
}

func pct(v uint32) int { return int((v*100 + 0x7fff) / 0xffff) }

// quantize gives the picture's palette and each pel's index in it (-1 for
// transparent).
func quantize(img image.Image) ([]color.Color, []int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	idx := make([]int, w*h)
	seen := map[color.NRGBA]int{}
	var pal []color.Color
	exact := true
	for y := 0; y < h && exact; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			if c.A < 128 {
				idx[y*w+x] = -1
				continue
			}
			c.A = 255
			i, ok := seen[c]
			if !ok {
				if len(pal) == MaxColors {
					exact = false
					break
				}
				i = len(pal)
				seen[c] = i
				pal = append(pal, c)
			}
			idx[y*w+x] = i
		}
	}
	if exact {
		return pal, idx
	}
	// too many colors: dither to the web-safe palette (216)
	dst := image.NewPaletted(image.Rect(0, 0, w, h), palette.WebSafe)
	draw.FloydSteinberg.Draw(dst, dst.Bounds(), img, b.Min)
	pal = palette.WebSafe
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if a < 0x8000 {
				idx[y*w+x] = -1
			} else {
				idx[y*w+x] = int(dst.ColorIndexAt(x, y))
			}
		}
	}
	return pal, idx
}
