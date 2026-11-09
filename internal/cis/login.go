package cis

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// PPN is a user's CompuServe User ID: their account number, in octal
// (CompuServe's IDs have only the digits 0-7), as a project number from
// 72000 and a programmer number.  Account 1 is 72000,1.
func PPN(id int64) string {
	return fmt.Sprintf("%o,%o", 0o72000+id>>12, id&0o7777)
}

var ppnPattern = regexp.MustCompile(`^([0-7]{5,6})[,.]([0-7]{1,5})$`)

// ParsePPN is the account number of a User ID, or false if it is not one
// PPN makes.
func ParsePPN(s string) (int64, bool) {
	m := ppnPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	proj, _ := strconv.ParseInt(m[1], 8, 64)
	prog, _ := strconv.ParseInt(m[2], 8, 64)
	if proj < 0o72000 || prog > 0o7777 {
		return 0, false
	}
	id := (proj-0o72000)<<12 | prog
	return id, id > 0
}

// UserID is what a caller typed at "User ID:": the ID, and what came with
// it.
type UserID struct {
	ID     string // the ID (or name) itself
	Secure bool   // a leading +: CIM asking for the Secure Login Protocol
	Opts   []string
	TTY    bool   // ;TTY: default terminal settings
	Go     string // /GO:word: the first service
}

// ParseUserID reads "+70000,123/INT:60 /secure:1", "73237,341;TTY",
// "70000,123*NAVIG", "alice/GO:MAIL".
func ParseUserID(s string) UserID {
	s = strings.TrimSpace(s)
	var u UserID
	if strings.HasPrefix(s, "+") {
		u.Secure = true
		s = s[1:]
	}
	if i := strings.IndexAny(s, "/*;"); i >= 0 {
		u.ID = strings.TrimSpace(s[:i])
		rest := s[i:]
		for _, f := range strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == ' ' }) {
			switch {
			case strings.EqualFold(f, ";TTY"):
				u.TTY = true
			case strings.HasPrefix(f, "*"), strings.HasPrefix(f, ";"):
				u.Opts = append(u.Opts, f)
			case len(f) > 3 && strings.EqualFold(f[:3], "GO:"):
				u.Go = f[3:]
			case strings.EqualFold(f, "secure:1"):
				u.Secure = true
				u.Opts = append(u.Opts, f)
			default:
				if j := strings.Index(f, ";"); j >= 0 && strings.EqualFold(f[j:], ";TTY") {
					u.TTY = true
					f = f[:j]
				}
				u.Opts = append(u.Opts, f)
			}
		}
	} else {
		u.ID = s
	}
	return u
}

// hosts are the names the network's "Host Name:" takes for the service:
// the manuals' CIS, the 1996 scripts' CPS, and CISAGREE over the internet.
var hosts = map[string]bool{"CIS": true, "CPS": true, "CISAGREE": true, "COMPUSERVE": true}

// hostName is the network node's dialogue: "Host Name:" until the caller
// names the service.
func (s *session) hostName() error {
	for try := 0; ; try++ {
		s.out("\nHost Name: ")
		h, err := s.input(true, s.b.Conf().LoginTimeout)
		if errors.Is(err, errAbort) {
			continue
		}
		if err != nil {
			return s.loginErr(err)
		}
		h = strings.ToUpper(strings.TrimSpace(h))
		if h == "" {
			try--
			if try < -10 {
				return errHangup
			}
			continue
		}
		if i := strings.IndexAny(h, "/;"); i >= 0 {
			h = h[:i]
		}
		if hosts[h] {
			return nil
		}
		s.out("Host is unavailable\n")
		if try >= 2 {
			return errHangup
		}
	}
}

func (s *session) loginErr(err error) error {
	if errors.Is(err, errIdle) {
		s.out("\n")
		return errHangup
	}
	return err
}

// login is "User ID:" and "Password:", up to the BBS's attempts.  A bad
// ID or password is "?? LOGINE - Invalid entry - try again".
func (s *session) login() (*store.User, error) {
	attempts := s.b.Conf().Attempts
	if attempts < 1 {
		attempts = 3
	}
	for try := 0; try < attempts; try++ {
		s.out("\nUser ID: ")
		line, err := s.input(true, s.b.Conf().LoginTimeout)
		if errors.Is(err, errAbort) {
			try--
			continue
		}
		if err != nil {
			return nil, s.loginErr(err)
		}
		id := ParseUserID(line)
		if id.ID == "" {
			try--
			continue
		}
		name := id.ID
		s.byName = true
		if n, ok := ParsePPN(id.ID); ok {
			if u, err := s.b.Store.UserByID(n); err == nil {
				name, s.byName = u.Name, false
			}
		}
		var u *store.User
		if id.Secure {
			u, err = s.secureLogin(name)
		} else {
			s.out("Password: ")
			var pw string
			pw, err = s.input(false, s.b.Conf().LoginTimeout)
			if errors.Is(err, errAbort) {
				continue
			}
			if err != nil {
				return nil, s.loginErr(err)
			}
			u, err = s.node.Login(name, pw)
		}
		switch {
		case err == nil:
			if id.Go != "" {
				s.firstGo = id.Go
			}
			s.tty = id.TTY
			return u, nil
		case errors.Is(err, bbs.ErrBadLogin) && id.Secure:
			s.out("No.\n") // CIM's script says "Incorrect User ID or password"
		case errors.Is(err, bbs.ErrBadLogin):
			s.out("\n?? LOGINE - Invalid entry - try again\n")
		case errors.Is(err, bbs.ErrLocked):
			s.out("\n?LOG STU - Please check your User ID\n")
			return nil, errHangup
		case errors.Is(err, bbs.ErrThrottled):
			s.out("\n?LOG SNA - System unavailable\n")
			return nil, errHangup
		default:
			s.out("\n?LOG SIU - System unavailable\n")
			return nil, err
		}
	}
	return nil, errHangup
}

// date formats of the banner and the logoff
const (
	bannerTime = "15:04 MST Monday 02-Jan-06"
	accessTime = "15:04 02-Jan-06"
	offTime    = "15:04 MST 2-Jan-06"
)

// prime is CompuServe's prime time: 8 to 7 on weekdays.
func prime(t time.Time) bool {
	return t.Weekday() != time.Saturday && t.Weekday() != time.Sunday && t.Hour() >= 8 && t.Hour() < 19
}

// banner is what the caller sees once in.
func (s *session) banner() {
	now := s.now()
	p := ""
	if prime(now) {
		p = " P"
	}
	s.out("\nCompuServe Information Service\n")
	s.outf(" %s%s\n", now.Format(bannerTime), p)
	if !s.user.LastLogin.IsZero() {
		s.outf(" Last access: %s\n", s.user.LastLogin.Format(accessTime))
	}
	if name := s.b.Conf().Name; name != "" {
		s.outf("     Copyright (c) %d\n   %s\n", now.Year(), name)
	}
	if s.byName {
		// not CompuServe's; only a caller who logged on by name sees it,
		// and a script logs on by its User ID
		s.outf(" Your User ID is %s\n", PPN(s.user.ID))
	}
	if n, err := s.b.Store.UnreadMail(s.user.ID); err == nil && n > 0 {
		s.out("You have Electronic Mail waiting.\n")
	}
}

// logoff is CompuServe's goodbye.
func (s *session) logoff() {
	now := s.now()
	d := now.Sub(s.start)
	mins := int(d.Round(time.Minute) / time.Minute)
	if mins < 1 {
		mins = 1
	}
	s.out("\nThank you for using CompuServe!\n")
	s.outf("Off at %s\n", now.Format(offTime))
	s.outf("Connect time = %d:%02d\n", mins/60, mins%60)
}
