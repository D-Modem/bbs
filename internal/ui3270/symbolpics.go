package ui3270

// Pictures in programmed symbols: the picture cut into character cells,
// each a symbol loaded into the terminal's symbol sets before the screen.
// A set holds 190 symbols (code points 0x41 to 0xFE), so the picture is
// made as large as its cells fit in the sets there are; a blank cell needs
// none, cells alike share one.
//
//   - triple-plane (color) sets, where the terminal takes the extended
//     Load PS that loads a plane: a cell's pels in any of 8 colors, its
//     blue, red and green planes added (PCOMM with programmed symbols
//     alone has 3 such sets); a cell of one color and black goes in a
//     single-plane set instead, while there is room, its color a Set
//     Attribute
//   - single-plane sets alone: each cell one of the 7 colors and a symbol
//     of its pels on or off

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/racingmars/go3270"
)

// pictureSets are the symbol sets pictures can be loaded into: the
// loadable ones (after the logon screen the logo's set is free).
func (u *ui) pictureSets() []go3270.SymbolSet {
	if _, _, _, _, ok := symbolCell(u.dev); !ok {
		return nil
	}
	var out []go3270.SymbolSet
	for _, s := range u.dev.SymbolSets().LoadableSets() {
		if !s.DBCS {
			out = append(out, s)
		}
	}
	return out
}

// colorSets is whether triple-plane sets can be loaded a plane at a time
// (the extended form of Load PS).
func (u *ui) colorSets() bool {
	if !u.dev.SymbolSets().Extended {
		return false
	}
	for _, s := range u.pictureSets() {
		if s.Triple {
			return true
		}
	}
	return false
}

// a cell of a symbol picture: its set (an index into the sets loaded, -1:
// none, a blank), code point and color attribute
type symCell struct {
	set   int
	code  byte
	color go3270.Color
}

// symbolPicture lays img out in programmed symbols from row, col in at
// most rows rows and cols columns: the fields, the rows taken, and the
// Load PS structured fields to send before the screen.
func (u *ui) symbolPicture(img image.Image, row, col, rows, cols int) (go3270.Screen, int, [][]byte, error) {
	var grid [][]symCell
	var loads [][]byte
	var err error
	if u.colorSets() {
		grid, loads, err = u.colorSymbols(img, rows, cols)
	} else {
		grid, loads, err = u.monoSymbols(img, rows, cols)
	}
	if err != nil || len(loads) == 0 {
		return nil, 0, nil, err // all black: said in words
	}
	return symbolFields(grid, row, col), len(grid), loads, nil
}

// symbolFields is a symbol picture's fields: a row's runs of a set and a
// color, each a Set Attribute of both (a blank cell goes in any run), and
// the attributes set back after it.
func symbolFields(grid [][]symCell, row, col int) go3270.Screen {
	var scr go3270.Screen
	for r, cells := range grid {
		var run []byte
		runAt, runSet, runColor := 0, -1, go3270.DefaultColor
		flush := func() {
			if len(run) > 0 {
				scr = append(scr, go3270.Field{Row: row + r, Col: col + runAt, AttributeOnly: true,
					Color: runColor, CharSet: byte(pictureLCID + max(runSet, 0)), Raw: run})
			}
			run = nil
		}
		for c, g := range cells {
			if g.set >= 0 && (g.set != runSet || g.color != runColor) && len(run) > 0 {
				flush()
			}
			if len(run) == 0 {
				runAt = c
			}
			if g.set >= 0 {
				runSet, runColor = g.set, g.color
			}
			run = append(run, g.code)
		}
		flush()
	}
	return append(scr, resetAttributes(row+len(grid)-1, col+len(grid[0]), true))
}

// fitCells is img's size in pels, made to fit cols by rows cells of cw by
// ch, and then smaller by k.
func fitCells(img image.Image, cw, ch, rows, cols int, k float64) (int, int) {
	w, h := fitSize(img.Bounds().Dx(), img.Bounds().Dy(), cols*cw, rows*ch)
	return max(1, int(float64(w)*k)), max(1, int(float64(h)*k))
}

// canvasOf is img scaled to w by h, on black cells of cw by ch.
func canvasOf(img image.Image, w, h, cw, ch int) *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, cells(w, cw)*cw, cells(h, ch)*ch))
	draw := scale(img, w, h)
	for y := 0; y < canvas.Bounds().Dy(); y++ {
		for x := 0; x < canvas.Bounds().Dx(); x++ {
			if x < w && y < h {
				canvas.SetRGBA(x, y, draw.RGBAAt(x, y))
			} else {
				canvas.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
	}
	return canvas
}

// shape is the cell at r, c of a grid of pels as a symbol: its pels for
// which on is true.
func shape(r, c, cw, ch int, on func(x, y int) bool) (go3270.Symbol, bool) {
	s := make(go3270.Symbol, ch)
	blank := true
	for y := 0; y < ch; y++ {
		var b strings.Builder
		for x := 0; x < cw; x++ {
			if on(c*cw+x, r*ch+y) {
				b.WriteByte('#')
				blank = false
			} else {
				b.WriteByte('.')
			}
		}
		s[y] = b.String()
	}
	return s, blank
}

// monoSymbols: each cell one of the 7 colors (the one that, with black,
// comes nearest its pels) and its pels on or off, in the single-plane
// sets (and triple-plane ones loaded on all their planes).
func (u *ui) monoSymbols(img image.Image, rows, cols int) ([][]symCell, [][]byte, error) {
	cw, ch, format, ext, _ := symbolCell(u.dev)
	sets := u.pictureSets()
	budget := len(sets) * symbolsASet
	// as large as fits the screen's room and the symbols there are
	var w, h int
	for k := 1.0; ; k *= 0.95 {
		if w, h = fitCells(img, cw, ch, rows, cols, k); cells(w, cw)*cells(h, ch) <= budget {
			break
		}
	}
	canvas := canvasOf(img, w, h, cw, ch)
	colors, on := ditherCells(canvas, cw, ch)
	cx, cy := len(colors[0]), len(colors)
	seen := map[string]symCell{}
	var symbols [][]go3270.Symbol
	grid := make([][]symCell, cy)
	for r := range grid {
		grid[r] = make([]symCell, cx)
		for c := range grid[r] {
			s, blank := shape(r, c, cw, ch, func(x, y int) bool { return on[y][x] })
			if blank {
				grid[r][c] = symCell{set: -1, code: 0x40}
				continue
			}
			key := strings.Join(s, "/")
			got, ok := seen[key]
			if !ok {
				n := len(seen)
				got = symCell{set: n / symbolsASet, code: byte(0x41 + n%symbolsASet)}
				if got.set >= len(symbols) {
					symbols = append(symbols, nil)
				}
				symbols[got.set] = append(symbols[got.set], s)
				seen[key] = got
			}
			got.color = colors[r][c]
			grid[r][c] = got
		}
	}
	var loads [][]byte
	for i, ss := range symbols {
		sf, err := go3270.LoadPS{RWS: sets[i].RWS, LCID: byte(pictureLCID + i), First: 0x41, Clear: true,
			Format: format, Width: cw, Height: ch, Extended: ext, Symbols: ss}.StructuredField()
		if err != nil {
			return nil, nil, err
		}
		loads = append(loads, sf)
	}
	return grid, loads, nil
}

// colorSymbols: the picture's pels in the 8 colors of the planes; a cell
// of one color (and black) a symbol in a single-plane set, its color a Set
// Attribute, while there is room, every other cell a symbol in a
// triple-plane set, its planes loaded one by one, shown neutral (its
// planes' colors added).
func (u *ui) colorSymbols(img image.Image, rows, cols int) ([][]symCell, [][]byte, error) {
	cw, ch, format, ext, _ := symbolCell(u.dev)
	var triples, singles []go3270.SymbolSet
	for _, s := range u.pictureSets() {
		if s.Triple {
			triples = append(triples, s)
		} else {
			singles = append(singles, s)
		}
	}
	tripleRoom, singleRoom := len(triples)*symbolsASet, len(singles)*symbolsASet

	// a cell: blank, one color (its shape), or several (its three planes)
	type cellKind struct {
		blank  bool
		color  int // 1 to 7, for a cell of one color
		key    string
		planes [3]go3270.Symbol // blue, red, green
	}
	var kinds [][]cellKind
	var cx, cy int
	for k := 1.0; ; k *= 0.95 {
		w, h := fitCells(img, cw, ch, rows, cols, k)
		canvas := canvasOf(img, w, h, cw, ch)
		pels := ditherPels(canvas)
		cx, cy = canvas.Bounds().Dx()/cw, canvas.Bounds().Dy()/ch
		kinds = make([][]cellKind, cy)
		ones, multis := map[string]bool{}, map[string]bool{}
		for r := 0; r < cy; r++ {
			kinds[r] = make([]cellKind, cx)
			for c := 0; c < cx; c++ {
				used := 0
				for y := r * ch; y < (r+1)*ch; y++ {
					for x := c * cw; x < (c+1)*cw; x++ {
						used |= 1 << pels[y][x]
					}
				}
				used &^= 1 // black
				var kd cellKind
				for i, plane := range []uint8{1, 2, 4} {
					kd.planes[i], _ = shape(r, c, cw, ch, func(x, y int) bool { return pels[y][x]&plane != 0 })
				}
				switch {
				case used == 0:
					kd.blank = true
				case used&(used-1) == 0: // one color
					for kd.color = 1; used != 1<<kd.color; kd.color++ {
					}
					s, _ := shape(r, c, cw, ch, func(x, y int) bool { return pels[y][x] != 0 })
					kd.planes = [3]go3270.Symbol{s}
					kd.key = strings.Join(s, "/")
					ones[kd.key] = true
				default:
					kd.key = strings.Join(kd.planes[0], "/") + "|" + strings.Join(kd.planes[1], "/") + "|" +
						strings.Join(kd.planes[2], "/")
					multis[kd.key] = true
				}
				kinds[r][c] = kd
			}
		}
		// the cells of one color that do not fit the single-plane sets go
		// in the triple-plane ones too
		if len(multis) <= tripleRoom && len(ones) <= singleRoom+tripleRoom-len(multis) {
			break
		}
	}

	// the sets loaded: the triple-plane ones, then the single-plane ones
	sets := append(append([]go3270.SymbolSet{}, triples...), singles...)
	type slot struct {
		set  int
		code byte
	}
	var tripleSyms [][3][]go3270.Symbol // a set's planes
	var singleSyms [][]go3270.Symbol
	add3 := func(planes [3]go3270.Symbol) slot {
		n := 0
		for _, t := range tripleSyms {
			n += len(t[0])
		}
		s := slot{set: n / symbolsASet, code: byte(0x41 + n%symbolsASet)}
		if s.set >= len(tripleSyms) {
			tripleSyms = append(tripleSyms, [3][]go3270.Symbol{})
		}
		for i := range planes {
			tripleSyms[s.set][i] = append(tripleSyms[s.set][i], planes[i])
		}
		return s
	}
	multiSlot, oneSlot := map[string]slot{}, map[string]slot{}
	grid := make([][]symCell, cy)
	// the cells of several colors first: they need the triple-plane sets
	for r := range kinds {
		for _, kd := range kinds[r] {
			if !kd.blank && kd.color == 0 {
				if _, ok := multiSlot[kd.key]; !ok {
					multiSlot[kd.key] = add3(kd.planes)
				}
			}
		}
	}
	for r := range kinds {
		grid[r] = make([]symCell, cx)
		for c, kd := range kinds[r] {
			switch {
			case kd.blank:
				grid[r][c] = symCell{set: -1, code: 0x40}
			case kd.color == 0:
				s := multiSlot[kd.key]
				grid[r][c] = symCell{set: s.set, code: s.code, color: go3270.White} // neutral: the planes' colors
			default:
				s, ok := oneSlot[kd.key]
				if !ok {
					n := 0
					for _, ss := range singleSyms {
						n += len(ss)
					}
					if n < singleRoom {
						s = slot{set: len(triples) + n/symbolsASet, code: byte(0x41 + n%symbolsASet)}
						if n/symbolsASet >= len(singleSyms) {
							singleSyms = append(singleSyms, nil)
						}
						singleSyms[n/symbolsASet] = append(singleSyms[n/symbolsASet], kd.planes[0])
					} else {
						// in a triple-plane set: the shape in its color's planes
						var planes [3]go3270.Symbol
						blank := make(go3270.Symbol, ch)
						for y := range blank {
							blank[y] = strings.Repeat(".", cw)
						}
						for i, plane := range []int{1, 2, 4} {
							planes[i] = blank
							if kd.color&plane != 0 {
								planes[i] = kd.planes[0]
							}
						}
						s = add3(planes)
					}
					oneSlot[kd.key] = s
				}
				clr := go3270.White // neutral, in a triple-plane set: its planes
				if s.set >= len(triples) {
					clr = go3270.Color(0xf0 + kd.color)
				}
				grid[r][c] = symCell{set: s.set, code: s.code, color: clr}
			}
		}
	}

	var loads [][]byte
	for i, t := range tripleSyms {
		for p, plane := range []byte{go3270.PlaneBlue, go3270.PlaneRed, go3270.PlaneGreen} {
			sf, err := go3270.LoadPS{RWS: sets[i].RWS, LCID: byte(pictureLCID + i), First: 0x41, Clear: p == 0,
				Format: format, Width: cw, Height: ch, Extended: ext, Plane: plane, Symbols: t[p]}.StructuredField()
			if err != nil {
				return nil, nil, err
			}
			loads = append(loads, sf)
		}
	}
	for i, ss := range singleSyms {
		set := len(triples) + i
		sf, err := go3270.LoadPS{RWS: sets[set].RWS, LCID: byte(pictureLCID + set), First: 0x41, Clear: true,
			Format: format, Width: cw, Height: ch, Extended: ext, Symbols: ss}.StructuredField()
		if err != nil {
			return nil, nil, err
		}
		loads = append(loads, sf)
	}
	return grid, loads, nil
}

// cells is how many cells of size n it takes to cover v.
func cells(v, n int) int { return (v + n - 1) / n }

// planeColors are the 8 colors of the planes, by their bits: 1 blue, 2
// red, 4 green.
var planeColors = [8]color.RGBA{{0, 0, 0, 255}, {0, 0, 255, 255}, {255, 0, 0, 255}, {255, 0, 255, 255},
	{0, 255, 0, 255}, {0, 255, 255, 255}, {255, 255, 0, 255}, {255, 255, 255, 255}}

// ditherPels is img's pels in the 8 colors of the planes (Floyd-Steinberg),
// each its planes' bits.
func ditherPels(img *image.RGBA) [][]uint8 {
	W, H := img.Bounds().Dx(), img.Bounds().Dy()
	type acc [3]float64
	cur, next := make([]acc, W+2), make([]acc, W+2)
	out := make([][]uint8, H)
	for y := 0; y < H; y++ {
		out[y] = make([]uint8, W)
		for x := 0; x < W; x++ {
			p := img.RGBAAt(x, y)
			cur[x+1][0] += float64(p.R)
			cur[x+1][1] += float64(p.G)
			cur[x+1][2] += float64(p.B)
		}
		for x := 0; x < W; x++ {
			want := cur[x+1]
			best, bestD := 0, math.MaxFloat64
			for i, c := range planeColors {
				dr, dg, db := want[0]-float64(c.R), want[1]-float64(c.G), want[2]-float64(c.B)
				if d := 2*dr*dr + 4*dg*dg + 3*db*db; d < bestD {
					best, bestD = i, d
				}
			}
			out[y][x] = uint8(best)
			c := planeColors[best]
			e := acc{want[0] - float64(c.R), want[1] - float64(c.G), want[2] - float64(c.B)}
			for k := 0; k < 3; k++ {
				cur[x+2][k] += e[k] * 7 / 16
				next[x][k] += e[k] * 3 / 16
				next[x+1][k] += e[k] * 5 / 16
				next[x+2][k] += e[k] * 1 / 16
			}
		}
		cur, next = next, make([]acc, W+2)
	}
	return out
}

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
