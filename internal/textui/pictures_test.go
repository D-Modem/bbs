package textui

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// testPicture is a picture of w by h red pels.
func testPicture(w, h int) *view.Picture {
	return &view.Picture{Alt: "test.png", Load: func(maxW, maxH int) (image.Image, error) {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.Set(x, y, color.RGBA{255, 0, 0, 255})
			}
		}
		return img, nil
	}}
}

// ansiTerm is an 80x24 ANSI terminal with UTF-8, writing to the bufConn.
func ansiTerm() (*Term, *bufConn) {
	c := &bufConn{}
	t := NewTerm(c)
	t.ANSI, t.UTF8, t.Cols, t.Rows = true, true, 80, 24
	return t, c
}

// TestPictureModes: a terminal's pictures by what it is (kitty's, sixel,
// iTerm2's, blocks, none), and none when off, mono or not ANSI.
func TestPictureModes(t *testing.T) {
	for _, c := range []struct {
		ident string
		sixel bool
		utf8  bool
		want  string
	}{
		{"kitty(0.35.2)", false, true, picKitty},
		{"ghostty 1.1.0", false, true, picKitty},
		{"XTerm(390)", true, true, picSixel},
		{"WezTerm 20240203", true, true, picSixel},
		{"iTerm2 3.4.0", false, true, picITerm},
		{"mintty 3.6", false, false, picITerm},
		{"VTE", false, true, picBlocks},
		{"", false, false, picNone},
	} {
		term, _ := ansiTerm()
		term.Ident, term.Sixel, term.UTF8 = c.ident, c.sixel, c.utf8
		if got := term.PictureMode(); got != c.want {
			t.Errorf("%q sixel %v: %q, want %q", c.ident, c.sixel, got, c.want)
		}
	}
	term, _ := ansiTerm()
	term.Sixel = true
	for name, set := range map[string]func(){
		"off":   func() { term.NoPictures = true },
		"mono":  func() { term.Mono = true },
		"ascii": func() { term.ANSI = false },
	} {
		term.NoPictures, term.Mono, term.ANSI = false, false, true
		set()
		if term.PictureMode() != picNone {
			t.Errorf("%s: pictures", name)
		}
	}
}

// TestShowPicture: each way a picture is sent, fitted to the rows given:
// sixel, kitty's PNG in pieces, iTerm2's OSC 1337, half blocks in 256
// colors; nothing at all where there are no pictures.
func TestShowPicture(t *testing.T) {
	// sixel: fitted to the room (cells of 10x20: 79 columns, 4 rows)
	term, c := ansiTerm()
	term.Sixel, term.CellW, term.CellH = true, 10, 20
	if n := term.ShowPicture(testPicture(2000, 1000), 4); n != 4 {
		t.Errorf("sixel took %d rows", n)
	}
	term.Flush()
	out := c.String()
	if !strings.HasPrefix(out, "\x1bP") || !strings.Contains(out, "\"1;1;160;80") {
		t.Errorf("sixel: %q", out[:min(len(out), 60)])
	}

	// kitty: a PNG in pieces, columns and rows given
	term, c = ansiTerm()
	term.Ident, term.CellW, term.CellH = "kitty(0.35)", 10, 20
	if n := term.ShowPicture(testPicture(300, 200), 10); n != 10 {
		t.Errorf("kitty took %d rows", n)
	}
	term.Flush()
	m := regexp.MustCompile(`^\x1b_Ga=T,f=100,q=2,c=30,r=10,m=([01]);([A-Za-z0-9+/=]+)\x1b\\`).FindStringSubmatch(c.String())
	if m == nil {
		t.Fatalf("kitty: %q", c.String()[:min(c.Len(), 80)])
	}
	var all strings.Builder
	for _, p := range regexp.MustCompile(`;([A-Za-z0-9+/=]+)\x1b\\`).FindAllStringSubmatch(c.String(), -1) {
		all.WriteString(p[1])
	}
	raw, _ := base64.StdEncoding.DecodeString(all.String())
	if cfg, err := png.DecodeConfig(bytes.NewReader(raw)); err != nil || cfg.Width != 300 || cfg.Height != 200 {
		t.Errorf("kitty's PNG: %v %+v", err, cfg)
	}

	// iTerm2: one OSC 1337
	term, c = ansiTerm()
	term.Ident = "iTerm2 3.4"
	term.ShowPicture(testPicture(100, 50), 10)
	term.Flush()
	if !regexp.MustCompile(`^\x1b\]1337;File=inline=1;size=\d+;width=10;height=3;preserveAspectRatio=1:[A-Za-z0-9+/=]+\a\r\n$`).MatchString(c.String()) {
		t.Errorf("iTerm2: %q", c.String()[:min(c.Len(), 80)])
	}

	// blocks: two pixels a cell, red is colour 196
	term, c = ansiTerm()
	if n := term.ShowPicture(testPicture(160, 40), 5); n != 5 {
		t.Errorf("blocks took %d rows", n)
	}
	term.Flush()
	lines := strings.Split(strings.TrimSuffix(c.String(), "\r\n"), "\r\n")
	if len(lines) != 5 || !strings.HasPrefix(lines[0], "\x1b[38;5;196;48;5;196m▀") ||
		strings.Count(lines[0], "▀") != 40 {
		t.Errorf("blocks: %d lines, %q", len(lines), lines[0][:min(len(lines[0]), 40)])
	}

	// none: nothing sent
	term, c = ansiTerm()
	term.UTF8 = false
	if n := term.ShowPicture(testPicture(10, 10), 5); n != 0 || c.Len() != 0 {
		t.Errorf("none: %d rows, %q", n, c.String())
	}
}

// TestColor256: colors to the nearest of xterm's 256.
func TestColor256(t *testing.T) {
	for c, want := range map[color.RGBA]int{
		{0, 0, 0, 255}: 16, {255, 255, 255, 255}: 231, {255, 0, 0, 255}: 196,
		{128, 128, 128, 255}: 244, {0, 0, 255, 255}: 21,
	} {
		if got := color256(c); got != want {
			t.Errorf("%v: %d, want %d", c, got, want)
		}
	}
}
