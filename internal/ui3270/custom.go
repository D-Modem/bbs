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

// runMenu runs a menu of the sysop's until it is left (back) or the
// caller logs off.
func (u *ui) runMenu(full *bbs.Menu, depth int) error {
	msg := ""
	vars := u.n.Vars()
	for {
		u.user = u.n.Refresh()
		m := u.b.MenuFor(full, u.user)
		title := screens.String(m.Title, vars)
		u.n.Doing(title)
		scr := u.frame(title, "Enter=Select  PF3=Back", msg)
		msg = ""
		pages, sc, shown := []([]string)(nil), (*screens.Screen)(nil), false
		if m.Screen != "" {
			pages, sc, shown = u.screenPages(m.Screen, u.rows-5)
		}
		if shown && len(pages) > 0 {
			for j, l := range pages[0] {
				scr = append(scr, u.lineFields(sc, 1+j, l)...)
			}
		} else {
			scr = append(scr, u.boxed(2, 2, 3+len(m.Items)+1, 44, go3270.Blue)...)
			for i, it := range m.Items {
				scr = append(scr, u.text(3+i, 4, it.Key, go3270.Yellow, true),
					u.text(3+i, 7, screens.String(it.Label, vars), go3270.Green, false))
			}
		}
		prompt := "Option ===>"
		if m.Prompt != "" {
			prompt = bbs.Trunc(screens.String(m.Prompt, vars), 20)
		}
		scr = append(scr, u.text(u.rows-3, 2, prompt, go3270.Turquoise, false))
		scr = append(scr, u.input(u.rows-3, 4+len([]rune(prompt)), 2, "opt", false)...)
		resp, v, err := u.show(scr, nil)
		if err != nil {
			return err
		}
		if resp.AID == go3270.AIDPF3 {
			if depth > 0 {
				return nil
			}
			v["opt"] = ""
			if it := u.logoffItem(m); it != nil {
				v["opt"] = it.Key
			}
		} else if resp.AID != go3270.AIDEnter {
			continue
		}
		key := strings.TrimSpace(v["opt"])
		if key == "" {
			continue
		}
		it := m.Item(key)
		if it == nil {
			msg = "No such option."
			continue
		}
		if it.Action == "back" {
			if depth > 0 {
				return nil
			}
			continue
		}
		if msg, err = u.do(it, depth); err != nil {
			return err
		}
	}
}

// logoffItem is a menu's log off item, for PF3 on the top menu.
func (u *ui) logoffItem(m *bbs.Menu) *bbs.MenuItem {
	for i := range m.Items {
		if m.Items[i].Action == "logoff" {
			return &m.Items[i]
		}
	}
	return nil
}

// do does a menu item; the message for the menu's screen after.
func (u *ui) do(it *bbs.MenuItem, depth int) (string, error) {
	switch it.Action {
	case "areas":
		return "", u.areas()
	case "mail":
		return "", u.mail()
	case "who":
		return "", u.who()
	case "users":
		return "", u.users()
	case "profile":
		return "", u.profile()
	case "info":
		return "", u.info()
	case "terminal":
		return "", u.terminal()
	case "files":
		if u.b.Files == nil {
			return "There are no file areas.", nil
		}
		return "", u.fileAreas()
	case "doors", "door":
		return "Doors are for text terminals (telnet, modem).", nil
	case "chat":
		return "", u.chat("", "")
	case "page":
		return "", u.page()
	case "logoff":
		u.b.Log.Printf("node %d: %s logged off", u.n.Num, u.user.Name)
		u.bye()
		return "", errLogoff
	case "menu":
		if depth >= 10 {
			return "Menus nest too deep.", nil
		}
		sub := u.b.Menus[strings.ToLower(it.Arg)]
		if !bbs.MustRequire(sub.Require).Allows(u.user) {
			return "That menu is not for you.", nil
		}
		return "", u.runMenu(sub, depth+1)
	case "screen":
		shown, err := u.screen(it.Arg)
		if !shown {
			return "There is nothing to show.", err
		}
		return "", err
	case "area":
		a, err := u.b.Store.AreaByName(it.Arg)
		if err != nil || !u.b.CanRead(u.user, a) {
			return "There is no area " + it.Arg + ".", nil
		}
		return "", u.area(a)
	}
	return "", nil
}
