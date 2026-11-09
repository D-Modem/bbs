package cis

// GO DEFAULT (TERMINAL, PROFILE's first choice): the terminal settings and
// service options, session and permanent, as CompuServe's menus had them
// in 1991.  TAPCIS's PARAMS.SCR (ALT-P) walks these screens to set a new
// member up, so the menus, their order, the bracketed values and the
// prompts follow its transcript.  The settings are kept as the user's
// prefs (cis-paged, cis-width...).  What they do here: PAGED, BRIEF, WIDTH
// and LINES (the session's, from the logon), Mail waiting (GO MAIL at the
// logon), the online editor (Mail's, when Mail has none of its own), and
// the protocol preference (no protocol menu).  The rest are kept and
// shown, as a script expects, and change nothing.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
)

// tchoice is one of a setting's values.
type tchoice struct {
	value string   // as kept
	label string   // its menu line
	help  []string // the lines under it
	shown string   // in the brackets ("" for its label's first word, in capitals)
}

// tsetting is a permanent setting.
type tsetting struct {
	key     string   // its pref, without "cis-"
	label   string   // its line in its group's menu
	under   []string // lines under that line
	before  string   // a heading before that line
	title   string   // its screen's heading
	text    []string // before "Permanent setting ="
	after   []string // after it
	choices []tchoice
	lo, hi  int // a number from lo to hi, when hi > 0
	prompt  string
	def     string
	none    []string // not a setting: a screen with this, and back
}

// tgroup is one of PERMANENT SETTINGS' menus.
type tgroup struct {
	label, title string
	items        []*tsetting
	extra        []string
}

var yesNoChoices = []tchoice{{value: "yes", label: "YES"}, {value: "no", label: "NO"}}

func yesNoHelp(yes, no []string) []tchoice {
	return []tchoice{{value: "yes", label: "YES", help: yes}, {value: "no", label: "NO", help: no}}
}

var (
	setPaged = &tsetting{key: "paged", label: "PAGED display", title: "PAGED DISPLAY", def: "no",
		choices: yesNoHelp(
			[]string{"   (Output is presented in pages;", "   you are asked to enter <CR> after", "   each page.)", ""},
			[]string{"   (Output is scrolled continuously", "   to end of article.)"})}
	setBrief = &tsetting{key: "brief", label: "BRIEF prompts", title: "BRIEF PROMPTS", def: "no",
		choices: []tchoice{
			{value: "yes", label: "YES - expert mode", help: []string{"   (You will receive abbreviated", "   prompts such as ! or More!.)"}},
			{value: "no", label: "NO - novice/intermediate mode", help: []string{"   (You will receive full prompts", "   such as Enter choice! or Enter", "   <CR> for more!.)"}},
		}}
	setWidth = &tsetting{key: "width", label: "Screen WIDTH", title: "SCREEN WIDTH", def: "80",
		after: []string{"", "This determines the number of characters", "per line."},
		lo:    40, hi: 132, prompt: "Enter characters per line, 40 to 132, or <CR> for no change:"}
	setLines = &tsetting{key: "lines", label: "LINES per page", title: "LINES PER PAGE", def: "0",
		lo: 0, hi: 200, prompt: "Enter lines per page, 0 to 200, or <CR> for no change:"}
	setTerm = &tsetting{key: "term", label: "TERMINAL type", title: "TERMINAL TYPE", def: "other",
		choices: []tchoice{
			{value: "vidtex", label: "VIDTEX (Professional Connection)"},
			{value: "ansi", label: "ANSI"},
			{value: "vt100", label: "VT100"},
			{value: "vt52", label: "VT52"},
			{value: "heath", label: "Heath (Zenith)"},
			{value: "adm", label: "ADM"},
			{value: "crt", label: "CRT"},
			{value: "other", label: "Other"},
		}}
	setProtocol = &tsetting{key: "protocol", label: "PROTOCOL preference", before: "FILE TRANSFER PROTOCOL",
		title: "PROTOCOL PREFERENCE", def: "menu",
		choices: []tchoice{
			{value: "b", label: "B Protocol", shown: "B PROTOCOL"},
			{value: "qb", label: "Quick B protocol", shown: "QUICK B"},
			{value: "xmodem", label: "XMODEM"},
			{value: "kermit", label: "KERMIT"},
			{value: "a", label: "A Protocol", shown: "A PROTOCOL"},
			{value: "menu", label: "SHOW MENU", shown: "SHOW MENU", help: []string{"   (if this choice is selected, you", "   will be presented a menu of", "   protocols when you request a file", "   transfer.)"}},
		}}
	setMailWait = &tsetting{key: "mailwait", label: "Mail Waiting", title: "MAIL WAITING", def: "notify",
		choices: []tchoice{
			{value: "mail", label: "Go to MAIL", shown: "GO TO MAIL", help: []string{"   (Will take you to Mail when you", "   have messages waiting)"}},
			{value: "notify", label: "Notify only", shown: "NOTIFY ONLY", help: []string{"   (Will notify you that you have", "   messages waiting - then take you to", "   your first service)", ""}},
		}}
	setEditor = &tsetting{key: "editor", label: "Online editor", title: "ONLINE EDITOR PREFERENCE", def: "edit",
		text: []string{"This setting determines the editor you", "will normally use.  You may change this", "for a specific forum via the forum", "options or for Mail via the", "Mail options."},
		choices: []tchoice{
			{value: "edit", label: "EDIT", help: []string{"   (does not use line numbers)"}},
			{value: "linedit", label: "LINEDIT", help: []string{"   (uses line numbers)"}},
			{value: "default", label: "DEFAULT", help: []string{"   (no preference)", ""}},
		}}
)

// first and top: where the logon and TOP go
func serviceChoices(what string) []tchoice {
	return []tchoice{
		{value: "main", label: "MAIN menu", help: []string{"   (Normal setting)"}},
		{value: "go", label: "Designated " + what, shown: "DESIGNATED", help: []string{"   (You specify which one.)"}},
		{value: "menu", label: "Personal MENU", shown: "MENU", help: []string{"  (A menu you have constructed)"}},
		{value: "command", label: "COMMAND mode in the personal", help: []string{"   file area", ""}},
	}
}

var termGroups = []*tgroup{
	{label: "Logon/Service options", title: "LOGON/SERVICE OPTIONS", items: []*tsetting{
		{key: "first", label: "First service at logon", title: "FIRST SERVICE", def: "main", choices: serviceChoices("first service")},
		setMailWait,
		{label: "Personal menu established", under: []string{"   (Select this to create or", "   change a personal menu.)"},
			title: "PERSONAL MENU", def: "NO", none: []string{"Personal menus are not available on this service."}},
		{key: "top", label: "TOP goes to", title: "TOP GOES TO", def: "main",
			after:   []string{" As alternative, use 2 and choose INDEX or create a personal menu"},
			choices: serviceChoices("top page")},
		setEditor,
		{key: "forummode", label: "Forum presentation mode", title: "FORUM PRESENTATION MODE", def: "command",
			text: []string{"This setting determines the type of", "presentation you will normally receive", "in each forum you visit.  You may change", "this for a specific forum via the forum", "options."},
			choices: []tchoice{
				{value: "menu", label: "MENU mode - novice/intermediate", help: []string{"   (menu of choices)"}},
				{value: "command", label: "COMMAND mode - expert", help: []string{"   (one word prompts)"}},
				{value: "default", label: "DEFAULT", help: []string{"    (no preference)", ""}},
			}},
	}},
	{label: "Display options", title: "PERMANENT DISPLAY OPTIONS", items: []*tsetting{
		setPaged, setBrief,
		{key: "clear", label: "CLEAR screen between pages", title: "CLEAR SCREEN", def: "yes", choices: yesNoChoices},
		{key: "blank", label: "BLANK lines sent", title: "BLANK LINES", def: "yes", choices: yesNoChoices},
		{key: "lf", label: "Line feeds sent", title: "LINE FEEDS", def: "yes", choices: yesNoChoices},
	}},
	{label: "Terminal type/parameters", title: "TERMINAL TYPE/PARAMETERS", items: []*tsetting{
		setTerm, setWidth, setLines,
		{key: "feeds", label: "Form FEEDS", title: "FORM FEEDS", def: "simulated", choices: []tchoice{{value: "simulated", label: "SIMULATED"}, {value: "real", label: "REAL"}}},
		{key: "tabs", label: "Horizontal TABS", title: "HORIZONTAL TABS", def: "simulated", choices: []tchoice{{value: "simulated", label: "SIMULATED"}, {value: "real", label: "REAL"}}},
		{key: "case", label: "Chars. received (CASE)", title: "CHARACTERS RECEIVED (CAPS)", def: "ul", choices: []tchoice{
			{value: "ul", label: "U/L - normal mode", shown: "U/L", help: []string{"   (Output to you will be in upper or", "   lower case as available on the", "   host computer.)"}},
			{value: "upper", label: "UPPER only", help: []string{"   (You will receive only upper case", "   output.)"}},
			{value: "lower", label: "LOWER only", help: []string{"   (You will receive only lower case", "   output.)", ""}},
		}},
		{key: "caps", label: "Chars. sent in CAPS", title: "CHARACTERS SENT IN CAPS", def: "no", choices: []tchoice{
			{value: "no", label: "NO - normal mode", help: []string{"   (Whatever you input will be in the", "   case you send it.)"}},
			{value: "yes", label: "YES", help: []string{"   (Whatever you input will always be", "   in CAPS regardless of how you send", "   it.)", ""}},
		}},
		{key: "parity", label: "PARITY", title: "PARITY", def: "zero", choices: []tchoice{
			{value: "even", label: "EVEN"}, {value: "odd", label: "ODD"}, {value: "zero", label: "ZERO"}, {value: "one", label: "ONE"}}},
		{key: "delays", label: "Output DELAYS", title: "OUTPUT DELAYS", def: "0", lo: 0, hi: 9,
			prompt: "Enter delay, 0 to 9, or <CR> for no change:"},
		{key: "erase", label: "ERASE when backspacing", title: "BACKSPACE ERASE", def: "yes", choices: yesNoHelp(
			[]string{"   (When a backspace is received,", "   the host sends a space and another", "   backspace which has the effect of", "   erasing the last character typed.)"},
			[]string{"   (no special processing when a", "   backspace is received)", ""})},
		{key: "inquiry", label: "Micro inquiry sequence at logon", title: "SEND MICRO INQUIRY SEQUENCE AT LOGON", def: "no",
			choices: []tchoice{{value: "yes", label: "YES", help: []string{"   (Use this only if your software", "   supports an inquiry sequence.)"}}, {value: "no", label: "NO", help: []string{""}}}},
	}},
	{label: "Transfer protocol/graphic support", title: "TRANSFER PROTOCOLS/GRAPHICS SUPPORT", items: []*tsetting{
		setProtocol,
		{key: "gif", label: "GIF SUPPORT", before: "GRAPHICS SUPPORT", title: "GIF SUPPORT", def: "no", choices: yesNoChoices},
		{key: "naplps", label: "NAPLPS SUPPORT", title: "NAPLPS SUPPORT", def: "no", choices: yesNoChoices},
		{key: "rle", label: "RLE SUPPORT", title: "RLE SUPPORT", def: "no", choices: yesNoChoices},
	}, extra: []string{"(Note: Please consult your software and hardware documentation for graphics", "support before changing these settings.)"}},
}

// the session's own settings, for 4 and 5
var sessionSettings = []*tsetting{setPaged, setBrief, setWidth, setLines}

func init() {
	for _, g := range termGroups {
		for _, t := range g.items {
			if t.key == "" {
				continue
			}
			t := t
			bbs.RegisterPref("cis-"+t.key, func(v string) bool { return t.valid(v) })
		}
	}
}

// valid is whether v is one of t's values.
func (t *tsetting) valid(v string) bool {
	if t.hi > 0 {
		n, err := strconv.Atoi(v)
		return err == nil && n >= t.lo && n <= t.hi && strconv.Itoa(n) == v
	}
	for _, c := range t.choices {
		if c.value == v {
			return true
		}
	}
	return false
}

// show is v as the brackets show it.
func (t *tsetting) show(v string) string {
	for _, c := range t.choices {
		if c.value == v {
			if c.shown != "" {
				return c.shown
			}
			return strings.ToUpper(strings.Fields(c.label)[0])
		}
	}
	return strings.ToUpper(v)
}

// bracketed is text with [v] at its right, 40 wide.
func bracketed(text, v string) string {
	b := "[" + v + "]"
	return text + strings.Repeat(" ", max(40-len(text)-len(b), 1)) + b
}

// profile is GO DEFAULT's state: the settings kept, and the changes to be
// kept when the caller confirms them.
type profile struct {
	kept    bbs.Prefs
	pending map[string]string
}

// get is a permanent setting: as changed, as kept, or its default.
func (p *profile) get(t *tsetting) string {
	if v, ok := p.pending[t.key]; ok {
		return v
	}
	if v, ok := p.kept["cis-"+t.key]; ok && t.valid(v) {
		return v
	}
	return t.def
}

// sessionValue is a session setting as the menus show it.
func (s *session) sessionValue(t *tsetting) string {
	switch t {
	case setPaged:
		return map[bool]string{true: "yes", false: "no"}[s.paged]
	case setBrief:
		return map[bool]string{true: "yes", false: "no"}[s.brief]
	case setWidth:
		return strconv.Itoa(s.width)
	case setLines:
		return strconv.Itoa(s.lines)
	}
	return ""
}

// applySetting makes a setting the session's (the four the session has).
func (s *session) applySetting(t *tsetting, v string) {
	n, _ := strconv.Atoi(v)
	switch t {
	case setPaged:
		s.paged = v == "yes"
		if s.paged && s.lines == 0 {
			s.lines = 24
		}
	case setBrief:
		s.brief = v == "yes"
	case setWidth:
		s.width = n
	case setLines:
		s.lines = n
	}
}

// loadSettings sets the session from the kept settings at the logon,
// unless the caller asked for the defaults (;TTY).
func (s *session) loadSettings(tty bool) {
	if tty {
		return
	}
	p := &profile{kept: s.node.Prefs()}
	for _, t := range []*tsetting{setWidth, setLines, setBrief, setPaged} {
		if _, ok := p.kept["cis-"+t.key]; ok {
			s.applySetting(t, p.get(t))
		}
	}
}

// keptSetting is a permanent setting, outside GO DEFAULT.
func (s *session) keptSetting(t *tsetting) string {
	return (&profile{kept: s.node.Prefs()}).get(t)
}

// leaveFor is a command typed at one of the settings' prompts: GO DEFAULT
// is left, and it is done.
type leaveFor struct{ cmd string }

func (l leaveFor) Error() string { return "cis: leaving the settings for " + l.cmd }

// settingsChoice reads a choice: "" for <CR> (or ^C), a number, or a
// command to leave for.
func (s *session) settingsChoice(p string) (string, error) {
	c, err := s.prompt(s.choicePrompt(p))
	if errors.Is(err, errAbort) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	c = strings.TrimSpace(c)
	if c == "" || isNumber(c) {
		return c, nil
	}
	return "", leaveFor{c}
}

// terminalSettings is GO DEFAULT.
func (s *session) terminalSettings() error {
	s.node.Doing("CompuServe: terminal settings")
	p := &profile{kept: s.node.Prefs(), pending: map[string]string{}}
	err := s.settingsTop(p)
	var l leaveFor
	if errors.As(err, &l) {
		if len(p.pending) > 0 {
			s.out("Your permanent settings have not been changed.\n")
		}
		return s.commands(l.cmd)
	}
	return err
}

func (s *session) settingsTop(p *profile) error {
	for {
		s.out("\nTERMINAL SETTINGS/SERVICE OPTIONS\n" +
			"Use this area to change your terminal type/parameters and/or service options.\n\n" +
			" 1 Instructions\n 2 Change permanent settings\n\n" +
			" 3 Explanation of session vs. permanent\n 4 Show session vs. permanent\n" +
			" 5 Change current session settings\n")
		c, err := s.settingsChoice("Enter choice !")
		if err != nil {
			return err
		}
		switch c {
		case "":
			return s.leaveSettings(p)
		case "1":
			err = s.settingsText("INSTRUCTIONS", []string{
				"Choose 2 to change the settings kept for every logon, or 5 to",
				"change them for this session only.  In each menu, enter a",
				"number to change that setting, or <CR> to go back.  When you",
				"leave, you are asked to confirm the permanent changes.",
			})
		case "2":
			err = s.permanentSettings(p)
		case "3":
			err = s.settingsText("SESSION VS. PERMANENT", []string{
				"Session settings last until you log off; the SET command",
				"changes them too.  Permanent settings are kept with your User",
				"ID and are in effect each time you log on.  Changing a",
				"permanent setting changes it for this session as well.",
				"Append ;TTY to your User ID at logon to override them.",
			})
		case "4":
			err = s.showSettings(p)
		case "5":
			err = s.settingsMenu(p, "CURRENT SESSION SETTINGS", sessionSettings, nil, true)
		default:
			s.out(c + " is not a choice !\n")
		}
		if err != nil {
			return err
		}
	}
}

// settingsText is a screen of text, and <CR> back.
func (s *session) settingsText(title string, lines []string) error {
	s.out("\n" + title + "\n\n")
	for _, l := range lines {
		s.out(l + "\n")
	}
	_, err := s.settingsChoice("Last page !")
	return err
}

func (s *session) showSettings(p *profile) error {
	s.out("\nSESSION VS. PERMANENT SETTINGS\n\n")
	s.outf("%-34s %-10s %s\n", "", "Session", "Permanent")
	for _, g := range termGroups {
		for _, t := range g.items {
			if t.key == "" {
				continue
			}
			sess := s.sessionValue(t)
			if sess == "" {
				sess = p.get(t)
			}
			s.outf(" %-33s %-10s %s\n", t.label, t.show(sess), t.show(p.get(t)))
		}
	}
	_, err := s.settingsChoice("Last page !")
	return err
}

func (s *session) permanentSettings(p *profile) error {
	for {
		s.out("\nPERMANENT SETTINGS\n\n 1 Explanation\n")
		for i, g := range termGroups {
			s.outf(" %d %s\n", i+2, g.label)
		}
		s.outf(" %d Make session settings permanent\n", len(termGroups)+2)
		c, err := s.settingsChoice("Enter choice !")
		if err != nil {
			return err
		}
		n, _ := strconv.Atoi(c)
		switch {
		case c == "":
			return nil
		case n == 1:
			err = s.settingsText("PERMANENT SETTINGS", []string{
				"These settings are kept with your User ID and are in effect",
				"each time you log on.  When you leave TERMINAL SETTINGS you",
				"are asked to confirm them.",
			})
		case n >= 2 && n < len(termGroups)+2:
			g := termGroups[n-2]
			err = s.settingsMenu(p, g.title, g.items, g.extra, false)
		case n == len(termGroups)+2:
			for _, t := range sessionSettings {
				p.pending[t.key] = s.sessionValue(t)
			}
			err = s.settingsText("MAKE SESSION SETTINGS PERMANENT", []string{
				"Your session settings (PAGED, BRIEF, WIDTH and LINES) will be",
				"made permanent when you confirm your changes.",
			})
		default:
			s.out(c + " is not a choice !\n")
		}
		if err != nil {
			return err
		}
	}
}

// settingsMenu is a group's menu: each setting and its value.
func (s *session) settingsMenu(p *profile, title string, items []*tsetting, extra []string, session bool) error {
	for {
		s.out("\n" + title + "\n\n")
		for i, t := range items {
			if t.before != "" {
				s.out(t.before + "\n")
			}
			v := t.def
			switch {
			case t.key == "":
			case session:
				v = s.sessionValue(t)
			default:
				v = p.get(t)
			}
			if t.key != "" {
				v = t.show(v)
			}
			s.out(bracketed(fmt.Sprintf("%2d %s", i+1, t.label), v) + "\n")
			for _, l := range t.under {
				s.out(l + "\n")
			}
		}
		for _, l := range extra {
			s.out(l + "\n")
		}
		c, err := s.settingsChoice("Enter choice !")
		if err != nil {
			return err
		}
		n, _ := strconv.Atoi(c)
		switch {
		case c == "":
			return nil
		case n >= 1 && n <= len(items):
			err = s.changeSetting(p, items[n-1], session)
		default:
			s.out(c + " is not a choice !\n")
		}
		if err != nil {
			return err
		}
	}
}

// changeSetting is a setting's screen: its value, its choices, the new one.
func (s *session) changeSetting(p *profile, t *tsetting, session bool) error {
	s.out("\n" + t.title + "\n\n")
	if t.none != nil {
		for _, l := range t.none {
			s.out(l + "\n")
		}
		_, err := s.settingsChoice("Press <CR> !")
		return err
	}
	for _, l := range t.text {
		s.out(l + "\n")
	}
	if len(t.text) > 0 {
		s.out("\n")
	}
	label, v := "Permanent setting =", p.get(t)
	if session {
		label, v = "Session setting =", s.sessionValue(t)
	}
	s.out(bracketed(label, t.show(v)) + "\n")
	for _, l := range t.after {
		s.out(l + "\n")
	}
	var nv string
	if t.hi > 0 {
		for {
			c, err := s.prompt(t.prompt)
			if errors.Is(err, errAbort) {
				return nil
			}
			if err != nil {
				return err
			}
			c = strings.TrimSpace(c)
			if c == "" {
				return nil
			}
			if n, err := strconv.Atoi(c); err == nil && t.valid(strconv.Itoa(n)) {
				nv = strconv.Itoa(n)
				break
			}
			s.outf("Please enter a number from %d to %d.\n", t.lo, t.hi)
		}
	} else {
		s.out("\n")
		for i, c := range t.choices {
			s.outf("%2d %s\n", i+1, c.label)
			for _, l := range c.help {
				s.out(l + "\n")
			}
		}
		for {
			c, err := s.settingsChoice("Enter choice !")
			if err != nil || c == "" {
				return err
			}
			if n, _ := strconv.Atoi(c); n >= 1 && n <= len(t.choices) {
				nv = t.choices[n-1].value
				break
			}
			s.out(c + " is not a choice !\n")
		}
	}
	if !session {
		p.pending[t.key] = nv
	}
	s.applySetting(t, nv)
	switch {
	case t == setBrief && nv == "yes":
		s.out("\nFULL/BRIEF PROMPTS\n\n" +
			"Please note: Now that you will be receiving Short prompts, some CompuServe\n" +
			"services may not warn you about SURCHARGED usage of the service.\n")
		_, err := s.settingsChoice("Press <CR> !")
		return err
	case t == setTerm && !session:
		// a terminal type brings its parameters
		for k, v := range map[*tsetting]string{setWidth: "80", setLines: "24"} {
			p.pending[k.key] = v
			s.applySetting(k, v)
		}
		p.pending["parity"], p.pending["erase"] = "even", "no"
		s.out("\nTERMINAL TYPE\n\n")
		s.out("Your terminal type is now [" + t.show(nv) + "]\n" +
			"and your terminal parameters have been adjusted accordingly.  If you wish to\n" +
			"override these parameters, you can append ;TTY to your User ID when logging on.\n")
		_, err := s.settingsChoice("Press <CR> !")
		return err
	}
	return nil
}

// leaveSettings is <CR> at the top: changes to keep are confirmed first,
// then PROFILE.
func (s *session) leaveSettings(p *profile) error {
	if len(p.pending) == 0 {
		s.show(s.page)
		return nil
	}
	for {
		s.out("\nCHANGE PERMANENT SETTINGS\n\n" +
			" 1 Confirm\n" +
			"    (Select this choice if you wish to\n" +
			"    update your User Profile with the\n" +
			"    permanent changes you have made.)\n" +
			" 2 Cancel\n" +
			"    (Select this choice if you do not\n" +
			"    wish to save the changes you have\n" +
			"    made to your permanent settings.\n" +
			"    This will not \"undo\" the changes\n" +
			"    for this session, but they will not\n" +
			"    be in effect the next time you log\n" +
			"    onto CompuServe.)\n")
		c, err := s.settingsChoice("Enter choice !")
		if err != nil {
			return err
		}
		if c == "1" {
			for k, v := range p.pending {
				if err := s.node.SetPref("cis-"+k, v); err != nil {
					s.b.Log.Printf("node %d: CompuServe: keeping cis-%s: %v", s.node.Num, k, err)
				}
			}
			break
		}
		if c == "2" {
			break
		}
	}
	s.goTo(s.pages.Get("PROFILE"))
	return nil
}
