package ui3270

import (
	"strconv"
	"strings"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// Sheet is a sheet as a 3270 screen: the values as fields to type over
// (all sent with Enter), S by a menu or action to choose it, PF10 to save
// and leave, PF9 to leave without.
func (u *ui) Sheet(sh *view.Sheet) (view.SheetResult, error) {
	per := u.rows - 5 // rows 2 .. rows-4
	top := 0
	if sh.Cursor >= per {
		top = sh.Cursor / per * per
	}
	lw := min(28, max(u.cols/3, 16))
	nw := 0
	for _, e := range sh.Entries {
		nw = max(nw, len(e.Note))
	}
	nw = min(nw, u.cols/5)
	keys := "S+Enter=Open PF1=Help PF3=Back PF7/8=Page PF9=Quit PF10=Save+exit"
	if k := pfKeys(sh.Keys, "  "); k != "" {
		keys = k + "  " + keys
	}
	if len(keys) > u.cols-1 {
		keys = strings.ReplaceAll(strings.ReplaceAll(keys, "  ", " "), "S+Enter=Open", "S=Open")
	}
	msg := ""
	for {
		status := msg
		if status == "" {
			status = sh.Status
		}
		scr := u.frame(sh.Title, keys, status)
		msg = ""
		crumbs := sh.Crumbs
		if len(sh.Note) > 0 {
			crumbs = sh.Note[0].Text
		}
		scr = append(scr, u.text(1, 1, crumbs, go3270.Blue, false))
		vals := map[string]string{}
		long := map[int]bool{} // text entries too long for their field: chosen, they get a form
		for r := 0; r < per && top+r < len(sh.Entries); r++ {
			i := top + r
			e := sh.Entries[i]
			row := 2 + r
			id := strconv.Itoa(i)
			if e.Kind == view.EntryHeading {
				scr = append(scr, u.text(row, 1, e.Label, go3270.Turquoise, true))
				continue
			}
			color := go3270.Green
			if e.Changed {
				color = go3270.Yellow
			}
			mark := " "
			if e.Changed {
				mark = "*"
			}
			label := e.Label
			if e.Kind == view.EntryMenu {
				label += " >"
			}
			scr = append(scr, u.text(row, 2, mark+bbs.Fit(bbs.Trunc(label, lw), lw), color, e.Changed))
			vcol := 2 + 1 + lw + 1
			vw := u.cols - vcol - 3 - nw
			if e.Note != "" {
				scr = append(scr, u.text(row, u.cols-nw-2, e.Note, go3270.Blue, false))
			}
			switch {
			case e.Disabled || e.Kind == view.EntryInfo:
				scr = append(scr, u.text(row, vcol, e.Shown(), go3270.Blue, false))
			case e.Kind == view.EntryMenu || e.Kind == view.EntryAction:
				scr = append(scr, go3270.Field{Row: row, Col: 0, Write: true, Name: "s" + id,
					Color: go3270.Green, Highlighting: go3270.Underscore})
				scr = append(scr, go3270.Field{Row: row, Col: 2, Autoskip: true})
			case e.Kind == view.EntryText && len([]rune(e.Value)) > vw:
				long[i] = true
				scr = append(scr, go3270.Field{Row: row, Col: 0, Write: true, Name: "s" + id,
					Color: go3270.Green, Highlighting: go3270.Underscore})
				scr = append(scr, go3270.Field{Row: row, Col: 2, Autoskip: true})
				scr = append(scr, u.text(row, vcol, bbs.Trunc(e.Value, vw-4)+" ...", go3270.White, false))
			default:
				scr = append(scr, go3270.Field{Row: row, Col: vcol, Write: true, Name: "v" + id,
					Hidden: e.Secret, Color: go3270.Turquoise, Highlighting: go3270.Underscore})
				if !e.Secret {
					vals["v"+id] = e.Value
				}
				scr = append(scr, go3270.Field{Row: row, Col: vcol + 1 + vw, Autoskip: true})
			}
		}
		resp, v, err := u.show(scr, vals)
		if err != nil {
			return view.SheetResult{}, err
		}
		switch resp.AID {
		case go3270.AIDPF3:
			return view.SheetResult{Action: view.Back, Entry: top}, nil
		case go3270.AIDPF1:
			// the help of the entry the cursor is on
			i := top + resp.Row - 2
			if resp.Row >= 2 && i < len(sh.Entries) && i >= top {
				return view.SheetResult{Action: view.SheetHelp, Entry: i}, nil
			}
			msg = "Put the cursor on a line for its help."
			continue
		case go3270.AIDPF9:
			return view.SheetResult{Action: view.SheetQuit, Entry: top}, nil
		case go3270.AIDPF10:
			return view.SheetResult{Action: view.SheetSave, Entry: top}, nil
		case go3270.AIDPF7:
			if top == 0 {
				msg = "That is the top."
			}
			top = max(0, top-per)
			continue
		case go3270.AIDPF8:
			if top+per >= len(sh.Entries) {
				msg = "That is the end."
				continue
			}
			top += per
			continue
		case go3270.AIDEnter:
			for r := 0; r < per && top+r < len(sh.Entries); r++ {
				i := top + r
				if strings.TrimSpace(strings.Trim(v["s"+strconv.Itoa(i)], "\x00")) == "" {
					continue
				}
				e := sh.Entries[i]
				if !long[i] {
					return view.SheetResult{Action: view.Pick, Entry: i}, nil
				}
				f, ok, err := u.Form(&view.Form{Title: e.Label, Intro: []view.Line{view.T(view.Dim, e.Help)},
					Fields: []view.Field{{Name: "v", Label: e.Label, Width: max(e.Width, 255), Value: e.Value, Edit: true}},
					Submit: "Set"})
				if err != nil {
					return view.SheetResult{}, err
				}
				if ok && f["v"] != e.Value {
					return view.SheetResult{Action: view.SheetSet, Entry: i, Values: map[string]string{e.ID: f["v"]}}, nil
				}
				break
			}
			changed := map[string]string{}
			first := -1
			for r := 0; r < per && top+r < len(sh.Entries); r++ {
				i := top + r
				nv, sent := v["v"+strconv.Itoa(i)]
				if !sent {
					continue
				}
				e := sh.Entries[i]
				nv = strings.TrimSpace(strings.Trim(nv, "\x00"))
				if e.Secret && nv == "" || !e.Secret && nv == e.Value {
					continue
				}
				changed[e.ID] = nv
				if first < 0 {
					first = i
				}
			}
			if len(changed) > 0 {
				return view.SheetResult{Action: view.SheetSet, Entry: first, Values: changed}, nil
			}
			continue
		}
		if id := pfAction(sh.Keys, resp.AID); id != "" {
			return view.SheetResult{Action: id, Entry: top}, nil
		}
	}
}
