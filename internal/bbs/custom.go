package bbs

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/screens"
	"github.com/D-Modem/bbs/internal/store"
)

// Command is one thing a menu item does: a command's name (Commands)
// and its data ("" for none).
type Command struct {
	Name string
	Arg  string
}

func (c Command) String() string {
	if c.Arg == "" {
		return c.Name
	}
	return c.Name + " " + c.Arg
}

// MenuItem is a key of a sysop's menu and what it does.
type MenuItem struct {
	// Key is one character or a word (upper case), or a special key:
	// @FIRST (done when the menu opens), @EVERY (done each time that the
	// menu shows), @ENTER (Enter on an empty prompt).
	Key     string
	Do      []Command // done in turn
	Label   string    // with @-codes; "" for a special key
	Require string    // the access requirement ("": anyone)
	Hidden  bool      // its key works, but it is not in the list
	// Action and Arg are Do[0]'s name and data, as a short [menu] line
	// gives them.
	Action string
	Arg    string
}

// Special is a key that no caller types as it is (@FIRST, @EVERY, @ENTER).
func (it *MenuItem) Special() bool { return strings.HasPrefix(it.Key, "@") }

// Menu is a sysop's menu ([menu NAME] or NAME.menu); "main" replaces the
// built-in one.
type Menu struct {
	Name    string
	Title   string // with @-codes
	Screen  string // a display file shown instead of the item list ("": none)
	Prompt  string // ("": the command prompt)
	Items   []MenuItem
	Require string // to be taken into it ("": anyone)
	Fail    string // the menu for a caller without access ("": stay)
}

// Words is whether a menu has a key of more than one character: then the
// caller types a line, not one key.
func (m *Menu) Words() bool {
	for _, it := range m.Items {
		if !it.Special() && len(it.Key) > 1 {
			return true
		}
	}
	return false
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

// ArgKind is what a command's data is.
type ArgKind int

const (
	NoArg       ArgKind = iota
	MenuArg             // the name of a menu
	DoorArg             // the name of a [door]
	ScreenArg           // the name of a screen in screens_dir
	ScriptArg           // the name of a script in scripts_dir
	AreaArg             // the name of a message area
	OptionalArg         // a word, or nothing
)

// CommandInfo is what a command does, and its data.
type CommandInfo struct {
	Help string
	Arg  ArgKind
	// Words are the data that the command takes, if it takes only these
	// (OptionalArg)
	Words []string
}

// Commands are what menu items can do.
var Commands = map[string]CommandInfo{
	"areas":     {Help: "the message areas"},
	"mail":      {Help: "e-mail"},
	"who":       {Help: "who's online"},
	"users":     {Help: "the user list"},
	"profile":   {Help: "the caller's profile"},
	"info":      {Help: "system info"},
	"files":     {Help: "the file areas"},
	"boards":    {Help: "the image boards"},
	"doors":     {Help: "the doors"},
	"chat":      {Help: "chat"},
	"page":      {Help: "page the sysop"},
	"callers":   {Help: "the last callers"},
	"feedback":  {Help: "mail to the sysop"},
	"qwk":       {Help: "offline mail (QWK packets)"},
	"news":      {Help: "news, weather and quotes"},
	"headlines": {Help: "the news headlines"},
	"weather":   {Help: "the caller's weather"},
	"quotes":    {Help: "stock quotes"},
	"validate":  {Help: "validate new users (sysops only)"},
	"sysop":     {Help: "the sysop's tools (sysops only)"},
	"terminal":  {Help: "a 3270's terminal details (nothing elsewhere)"},
	"logoff":    {Help: "log off"},
	"door":      {Help: "a door", Arg: DoorArg},
	"screen":    {Help: "a display file", Arg: ScreenArg},
	"script":    {Help: "a script", Arg: ScriptArg},
	"area":      {Help: "a message area", Arg: AreaArg},

	"menu.gosub":  {Help: "a menu, and back to this one after it", Arg: MenuArg},
	"menu.goto":   {Help: "a menu in place of this one", Arg: MenuArg},
	"menu.return": {Help: "back to the menu before"},
	"menu.main":   {Help: "back to the main menu"},
}

// commandAliases are other names of commands: the short names of a
// [menu] line, and names in groups.
var commandAliases = map[string]string{
	"menu":          "menu.gosub",
	"back":          "menu.return",
	"msg.areas":     "areas",
	"mail.read":     "mail",
	"mail.feedback": "feedback",
	"info.who":      "who",
	"info.users":    "users",
	"info.system":   "info",
	"info.callers":  "callers",
	"user.profile":  "profile",
	"file.areas":    "files",
}

// MenuActions are the commands that take no data, and what they do.
var MenuActions = func() map[string]string {
	m := map[string]string{"back": "back to the menu before"}
	for k, c := range Commands {
		if c.Arg == NoArg || c.Arg == OptionalArg {
			m[k] = c.Help
		}
	}
	return m
}()

// ParseCommand reads one command: "NAME", "NAME DATA" or "NAME:DATA".
func ParseCommand(s string) (Command, error) {
	s = strings.TrimSpace(s)
	name, arg, _ := strings.Cut(s, " ")
	if n, a, ok := strings.Cut(name, ":"); ok {
		name, arg = n, a+" "+arg
	}
	name, arg = strings.ToLower(name), strings.TrimSpace(arg)
	if c, ok := commandAliases[name]; ok {
		name = c
	}
	info, ok := Commands[name]
	if !ok {
		return Command{}, fmt.Errorf("%q: no such command", name)
	}
	switch info.Arg {
	case NoArg:
		if arg != "" {
			return Command{}, fmt.Errorf("%s takes no data", name)
		}
	case OptionalArg:
		arg = strings.ToLower(arg)
		if arg != "" && !slices.Contains(info.Words, arg) {
			return Command{}, fmt.Errorf("%s %s: the data is one of %s", name, arg, strings.Join(info.Words, ", "))
		}
	default:
		if arg == "" {
			return Command{}, fmt.Errorf("%s needs a name", name)
		}
		if (info.Arg == ScreenArg || info.Arg == ScriptArg) && !screens.ValidName(arg) {
			return Command{}, fmt.Errorf("%s %s: a %s's name is lower case letters, digits, _ and -", name, arg, name)
		}
		if info.Arg == MenuArg {
			arg = strings.ToLower(arg)
		}
	}
	return Command{Name: name, Arg: arg}, nil
}

// ParseCommands reads commands separated by semicolons.
func ParseCommands(s string) ([]Command, error) {
	var out []Command
	for _, part := range strings.Split(s, ";") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		c, err := ParseCommand(part)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("no command")
	}
	return out, nil
}

// specialKeys are the keys that a caller does not type as they are.
var specialKeys = []string{"@FIRST", "@EVERY", "@ENTER"}

// ParseMenuKey reads a menu key: one character (not a space), a word of
// letters and digits (at most 12), or a special key.
func ParseMenuKey(key string) (string, error) {
	k := strings.ToUpper(strings.TrimSpace(key))
	if strings.HasPrefix(k, "@") {
		if slices.Contains(specialKeys, k) {
			return k, nil
		}
		return "", fmt.Errorf("%q: the special keys are %s", key, strings.ToLower(strings.Join(specialKeys, ", ")))
	}
	r := []rune(k)
	if len(r) == 1 && r[0] >= '!' && r[0] <= '~' {
		return k, nil
	}
	if len(r) < 2 || len(r) > 12 {
		return "", fmt.Errorf("%q: a menu key is one character or a word of 2 to 12 letters and digits", key)
	}
	for _, c := range r {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return "", fmt.Errorf("%q: a menu key is one character or a word of 2 to 12 letters and digits", key)
		}
	}
	return k, nil
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
	v, set := n.b.text()[key]
	if !set {
		v = Strings[key]
	}
	return screens.String(v, n.Vars()), set
}

// StrMarked is Str with its tags kept, marked (screens.Marked), for a
// text terminal to print.
func (n *Node) StrMarked(key string) (string, bool) {
	v, set := n.b.text()[key]
	if !set {
		v = Strings[key]
	}
	return screens.Marked(v, n.Vars()), set
}

// Vars are the @-codes' values for the node's caller:
//
//	BBS SYSOP LOCATION         the system
//	PHONE ADDRESSES POSTAL     how to reach it
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
			v = n.b.Conf().Name
		case "SYSOP":
			v = n.b.Conf().Sysop
		case "LOCATION":
			v = n.b.Conf().Location
		case "PHONE":
			v = n.b.Conf().Phone
		case "ADDRESSES":
			v = n.b.Conf().Addresses
		case "POSTAL":
			v = n.b.Conf().PostalAddress
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

// ParseMenuItem reads a [menu] line: KEY = "ACTION Label words",
// perhaps with "| requirement" after it.
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
	c, err := ParseCommand(action)
	if err != nil {
		if strings.Contains(err.Error(), "no such command") {
			return MenuItem{}, fmt.Errorf("%q: no such action", action)
		}
		return MenuItem{}, fmt.Errorf("%s: %v", key, err)
	}
	if strings.Contains(action, ":") {
		// door:LORD keeps its case, menu:extras is lower case
		_, c.Arg, _ = strings.Cut(action, ":")
		if Commands[c.Name].Arg == MenuArg {
			c.Arg = strings.ToLower(c.Arg)
		}
		if c.Arg == "" {
			return MenuItem{}, fmt.Errorf("%s: a name after the colon", action)
		}
	}
	it := MenuItem{Key: string(k), Label: strings.TrimSpace(label), Require: strings.TrimSpace(req),
		Do: []Command{c}}
	it.Action, it.Arg = shortAction(c)
	if it.Label == "" {
		return MenuItem{}, fmt.Errorf("%s: a label after the action", key)
	}
	return it, nil
}

// shortAction is a command's name as a [menu] line gives it: menu for
// menu.gosub, back for menu.return.
func shortAction(c Command) (string, string) {
	switch c.Name {
	case "menu.gosub":
		return "menu", c.Arg
	case "menu.return":
		return "back", ""
	}
	return c.Name, c.Arg
}

// ParseMenuFile reads a menu file (NAME.menu): a [menu] section with
// title, prompt, screen, require and fail, then an [item KEY] section for
// each item, with label, do, require and hidden.
func ParseMenuFile(name string, cf *config.File) (*Menu, error) {
	name = strings.ToLower(name)
	if !screens.ValidName(name) {
		return nil, fmt.Errorf("%s: a menu's name is lower case letters, digits, _ and -", name)
	}
	m := &Menu{Name: name, Title: name}
	seen := map[string]bool{}
	for _, s := range cf.Sections {
		switch s.Kind {
		case "menu":
			if s.Name != "" {
				return nil, fmt.Errorf("line %d: [menu] has no name in a menu file", s.Line)
			}
			m.Title = s.String("title", name)
			m.Screen = s.String("screen", "")
			m.Prompt = s.String("prompt", "")
			m.Require = s.String("require", "")
			m.Fail = strings.ToLower(s.String("fail", ""))
			if _, err := ParseRequire(m.Require); err != nil {
				return nil, fmt.Errorf("line %d: require: %v", s.Line, err)
			}
			if m.Screen != "" && !screens.ValidName(m.Screen) {
				return nil, fmt.Errorf("line %d: screen: a screen's name is lower case letters, digits, _ and -", s.Line)
			}
			if bad := unknownKeys(s, "title", "screen", "prompt", "require", "fail"); bad != "" {
				return nil, fmt.Errorf("line %d: [menu] has no %s", s.Line, bad)
			}
		case "item":
			key, err := ParseMenuKey(s.Name)
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", s.Line, err)
			}
			if seen[key] {
				return nil, fmt.Errorf("line %d: [item %s] given twice", s.Line, s.Name)
			}
			seen[key] = true
			it := MenuItem{Key: key, Label: s.String("label", ""), Require: s.String("require", "")}
			if _, err := ParseRequire(it.Require); err != nil {
				return nil, fmt.Errorf("line %d: require: %v", s.Line, err)
			}
			if it.Hidden, err = s.Bool("hidden", false); err != nil {
				return nil, fmt.Errorf("line %d: %v", s.Line, err)
			}
			if it.Do, err = ParseCommands(s.String("do", "")); err != nil {
				return nil, fmt.Errorf("line %d: [item %s] do: %v", s.Line, s.Name, err)
			}
			it.Action, it.Arg = shortAction(it.Do[0])
			if it.Label == "" && !it.Special() && !it.Hidden {
				return nil, fmt.Errorf("line %d: [item %s] needs a label (or hidden = yes)", s.Line, s.Name)
			}
			if bad := unknownKeys(s, "label", "do", "require", "hidden"); bad != "" {
				return nil, fmt.Errorf("line %d: [item %s] has no %s", s.Line, s.Name, bad)
			}
			m.Items = append(m.Items, it)
		default:
			return nil, fmt.Errorf("line %d: [%s]: a menu file has [menu] and [item KEY] sections", s.Line, s.Kind)
		}
	}
	if len(m.Items) == 0 {
		return nil, errors.New("no items: [item KEY] with label and do")
	}
	return m, nil
}

// unknownKeys is the first key of s not in known ("": none).
func unknownKeys(s *config.Section, known ...string) string {
	for _, k := range s.Keys() {
		if !slices.Contains(known, k) {
			return k
		}
	}
	return ""
}

// Item finds a menu's item by its key (not a special key).
func (m *Menu) Item(key string) *MenuItem {
	key = strings.ToUpper(strings.TrimSpace(key))
	if strings.HasPrefix(key, "@") {
		return nil
	}
	for i := range m.Items {
		if m.Items[i].Key == key {
			return &m.Items[i]
		}
	}
	return nil
}

// Special finds a menu's special item (@FIRST, @EVERY, @ENTER), or nil.
func (m *Menu) Special(key string) *MenuItem {
	for i := range m.Items {
		if m.Items[i].Key == key {
			return &m.Items[i]
		}
	}
	return nil
}
