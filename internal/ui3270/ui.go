// Package ui3270 is the BBS on a 3270 terminal (a TN3270 client such as
// x3270, c3270, wc3270 or Vista): full screens of fields, sent and read
// whole by go3270, PF keys to move about.
package ui3270

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// ErrIdle: the caller pressed nothing for too long.
var ErrIdle = errors.New("idle timeout")

// errLogoff: the caller logged off.
var errLogoff = errors.New("logged off")

// maxResponse is the most a client may send for one screen; a real one
// sends a few kilobytes at most.
const maxResponse = 64 * 1024

// Options are a session's settings from its listener.
type Options struct {
	// a trusted user unknown here gets an account of its own
	CreateTrusted bool
}

// guard limits what is read for one screen.
type guard struct {
	net.Conn
	n int
}

var errTooMuch = errors.New("3270: too much data from the client")

func (g *guard) Read(p []byte) (int, error) {
	if g.n >= maxResponse {
		return 0, errTooMuch
	}
	if len(p) > maxResponse-g.n {
		p = p[:maxResponse-g.n]
	}
	n, err := g.Conn.Read(p)
	g.n += n
	return n, err
}

type ui struct {
	b        *bbs.BBS
	n        *bbs.Node
	g        *guard
	dev      go3270.DevInfo
	rows     int
	cols     int
	o        Options
	user     *store.User
	deadline time.Time // the login's limit
}

// Run is a caller's session on a 3270, from the logon screen to the
// goodbye.  dev is go3270's NegotiateTelnet's.
func Run(b *bbs.BBS, n *bbs.Node, conn net.Conn, dev go3270.DevInfo, o Options) error {
	u := &ui{b: b, n: n, g: &guard{Conn: conn}, dev: dev, o: o}
	u.rows, u.cols = dev.AltDimensions()
	if u.rows < 24 || u.cols < 80 {
		u.rows, u.cols = 24, 80
	}
	err := u.run()
	if errors.Is(err, errLogoff) {
		err = nil
	}
	if errors.Is(err, ErrIdle) {
		u.goodbye("Timed out.  Goodbye.")
	}
	return err
}

func (u *ui) run() error {
	if err := u.logon(); err != nil {
		return err
	}
	return u.mainMenu()
}

// show sends a screen and returns the caller's answer, the values merged
// over vals.  Clear and the PA keys show the screen again.
func (u *ui) show(scr go3270.Screen, vals map[string]string) (go3270.Response, map[string]string, error) {
	crow, ccol := 1, 1
	for _, f := range scr {
		if f.Write {
			crow, ccol = f.Row, f.Col+1
			break
		}
	}
	for {
		u.g.n = 0
		d := time.Now().Add(u.b.IdleTimeout)
		if !u.deadline.IsZero() && u.deadline.Before(d) {
			d = u.deadline
		}
		u.g.SetReadDeadline(d)
		resp, err := go3270.ShowScreenOpts(scr, vals, u.g, go3270.ScreenOpts{
			AltScreen: u.dev, Codepage: u.dev.Codepage(),
			CursorRow: crow, CursorCol: ccol,
		})
		u.g.SetReadDeadline(time.Time{})
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return resp, nil, ErrIdle
			}
			return resp, nil, err
		}
		switch resp.AID {
		case go3270.AIDClear, go3270.AIDPA1, go3270.AIDPA2, go3270.AIDPA3:
			continue
		}
		out := map[string]string{}
		for k, v := range vals {
			out[k] = v
		}
		for k, v := range resp.Values {
			out[k] = v
		}
		return resp, out, nil
	}
}

// goodbye shows a last screen, not waiting for an answer.
func (u *ui) goodbye(msg string) {
	scr := u.frame("Goodbye", "", "")
	scr = append(scr, u.text(u.rows/2, 2, msg, go3270.White, true))
	u.g.SetWriteDeadline(time.Now().Add(10 * time.Second))
	go3270.ShowScreenOpts(scr, nil, u.g, go3270.ScreenOpts{AltScreen: u.dev,
		Codepage: u.dev.Codepage(), NoResponse: true})
	u.g.SetWriteDeadline(time.Time{})
}

// text is a protected field of text at row, col (the attribute there, the
// text from col+1), cut to the screen.
func (u *ui) text(row, col int, s string, color go3270.Color, intense bool) go3270.Field {
	return go3270.Field{Row: row, Col: col, Content: bbs.Trunc(s, u.cols-col-1),
		Color: color, Intense: intense}
}

// input is a field to type in, width wide, and the attribute that ends it.
func (u *ui) input(row, col, width int, name string, hidden bool) []go3270.Field {
	if col+1+width > u.cols-1 {
		width = u.cols - 2 - col
	}
	return []go3270.Field{
		{Row: row, Col: col, Write: true, Name: name, Hidden: hidden,
			Color: go3270.Green, Highlighting: go3270.Underscore},
		{Row: row, Col: col + 1 + width, Autoskip: true},
	}
}

// frame is a screen's title line, message line and key line.
func (u *ui) frame(title, keys, msg string) go3270.Screen {
	node := "Node " + strconv.Itoa(u.n.Num)
	name := bbs.Trunc(u.b.Name, 30)
	mid := u.cols - 1 - 30 - len(node)
	head := bbs.Fit(name, 30) + bbs.Fit(centre(title, mid), mid) + node
	return go3270.Screen{
		u.text(0, 0, head, go3270.Turquoise, true),
		{Row: u.rows - 2, Col: 0, Name: "msg", Content: bbs.Trunc(msg, u.cols-2),
			Color: go3270.Red, Intense: true},
		u.text(u.rows-1, 0, keys, go3270.Blue, false),
	}
}

func centre(s string, w int) string {
	s = bbs.Trunc(s, w)
	return strings.Repeat(" ", (w-len([]rune(s)))/2) + s
}

// logon: the logon screen, or the upstream menu's word for who it is
func (u *ui) logon() error {
	c := u.n.Caller
	if c.TrustedUser != "" {
		user, err := u.n.LoginTrusted(c.TrustedUser, u.o.CreateTrusted)
		if err != nil {
			u.goodbye("You could not be logged in: " + err.Error())
			return errLogoff
		}
		u.user = user
		return nil
	}

	u.deadline = time.Now().Add(u.b.LoginTimeout)
	defer func() { u.deadline = time.Time{} }()
	msg := ""
	vals := map[string]string{}
	for try := 0; try < u.b.Attempts; {
		keys := "Enter=Log on  PF3=Exit"
		if u.b.NewUsers {
			keys += "  PF5=New user"
		}
		scr := u.frame("Logon", keys, msg)
		row := 2
		for _, l := range strings.Split(strings.TrimRight(u.b.Banner, "\n"), "\n") {
			if row > u.rows-10 {
				break
			}
			if l = bbs.Clean(l, u.cols-4); l != "" || row > 2 {
				scr = append(scr, u.text(row, 2, l, go3270.White, false))
				row++
			}
		}
		via := "Connected by " + c.Via
		if u.dev.TerminalType() != "" {
			via += " as " + u.dev.TerminalType()
		}
		scr = append(scr, u.text(row+1, 2, via, go3270.Blue, false))
		r := row + 3
		scr = append(scr, u.text(r, 2, "Name     ===>", go3270.Turquoise, false))
		scr = append(scr, u.input(r, 16, bbs.MaxNameLen, "name", false)...)
		scr = append(scr, u.text(r+1, 2, "Password ===>", go3270.Turquoise, false))
		scr = append(scr, u.input(r+1, 16, 32, "password", true)...)
		if u.b.NewUsers {
			scr = append(scr, u.text(r+3, 2, "New here?  Press PF5 to make an account.", go3270.White, false))
		}
		vals["password"] = ""
		resp, v, err := u.show(scr, vals)
		if err != nil {
			return err
		}
		vals = map[string]string{"name": v["name"]}
		switch resp.AID {
		case go3270.AIDPF3:
			u.goodbye(u.b.Goodbye)
			return errLogoff
		case go3270.AIDPF5:
			if !u.b.NewUsers {
				continue
			}
			ok, err := u.register()
			if err != nil || ok {
				return err
			}
			msg = ""
			continue
		case go3270.AIDEnter:
		default:
			continue
		}
		if v["name"] == "" {
			msg = "Type your name and password."
			continue
		}
		try++
		user, err := u.n.Login(v["name"], v["password"])
		if err == nil {
			u.user = user
			return nil
		}
		msg = err.Error() + "."
		if errors.Is(err, bbs.ErrThrottled) {
			break
		}
	}
	u.goodbye(msg + "  Goodbye.")
	return errLogoff
}

// register: the new user screen; false if the caller went back
func (u *ui) register() (bool, error) {
	msg := ""
	vals := map[string]string{}
	for {
		scr := u.frame("New user", "Enter=Make the account  PF3=Back", msg)
		scr = append(scr,
			u.text(2, 2, "Pick a name: letters, digits, spaces, dots and dashes, 2 to 24 long,", go3270.White, false),
			u.text(3, 2, "and a password of 8 or more characters.", go3270.White, false),
			u.text(5, 2, "Name        ===>", go3270.Turquoise, false),
			u.text(6, 2, "Password    ===>", go3270.Turquoise, false),
			u.text(7, 2, "Again       ===>", go3270.Turquoise, false),
			u.text(8, 2, "Calling from ==>", go3270.Turquoise, false))
		scr = append(scr, u.input(5, 19, bbs.MaxNameLen, "name", false)...)
		scr = append(scr, u.input(6, 19, 32, "pw", true)...)
		scr = append(scr, u.input(7, 19, 32, "again", true)...)
		scr = append(scr, u.input(8, 19, bbs.MaxLocationLen, "location", false)...)
		vals["pw"], vals["again"] = "", ""
		resp, v, err := u.show(scr, vals)
		if err != nil {
			return false, err
		}
		vals = map[string]string{"name": v["name"], "location": v["location"]}
		switch resp.AID {
		case go3270.AIDPF3:
			return false, nil
		case go3270.AIDEnter:
		default:
			continue
		}
		if v["pw"] != v["again"] {
			msg = "The passwords differ."
			continue
		}
		user, err := u.n.Register(v["name"], v["pw"], v["location"])
		if err != nil {
			msg = err.Error() + "."
			continue
		}
		u.user = user
		return true, nil
	}
}

func (u *ui) mainMenu() error {
	msg := ""
	first := true
	for {
		u.user = u.n.Refresh()
		u.n.Doing("Main menu")
		unread, _ := u.b.Store.UnreadMail(u.user.ID)
		mail := "E-mail"
		if unread > 0 {
			mail += fmt.Sprintf("   (%d new)", unread)
		}
		scr := u.frame("Main menu", "Enter=Select  PF3=Log off", msg)
		msg = ""
		scr = append(scr, u.text(2, 2, "Welcome, "+u.user.Name+".", go3270.White, true))
		if first && !u.user.LastLogin.IsZero() {
			scr = append(scr, u.text(3, 2, "Your last call was "+
				u.user.LastLogin.Format("2006-01-02 15:04")+".", go3270.Blue, false))
		}
		items := []string{"Message areas", mail, "Who's online", "User list", "Your profile", "System info"}
		for i, it := range items {
			scr = append(scr, u.text(5+i, 4, strconv.Itoa(i+1), go3270.Yellow, true),
				u.text(5+i, 7, it, go3270.Green, false))
		}
		scr = append(scr, u.text(5+len(items), 4, "X", go3270.Yellow, true),
			u.text(5+len(items), 7, "Log off", go3270.Green, false))
		row := 6 + len(items) + 1
		if motd := u.b.MOTDText(); motd != "" && first {
			for _, l := range strings.Split(strings.TrimRight(motd, "\n"), "\n") {
				if row >= u.rows-4 {
					break
				}
				scr = append(scr, u.text(row, 2, bbs.Clean(l, u.cols-4), go3270.White, false))
				row++
			}
		}
		first = false
		scr = append(scr, u.text(u.rows-3, 2, "Option ===>", go3270.Turquoise, false))
		scr = append(scr, u.input(u.rows-3, 14, 2, "opt", false)...)
		resp, v, err := u.show(scr, nil)
		if err != nil {
			return err
		}
		if resp.AID == go3270.AIDPF3 {
			v["opt"] = "X"
		} else if resp.AID != go3270.AIDEnter {
			continue
		}
		switch strings.ToUpper(v["opt"]) {
		case "":
		case "1":
			err = u.areas()
		case "2":
			err = u.mail()
		case "3":
			err = u.who()
		case "4":
			err = u.users()
		case "5":
			err = u.profile()
		case "6":
			err = u.info()
		case "X":
			u.b.Log.Printf("node %d: %s logged off", u.n.Num, u.user.Name)
			u.goodbye(u.b.Goodbye)
			return errLogoff
		default:
			msg = "Choose 1 to 6, or X."
		}
		if err != nil {
			return err
		}
	}
}

// table is a page of selectable lines: an S field before each, the column
// heading above.  It returns the index chosen (-1 for none), the AID, and
// the page's first index after paging keys.
type table struct {
	title, heading, keys string
	lines                []string
	top                  int
}

func (u *ui) perPage() int { return u.rows - 6 }

func (u *ui) pickFrom(tb *table, msg string) (int, go3270.AID, error) {
	for {
		per := u.perPage()
		scr := u.frame(tb.title, tb.keys, msg)
		msg = ""
		scr = append(scr, u.text(2, 0, "   "+tb.heading, go3270.Turquoise, false))
		for i := 0; i < per && tb.top+i < len(tb.lines); i++ {
			name := "s" + strconv.Itoa(i)
			scr = append(scr, go3270.Field{Row: 3 + i, Col: 0, Write: true, Name: name,
				Color: go3270.Green, Highlighting: go3270.Underscore})
			scr = append(scr, u.text(3+i, 2, tb.lines[tb.top+i], go3270.Green, false))
		}
		if len(tb.lines) == 0 {
			scr = append(scr, u.text(4, 2, "Nothing here yet.", go3270.White, false))
		}
		resp, v, err := u.show(scr, nil)
		if err != nil {
			return -1, resp.AID, err
		}
		switch resp.AID {
		case go3270.AIDPF7:
			if tb.top -= per; tb.top < 0 {
				tb.top = 0
			}
			continue
		case go3270.AIDPF8:
			if tb.top+per < len(tb.lines) {
				tb.top += per
			} else {
				msg = "That is the end."
			}
			continue
		case go3270.AIDEnter:
			for i := 0; i < per && tb.top+i < len(tb.lines); i++ {
				if v["s"+strconv.Itoa(i)] != "" {
					return tb.top + i, resp.AID, nil
				}
			}
			return -1, resp.AID, nil
		}
		return -1, resp.AID, nil
	}
}

func (u *ui) areas() error {
	msg := ""
	top := 0
	for {
		u.n.Doing("Message areas")
		areas, err := u.b.AreasFor(u.user)
		if err != nil {
			return err
		}
		tb := &table{title: "Message areas", top: top,
			heading: fmt.Sprintf("%-20s %-38s %6s %5s", "Area", "Description", "Msgs", "New"),
			keys:    "Type S by an area, Enter  PF3=Back  PF7=Up  PF8=Down"}
		for _, a := range areas {
			tb.lines = append(tb.lines, fmt.Sprintf("%s %s %6d %5d", bbs.Fit(a.Name, 20),
				bbs.Fit(a.Description, 38), a.Messages, a.New))
		}
		i, aid, err := u.pickFrom(tb, msg)
		top = tb.top
		msg = ""
		if err != nil {
			return err
		}
		if aid == go3270.AIDPF3 {
			return nil
		}
		if i >= 0 {
			if err := u.area(areas[i].Area); err != nil {
				return err
			}
		}
	}
}

// area: an area's messages, a page at a time
func (u *ui) area(a *store.Area) error {
	starts := []int64{0}
	msg := ""
	for {
		u.n.Doing("In " + a.Name)
		last, err := u.b.Store.LastRead(u.user.ID, a.ID)
		if err != nil {
			return err
		}
		per := u.perPage()
		ms, err := u.b.Store.Messages(a.ID, starts[len(starts)-1], per)
		if err != nil {
			return err
		}
		keys := "S+Enter=Read  PF3=Back  PF5=Read new  PF7=Up  PF8=Down"
		if bbs.CanPost(u.user, a) {
			keys += "  PF6=Post"
		}
		tb := &table{title: a.Name, keys: keys,
			heading: fmt.Sprintf("%7s %-16s %-38s %s", "#", "From", "Subject", "Date")}
		for _, m := range ms {
			mark := " "
			if m.ID > last {
				mark = "*"
			}
			tb.lines = append(tb.lines, fmt.Sprintf("%s%6d %s %s %s", mark, m.ID, bbs.Fit(m.From, 16),
				bbs.Fit(m.Subject, 38), m.Created.Format("01-02 15:04")))
		}
		i, aid, err := u.pickFrom(tb, msg)
		msg = ""
		if err != nil {
			return err
		}
		switch aid {
		case go3270.AIDPF3:
			return nil
		case go3270.AIDPF5:
			nm, err := u.b.Store.Messages(a.ID, last, 1)
			if err != nil {
				return err
			}
			if len(nm) == 0 {
				msg = "No new messages."
				continue
			}
			if err := u.read(a, nm[0]); err != nil {
				return err
			}
		case go3270.AIDPF6:
			if bbs.CanPost(u.user, a) {
				if msg, err = u.post(a, "All", "", nil, 0); err != nil {
					return err
				}
			}
		case go3270.AIDPF7:
			if len(starts) > 1 {
				starts = starts[:len(starts)-1]
			}
		case go3270.AIDPF8:
			if len(ms) == per {
				starts = append(starts, ms[len(ms)-1].ID)
			} else {
				msg = "That is the end."
			}
		case go3270.AIDEnter:
			if i >= 0 {
				if err := u.read(a, ms[i]); err != nil {
					return err
				}
			}
		}
	}
}

// messageScreen: a message's headers and a window of its body from off
func (u *ui) messageScreen(m *store.Message, title, keys, msg string, off int) (go3270.Screen, int) {
	scr := u.frame(title, keys, msg)
	scr = append(scr,
		u.text(1, 0, "From:", go3270.Turquoise, false),
		u.text(1, 6, bbs.Fit(m.From, 24), go3270.Yellow, true),
		u.text(1, 32, "To:", go3270.Turquoise, false),
		u.text(1, 36, bbs.Fit(m.To, 24), go3270.Green, false),
		u.text(2, 0, "Subj:", go3270.Turquoise, false),
		u.text(2, 6, m.Subject, go3270.White, true),
		u.text(3, 0, "Date:", go3270.Turquoise, false),
		u.text(3, 6, m.Created.Format("2006-01-02 15:04"), go3270.Green, false),
		u.text(4, 0, strings.Repeat("-", u.cols-2), go3270.Blue, false))
	// the body as written: lines of up to 79, the width of the text editor
	width := min(u.cols-1, bbs.MaxLineLen)
	lines := bbs.Wrap(m.Body, width)
	room := u.rows - 7
	for i := 0; i < room && off+i < len(lines); i++ {
		scr = append(scr, u.text(5+i, 0, bbs.Clean(lines[off+i], width), go3270.Green, false))
	}
	return scr, len(lines)
}

// scroll moves a body window by a PF7 or PF8; the new offset, and false if
// the key was not a scroll.
func (u *ui) scroll(aid go3270.AID, off, total int) (int, bool) {
	room := u.rows - 7
	switch aid {
	case go3270.AIDPF7:
		return max(0, off-room), true
	case go3270.AIDPF8:
		if off+room < total {
			return off + room, true
		}
		return off, true
	}
	return off, false
}

// read: messages of an area, from m on
func (u *ui) read(a *store.Area, m *store.Message) error {
	off := 0
	msg := ""
	for {
		u.n.Doing("Reading " + a.Name)
		u.b.Store.SetLastRead(u.user.ID, a.ID, m.ID)
		canDelete := u.user.Sysop || u.user.ID == m.FromID
		keys := "PF3=Back PF7/8=Scroll PF10=Prev PF11=Next"
		if bbs.CanPost(u.user, a) {
			keys += " PF4=Reply"
		}
		keys += " PF5=Mail"
		if canDelete {
			keys += " PF9=Del"
		}
		scr, total := u.messageScreen(m, fmt.Sprintf("%s  #%d", a.Name, m.ID), keys, msg, off)
		msg = ""
		resp, _, err := u.show(scr, nil)
		if err != nil {
			return err
		}
		if o, ok := u.scroll(resp.AID, off, total); ok {
			off = o
			continue
		}
		switch resp.AID {
		case go3270.AIDPF3:
			return nil
		case go3270.AIDPF10, go3270.AIDPF11, go3270.AIDEnter:
			next := resp.AID != go3270.AIDPF10
			nm, err := u.b.Store.Neighbour(a.ID, m.ID, next)
			if errors.Is(err, store.ErrNotFound) {
				if next {
					msg = "That was the last message."
				} else {
					msg = "That is the first message."
				}
				continue
			} else if err != nil {
				return err
			}
			m, off = nm, 0
		case go3270.AIDPF4:
			if bbs.CanPost(u.user, a) {
				if msg, err = u.post(a, m.From, bbs.ReplySubject(m.Subject),
					bbs.Quote(m.From, m.Body, 6), m.ID); err != nil {
					return err
				}
			}
		case go3270.AIDPF5:
			if msg, err = u.compose(m.From, bbs.ReplySubject(m.Subject),
				bbs.Quote(m.From, m.Body, 6), 0); err != nil {
				return err
			}
		case go3270.AIDPF9:
			if canDelete {
				if err := u.n.DeleteMessage(m); err != nil {
					msg = err.Error()
					continue
				}
				return nil
			}
		}
	}
}

// editor: To, Subject and a screen of body lines.  ok is false if the
// caller gave up; check is called on PF5 and its error shown.
func (u *ui) editor(title, to, subject string, body []string,
	check func(to, subject string, body []string) error) (bool, error) {
	width := min(u.cols-1, bbs.MaxLineLen)
	room := u.rows - 7
	vals := map[string]string{"to": to, "subject": subject}
	for i := 0; i < room && i < len(body); i++ {
		vals["l"+strconv.Itoa(i)] = body[i]
	}
	msg := ""
	for {
		scr := u.frame(title, "PF5=Send  PF3=Give up", msg)
		scr = append(scr,
			u.text(1, 0, "To      ===>", go3270.Turquoise, false),
			u.text(2, 0, "Subject ===>", go3270.Turquoise, false),
			u.text(3, 0, strings.Repeat("-", u.cols-2), go3270.Blue, false))
		scr = append(scr, u.input(1, 13, bbs.MaxNameLen, "to", false)...)
		scr = append(scr, u.input(2, 13, bbs.MaxSubjectLen, "subject", false)...)
		for i := 0; i < room; i++ {
			scr = append(scr, go3270.Field{Row: 4 + i, Col: 0, Write: true,
				Name: "l" + strconv.Itoa(i), KeepSpaces: true, Color: go3270.Green})
			if 1+width < u.cols {
				scr = append(scr, go3270.Field{Row: 4 + i, Col: 1 + width, Autoskip: true})
			}
		}
		// the last line ends here, not at the message line
		scr = append(scr, go3270.Field{Row: 4 + room, Col: 0, Autoskip: true})
		resp, v, err := u.show(scr, vals)
		if err != nil {
			return false, err
		}
		vals = v
		switch resp.AID {
		case go3270.AIDPF3:
			return false, nil
		case go3270.AIDPF5:
		default:
			msg = "Press PF5 to send, PF3 to give up."
			continue
		}
		var lines []string
		for i := 0; i < room; i++ {
			lines = append(lines, strings.TrimRight(v["l"+strconv.Itoa(i)], " \x00"))
		}
		if err := check(v["to"], v["subject"], lines); err != nil {
			msg = err.Error() + "."
			continue
		}
		return true, nil
	}
}

// post: a message to an area; the message for the screen after
func (u *ui) post(a *store.Area, to, subject string, quote []string, replyTo int64) (string, error) {
	u.n.Doing("Writing in " + a.Name)
	var id int64
	ok, err := u.editor("Post in "+a.Name, to, subject, quote, func(to, subject string, body []string) error {
		var err error
		id, err = u.n.Post(a, to, subject, body, replyTo)
		return err
	})
	if err != nil || !ok {
		return "", err
	}
	return fmt.Sprintf("Posted as #%d.", id), nil
}

// compose: a private mail; the message for the screen after
func (u *ui) compose(to, subject string, quote []string, replyTo int64) (string, error) {
	u.n.Doing("Writing mail")
	ok, err := u.editor("New mail", to, subject, quote, func(to, subject string, body []string) error {
		return u.n.SendMail(to, subject, body, replyTo)
	})
	if err != nil || !ok {
		return "", err
	}
	return "Sent.", nil
}

func (u *ui) mail() error {
	msg := ""
	top := 0
	for {
		u.n.Doing("Reading mail")
		inbox, err := u.b.Store.Inbox(u.user.ID)
		if err != nil {
			return err
		}
		tb := &table{title: "Your mail", top: top,
			keys:    "S+Enter=Read  PF3=Back  PF6=New mail  PF7=Up  PF8=Down",
			heading: fmt.Sprintf(" %-20s %-40s %s", "From", "Subject", "Date")}
		for _, m := range inbox {
			mark := " "
			if !m.Read {
				mark = "*"
			}
			tb.lines = append(tb.lines, fmt.Sprintf("%s%s %s %s", mark, bbs.Fit(m.From, 20),
				bbs.Fit(m.Subject, 40), m.Created.Format("01-02 15:04")))
		}
		i, aid, err := u.pickFrom(tb, msg)
		top = tb.top
		msg = ""
		if err != nil {
			return err
		}
		switch aid {
		case go3270.AIDPF3:
			return nil
		case go3270.AIDPF6:
			if msg, err = u.compose("", "", nil, 0); err != nil {
				return err
			}
		case go3270.AIDEnter:
			if i >= 0 {
				if msg, err = u.readMail(inbox, i); err != nil {
					return err
				}
			}
		}
	}
}

func (u *ui) readMail(inbox []*store.Message, i int) (string, error) {
	off := 0
	msg := ""
	for {
		m := inbox[i]
		u.b.Store.MarkMailRead(u.user.ID, m.ID)
		scr, total := u.messageScreen(m, fmt.Sprintf("Mail %d of %d", i+1, len(inbox)),
			"PF3=Back PF4=Reply PF7/8=Scroll PF9=Delete PF10=Prev PF11=Next", msg, off)
		msg = ""
		resp, _, err := u.show(scr, nil)
		if err != nil {
			return "", err
		}
		if o, ok := u.scroll(resp.AID, off, total); ok {
			off = o
			continue
		}
		switch resp.AID {
		case go3270.AIDPF3:
			return "", nil
		case go3270.AIDPF4:
			if msg, err = u.compose(m.From, bbs.ReplySubject(m.Subject),
				bbs.Quote(m.From, m.Body, 6), m.ID); err != nil {
				return "", err
			}
		case go3270.AIDPF9:
			if err := u.b.Store.DeleteMail(u.user.ID, m.ID); err != nil {
				return "", err
			}
			return "Deleted.", nil
		case go3270.AIDPF10:
			if i > 0 {
				i, off = i-1, 0
			} else {
				msg = "That is the first."
			}
		case go3270.AIDPF11, go3270.AIDEnter:
			if i+1 < len(inbox) {
				i, off = i+1, 0
			} else {
				msg = "That was the last."
			}
		}
	}
}

// list shows lines a page at a time until PF3
func (u *ui) list(title, heading string, lines []string) error {
	top := 0
	msg := ""
	for {
		scr := u.frame(title, "PF3=Back  PF7=Up  PF8=Down", msg)
		msg = ""
		scr = append(scr, u.text(2, 0, heading, go3270.Turquoise, false))
		per := u.perPage()
		for i := 0; i < per && top+i < len(lines); i++ {
			scr = append(scr, u.text(3+i, 0, lines[top+i], go3270.Green, false))
		}
		resp, _, err := u.show(scr, nil)
		if err != nil {
			return err
		}
		switch resp.AID {
		case go3270.AIDPF3, go3270.AIDEnter:
			return nil
		case go3270.AIDPF7:
			top = max(0, top-per)
		case go3270.AIDPF8:
			if top+per < len(lines) {
				top += per
			} else {
				msg = "That is the end."
			}
		}
	}
}

func (u *ui) who() error {
	u.n.Doing("Who's online")
	var lines []string
	for _, n := range u.b.Nodes() {
		name := n.User
		if name == "" {
			name = "(logging in)"
		}
		lines = append(lines, fmt.Sprintf("%4d  %s %s %5dm  %s", n.Num, bbs.Fit(name, 20),
			bbs.Fit(n.Via, 16), int(time.Since(n.Since).Minutes()), n.Activity))
	}
	return u.list("Who's online", fmt.Sprintf("%4s  %-20s %-16s %6s  %s", "Node", "User", "Via", "Time", "Doing"), lines)
}

func (u *ui) users() error {
	u.n.Doing("User list")
	us, err := u.b.Store.Users()
	if err != nil {
		return err
	}
	var lines []string
	for _, x := range us {
		last := "never"
		if !x.LastLogin.IsZero() {
			last = x.LastLogin.Format("2006-01-02")
		}
		mark := " "
		if x.Sysop {
			mark = "*"
		}
		lines = append(lines, fmt.Sprintf("%s%s %s %6d  %s", mark, bbs.Fit(x.Name, 24),
			bbs.Fit(x.Location, 26), x.Posts, last))
	}
	return u.list("User list", fmt.Sprintf(" %-24s %-26s %6s  %s", "User", "From", "Posts", "Last call"), lines)
}

func (u *ui) profile() error {
	msg := ""
	for {
		u.user = u.n.Refresh()
		x := u.user
		u.n.Doing("Profile")
		scr := u.frame("Your profile", "Enter=Save  PF3=Back", msg)
		msg = ""
		rows, cols := u.dev.AltDimensions()
		cp := "default"
		if c := u.dev.Codepage(); c != nil {
			cp = c.ID()
		}
		scr = append(scr,
			u.text(2, 2, "Name:          "+x.Name, go3270.White, true),
			u.text(3, 2, fmt.Sprintf("Member:        since %s, %d calls, %d posts",
				x.Created.Format("2006-01-02"), x.Logins, x.Posts), go3270.Green, false),
			u.text(4, 2, fmt.Sprintf("Terminal:      %s, %dx%d, code page %s",
				u.dev.TerminalType(), cols, rows, cp), go3270.Green, false),
			u.text(6, 2, "Calling from ===>", go3270.Turquoise, false))
		scr = append(scr, u.input(6, 20, bbs.MaxLocationLen, "location", false)...)
		if !x.External {
			scr = append(scr,
				u.text(8, 2, "To change your password:", go3270.White, false),
				u.text(9, 2, "Password now ===>", go3270.Turquoise, false),
				u.text(10, 2, "New password ===>", go3270.Turquoise, false),
				u.text(11, 2, "Again        ===>", go3270.Turquoise, false))
			scr = append(scr, u.input(9, 20, 32, "old", true)...)
			scr = append(scr, u.input(10, 20, 32, "new", true)...)
			scr = append(scr, u.input(11, 20, 32, "again", true)...)
		}
		resp, v, err := u.show(scr, map[string]string{"location": x.Location})
		if err != nil {
			return err
		}
		if resp.AID == go3270.AIDPF3 {
			return nil
		}
		if resp.AID != go3270.AIDEnter {
			continue
		}
		loc := bbs.Clean(v["location"], bbs.MaxLocationLen)
		if loc != x.Location {
			if err := u.b.Store.SetLocation(x.ID, loc); err != nil {
				return err
			}
			msg = "Saved."
		}
		if v["old"] != "" || v["new"] != "" {
			if v["new"] != v["again"] {
				msg = "The new passwords differ."
				continue
			}
			if err := u.n.ChangePassword(v["old"], v["new"]); err != nil {
				msg = err.Error() + "."
				continue
			}
			msg = "Your password is changed."
		}
	}
}

func (u *ui) info() error {
	b := u.b
	areas, _ := b.Store.Areas()
	msgs := 0
	for _, a := range areas {
		msgs += a.Messages
	}
	us, _ := b.Store.Users()
	lines := []string{b.Name, ""}
	if b.Location != "" {
		lines = append(lines, "Location:  "+b.Location)
	}
	lines = append(lines,
		"Sysop:     "+b.Sysop,
		fmt.Sprintf("Users:     %d", len(us)),
		fmt.Sprintf("Messages:  %d in %d areas", msgs, len(areas)),
		fmt.Sprintf("Nodes:     %d of %d in use", len(b.Nodes()), b.MaxNodes),
		"Software:  bbs-sec, with telnet, TN3270, rlogin and modem callers")
	return u.list("System info", "", lines)
}
