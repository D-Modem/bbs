package confedit

import (
	"regexp"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/config"
)

// Catalog is what the sections and keys are for, as the sample file
// says.
type Catalog struct {
	Kinds  []*Kind
	byName map[string]*Kind
}

// Kind is a kind of section: [general], [listen NAME] ...
type Kind struct {
	Name  string
	Help  string
	Named bool   // [KIND NAME]: there may be many, each with a name
	Free  string // keys of the sysop's own (a menu's item keys): what they are ("": none)
	Keys  []*Key
	byKey map[string]*Key
}

// Key is a key of a kind of section.
type Key struct {
	Name     string
	Help     string
	Default  string   // as the sample says ("": see the help)
	Examples []string // values the sample shows
}

var (
	listenDocRE  = regexp.MustCompile(`^;\s{2,4}([a-z0-9_]+) = (\S+)\s*(.*)$`)
	listenMoreRE = regexp.MustCompile(`^;\s{5,}(.*)$`)
)

// kinds' help where the sample's comments before them are about the
// first example, not the kind
var kindHelp = map[string]string{
	"general": "The system: its name, database, log, logins, and what callers see.",
	"listen":  "A listener: a port (or unix socket) callers come in by, and how their terminals are treated.",
	"menu":    "A menu of the sysop's: [menu main] takes the place of the built-in main menu; others are reached from it (menu:NAME).",
	"strings": "The prompts and messages, with @-codes and tags.",
	"level":   "An access level: a user's level gets what the [level] at or below it says.",
	"door":    "A door: a program callers play, or another system's doors.",
}

// keys the sample shows only in prose
var extraKeys = map[string][]Key{
	"general": {
		{Name: "new_user_level", Default: "20", Help: "Access levels: a new user's level (0-255)."},
		{Name: "validated_level", Default: "20", Help: "Access levels: the level a sysop's validation gives a user."},
	},
	"menu": {
		{Name: "require", Help: "The access a caller needs to be taken into the menu (a level and flags; see docs/wiki/Access-Levels.md)."},
	},
}

var freeKeys = map[string]string{
	"menu":   "a menu item: KEY = ACTION Label, KEY one character; ACTION areas, mail, who, users, profile, info, files, boards, doors, chat, page, callers, feedback, qwk, sysop, terminal, back, logoff, or door:NAME menu:NAME screen:NAME script:NAME area:NAME; |REQUIRE after the label limits it",
	"qwknet": "a conference carried here: CONFERENCE = AREA (the hub's conference number, this BBS's area)",
}

// NewCatalog reads the sample's comments: a block of comments before a
// section's header is its help, before a key the help of it and the keys
// after it.
func NewCatalog(sample []byte) *Catalog {
	c := &Catalog{byName: map[string]*Kind{}}
	var cur *Kind
	var prose []string
	group := ""
	listenDoc := false
	var lastDoc *Key
	for _, raw := range strings.Split(string(sample), "\n") {
		line := strings.TrimRight(raw, " \t\r")
		t := strings.TrimSpace(line)
		if t == "" {
			prose, group, listenDoc = nil, "", false
			continue
		}
		if listenDoc {
			if m := listenDocRE.FindStringSubmatch(t); m != nil {
				k := c.kind("listen").key(m[1])
				k.Default = m[2]
				k.Help = m[3]
				lastDoc = k
				continue
			}
			if m := listenMoreRE.FindStringSubmatch(t); m != nil && lastDoc != nil {
				lastDoc.Help += " " + strings.TrimSpace(m[1])
				continue
			}
		}
		if s, ok := header(t); ok {
			cur = c.header(s, prose)
			prose, group = nil, ""
			continue
		}
		if s, ok := commentedHeader(t); ok {
			cur = c.header(s, prose)
			prose, group = nil, ""
			continue
		}
		k, v, isKey := keyLine(t)
		if !isKey {
			if ek, ok := example(t); ok {
				_, v, _ = strings.Cut(t, "=")
				k, v, isKey = ek, strings.TrimSpace(v), true
			}
		}
		if isKey {
			if cur == nil {
				continue
			}
			if prose != nil {
				group, prose = strings.Join(prose, " "), nil
			}
			key := cur.key(k)
			if key.Help == "" {
				key.Help = group
			}
			if uv, err := config.Unquote(v); err == nil {
				v = uv
			}
			key.example(v)
			continue
		}
		if t[0] == ';' || t[0] == '#' {
			p := strings.TrimSpace(t[1:])
			if p == "A listener:" {
				listenDoc = true
				c.kind("listen")
				prose = nil
				continue
			}
			if p != "" {
				prose = append(prose, p)
			}
		}
	}
	for kind, keys := range extraKeys {
		for _, x := range keys {
			k := c.kind(kind).key(x.Name)
			if k.Help == "" {
				k.Help = x.Help
			}
			if k.Default == "" {
				k.Default = x.Default
			}
		}
	}
	for kind, help := range freeKeys {
		kd := c.kind(kind)
		kd.Free = help
		var keep []*Key
		for _, k := range kd.Keys {
			if kd.free(k.Name) {
				delete(kd.byKey, k.Name)
				continue
			}
			keep = append(keep, k)
		}
		kd.Keys = keep
	}
	for name, help := range kindHelp {
		c.kind(name).Help = help
	}
	return c
}

// header is the kind of section s is, made Named if s has a name, and given
// the prose before the header as its help if it has none yet.
func (c *Catalog) header(s Section, prose []string) *Kind {
	k := c.kind(s.Kind)
	if s.Name != "" {
		k.Named = true
	}
	if k.Help == "" && len(prose) > 0 {
		k.Help = strings.Join(prose, " ")
	}
	return k
}

// kind is the kind of section name, made (and listed) if it is new.
func (c *Catalog) kind(name string) *Kind {
	if k := c.byName[name]; k != nil {
		return k
	}
	k := &Kind{Name: name, byKey: map[string]*Key{}}
	c.byName[name] = k
	c.Kinds = append(c.Kinds, k)
	return k
}

// key is the kind's key name, made (and listed) if it is new.
func (k *Kind) key(name string) *Key {
	if x := k.byKey[name]; x != nil {
		return x
	}
	x := &Key{Name: name}
	k.byKey[name] = x
	k.Keys = append(k.Keys, x)
	return x
}

// example adds v to the key's examples, unless it is one already.
func (x *Key) example(v string) {
	for _, e := range x.Examples {
		if e == v {
			return
		}
	}
	x.Examples = append(x.Examples, v)
}

// free reports whether a key is one of the sysop's own (a menu item's).
func (k *Kind) free(key string) bool {
	switch k.Name {
	case "menu":
		return len([]rune(key)) == 1
	case "qwknet":
		return key != "" && strings.Trim(key, "0123456789") == ""
	}
	return false
}

// Kind is a kind of section (nil: none such).
func (c *Catalog) Kind(name string) *Kind {
	return c.byName[name]
}

// Key is a key of a kind of section: one of the sample's, or one of the
// sysop's own (a menu's item); nil if it is neither.
func (c *Catalog) Key(kind, key string) *Key {
	k := c.byName[kind]
	if k == nil {
		return nil
	}
	if x := k.byKey[key]; x != nil {
		return x
	}
	if k.free(key) {
		return &Key{Name: key, Help: strings.ToUpper(k.Free[:1]) + k.Free[1:] + "."}
	}
	return nil
}

// what takes effect while the BBS runs (a reload: SIGHUP, or the sysop's
// Configuration); the rest when it starts again
var liveKeys = map[string]map[string]bool{
	"general": set("name", "sysop", "location", "postal_address", "phone", "addresses", "banner", "banner_file", "motd", "motd_file",
		"goodbye", "new_users", "max_nodes", "attempts", "fail_delay", "login_timeout",
		"idle_timeout", "throttle_failures", "throttle_time", "page_command", "qwk_id",
		"geoip", "geoip_credit", "geoip_credit_url", "phone_country", "new_user_level",
		"validated_level"),
	"http": set("forum_guests", "forum_theme", "forum_flags"),
}

// kinds all of whose keys are live (but chan's dir)
var liveKinds = set("level", "menu", "strings", "chan", "backup")

// set is the set of ks.
func set(ks ...string) map[string]bool {
	m := map[string]bool{}
	for _, k := range ks {
		m[k] = true
	}
	return m
}

// Live reports whether a key takes effect at a reload, without the BBS
// starting again (key "": the section itself made or taken out).
func Live(kind, key string) bool {
	if key == "" {
		return liveKinds[kind] || kind == "general"
	}
	if kind == "chan" && key == "dir" {
		return false
	}
	return liveKinds[kind] || liveKeys[kind][key]
}

// Live reports whether a change takes effect at a reload.
func (c Change) Live() bool {
	return Live(c.Section.Kind, c.Key)
}
