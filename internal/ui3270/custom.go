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
// pages of lines (split at @CLS@ and @PAUSE@, {clear} and {pause}, and
// where a page is full), their tags as marks for lineFields.
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
	var line strings.Builder
	open := false  // a line begun
	fresh := false // just after a clear or a pause: its line's end is dropped
	endLine := func() {
		cur = append(cur, line.String())
		line.Reset()
		open = false
		if len(cur) == per {
			flush()
		}
	}
	for _, p := range screens.ExpandTags(sc.Text, u.n.Vars()) {
		switch {
		case p.Clear || p.Pause:
			if open {
				endLine()
			}
			flush()
			fresh = true
		case p.Tag != "":
			line.WriteString(screens.Mark(p.Tag))
			open = true
		default:
			text := p.Text
			if fresh {
				text = strings.TrimPrefix(text, "\n") // the rest of a {clear}'s line
				fresh = false
			}
			for i, part := range strings.Split(text, "\n") {
				if i > 0 {
					endLine()
				}
				line.WriteString(part)
				open = open || part != ""
			}
		}
	}
	if open {
		endLine()
	}
	flush()
	return pages, sc, true
}

// lineFields draws a display file's line at row in its colors (each
// change of color takes a column, as 3270 attributes do): a .3270's, or a
// text file's in green where its tags give none.
func (u *ui) lineFields(sc *screens.Screen, row int, line string) go3270.Screen {
	plain := go3270.DefaultColor
	if sc.Format != screens.Color {
		plain = go3270.Green
	}
	return u.spanFields(row, 1, u.cols-1, line, plain)
}

// spanFields draws a marked line (screens.Spans) at row from col to
// before end, in plain where its tags give no color.
func (u *ui) spanFields(row, col, end int, line string, plain go3270.Color) go3270.Screen {
	var out go3270.Screen
	for _, sp := range screens.Spans(line) {
		if col >= end {
			break
		}
		t := bbs.Clean(sp.Text, end-col-1)
		if t == "" {
			continue
		}
		c := colors[sp.Color]
		if sp.Color == "" {
			c = plain
		}
		out = append(out, u.text(row, col, t, c, sp.Bright))
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
