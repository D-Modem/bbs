package ui3270

// Pictures in programmed symbols: the picture cut into character cells,
// each cell one of the 3270's 7 colors (a Set Attribute) and a symbol of
// its pels on or off, loaded into the terminal's single-plane symbol sets
// before the screen.  A set holds 190 symbols (code points 0x41 to 0xFE),
// so the picture is made as large as its cells fit in the sets there are;
// a blank cell needs none, cells alike share one.

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/racingmars/go3270"
)

// pictureSets are the symbol sets pictures can be loaded into: the
// loadable single-plane ones (after the logon screen the logo's set is
// free).
func (u *ui) pictureSets() []go3270.SymbolSet {
	if _, _, _, _, ok := symbolCell(u.dev); !ok {
		return nil
	}
	var out []go3270.SymbolSet
	for _, s := range u.dev.SymbolSets().LoadableSets() {
		if !s.Triple && !s.DBCS {
			out = append(out, s)
		}
	}
	return out
}

// symbolPicture lays img out in programmed symbols from row, col in at
// most rows rows and cols columns: the fields, the rows taken, and the
// Load PS structured fields to send before the screen.
func (u *ui) symbolPicture(img image.Image, row, col, rows, cols int) (go3270.Screen, int, [][]byte, error) {
	cw, ch, format, ext, _ := symbolCell(u.dev)
	sets := u.pictureSets()
	budget := len(sets) * symbolsASet
	// as large as fits the screen's room and the symbols there are, its
	// shape kept
	w, h := fitSize(img.Bounds().Dx(), img.Bounds().Dy(), cols*cw, rows*ch)
	for k := 1.0; ; k *= 0.95 {
		sw, sh := max(1, int(float64(w)*k)), max(1, int(float64(h)*k))
		if cells(sw, cw)*cells(sh, ch) <= budget {
			w, h = sw, sh
			break
		}
	}
	cx, cy := cells(w, cw), cells(h, ch)
	canvas := image.NewRGBA(image.Rect(0, 0, cx*cw, cy*ch))
	src := scale(img, w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			canvas.SetRGBA(x, y, src.RGBAAt(x, y))
		}
	}
	colors, on := ditherCells(canvas, cw, ch)

	// the symbols: a blank cell is the blank (0x40), cells alike share one
	type sym struct {
		set  int
		code byte
	}
	seen := map[string]sym{}
	var symbols [][]go3270.Symbol
	grid := make([][]sym, cy)
	for r := 0; r < cy; r++ {
		grid[r] = make([]sym, cx)
		for c := 0; c < cx; c++ {
			s := make(go3270.Symbol, ch)
			blank := true
			for y := 0; y < ch; y++ {
				var b strings.Builder
				for x := 0; x < cw; x++ {
					if on[r*ch+y][c*cw+x] {
						b.WriteByte('#')
						blank = false
					} else {
						b.WriteByte('.')
					}
				}
				s[y] = b.String()
			}
			if blank {
				grid[r][c] = sym{set: -1, code: 0x40}
				continue
			}
			key := strings.Join(s, "/")
			got, ok := seen[key]
			if !ok {
				n := len(seen)
				got = sym{set: n / symbolsASet, code: byte(0x41 + n%symbolsASet)}
				if got.set >= len(symbols) {
					symbols = append(symbols, nil)
				}
				symbols[got.set] = append(symbols[got.set], s)
				seen[key] = got
			}
			grid[r][c] = got
		}
	}

	if len(symbols) == 0 {
		return nil, 0, nil, nil // all black: said in words
	}
	var loads [][]byte
	for i, ss := range symbols {
		sf, err := go3270.LoadPS{RWS: sets[i].RWS, LCID: byte(pictureLCID + i), First: 0x41, Clear: true,
			Format: format, Width: cw, Height: ch, Extended: ext, Symbols: ss}.StructuredField()
		if err != nil {
			return nil, 0, nil, err
		}
		loads = append(loads, sf)
	}

	// a row's runs of a set and a color, each a Set Attribute of both; a
	// blank cell goes in any run
	var scr go3270.Screen
	for r := 0; r < cy; r++ {
		var run []byte
		runAt, runSet, runColor := 0, -1, go3270.DefaultColor
		flush := func() {
			if len(run) > 0 {
				lcid := byte(pictureLCID)
				if runSet > 0 {
					lcid += byte(runSet)
				}
				scr = append(scr, go3270.Field{Row: row + r, Col: col + runAt, AttributeOnly: true,
					Color: runColor, CharSet: lcid, Raw: run})
			}
			run = nil
		}
		for c := 0; c < cx; c++ {
			g := grid[r][c]
			if g.set >= 0 && (g.set != runSet || colors[r][c] != runColor) && len(run) > 0 {
				flush()
			}
			if len(run) == 0 {
				runAt = c
			}
			if g.set >= 0 {
				runSet, runColor = g.set, colors[r][c]
			}
			run = append(run, g.code)
		}
		flush()
	}
	scr = append(scr, resetAttributes(row+cy-1, col+cx, true))
	return scr, cy, loads, nil
}

// cells is how many cells of size n it takes to cover v.
func cells(v, n int) int { return (v + n - 1) / n }

// ditherCells chooses each cw by ch cell of img a color of the 7 (the one
// that, with black, comes nearest its pels), then diffuses img's error
// (Floyd-Steinberg) with each pel either black or its cell's color: the
// cells' colors and the pels on.
func ditherCells(img *image.RGBA, cw, ch int) ([][]go3270.Color, [][]bool) {
	black := color.RGBA{0, 0, 0, 255}
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	cx, cy := W/cw, H/ch
	colors := make([][]go3270.Color, cy)
	rgb := make([][]color.RGBA, cy)
	for r := 0; r < cy; r++ {
		colors[r] = make([]go3270.Color, cx)
		rgb[r] = make([]color.RGBA, cx)
		for c := 0; c < cx; c++ {
			best, bestD := 0, math.MaxInt
			for i, bc := range blockColors {
				d := 0
				for y := r * ch; y < (r+1)*ch; y++ {
					for x := c * cw; x < (c+1)*cw; x++ {
						p := img.RGBAAt(x, y)
						d += min(colorDist(p, black), colorDist(p, bc.rgb))
					}
				}
				if d < bestD {
					best, bestD = i, d
				}
			}
			colors[r][c], rgb[r][c] = blockColors[best].c, blockColors[best].rgb
		}
	}
	type acc [3]float64
	cur, next := make([]acc, W+2), make([]acc, W+2)
	on := make([][]bool, H)
	for y := 0; y < H; y++ {
		on[y] = make([]bool, W)
		for x := 0; x < W; x++ {
			p := img.RGBAAt(x, y)
			cur[x+1][0] += float64(p.R)
			cur[x+1][1] += float64(p.G)
			cur[x+1][2] += float64(p.B)
		}
		for x := 0; x < W; x++ {
			want := cur[x+1]
			c := rgb[y/ch][x/cw]
			d := func(t color.RGBA) float64 {
				dr, dg, db := want[0]-float64(t.R), want[1]-float64(t.G), want[2]-float64(t.B)
				return 2*dr*dr + 4*dg*dg + 3*db*db
			}
			got := black
			if d(c) < d(black) {
				got, on[y][x] = c, true
			}
			e := acc{want[0] - float64(got.R), want[1] - float64(got.G), want[2] - float64(got.B)}
			for k := 0; k < 3; k++ {
				cur[x+2][k] += e[k] * 7 / 16
				next[x][k] += e[k] * 3 / 16
				next[x+1][k] += e[k] * 5 / 16
				next[x+2][k] += e[k] * 1 / 16
			}
		}
		cur, next = next, make([]acc, W+2)
	}
	return colors, on
}
