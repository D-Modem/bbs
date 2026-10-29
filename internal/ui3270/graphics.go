package ui3270

// 3270 graphics, as far as the terminal has them:
//
//   - graphic escape (code page 310): line drawing and block characters,
//     on nearly every 3270 (x3270 too); plain ASCII otherwise
//   - programmed symbols: the logo, cut into character cells and loaded
//     into one of the terminal's loadable symbol sets (PCOMM, HostExplorer)
//   - vector graphics: the logo drawn as a bitmap image (PCOMM's native
//     host graphics), experimental and off unless asked for

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"github.com/racingmars/go3270"

	logopkg "github.com/D-Modem/bbs/internal/logo"
)

// Graphics are the 3270 graphics settings ([general] tn3270_*).
type Graphics struct {
	Symbols  bool   // programmed symbols, when the terminal has them
	Vector   bool   // vector graphics, when the terminal has them
	LogoFile string // a PNG for the logo, instead of the system's name
}

// the symbol set's local ID: any of 0x40 to 0xEF
const logoLCID = 0xc1

// maxLogoRows is the most character rows the logo may take.
const maxLogoRows = 6

// logo is the logo as the terminal shows it.
type logo struct {
	rows  [][]byte // programmed symbols' code points, row by row
	img   *image.Gray
	cellW int
	cellH int
}

// box is the line drawing of a terminal: code page 310's with graphic
// escape, ASCII without.
type box struct {
	h, v, tl, tr, bl, br, full, half string
}

var (
	geBox    = box{"─", "│", "┌", "┐", "└", "┘", "█", "▌"}
	asciiBox = box{"-", "|", "+", "+", "+", "+", "#", "|"}
)

// box is the terminal's line drawing: code page 310's where it has graphic
// escape, ASCII where it has not.
func (u *ui) box() box {
	if u.dev.SupportsGE() {
		return geBox
	}
	return asciiBox
}

// rule is a horizontal line n wide.
func (u *ui) rule(n int) string { return strings.Repeat(u.box().h, n) }

// bar is a bar of v out of most, at most width wide.
func (u *ui) bar(v, most, width int) string {
	if most <= 0 || v <= 0 {
		return ""
	}
	n := v * width / most
	if n == 0 {
		return u.box().half
	}
	return strings.Repeat(u.box().full, n)
}

// boxed draws a box around rows top to bottom, columns left to right.
func (u *ui) boxed(top, left, bottom, right int, color go3270.Color) go3270.Screen {
	b := u.box()
	w := right - left - 1
	scr := go3270.Screen{
		u.text(top, left, b.tl+strings.Repeat(b.h, w)+b.tr, color, false),
		u.text(bottom, left, b.bl+strings.Repeat(b.h, w)+b.br, color, false),
	}
	for r := top + 1; r < bottom; r++ {
		scr = append(scr, u.text(r, left, b.v, color, false), u.text(r, right, b.v, color, false))
	}
	return scr
}

// symbolCell is the cell the logo's symbols are drawn for, and the Load PS
// format to load them in; ok false if the terminal cannot take them.
func symbolCell(dev interface {
	SymbolSets() *go3270.SymbolSets
	CellSize() (int, int)
}) (w, h, format int, extended, ok bool) {
	s := dev.SymbolSets()
	if s == nil || len(s.LoadableSets()) == 0 {
		return 0, 0, 0, false, false
	}
	w, h = s.SlotWidth, s.SlotHeight
	if set := s.LoadableSets()[0]; set.SlotWidth > 0 && set.SlotHeight > 0 {
		w, h = set.SlotWidth, set.SlotHeight
	}
	if w == 0 || h == 0 {
		w, h = dev.CellSize()
	}
	switch {
	case w == 0 || h == 0 || w > 32 || h > 32:
		return 0, 0, 0, false, false
	case s.SupportsFormat(1) && w == 9 && h <= 16:
		// the 3278's own format: unambiguous
		return w, h, 1, false, true
	case s.SupportsFormat(3) && (w == 8 || !s.SizeMatch):
		// 8 wide (or narrower than the slot, when that is allowed):
		// a byte a row however the format is read
		if w > 8 {
			w = 8
		}
		return w, h, 3, s.Extended, true
	case s.SupportsFormat(3):
		return w, h, 3, s.Extended, true
	}
	return 0, 0, 0, false, false
}

// logoImage is the logo's picture: the PNG, or the name in a pixel font
// tall enough to fill rows character rows of cellH pels.
func logoImage(g Graphics, name string, rows, cellH int) (*image.Gray, error) {
	if g.LogoFile != "" {
		src, err := logopkg.Load(g.LogoFile)
		if err != nil {
			return nil, err
		}
		return logopkg.Mono(src), nil
	}
	return logopkg.Name(name, rows*cellH), nil
}

// cut cuts img into cells: the distinct cells' symbols (an empty cell is the
// blank, code point 0x40) and the code points of each row of cells.
func cut(img *image.Gray, cellW, cellH int) ([]go3270.Symbol, [][]byte, error) {
	cols := (img.Bounds().Dx() + cellW - 1) / cellW
	rows := (img.Bounds().Dy() + cellH - 1) / cellH
	seen := map[string]byte{}
	var symbols []go3270.Symbol
	grid := make([][]byte, rows)
	for r := 0; r < rows; r++ {
		grid[r] = make([]byte, cols)
		for c := 0; c < cols; c++ {
			sym := make(go3270.Symbol, cellH)
			blank := true
			for y := 0; y < cellH; y++ {
				var row strings.Builder
				for x := 0; x < cellW; x++ {
					px, py := c*cellW+x, r*cellH+y
					if px < img.Bounds().Dx() && py < img.Bounds().Dy() && img.GrayAt(px, py).Y >= 128 {
						row.WriteByte('#')
						blank = false
					} else {
						row.WriteByte('.')
					}
				}
				sym[y] = row.String()
			}
			if blank {
				grid[r][c] = 0x40
				continue
			}
			key := strings.Join(sym, "/")
			cp, ok := seen[key]
			if !ok {
				if len(symbols) == 0xfe-0x41+1 {
					return nil, nil, errors.New("logo: more than 190 different cells")
				}
				cp = byte(0x41 + len(symbols))
				seen[key] = cp
				symbols = append(symbols, sym)
			}
			grid[r][c] = cp
		}
	}
	return symbols, grid, nil
}

// fitLogo shrinks a too-wide or too-tall picture by whole factors.
func fitLogo(img *image.Gray, maxW, maxH int) *image.Gray {
	f := 1
	for img.Bounds().Dx()/f > maxW || img.Bounds().Dy()/f > maxH {
		f++
	}
	if f == 1 {
		return img
	}
	out := image.NewGray(image.Rect(0, 0, img.Bounds().Dx()/f, img.Bounds().Dy()/f))
	draw.Draw(out, out.Bounds(), image.Black, image.Point{}, draw.Src)
	for y := 0; y < out.Bounds().Dy(); y++ {
		for x := 0; x < out.Bounds().Dx(); x++ {
			// on if any pel of the f by f square is
			for dy := 0; dy < f; dy++ {
				for dx := 0; dx < f; dx++ {
					if img.GrayAt(x*f+dx, y*f+dy).Y >= 128 {
						out.SetGray(x, y, color.Gray{255})
					}
				}
			}
		}
	}
	return out
}

// setupGraphics loads the logo into the terminal's programmed symbols, or
// readies it for vector graphics, if the terminal and the settings allow;
// a failure only costs the logo.
func (u *ui) setupGraphics() {
	g := u.o.Graphics
	if !g.Symbols && !g.Vector {
		return
	}
	if g.Vector && u.dev.SupportsVectorGraphics() {
		w, h := u.dev.CellSize()
		if w == 0 || h == 0 {
			w, h = 9, 16
		}
		img, err := logoImage(g, u.b.Conf().Name, 2, h)
		if err != nil {
			u.b.Log.Printf("node %d: logo: %v", u.n.Num, err)
			return
		}
		img = fitLogo(img, (u.cols-4)*w, maxLogoRows*h)
		u.logo = &logo{img: img, cellW: w, cellH: h}
		return
	}
	if !g.Symbols {
		return
	}
	cw, ch, format, ext, ok := symbolCell(u.dev)
	if !ok {
		return
	}
	img, err := logoImage(g, u.b.Conf().Name, 2, ch)
	if err == nil {
		img = fitLogo(img, (u.cols-4)*cw, maxLogoRows*ch)
	}
	var symbols []go3270.Symbol
	var grid [][]byte
	if err == nil {
		symbols, grid, err = cut(img, cw, ch)
	}
	var sf []byte
	if err == nil {
		set := u.dev.SymbolSets().LoadableSets()[0]
		sf, err = go3270.LoadPS{RWS: set.RWS, LCID: logoLCID, First: 0x41, Clear: true,
			Format: format, Width: cw, Height: ch, Extended: ext, Symbols: symbols}.StructuredField()
	}
	if err == nil {
		err = go3270.WriteStructuredFields(u.g, sf)
	}
	if err != nil {
		u.b.Log.Printf("node %d: logo: %v", u.n.Num, err)
		return
	}
	u.b.Log.Printf("node %d: loaded %d programmed symbols (format %d, %dx%d) for the logo",
		u.n.Num, len(symbols), format, cw, ch)
	u.logo = &logo{rows: grid, cellW: cw, cellH: ch}
}

// logoFields puts the logo at row, col: programmed symbols' fields, or
// nothing (vector graphics are drawn after the screen, by drawLogo); the
// rows it takes.
func (u *ui) logoFields(row, col int) (go3270.Screen, int) {
	if u.logo == nil {
		return nil, 0
	}
	if u.logo.rows == nil {
		rows := (u.logo.img.Bounds().Dy() + u.logo.cellH - 1) / u.logo.cellH
		u.logoAt = [2]int{row, col}
		return nil, rows
	}
	var scr go3270.Screen
	for i, r := range u.logo.rows {
		if row+i >= u.rows-4 || col+1+len(r) >= u.cols {
			break
		}
		scr = append(scr,
			go3270.Field{Row: row + i, Col: col, CharSet: logoLCID, Raw: r, Color: go3270.Turquoise},
			go3270.Field{Row: row + i, Col: col + 1 + len(r), Autoskip: true})
	}
	return scr, len(u.logo.rows)
}

// drawLogo draws the vector logo where logoFields left room for it, once
// the screen is on the terminal.
func (u *ui) drawLogo() error {
	if u.logo == nil || u.logo.img == nil || u.logoAt[0] == 0 {
		return nil
	}
	l := u.logo
	// graphics coordinates: pels, 0,0 the middle of the screen, y upwards
	x := (u.logoAt[1]+1)*l.cellW - u.cols*l.cellW/2
	y := u.rows*l.cellH/2 - u.logoAt[0]*l.cellH
	var rows []string
	for py := 0; py < l.img.Bounds().Dy(); py++ {
		var b strings.Builder
		for px := 0; px < l.img.Bounds().Dx(); px++ {
			if l.img.GrayAt(px, py).Y >= 128 {
				b.WriteByte('#')
			} else {
				b.WriteByte('.')
			}
		}
		rows = append(rows, b.String())
	}
	sf, err := go3270.ImagePicture(x, y, go3270.Bitmap{Color: go3270.Turquoise, Rows: rows})
	if err == nil {
		err = go3270.WriteStructuredFields(u.g, sf)
	}
	if err != nil {
		return err
	}
	u.logoAt = [2]int{}
	u.drawn = true
	return nil
}

// qcodeNames are the query replies' names (GA23-0059 6.1).
var qcodeNames = map[byte]string{
	0x80: "Summary", 0x81: "Usable Area", 0x82: "Image", 0x83: "Text Partitions",
	0x84: "Alphanumeric Partitions", 0x85: "Character Sets", 0x86: "Color",
	0x87: "Highlighting", 0x88: "Reply Modes", 0x8a: "Field Validation",
	0x8b: "MSR Control", 0x8c: "Field Outlining", 0x8e: "Partition Characteristics",
	0x8f: "OEM Auxiliary Device", 0x90: "Format Presentation", 0x91: "DBCS-Asia",
	0x92: "Save/Restore Format", 0x93: "PC3270", 0x94: "Format Storage Aux Device",
	0x95: "DDM", 0x96: "Storage Pools", 0x97: "DIA", 0x98: "Data Chaining",
	0x99: "Auxiliary Device", 0x9a: "3270 IPDS", 0x9c: "Product Defined DS",
	0x9e: "IBM Auxiliary Device", 0x9f: "Begin/End of File", 0xa0: "Device Characteristics",
	0xa1: "RPQ Names", 0xa2: "Data Streams", 0xa6: "Implicit Partition",
	0xa7: "Paper Feed Techniques", 0xa8: "Transparency", 0xa9: "Settable Printer Char",
	0xaa: "IOCA Auxiliary Device", 0xab: "Cooperative Proc Requestor", 0xb0: "Segment",
	0xb1: "Procedure", 0xb2: "Line Type", 0xb3: "Port", 0xb4: "Graphic Color",
	0xb5: "Extended Drawing Routine", 0xb6: "Graphic Symbol Sets", 0xff: "Null",
}

// Capabilities describes what a 3270 can do, in a line, for the log.
func Capabilities(dev go3270.DevInfo) string {
	rows, cols := dev.AltDimensions()
	var parts []string
	parts = append(parts, fmt.Sprintf("%s %dx%d", dev.TerminalType(), cols, rows))
	if cp := dev.Codepage(); cp != nil {
		parts = append(parts, "cp "+cp.ID())
	}
	if w, h := dev.CellSize(); w > 0 {
		parts = append(parts, fmt.Sprintf("cell %dx%d", w, h))
	}
	if dev.SupportsGE() {
		parts = append(parts, "GE")
	}
	if s := dev.SymbolSets(); s != nil && s.Loadable {
		var fs []string
		for t := 1; t <= 8; t++ {
			if s.SupportsFormat(t) {
				fs = append(fs, fmt.Sprint(t))
			}
		}
		parts = append(parts, fmt.Sprintf("PS %d sets, formats %s, slot %dx%d",
			len(s.LoadableSets()), strings.Join(fs, ","), s.SlotWidth, s.SlotHeight))
	}
	if dev.SupportsVectorGraphics() {
		parts = append(parts, "vector")
	}
	var codes []string
	for _, q := range dev.QueryCodes() {
		codes = append(codes, fmt.Sprintf("%02X", q))
	}
	parts = append(parts, "replies "+strings.Join(codes, " "))
	return strings.Join(parts, ", ")
}

// terminal shows what the terminal told us: for setting graphics up, and
// for reports of what an emulator does.
func (u *ui) terminal() error {
	u.n.Doing("Terminal info")
	d := u.dev
	rows, cols := d.AltDimensions()
	yn := map[bool]string{true: "yes", false: "no"}
	cp := "default (1047)"
	if c := d.Codepage(); c != nil {
		cp = c.ID()
	}
	cw, ch := d.CellSize()
	lines := []string{
		fmt.Sprintf("Terminal type:        %s", d.TerminalType()),
		fmt.Sprintf("Screen:               %d x %d", cols, rows),
		fmt.Sprintf("Code page:            %s", cp),
		fmt.Sprintf("Character cell:       %d x %d pels", cw, ch),
		fmt.Sprintf("Graphic escape:       %s  %s", yn[d.SupportsGE()], u.rule(5)+u.box().full+u.box().half),
	}
	if s := d.SymbolSets(); s != nil {
		var fs []string
		for t := 1; t <= 8; t++ {
			if s.SupportsFormat(t) {
				fs = append(fs, fmt.Sprint(t))
			}
		}
		lines = append(lines,
			fmt.Sprintf("Programmed symbols:   %s (extended %s, slot %d x %d, size match %s, formats %s)",
				yn[s.Loadable], yn[s.Extended], s.SlotWidth, s.SlotHeight, yn[s.SizeMatch], strings.Join(fs, " ")))
		for _, set := range s.Sets {
			kind := "fixed"
			if set.Loadable {
				kind = "loadable"
			}
			if set.Triple {
				kind += ", 3 planes"
			}
			lines = append(lines, fmt.Sprintf("  set %02X: LCID %02X, %s, CGCSGID %08X", set.RWS, set.LCID, kind, set.CGCSGID))
		}
	} else {
		lines = append(lines, "Programmed symbols:   no Character Sets reply")
	}
	lines = append(lines, fmt.Sprintf("Vector graphics:      %s", yn[d.SupportsVectorGraphics()]))
	logoNow := "none"
	if u.logo != nil && u.logo.rows != nil {
		logoNow = "programmed symbols"
	} else if u.logo != nil {
		logoNow = "vector image"
	}
	lines = append(lines, fmt.Sprintf("Logo:                 %s", logoNow), "", "Query replies:")
	for _, q := range d.QueryCodes() {
		r := d.QueryReply(q)
		lines = append(lines, fmt.Sprintf("  %02X %s (%d bytes)", q, qcodeNames[q], len(r)))
		per := (u.cols - 6) / 3 // bytes a line
		for i := 0; i < len(r); i += per {
			lines = append(lines, "    "+fmt.Sprintf("% X", r[i:min(i+per, len(r))]))
		}
	}
	return u.list("Your terminal", "", lines)
}
