package confui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/confedit"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// data is what the screens change in the database: copies of the
// accounts and areas as read, changed as the sysop says, and made so at
// the save.
type data struct {
	b         *bbs.BBS
	users     []*store.User // as they will be (ID 0: a new one)
	was       map[int64]store.User
	passwords map[*store.User]string // new passwords
	deleted   map[int64]bool
	areas     []*store.Area
	areasWas  map[int64]store.Area
	purge     bool // old messages taken out at the save
}

// op is a change to make at the save.
type op struct {
	desc string
	run  func() error
}

// data reads the accounts and areas, the first time they are wanted.
func (e *Editor) data() (*data, error) {
	if e.db != nil {
		return e.db, nil
	}
	if e.Open == nil {
		return nil, errors.New("no database here")
	}
	b, err := e.Open()
	if err != nil {
		return nil, err
	}
	d := &data{b: b, was: map[int64]store.User{}, passwords: map[*store.User]string{}, deleted: map[int64]bool{},
		areasWas: map[int64]store.Area{}}
	us, err := b.Store.Users()
	if err != nil {
		return nil, err
	}
	for _, u := range us {
		c := *u
		d.users = append(d.users, &c)
		d.was[u.ID] = *u
	}
	as, err := b.Store.Areas()
	if err != nil {
		return nil, err
	}
	for _, a := range as {
		c := *a
		d.areas = append(d.areas, &c)
		d.areasWas[a.ID] = *a
	}
	e.db = d
	return d, nil
}

// ops are the changes, in the order they are made.
func (d *data) ops() []op {
	st := d.b.Store
	var out []op
	for _, u := range d.users {
		u := u
		if u.ID == 0 {
			pw := d.passwords[u]
			out = append(out, op{"user " + u.Name + ": new", func() error {
				nu, err := st.CreateUser(u.Name, pw, "", u.Sysop)
				if err != nil {
					return err
				}
				if err := st.SetLevel(nu.ID, u.Level); err != nil {
					return err
				}
				return st.SetFlags(nu.ID, u.Flags)
			}})
			continue
		}
		if d.deleted[u.ID] {
			out = append(out, op{"user " + u.Name + ": deleted", func() error { return st.DeleteUser(u.ID) }})
			continue
		}
		w := d.was[u.ID]
		if u.Level != w.Level {
			out = append(out, op{fmt.Sprintf("user %s: level %d (was %d)", u.Name, u.Level, w.Level),
				func() error { return st.SetLevel(u.ID, u.Level) }})
		}
		if u.Flags != w.Flags {
			out = append(out, op{fmt.Sprintf("user %s: flags %q (was %q)", u.Name, u.Flags, w.Flags),
				func() error { return st.SetFlags(u.ID, u.Flags) }})
		}
		if u.Sysop != w.Sysop {
			out = append(out, op{fmt.Sprintf("user %s: sysop %s", u.Name, yesno(u.Sysop)),
				func() error { return st.SetSysop(u.ID, u.Sysop) }})
		}
		if u.Locked != w.Locked {
			out = append(out, op{fmt.Sprintf("user %s: locked %s", u.Name, yesno(u.Locked)),
				func() error { return st.SetLocked(u.ID, u.Locked) }})
		}
		if pw, ok := d.passwords[u]; ok {
			out = append(out, op{"user " + u.Name + ": a new password", func() error { return st.SetPassword(u.ID, pw) }})
		}
	}
	for _, a := range d.areas {
		a := a
		w := d.areasWas[a.ID]
		if a.MaxMsgs != w.MaxMsgs || a.MaxDays != w.MaxDays {
			out = append(out, op{fmt.Sprintf("area %s: keeps %s", a.Name, keeps(a)),
				func() error { return st.SetAreaKeep(a.ID, a.MaxMsgs, a.MaxDays) }})
		}
	}
	if d.purge {
		out = append(out, op{"old messages taken out", func() error {
			_, err := d.b.PurgeAreas()
			return err
		}})
	}
	return out
}

// keeps is what an area keeps, in words: "the newest 100, for 30 days".
func keeps(a *store.Area) string {
	m, days := "all messages", "for ever"
	if a.MaxMsgs > 0 {
		m = fmt.Sprintf("the newest %d", a.MaxMsgs)
	}
	if a.MaxDays > 0 {
		days = fmt.Sprintf("for %d days", a.MaxDays)
	}
	return m + ", " + days
}

// noData is a page's word that the database is not to be had.
func noData(err error) []item {
	return []item{{Entry: view.Entry{Kind: view.EntryInfo, Label: "The database",
		Value: "not to be had: " + err.Error(), Disabled: true}}}
}

// usersMenu is Users and groups: the accounts, new and validated users'
// levels, and the [level] sections.
func (e *Editor) usersMenu() page {
	g := sec("general")
	return page{title: "Users and groups", items: func() []item {
		out := []item{menu("User accounts", e.userCount(), func() error { return e.show(e.accountsPage()) })}
		out = append(out, heading("Access levels and flags"))
		out = append(out, e.fields(g, "new_user_level", "validated_level")...)
		out = append(out, menu("Levels", e.count("level", nil, "level"), func() error {
			return e.show(e.listPage("Access levels", "level", "level", nil, func(s confedit.Section) string {
				n, _ := e.doc.Get(s, "name")
				return n
			}, nil))
		}))
		return out
	}}
}

// userCount is how many accounts there will be after the save; "" until the
// database has been read.
func (e *Editor) userCount() string {
	if e.db == nil {
		return ""
	}
	n := 0
	for _, u := range e.db.users {
		if !e.db.deleted[u.ID] || u.ID == 0 {
			n++
		}
	}
	return fmt.Sprintf("%d", n)
}

// accountsPage is the accounts, each with its level, flags, and what is to
// happen to it; A adds one.
func (e *Editor) accountsPage() page {
	return page{title: "User accounts",
		keys: []view.Action{{ID: "add", Label: "Add", Key: 'A', PF: 5}},
		items: func() []item {
			d, err := e.data()
			if err != nil {
				return noData(err)
			}
			var out []item
			for _, u := range d.users {
				u := u
				w, old := d.was[u.ID]
				v := fmt.Sprintf("level %d", u.Level)
				if u.Flags != "" {
					v += ", flags " + u.Flags
				}
				if u.Sysop {
					v += ", sysop"
				}
				if u.Locked {
					v += ", locked"
				}
				if d.deleted[u.ID] && old {
					v = "deleted at the save"
				}
				it := menu(u.Name, v, func() error { return e.show(e.userPage(u)) })
				_, pw := d.passwords[u]
				it.Changed = !old || d.deleted[u.ID] || pw || u.Level != w.Level || u.Flags != w.Flags ||
					u.Sysop != w.Sysop || u.Locked != w.Locked
				out = append(out, it)
			}
			out = append(out, action("Add a user", e.addUser))
			return out
		},
		key: func(id string) error {
			if id == "add" {
				return e.addUser()
			}
			return nil
		}}
}

// addUser asks for a new account's name and password and, if both will do,
// opens its page with it there to be made at the save, at the new users'
// level.  What will not do is said and nothing added.
func (e *Editor) addUser() error {
	d, err := e.data()
	if err != nil {
		e.say(view.Bad, capital(err.Error())+".")
		return nil
	}
	v, ok, err := e.UI.Form(&view.Form{Title: "A new user",
		Fields: []view.Field{{Name: "name", Label: "Name", Width: bbs.MaxNameLen},
			{Name: "pw", Label: "Password", Width: 72, Secret: true}}, Submit: "Add"})
	if err != nil || !ok {
		return err
	}
	name := strings.TrimSpace(v["name"])
	if name == "" {
		return nil
	}
	if msg := bbs.ValidName(name); msg != "" {
		e.say(view.Bad, msg)
		return nil
	}
	for _, u := range d.users {
		if strings.EqualFold(u.Name, name) {
			e.say(view.Bad, name+" is there already.")
			return nil
		}
	}
	if msg := bbs.ValidPassword(v["pw"], name); msg != "" {
		e.say(view.Bad, msg)
		return nil
	}
	lvl := 0
	if l, err := strconv.Atoi(e.value(sec("general"), "new_user_level", "20")); err == nil {
		lvl = l
	}
	u := &store.User{Name: name, Level: lvl}
	d.users = append(d.users, u)
	d.passwords[u] = v["pw"]
	return e.show(e.userPage(u))
}

// value is a key's value as it will be, or def.
func (e *Editor) value(s confedit.Section, key, def string) string {
	if v, ok := e.doc.Get(s, key); ok && v != "" {
		return v
	}
	return def
}

// userPage is an account's page: its level, flags, sysop, locked, a new
// password, and Delete (or Keep, undoing it).  Whoever edits cannot change
// sysop or locked on his own account, or delete it; a new account deleted
// is gone at once (errClose).
func (e *Editor) userPage(u *store.User) page {
	d := e.db
	return page{title: "User " + u.Name, items: func() []item {
		w, old := d.was[u.ID]
		if !old {
			w = store.User{Level: -1}
		}
		self := e.Who != "" && strings.EqualFold(e.Who, u.Name)
		level := item{Entry: view.Entry{ID: "level", Kind: view.EntryText, Label: "Level", Value: strconv.Itoa(u.Level),
			Changed: u.Level != w.Level, Help: "0 to 255: what the [level] at or below it allows."}}
		level.set = func(v string) error {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 || n > bbs.MaxLevel {
				return fmt.Errorf("a level is 0 to %d", bbs.MaxLevel)
			}
			u.Level = n
			return nil
		}
		flags := item{Entry: view.Entry{ID: "flags", Kind: view.EntryText, Label: "Flags", Value: u.Flags,
			Changed: u.Flags != w.Flags && old, Help: "Letters A to Z, for areas, doors and menus that require them."}}
		flags.set = func(v string) error {
			f, err := bbs.EditFlags("", strings.ToUpper(strings.ReplaceAll(v, " ", "")))
			if err != nil {
				return err
			}
			u.Flags = f
			return nil
		}
		boolItem := func(id, label string, p *bool, was bool, help string) item {
			it := item{Entry: view.Entry{ID: id, Kind: view.EntryBool, Label: label, Value: yesno(*p),
				Changed: *p != was && old, Help: help, Disabled: self}}
			if self {
				it.Note = "not your own"
			}
			it.set = func(v string) error {
				*p = v == "yes"
				return nil
			}
			return it
		}
		_, newPW := d.passwords[u]
		pw := item{Entry: view.Entry{ID: "pw", Kind: view.EntryText, Label: "New password", Secret: true,
			Changed: newPW && old, Value: map[bool]string{true: "set", false: ""}[newPW],
			Help: "Typed unseen; nothing typed keeps the one there is."}}
		pw.set = func(v string) error {
			if v == "" {
				return nil
			}
			if msg := bbs.ValidPassword(v, u.Name); msg != "" {
				return errors.New(strings.TrimSuffix(msg, "."))
			}
			d.passwords[u] = v
			return nil
		}
		out := []item{
			{Entry: view.Entry{Kind: view.EntryInfo, Label: "Name", Value: u.Name}},
			level, flags,
			boolItem("sysop", "Sysop", &u.Sysop, w.Sysop, "The sysop's tools, and every area and door."),
			boolItem("locked", "Locked out", &u.Locked, w.Locked, "A locked account cannot log in."),
			pw,
		}
		if old {
			out = append(out, item{Entry: view.Entry{Kind: view.EntryInfo, Label: "Calls, posts",
				Value: fmt.Sprintf("%d, %d", u.Logins, u.Posts)}})
		}
		del := "Delete this user"
		if d.deleted[u.ID] && old {
			del = "Keep this user (not deleted)"
		}
		da := action(del, func() error {
			if !old {
				for i, x := range d.users {
					if x == u {
						d.users = append(d.users[:i], d.users[i+1:]...)
					}
				}
				delete(d.passwords, u)
				return errClose
			}
			d.deleted[u.ID] = !d.deleted[u.ID]
			return nil
		})
		da.Disabled = self
		out = append(out, da)
		return out
	}}
}
