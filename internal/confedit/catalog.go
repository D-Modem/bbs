package confedit

import (
	"regexp"
	"strings"

	"github.com/D-Modem/bbs/internal/config"
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
	// help for the keys the sample shows with no comment of their own
	for _, k := range c.Kinds {
		for _, x := range k.Keys {
			if x.Help != "" {
				continue
			}
			if h, ok := keyHelp[k.Name+"."+x.Name]; ok {
				x.Help = h
			} else if h, ok := commonHelp[x.Name]; ok {
				x.Help = h
			}
		}
	}
	return c
}

// keyHelp is the help of keys the sample shows bare, by "kind.key".
var keyHelp = map[string]string{
	"listen.tls_cert":         "The certificate (PEM) for telnet over TLS (port 992); acme for the [acme] certificate.  None: plain telnet.",
	"listen.tls_key":          "The certificate's private key (PEM); not needed with acme.",
	"listen.host_keys":        "SSH listeners: the host key files, by spaces (default: made beside the database the first time).",
	"listen.tn3270":           "yes: 3270 terminals (c3270, x3270, a mainframe's) may come in by this telnet listener and get full screens.",
	"security.deny_file":      "A file of addresses and networks (one a line, CIDR) refused before the login.",
	"security.name_deny_file": "A file of names no new user may take, one a line (* wildcards).",
	"security.hack_log":       "Where failed logins and refused callers are written, apart from the log.",
	"security.ban_minutes":    "Minutes an address locked out for failed logins is refused outright (0: not banned).",
	"event.command":           "The program to run, with its arguments (no shell).",
	"event.every":             "Run it every this many minutes (or at = HH:MM, daily).",
	"event.timeout":           "Seconds it may run before it is stopped.",
	"modemmenu.auth_socket":   "modemmenu's auth socket ([authd] there): the BBS asks it to check passwords of accounts modemmenu knows.",
	"news.hostname":           "This site's name in articles' Path lines (default: the machine's name).",
	"news.domain":             "The domain of the Message-IDs made here (default: the machine's name).",
	"news.organization":       "The Organization header of articles posted here (default: the BBS's name).",
	"peer.address":            "The peer's NNTP server, host:port.",
	"peer.tls":                "no, starttls or implicit (NNTPS, usually port 563).",
	"peer.ca_file":            "A PEM file its certificate is checked against (default: the system's CAs).",
	"peer.username":           "The account it gave us (AUTHINFO), if it asks for one.",
	"peer.password":           "And its password.",
	"peer.groups":             "The newsgroups exchanged with it: a wildmat (bbs.*,!bbs.test).",
	"peer.pull":               "yes: its new articles in our newsgroups are fetched.",
	"peer.push":               "yes: our new articles are offered to it (IHAVE, or POST if it refuses IHAVE).",
	"peer.interval":           "Seconds between exchanges.",
	"peer.max_per_run":        "The most articles fetched from it at one exchange.",
	"mail.domain":             "The users' mail domain: each user is name@domain.",
	"smarthost.address":       "The mail server sent through, host:port (587 for STARTTLS, 465 for implicit TLS).",
	"smarthost.tls":           "starttls, implicit (port 465) or no.",
	"smarthost.username":      "The account to log in to it with.",
	"smarthost.password":      "And its password.",
	"smarthost.insecure":      "yes: its certificate is not checked (a relay on this machine's own network only).",
	"imap.idle_timeout":       "Seconds a connection may be idle before it is closed; IMAP's IDLE wants 1800 or more.",
	"mx.checks":               "yes: SPF, DKIM and DMARC are checked, and mail that fails a DMARC reject policy (or, with no DMARC, fails SPF unsigned) is refused.",
	"finger.online":           "yes: finger @host lists who is on.",
	"finger.users":            "yes: finger name@host tells about a user.",
	"door.description":        "The door's name in the doors list.",
	"door.command":            "The program to run (no shell), {node}, {dir}, {uid}, {gid} filled in: a door on this machine.  None for a door elsewhere (type).",
	"door.type":               "Where the door is: on this machine (none: command), or another system's, by telnet, rlogin, bbslink or doorparty.",
	"door.pty":                "yes: the program gets a pseudo-terminal (a Linux program); no: a pipe (a DOS door in dosemu2).",
	"door.door32_comm":        "DOOR32.SYS's comm type: serial (a FOSSIL, for DOS doors), telnet or local.",
	"door.address":            "The door server's host:port (telnet and rlogin doors).",
	"door.syscode":            "BBSLink: the system code it gave you.",
	"door.authcode":           "BBSLink: the auth code it gave you.",
	"door.schemecode":         "BBSLink: the scheme code it gave you.",
	"door.door":               "BBSLink and DoorParty: the door's code (lord ...); DoorParty with none: its menu.",
	"door.ssh_user":           "DoorParty: the SSH account it gave you.",
	"door.ssh_password":       "DoorParty: and its password.",
	"door.tag":                "DoorParty: your system's tag.",
	"door.host_key":           "DoorParty: its SSH host key's fingerprint (SHA256:...), checked at each call.",
	"feed.enabled":            "no: the feed (yours, or the catalogue's of this name) is left out.",
	"feed.url":                "The feed's address (RSS or Atom).",
	"feed.title":              "Who its messages are from (default: the feed's own title).",
	"menu.title":              "The menu's title, above its items (@-codes and {tags}).",
	"menu.screen":             "A display file (in screens_dir) shown instead of the list of items.",
	"menu.prompt":             "The prompt under the menu (default: [strings] command_prompt).",
	"quotes.file":             "The day's quotes, as bbs quotes fetch writes them (run by an [event] each morning).",
	"nts.tls_cert":            "The NTS-KE server's certificate (PEM): TLS 1.3, so a modern one ([acme]'s by default).",
	"nts.tls_key":             "Its private key (PEM).",
	"strings.name_prompt":     "Asking a caller's name at the login.",
	"strings.new_name_prompt": "Asking the name where new users may sign up (NEW for a new account).",
	"strings.password_prompt": "Asking the password.",
	"strings.command_prompt":  "The prompt under the menus (@USER@, @BBS@ ... and {tags}).",
	"strings.pause":           "Shown at the end of a page, waiting for a key.",
	"strings.logoff_confirm":  "Asking whether to log off.",
	"strings.welcome":         "Said after the login.",
}

// commonHelp is the help of keys many servers have, where the sample gives
// none of their own.
var commonHelp = map[string]string{
	"address":      "Where it listens: host:port (:PORT for every address, 127.0.0.1:PORT for this machine only).",
	"max_clients":  "Connections at once.",
	"max_per_ip":   "Connections at once from one address.",
	"tls":          "no, optional (STARTTLS offered), required (STARTTLS before a login) or implicit (TLS from the first byte).",
	"tls_cert":     "The certificate (PEM); acme for the [acme] certificate.",
	"tls_key":      "The certificate's private key (PEM); not needed with acme.",
	"cert_file":    "The certificate (PEM); acme for the [acme] certificate.",
	"key_file":     "The certificate's private key (PEM); not needed with acme.",
	"idle_timeout": "Seconds a connection may be idle before it is closed.",
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
// IsFree is whether key is one of the sysop's own keys of the kind (a
// menu's item, a QWK network's conference), not one the sample names.
func (k *Kind) IsFree(key string) bool {
	return k != nil && k.byKey[key] == nil && k.free(key)
}

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
		"line_speeds", "line_speed", "line_speed_ask",
		"geoip", "geoip_credit", "geoip_credit_url", "phone_country", "geoip_users", "geoip_prefill", "new_user_level",
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
