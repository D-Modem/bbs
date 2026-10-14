package bbs

import (
	"fmt"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/screens"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// MenuItem is a key of a sysop's menu and what it does.
type MenuItem struct {
	Key     string // one character, upper case
	Action  string // see MenuActions; or door, menu, screen, area with Arg
	Arg     string
	Label   string // with @-codes
	Require string // the access requirement ("": anyone)
}

// Menu is a sysop's menu ([menu NAME]); "main" replaces the built-in one.
type Menu struct {
	Name    string
	Title   string // with @-codes
	Screen  string // a display file shown instead of the item list ("": none)
	Prompt  string // ("": the command prompt)
	Items   []MenuItem
	Require string // to be taken into it ("": anyone)
}

// MenuFor is m with only the items u may choose.
func (b *BBS) MenuFor(m *Menu, u *store.User) *Menu {
	out := *m
	out.Items = nil
	for _, it := range m.Items {
		if MustRequire(it.Require).Allows(u) {
			out.Items = append(out.Items, it)
		}
	}
	return &out
}

// MenuActions are what a menu item may do by itself; door:NAME,
// menu:NAME, screen:NAME and area:NAME take a name.
var MenuActions = map[string]string{
	"areas":    "the message areas",
	"mail":     "e-mail",
	"who":      "who's online",
	"users":    "the user list",
	"profile":  "the caller's profile",
	"info":     "system info",
	"files":    "the file areas",
	"boards":   "the image boards",
	"doors":    "the doors",
	"chat":     "chat",
	"page":     "page the sysop",
	"callers":  "the last callers",
	"feedback": "mail to the sysop",
	"qwk":      "offline mail (QWK packets)",
	"sysop":    "the sysop's tools (sysops only)",
	"terminal": "a 3270's terminal details (nothing elsewhere)",
	"back":     "back to the menu before",
	"logoff":   "log off",
}

// Strings are the prompts and messages a sysop may change ([strings]),
// with their defaults; @-codes are filled in.
var Strings = map[string]string{
	"name_prompt":     "Name: ",
	"new_name_prompt": "Name (or NEW): ",
	"password_prompt": "Password: ",
	"command_prompt":  "Command: ",
	"pause":           "[Press a key]",
	"logoff_confirm":  "Log off?",
	"welcome":         "Welcome, @USER@.",
}

// Str is a string, the sysop's or the default, its @-codes filled in for
// the node and its tags ({red}: screens.Tags) dropped; and whether the
// sysop set it.
func (n *Node) Str(key string) (string, bool) {
	v, set := n.b.Text[key]
	if !set {
		v = Strings[key]
	}
	return screens.String(v, n.Vars()), set
}

// StrMarked is Str with its tags kept, marked (screens.Marked), for a
// text terminal to print.
func (n *Node) StrMarked(key string) (string, bool) {
	v, set := n.b.Text[key]
	if !set {
		v = Strings[key]
	}
	return screens.Marked(v, n.Vars()), set
}

// Vars are the @-codes' values for the node's caller:
//
//	BBS SYSOP LOCATION         the system
//	USER USERLOC CALLS POSTS   the caller
//	LASTON SINCE NEWMAIL
//	NODE VIA BAUD ONLINE       this call, and how many are on
//	DATE TIME
func (n *Node) Vars() screens.Vars {
	return func(code string) (string, bool) {
		u := n.User()
		v := ""
		switch code {
		case "BBS":
			v = n.b.Name
		case "SYSOP":
			v = n.b.Sysop
		case "LOCATION":
			v = n.b.Location
		case "NODE":
			v = fmt.Sprint(n.Num)
		case "VIA":
			v = n.Caller.Via
		case "BAUD":
			v = n.Caller.Baud()
		case "ONLINE":
			on := 0
			for _, x := range n.b.Nodes() {
				if x.User != "" {
					on++
				}
			}
			v = fmt.Sprint(on)
		case "DATE":
			v = time.Now().Format("2006-01-02")
		case "TIME":
			v = time.Now().Format("15:04")
		case "USER", "USERLOC", "CALLS", "POSTS", "LASTON", "SINCE", "NEWMAIL":
			if u == nil {
				return "", true
			}
			switch code {
			case "USER":
				v = u.Name
			case "USERLOC":
				v = u.Location
			case "CALLS":
				v = fmt.Sprint(u.Logins)
			case "POSTS":
				v = fmt.Sprint(u.Posts)
			case "LASTON":
				if u.LastLogin.IsZero() {
					v = "never"
				} else {
					v = u.LastLogin.Format("2006-01-02 15:04")
				}
			case "SINCE":
				v = u.Created.Format("2006-01-02")
			case "NEWMAIL":
				c, _ := n.b.Store.UnreadMail(u.ID)
				v = fmt.Sprint(c)
			}
		default:
			return "", false
		}
		return Clean(v, 200), true
	}
}

// ParseMenuItem reads a menu line: "ACTION Label words".
func ParseMenuItem(key, value string) (MenuItem, error) {
	k := []rune(strings.ToUpper(key))
	if len(k) != 1 || k[0] < '!' || k[0] > '~' {
		return MenuItem{}, fmt.Errorf("%q: a menu key is one character", key)
	}
	value, req, _ := strings.Cut(value, "|")
	if _, err := ParseRequire(req); err != nil {
		return MenuItem{}, fmt.Errorf("%s: %v", key, err)
	}
	action, label, _ := strings.Cut(strings.TrimSpace(value), " ")
	it := MenuItem{Key: string(k), Label: strings.TrimSpace(label), Require: strings.TrimSpace(req)}
	if a, arg, ok := strings.Cut(action, ":"); ok {
		switch a {
		case "door", "menu", "area":
		case "screen", "script":
			if !screens.ValidName(arg) {
				return MenuItem{}, fmt.Errorf("%s: a %s's name is lower case letters, digits, _ and -", action, a)
			}
		default:
			return MenuItem{}, fmt.Errorf("%s: no such action", action)
		}
		if arg == "" {
			return MenuItem{}, fmt.Errorf("%s: a name after the colon", action)
		}
		it.Action, it.Arg = a, arg
	} else {
		if _, ok := MenuActions[action]; !ok {
			return MenuItem{}, fmt.Errorf("%q: no such action", action)
		}
		it.Action = action
	}
	if it.Label == "" {
		return MenuItem{}, fmt.Errorf("%s: a label after the action", key)
	}
	return it, nil
}

// Item finds a menu's item by its key.
func (m *Menu) Item(key string) *MenuItem {
	key = strings.ToUpper(strings.TrimSpace(key))
	for i := range m.Items {
		if m.Items[i].Key == key {
			return &m.Items[i]
		}
	}
	return nil
}
