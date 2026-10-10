package sixel

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func picture() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 9))
	red := color.NRGBA{255, 0, 0, 255}
	blue := color.NRGBA{0, 0, 255, 255}
	for x := 0; x < 20; x++ {
		img.Set(x, 0, red) // a red line
		img.Set(x, 8, blue)
	}
	for y := 0; y < 9; y++ {
		img.Set(3, y, blue) // a blue column through both bands
	}
	return img
}

func TestEncodeShape(t *testing.T) {
	data, err := Encode(picture())
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "\x1bP0;1;0q\"1;1;20;9") || !strings.HasSuffix(s, "\x1b\\") {
		t.Errorf("framing: %q", s)
	}
	if strings.Count(s, "-") != 2 {
		t.Errorf("want 2 bands: %q", s)
	}
	if !strings.Contains(s, "!") {
		t.Errorf("no run-length: %q", s)
	}
	// nothing but sixel characters, digits and the controls of the format
	// between the introducer and the terminator
	for _, c := range s[len("\x1bP0;1;0q") : len(s)-2] {
		if c < 0x20 || c > 0x7e {
			t.Fatalf("byte %#x in the picture", c)
		}
	}
	if _, err := Encode(image.NewNRGBA(image.Rect(0, 0, 0, 0))); err == nil {
		t.Error("encoded an empty picture")
	}
}

func TestManyColors(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.NRGBA{uint8(x * 4), uint8(y * 4), 128, 255})
		}
	}
	data, err := Encode(img)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), ";2;"); n > MaxColors+1 {
		t.Errorf("%d colors", n)
	}
}

// ImageMagick reads sixel: the picture it gets back must be ours
func TestDecodeWithImageMagick(t *testing.T) {
	convert, err := exec.LookPath("convert")
	if err != nil {
		t.Skip("no ImageMagick")
	}
	data, err := Encode(picture())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	in, out := filepath.Join(dir, "p.six"), filepath.Join(dir, "p.png")
	os.WriteFile(in, data, 0600)
	if msg, err := exec.Command(convert, "sixel:"+in, out).CombinedOutput(); err != nil {
		t.Skipf("convert: %v %s", err, msg)
	}
	f, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(f))
	if err != nil {
		t.Fatal(err)
	}
	want := picture()
	if got.Bounds().Dx() != 20 || got.Bounds().Dy() < 9 {
		t.Fatalf("size %v", got.Bounds())
	}
	for y := 0; y < 9; y++ {
		for x := 0; x < 20; x++ {
			wr, wg, wb, wa := want.At(x, y).RGBA()
			gr, gg, gb, _ := got.At(x, y).RGBA()
			if wa == 0 {
				continue // background: whatever the decoder makes of it
			}
			if wr>>8 != gr>>8 || wg>>8 != gg>>8 || wb>>8 != gb>>8 {
				t.Errorf("(%d,%d) = %d,%d,%d, want %d,%d,%d", x, y, gr>>8, gg>>8, gb>>8, wr>>8, wg>>8, wb>>8)
			}
		}
	}
}
