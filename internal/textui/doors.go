package textui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/door"
)

// Doors are the doors the caller may open.
func (s *session) Doors() []*door.Door {
	var out []*door.Door
	s.user = s.n.User()
	if s.t.PETSCII {
		return nil // doors are ANSI programs
	}
	for _, d := range s.b.Doors {
		if s.b.CanDoor(s.user, d) {
			out = append(out, d)
		}
	}
	return out
}

// baud is the caller's line rate as drop files want it: a modem's, or
// 38400 for the network.
func baud(c *bbs.Caller) int {
	if c != nil {
		f := strings.FieldsFunc(c.Rate, func(r rune) bool { return r < '0' || r > '9' })
		if len(f) > 0 {
			if n, err := strconv.Atoi(f[0]); err == nil && n >= 300 && n <= 115200 {
				return n
			}
		}
	}
	return 38400
}

// OpenDoor runs a door.
func (s *session) OpenDoor(d *door.Door) error {
	t := s.t
	u := s.n.Refresh()
	limit := d.TimeLimit
	if left, ok := s.n.TimeLeft(); ok && (limit == 0 || left < limit) {
		limit = left
	}
	t.Println(t.C(Green, fmt.Sprintf("Opening %s (%d minutes)...", d.Name, int(limit/time.Minute))))
	port, err := t.Port()
	if err != nil {
		return err
	}
	in := &door.Info{Node: s.n.Num, UserID: u.ID, UserName: u.Name, Location: u.Location,
		Sysop: u.Sysop, Logins: u.Logins, Posts: u.Posts, LastLogin: u.LastLogin,
		ANSI: t.ANSI, Rows: t.Rows, Cols: t.Cols, Baud: baud(s.c),
		BBSName: s.b.Conf().Name, SysopName: s.b.Conf().Sysop, Level: u.Level}
	if left, ok := s.n.TimeLeft(); ok {
		in.TimeLeft = max(left, time.Minute)
	}
	s.n.Doing("In " + d.Name)
	s.b.Log.Printf("node %d: %s opened door %s", s.n.Num, u.Name, d.Name)
	res, err := d.Run(port, in, t.UTF8, t.Idle, func(f string, a ...any) {
		s.b.Log.Printf("node %d: "+f, append([]any{s.n.Num}, a...)...)
	})
	if errors.Is(err, door.ErrBusy) {
		t.Println(t.C(Red, err.Error()+"."))
		return nil
	}
	if err != nil {
		s.b.Log.Printf("node %d: door %s: %v", s.n.Num, d.Name, err)
		t.Println(t.C(Red, "The door would not open."))
		return nil
	}
	if res.HungUp {
		return errHangup
	}
	if res.Idle {
		return ErrIdle
	}
	t.Settle(300 * time.Millisecond)
	if t.ANSI {
		t.Print("\x1b[0m")
	}
	t.Println("")
	if res.TimeUp {
		t.Println(t.C(Red, "Your time in "+d.Name+" is up."))
	}
	t.Println(t.C(Green, "Back on "+s.b.Conf().Name+"."))
	return nil
}
