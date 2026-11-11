package confui

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/confedit"
	"github.com/D-Modem/bbs/internal/forum"
	"github.com/D-Modem/bbs/internal/vhost"
	"github.com/D-Modem/bbs/internal/view"
)

// sec is a section by kind and name.
func sec(kind string, name ...string) confedit.Section {
	s := confedit.Section{Kind: kind}
	if len(name) > 0 {
		s.Name = name[0]
	}
	return s
}

// choices are the values some keys take (by "kind.key"); "" is the
// default, the key unset.
var choices = map[string][]string{
	"listen.type":        {"telnet", "raw", "rlogin", "ssh"},
	"listen.ansi":        {"auto", "yes", "no"},
	"listen.utf8":        {"auto", "yes", "no"},
	"listen.sixel":       {"auto", "yes", "no"},
	"listen.vt":          {"auto", "yes", "no"},
	"listen.rip":         {"auto", "yes", "no"},
	"listen.petscii":     {"no", "auto", "yes"},
	"ftp.tls":            {"required", "implicit", "optional", "no"},
	"nntp.tls":           {"no", "optional", "required", "implicit"},
	"nntp.read":          {"anyone", "users"},
	"peer.tls":           {"no", "starttls", "implicit"},
	"qwknet.tls":         {"explicit", "implicit", "no"},
	"chan.captcha_level": {"easy", "normal", "hard"},
	"chan.style":         {"", "yotsuba", "yotsuba-b", "futaba", "burichan", "photon", "tomorrow"},
	"fido.charset":       {"cp437", "utf-8"},
	"door.charset":       {"cp437", "utf-8", "raw"},
	"door.type":          {"", "telnet", "rlogin", "bbslink", "doorparty"},
	"link.packer":        {"zip", "arc", "arj", "lha", "none"},
}

// init gives http.forum_theme the forum's themes for its choices.
func init() {
	var themes []string
	for _, t := range forum.Themes {
		themes = append(themes, t.Name)
	}
	choices["http.forum_theme"] = themes
	speeds := []string{"off"}
	for _, bps := range bbs.LineSpeeds {
		speeds = append(speeds, strconv.Itoa(bps))
	}
	choices["general.line_speed"] = speeds
}

// labels are keys' names as a sysop reads them, where the key's own is
// not plain enough
var labels = map[string]string{
	"general.name": "BBS name", "general.sysop": "Sysop's name", "general.location": "Location",
	"general.postal_address": "Postal address", "general.phone": "Dial-in numbers",
	"general.addresses": "Internet addresses", "general.qwk_id": "QWK packet name",
	"general.banner": "Banner (before login)", "general.banner_file": "Banner file",
	"general.motd": "Message of the day", "general.motd_file": "Message of the day file",
	"general.goodbye": "Goodbye", "general.new_users": "New users may sign up",
	"general.logo": "Logo (PNG)", "general.database": "Database", "general.log": "Log",
	"general.files_dir": "File areas' files", "general.max_file_size": "Largest file",
	"general.upload_quota": "Uploads a day (each)", "general.screens_dir": "Display files",
	"general.scripts_dir": "Scripts", "general.max_nodes": "Callers at once",
	"general.attempts": "Login attempts", "general.fail_delay": "Delay after a wrong one (s)",
	"general.login_timeout": "Time to log in (s)", "general.idle_timeout": "Idle time out (s)",
	"general.throttle_failures": "Failures before a lock-out", "general.throttle_time": "Within (s)",
	"general.line_speeds": "Callers may pick a line speed", "general.line_speed": "Line speed until they pick",
	"general.line_speed_ask": "Ask the speed at each login",
	"general.phone_country":  "Caller ID country", "general.geoip": "GeoIP database",
	"general.geoip_credit": "GeoIP credit", "general.geoip_credit_url": "GeoIP credit link",
	"general.geoip_users": "Users may share where they are", "general.geoip_prefill": "Fill in a new user's location",
	"general.new_user_level": "New users' level", "general.validated_level": "Validated users' level",
	"general.tn3270_symbols": "Programmed symbols (pictures, logo)", "general.tn3270_vector": "Vector graphics (pictures, logo)",
	"general.tn3270_logo": "3270 logo (PNG)", "general.page_command": "Page command",
	"security.deny_file": "Addresses refused (file)", "security.name_deny_file": "Names refused (file)",
	"security.hack_log": "Hack log", "security.ban_minutes": "Ban after a lock-out (min)",
	"security.ntlm": "Keep NTLM hashes (MSN 2.x)",
	"chan.dir":      "Image boards' files", "fido.dir": "FidoNet's packets",
	"general.user": "Run as user", "general.group": "and group",
	"modemmenu.auth_socket": "modemmenu's auth socket", "modemmenu.create_users": "Make accounts it knows",
	"modemmenu.timeout": "Its answer within (s)",
	"backup.dir":        "Backup directory", "backup.at": "Daily at (HH:MM)", "backup.keep": "Backups kept",
	"backup.files": "With the files",
}

// labelOf is a key's label: labels' name for it, or the key itself.
func labelOf(kind, key string) string {
	if l, ok := labels[kind+"."+key]; ok {
		return l
	}
	return key
}

// field is an item for a key of a section: its value as it is now
// (marked if not saved), its help, when it takes effect; set or unset as
// it is changed (in the file as it will be written: nothing is saved
// until the sysop says).
func (e *Editor) field(s confedit.Section, key string, label ...string) item {
	x := e.cat.Key(s.Kind, key)
	v, _ := e.doc.Get(s, key)
	ov, _ := e.orig.Get(s, key)
	it := item{Entry: view.Entry{ID: s.String() + " " + key, Kind: view.EntryText, Label: labelOf(s.Kind, key),
		Value: v, Changed: v != ov || e.doc.Has(s) != e.orig.Has(s) && v != "", Secret: confedit.Secret(key)}}
	if len(label) > 0 {
		it.Label = label[0]
	}
	if x != nil {
		it.help = x.Help
		it.Help = firstSentence(x.Help)
		if x.Default != "" && !strings.ContainsAny(x.Default, ".:/") {
			it.Default = x.Default
		}
		if bool := isBool(x); bool {
			it.Kind = view.EntryBool
		}
	}
	if it.Default == "" && v == "" {
		it.Default = "default"
	}
	if cs, ok := choices[s.Kind+"."+key]; ok {
		it.Kind, it.Choices = view.EntryChoice, cs
	} else if o, ok := e.offerFor(s, key); ok && it.Kind != view.EntryBool {
		it.Kind, it.Choices, it.ChoiceNotes, it.Open, it.Multi = view.EntryChoice, o.values, o.notes, o.open, o.multi
	}
	if isKeyKey(key) {
		if c, _ := e.doc.Get(s, certOf(key)); certs.IsACME(c) || certs.IsCA(c) {
			it.Default = "not needed with " + c
		}
	}
	if !confedit.Live(s.Kind, key) {
		it.Note = "restart"
	}
	it.set = func(nv string) error {
		nv = strings.TrimSpace(nv)
		if it.Kind == view.EntryBool && nv != "" && nv != "yes" && nv != "no" {
			return fmt.Errorf("%s: yes or no", it.Label)
		}
		if it.Kind == view.EntryChoice && !it.Open && nv != "" {
			vals := []string{nv}
			if it.Multi {
				vals = strings.Split(nv, ",")
			}
			for _, v := range vals {
				if !contains(it.Choices, strings.TrimSpace(v)) {
					return fmt.Errorf("%s: one of %s", it.Label, strings.Join(nonEmpty(it.Choices), ", "))
				}
			}
		}
		old, had := e.doc.Get(s, key)
		wasMixed := e.refuseMixed() != nil
		if nv == "" {
			e.doc.Unset(s, key)
		} else if err := e.doc.Set(s, key, nv); err != nil {
			return err
		}
		if err := e.refuseMixed(); err != nil && !wasMixed {
			// put it back: the BBS could not listen on both
			if had {
				e.doc.Set(s, key, old)
			} else {
				e.doc.Unset(s, key)
			}
			return err
		}
		e.checkDoc()
		return nil
	}
	return it
}

// refuseMixed is the error, if the file now has one port both on every
// address and on an IP of its own (the chat's HTTPS services: an IP each,
// or all on one), for the edit that made it to be refused.  A file that
// had it already is left to be put right an edit at a time.
func (e *Editor) refuseMixed() error {
	if e.Tools.Check == nil {
		return nil
	}
	if _, err := e.Tools.Check(e.doc.Bytes()); errors.Is(err, vhost.ErrMixedAddresses) {
		return err
	}
	return nil
}

// isBool is whether a key is a yes or no: its default and examples are all
// yes, no or nothing, and one is yes or no.
func isBool(x *confedit.Key) bool {
	vals := append([]string{x.Default}, x.Examples...)
	n := 0
	for _, v := range vals {
		switch v {
		case "yes", "no":
			n++
		case "":
		default:
			return false
		}
	}
	return n > 0
}

// nonEmpty is ss without "".
func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// contains is whether s is one of ss.
func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// heading is an item that is a heading, label, between others.
func heading(label string) item {
	return item{Entry: view.Entry{Kind: view.EntryHeading, Label: label}}
}

// menu is an item that opens another page, value shown beside it.
func menu(label, value string, open func() error) item {
	return item{Entry: view.Entry{ID: "menu " + label, Kind: view.EntryMenu, Label: label, Value: value}, open: open}
}

// action is an item that does do when picked.
func action(label string, do func() error) item {
	return item{Entry: view.Entry{ID: "do " + label, Kind: view.EntryAction, Label: label}, do: do}
}

// fields are items for keys of a section.
func (e *Editor) fields(s confedit.Section, keys ...string) []item {
	var out []item
	for _, k := range keys {
		out = append(out, e.field(s, k))
	}
	return out
}

// --- sections

// enabled is the yes/no of whether a section is there (one whose being
// there turns a server on).
func (e *Editor) enabled(s confedit.Section, label string) item {
	on, was := e.doc.Has(s), e.orig.Has(s)
	it := item{Entry: view.Entry{ID: "on " + s.String(), Kind: view.EntryBool, Label: label,
		Value: yesno(on), Changed: on != was, Note: "restart"}}
	if k := e.cat.Kind(s.Kind); k != nil {
		it.help = k.Help
		it.Help = firstSentence(k.Help)
	}
	it.set = func(v string) error {
		switch {
		case v == "yes" && !e.doc.Has(s):
			if err := e.doc.AddSection(s); err != nil {
				return err
			}
		case v == "no" && e.doc.Has(s):
			e.doc.RemoveSection(s)
		}
		e.checkDoc()
		return nil
	}
	if s.Kind == "level" || s.Kind == "menu" || s.Kind == "strings" || s.Kind == "chan" {
		it.Note = ""
	}
	return it
}

// yesno is b as the file says it: "yes" or "no".
func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// sectionPage is a section's keys: those the sample has for its kind (and
// any others it has), with an Enabled for a section whose being there is
// a server's being on.
func (e *Editor) sectionPage(s confedit.Section, title string, toggle bool) page {
	return page{title: title, items: func() []item {
		var out []item
		if toggle {
			out = append(out, e.enabled(s, "Enabled"))
		}
		out = append(out, e.sectionFields(s)...)
		return out
	}}
}

// sectionFields are a section's keys as items: the sample's for its
// kind, then any others it has.
func (e *Editor) sectionFields(s confedit.Section) []item {
	var out []item
	seen := map[string]bool{}
	if k := e.cat.Kind(s.Kind); k != nil {
		for _, x := range k.Keys {
			seen[x.Name] = true
			out = append(out, e.field(s, x.Name))
		}
	}
	for _, key := range e.doc.Keys(s) {
		if !seen[key] {
			seen[key] = true
			out = append(out, e.field(s, key))
		}
	}
	return out
}

// instances are the named sections of a kind, those keep says.
func (e *Editor) instances(kind string, keep func(confedit.Section) bool) []confedit.Section {
	var out []confedit.Section
	for _, s := range e.doc.Sections() {
		if s.Kind == kind && (keep == nil || keep(s)) {
			out = append(out, s)
		}
	}
	return out
}

// listPage is the named sections of a kind (those keep says): each opens
// its page; A adds one (made with the keys of with), and each page has
// its name to change and a Delete.
func (e *Editor) listPage(title, kind, noun string, keep func(confedit.Section) bool, summary func(confedit.Section) string,
	with map[string]string) page {
	return page{title: title,
		keys: []view.Action{{ID: "add", Label: "Add", Key: 'A', PF: 5}},
		items: func() []item {
			var out []item
			for _, s := range e.instances(kind, keep) {
				s := s
				v := ""
				if summary != nil {
					v = summary(s)
				}
				it := menu(s.Name, v, func() error { return e.show(e.instancePage(s, noun)) })
				it.Changed = !e.orig.Has(s)
				out = append(out, it)
			}
			if len(out) == 0 {
				out = append(out, item{Entry: view.Entry{Kind: view.EntryInfo, Label: "None yet: A adds one."}})
			}
			out = append(out, action("Add a "+noun, func() error { return e.addInstance(kind, noun, with) }))
			return out
		},
		key: func(id string) error {
			if id == "add" {
				return e.addInstance(kind, noun, with)
			}
			return nil
		}}
}

// addInstance asks for a new section's name, adds [kind NAME] with the keys
// of with, and opens its page.  A name there already, or a bad one, is said
// and nothing added; an empty one adds nothing.
func (e *Editor) addInstance(kind, noun string, with map[string]string) error {
	v, ok, err := e.UI.Form(&view.Form{Title: "A new " + noun,
		Intro:  []view.Line{view.L("Its name: one word, [" + kind + " NAME] in the file.")},
		Fields: []view.Field{{Name: "name", Label: "Name", Width: 32}}, Submit: "Add"})
	if err != nil || !ok {
		return err
	}
	name := strings.TrimSpace(v["name"])
	if name == "" {
		return nil
	}
	s := sec(kind, name)
	if e.doc.Has(s) {
		e.say(view.Bad, s.String()+" is there already.")
		return nil
	}
	if err := e.doc.AddSection(s); err != nil {
		e.say(view.Bad, capital(err.Error())+".")
		return nil
	}
	keys := make([]string, 0, len(with))
	for k := range with {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e.doc.Set(s, k, with[k])
	}
	return e.show(e.instancePage(s, noun))
}

// instancePage is a named section's page: its name, its keys, Delete.
func (e *Editor) instancePage(s confedit.Section, noun string) page {
	cur := s
	return page{title: s.String(),
		keys: []view.Action{{ID: "del", Label: "Delete", Key: 'D', PF: 11}},
		items: func() []item {
			name := item{Entry: view.Entry{ID: "name", Kind: view.EntryText, Label: "Name", Value: cur.Name,
				Help: "Its name: [" + cur.Kind + " NAME]"}}
			name.set = func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" || v == cur.Name {
					return nil
				}
				if err := e.doc.Rename(cur, v); err != nil {
					return err
				}
				cur.Name = v
				return nil
			}
			out := []item{name}
			out = append(out, e.sectionFields(cur)...)
			if k := e.cat.Kind(cur.Kind); k != nil && k.Free != "" {
				out = append(out, action(freeNoun[cur.Kind], func() error { return e.addFree(cur) }))
			}
			out = append(out, action("Delete this "+noun, func() error { return e.deleteSection(cur) }))
			return out
		},
		key: func(id string) error {
			if id == "del" {
				return e.deleteSection(cur)
			}
			return nil
		}}
}

// freeNoun is the Add item for the keys of the sysop's own of a kind.
var freeNoun = map[string]string{"menu": "Add a menu item", "qwknet": "Carry a conference"}

// addFree adds a key of the sysop's own to s (a menu's item, a QWK
// network's conference): its key asked, then its value picked from what
// there is (or typed).
func (e *Editor) addFree(s confedit.Section) error {
	intro, label := "The key callers press: one letter or digit.", "Key"
	if s.Kind == "qwknet" {
		intro, label = "The hub's conference number.", "Conference"
	}
	v, ok, err := e.UI.Form(&view.Form{Title: freeNoun[s.Kind], Intro: []view.Line{view.L(intro)},
		Fields: []view.Field{{Name: "key", Label: label, Width: 8}}, Submit: "Next"})
	if err != nil || !ok {
		return err
	}
	key := strings.ToLower(strings.TrimSpace(v["key"]))
	if key == "" {
		return nil
	}
	if !e.cat.Kind(s.Kind).IsFree(key) {
		e.say(view.Bad, key+": not a "+strings.ToLower(label)+".")
		return nil
	}
	if had, _ := e.doc.Get(s, key); had != "" {
		e.say(view.Bad, key+" is there already: "+had+".")
		return nil
	}
	it := e.field(s, key)
	val, ok, err := view.PickChoice(e.UI, it.Entry)
	if err != nil || !ok || strings.TrimSpace(val) == "" {
		return err
	}
	return it.set(val)
}

// deleteSection takes s out of the file if the sysop says yes, and is then
// errClose: its page is gone.
func (e *Editor) deleteSection(s confedit.Section) error {
	yes, err := e.UI.Confirm("Delete "+s.String()+"?", false)
	if err != nil || !yes {
		return err
	}
	e.doc.RemoveSection(s)
	e.checkDoc()
	return errClose
}

// allSections is every section of the file, for what the menus do not
// reach.
func (e *Editor) allSections() page {
	return page{title: "All settings, by section",
		note: []view.Line{view.T(view.Dim, "Every section of the file as it is.")},
		items: func() []item {
			var out []item
			for _, s := range e.doc.Sections() {
				s := s
				help := ""
				if k := e.cat.Kind(s.Kind); k != nil {
					help = firstSentence(k.Help)
				}
				it := menu(s.String(), fmt.Sprintf("%d keys", len(e.doc.Keys(s))), func() error {
					if s.Name != "" {
						return e.show(e.instancePage(s, s.Kind))
					}
					return e.show(e.sectionPage(s, s.String(), true))
				})
				it.Help = help
				out = append(out, it)
			}
			return out
		}}
}
