package ui3270

// Pictures (an image board's, view.Picture) on a 3270, the best way the
// terminal has:
//
//   - vector graphics: the picture as bitmaps in the terminal's own
//     colors (16 on IBM Personal Communications), drawn over cells kept
//     empty for it: a character, or the cursor, over a picture clears it
//     in its cell
//   - programmed symbols: the picture cut into cells, each a symbol loaded
//     into the terminal's symbol sets: in triple-plane sets each pel any
//     of 8 colors, in single-plane ones each cell one of the 7
//     (symbolpics.go): HostExplorer, Quick3270, PCOMM with only
//     programmed symbols on
//   - graphic escape's blocks (▀ ▄ ▌ ▐ █) in the 3270's 7 colors, a cell
//     two pixels by two: x3270 and nearly every color 3270
//
// and in words on the rest.  The caller can turn them off on the profile.

import (
	"image"
	"image/color"
	"strings"

	"github.com/racingmars/go3270"
	"golang.org/x/image/draw"

	"github.com/D-Modem/bbs/internal/view"
)

// Ways of showing a picture.
const (
	picNone    = ""
	picVector  = "vector"
	picSymbols = "symbols"
	picBlocks  = "blocks"
)

// the symbol sets' local IDs for pictures: 0xC2 on (the logo's is 0xC1)
const pictureLCID = 0xc2

// symbolsASet is how many symbols a set takes: code points 0x41 to 0xFE
const symbolsASet = 0xfe - 0x41 + 1

// placed is a vector picture to draw once its screen is on the terminal.
type placed struct {
	sfs [][]byte // Object Pictures
}

// laidOut is a picture as picture made it, kept for the screen's next
// showing (a message scrolled, say) rather than loaded and dithered again.
type laidOut struct {
	p                    *view.Picture
	row, col, rows, cols int
	mode                 string
	fields               go3270.Screen
	used                 int
	sfs                  [][]byte // vector graphics, drawn after the screen
	loads                [][]byte // Load PS, sent before it
}

// pictureMode is how pictures are shown on the terminal ("": they are not).
func (u *ui) pictureMode() string {
	switch {
	case u.noPictures:
		return picNone
	case u.o.Graphics.Vector && u.dev.SupportsVectorGraphics():
		return picVector
	case u.o.Graphics.Symbols && len(u.pictureSets()) > 0 && u.colorTerminal():
		return picSymbols
	case u.dev.SupportsGE() && u.colorTerminal():
		return picBlocks
	}
	return picNone
}

// colorTerminal: the terminal shows colors (a 3279, or a Color reply of
// more than the defaults).
func (u *ui) colorTerminal() bool {
	if strings.Contains(u.dev.TerminalType(), "3279") {
		return true
	}
	r := u.dev.QueryReply(0x86)
	return len(r) >= 3 && r[2] >= 8 // QCODE, flags, NP: the pairs
}

// cell is the character cell in pels, as the terminal said or a 3279's.
func (u *ui) cell() (int, int) {
	if w, h := u.dev.CellSize(); w > 0 && h > 0 {
		return w, h
	}
	return 9, 16
}

// picture lays p out with its top left at row, col, in at most rows rows
// and cols columns: the fields to add to the screen (blocks; vector
// pictures are drawn after it, by drawGraphics, over cells left empty) and
// the rows it takes; 0 rows: it is not shown, and its Alt is the caller's
// to say.
func (u *ui) picture(p *view.Picture, row, col, rows, cols int) (go3270.Screen, int) {
	mode := u.pictureMode()
	if mode == picNone || p == nil || p.Load == nil || rows < 1 || cols < 1 {
		return nil, 0
	}
	c := u.laid
	if c == nil || c.p != p || c.row != row || c.col != col || c.rows != rows || c.cols != cols || c.mode != mode {
		c = u.layOut(mode, p, row, col, rows, cols)
		c.p, c.row, c.col, c.rows, c.cols, c.mode = p, row, col, rows, cols, mode
		u.laid = c
	}
	if c.sfs != nil {
		u.pictures = append(u.pictures, placed{sfs: c.sfs})
	}
	u.loads = append(u.loads, c.loads...)
	return c.fields, c.used
}

// layOut makes p into blocks' or symbols' fields (and the symbols' Load
// PS), or vector graphics' structured fields.
func (u *ui) layOut(mode string, p *view.Picture, row, col, rows, cols int) *laidOut {
	cw, ch := u.cell()
	maxW, maxH := cols*cw, rows*ch
	img, err := p.Load(maxW, maxH)
	if err != nil || img == nil {
		return &laidOut{}
	}
	switch mode {
	case picBlocks:
		// a cell is two block pixels by two, each cw/2 by ch/2 pels: the
		// image as many of them as fit, its shape kept
		w, h := fitSize(img.Bounds().Dx(), img.Bounds().Dy()*cw/ch, 2*cols, 2*rows)
		w, h = w+w%2, h+h%2
		return &laidOut{fields: blockFields(scale(img, w, h), row, col), used: h / 2}
	case picSymbols:
		fields, used, loads, err := u.symbolPicture(img, row, col, rows, cols)
		if err != nil {
			u.b.Log.Printf("node %d: picture: %v", u.n.Num, err)
			return &laidOut{}
		}
		return &laidOut{fields: fields, used: used, loads: loads}
	}
	// fitted in pels, which are square: the shape it has
	w, h := fitSize(img.Bounds().Dx(), img.Bounds().Dy(), maxW, maxH)
	used := (h + ch - 1) / ch
	bitmaps := go3270.DitherBitmaps(scale(img, w, h), go3270.GraphicColors(u.dev))
	// graphics coordinates: pels, 0,0 the middle of the screen, y upwards
	x := col*cw - u.cols*cw/2
	y := u.rows*ch/2 - row*ch
	// a screen has one picture: its segments are numbered from 1
	sfs, _, err := go3270.ImagePictures(x, y, go3270.PictureOptions{Mix: go3270.MixOverpaint}, bitmaps...)
	if err != nil {
		u.b.Log.Printf("node %d: picture: %v", u.n.Num, err)
		return &laidOut{}
	}
	return &laidOut{used: used, sfs: sfs}
}

// fitSize is w by h made to fit maxW by maxH, its shape kept (never made
// larger).
func fitSize(w, h, maxW, maxH int) (int, int) {
	if w <= maxW && h <= maxH {
		return max(w, 1), max(h, 1)
	}
	if w*maxH > h*maxW {
		return maxW, max(1, h*maxW/w)
	}
	return max(1, w*maxH/h), maxH
}

// scale is img made w by h.
func scale(img image.Image, w, h int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.Black, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(out, out.Bounds(), img, img.Bounds(), draw.Over, nil)
	return out
}

// blockColors are the 3270's colors for blocks.
var blockColors = []struct {
	c   go3270.Color
	rgb color.RGBA
}{
	{go3270.Blue, color.RGBA{0, 0, 255, 255}}, {go3270.Red, color.RGBA{255, 0, 0, 255}},
	{go3270.Pink, color.RGBA{255, 0, 255, 255}}, {go3270.Green, color.RGBA{0, 255, 0, 255}},
	{go3270.Turquoise, color.RGBA{0, 255, 255, 255}}, {go3270.Yellow, color.RGBA{255, 255, 0, 255}},
	{go3270.White, color.RGBA{255, 255, 255, 255}},
}

// colorDist is how far apart two colors look: green counts most.
func colorDist(a, b color.RGBA) int {
	dr, dg, db := int(a.R)-int(b.R), int(a.G)-int(b.G), int(a.B)-int(b.B)
	return 2*dr*dr + 4*dg*dg + 3*db*db
}

// blocks are graphic escape's blocks by the quarters of the cell they
// fill: top left, top right, bottom left, bottom right.
var blocks = []struct {
	ch string
	on [4]bool
}{
	{"█", [4]bool{true, true, true, true}}, {"▀", [4]bool{true, true, false, false}},
	{"▄", [4]bool{false, false, true, true}}, {"▌", [4]bool{true, false, true, false}},
	{"▐", [4]bool{false, true, false, true}},
}

// blockFields draws img, two pixels a cell each way, in blocks from row,
// col: a cell is one color, or black, so each takes the color and the
// block nearest its four pixels.  Runs of a color are one Set Attribute.
func blockFields(img *image.RGBA, row, col int) go3270.Screen {
	dist := colorDist
	black := color.RGBA{0, 0, 0, 255}
	var scr go3270.Screen
	cells := img.Bounds().Dx() / 2
	for r := 0; r < img.Bounds().Dy()/2; r++ {
		var run strings.Builder
		var runColor go3270.Color
		runAt := 0
		flush := func(at int) {
			if run.Len() > 0 {
				scr = append(scr, go3270.Field{Row: row + r, Col: col + runAt, AttributeOnly: true,
					Color: runColor, Content: run.String()})
			}
			run.Reset()
			runAt = at
		}
		for c := 0; c < cells; c++ {
			q := [4]color.RGBA{img.RGBAAt(2*c, 2*r), img.RGBAAt(2*c+1, 2*r), img.RGBAAt(2*c, 2*r+1), img.RGBAAt(2*c+1, 2*r+1)}
			best, bestColor, bestD := " ", runColor, 0
			for _, s := range q {
				bestD += dist(s, black)
			}
			for _, bc := range blockColors {
				for _, b := range blocks {
					d := 0
					for i, s := range q {
						if b.on[i] {
							d += dist(s, bc.rgb)
						} else {
							d += dist(s, black)
						}
					}
					if d < bestD {
						best, bestColor, bestD = b.ch, bc.c, d
					}
				}
			}
			if best != " " && bestColor != runColor {
				flush(c)
				runColor = bestColor
			}
			if run.Len() == 0 {
				runAt = c
			}
			run.WriteString(best)
		}
		flush(0)
	}
	return append(scr, resetAttributes(row+img.Bounds().Dy()/2-1, col+cells, false))
}

// resetAttributes is a Set Attribute back to the defaults (the character
// set too, if charSet: a terminal without programmed symbols may refuse
// it), at the end of a picture: a Set Attribute lasts the rest of the
// screen's write, its text after the picture too.
func resetAttributes(row, col int, charSet bool) go3270.Field {
	return go3270.Field{Row: row, Col: col, AttributeOnly: true, DefaultCharSet: charSet}
}

// drawGraphics draws the screen's vector graphics, the logo and pictures,
// once the screen is on the terminal.
func (u *ui) drawGraphics() error {
	if err := u.drawLogo(); err != nil {
		return err
	}
	for _, p := range u.pictures {
		if err := go3270.WriteStructuredFields(u.g, p.sfs...); err != nil {
			return err
		}
		u.drawn = true
	}
	return nil
}
