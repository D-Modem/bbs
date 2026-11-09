package aol

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/aol/fdo"
)

// Keywords: the client's Keyword window sends Kk with the word typed as
// de_data, and a button of the host's sends it with the word as it is
// (sm_send_token_raw <"Kk", "...">).  Each takes the user to a part of
// the BBS.

// keywords are the ones HELP lists, with what each does.
var keywords = []struct{ word, what string }{
	{"Chat", "People Connection, the BBS's main chat room"},
	{"Mail", "your mailbox"},
	{"Who", "who is on the BBS now"},
	{"Help", "this list"},
}

// The host's other windows, by global id.
const (
	whoWindow  = "50-2-1"
	helpWindow = "50-2-2"
)

// keyword takes the user where a keyword goes.
func (ss *session) keyword(kw string) {
	kw = strings.Join(strings.Fields(kw), " ")
	k := strings.ToLower(kw)
	if verb, arg, ok := strings.Cut(k, " mail "); ok {
		id, err := strconv.ParseInt(arg, 10, 64)
		switch {
		case err != nil:
		case verb == "read":
			ss.readMail(id)
			return
		case verb == "delete":
			ss.deleteMail(id)
			return
		}
	}
	switch k {
	case "":
		ss.send(waitOff)
	case "chat", "people connection", "pc", "lobby", "people":
		ss.enterChat()
	case "mail", "read mail", "new mail", "mailbox", "check mail", "email", "e-mail":
		ss.mailbox()
	case "who", "online", "who's online", "whos online", "members":
		ss.who()
	case "help", "keyword", "keywords", "keyword list":
		ss.help()
	default:
		ss.notice(fmt.Sprintf("There is no keyword \"%s\" here.  The keyword Help lists those there are.", kw))
	}
}

// keywordOf is the word in a Kk: the first field a form sent, or the
// text a button sent as it is.
func keywordOf(body []byte) string {
	if isText(body) {
		return fdo.FromLatin1(body)
	}
	_, atoms := splitStream("Kk", body)
	if f := texts(atoms); len(f) > 0 {
		return f[0]
	}
	return ""
}

// isText is whether b is plain text, not a stream id and atoms.
func isText(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 0x20 && c != '\t' {
			return false
		}
	}
	return true
}

// who lists the callers on now.
func (ss *session) who() {
	var lines []string
	for _, n := range ss.s.B.Nodes() {
		name := n.User
		if name == "" {
			name = "(signing on)"
		}
		line := fmt.Sprintf("%s, by %s, %dm", name, n.Via, int(time.Since(n.Since).Minutes()))
		if n.Activity != "" {
			line += ": " + n.Activity
		}
		lines = append(lines, line)
	}
	text := fmt.Sprintf("%s on now:\r\n\r\n%s", count(len(lines), "caller", "callers"), strings.Join(lines, "\r\n"))
	ss.send(closeForm(whoWindow))
	ss.send(windowForm(whoWindow, "Who's Online", text, 12, []button{{"Chat", "chat"}}))
}

// help lists the keywords.
func (ss *session) help() {
	var lines []string
	var buttons []button
	for _, k := range keywords {
		lines = append(lines, k.word+": "+k.what)
		if k.word != "Help" {
			buttons = append(buttons, button{k.word, strings.ToLower(k.word)})
		}
	}
	text := "Type a keyword in the Keyword window (Ctrl+K) to go there:\r\n\r\n" + strings.Join(lines, "\r\n")
	ss.send(closeForm(helpWindow))
	ss.send(windowForm(helpWindow, "Keywords", text, 8, buttons))
}

// button is a button in a host window that sends a keyword.
type button struct{ label, keyword string }

// windowForm is a host window: its title, a view of text, and its
// buttons.
func windowForm(gid, title, text string, rows int, buttons []button) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "uni_start_stream\n  man_start_object <ind_group, %s>\n    mat_object_id <%s>\n"+
		"    mat_orientation <vcf>\n    mat_position <center_center>\n", fdo.Quote(title), gid)
	fmt.Fprintf(&sb, "    man_start_object <view, \"\">\n      mat_size <50, %d>\n      man_append_data <%s>\n"+
		"      man_end_data\n    man_end_object\n", rows, fdo.Quote(text))
	for _, b := range buttons {
		fmt.Fprintf(&sb, "    man_start_object <trigger, %s>\n      act_replace_select_action\n        <\n"+
			"        uni_start_stream\n          sm_send_token_raw <\"Kk\", %s>\n        uni_end_stream\n        >\n"+
			"    man_end_object\n", fdo.Quote(b.label), fdo.Quote(b.keyword))
	}
	sb.WriteString("  man_end_object\n  man_update_display\n  uni_wait_off\nuni_end_stream\n")
	return sb.String()
}

// notice is a message box that also ends the client's wait.
func (ss *session) notice(text string) {
	ss.send(fmt.Sprintf("uni_start_stream\n  async_alert <info, %s>\n  uni_wait_off\nuni_end_stream\n", fdo.Quote(text)))
}
