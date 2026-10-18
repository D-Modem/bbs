// Package ui3270 is the BBS on a 3270 terminal (a TN3270 client such as
// x3270, c3270, wc3270 or Vista): full screens of fields, sent and read
// whole by go3270, PF keys to move about.
package ui3270

import (
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/app"
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
	Graphics      Graphics
}

// guard limits what is read for one screen.
type guard struct {
	net.Conn
	n int
}

var errTooMuch = errors.New("3270: too much data from the client")

// Read reads from the connection, no more than maxResponse bytes in all since n
// was last reset (for each screen): errTooMuch past that.
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
	logo     *logo     // nil: no graphics logo
	logoAt   [2]int    // where the vector logo goes on this screen
	drawn    bool      // vector graphics are on the screen
	pictures []placed  // vector pictures for this screen
	laid     *laidOut  // the last picture laid out
	loads    [][]byte  // Load PS for this screen's picture, sent before it
	// noPictures: the caller turned pictures off (pictureMode)
	noPictures bool
	chatting   bool     // in a chat room (its lines are its screen's)
	backlog    []string // other nodes' lines not shown yet
	say        string   // for the next screen's message line
	sayBad     bool     // and it is of something refused or failed
}

// Run is a caller's session on a 3270, from the logon screen to the
// goodbye.  dev is go3270's NegotiateTelnet's.
func Run(b *bbs.BBS, n *bbs.Node, conn net.Conn, dev go3270.DevInfo, o Options) error {
	u := &ui{b: b, n: n, g: &guard{Conn: conn}, dev: dev, o: o}
	u.rows, u.cols = dev.AltDimensions()
	if u.rows < 24 || u.cols < 80 {
		u.rows, u.cols = 24, 80
	}
	u.setupGraphics()
	err := u.run()
	if errors.Is(err, errLogoff) {
		err = nil
	}
	if errors.Is(err, ErrIdle) {
		u.goodbye("Timed out.  Goodbye.")
	}
	return err
}

// run is the logon, then the shared screens; the app's log off is errLogoff.
func (u *ui) run() error {
	if err := u.logon(); err != nil {
		return err
	}
	u.applyPrefs()
	err := app.Run(u.b, u.n, u)
	if errors.Is(err, app.ErrLogoff) {
		err = errLogoff
	}
	return err
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
	// the screen's vector pictures are its own
	defer func() { u.pictures, u.loads = nil, nil }()
	for {
		if u.drawn {
			// vector graphics stay until the terminal is reset: Erase/Reset,
			// to the alternate screen size if that is what go3270 writes to
			flag := byte(0)
			if u.rows != 24 || u.cols != 80 {
				flag = 0x80
			}
			if err := go3270.WriteStructuredFields(u.g, []byte{0x03, flag}); err != nil {
				return go3270.Response{}, nil, err
			}
			u.drawn = false
		}
		if len(u.loads) > 0 {
			// the symbols before the screen that shows them
			if err := go3270.WriteStructuredFields(u.g, u.loads...); err != nil {
				return go3270.Response{}, nil, err
			}
		}
		u.g.n = 0
		d := time.Now().Add(u.b.Conf().IdleTimeout)
		if !u.deadline.IsZero() && u.deadline.Before(d) {
			d = u.deadline
		}
		u.g.SetReadDeadline(d)
		resp, err := go3270.ShowScreenOpts(scr, vals, u.g, go3270.ScreenOpts{
			AltScreen: u.dev, Codepage: u.dev.Codepage(),
			CursorRow: crow, CursorCol: ccol,
			PostSendCallback: func(any) error { return u.drawGraphics() },
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
	u.sendLast(scr)
}

// sendLast sends a screen that is answered by nothing: the last.
func (u *ui) sendLast(scr go3270.Screen) {
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
	name := bbs.Trunc(u.b.Conf().Name, 30)
	mid := u.cols - 1 - 30 - len(node)
	head := bbs.Fit(name, 30) + bbs.Fit(centre(title, mid), mid) + node
	color := go3270.Red
	if msg == "" && u.say != "" {
		msg, u.say, color = u.say, "", go3270.White
		if u.sayBad {
			color, u.sayBad = go3270.Red, false
		}
	}
	if msg == "" {
		if msg = u.notice(); msg != "" {
			color = go3270.Yellow
		}
	}
	return go3270.Screen{
		u.text(0, 0, head, go3270.Turquoise, true),
		{Row: u.rows - 2, Col: 0, Name: "msg", Content: bbs.Trunc(msg, u.cols-2),
			Color: color, Intense: true},
		u.text(u.rows-1, 0, keys, go3270.Blue, false),
	}
}

// centre is s, cut to w, with the spaces before it that centre it in w columns
// (none after).
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

	u.deadline = time.Now().Add(u.b.Conf().LoginTimeout)
	defer func() { u.deadline = time.Time{} }()
	msg := ""
	vals := map[string]string{}
	for try := 0; try < u.b.Conf().Attempts; {
		keys := "Enter=Log on  PF3=Exit"
		if u.b.Conf().NewUsers {
			keys += "  PF5=New user"
		}
		scr := u.frame("Logon", keys, msg)
		row := 2
		if lf, n := u.logoFields(2, 2); n > 0 {
			scr = append(scr, lf...)
			row += n + 1
		}
		if pages, sc, ok := u.screenPages("logon", u.rows-11); ok && len(pages) > 0 {
			for _, l := range pages[0] {
				scr = append(scr, u.lineFields(sc, row, l)...)
				row++
			}
		} else {
			for _, l := range strings.Split(strings.TrimRight(u.b.Conf().Banner, "\n"), "\n") {
				if row > u.rows-10 {
					break
				}
				if l = bbs.Clean(l, u.cols-4); l != "" || row > 2 {
					scr = append(scr, u.text(row, 2, l, go3270.White, false))
					row++
				}
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
		if u.b.Conf().NewUsers {
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
			u.bye()
			return errLogoff
		case go3270.AIDPF5:
			if !u.b.Conf().NewUsers {
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

// perPage is the rows of a list on one screen: all but the title, note and
// heading lines above it and the input, message and key lines below.
func (u *ui) perPage() int { return u.rows - 6 }

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
			u.text(3, 0, u.rule(u.cols-2), go3270.Blue, false))
		scr = append(scr, u.input(1, 13, bbs.MaxMailToLen, "to", false)...)
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
			msg = capital(err.Error()) + "."
			continue
		}
		return true, nil
	}
}

// list shows lines a page at a time until PF3
func (u *ui) list(title, heading string, lines []string) error {
	return u.listColored(title, heading, lines, nil)
}

// listColored is list with each line's color (nil: all green).
func (u *ui) listColored(title, heading string, lines []string, colors []go3270.Color) error {
	top := 0
	msg := ""
	for {
		scr := u.frame(title, "PF3=Back  PF7=Up  PF8=Down", msg)
		msg = ""
		scr = append(scr, u.text(2, 0, heading, go3270.Turquoise, false))
		per := u.perPage()
		for i := 0; i < per && top+i < len(lines); i++ {
			color := go3270.Green
			if top+i < len(colors) {
				color = colors[top+i]
			}
			scr = append(scr, u.text(3+i, 0, lines[top+i], color, false))
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
