package imageboard

import (
	"errors"
	"image"
	"image/color"
	"os"

	"golang.org/x/image/draw"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// For the terminals: the BBS's screens post with Submit, and show the
// pictures with Picture (bbs.ImageBoards).

// Picture is a post's image to fit in maxW by maxH pixels: its thumbnail
// (or, spoilered, the spoiler's), or full, the image itself.  The image is
// read only when the thumbnail is smaller than the room.
func (s *Server) Picture(b *store.Board, p *store.BoardPost, full bool, maxW, maxH int) (image.Image, error) {
	if !p.HasFile() {
		return nil, errors.New("the post has no image")
	}
	maxW, maxH = max(maxW, 1), max(maxH, 1)
	if !full && p.Spoiler {
		if s.spoiler == nil {
			s.spoiler = spoilerImage()
		}
		return decode(s.spoiler, ".png")
	}
	ww, wh := fitIn(p.W, p.H, maxW, maxH)
	var img image.Image
	if !full || (ww <= p.TnW && wh <= p.TnH) {
		data, err := os.ReadFile(s.thumbPath(b.Dir, p.Tim))
		if err != nil {
			return nil, err
		}
		if img, err = decode(data, ".jpg"); err != nil {
			return nil, err
		}
		if !full {
			ww, wh = fitIn(p.TnW, p.TnH, maxW, maxH)
		}
	} else {
		data, err := os.ReadFile(s.srcPath(b.Dir, p.Tim, p.Ext))
		if err != nil {
			return nil, err
		}
		if img, err = decode(data, p.Ext); err != nil {
			return nil, err
		}
	}
	if b := img.Bounds(); b.Dx() == ww && b.Dy() == wh {
		return img, nil
	}
	out := image.NewRGBA(image.Rect(0, 0, ww, wh))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.BiLinear.Scale(out, out.Bounds(), img, img.Bounds(), draw.Over, nil)
	return out, nil
}

// fitIn is w by h made to fit in maxW by maxH, never larger.
func fitIn(w, h, maxW, maxH int) (int, int) {
	if w <= maxW && h <= maxH {
		return w, h
	}
	if w*maxH > h*maxW { // wider than the room
		return maxW, max(1, h*maxW/w)
	}
	return max(1, w*maxH/h), maxH
}
