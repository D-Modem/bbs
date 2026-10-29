package textui

import (
	"image"
	"image/color"

	"github.com/D-Modem/bbs/internal/logo"
	"github.com/D-Modem/bbs/internal/sixel"
)

// the cell assumed when the terminal does not say
const (
	defaultCellW = 10
	defaultCellH = 20
	logoRows     = 6 // the most text rows the logo takes
)

// cyan is the name logo's color
var cyan = color.NRGBA{0x00, 0xb4, 0xff, 0xff}

// sixelLogo is the logo as sixel graphics for t: the PNG in file, or name
// in a pixel font two text rows high; nil if there is none to show.
func sixelLogo(t *Term, name, file string) []byte {
	cw, ch := t.CellW, t.CellH
	if cw == 0 || ch == 0 {
		cw, ch = defaultCellW, defaultCellH
	}
	var img image.Image
	if file != "" {
		src, err := logo.Load(file)
		if err != nil {
			return nil
		}
		img = src
	} else {
		mono := logo.Name(name, 2*ch)
		col := image.NewNRGBA(mono.Bounds())
		for y := 0; y < mono.Bounds().Dy(); y++ {
			for x := 0; x < mono.Bounds().Dx(); x++ {
				if mono.GrayAt(x, y).Y >= 128 {
					col.SetNRGBA(x, y, cyan)
				}
			}
		}
		img = col
	}
	img = shrink(img, (t.Cols-1)*cw, logoRows*ch)
	data, err := sixel.Encode(img)
	if err != nil {
		return nil
	}
	return data
}

// shrink scales img down by a whole factor to fit maxW by maxH.
func shrink(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	f := 1
	for b.Dx()/f > maxW || b.Dy()/f > maxH {
		f++
	}
	if f == 1 {
		return img
	}
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx()/f, b.Dy()/f))
	for y := 0; y < out.Bounds().Dy(); y++ {
		for x := 0; x < out.Bounds().Dx(); x++ {
			out.Set(x, y, img.At(b.Min.X+x*f, b.Min.Y+y*f))
		}
	}
	return out
}
