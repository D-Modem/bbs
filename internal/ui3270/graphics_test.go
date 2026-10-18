package ui3270

import (
	"image"
	"image/color"
	"testing"

	"github.com/racingmars/go3270"
)

// TestCut: an image cut into cells: cells alike are one symbol, an empty cell a
// blank (0x40), and each symbol its rows of pels.
func TestCut(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 18, 32))
	// a pel in the first cell, the same in the third, the second empty
	img.SetGray(0, 0, color.Gray{255})
	img.SetGray(0, 16, color.Gray{255})
	img.SetGray(10, 20, color.Gray{255})
	syms, grid, err := cut(img, 9, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 2 {
		t.Errorf("%d symbols", len(syms))
	}
	want := [][]byte{{0x41, 0x40}, {0x41, 0x42}}
	for r := range want {
		for c := range want[r] {
			if grid[r][c] != want[r][c] {
				t.Errorf("grid %x", grid)
			}
		}
	}
	if syms[0][0][0] != '#' || syms[1][4][1] != '#' {
		t.Errorf("symbols %q", syms)
	}
}

// TestCutTooMany: more different cells than a symbol set holds are an error.
func TestCutTooMany(t *testing.T) {
	// 200 different cells: more than a symbol set holds
	img := image.NewGray(image.Rect(0, 0, 9*200, 16))
	for i := 0; i < 200; i++ {
		for b := 0; b < 8; b++ {
			if i&(1<<b) != 0 {
				img.SetGray(i*9+b, 0, color.Gray{255})
			}
		}
		img.SetGray(i*9, 15, color.Gray{255})
	}
	if _, _, err := cut(img, 9, 16); err == nil {
		t.Error("took 200 symbols")
	}
}

// TestNameLogo: the system's name as a logo, the 7x13 font at twice its size,
// cut into cells; fitLogo makes it fit a smaller box.
func TestNameLogo(t *testing.T) {
	img, err := logoImage(Graphics{}, "BBS", 2, 16)
	if err != nil {
		t.Fatal(err)
	}
	// 7x13 font at 2x: 3 letters, 42x26
	if img.Bounds().Dx() != 42 || img.Bounds().Dy() != 26 {
		t.Errorf("size %v", img.Bounds())
	}
	syms, grid, err := cut(img, 9, 16)
	if err != nil || len(grid) != 2 || len(grid[0]) != 5 || len(syms) == 0 {
		t.Errorf("cut: %d symbols, %d rows, %v", len(syms), len(grid), err)
	}
	small := fitLogo(img, 21, 13)
	if small.Bounds().Dx() > 21 || small.Bounds().Dy() > 13 {
		t.Errorf("fit %v", small.Bounds())
	}
}

// TestSymbolCell: the cell and Load PS format for a terminal's symbol sets: the
// 3278's format 1 at 9 by 16, format 3 at the slot's size where sizes must
// match and 8 wide where they need not; none without a loadable set, or with
// the sets not loadable.
func TestSymbolCell(t *testing.T) {
	cases := []struct {
		s            *go3270.SymbolSets
		w, h, format int
		ok           bool
	}{
		{nil, 0, 0, 0, false},
		{&go3270.SymbolSets{Loadable: true, SlotWidth: 9, SlotHeight: 16, Formats: 1 << 30,
			Sets: []go3270.SymbolSet{{RWS: 2, Loadable: true}}}, 9, 16, 1, true},
		{&go3270.SymbolSets{Loadable: true, SizeMatch: true, SlotWidth: 9, SlotHeight: 12, Formats: 1 << 28,
			Sets: []go3270.SymbolSet{{RWS: 2, Loadable: true}}}, 9, 12, 3, true},
		{&go3270.SymbolSets{Loadable: true, SlotWidth: 10, SlotHeight: 20, Formats: 1 << 28,
			Sets: []go3270.SymbolSet{{RWS: 2, Loadable: true}}}, 8, 20, 3, true}, // no size match: 8 wide
		{&go3270.SymbolSets{Loadable: true, SlotWidth: 9, SlotHeight: 16, Formats: 1 << 30}, 0, 0, 0, false}, // no sets
		{&go3270.SymbolSets{Loadable: false, SlotWidth: 9, SlotHeight: 16, Formats: 1 << 30,
			Sets: []go3270.SymbolSet{{RWS: 2, Loadable: true}}}, 0, 0, 0, false},
	}
	for i, c := range cases {
		w, h, f, _, ok := symbolCell(fakeDev{c.s})
		if w != c.w || h != c.h || f != c.format || ok != c.ok {
			t.Errorf("%d: %d %d %d %v", i, w, h, f, ok)
		}
	}
}

type fakeDev struct{ s *go3270.SymbolSets }

// SymbolSets is the sets given.
func (f fakeDev) SymbolSets() *go3270.SymbolSets { return f.s }

// CellSize is not known: 0 by 0.
func (f fakeDev) CellSize() (int, int) { return 0, 0 }
