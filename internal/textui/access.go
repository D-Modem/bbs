package textui

import (
	"fmt"
	"strconv"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
)

// validate: the sysop goes through the users waiting to be validated.
func (s *session) validate() error {
	t := s.t
	waiting, err := s.b.Store.UsersBelow(s.b.ValidatedLevel)
	if err != nil {
		return err
	}
	for _, w := range waiting {
		t.Println("")
		t.Println(t.C(Cyan, w.Name) + t.C(Dim, fmt.Sprintf("  from %q, since %s, %d calls, level %d",
			w.Location, w.Created.Format("2006-01-02"), w.Logins, w.Level)))
		t.Print(fmt.Sprintf("[V]alidate (level %d), [L]evel..., [K] lock, [S]kip, [Q]uit: ", s.b.ValidatedLevel))
		k, err := t.Key()
		if err != nil {
			return err
		}
		t.Println(string(k))
		by := fmt.Sprintf("node %d: %s", s.n.Num, s.user.Name)
		switch k {
		case 'V':
			if err := s.b.Validate(w, by); err != nil {
				return err
			}
			t.Println(t.C(Green, "Validated."))
		case 'L':
			n, err := t.Input("Level (0-255): ", 3)
			if err != nil {
				return err
			}
			l, err := strconv.Atoi(n)
			if err != nil || l < 0 || l > bbs.MaxLevel {
				t.Println(t.C(Red, "That is not a level."))
				continue
			}
			if err := s.b.Store.SetLevel(w.ID, l); err != nil {
				return err
			}
			s.b.Log.Printf("%s set %s's level to %d", by, w.Name, l)
			t.Println(t.C(Green, "Done."))
		case 'K':
			if err := s.b.Store.SetLocked(w.ID, true); err != nil {
				return err
			}
			s.b.Log.Printf("%s locked %s", by, w.Name)
			t.Println(t.C(Green, "Locked."))
		case 'Q', '\r':
			return nil
		}
	}
	t.Println("No one else is waiting.")
	return nil
}
