package textui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"golang.org/x/image/draw"

	"github.com/Neo-Desktop/bbs-sec/internal/sixel"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// Pictures (an image board's, view.Picture) on a text terminal, the best
// way it has:
//
//   - kitty's graphics protocol: kitty and Ghostty (which have no sixel)
//   - sixel: xterm -ti vt340, foot, mlterm, WezTerm, Windows Terminal,
//     iTerm2, SyncTERM, the web terminal...
//   - iTerm2's inline images: iTerm2 before it had sixel, mintty
//   - coloured half blocks (▀, 256 colours), on any other UTF-8 terminal
//     with colour: rough, but a picture
//
// and in words on the rest.  The caller can turn them off on the profile.

// Ways of showing a picture.
const (
	picNone   = ""
	picKitty  = "kitty"
	picSixel  = "sixel"
	picITerm  = "iterm"
	picBlocks = "blocks"
)

// PictureMode is how pictures are shown on the terminal ("": they are not).
func (t *Term) PictureMode() string {
	if t.NoPictures || !t.ANSI || t.PETSCII || t.VT52 || t.DEC || t.Mono {
		return picNone
	}
	name := strings.ToLower(t.Ident)
	switch {
	case strings.Contains(name, "kitty") || strings.Contains(name, "ghostty"):
		return picKitty
	case t.Sixel:
		return picSixel
	case strings.Contains(name, "iterm2") || strings.Contains(name, "wezterm") || strings.Contains(name, "mintty"):
		return picITerm
	case t.UTF8:
		return picBlocks
	}
	return picNone
}

// cell is the character cell in pixels, as the terminal said or a guess.
func (t *Term) cell() (int, int) {
	if t.CellW > 0 && t.CellH > 0 {
		return t.CellW, t.CellH
	}
	return defaultCellW, defaultCellH
}

// ShowPicture draws a picture in at most rows text rows, on lines of its
// own: the rows it took (0: none shown; its Alt is then the caller's to
// say).
func (t *Term) ShowPicture(p *view.Picture, rows int) int {
	mode := t.PictureMode()
	if mode == picNone || p == nil || p.Load == nil || rows < 1 {
		return 0
	}
	cw, ch := t.cell()
	cols := max(t.Cols-1, 10)
	maxW, maxH := cols*cw, rows*ch
	if mode == picBlocks {
		maxW, maxH = cols, rows*2 // a cell is a pixel's width, two pixels' height
	}
	img, err := p.Load(maxW, maxH)
	if err != nil || img == nil {
		return 0
	}
	img = fitImage(img, maxW, maxH)
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	used := (h + ch - 1) / ch
	switch mode {
	case picSixel:
		data, err := sixel.Encode(img)
		if err != nil {
			return 0
		}
		t.w.Write(data)
		t.w.WriteString("\r\n")
	case picKitty, picITerm:
		var b bytes.Buffer
		if png.Encode(&b, img) != nil {
			return 0
		}
		c, r := (w+cw-1)/cw, used
		enc := base64.StdEncoding.EncodeToString(b.Bytes())
		if mode == picITerm {
			fmt.Fprintf(t.w, "\x1b]1337;File=inline=1;size=%d;width=%d;height=%d;preserveAspectRatio=1:%s\a\r\n",
				b.Len(), c, r, enc)
			break
		}
		// kitty: the PNG in pieces of 4096, the last saying so
		for i := 0; i < len(enc); i += 4096 {
			end, more := min(i+4096, len(enc)), 1
			if end == len(enc) {
				more = 0
			}
			if i == 0 {
				fmt.Fprintf(t.w, "\x1b_Ga=T,f=100,q=2,c=%d,r=%d,m=%d;%s\x1b\\", c, r, more, enc[i:end])
			} else {
				fmt.Fprintf(t.w, "\x1b_Gm=%d;%s\x1b\\", more, enc[i:end])
			}
		}
		t.w.WriteString("\r\n")
	case picBlocks:
		used = (h + 1) / 2
		t.w.WriteString(blocks(img))
	}
	t.sinceKey += used
	return used
}

// fitImage scales an image down to fit maxW by maxH (never up).
func fitImage(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxW && h <= maxH {
		return img
	}
	if w*maxH > h*maxW {
		w, h = maxW, max(1, h*maxW/w)
	} else {
		w, h = max(1, w*maxH/h), maxH
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	draw.BiLinear.Scale(out, out.Bounds(), img, b, draw.Over, nil)
	return out
}

// blocks is an image as lines of upper half blocks: each cell two pixels,
// the upper its foreground, the lower its background, in the 256 colours.
func blocks(img image.Image) string {
	b := img.Bounds()
	var s strings.Builder
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		lastFG, lastBG := -1, -1
		for x := b.Min.X; x < b.Max.X; x++ {
			fg := color256(img.At(x, y))
			bg := 16 // black under an odd last row
			if y+1 < b.Max.Y {
				bg = color256(img.At(x, y+1))
			}
			if fg != lastFG || bg != lastBG {
				fmt.Fprintf(&s, "\x1b[38;5;%d;48;5;%dm", fg, bg)
				lastFG, lastBG = fg, bg
			}
			s.WriteString("▀")
		}
		s.WriteString("\x1b[0m\r\n")
	}
	return s.String()
}

// color256 is the nearest of xterm's 256 colours: its 6x6x6 cube, or its
// greys.
func color256(c color.Color) int {
	r, g, b, _ := c.RGBA()
	r8, g8, b8 := int(r>>8), int(g>>8), int(b>>8)
	cube := func(v int) int {
		if v < 48 {
			return 0
		}
		if v < 115 {
			return 1
		}
		return (v - 35) / 40
	}
	level := []int{0, 95, 135, 175, 215, 255}
	cr, cg, cb := cube(r8), cube(g8), cube(b8)
	ci := 16 + 36*cr + 6*cg + cb
	dist := func(a, b, c int) int {
		return (a-r8)*(a-r8) + (b-g8)*(b-g8) + (c-b8)*(c-b8)
	}
	cd := dist(level[cr], level[cg], level[cb])
	avg := (r8 + g8 + b8) / 3
	gi := max(0, min(23, (avg-3)/10))
	gv := 8 + gi*10
	if dist(gv, gv, gv) < cd {
		return 232 + gi
	}
	return ci
}
