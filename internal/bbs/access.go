package bbs

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/door"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// MaxLevel is the highest access level.
const MaxLevel = 255

// ErrNotAllowed: the user's access does not allow it.
var ErrNotAllowed = errors.New("your access does not allow that")

// Require is an access requirement: a level at least, flags to have and
// flags not to have, or the sysop.  Sysops meet every one.
type Require struct {
	Level int
	Has   string // flags all needed
	Lacks string // flags none of which may be had
	Sysop bool
}

// ParseRequire reads a requirement: terms separated by spaces, all of
// which must hold: a number (that level or higher), letters (those flags),
// !letters (none of those flags), "sysop".  "" is anyone.
func ParseRequire(s string) (Require, error) {
	var r Require
	for _, f := range strings.Fields(s) {
		switch {
		case strings.EqualFold(f, "sysop"):
			r.Sysop = true
		case f[0] >= '0' && f[0] <= '9':
			n, err := strconv.Atoi(f)
			if err != nil || n < 0 || n > MaxLevel {
				return Require{}, fmt.Errorf("%q: a level is 0 to %d", f, MaxLevel)
			}
			r.Level = max(r.Level, n)
		case f[0] == '!':
			fl, ok := flagSet(f[1:])
			if !ok || fl == "" {
				return Require{}, fmt.Errorf("%q: flags are letters A to Z", f)
			}
			r.Lacks = mergeFlags(r.Lacks, fl)
		default:
			fl, ok := flagSet(f)
			if !ok {
				return Require{}, fmt.Errorf("%q: not a level, flags, !flags or sysop", f)
			}
			r.Has = mergeFlags(r.Has, fl)
		}
	}
	return r, nil
}

// MustRequire reads a requirement known to be good ("" if not).
func MustRequire(s string) Require {
	r, err := ParseRequire(s)
	if err != nil {
		return Require{Sysop: true} // a bad one shuts everyone but the sysop out
	}
	return r
}

// String writes the requirement as ParseRequire reads it.
func (r Require) String() string {
	var parts []string
	if r.Sysop {
		parts = append(parts, "sysop")
	}
	if r.Level > 0 {
		parts = append(parts, strconv.Itoa(r.Level))
	}
	if r.Has != "" {
		parts = append(parts, r.Has)
	}
	if r.Lacks != "" {
		parts = append(parts, "!"+r.Lacks)
	}
	return strings.Join(parts, " ")
}

// Allows reports whether u meets the requirement (nil, a caller not
// logged in, meets only the empty one).
func (r Require) Allows(u *store.User) bool {
	if u != nil && u.Sysop {
		return true
	}
	if u == nil {
		return r == Require{}
	}
	if r.Sysop || u.Locked || u.Level < r.Level {
		return false
	}
	for _, c := range r.Has {
		if !strings.ContainsRune(u.Flags, c) {
			return false
		}
	}
	for _, c := range r.Lacks {
		if strings.ContainsRune(u.Flags, c) {
			return false
		}
	}
	return true
}

// flagSet is flags as kept: upper case letters, each once, in order.
func flagSet(s string) (string, bool) {
	var seen [26]bool
	for _, c := range strings.ToUpper(s) {
		if c < 'A' || c > 'Z' {
			return "", false
		}
		seen[c-'A'] = true
	}
	var b strings.Builder
	for i, ok := range seen {
		if ok {
			b.WriteByte(byte('A' + i))
		}
	}
	return b.String(), true
}

func mergeFlags(a, b string) string {
	s, _ := flagSet(a + b)
	return s
}

// EditFlags changes flags by an edit: "+AB -C" adds A and B and takes C
// away; letters with no sign are the flags (all of them); "" is none.
func EditFlags(flags, edit string) (string, error) {
	edit = strings.TrimSpace(edit)
	if edit == "" {
		return "", nil // none
	}
	if edit[0] != '+' && edit[0] != '-' {
		fl, ok := flagSet(edit)
		if !ok {
			return "", errors.New("flags are letters A to Z")
		}
		return fl, nil
	}
	have := map[rune]bool{}
	for _, c := range flags {
		have[c] = true
	}
	sign := '+'
	for _, c := range strings.ToUpper(edit) {
		switch {
		case c == '+' || c == '-':
			sign = c
		case c == ' ':
		case c >= 'A' && c <= 'Z':
			have[c] = sign == '+'
		default:
			return "", errors.New("flags are letters A to Z, after + or -")
		}
	}
	var b strings.Builder
	for c := 'A'; c <= 'Z'; c++ {
		if have[c] {
			b.WriteRune(c)
		}
	}
	return b.String(), nil
}

// Level is what a range of access levels may do ([level N]): from Num up
// to the next.
type Level struct {
	Num       int
	Name      string
	Time      time.Duration // online a day (0: no limit)
	Downloads int           // files a day (0: no limit)
	// Ratio is the files a user may download for each they have uploaded,
	// once they have downloaded RatioFree (Ratio 0: no ratio)
	Ratio, RatioFree int
	Post             bool // in message areas
	Mail             bool // mail to users other than sysops
	Chat             bool
	Page             bool // the sysop
	Download         bool
	Upload           bool
	Doors            bool
}

// AllowAll is a level with no limits.
func AllowAll(num int, name string) Level {
	return Level{Num: num, Name: name, Post: true, Mail: true, Chat: true, Page: true,
		Download: true, Upload: true, Doors: true}
}

// SortLevels orders levels by their numbers.
func SortLevels(ls []Level) { sort.Slice(ls, func(i, j int) bool { return ls[i].Num < ls[j].Num }) }

// LevelOf is what u's level may do: the [level] at or below it; sysops,
// and users under no [level], have no limits.
func (b *BBS) LevelOf(u *store.User) Level {
	if u == nil {
		return Level{Name: "not logged in"}
	}
	if u.Sysop {
		return AllowAll(u.Level, "Sysop")
	}
	var out *Level
	for i := range b.Conf().Levels {
		if b.Conf().Levels[i].Num <= u.Level {
			out = &b.Conf().Levels[i]
		}
	}
	if out == nil {
		return AllowAll(u.Level, fmt.Sprintf("Level %d", u.Level))
	}
	l := *out
	return l
}

// CanRead reports whether u may read area a.
func (b *BBS) CanRead(u *store.User, a *store.Area) bool {
	return u != nil && MustRequire(a.ReadReq).Allows(u)
}

// CanPost reports whether u may post in area a.
func (b *BBS) CanPost(u *store.User, a *store.Area) bool {
	if u == nil || !b.CanRead(u, a) {
		return false
	}
	if u.Sysop {
		return true
	}
	return !a.SysopOnly && b.LevelOf(u).Post && MustRequire(a.PostReq).Allows(u)
}

// CanDownload reports whether u may download from file area a (the daily
// limit aside: see MayDownload).
func (b *BBS) CanDownload(u *store.User, a *store.FileArea) bool {
	return u != nil && !u.Locked && b.LevelOf(u).Download && MustRequire(a.DownloadReq).Allows(u)
}

// CanUpload reports whether u may upload into file area a.
func (b *BBS) CanUpload(u *store.User, a *store.FileArea) bool {
	return files.CanUpload(u, a) && b.LevelOf(u).Upload && MustRequire(a.UploadReq).Allows(u)
}

// MayDownload says why u may not download from a now, nil if they may.
func (b *BBS) MayDownload(u *store.User, a *store.FileArea) error {
	if !b.CanDownload(u, a) {
		return ErrNotAllowed
	}
	if left, why := b.DownloadsLeft(u); left == 0 {
		return why
	}
	return nil
}

// DownloadsLeft is how many files u may download now (-1: no limit), and
// if there is a limit, the reason as an error.
func (b *BBS) DownloadsLeft(u *store.User) (int, error) {
	l := b.LevelOf(u)
	left, why := -1, error(nil)
	if l.Downloads > 0 {
		left = max(l.Downloads-u.DownloadsToday(), 0)
		why = fmt.Errorf("that is your %d downloads for today", l.Downloads)
	}
	if l.Ratio > 0 {
		r := max(l.RatioFree+l.Ratio*u.Uploads-u.Downloads, 0)
		if left < 0 || r < left {
			left = r
			why = fmt.Errorf("you have downloaded %d files for %d uploads: upload a file to download %d more",
				u.Downloads, u.Uploads, l.Ratio)
		}
	}
	return left, why
}

// Downloaded counts a download of f by u (u nil: anonymous, as gopher's).
func (b *BBS) Downloaded(u *store.User, f *store.File) {
	b.Store.CountDownload(f.ID)
	if u != nil && u.ID != 0 {
		b.Store.CountUserDownload(u.ID)
		u.DLCount, u.DLDay = u.DownloadsToday()+1, store.Today()
		u.Downloads++
	}
}

// CanDoor reports whether u may open door d.
func (b *BBS) CanDoor(u *store.User, d *door.Door) bool {
	if u == nil {
		return false
	}
	if u.Sysop {
		return true
	}
	return !d.SysopOnly && b.LevelOf(u).Doors && MustRequire(d.Require).Allows(u)
}

// Validate raises a waiting user to the validated level (no lower than
// they are).
func (b *BBS) Validate(u *store.User, by string) error {
	level := max(b.Conf().ValidatedLevel, u.Level)
	if err := b.Store.SetLevel(u.ID, level); err != nil {
		return err
	}
	b.Log.Printf("%s validated %s (level %d)", by, u.Name, level)
	return nil
}
