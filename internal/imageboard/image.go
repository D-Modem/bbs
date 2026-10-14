package imageboard

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/webp"
)

// The images taken: what they are is found from their bytes, never their
// names.
var imageTypes = map[string]string{
	".jpg":  "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
}

const (
	maxSide    = 10000    // pixels, an image's width or height
	maxPixels  = 25 << 20 // its width times its height
	opThumb    = 250      // a thread's first image's thumbnail, at most
	replyThumb = 125      // a reply's
)

// sniff is an image's extension by its first bytes ("": not one taken).
func sniff(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xff, 0xd8, 0xff}):
		return ".jpg"
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return ".png"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return ".gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return ".webp"
	}
	return ""
}

// picture is an uploaded image made ready to keep: its bytes (a JPEG's
// metadata taken out), what it is, and its thumbnail.
type picture struct {
	Data     []byte
	Ext      string
	W, H     int
	MD5      string
	Thumb    []byte // a JPEG
	TnW, TnH int
	Replay   bool  // a drawing that carries its strokes (oekaki.go)
	DrawMS   int64 // how long it took to draw
}

var (
	errNotImage = errors.New("That file is not an image this board takes (JPEG, PNG, GIF or WebP).")
	errTooLarge = fmt.Errorf("That image is too large: at most %d by %d pixels.", maxSide, maxSide)
)

// decodeSlots limits the images decoded at once: each can take a hundred
// megabytes.
var decodeSlots = make(chan struct{}, 2)

// prepare checks an upload and makes its thumbnail, no bigger than side
// on a side, on bg (what shows through a transparent image).
func prepare(data []byte, side int, bg color.Color, strip bool) (*picture, error) {
	ext := sniff(data)
	if ext == "" {
		return nil, errNotImage
	}
	if ext == ".jpg" && strip {
		data = stripJPEG(data)
	}
	var cfg image.Config
	var err error
	switch ext {
	case ".jpg":
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case ".png":
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	case ".gif":
		cfg, err = gif.DecodeConfig(bytes.NewReader(data))
	case ".webp":
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
	}
	if err != nil {
		return nil, errNotImage
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxSide || cfg.Height > maxSide || cfg.Width*cfg.Height > maxPixels {
		return nil, errTooLarge
	}
	var replayed bool
	var drawMS int64
	if ext == ".png" {
		data, drawMS, replayed = pngReplay(data, cfg.Width, cfg.Height)
	}
	decodeSlots <- struct{}{}
	defer func() { <-decodeSlots }()
	var img image.Image
	switch ext {
	case ".jpg":
		img, err = jpeg.Decode(bytes.NewReader(data))
	case ".png":
		img, err = png.Decode(bytes.NewReader(data))
	case ".gif":
		img, err = gif.Decode(bytes.NewReader(data)) // the first frame
	case ".webp":
		img, err = webp.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, errNotImage
	}
	sum := md5.Sum(data)
	p := &picture{Data: data, Ext: ext, W: cfg.Width, H: cfg.Height, MD5: base64.StdEncoding.EncodeToString(sum[:]),
		Replay: replayed, DrawMS: drawMS}
	p.TnW, p.TnH = fit(cfg.Width, cfg.Height, side)
	tn := image.NewRGBA(image.Rect(0, 0, p.TnW, p.TnH))
	draw.Draw(tn, tn.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	draw.BiLinear.Scale(tn, tn.Bounds(), img, img.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, tn, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	p.Thumb = out.Bytes()
	return p, nil
}

// fit is w by h made to fit in side by side, never larger.
func fit(w, h, side int) (int, int) {
	if w <= side && h <= side {
		return w, h
	}
	if w >= h {
		return side, max(1, h*side/w)
	}
	return max(1, w*side/h), side
}

// stripJPEG takes a JPEG's metadata out (EXIF, with where a phone took
// it; XMP; Photoshop's; comments) and leaves the picture; a JPEG it
// cannot follow it leaves as it is.
func stripJPEG(b []byte) []byte {
	if len(b) < 4 || b[0] != 0xff || b[1] != 0xd8 {
		return b
	}
	out := []byte{0xff, 0xd8}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xff {
			return b
		}
		marker := b[i+1]
		if marker == 0xff { // fill
			i++
			continue
		}
		if marker == 0xda { // the scan: the rest is the picture
			return append(out, b[i:]...)
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			out = append(out, b[i:i+2]...)
			i += 2
			continue
		}
		n := int(b[i+2])<<8 | int(b[i+3])
		if n < 2 || i+2+n > len(b) {
			return b
		}
		switch marker {
		case 0xe1, 0xed, 0xfe: // APP1 (EXIF, XMP), APP13 (Photoshop), COM
		default:
			out = append(out, b[i:i+2+n]...)
		}
		i += 2 + n
	}
	return b
}

// spoilerImage is the thumbnail a spoilered image shows: grey, saying
// so.
func spoilerImage() []byte {
	const w, h = 100, 100
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{0x88, 0x88, 0x88, 0xff}), image.Point{}, draw.Src)
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.White), Face: basicfont.Face7x13}
	for i, line := range []string{"Spoiler", "Image"} {
		d.Dot = fixed.P((w-d.MeasureString(line).Round())/2, 46+i*16)
		d.DrawString(line)
	}
	var out bytes.Buffer
	png.Encode(&out, img)
	return out.Bytes()
}
