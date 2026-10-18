package server

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/screens"
	"github.com/Neo-Desktop/bbs-sec/internal/script"
)

// customSections reads screens_dir, scripts_dir, the [menu NAME]s and
// [strings].
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
	for _, s := range cf.All("menu") {
		m, err := menu(s)
		if err != nil {
			return fmt.Errorf("[menu %s] %w", s.Name, err)
		}
		if c.Menus[m.Name] != nil {
			return fmt.Errorf("[menu %s] given twice", m.Name)
		}
		c.Menus[m.Name] = m
	}
	// what menus name must be there
	doors := map[string]bool{}
	for _, d := range c.Doors {
		doors[strings.ToLower(d.Name)] = true
	}
	for _, m := range c.Menus {
		for _, it := range m.Items {
			switch it.Action {
			case "menu":
				if c.Menus[strings.ToLower(it.Arg)] == nil {
					return fmt.Errorf("[menu %s] %s: there is no [menu %s]", m.Name, it.Key, it.Arg)
				}
			case "door":
				if !doors[strings.ToLower(it.Arg)] {
					return fmt.Errorf("[menu %s] %s: there is no [door %s]", m.Name, it.Key, it.Arg)
				}
			case "screen":
				if c.Screens == nil {
					return fmt.Errorf("[menu %s] %s: screen:%s, but no screens_dir", m.Name, it.Key, it.Arg)
				}
			case "script":
				if c.ScriptsDir == "" {
					return fmt.Errorf("[menu %s] %s: script:%s, but no scripts_dir", m.Name, it.Key, it.Arg)
				}
				if !script.For(c.ScriptsDir).Has(it.Arg) {
					return fmt.Errorf("[menu %s] %s: there is no %s.js in %s", m.Name, it.Key, it.Arg, c.ScriptsDir)
				}
			}
		}
		if m.Screen != "" && c.Screens == nil {
			return fmt.Errorf("[menu %s] screen = %s, but no screens_dir", m.Name, m.Screen)
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
