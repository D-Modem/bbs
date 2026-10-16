package ui3270

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/color"
	"net"
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
		cli.Write(rec(pcommReplies[0]))
		until([]byte{0x01, 0xff, 0xff, 0x03, 0x00})
		cli.Write(rec(pcommReplies[1]))
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
	if len(scr) != 2 {
		t.Fatalf("fields %+v", scr)
	}
	if f := scr[0]; f.Row != 7 || f.Col != 2 || f.Color != go3270.Red || f.Content != "▀▌ " || !f.AttributeOnly {
		t.Errorf("first %+v", f)
	}
	if f := scr[1]; f.Col != 5 || f.Color != go3270.Blue || f.Content != "█" {
		t.Errorf("second %+v", f)
	}
}
