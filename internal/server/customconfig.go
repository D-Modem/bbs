package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/D-Modem/bbs/internal/app"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/screens"
	"github.com/D-Modem/bbs/internal/script"
)

// customSections reads screens_dir, scripts_dir, the [menu NAME]s,
// menus_dir's NAME.menu files and [strings].
func (c *Config) customSections(cf *config.File) error {
	if dir := cf.Get("general", "").String("screens_dir", ""); dir != "" {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			c.Warnings = append(c.Warnings, fmt.Sprintf("[general] screens_dir %s is not a directory: no screens", dir))
		}
		c.Screens = &screens.Set{Dir: dir}
	}
	if dir := cf.Get("general", "").String("scripts_dir", ""); dir != "" {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			return fmt.Errorf("[general] scripts_dir %s is not a directory", dir)
		}
		c.ScriptsDir = dir
		// what is there compiles, so that a mistake shows now, not to a caller
		e := script.For(dir)
		for _, name := range e.Names() {
			if err := e.Check(name); err != nil {
				return fmt.Errorf("scripts_dir: %s.js: %v", name, err)
			}
		}
	}
	c.Menus = map[string]*bbs.Menu{}
	where := map[string]string{} // where each menu is given, for messages
	for _, s := range cf.All("menu") {
		m, err := menu(s)
		if err != nil {
			return fmt.Errorf("[menu %s] %w", s.Name, err)
		}
		if c.Menus[m.Name] != nil {
			return fmt.Errorf("[menu %s] given twice", m.Name)
		}
		c.Menus[m.Name] = m
		where[m.Name] = "[menu " + m.Name + "]"
	}
	if dir := cf.Get("general", "").String("menus_dir", ""); dir != "" {
		files, err := filepath.Glob(filepath.Join(dir, "*.menu"))
		if fi, serr := os.Stat(dir); serr != nil || !fi.IsDir() || err != nil {
			return fmt.Errorf("[general] menus_dir %s is not a directory", dir)
		}
		for _, p := range files {
			name := strings.ToLower(strings.TrimSuffix(filepath.Base(p), ".menu"))
			mf, err := config.Load(p)
			if err != nil {
				return fmt.Errorf("menus_dir: %v", err)
			}
			m, err := bbs.ParseMenuFile(name, mf)
			if err != nil {
				return fmt.Errorf("menus_dir: %s: %v", filepath.Base(p), err)
			}
			if c.Menus[m.Name] != nil {
				return fmt.Errorf("menus_dir: %s: the menu %s is also given as [menu %s] in the configuration", filepath.Base(p), m.Name, m.Name)
			}
			c.Menus[m.Name] = m
			where[m.Name] = filepath.Base(p)
		}
	}
	// what menus name must be there
	doors := map[string]bool{}
	for _, d := range c.Doors {
		doors[strings.ToLower(d.Name)] = true
	}
	hasMenu := func(name string) bool { return c.Menus[name] != nil || app.BuiltinMenu(name) != nil }
	for _, m := range c.Menus {
		at := where[m.Name]
		if m.Fail != "" && !hasMenu(m.Fail) {
			return fmt.Errorf("%s: fail = %s, but there is no menu %s", at, m.Fail, m.Fail)
		}
		for _, it := range m.Items {
			for _, cmd := range it.Do {
				if err := c.checkCommand(cmd, hasMenu, doors); err != nil {
					return fmt.Errorf("%s %s: %v", at, it.Key, err)
				}
			}
		}
		if m.Screen != "" && c.Screens == nil {
			return fmt.Errorf("%s screen = %s, but no screens_dir", at, m.Screen)
		}
	}
	c.Text = map[string]string{}
	if s := cf.Get("strings", ""); s != nil {
		for _, k := range s.Keys() {
			if _, ok := bbs.Strings[k]; !ok {
				return fmt.Errorf("[strings] %s: no such string (%s)", k, strings.Join(stringKeys(), ", "))
			}
			c.Text[k] = s.String(k, "")
		}
	}
	return nil
}

// checkCommand: what a menu command names is there.
func (c *Config) checkCommand(cmd bbs.Command, hasMenu func(string) bool, doors map[string]bool) error {
	switch bbs.Commands[cmd.Name].Arg {
	case bbs.MenuArg:
		if !hasMenu(cmd.Arg) {
			return fmt.Errorf("there is no [menu %s]", cmd.Arg)
		}
	case bbs.DoorArg:
		if !doors[strings.ToLower(cmd.Arg)] {
			return fmt.Errorf("there is no [door %s]", cmd.Arg)
		}
	case bbs.ScreenArg:
		if c.Screens == nil {
			return fmt.Errorf("screen:%s, but no screens_dir", cmd.Arg)
		}
	case bbs.ScriptArg:
		if c.ScriptsDir == "" {
			return fmt.Errorf("script:%s, but no scripts_dir", cmd.Arg)
		}
		if !script.For(c.ScriptsDir).Has(cmd.Arg) {
			return fmt.Errorf("there is no %s.js in %s", cmd.Arg, c.ScriptsDir)
		}
	}
	return nil
}

// stringKeys is the names of the strings [strings] may set, sorted.
func stringKeys() []string {
	var out []string
	for k := range bbs.Strings {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// menu reads a [menu NAME] (its name lower-cased): its title, screen,
// prompt, requirement and items, at least one, no key twice.
func menu(s *config.Section) (*bbs.Menu, error) {
	name := strings.ToLower(s.Name)
	if name == "" {
		return nil, errors.New("a menu needs a name: [menu main]")
	}
	m := &bbs.Menu{Name: name, Title: s.String("title", s.Name), Screen: s.String("screen", ""),
		Prompt: s.String("prompt", ""), Require: s.String("require", "")}
	if _, err := bbs.ParseRequire(m.Require); err != nil {
		return nil, fmt.Errorf("require: %v", err)
	}
	if m.Screen != "" && !screens.ValidName(m.Screen) {
		return nil, errors.New("screen: a screen's name is lower case letters, digits, _ and -")
	}
	seen := map[string]bool{}
	for _, k := range s.Keys() {
		switch k {
		case "title", "screen", "prompt", "require":
			continue
		}
		it, err := bbs.ParseMenuItem(k, s.String(k, ""))
		if err != nil {
			return nil, err
		}
		if seen[it.Key] {
			return nil, fmt.Errorf("%s given twice", it.Key)
		}
		seen[it.Key] = true
		m.Items = append(m.Items, it)
	}
	if len(m.Items) == 0 {
		return nil, errors.New("no items: KEY = ACTION Label, e.g. m = areas Message areas")
	}
	return m, nil
}
