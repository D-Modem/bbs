// Package logo is the BBS's logo as a picture: a PNG the sysop gives, or
// the system's name drawn in a pixel font.  The 3270 and sixel front ends
// each show it their own way.
package logo

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Largest picture taken from a file.
const (
	MaxWidth  = 1000
	MaxHeight = 400
)

// Load reads a PNG logo.
func Load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	// look at the size before decoding: a small file can claim a huge one
	if cfg.Width > MaxWidth || cfg.Height > MaxHeight {
		return nil, errors.New("logo: larger than 1000x400")
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	return png.Decode(f)
}

// On reports whether a pel of a picture is drawn: opaque and not near
// white (dark on white, or anything on transparent).
func On(c color.Color) bool {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	lum := (299*int(n.R) + 587*int(n.G) + 114*int(n.B)) / 1000
	return n.A >= 128 && lum < 200
}

// Mono is a picture as on (255) and off (0) pels.
func Mono(src image.Image) *image.Gray {
	b := src.Bounds()
	img := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if On(src.At(b.Min.X+x, b.Min.Y+y)) {
				img.SetGray(x, y, color.Gray{255})
			}
		}
	}
	return img
}

// Name draws text in a 7x13 pixel font, scaled by a whole factor to be
// about height pels high (at least 13): on pels 255, off 0.
func Name(text string, height int) *image.Gray {
	n := len([]rune(text))
	small := image.NewGray(image.Rect(0, 0, 7*n, 13))
	d := font.Drawer{Dst: small, Src: image.White, Face: basicfont.Face7x13, Dot: fixed.P(0, 11)}
	d.DrawString(text)
	scale := max(1, height/13)
	img := image.NewGray(image.Rect(0, 0, 7*n*scale, 13*scale))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.SetGray(x, y, small.GrayAt(x/scale, y/scale))
		}
	}
	return img
}
