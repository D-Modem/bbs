package bbs

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Prefs are a user's settings, kept with the account as "key=value"
// pairs: how their terminal is to be treated, and their choices of how the
// BBS works for them.
type Prefs map[string]string

// prefValues are the settings there are, and the values each takes.
var prefValues = map[string][]string{
	"ansi":   {"on", "off"},    // ANSI color and cursor movement
	"utf8":   {"on", "off"},    // UTF-8 rather than CP437 (or ASCII)
	"sixel":  {"on", "off"},    // sixel graphics, where the terminal has them
	"rip":    {"on", "off"},    // RIPscrip menus, where the terminal has them
	"width":  {"40", "80"},     // a Commodore's columns
	"editor": {"full", "line"}, // the full-screen editor, or the line editor
}

// PrefKeys are the settings' names, sorted.
func PrefKeys() []string {
	var out []string
	for k := range prefValues {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CheckPref says what is wrong with a setting, or nil.
func CheckPref(key, value string) error {
	vs, ok := prefValues[key]
	if !ok {
		return fmt.Errorf("there is no setting %q", key)
	}
	for _, v := range vs {
		if v == value {
			return nil
		}
	}
	return fmt.Errorf("%s is one of %s", key, strings.Join(vs, ", "))
}

// ParsePrefs reads a user's settings, leaving out any not known (or no
// longer).
func ParsePrefs(s string) Prefs {
	p := Prefs{}
	for _, f := range strings.Fields(s) {
		k, v, ok := strings.Cut(f, "=")
		if ok && CheckPref(k, v) == nil {
			p[k] = v
		}
	}
	return p
}

// String is the settings as they are kept.
func (p Prefs) String() string {
	var out []string
	for _, k := range PrefKeys() {
		if v, ok := p[k]; ok {
			out = append(out, k+"="+v)
		}
	}
	return strings.Join(out, " ")
}

// On is whether a setting is on; ok false if the user has not set it.
func (p Prefs) On(key string) (on, ok bool) {
	v, ok := p[key]
	return v == "on", ok
}

// ChangePrefs is kept settings with changes made (a value "": forget it,
// back to what the terminal says), or what is wrong with a change.
func ChangePrefs(kept string, changes map[string]string) (string, error) {
	p := ParsePrefs(kept)
	for k, v := range changes {
		if v == "" {
			if _, ok := prefValues[k]; !ok {
				return "", fmt.Errorf("there is no setting %q", k)
			}
			delete(p, k)
			continue
		}
		if err := CheckPref(k, v); err != nil {
			return "", err
		}
		p[k] = v
	}
	return p.String(), nil
}

// errNoUser: no one is logged in on the node.
var errNoUser = errors.New("no one is logged in")

// Prefs are the user's settings.
func (n *Node) Prefs() Prefs {
	if u := n.User(); u != nil {
		return ParsePrefs(u.Prefs)
	}
	return Prefs{}
}

// SetPref keeps one of the user's settings.
func (n *Node) SetPref(key, value string) error {
	if err := CheckPref(key, value); err != nil {
		return err
	}
	u := n.Refresh()
	if u == nil {
		return errNoUser
	}
	if ParsePrefs(u.Prefs)[key] == value {
		return nil
	}
	prefs, _ := ChangePrefs(u.Prefs, map[string]string{key: value})
	if err := n.b.Store.SetPrefs(u.ID, prefs); err != nil {
		return err
	}
	n.Refresh()
	return nil
}
