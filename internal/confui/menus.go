package confui

import (
	"fmt"
	"strings"

	"github.com/D-Modem/bbs/internal/confedit"
	"github.com/D-Modem/bbs/internal/view"
)

// The menus, by what a sysop looks for.

// The first page's menus, by their labels (the pull-down menus' paths
// name them).
const (
	mBoard    = "Board configuration"
	mSystem   = "System configuration"
	mLines    = "Connections and modems"
	mInternet = "Internet services"
	mOnline   = "Online services"
	mChat     = "Messengers and chat"
	mMail     = "E-mail"
	mNetworks = "Networks and doors"
	mTime     = "Time"
	mUsers    = "Users and groups"
	mMaint    = "Maintenance"
	mReview   = "Review changes"
	mCheck    = "Check the configuration"
)

// mainMenu is the first page: the menus, then review, check, save and quit.
func (e *Editor) mainMenu() page {
	return page{title: "Configuration", items: func() []item {
		g := sec("general")
		name, _ := e.doc.Get(g, "name")
		n := e.changes()
		review := action(mReview, e.review)
		review.Value = fmt.Sprintf("%d not saved", n)
		return []item{
			withHelp(e.submenu(mBoard, name, e.boardMenu), "The BBS's name, sysop and where it is; what callers see; the menus and prompts."),
			withHelp(e.submenu(mSystem, "", e.systemMenu), "The database and log, where files are kept, security, the user it runs as, scheduled events, and every section of the file."),
			withHelp(e.submenu(mLines, e.count("listen", nil, "listener"), e.connectionsMenu), "Modem lines and listeners (the ports callers come in by), callers' limits, modemmenu's passwords, caller ID."),
			heading("What callers reach"),
			withHelp(e.submenu(mInternet, "", e.servicesMenu), "Telnet, TN3270, SSH and RLogin for terminals; finger, gopher, Gemini, FTP, news and the web."),
			withHelp(e.submenu(mOnline, e.onCount("aol", "marvel"), e.onlineMenu), "The BBS as America Online, CompuServe and The Microsoft Network, for their own 1990s client programs."),
			withHelp(e.submenu(mChat, e.onCount("oscar", "msn", "ymsg", "irc", "irclink", "mrc"), e.chatMenu), "AIM and ICQ, MSN Messenger, Yahoo! Messenger, the IRC server and its network links, Multi Relay Chat."),
			withHelp(e.submenu(mMail, e.onOff(sec("mail")), e.mailMenu), "Each user's e-mail: the domain, sending (direct or by a smarthost), POP3, IMAP, SMTP, MX and LMTP."),
			withHelp(e.submenu(mNetworks, "", e.networksMenu), "FidoNet and binkp, QWK networks, and the doors."),
			withHelp(e.submenu(mTime, e.onCount("time", "ntp", "nts", "ptp"), e.timeMenu), "The clock the BBS follows (NTP, NTS, PTP servers) and the time servers it can be."),
			heading("Accounts and upkeep"),
			withHelp(e.submenu(mUsers, "", e.usersMenu), "User accounts, access levels and flags."),
			withHelp(e.submenu(mMaint, "", e.maintenanceMenu), "Disk space, what the message areas keep, backups."),
			heading(""),
			review,
			action(mCheck, e.check),
			action("Save and exit (F10)", func() error { return errSave }),
			action("Quit without saving (F9)", func() error { return errQuit }),
		}
	}}
}

// withHelp is it with its help.
func withHelp(it item, help string) item {
	it.Help = help
	return it
}

// submenu is an item that opens the page p makes.
func (e *Editor) submenu(label, value string, p func() page) item {
	it := menu(label, value, func() error { return e.show(p()) })
	it.sub = p
	return it
}

// onCount is how many of the sections of kinds the file has, in words.
func (e *Editor) onCount(kinds ...string) string {
	n := 0
	for _, k := range kinds {
		if e.doc.Has(sec(k)) {
			n++
		}
	}
	if n == 0 {
		return "off"
	}
	return fmt.Sprintf("%d on", n)
}

// pullDowns are the menu bar's menus: File, then the first page's menus
// gathered under short names, each item a jump to its page.
func (e *Editor) pullDowns() []view.PullDown {
	tops := map[string]item{}
	for _, it := range e.mainMenu().items() {
		tops[it.Label] = it
	}
	// expand is a first-page menu's own menus, under a heading of it
	expand := func(label string, head bool) []view.PullItem {
		var out []view.PullItem
		top, ok := tops[label]
		if !ok || top.sub == nil {
			return nil
		}
		if head {
			out = append(out, view.PullItem{Label: label, Heading: true})
		}
		for _, it := range top.sub().items() {
			switch {
			case it.Kind == view.EntryHeading:
				if len(out) > 0 && !out[len(out)-1].Heading {
					out = append(out, view.PullItem{Label: it.Label, Heading: true})
				}
			case it.Kind == view.EntryMenu && !it.Disabled:
				out = append(out, view.PullItem{Label: it.Label, Path: []string{label, it.Label}})
			}
		}
		for len(out) > 0 && out[len(out)-1].Heading {
			out = out[:len(out)-1]
		}
		return out
	}
	page := func(label string) view.PullItem { return view.PullItem{Label: label, Path: []string{label}} }
	pd := func(label string, items ...[]view.PullItem) view.PullDown {
		m := view.PullDown{Label: label}
		for _, its := range items {
			m.Items = append(m.Items, its...)
		}
		return m
	}
	one := func(its ...view.PullItem) []view.PullItem { return its }
	rule := view.PullItem{Heading: true}
	return []view.PullDown{
		pd("File", one(page(mReview), page(mCheck), rule,
			view.PullItem{Label: "Save and exit  F10", Action: view.SheetSave},
			view.PullItem{Label: "Quit without saving  F9", Action: view.SheetQuit})),
		pd("Setup", one(page(mBoard), page(mSystem), page(mUsers), page(mMaint), rule,
			view.PullItem{Label: "All settings, by section", Path: []string{mSystem, "All settings, by section"}})),
		pd("Lines", one(page(mLines), rule), expand(mLines, false)),
		pd("Internet", one(page(mInternet), rule), expand(mInternet, false)),
		pd("Online", expand(mOnline, true), expand(mChat, true)),
		pd("Mail", one(page(mMail), rule), expand(mMail, false)),
		pd("Networks", one(page(mNetworks), rule), expand(mNetworks, false)),
		pd("Time", one(page(mTime), rule), expand(mTime, false)),
	}
}

// count is how many sections of a kind there are, in words.
func (e *Editor) count(kind string, keep func(confedit.Section) bool, noun string) string {
	n := len(e.instances(kind, keep))
	switch n {
	case 0:
		return "none"
	case 1:
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// boardMenu is Board configuration: the BBS's name and where it is, what
// callers see, menus and prompts.
func (e *Editor) boardMenu() page {
	g := sec("general")
	return page{title: "Board configuration", items: func() []item {
		out := e.fields(g, "name", "sysop", "location", "postal_address", "phone", "addresses", "qwk_id")
		out = append(out, heading("What callers see"))
		out = append(out, e.fields(g, "banner", "banner_file", "motd", "motd_file", "goodbye", "new_users", "logo")...)
		out = append(out, heading("Menus and prompts"),
			menu("Menus", e.count("menu", nil, "menu"), func() error {
				return e.show(e.listPage("Menus", "menu", "menu", nil, func(s confedit.Section) string {
					t, _ := e.doc.Get(s, "title")
					return t
				}, nil))
			}),
			menu("Prompts and messages", "", func() error { return e.show(e.stringsPage()) }))
		return out
	}}
}

// stringsPage is [strings]: every prompt the BBS has, its default shown.
func (e *Editor) stringsPage() page {
	s := sec("strings")
	return page{title: "Prompts and messages", items: func() []item {
		var out []item
		for _, k := range []string{"name_prompt", "new_name_prompt", "password_prompt", "command_prompt", "pause",
			"logoff_confirm", "welcome"} {
			out = append(out, e.field(s, k))
		}
		return out
	}}
}

// systemMenu is System configuration: the database and log, where files
// are, security, the user and group it runs as, scheduled events, and every
// section.
func (e *Editor) systemMenu() page {
	g := sec("general")
	return page{title: "System configuration", items: func() []item {
		out := e.fields(g, "database", "log")
		out = append(out, heading("File locations"))
		out = append(out, e.fields(g, "files_dir", "max_file_size", "upload_quota", "screens_dir", "scripts_dir")...)
		out = append(out, e.field(sec("chan"), "dir"), e.field(sec("fido"), "dir"))
		out = append(out, heading("Security"))
		out = append(out, e.fields(sec("security"), "deny_file", "name_deny_file", "hack_log", "ban_minutes")...)
		out = append(out, heading("The process"))
		out = append(out, e.fields(g, "user", "group")...)
		out = append(out,
			heading("More"),
			menu("Scheduled events", e.count("event", nil, "event"), func() error {
				return e.show(e.listPage("Scheduled events", "event", "event", nil, func(s confedit.Section) string {
					c, _ := e.doc.Get(s, "command")
					return c
				}, nil))
			}),
			menu("All settings, by section", "", func() error { return e.show(e.allSections()) }))
		return out
	}}
}

// listener kinds
func listenType(e *Editor, s confedit.Section) string {
	t, _ := e.doc.Get(s, "type")
	if t == "" {
		t = "telnet"
	}
	return t
}

// listeners is a list of [listen] sections (those keep says), each with its
// type and address; added ones get the keys of with.
func (e *Editor) listeners(keep func(confedit.Section) bool, title string, with map[string]string) page {
	return e.listPage(title, "listen", "listener", keep, func(s confedit.Section) string {
		a, _ := e.doc.Get(s, "address")
		return listenType(e, s) + " " + a
	}, with)
}

// isModem is whether a listener is a modem line: raw, or with header = yes.
func (e *Editor) isModem(s confedit.Section) bool {
	h, _ := e.doc.Get(s, "header")
	return listenType(e, s) == "raw" || h == "yes"
}

// connectionsMenu is Connections and modems: modem lines and all listeners,
// callers' limits, modemmenu's auth socket, caller ID and GeoIP.
func (e *Editor) connectionsMenu() page {
	g := sec("general")
	return page{title: "Connections and modems", items: func() []item {
		out := []item{
			menu("Modem lines", e.count("listen", e.isModem, "line"), func() error {
				return e.show(e.listeners(e.isModem, "Modem lines", map[string]string{"type": "raw", "address": "127.0.0.1:2323"}))
			}),
			menu("All listeners", e.count("listen", nil, "listener"), func() error {
				return e.show(e.listeners(nil, "All listeners", map[string]string{"type": "telnet", "address": ":23"}))
			}),
			heading("Callers"),
		}
		out = append(out, e.fields(g, "max_nodes", "attempts", "fail_delay", "login_timeout", "idle_timeout",
			"throttle_failures", "throttle_time", "page_command")...)
		out = append(out, heading("Line speeds (telnet, SSH and web callers)"))
		out = append(out, e.fields(g, "line_speeds", "line_speed", "line_speed_ask")...)
		out = append(out, heading("Passwords by modemmenu (its auth socket)"))
		out = append(out, e.fields(sec("modemmenu"), "auth_socket", "create_users", "timeout")...)
		out = append(out, heading("Caller ID and where callers are"))
		out = append(out, e.fields(g, "phone_country", "geoip", "geoip_credit", "geoip_credit_url",
			"geoip_users", "geoip_prefill")...)
		return out
	}}
}

// onOff is "on" if the file has section s, "off" if not.
func (e *Editor) onOff(s confedit.Section) string {
	if e.doc.Has(s) {
		return "on"
	}
	return "off"
}

// ofType keeps the listeners of a type.
func (e *Editor) ofType(t string) func(confedit.Section) bool {
	return func(s confedit.Section) bool { return listenType(e, s) == t }
}

// ofFrontEnd keeps the listeners that answer as a front end (compuserve,
// marvel).
func (e *Editor) ofFrontEnd(f string) func(confedit.Section) bool {
	return func(s confedit.Section) bool {
		v, _ := e.doc.Get(s, "frontend")
		return v == f
	}
}

// single is an item for a section whose being there turns a server on:
// its page has Enabled and its keys.
func (e *Editor) single(label, kind string) item {
	it := menu(label, e.onOff(sec(kind)), func() error { return e.show(e.sectionPage(sec(kind), label, true)) })
	if k := e.cat.Kind(kind); k != nil {
		it.Help = firstSentence(k.Help)
		it.help = k.Help
	}
	return it
}

// servicesMenu is Internet services: the terminals' listeners by type, and
// the servers whose section turns them on.
func (e *Editor) servicesMenu() page {
	listeners := func(label, t, addr string) item {
		return menu(label, e.count("listen", e.ofType(t), "listener"), func() error {
			return e.show(e.listeners(e.ofType(t), label, map[string]string{"type": t, "address": addr}))
		})
	}
	return page{title: mInternet, items: func() []item {
		return []item{
			heading("Callers' terminals"),
			listeners("Telnet", "telnet", ":23"),
			menu("TN3270", "", func() error { return e.show(e.tn3270Page()) }),
			listeners("SSH", "ssh", ":22"),
			listeners("RLogin", "rlogin", "127.0.0.1:513"),
			heading("Servers"),
			e.single("Finger", "finger"),
			e.single("Gopher", "gopher"),
			e.single("Gemini", "gemini"),
			e.single("FTP", "ftp"),
			menu("News (NNTP)", e.onOff(sec("nntp")), func() error { return e.show(e.newsPage()) }),
			menu("Web: API, forum, image boards", e.onOff(sec("http")), func() error { return e.show(e.webPage()) }),
			menu("Certificates (ACME)", e.onOff(sec("acme")), func() error { return e.show(e.acmePage()) }),
			heading("News, weather and quotes"),
			e.single("News feeds", "feeds"),
			menu("Feeds of your own", e.count("feed", nil, "feed"), func() error {
				return e.show(e.listPage("Feeds of your own", "feed", "feed", nil, func(s confedit.Section) string {
					u, _ := e.doc.Get(s, "url")
					return u
				}, nil))
			}),
			e.single("Weather", "weather"),
			e.single("Stock quotes", "quotes"),
		}
	}}
}

// onlineMenu is Online services: the dial-up services of the 1990s the BBS
// answers as, for their own client programs.
func (e *Editor) onlineMenu() page {
	return page{title: mOnline,
		note: []view.Line{view.T(view.Dim, "The BBS as the online services, for their own client programs.")},
		items: func() []item {
			return []item{
				e.single("America Online (AOL)", "aol"),
				menu("CompuServe (CIS)", e.count("listen", e.ofFrontEnd("compuserve"), "listener"), func() error {
					return e.show(e.frontEndListeners("CompuServe (CIS)", "compuserve",
						"Listeners with frontend = compuserve answer as CompuServe.",
						map[string]string{"type": "telnet", "address": ":2324", "frontend": "compuserve"}))
				}),
				menu("The Microsoft Network (MSN 1995)", e.onOff(sec("marvel")), func() error { return e.show(e.marvelPage()) }),
			}
		}}
}

// frontEndListeners is the listeners that answer as a front end, with a
// word on it; added ones get the keys of with.
func (e *Editor) frontEndListeners(title, frontend, note string, with map[string]string) page {
	p := e.listeners(e.ofFrontEnd(frontend), title, with)
	p.note = []view.Line{view.T(view.Dim, note)}
	return p
}

// marvelPage is MSN Classic: its TCP host, and its dial-in listeners.
func (e *Editor) marvelPage() page {
	return page{title: "The Microsoft Network (MSN 1995)", items: func() []item {
		out := []item{e.enabled(sec("marvel"), "Over TCP (port 569)")}
		out = append(out, e.sectionFields(sec("marvel"))...)
		out = append(out, heading("Modem callers"),
			menu("Listeners with frontend = marvel", e.count("listen", e.ofFrontEnd("marvel"), "listener"), func() error {
				return e.show(e.frontEndListeners("MSN dial-in listeners", "marvel",
					"Windows 95's MSN client dialing in comes through these.",
					map[string]string{"type": "raw", "address": "127.0.0.1:2325", "frontend": "marvel"}))
			}))
		return out
	}}
}

// chatMenu is Messengers and chat: the instant messengers, IRC and its
// links, Multi Relay Chat.
func (e *Editor) chatMenu() page {
	return page{title: mChat, items: func() []item {
		return []item{
			heading("Instant messengers"),
			e.single("AIM and ICQ (OSCAR)", "oscar"),
			e.single("MSN Messenger", "msn"),
			e.single("Yahoo! Messenger", "ymsg"),
			heading("IRC"),
			e.single("IRC server", "irc"),
			menu("IRC network links", e.onOff(sec("irclink")), func() error { return e.show(e.ircLinkPage()) }),
			heading("Between BBSes"),
			e.single("Multi Relay Chat", "mrc"),
		}
	}}
}

// ircLinkPage is IRC server linking: [irclink] and the [ircpeer]s.
func (e *Editor) ircLinkPage() page {
	return page{title: "IRC network links", items: func() []item {
		out := []item{e.enabled(sec("irclink"), "Linked to a network")}
		out = append(out, e.sectionFields(sec("irclink"))...)
		out = append(out, heading("Servers linked with"), menu("Peer servers", e.count("ircpeer", nil, "server"), func() error {
			return e.show(e.listPage("Peer servers", "ircpeer", "server", nil, func(s confedit.Section) string {
				a, _ := e.doc.Get(s, "address")
				return a
			}, nil))
		}))
		return out
	}}
}

// mailMenu is E-mail: [mail], the smarthost, and each mail server.
func (e *Editor) mailMenu() page {
	return page{title: mMail, items: func() []item {
		out := []item{e.enabled(sec("mail"), "E-mail")}
		out = append(out, e.sectionFields(sec("mail"))...)
		out = append(out,
			heading("Sending"),
			e.single("Smarthost (send through)", "smarthost"),
			heading("Servers"),
			e.single("POP3", "pop3"),
			e.single("IMAP", "imap"),
			e.single("SMTP submission", "smtp"),
			e.single("MX (mail from the internet)", "mx"),
			e.single("LMTP (from a mail server)", "lmtp"))
		return out
	}}
}

// networksMenu is Networks and doors: FidoNet, QWK, the doors.
func (e *Editor) networksMenu() page {
	return page{title: mNetworks, items: func() []item {
		return []item{
			menu("FidoNet", e.onOff(sec("fido")), func() error { return e.show(e.fidoPage()) }),
			menu("QWK networks", e.count("qwknet", nil, "network"), func() error {
				return e.show(e.listPage("QWK networks", "qwknet", "network", nil, func(s confedit.Section) string {
					a, _ := e.doc.Get(s, "address")
					return a
				}, nil))
			}),
			menu("Doors", e.count("door", nil, "door"), func() error {
				return e.show(e.listPage("Doors", "door", "door", nil, func(s confedit.Section) string {
					d, _ := e.doc.Get(s, "description")
					return d
				}, nil))
			}),
		}
	}}
}

// timeMenu is Time: the clock the BBS follows, and its time servers.
func (e *Editor) timeMenu() page {
	return page{title: mTime, items: func() []item {
		return []item{
			e.single("Clock and time sources", "time"),
			heading("Time servers"),
			e.single("NTP server", "ntp"),
			e.single("NTS-KE server (secure NTP)", "nts"),
			e.single("PTP grandmaster", "ptp"),
		}
	}}
}

// acmePage is Certificates (ACME): the ACME client, and its [acme NAME]
// certificates.
func (e *Editor) acmePage() page {
	named := func(s confedit.Section) bool { return s.Name != "" }
	return page{title: "Certificates (ACME)", items: func() []item {
		out := []item{e.enabled(sec("acme"), "ACME client")}
		out = append(out, e.sectionFields(sec("acme"))...)
		out = append(out, heading("More certificates"), menu("Certificates", e.count("acme", named, "certificate"), func() error {
			return e.show(e.listPage("Certificates", "acme", "certificate", named, func(s confedit.Section) string {
				d, _ := e.doc.Get(s, "domains")
				return d
			}, map[string]string{"domains": "mail.example.org"}))
		}))
		return out
	}}
}

// tn3270Page is TN3270: the 3270 pictures and logo, and the tn3270 key of
// each telnet listener.
func (e *Editor) tn3270Page() page {
	g := sec("general")
	return page{title: "TN3270", note: []view.Line{view.T(view.Dim, "3270s come in by telnet listeners with TN3270 on.")},
		items: func() []item {
			out := e.fields(g, "tn3270_symbols", "tn3270_vector", "tn3270_logo")
			out = append(out, heading("Telnet listeners"))
			for _, s := range e.instances("listen", func(s confedit.Section) bool { return listenType(e, s) == "telnet" }) {
				out = append(out, e.field(s, "tn3270", s.String()+" takes 3270s"))
			}
			return out
		}}
}

// newsPage is News (NNTP): the server, the [news] keys, and the peers.
func (e *Editor) newsPage() page {
	return page{title: "News (NNTP)", items: func() []item {
		out := []item{e.enabled(sec("nntp"), "NNTP server")}
		out = append(out, e.sectionFields(sec("nntp"))...)
		out = append(out, heading("This site's name in the news"))
		out = append(out, e.sectionFields(sec("news"))...)
		out = append(out, heading("Peers"), menu("Peer news servers", e.count("peer", nil, "peer"), func() error {
			return e.show(e.listPage("Peer news servers", "peer", "peer", nil, func(s confedit.Section) string {
				a, _ := e.doc.Get(s, "address")
				return a
			}, nil))
		}))
		return out
	}}
}

// webPage is the HTTP server and the image boards (their dir is under
// System configuration).
func (e *Editor) webPage() page {
	return page{title: "Web: API, forum, image boards", items: func() []item {
		out := []item{e.enabled(sec("http"), "HTTP server")}
		out = append(out, e.sectionFields(sec("http"))...)
		out = append(out, heading("Image boards"))
		for _, it := range e.sectionFields(sec("chan")) {
			if !strings.HasSuffix(it.ID, " dir") {
				out = append(out, it)
			}
		}
		return out
	}}
}

// fidoPage is FidoNet: [fido] but its dir (under System configuration),
// binkp's answering, and the links.
func (e *Editor) fidoPage() page {
	return page{title: "FidoNet", items: func() []item {
		out := []item{e.enabled(sec("fido"), "On FidoNet")}
		for _, it := range e.sectionFields(sec("fido")) {
			if !strings.HasSuffix(it.ID, " dir") {
				out = append(out, it)
			}
		}
		out = append(out, heading("binkp: answering the links' calls"), e.enabled(sec("binkp"), "Answer"))
		out = append(out, e.sectionFields(sec("binkp"))...)
		out = append(out, heading("Links"), menu("Links", e.count("link", nil, "link"), func() error {
			return e.show(e.listPage("Links", "link", "link", nil, func(s confedit.Section) string {
				a, _ := e.doc.Get(s, "address")
				return a
			}, nil))
		}))
		return out
	}}
}
