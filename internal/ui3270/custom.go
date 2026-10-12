package ui3270

import (
	"strings"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/screens"
)

var colors = map[string]go3270.Color{
	"": go3270.DefaultColor, "blue": go3270.Blue, "red": go3270.Red, "pink": go3270.Pink,
	"green": go3270.Green, "turquoise": go3270.Turquoise, "yellow": go3270.Yellow, "white": go3270.White,
}

// screenPages is the sysop's display file of that name for a 3270, as
// pages of lines (split at @CLS@ and @PAUSE@ and where a page is full).
func (u *ui) screenPages(name string, per int) ([][]string, *screens.Screen, bool) {
	sc, ok := u.b.Screens.Find(name, screens.TN3270)
	if !ok {
		return nil, nil, false
	}
	var pages [][]string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			pages = append(pages, cur)
			cur = nil
		}
	}
	for _, p := range screens.Expand(sc.Text, u.n.Vars()) {
		if p.Clear || p.Pause {
			flush()
			continue
		}
		for _, l := range strings.Split(strings.TrimSuffix(p.Text, "\n"), "\n") {
			cur = append(cur, l)
			if len(cur) == per {
				flush()
			}
		}
	}
	flush()
	return pages, sc, true
}

// lineFields draws a display file's line at row: a .3270 line's colors
// (each change of color takes a column, as 3270 attributes do), or text.
func (u *ui) lineFields(sc *screens.Screen, row int, line string) go3270.Screen {
	if sc.Format != screens.Color {
		return go3270.Screen{u.text(row, 1, bbs.Clean(line, u.cols-3), go3270.Green, false)}
	}
	var out go3270.Screen
	col := 1
	for _, sp := range screens.Spans(line) {
		if col >= u.cols-1 {
			break
		}
		t := bbs.Clean(sp.Text, u.cols-col-2)
		if t == "" {
			continue
		}
		out = append(out, u.text(row, col, t, colors[sp.Color], sp.Bright))
		col += len([]rune(t)) + 1
	}
	return out
}

// screen shows the sysop's display file of that name, a page at a time;
// false if there is none for a 3270.
func (u *ui) screen(name string) (bool, error) {
	pages, sc, ok := u.screenPages(name, u.rows-4)
	if !ok {
		return false, nil
	}
	for i, page := range pages {
		keys := "Enter=Continue  PF3=Skip"
		if i == len(pages)-1 {
			keys = "Enter=Continue"
		}
		scr := u.frame("", keys, "")
		for j, l := range page {
			scr = append(scr, u.lineFields(sc, 1+j, l)...)
		}
		resp, _, err := u.show(scr, nil)
		if err != nil {
			return true, err
		}
		if resp.AID == go3270.AIDPF3 {
			break
		}
	}
	return true, nil
}

// bye is the goodbye at a logoff: the sysop's goodbye screen, or the
// goodbye line.
func (u *ui) bye() {
	pages, sc, ok := u.screenPages("goodbye", u.rows-3)
	if !ok || len(pages) == 0 {
		u.goodbye(u.b.Goodbye)
		return
	}
	scr := u.frame("Goodbye", "", "")
	for j, l := range pages[0] {
		scr = append(scr, u.lineFields(sc, 1+j, l)...)
	}
	u.sendLast(scr)
}
