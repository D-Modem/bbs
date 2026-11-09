package cis

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/xfer"
)

// CompuServe Mail on the BBS's private mail.  The wording is TAPCIS's: its
// PARAMS.SCR (5.4, 1991) and the strings it waits for ("Mail!", "ID):",
// "ect:", "Action!"), and 1993 captures of menu mode.  A message is
// "pending" until it is read; read ones stay in the BBS's mailbox (as if
// SAVEd) until deleted (to the user's trash).  >INTERNET:addr is e-mail,
// where the BBS has it.

// Mail's modes of operation.
const (
	mailMenu    = "menu"
	mailPrompt  = "prompt"
	mailCommand = "command"
)

// mailState is a session's Mail settings and its list of messages.
type mailState struct {
	mode     string
	lineEdit bool // EDITOR uses line numbers
	paged    bool
	retain   bool
	name     string            // the Mail name (ADDRESS NAME), for this session
	book     map[string]string // the address book: name to address, for this session
	list     []*store.Message  // the messages as last listed, numbered from 1
}

// mailPrefs are the users' kept settings (bbs.Prefs).
const (
	prefMailMode = "cis-mailmode"
	prefMailEdit = "cis-mailedit"
	prefMailPage = "cis-mailpage"
)

// errLeaveMail ends Mail, its leaving done by the func carried.
type leaveMail struct{ then func() error }

func (l leaveMail) Error() string { return "cis: leaving Mail" }

// mailSettings are the session's, from the kept ones the first time.
func (s *session) mailSettings() *mailState {
	if s.mail != nil {
		return s.mail
	}
	p := s.node.Prefs()
	m := &mailState{mode: mailMenu, book: map[string]string{}}
	switch p[prefMailMode] {
	case mailPrompt, mailCommand:
		m.mode = p[prefMailMode]
	}
	m.lineEdit = p[prefMailEdit] == "line"
	if p[prefMailEdit] == "" {
		// none of Mail's own: the permanent online editor
		m.lineEdit = s.keptSetting(setEditor) == "linedit"
	}
	m.paged = p[prefMailPage] == "on"
	s.mail = m
	return m
}

// keepMailSettings makes the settings permanent.
func (s *session) keepMailSettings() {
	m := s.mail
	edit, page := "noline", "off"
	if m.lineEdit {
		edit = "line"
	}
	if m.paged {
		page = "on"
	}
	for k, v := range map[string]string{prefMailMode: m.mode, prefMailEdit: edit, prefMailPage: page} {
		if err := s.node.SetPref(k, v); err != nil {
			s.b.Log.Printf("node %d: CompuServe: keeping %s: %v", s.node.Num, k, err)
		}
	}
}

// pending are the user's unread messages.
func (s *session) pending() []*store.Message {
	all, err := s.b.Store.Inbox(s.user.ID)
	if err != nil {
		s.b.Log.Printf("node %d: CompuServe: mail: %v", s.node.Num, err)
		return nil
	}
	var out []*store.Message
	for _, m := range all {
		if !m.Read {
			out = append(out, m)
		}
	}
	return out
}

// saved are the user's read messages.
func (s *session) saved() []*store.Message {
	all, _ := s.b.Store.Inbox(s.user.ID)
	var out []*store.Message
	for _, m := range all {
		if m.Read {
			out = append(out, m)
		}
	}
	return out
}

func pendingLine(n int) string {
	switch n {
	case 0:
		return "No messages pending"
	case 1:
		return "1 message pending"
	}
	return fmt.Sprintf("%d messages pending", n)
}

// enterMail is GO MAIL: its prompt (or menu) until the caller leaves.
func (s *session) enterMail() error {
	m := s.mailSettings()
	s.node.Doing("CompuServe: Mail")
	m.list = s.pending()
	s.out("\nCompuServe Mail\n\n")
	if m.mode == mailMenu {
		s.mailMenu()
	} else {
		s.out(pendingLine(len(m.list)) + "\n\n")
	}
	for {
		line, err := s.prompt(s.mailPromptFor())
		if errors.Is(err, errAbort) {
			continue
		}
		if err != nil {
			return err
		}
		if err := s.mailCommands(line); err != nil {
			var l leaveMail
			if errors.As(err, &l) {
				return l.then()
			}
			return err
		}
	}
}

func (s *session) mailPromptFor() string {
	switch s.mail.mode {
	case mailMenu:
		return s.choicePrompt("Enter choice !")
	case mailPrompt:
		s.out("READ, SCAN, COMPOSE, UPLOAD, DOWNLOAD, ADDRESS, SET, HELP or EXIT\n")
	}
	return "Mail!"
}

// mailMenu is the Main Menu.
func (s *session) mailMenu() {
	n := len(s.pending())
	s.out("CompuServe Mail  Main Menu\n\n")
	s.outf(" 1 READ mail, %s\n", strings.ToLower(pendingLine(n)[:1])+pendingLine(n)[1:])
	s.out(" 2 COMPOSE a new message\n 3 UPLOAD a message\n 4 USE a file from PER area\n 5 ADDRESS Book\n 6 SET options\n\n")
}

// mailCommands does a line of Mail commands, separated by ";".
func (s *session) mailCommands(line string) error {
	for _, c := range strings.Split(line, ";") {
		if err := s.mailCommand(strings.TrimSpace(c)); err != nil {
			return err
		}
	}
	return nil
}

// mailCommand does a Mail command.  At the menu, a number is its choice.
func (s *session) mailCommand(c string) error {
	m := s.mail
	verb, arg := splitVerb(c)
	if m.mode == mailMenu && isNumber(verb) {
		switch verb {
		case "1":
			return s.readMenu()
		case "2":
			return s.compose("", nil)
		case "3":
			return s.mailUpload("")
		case "4":
			s.out("There is no PER area on this service.\n")
		case "5":
			return s.addressBook()
		case "6":
			return s.mailOptions()
		default:
			s.out(verb + " is not a choice !\n")
		}
		return nil
	}
	switch {
	case c == "":
		if m.mode == mailMenu {
			s.mailMenu()
		}
	case is(verb, "READ", 2):
		return s.mailRead(arg)
	case is(verb, "SCAN", 2) || is(verb, "LIST", 2) || is(verb, "DIRECTORY", 3):
		s.mailScan(arg)
	case is(verb, "COMPOSE", 3) || is(verb, "CREATE", 2) || is(verb, "SEND", 3):
		return s.compose(arg, nil)
	case is(verb, "UPLOAD", 3):
		return s.mailUpload(arg)
	case is(verb, "DOWNLOAD", 3):
		n, rest := mailArg(arg)
		return s.mailDownload(s.mailPick(n), rest)
	case is(verb, "DELETE", 3) || is(verb, "ERASE", 2):
		ms := s.mailPick(arg)
		if len(ms) == 0 {
			s.out("% No such message\n")
		}
		for _, msg := range ms {
			s.mailDelete(msg)
		}
	case is(verb, "ADDRESS", 3):
		if arg != "" {
			return s.addressCommand(arg)
		}
		return s.addressBook()
	case is(verb, "SET", 3):
		if arg == "" {
			return s.mailOptions()
		}
		s.mailSet(arg)
	case is(verb, "MODE", 2):
		return s.mailMode()
	case verb == "M" || is(verb, "MENU", 4) || is(verb, "MAIN", 4):
		s.mailMenu()
	case is(verb, "HELP", 1) || verb == "?":
		s.mailHelp()
	case is(verb, "EXIT", 2) || is(verb, "QUIT", 4):
		return leaveMail{func() error { s.show(s.page); return nil }}
	case verb == "T" || is(verb, "TOP", 3):
		return leaveMail{func() error { s.goTo(s.pages.Get("TOP")); return nil }}
	case verb == "G" || is(verb, "GO", 2):
		w := arg
		return leaveMail{func() error { return s.command("GO " + w) }}
	case is(verb, "OFF", 3) || is(verb, "BYE", 3):
		return leaveMail{func() error { s.logoff(); return errHangup }}
	case is(verb, "LOG", 3):
		return leaveMail{func() error { s.logoff(); return errRelog }}
	default:
		s.out("% Invalid command\n")
	}
	return nil
}

// mailArg splits "3 /PRO:B" into "3", "/PRO:B".
func mailArg(arg string) (string, string) {
	if i := strings.Index(arg, "/"); i >= 0 {
		return strings.TrimSpace(arg[:i]), arg[i:]
	}
	return strings.TrimSpace(arg), ""
}

// mailPick is the messages an argument names: numbers of the list (or
// ALL, or none for all pending).
func (s *session) mailPick(arg string) []*store.Message {
	m := s.mail
	arg = strings.ToUpper(strings.TrimSpace(arg))
	switch arg {
	case "", "ALL", "NEW", "WAITING", "WAI", "PENDING":
		m.list = s.pending()
		return m.list
	case "SAVED", "SAV", "OLD", "FILED":
		m.list = s.saved()
		return m.list
	}
	var out []*store.Message
	for _, f := range strings.FieldsFunc(arg, func(r rune) bool { return r == ',' || r == ' ' }) {
		lo, hi, isRange := strings.Cut(f, "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			continue
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(hi); err != nil {
				b = len(m.list)
			}
		}
		for i := a; i <= b && i <= len(m.list); i++ {
			if i >= 1 {
				out = append(out, m.list[i-1])
			}
		}
	}
	return out
}

// fromLine is a message's sender: "Name [72000,1]".
func fromLine(msg *store.Message) string {
	if msg.FromID > 0 {
		return fmt.Sprintf("%s [%s]", msg.From, PPN(msg.FromID))
	}
	return msg.From
}

// mailScan lists messages as the Read Menu does: " 1 Name/Subject".
func (s *session) mailScan(arg string) {
	ms := s.mailPick(arg)
	if len(ms) == 0 {
		s.out("No messages pending\n")
		return
	}
	for i, msg := range ms {
		s.outf("%s %s/%s\n", padNum(i+1, len(ms)), msg.From, msg.Subject)
	}
}

// readMenu is the menu's READ: the list, then a choice.
func (s *session) readMenu() error {
	m := s.mail
	m.list = s.pending()
	if len(m.list) == 0 {
		s.out("No messages pending\n")
		return nil
	}
	s.out("CompuServe Mail  Read Menu\n\n")
	for i, msg := range m.list {
		s.outf("%3d %s/%s\n", i+1, msg.From, msg.Subject)
	}
	s.outf("\n  0 READ ALL %d messages\n", len(m.list))
	for {
		c, err := s.prompt(s.choicePrompt("Last page.  Enter choice or M for CompuServe Mail main menu !"))
		if err != nil {
			return s.abortOK0(err)
		}
		c = strings.ToUpper(strings.TrimSpace(c))
		switch {
		case c == "M" || c == "":
			s.mailMenu()
			return nil
		case c == "0":
			return s.readMessages(m.list)
		case isNumber(c):
			ms := s.mailPick(c)
			if len(ms) == 0 {
				s.out(c + " is not a choice !\n")
				continue
			}
			return s.readMessages(ms)
		default:
			return s.mailCommand(c)
		}
	}
}

// mailRead is READ [n|ALL|SAVED].
func (s *session) mailRead(arg string) error {
	ms := s.mailPick(arg)
	if len(ms) == 0 {
		if strings.TrimSpace(arg) == "" || strings.EqualFold(strings.TrimSpace(arg), "ALL") {
			s.out("No messages pending\n")
		} else {
			s.out("% No such message\n")
		}
		return nil
	}
	return s.readMessages(ms)
}

// mailText is a message as it is read and downloaded:
//
//	Date:  18-Apr-93 22:19 EDT
//	From:  Deron Kazmaier [76317,3633]
//	Subj:  Hello
func mailText(msg *store.Message) []string {
	lines := []string{
		"Date:  " + msg.Created.Format("02-Jan-06 15:04 MST"),
		"From:  " + fromLine(msg),
		"Subj:  " + msg.Subject,
		"",
	}
	body := strings.TrimRight(strings.ReplaceAll(msg.Body, "\r\n", "\n"), "\n")
	return append(lines, strings.Split(body, "\n")...)
}

// readMessages reads messages one by one, each with its action prompt.
func (s *session) readMessages(ms []*store.Message) error {
	for i := 0; i < len(ms); i++ {
		msg := ms[i]
		s.out("\n")
		lines := mailText(msg)
		if s.mail.paged && s.lines > 0 {
			s.pageOut(lines)
		} else {
			for _, l := range lines {
				s.out(l + "\n")
			}
		}
		s.out("\n")
		if !msg.Read {
			s.b.Store.MarkMailRead(s.user.ID, msg.ID)
			msg.Read = true
		}
		again, rest, err := s.mailAction(msg)
		if err != nil {
			return err
		}
		if again {
			i--
			continue
		}
		if rest != "" {
			return s.mailCommands(rest)
		}
	}
	if s.mail.mode == mailMenu {
		s.mailMenu()
	}
	return nil
}

// mailAction is what to do with a message read: CR for the next, or
// DELETE, SAVE, REREAD, REPLY, FORWARD, DOWNLOAD.  Anything else ends the
// reading and is done as a Mail command (rest).
func (s *session) mailAction(msg *store.Message) (again bool, rest string, err error) {
	menu := s.mail.mode == mailMenu
	p := "Action!"
	if menu {
		s.out(" 1 DELETE this message\n 2 FILE in PER area\n 3 FORWARD\n 4 REREAD message\n 5 REPLY\n 6 SAVE in mailbox\n 7 DOWNLOAD message\n\n")
		p = s.choicePrompt("Enter choice or <CR> for next !")
	}
	for {
		c, err := s.prompt(p)
		if errors.Is(err, errAbort) {
			return false, "", nil
		}
		if err != nil {
			return false, "", err
		}
		c = strings.TrimSpace(c)
		verb, arg := splitVerb(c)
		if menu {
			if n, ok := map[string]string{"1": "DELETE", "2": "FILE", "3": "FORWARD", "4": "REREAD", "5": "REPLY", "6": "SAVE", "7": "DOWNLOAD"}[verb]; ok {
				verb = n
			}
		}
		switch {
		case c == "" || verb == "N" || is(verb, "NEXT", 2):
			return false, "", nil
		case is(verb, "DELETE", 3):
			s.mailDelete(msg)
			return false, "", nil
		case is(verb, "SAVE", 2) || is(verb, "KEEP", 2):
			s.out("Message saved\n")
			return false, "", nil
		case is(verb, "REREAD", 3):
			return true, "", nil
		case is(verb, "REPLY", 3):
			return false, "", s.compose(fromAddress(msg), msg)
		case is(verb, "FORWARD", 3):
			return false, "", s.forward(msg, arg)
		case is(verb, "DOWNLOAD", 3):
			return false, "", s.mailDownload([]*store.Message{msg}, arg)
		case is(verb, "FILE", 2):
			s.out("There is no PER area on this service.\n")
		case menu && isNumber(verb):
			s.out(verb + " is not a choice !\n")
		default:
			return false, c, nil
		}
	}
}

// fromAddress is where a reply to a message goes.
func fromAddress(msg *store.Message) string {
	if msg.FromID > 0 {
		return msg.From + " " + PPN(msg.FromID)
	}
	return msg.From
}

func (s *session) mailDelete(msg *store.Message) {
	if err := s.b.Store.DeleteMail(s.user.ID, msg.ID); err != nil {
		s.out("% Cannot delete message\n")
		return
	}
	s.out("Message deleted\n")
}

// mailDownload sends messages as a text file, by a protocol.
func (s *session) mailDownload(ms []*store.Message, arg string) error {
	if len(ms) == 0 {
		s.out("No messages pending\n")
		return nil
	}
	var b bytes.Buffer
	for _, msg := range ms {
		for _, l := range mailText(msg) {
			b.WriteString(l + "\r\n")
		}
		b.WriteString("\r\n")
	}
	s.outf("%d message%s and %d characters ready for download\n", len(ms), plural(len(ms)), b.Len())
	proto, err := s.chooseProtocol(protoArg(arg))
	if err != nil || proto == "" {
		return err
	}
	s.node.Doing("CompuServe: downloading mail")
	if err := s.send(proto, xfer.Outgoing{Name: "MAIL.TXT", Size: int64(b.Len()), ModTime: time.Now(), Data: &b}, false); err != nil {
		return nil
	}
	for _, msg := range ms {
		if !msg.Read {
			s.b.Store.MarkMailRead(s.user.ID, msg.ID)
			msg.Read = true
		}
	}
	s.out("*** File Transfer Completed! ***\n")
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// protoArg is /PROTO:x (or /PRO:x) of an argument, in capitals.
func protoArg(arg string) string {
	for _, f := range strings.FieldsFunc(arg, func(r rune) bool { return r == ' ' || r == '/' }) {
		if k, v, ok := strings.Cut(f, ":"); ok && is(strings.ToUpper(k), "PROTOCOL", 3) {
			return strings.ToUpper(v)
		}
	}
	return ""
}

// recipient is an address resolved: the name the BBS's mail takes, and
// how it is shown.
type recipient struct{ to, shown string }

// resolveAddress reads an address in CompuServe's forms:
//
//	72000,1   Alice 72000,1   Alice [72000,1]   alice
//	Alice >INTERNET:72000.1@compuserve.com   Bob@1:234/5 (FidoNet)
//
// A name in the address book is its address.
func (s *session) resolveAddress(a string) (recipient, error) {
	a = strings.TrimSpace(a)
	if i := strings.Index(strings.ToUpper(a), "/R"); i >= 0 && i == len(a)-2 {
		a = strings.TrimSpace(a[:i]) // return receipt: not kept
	}
	if addr, ok := s.mail.book[strings.ToUpper(a)]; ok {
		a = addr
	}
	name := ""
	if i := strings.Index(a, ">"); i >= 0 {
		name, a = strings.TrimSpace(a[:i]), strings.TrimSpace(a[i+1:])
	} else if i := strings.LastIndex(a, " "); i >= 0 {
		if _, ok := ParsePPN(strings.Trim(a[i+1:], "[]")); ok {
			name, a = strings.TrimSpace(a[:i]), strings.Trim(a[i+1:], "[]")
		}
	}
	a = strings.Trim(a, "[]")
	if net, rest, ok := strings.Cut(a, ":"); ok && !strings.Contains(a, "@") || ok && strings.EqualFold(net, "INTERNET") {
		switch strings.ToUpper(strings.TrimSpace(net)) {
		case "INTERNET":
			user, host, _ := strings.Cut(strings.TrimSpace(rest), "@")
			if strings.EqualFold(host, "compuserve.com") {
				if id, ok := ParsePPN(strings.Replace(user, ".", ",", 1)); ok {
					a = PPN(id)
					break
				}
			}
			addr := strings.TrimSpace(rest)
			if s.b.Email == nil || !bbs.IsEmailAddress(addr) {
				return recipient{}, errors.New("Internet mail is not offered on this service")
			}
			shown := "INTERNET:" + addr
			if name != "" {
				shown = name + " >" + shown
			}
			return recipient{addr, shown}, nil
		case "FAX", "POSTAL", "MCIMAIL", "X400", "MHS", "TLX", "TELEX", "EMS":
			return recipient{}, fmt.Errorf("%s mail is not offered on this service", strings.ToUpper(net))
		default:
			return recipient{}, errors.New("Invalid address")
		}
	}
	if id, ok := ParsePPN(a); ok {
		u, err := s.b.Store.UserByID(id)
		if err != nil {
			return recipient{}, errors.New("No such User ID")
		}
		return recipient{u.Name, u.Name + " [" + PPN(u.ID) + "]"}, nil
	}
	if a == "" {
		a = name
	}
	if strings.Contains(a, "@") {
		// FidoNet netmail: Name@zone:net/node
		return recipient{a, a}, nil
	}
	u, err := s.b.Store.UserByName(a)
	if err != nil {
		return recipient{}, errors.New("No such user")
	}
	return recipient{u.Name, u.Name + " [" + PPN(u.ID) + "]"}, nil
}

// askAddresses asks "Send to"; an entry ending in ";" asks for another.
func (s *session) askAddresses(given string) ([]recipient, error) {
	var out []recipient
	entry := given
	for {
		if entry == "" {
			var err error
			if entry, err = s.prompt("Send to (Name or User ID): "); err != nil {
				return nil, err
			}
			entry = strings.TrimSpace(entry)
			if entry == "" {
				return out, nil
			}
		}
		more := strings.HasSuffix(entry, ";")
		for _, a := range strings.Split(strings.TrimSuffix(entry, ";"), ";") {
			if strings.TrimSpace(a) == "" {
				continue
			}
			r, err := s.resolveAddress(a)
			if err != nil {
				s.outf("%% %s: %s\n", err, strings.TrimSpace(a))
				more = true
				continue
			}
			out = append(out, r)
		}
		if !more {
			return out, nil
		}
		entry = ""
	}
}

// compose is COMPOSE [address]: the text, then where it goes and its
// subject, then sent.  re is the message replied to.
func (s *session) compose(to string, re *store.Message) error {
	body, _, err := s.edit()
	if err != nil {
		return s.abortMail(err)
	}
	return s.sendMail(to, re, body)
}

// sendMail asks the addresses and the subject of a message, and sends it.
func (s *session) sendMail(to string, re *store.Message, body []string) error {
	rcpts, err := s.askAddresses(to)
	if err != nil {
		return s.abortMail(err)
	}
	if len(rcpts) == 0 {
		s.out("Message not sent\n")
		return nil
	}
	subject := ""
	if re != nil {
		subject = re.Subject
		s.outf("Subject: %s\n", subject)
	} else if subject, err = s.prompt("Subject: "); err != nil {
		return s.abortMail(err)
	}
	if strings.TrimSpace(subject) == "" {
		subject = "(no subject)"
	}
	ok, err := s.prompt("Send? (Y or N)! ")
	if err != nil {
		return s.abortMail(err)
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ok)), "Y") {
		s.out("Message not sent\n")
		return nil
	}
	var replyTo int64
	if re != nil {
		replyTo = re.ID
	}
	for _, r := range rcpts {
		if err := s.node.SendMail(r.to, subject, body, replyTo); err != nil {
			s.outf("%% Not sent to %s: %v\n", r.shown, err)
			continue
		}
		s.outf("Message sent to %s\n", r.shown)
	}
	return nil
}

// abortMail is an error of a prompt while writing: ^C gives up quietly.
func (s *session) abortMail(err error) error {
	if errors.Is(err, errAbort) {
		s.out("Message not sent\n")
		return nil
	}
	return err
}

// forward is FORWARD [address]: a message, with its header, to another.
func (s *session) forward(msg *store.Message, to string) error {
	body := append([]string{"[Forwarded message]"}, mailText(msg)...)
	re := &store.Message{Subject: msg.Subject}
	return s.sendMail(strings.TrimSpace(to), re, body)
}

// mailUpload is UPLOAD [/PRO:x] [/TYP:ASCII|LATIN1] [name]: a message's
// text by a protocol (TAPCIS's "UPL/PRO:B/TYP:ASCII MAIL%.TMP"), then sent.
func (s *session) mailUpload(arg string) error {
	typ := "ASCII"
	for _, f := range strings.FieldsFunc(arg, func(r rune) bool { return r == ' ' || r == '/' }) {
		if k, v, ok := strings.Cut(f, ":"); ok && is(strings.ToUpper(k), "TYPE", 3) {
			typ = strings.ToUpper(v)
		}
	}
	if strings.HasPrefix(typ, "B") || strings.HasPrefix(typ, "GIF") {
		s.out("Binary mail is not offered on this service.\n")
		return nil
	}
	proto, err := s.chooseProtocol(protoArg(arg))
	if err != nil || proto == "" {
		return err
	}
	if proto == "CAPTURE" {
		s.out("Uploads are by XMODEM, Kermit or the B protocol.\n")
		return nil
	}
	var buf bytes.Buffer
	in := &incoming{w: &limited{&buf, 2 << 20}, abort: func() {}, commit: func() error { return nil }}
	p := port{s}
	s.node.Doing("CompuServe: uploading mail")
	switch proto {
	case "B", "QB":
		k := xfer.NewBLink(p)
		if err = k.Start(xfer.HostParams); err == nil {
			_, err = k.ReceiveFile("MAIL.TXT", false, in, 2<<20)
		}
	default:
		s.outf("Starting %s transfer\n\nPlease initiate %s transfer\nand press <CR> when the transfer\nis complete.\n", proto, proto)
		sink := func(string, int64) (xfer.Incoming, error) { return in, nil }
		if proto == "XMODEM" {
			err = xfer.ReceiveXMODEM(p, sink)
		} else {
			err = xfer.ReceiveKermit(p, sink)
		}
		s.waitCR()
	}
	if err != nil {
		s.b.Log.Printf("node %d: CompuServe: %s mail upload failed: %v", s.node.Num, proto, err)
		s.out("\nTransfer aborted.\n")
		return nil
	}
	data := buf.Bytes()
	if proto == "XMODEM" {
		data = bytes.TrimRight(data, "\x1a") // the last block's padding
	}
	text := string(data)
	if typ == "LATIN1" || !utf8.ValidString(text) {
		r := make([]rune, len(data))
		for i, c := range data {
			r[i] = rune(c)
		}
		text = string(r)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	body := strings.Split(strings.TrimRight(text, "\n"), "\n")
	s.out("\n")
	return s.sendMail("", nil, body)
}

// limited is a writer that takes at most n bytes.
type limited struct {
	b *bytes.Buffer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if l.b.Len()+len(p) > l.n {
		return 0, errors.New("the message is too long")
	}
	return l.b.Write(p)
}

// mailOptions is SET alone: the Options Menu, and at the end whether to
// keep the changes.
func (s *session) mailOptions() error {
	m := s.mail
	changed := false
	for {
		s.out("\nMail  Options Menu\n\n[ ] Represents Current Setting\n    (yes/no options toggle)\n\n")
		s.outf(" 1 EDITOR uses line numbers [%s]\n", yesNo(m.lineEdit))
		s.outf(" 2 MODE of operation is [%s]\n   (MENU, PROMPT, COMMAND)\n", strings.ToUpper(m.mode))
		s.outf(" 3 Output is PAGED [%s]\n\n", yesNo(m.paged))
		c, err := s.prompt(s.choicePrompt("Enter choice or <CR> to exit !"))
		if err != nil {
			return s.abortOK0(err)
		}
		switch strings.TrimSpace(c) {
		case "":
			if changed {
				return s.askKeep()
			}
			return nil
		case "1":
			m.lineEdit, changed = !m.lineEdit, true
		case "2":
			if err := s.chooseMode(); err != nil {
				return err
			}
			changed = true
		case "3":
			m.paged, changed = !m.paged, true
		default:
			s.out(strings.TrimSpace(c) + " is not a choice !\n")
		}
	}
}

func yesNo(b bool) string {
	if b {
		return "YES"
	}
	return "NO"
}

// chooseMode is the Mode Menu.
func (s *session) chooseMode() error {
	s.out("\nMail  Mode Menu\n\n 1 MENU  - (Novice User)\n   List of choices in menu form.\n 2 PROMPT - (Intermediate User)\n   Brief list of commands.\n 3 COMMAND - (Expert User)\n   One word prompt.\n\n")
	c, err := s.prompt(s.choicePrompt("Enter choice !"))
	if err != nil {
		return s.abortOK0(err)
	}
	if mode, ok := map[string]string{"1": mailMenu, "2": mailPrompt, "3": mailCommand}[strings.TrimSpace(c)]; ok {
		s.mail.mode = mode
	}
	return nil
}

// mailMode is MODE: the Mode Menu, and whether to keep it.
func (s *session) mailMode() error {
	if err := s.chooseMode(); err != nil {
		return err
	}
	return s.askKeep()
}

func (s *session) askKeep() error {
	c, err := s.prompt("\nMake changes permanent for\nfuture Mail sessions ? (Y or N) !")
	if err != nil {
		return s.abortOK0(err)
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(c)), "Y") {
		s.keepMailSettings()
	}
	return nil
}

// mailSet is SET option value: EDIT LINE|NOLINE, PAGE ON|OFF, MODE x,
// RETAIN YES|NO.  These are kept.
func (s *session) mailSet(arg string) {
	m := s.mail
	k, v, _ := strings.Cut(strings.ToUpper(strings.TrimSpace(arg)), " ")
	v = strings.TrimSpace(v)
	switch {
	case is(k, "EDITOR", 2):
		m.lineEdit = v == "LINE" || v == "LINEDIT" || v == "YES" || v == "ON"
	case is(k, "PAGED", 3) || is(k, "PAGING", 3):
		m.paged = v == "ON" || v == "YES"
	case is(k, "RETAIN", 3):
		m.retain = v != "NO" && v != "OFF"
		return
	case is(k, "MODE", 2):
		mode, ok := map[string]string{"MENU": mailMenu, "PROMPT": mailPrompt, "COMMAND": mailCommand}[v]
		if !ok {
			s.out("% Invalid command\n")
			return
		}
		m.mode = mode
	default:
		s.out("% Invalid command\n")
		return
	}
	s.keepMailSettings()
}

// addressBook is ADDRESS: the Address Book menu.
func (s *session) addressBook() error {
	for {
		s.out("\nMail  Address Book\n\n 1 INSERT an entry\n 2 CHANGE an entry\n 3 DELETE an entry\n 4 LIST Address Book\n 5 Enter/Change your NAME\n\n")
		c, err := s.prompt(s.choicePrompt("Enter choice or <CR> to exit !"))
		if err != nil {
			return s.abortOK0(err)
		}
		c = strings.TrimSpace(c)
		if c == "" {
			return nil
		}
		if w, ok := map[string]string{"1": "INSERT", "2": "CHANGE", "3": "DELETE", "4": "LIST", "5": "NAME"}[c]; ok {
			c = w
		}
		if err := s.addressCommand(c); err != nil {
			return err
		}
	}
}

// addressCommand is an Address Book command (ADDRESS NAME, ...).  The
// book and the name last for the session.
func (s *session) addressCommand(c string) error {
	m := s.mail
	verb, arg := splitVerb(c)
	switch {
	case is(verb, "NAME", 2):
		old := m.name
		if old == "" {
			old = s.user.Name
		}
		s.out("\nEnter a period to erase any information,  or <CR> to leave it unchanged.\nTyping \"CANCEL\" cancels all changes made and leaves editing mode.\n\n")
		s.outf("Your CompuServe Mail name\n  Old: %s\n", old)
		n, err := s.prompt("  New: ")
		if err != nil {
			return s.abortOK0(err)
		}
		switch n = strings.TrimSpace(n); {
		case strings.EqualFold(n, "CANCEL"):
			return nil
		case n == ".":
			m.name = ""
		case n != "":
			m.name = n
		}
		yn, err := s.prompt("\nDo you want to enter your return address? (Y or N) !")
		if err != nil {
			return s.abortOK0(err)
		}
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(yn)), "Y") {
			s.out("Return addresses are not kept on this service.\n")
		}
	case is(verb, "INSERT", 3) || is(verb, "CHANGE", 3) || is(verb, "ADD", 3):
		name := strings.TrimSpace(arg)
		if name == "" {
			var err error
			if name, err = s.prompt("Name: "); err != nil {
				return s.abortOK0(err)
			}
		}
		addr, err := s.prompt("Address: ")
		if err != nil {
			return s.abortOK0(err)
		}
		if strings.TrimSpace(name) != "" && strings.TrimSpace(addr) != "" {
			m.book[strings.ToUpper(strings.TrimSpace(name))] = strings.TrimSpace(addr)
		}
	case is(verb, "DELETE", 3):
		name := strings.TrimSpace(arg)
		if name == "" {
			var err error
			if name, err = s.prompt("Name: "); err != nil {
				return s.abortOK0(err)
			}
		}
		delete(m.book, strings.ToUpper(strings.TrimSpace(name)))
	case is(verb, "LIST", 2):
		if len(m.book) == 0 {
			s.out("Your Address Book is empty.\n")
		}
		for i, k := range sortedKeys(m.book) {
			s.outf("%2d %-20s %s\n", i+1, k, m.book[k])
		}
	default:
		s.out("% Invalid command\n")
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (s *session) mailHelp() {
	s.out(`CompuServe Mail commands:
 READ [n|ALL|SAVED]  read messages (then CR, DELETE, SAVE, REREAD,
                     REPLY, FORWARD or DOWNLOAD at "Action!")
 SCAN [ALL|SAVED]    list messages       DELETE n     delete one
 COMPOSE [address]   write and send      UPLOAD       send a file's text
 DOWNLOAD [n] [/PROTO:B]                 ADDRESS      the address book
 SET [EDIT|PAGE|MODE x]                  EXIT, T, GO word, OFF
Addresses: a name, a User ID (72000,1), Name >INTERNET:72000.1@compuserve.com
`)
}
