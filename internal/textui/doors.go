package textui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/door"
)

// doorsFor are the doors a user may open.
func (s *session) doorsFor() []*door.Door {
	var out []*door.Door
	for _, d := range s.b.Doors {
		if !d.SysopOnly || s.user.Sysop {
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

// doors: the list of doors, and into one
func (s *session) doors() error {
	t := s.t
	for {
		s.n.Doing("Doors")
		ds := s.doorsFor()
		t.Println("")
		t.Println(t.C(Cyan, "Doors"))
		for i, d := range ds {
			t.Printf("  %s %s %s\n", t.C(Yellow, fmt.Sprintf("%2d", i+1)), bbs.Fit(d.Name, 20), t.C(Dim, d.Description))
		}
		if len(ds) == 0 {
			t.Println("  (none)")
		}
		num, err := t.Input("\nDoor number (Enter to go back): ", 3)
		if err != nil || num == "" {
			return err
		}
		i, err := strconv.Atoi(num)
		if err != nil || i < 1 || i > len(ds) {
			t.Println("No such door.")
			continue
		}
		if err := s.openDoor(ds[i-1]); err != nil {
			return err
		}
	}
}

func (s *session) openDoor(d *door.Door) error {
	t := s.t
	u := s.n.Refresh()
	t.Println(t.C(Green, fmt.Sprintf("Opening %s (%d minutes)...", d.Name, int(d.TimeLimit/time.Minute))))
	port, err := t.Port()
	if err != nil {
		return err
	}
	in := &door.Info{Node: s.n.Num, UserID: u.ID, UserName: u.Name, Location: u.Location,
		Sysop: u.Sysop, Logins: u.Logins, Posts: u.Posts, LastLogin: u.LastLogin,
		ANSI: t.ANSI, Rows: t.Rows, Cols: t.Cols, Baud: baud(s.c),
		BBSName: s.b.Name, SysopName: s.b.Sysop}
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
	t.Println(t.C(Green, "Back on "+s.b.Name+"."))
	return nil
}
