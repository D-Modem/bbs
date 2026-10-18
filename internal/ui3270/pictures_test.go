package ui3270

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// pcommReplies are IBM Personal Communications' query replies (from the
// QCODE on): those it sends for a Query, then those for the Query List
// asking for the rest of its Summary.
var pcommReplies = [][]string{{
	"810300005000180103a114000188087009100780", "a600000b01000050001800500018",
	"86000800f4f1f1f2f2f3f3f4f4f5f5f6f6f7f7", "870400f0f1f1f2f2f4f4", "88000102", "990000", "8400078004",
	"85f2400910700000000700000002b900250100f103c301360280ff000000000380ff00000000", "9500001f401f400101",
	"80808184858687889599a6a8b0b1b2b3b4b6",
}, {
	"a80200f0ffff", "b08002000000fc00", "b10001000000fc00064006400601fffffff0",
	"b20009000701010202030304040505060607070808", "b300030290090100030280007fff",
	"b4000400ffff00100010000000000000000100ff00000002ff0000000003ffff000000040000ff00000500ffff000006ff00ff000007ffffff00000800000000000900a00000000aff00a000000ba0a00000000c0000a000000d00a0a000000ea000a000000f60606000",
	"b6000009100000010000000012010000f0c1c1000009100002b900250000",
}}

// pcomm is a DevInfo as NegotiateTelnet makes it of PCOMM's answers.
func pcomm(t *testing.T) go3270.DevInfo {
	t.Helper()
	return pcommWith(t, pcommReplies)
}

// pcommTriple is PCOMM with programmed symbols alone (no vector
// graphics): three single-plane sets and three triple-plane ones.
func pcommTriple(t *testing.T) go3270.DevInfo {
	t.Helper()
	first := append([]string{}, pcommReplies[0]...)
	for i, r := range first {
		if strings.HasPrefix(r, "85") {
			first[i] = r + "04" + "80ff00000000" + "05" + "c0ff00000000" + "06" + "c0ff00000000" + "07" + "c0ff00000000"
		}
		if strings.HasPrefix(r, "80") {
			first[i] = "80808184858687889599a6" // no graphics replies
		}
	}
	return pcommWith(t, [][]string{first, nil})
}

func pcommWith(t *testing.T, replies [][]string) go3270.DevInfo {
	t.Helper()
	srv, cli := net.Pipe()
	t.Cleanup(func() { srv.Close(); cli.Close() })
	go func() {
		buf := make([]byte, 4096)
		var got []byte
		until := func(want []byte) {
			for !bytes.Contains(got, want) {
				n, err := cli.Read(buf)
				if err != nil {
					return
				}
				got = append(got, buf[:n]...)
			}
			got = got[bytes.Index(got, want)+len(want):]
		}
		rec := func(qs []string) []byte {
			r := []byte{0x88}
			for _, q := range qs {
				d, _ := hex.DecodeString(q)
				d = append([]byte{0x81}, d...)
				r = append(r, byte((len(d)+2)>>8), byte(len(d)+2))
				r = append(r, d...)
			}
			return append(bytes.ReplaceAll(r, []byte{0xff}, []byte{0xff, 0xff}), 0xff, 0xef)
		}
		for _, step := range [][2]string{{"fffd18", "fffb18"}, {"fffa1801fff0", "fffa1800" + hex.EncodeToString([]byte("IBM-3278-2-E")) + "fff0"},
			{"fffd19", "fffb19"}, {"fffd00", "fffb00"}, {"fffb19", "fffd19"}, {"fffb00", "fffd00"}} {
			w, _ := hex.DecodeString(step[0])
			a, _ := hex.DecodeString(step[1])
			until(w)
			cli.Write(a)
		}
		until([]byte{0x01, 0xff, 0xff, 0x02, 0xff, 0xef})
		cli.Write(rec(replies[0]))
		if replies[1] != nil {
			until([]byte{0x01, 0xff, 0xff, 0x03, 0x00})
			cli.Write(rec(replies[1]))
		}
		for {
			if _, err := cli.Read(buf); err != nil {
				return
			}
		}
	}()
	dev, err := go3270.NegotiateTelnet(srv)
	if err != nil {
		t.Fatal(err)
	}
	return dev
}

// TestVectorPicture: on PCOMM a picture is laid out in pels (its shape
// kept), in PCOMM's own colors, overpainted, its top left at the cell
// asked for; no fields (text would clear it).
func TestVectorPicture(t *testing.T) {
	dev := pcomm(t)
	u := &ui{dev: dev, rows: 24, cols: 80, o: Options{Graphics: Graphics{Vector: true}}}
	if m := u.pictureMode(); m != picVector {
		t.Fatalf("mode %q", m)
	}
	// orange, 400x100: fitted to 78 cells (702 pels) by 4 rows (64): 256x64
	img := image.NewRGBA(image.Rect(0, 0, 400, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{255, 160, 0, 255})
		}
	}
	p := &view.Picture{Load: func(w, h int) (image.Image, error) { return img, nil }}
	fields, used := u.picture(p, 5, 1, 4, 78)
	if fields != nil || used != 4 || len(u.pictures) != 1 {
		t.Fatalf("fields %v, rows %d, pictures %d", fields, used, len(u.pictures))
	}
	sf := u.pictures[0].sfs[0]
	seg := sf[19:]
	if seg[0] != 0x0c || seg[1] != 0x02 { // Set Mix: overpaint
		t.Errorf("mix % x", seg[:2])
	}
	// Set Current Position: col 1, 9 pels from the left (-360: -351), row 5
	// (80 pels) down from the top (192); Set Color 10 (orange); 256x64
	want := []byte{0x21, 4, 0xfe, 0xa1, 0x00, 0x70, 0x0a, 10, 0x91, 6, 0, 0, 1, 0, 0, 64}
	if !bytes.Equal(seg[2:2+len(want)], want) {
		t.Errorf("orders % x, want % x", seg[2:2+len(want)], want)
	}
	// laid out once: the screen shown again draws it again
	u.pictures = nil
	if _, used := u.picture(p, 5, 1, 4, 78); used != 4 || len(u.pictures) != 1 {
		t.Errorf("again: %d rows, %d pictures", used, len(u.pictures))
	}
	// off: none
	u.noPictures, u.laid = true, nil
	if _, used := u.picture(p, 5, 1, 4, 78); used != 0 {
		t.Error("off, and shown")
	}
}

// TestBlockFields: a cell takes the color and the block nearest its four
// pixels; a run of a color is one field.
func TestBlockFields(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 2))
	set := func(x, y int, c color.RGBA) { img.SetRGBA(x, y, c) }
	red, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}
	// cell 0: red top (▀); cell 1: red left (▌); cell 2: black; cell 3: blue full (█)
	set(0, 0, red)
	set(1, 0, red)
	set(2, 0, red)
	set(2, 1, red)
	for x := 6; x < 8; x++ {
		set(x, 0, blue)
		set(x, 1, blue)
	}
	for _, x := range []int{4, 5} {
		set(x, 0, color.RGBA{0, 0, 0, 255})
		set(x, 1, color.RGBA{0, 0, 0, 255})
	}
	for _, x := range []int{0, 1} {
		set(x, 1, color.RGBA{0, 0, 0, 255})
	}
	set(3, 0, color.RGBA{0, 0, 0, 255})
	set(3, 1, color.RGBA{0, 0, 0, 255})
	scr := blockFields(img, 7, 2)
	if len(scr) != 3 {
		t.Fatalf("fields %+v", scr)
	}
	// the colors set back after it, not the character set
	if f := scr[2]; !f.AttributeOnly || f.Color != go3270.DefaultColor || f.DefaultCharSet || f.Content != "" {
		t.Errorf("reset %+v", f)
	}
	if f := scr[0]; f.Row != 7 || f.Col != 2 || f.Color != go3270.Red || f.Content != "▀▌ " || !f.AttributeOnly {
		t.Errorf("first %+v", f)
	}
	if f := scr[1]; f.Col != 5 || f.Color != go3270.Blue || f.Content != "█" {
		t.Errorf("second %+v", f)
	}
}

// TestSymbolPicture: on PCOMM without vector graphics a picture is cells
// of programmed symbols: loaded before the screen into its two sets (380
// symbols at most, LCIDs 0xC2 and 0xC3), each cell a Set Attribute's
// color and set, the attributes reset after it.  What the terminal would
// show is drawn to SYMBOLS_PNG if set.
func TestSymbolPicture(t *testing.T) {
	dev := pcomm(t)
	u := &ui{dev: dev, rows: 24, cols: 80, o: Options{Graphics: Graphics{Symbols: true}}}
	if m := u.pictureMode(); m != picSymbols {
		t.Fatalf("mode %q", m)
	}
	// a disc: red to yellow left to right, blue below, on black
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			dx, dy := x-160, y-120
			switch {
			case dx*dx+dy*dy < 100*100:
				img.Set(x, y, color.RGBA{255, uint8(x * 255 / 320), 0, 255})
			case y > 200:
				img.Set(x, y, color.RGBA{0, 0, 200, 255})
			default:
				img.Set(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
	}
	p := &view.Picture{Load: func(w, h int) (image.Image, error) { return img, nil }}
	fields, used := u.picture(p, 5, 1, 12, 78)
	if used == 0 || used > 12 || len(u.loads) == 0 || len(u.loads) > 2 {
		t.Fatalf("rows %d, loads %d", used, len(u.loads))
	}
	symbols := 0
	for i, sf := range u.loads {
		// Load PS: 06, flags (clear), LCID, first code point, RWS
		if sf[0] != 0x06 || sf[2] != byte(0xc2+i) || sf[3] != 0x41 || sf[4] != byte(2+i) {
			t.Errorf("load %d: % x", i, sf[:5])
		}
		symbols += (len(sf) - 5) / 18 // format 1: 18 bytes a symbol
	}
	if symbols == 0 || symbols > 380 {
		t.Errorf("%d symbols", symbols)
	}
	last := fields[len(fields)-1]
	if !last.AttributeOnly || !last.DefaultCharSet || last.Color != go3270.DefaultColor {
		t.Errorf("no reset after it: %+v", last)
	}
	colorsSeen := map[go3270.Color]bool{}
	for _, f := range fields[:len(fields)-1] {
		if !f.AttributeOnly || f.CharSet < 0xc2 || f.CharSet > 0xc3 || len(f.Raw) == 0 || f.Row < 5 || f.Row >= 5+used {
			t.Fatalf("field %+v", f)
		}
		for _, c := range f.Raw {
			if c != 0x40 && (c < 0x41 || c > 0xfe) {
				t.Fatalf("code point %x", c)
			}
		}
		colorsSeen[f.Color] = true
	}
	for _, c := range []go3270.Color{go3270.Red, go3270.Yellow, go3270.Blue} {
		if !colorsSeen[c] {
			t.Errorf("no %x cells: %v", c, colorsSeen)
		}
	}
	if out := os.Getenv("SYMBOLS_PNG"); out != "" {
		drawSymbols(t, out, fields[:len(fields)-1], u.loads)
	}
}

// drawSymbols draws symbol fields as the terminal would, 2x, to a PNG.
func drawSymbols(t *testing.T, name string, fields go3270.Screen, loads [][]byte) {
	syms := map[[2]byte][]byte{} // LCID, code point: 18 bytes of format 1
	for _, sf := range loads {
		data := sf[5:]
		for i := 0; i+18 <= len(data); i += 18 {
			syms[[2]byte{sf[2], byte(0x41 + i/18)}] = data[i : i+18]
		}
	}
	rgb := map[go3270.Color]color.RGBA{}
	for _, bc := range blockColors {
		rgb[bc.c] = bc.rgb
	}
	out := image.NewRGBA(image.Rect(0, 0, 80*9*2, 24*16*2))
	for _, f := range fields {
		for i, c := range f.Raw {
			s, ok := syms[[2]byte{f.CharSet, c}]
			if !ok {
				continue
			}
			x0, y0 := (f.Col+i)*9, f.Row*16
			for y := 0; y < 16; y++ {
				for x := 0; x < 9; x++ {
					// format 1: 2 bytes of column 0's 16 bits, then a byte a row of columns 1 to 8
					var bit bool
					if x == 0 {
						bit = (uint16(s[0])<<8|uint16(s[1]))&(0x8000>>y) != 0
					} else {
						bit = s[2+y]&(0x80>>(x-1)) != 0
					}
					if bit {
						for d := 0; d < 4; d++ {
							out.SetRGBA(2*(x0+x)+d%2, 2*(y0+y)+d/2, rgb[f.Color])
						}
					}
				}
			}
		}
	}
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, out)
}

// TestColorSymbolPicture: with triple-plane sets a cell's pels are in any
// of 8 colors: cells of several colors in the triple-plane sets, each
// loaded a plane at a time (blue, red, green; cleared with the first), and
// shown neutral; cells of one color in the single-plane sets, in their
// color.  What the terminal would show is drawn to COLOR_SYMBOLS_PNG.
func TestColorSymbolPicture(t *testing.T) {
	dev := pcommTriple(t)
	u := &ui{dev: dev, rows: 24, cols: 80, o: Options{Graphics: Graphics{Symbols: true, Vector: true}}}
	if m := u.pictureMode(); m != picSymbols || !u.colorSets() {
		t.Fatalf("mode %q, color sets %v", m, u.colorSets())
	}
	// a rainbow, left to right, darker downwards, and a white disc
	img := image.NewRGBA(image.Rect(0, 0, 320, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 320; x++ {
			h := float64(x) / 320 * 6
			f := h - float64(int(h))
			v := 255 - y
			q, tt := uint8(float64(v)*(1-f)), uint8(float64(v)*f)
			c := [6]color.RGBA{{uint8(v), tt, 0, 255}, {q, uint8(v), 0, 255}, {0, uint8(v), tt, 255},
				{0, q, uint8(v), 255}, {tt, 0, uint8(v), 255}, {uint8(v), 0, q, 255}}[int(h)%6]
			if dx, dy := x-250, y-60; dx*dx+dy*dy < 30*30 {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	p := &view.Picture{Load: func(w, h int) (image.Image, error) { return img, nil }}
	fields, used := u.picture(p, 5, 1, 12, 78)
	if used == 0 {
		t.Fatal("not shown")
	}
	planes := map[byte][]byte{} // LCID: the planes loaded, in order
	for _, sf := range u.loads {
		lcid := sf[2]
		if sf[1]&0x80 != 0 && sf[5] == 6 { // extended, through the color plane
			planes[lcid] = append(planes[lcid], sf[10])
			if (sf[1]&0x40 != 0) != (sf[10] == go3270.PlaneBlue) {
				t.Errorf("LCID %x plane %d: clear %v", lcid, sf[10], sf[1]&0x40 != 0)
			}
		}
	}
	if len(planes) == 0 {
		t.Fatalf("no plane loaded: %d loads", len(u.loads))
	}
	for lcid, ps := range planes {
		if !bytes.Equal(ps, []byte{1, 2, 4}) || lcid < 0xc2 || lcid > 0xc4 {
			t.Errorf("LCID %x: planes %v", lcid, ps)
		}
	}
	neutral, single := 0, 0
	for _, f := range fields[:len(fields)-1] {
		if planes[f.CharSet] != nil {
			if f.Color != go3270.White {
				t.Errorf("a triple-plane run not neutral: %+v", f)
			}
			neutral++
		} else {
			single++
		}
	}
	if neutral == 0 {
		t.Errorf("%d neutral runs, %d single", neutral, single)
	}
	if out := os.Getenv("COLOR_SYMBOLS_PNG"); out != "" {
		drawColorSymbols(t, out, fields[:len(fields)-1], u.loads)
	}
}

// drawColorSymbols draws symbol fields as the terminal would, triple-plane
// sets' cells in their planes' colors, 2x, to a PNG.
func drawColorSymbols(t *testing.T, name string, fields go3270.Screen, loads [][]byte) {
	type key struct {
		lcid, code, plane byte
	}
	syms := map[key][]byte{}
	triple := map[byte]bool{}
	for _, sf := range loads {
		plane, data := byte(0), sf[5:]
		if sf[1]&0x80 != 0 {
			plane, data = sf[10], sf[5+int(sf[5]):]
			triple[sf[2]] = true
		}
		for i := 0; i+18 <= len(data); i += 18 {
			syms[key{sf[2], byte(0x41 + i/18), plane}] = data[i : i+18]
		}
	}
	bit := func(s []byte, x, y int) bool {
		if x == 0 {
			return (uint16(s[0])<<8|uint16(s[1]))&(0x8000>>y) != 0
		}
		return s[2+y]&(0x80>>(x-1)) != 0
	}
	rgb := map[go3270.Color]color.RGBA{}
	for _, bc := range blockColors {
		rgb[bc.c] = bc.rgb
	}
	out := image.NewRGBA(image.Rect(0, 0, 80*9*2, 24*16*2))
	for _, f := range fields {
		for i, c := range f.Raw {
			x0, y0 := (f.Col+i)*9, f.Row*16
			for y := 0; y < 16; y++ {
				for x := 0; x < 9; x++ {
					var px color.RGBA
					if triple[f.CharSet] {
						v := 0
						for _, p := range []byte{1, 2, 4} {
							if s, ok := syms[key{f.CharSet, c, p}]; ok && bit(s, x, y) {
								v |= int(p)
							}
						}
						px = planeColors[v]
					} else if s, ok := syms[key{f.CharSet, c, 0}]; ok && bit(s, x, y) {
						px = rgb[f.Color]
					}
					for d := 0; d < 4; d++ {
						out.SetRGBA(2*(x0+x)+d%2, 2*(y0+y)+d/2, px)
					}
				}
			}
		}
	}
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, out)
}
