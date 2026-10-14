package confui

import (
	"fmt"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/confedit"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// The menus, by what a sysop looks for.

func (e *Editor) mainMenu() page {
	return page{title: "Configuration", items: func() []item {
		g := sec("general")
		name, _ := e.doc.Get(g, "name")
		n := e.changes()
		review := menu("Review changes", fmt.Sprintf("%d not saved", n), e.review)
		review.Kind = view.EntryAction
		review.do, review.open = e.review, nil
		return []item{
			menu("Board configuration", name, func() error { return e.show(e.boardMenu()) }),
			menu("System configuration", "", func() error { return e.show(e.systemMenu()) }),
			menu("Connections and modems", e.count("listen", nil, "listener"), func() error { return e.show(e.connectionsMenu()) }),
			menu("Services", "", func() error { return e.show(e.servicesMenu()) }),
			menu("Users and groups", "", func() error { return e.show(e.usersMenu()) }),
			menu("Maintenance", "", func() error { return e.show(e.maintenanceMenu()) }),
			heading(""),
			review,
			action("Check the configuration", e.check),
			action("Save and exit (F10)", func() error { return errSave }),
			action("Quit without saving (F9)", func() error { return errQuit }),
		}
	}}
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

func (e *Editor) listeners(keep func(confedit.Section) bool, title string, with map[string]string) page {
	return e.listPage(title, "listen", "listener", keep, func(s confedit.Section) string {
		a, _ := e.doc.Get(s, "address")
		return listenType(e, s) + " " + a
	}, with)
}

func (e *Editor) isModem(s confedit.Section) bool {
	h, _ := e.doc.Get(s, "header")
	return listenType(e, s) == "raw" || h == "yes"
}

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
		out = append(out, heading("Passwords by modemmenu (its auth socket)"))
		out = append(out, e.fields(sec("modemmenu"), "auth_socket", "create_users", "timeout")...)
		out = append(out, heading("Caller ID and where callers are"))
		out = append(out, e.fields(g, "phone_country", "geoip", "geoip_credit", "geoip_credit_url")...)
		return out
	}}
}

func (e *Editor) onOff(s confedit.Section) string {
	if e.doc.Has(s) {
		return "on"
	}
	return "off"
}

func (e *Editor) servicesMenu() page {
	ofType := func(t string) func(confedit.Section) bool {
		return func(s confedit.Section) bool { return listenType(e, s) == t }
	}
	listeners := func(label, t, addr string) item {
		return menu(label, e.count("listen", ofType(t), "listener"), func() error {
			return e.show(e.listeners(ofType(t), label, map[string]string{"type": t, "address": addr}))
		})
	}
	single := func(label, kind string) item {
		return menu(label, e.onOff(sec(kind)), func() error { return e.show(e.sectionPage(sec(kind), label, true)) })
	}
	return page{title: "Services", items: func() []item {
		return []item{
			heading("Callers' terminals"),
			listeners("Telnet", "telnet", ":23"),
			menu("TN3270", "", func() error { return e.show(e.tn3270Page()) }),
			listeners("SSH", "ssh", ":22"),
			listeners("RLogin", "rlogin", "127.0.0.1:513"),
			heading("Servers"),
			single("Finger", "finger"),
			single("Gopher", "gopher"),
			single("FTP", "ftp"),
			single("IRC (ircd)", "irc"),
			menu("News (NNTP)", e.onOff(sec("nntp")), func() error { return e.show(e.newsPage()) }),
			menu("Web: API, forum, image boards", e.onOff(sec("http")), func() error { return e.show(e.webPage()) }),
			heading("Networks and doors"),
			single("Multi Relay Chat", "mrc"),
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
