package cis

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// Forum is a CompuServe forum: a group of the BBS's message areas, each a
// section (CompuServe's forums had up to 18, numbered from 0).  Its
// message numbers are the BBS's: forum-wide and rising, with gaps.
type Forum struct {
	Go       string // its GO word
	Name     string // "Amiga Tech Forum"
	Sections []*store.Area
}

// maxSections is how many sections a forum has at most.
const maxSections = 18

// Forums are the forums a user can read: one for each group of message
// areas (and one, named after the BBS, for those in none), split where a
// group has more than 18.
func (s *session) forums() ([]*Forum, error) {
	areas, err := s.b.Store.Areas()
	if err != nil {
		return nil, err
	}
	byGroup := map[string][]*store.Area{}
	var groups []string
	for _, a := range areas {
		if !s.b.CanRead(s.user, a) {
			continue
		}
		g := a.Group
		if _, ok := byGroup[g]; !ok {
			groups = append(groups, g)
		}
		byGroup[g] = append(byGroup[g], a)
	}
	var out []*Forum
	used := map[string]bool{}
	for _, g := range groups {
		name := g
		if name == "" {
			name = s.b.Conf().Name
		}
		as := byGroup[g]
		for part := 0; len(as) > 0; part++ {
			n := min(len(as), maxSections)
			f := &Forum{Name: forumName(name, part), Sections: as[:n]}
			f.Go = goWord(name, part, used)
			out = append(out, f)
			as = as[n:]
		}
	}
	return out, nil
}

func forumName(name string, part int) string {
	if !strings.HasSuffix(strings.ToLower(name), "forum") {
		name += " Forum"
	}
	if part > 0 {
		name += fmt.Sprintf(" %d", part+1)
	}
	return name
}

// goWord is a forum's GO word: its name's letters and digits, upper case,
// at most 10, made unique.
func goWord(name string, part int, used map[string]bool) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') && b.Len() < 10 {
			b.WriteRune(r)
		}
	}
	w := b.String()
	if w == "" {
		w = "FORUM"
	}
	if part > 0 {
		w += strconv.Itoa(part + 1)
	}
	base := w
	for i := 2; used[w]; i++ {
		w = base + strconv.Itoa(i)
	}
	used[w] = true
	return w
}

// forum finds a forum by GO word.
func (s *session) forum(word string) *Forum {
	fs, err := s.forums()
	if err != nil {
		return nil
	}
	word = strings.ToUpper(strings.TrimSpace(word))
	for _, f := range fs {
		if f.Go == word {
			return f
		}
	}
	return nil
}

// forumsPage is the FORUMS menu, made from the forums there are.
func (s *session) forumsPage() *Page {
	p := &Page{ID: "FORUMS", Title: "CompuServe"}
	fs, _ := s.forums()
	for _, f := range fs {
		p.Items = append(p.Items, Item{Text: f.Name, Target: f.Go})
	}
	if len(fs) == 0 {
		p.Text = []string{" There are no forums here yet."}
	}
	return p
}

// section is the section number of an area in f, or -1.
func (f *Forum) section(area int64) int {
	for i, a := range f.Sections {
		if a.ID == area {
			return i
		}
	}
	return -1
}

func sectionName(a *store.Area) string {
	n := a.Name
	if len(n) > 20 {
		n = n[:20]
	}
	return n
}

// forumState is a caller's time in a forum.
type forumState struct {
	f        *Forum
	messages bool            // at "Messages !" rather than "Forum !"
	nonstop  bool            // OPT TYPE NONSTOP: no pause after each message
	read     map[int64]int64 // the highest read here, by area
	last     *store.Message  // the message last shown
}

// enterForum is GO to a forum: its welcome, then its command prompt until
// the caller leaves.
func (s *session) enterForum(f *Forum) error {
	st := &forumState{f: f, read: map[int64]int64{}}
	s.node.Doing("CompuServe: " + f.Name)
	lo, hi := int64(0), int64(0)
	var lastRead int64
	for _, a := range f.Sections {
		if a.Messages > 0 {
			if lo == 0 || a.FirstID < lo {
				lo = a.FirstID
			}
			hi = max(hi, a.LastID)
		}
		lr, _ := s.b.Store.LastRead(s.user.ID, a.ID)
		st.read[a.ID] = lr
		lastRead = max(lastRead, lr)
	}
	s.out("One moment please...\n")
	s.outf("Welcome to %s, V. 4D(1)\n\n", f.Name)
	s.outf("Hello, %s\n", s.user.Name)
	if !s.user.LastLogin.IsZero() {
		s.outf("Last visit:  %s\n", s.user.LastLogin.Format("02-Jan-06  15:04:05"))
	}
	s.outf("Forum messages: %6d to %6d\n", lo, hi)
	s.outf("Last message you've read: %6d\n", lastRead)
	s.out("Subtopic(s) Selected:\n All Accessible\n")
	s.out("No members are in conference.\n")
	if n := s.waiting(st); n > 0 {
		s.outf("You have %d message(s) waiting.\n", n)
	}
	for {
		p := "Forum !"
		if st.messages {
			p = "Messages !"
		}
		line, err := s.prompt(p)
		if errors.Is(err, errAbort) {
			continue
		}
		if err != nil {
			return err
		}
		leave, err := s.forumCommands(st, line)
		if err != nil {
			return err
		}
		if leave != nil {
			s.exitForum(st, hi)
			return leave()
		}
	}
}

// exitForum is the leaving text.
func (s *session) exitForum(st *forumState, hi int64) {
	var lr int64
	for _, id := range st.read {
		lr = max(lr, id)
	}
	s.outf("Exiting at %s\n", s.now().Format("02-Jan-06  15:04:05"))
	s.outf("Last message in forum: %10d\n", hi)
	s.outf("Last message you've read: %6d\n", lr)
	s.outf("Thank you for visiting %s\n", st.f.Name)
}

// forumCommands does a line of forum commands.  A command that leaves the
// forum returns what to do after the leaving text.
func (s *session) forumCommands(st *forumState, line string) (func() error, error) {
	cmds := strings.Split(line, ";")
	for i := 0; i < len(cmds); i++ {
		c := strings.TrimSpace(cmds[i])
		verb, arg := splitVerb(c)
		switch {
		case c == "":
			if len(cmds) == 1 {
				continue
			}
		case is(verb, "OPTIONS", 2):
			// OPT's settings follow it on the line: OPT;TYP N;SKI N;...
			rest := cmds[i+1:]
			i = len(cmds)
			s.options(st, arg, rest)
		case is(verb, "MESSAGES", 3):
			st.messages = true
			if arg != "" {
				cmds = append(cmds[:i+1], append([]string{arg}, cmds[i+1:]...)...)
			}
		case verb == "M" || is(verb, "MENU", 4):
			if st.messages {
				st.messages = false
				continue
			}
			return func() error { s.show(s.page); return nil }, nil
		case is(verb, "READ", 3) || verb == "R" || verb == "RS" || verb == "RN" || verb == "RT",
			is(verb, "BROWSE", 2):
			rest, err := s.readCmd(st, verb, arg, is(verb, "BROWSE", 2))
			if err != nil || rest == "" {
				if err != nil {
					return nil, err
				}
				continue
			}
			if leave, err := s.forumCommands(st, rest); leave != nil || err != nil {
				return leave, err
			}
		case is(verb, "SCAN", 2):
			s.scan(st, arg)
		case is(verb, "POST", 3) || is(verb, "COMPOSE", 3) || is(verb, "LEAVE", 3) || verb == "L":
			if err := s.post(st, arg, nil); err != nil {
				return nil, err
			}
		case is(verb, "DESCRIPTIONS", 2) || is(verb, "SECTIONS", 3):
			s.sections(st)
		case is(verb, "NEW", 2):
			s.newCounts(st)
		case is(verb, "LIBRARY", 3) || is(verb, "DL", 2):
			s.out("The libraries of this forum are not open yet.\n")
		case is(verb, "CONFERENCE", 2):
			s.out("Conferencing is not open yet.\n")
		case is(verb, "HELP", 1) || verb == "?":
			s.forumHelp()
		case is(verb, "EXIT", 2) || verb == "T" || is(verb, "TOP", 3):
			if verb == "T" || is(verb, "TOP", 3) {
				return func() error { s.goTo(s.pages.Get("TOP")); return nil }, nil
			}
			return func() error { s.show(s.page); return nil }, nil
		case verb == "G" || is(verb, "GO", 2):
			w := arg
			return func() error { return s.command("GO " + w) }, nil
		case is(verb, "OFF", 3) || is(verb, "BYE", 3):
			return func() error { s.logoff(); return errHangup }, nil
		case is(verb, "LOG", 3):
			return func() error { s.logoff(); return errRelog }, nil
		case is(verb, "SET", 3):
			s.set(arg)
		default:
			s.outf("%s is not a forum command !\n", strings.ToUpper(c))
		}
	}
	return nil, nil
}

// splitVerb is a command's first word and the rest; "BRO/LONG" is
// "BRO", "/LONG".
func splitVerb(c string) (string, string) {
	i := strings.IndexAny(c, " /")
	if i < 0 {
		return strings.ToUpper(c), ""
	}
	return strings.ToUpper(c[:i]), strings.TrimSpace(c[i:])
}

// is reports whether v abbreviates word, in at least n letters.
func is(v, word string, n int) bool {
	return len(v) >= n && len(v) <= len(word) && strings.HasPrefix(word, v)
}

// options is OPT: its settings, given after it.
func (s *session) options(st *forumState, arg string, rest []string) {
	if arg != "" {
		rest = append([]string{arg}, rest...)
	}
	if len(rest) == 0 {
		s.out("TYPE (N)onstop or (P)ause, MODE (COM)mand -- e.g. OPT;TYP N\n")
		return
	}
	for _, r := range rest {
		verb, val := splitVerb(strings.TrimSpace(r))
		val = strings.ToUpper(strings.TrimSpace(val))
		switch {
		case is(verb, "TYPE", 2):
			st.nonstop = strings.HasPrefix(val, "N")
		}
		// the rest (SKIP, MODE, INITIAL, CHARACTER, PERMANENT...) are
		// taken: this forum is always in command mode
	}
	s.out("Options set\n")
}

// query is what REA, SCA and BRO choose.
type query struct {
	newOnly  bool
	waiting  bool
	reverse  bool
	thread   bool
	from, to string
	subject  string
	sections map[int]bool
	start    int64
	days     int
	numbers  []int64
}

func parseQuery(arg string) query {
	q := query{newOnly: true}
	for _, f := range strings.FieldsFunc(arg, func(r rune) bool { return r == ' ' || r == '/' }) {
		k, v, hasV := strings.Cut(f, ":")
		K := strings.ToUpper(k)
		switch {
		case isNumber(K):
			n, _ := strconv.ParseInt(K, 10, 64)
			q.numbers = append(q.numbers, n)
			q.newOnly = false
		case !hasV && is(K, "NEW", 3):
			q.newOnly = true
		case !hasV && is(K, "ALL", 3):
			q.newOnly = false
		case !hasV && is(K, "WAITING", 3):
			q.waiting = true
		case !hasV && is(K, "REVERSE", 3):
			q.reverse, q.newOnly = true, false
		case !hasV && is(K, "THREAD", 3):
			q.thread = true
		case !hasV && is(K, "FORWARD", 3):
			q.reverse = false
		case hasV && is(K, "FROM", 3):
			q.from = v
		case hasV && K == "TO":
			q.to = v
		case hasV && is(K, "SUBJECT", 3):
			q.subject = v
		case hasV && is(K, "SECTIONS", 3):
			q.sections = map[int]bool{}
			for _, n := range strings.Split(v, ",") {
				if k, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
					q.sections[k] = true
				}
			}
		case hasV && is(K, "START", 3):
			q.start, _ = strconv.ParseInt(v, 10, 64)
			q.newOnly = false
		case hasV && K == "DAYS":
			q.days, _ = strconv.Atoi(v)
			q.newOnly = false
		case hasV && is(K, "NUMBER", 3):
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				q.numbers = append(q.numbers, n)
				q.newOnly = false
			}
		}
	}
	return q
}

// findMessages is the forum's messages q chooses, oldest first (newest, REVERSE).
func (s *session) findMessages(st *forumState, q query) []*store.Message {
	var out []*store.Message
	if len(q.numbers) > 0 {
		for _, n := range q.numbers {
			m, err := s.b.Store.Message(n)
			if err == nil && st.f.section(m.AreaID) >= 0 {
				out = append(out, m)
			}
		}
		return out
	}
	cut := time.Time{}
	if q.days > 0 {
		cut = s.now().AddDate(0, 0, -q.days)
	}
	for i, a := range st.f.Sections {
		if q.sections != nil && !q.sections[i] {
			continue
		}
		after := q.start - 1
		if q.newOnly || q.waiting {
			after = max(after, st.read[a.ID])
		}
		ms, err := s.b.Store.Messages(a.ID, max(after, 0), 5000)
		if err != nil {
			continue
		}
		for _, m := range ms {
			switch {
			case !cut.IsZero() && m.Created.Before(cut):
			case q.waiting && !strings.EqualFold(m.To, s.user.Name):
			case q.from != "" && !matches(m.From, q.from) && PPN(m.FromID) != q.from:
			case q.to != "" && !matches(m.To, q.to):
			case q.subject != "" && !matches(m.Subject, q.subject):
			default:
				out = append(out, m)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if q.thread {
		// each topic together, in the order its first message came
		root := func(m *store.Message) int64 {
			if m.Thread != 0 {
				return m.Thread
			}
			return m.ID
		}
		sort.SliceStable(out, func(i, j int) bool { return root(out[i]) < root(out[j]) })
	}
	if q.reverse {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}

func matches(field, want string) bool {
	return strings.Contains(strings.ToUpper(field), strings.ToUpper(strings.TrimSpace(want)))
}

// waiting counts the new messages to the caller.
func (s *session) waiting(st *forumState) int {
	return len(s.findMessages(st, query{waiting: true}))
}

// replies are the numbers of the messages that answer m.
func (s *session) replies(m *store.Message) []int64 {
	ms, _, err := s.b.Store.Thread(m)
	if err != nil {
		return nil
	}
	var out []int64
	for _, r := range ms {
		if r.ReplyTo == m.ID {
			out = append(out, r.ID)
		}
	}
	return out
}

// header is a message's header, as forum software 4 printed it:
//
//	#: 210094 S10/Views:Zachmann
//	    04-Jul-92  12:33:25
//	Sb: #210070-#Independence Declaration
//	Fm: Bruce Biermann 72241,261
//	To: William F. Zachmann 72241,43
func (s *session) header(st *forumState, m *store.Message, replies []int64) []string {
	sec := st.f.section(m.AreaID)
	sb := m.Subject
	if len(replies) > 0 {
		sb = "#" + sb
	}
	if m.ReplyTo != 0 {
		sb = fmt.Sprintf("#%d-%s", m.ReplyTo, sb)
	}
	to := m.To
	if to == "" {
		to = "All"
	}
	if u, err := s.b.Store.UserByName(m.To); err == nil {
		to += " " + PPN(u.ID)
	}
	from := m.From
	if m.FromID != 0 {
		from += " " + PPN(m.FromID)
	}
	return []string{
		fmt.Sprintf("#: %d S%d/%s", m.ID, sec, sectionName(st.f.Sections[sec])),
		"    " + m.Created.Format("02-Jan-06  15:04:05"),
		"Sb: " + sb,
		"Fm: " + from,
		"To: " + to,
	}
}

// replyLine is the footer: "* Reply:  n" or "* Replies:  n, n".
func replyLine(replies []int64) []string {
	if len(replies) == 0 {
		return nil
	}
	label := "* Reply:  "
	if len(replies) > 1 {
		label = "* Replies:  "
	}
	var out []string
	line := label
	for i, r := range replies {
		item := strconv.FormatInt(r, 10)
		if i < len(replies)-1 {
			item += ","
		}
		if len(line)+len(item)+1 > 72 {
			out = append(out, line)
			line = strings.Repeat(" ", 12)
		} else if i > 0 {
			line += " "
		}
		line += item
	}
	return append(out, line)
}

// showMessage prints a message whole.
func (s *session) showMessage(st *forumState, m *store.Message) {
	replies := s.replies(m)
	lines := append(s.header(st, m, replies), "")
	body := strings.Split(strings.TrimRight(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n"), "\n")
	lines = append(lines, body...)
	if r := replyLine(replies); r != nil {
		lines = append(append(lines, ""), r...)
	}
	s.out("\n")
	for _, l := range lines {
		s.out(l + "\n")
	}
	st.last = m
	if m.ID > st.read[m.AreaID] {
		st.read[m.AreaID] = m.ID
		s.b.Store.SetLastRead(s.user.ID, m.AreaID, m.ID)
	}
}

// readCmd is READ (or BROWSE: its subjects first, each read on a CR).
// What the caller types at a read prompt that is not for it ends the
// reading, and is returned to be done as a forum command.
func (s *session) readCmd(st *forumState, verb, arg string, browse bool) (string, error) {
	q := parseQuery(arg)
	switch verb {
	case "RS", "RT":
		q.thread = true
	}
	ms := s.findMessages(st, q)
	if len(ms) == 0 {
		s.out("No messages\n")
		return "", nil
	}
	for i := 0; i < len(ms); i++ {
		m := ms[i]
		if browse {
			s.out("\n")
			for _, l := range s.header(st, m, s.replies(m)) {
				s.out(l + "\n")
			}
			line, err := s.prompt("Press <CR> to read, N for next, or type CHOICES !")
			if errors.Is(err, errAbort) {
				return "", nil
			}
			if err != nil {
				return "", err
			}
			switch strings.ToUpper(strings.TrimSpace(line)) {
			case "":
			case "N", "NEXT":
				continue
			default:
				return line, nil
			}
		}
		s.showMessage(st, m)
		if st.nonstop {
			continue
		}
		for {
			line, err := s.prompt("Read action !")
			if errors.Is(err, errAbort) {
				return "", nil
			}
			if err != nil {
				return "", err
			}
			verb, rarg := splitVerb(strings.TrimSpace(line))
			switch {
			case verb == "" || is(verb, "NEXT", 3):
			case is(verb, "REPLY", 2):
				if err := s.post(st, rarg, m); err != nil {
					return "", err
				}
				continue
			case is(verb, "REREAD", 3):
				s.showMessage(st, m)
				continue
			case verb == "P" || is(verb, "PARENT", 3):
				if m.ReplyTo == 0 {
					s.out("This message has no parent.\n")
					continue
				}
				if pm, err := s.b.Store.Message(m.ReplyTo); err == nil {
					s.showMessage(st, pm)
				}
				continue
			case is(verb, "CHOICES", 2):
				s.out(" RE  Reply    RER Reread    P   Parent\n NEX or <CR> Next    STOP Stop reading\n")
				continue
			case is(verb, "STOP", 2):
				return "", nil
			default:
				return line, nil
			}
			break
		}
	}
	return "", nil
}

// scan is SCAN: the headers of the messages chosen, without their text;
// QUICK, one line each.
func (s *session) scan(st *forumState, arg string) {
	quick := false
	for _, f := range strings.Fields(strings.ToUpper(arg)) {
		if is(f, "QUICK", 3) {
			quick = true
		}
	}
	ms := s.findMessages(st, parseQuery(arg))
	if len(ms) == 0 {
		s.out("No messages\n")
		return
	}
	for _, m := range ms {
		if quick {
			s.outf("%7d S%-2d %s\n", m.ID, st.f.section(m.AreaID), m.Subject)
			continue
		}
		s.out("\n")
		for _, l := range s.header(st, m, s.replies(m)) {
			s.out(l + "\n")
		}
	}
}

// sections lists the forum's sections.
func (s *session) sections(st *forumState) {
	s.out("\nSections\n")
	for i, a := range st.f.Sections {
		s.outf("%2d %s\n", i, sectionName(a))
	}
}

// newCounts is NEW: the new messages in each section.
func (s *session) newCounts(st *forumState) {
	s.out("\nSection                 New\n")
	for i, a := range st.f.Sections {
		n, _ := s.b.Store.NewCount(s.user.ID, a.ID)
		s.outf("%2d %-20s %4d\n", i, sectionName(a), n)
	}
}

func (s *session) forumHelp() {
	s.out(`
Forum commands:
 REA [NEW|ALL|WAI|REV|THR] [FROM:x] [TO:x] [SUB:x] [SEC:n,n] [n]
 SCA [QUI] ... (as READ, headers only)   BRO ... (as READ, a subject at a time)
 POST [SEC:n] [TO:x] [SUB:x]   DES (sections)   NEW (new messages)
 OPT;TYP N (nonstop)   MES (Messages !)   M or EXIT (leave)   OFF
`)
}

// post is POST (or REPLY to re, in its section with its subject).
func (s *session) post(st *forumState, arg string, re *store.Message) error {
	var to, subject string
	section := -1
	for _, f := range strings.FieldsFunc(arg, func(r rune) bool { return r == '/' }) {
		k, v, ok := strings.Cut(strings.TrimSpace(f), ":")
		K := strings.ToUpper(k)
		switch {
		case ok && K == "TO":
			to = v
		case ok && is(K, "SUBJECT", 3):
			subject = v
		case ok && is(K, "SECTIONS", 3):
			if n, err := strconv.Atoi(v); err == nil {
				section = n
			}
		}
	}
	var replyTo int64
	if re != nil {
		replyTo = re.ID
		section = st.f.section(re.AreaID)
		subject = re.Subject
		if to == "" {
			to = re.From
		}
	}
	var err error
	if to == "" {
		if to, err = s.prompt("To: "); err != nil {
			return s.abortOK(err)
		}
		if strings.TrimSpace(to) == "" {
			to = "All"
		}
	}
	if subject == "" {
		if subject, err = s.prompt("Subject: "); err != nil {
			return s.abortOK(err)
		}
		if strings.TrimSpace(subject) == "" {
			s.out("Message not stored\n")
			return nil
		}
	}
	body, done, err := s.edit()
	if err != nil {
		return s.abortOK(err)
	}
	for !done {
		a, err := s.prompt("Post action !")
		if err != nil {
			return s.abortOK(err)
		}
		v, _ := splitVerb(strings.TrimSpace(a))
		switch {
		case v == "S" || is(v, "STORE", 2) || is(v, "POST", 2):
			done = true
		case v == "A" || is(v, "ABORT", 2):
			s.out("Message not stored\n")
			return nil
		case v == "C" || is(v, "CONTINUE", 2):
			more, st2, err := s.edit()
			if err != nil {
				return s.abortOK(err)
			}
			body, done = append(body, more...), st2
		default:
			s.out(" S  Store the message\n C  Continue entering text\n A  Abort\n")
		}
	}
	for section < 0 || section >= len(st.f.Sections) || !s.b.CanPost(s.user, st.f.Sections[section]) {
		s.out("SUBTOPIC # REQUIRED\n")
		for i, a := range st.f.Sections {
			if s.b.CanPost(s.user, a) {
				s.outf("%2d %s\n", i, sectionName(a))
			}
		}
		a, err := s.prompt("Enter choice : ")
		if err != nil {
			return s.abortOK(err)
		}
		if strings.TrimSpace(a) == "" {
			s.out("Message not stored\n")
			return nil
		}
		section, _ = strconv.Atoi(strings.TrimSpace(a))
		if section < 0 || section >= len(st.f.Sections) {
			section = -1
		}
	}
	id, err := s.node.Post(st.f.Sections[section], strings.TrimSpace(to), strings.TrimSpace(subject), body, replyTo)
	if err != nil {
		s.outf("Message not stored: %v\n", err)
		return nil
	}
	s.outf("Message # %d Stored\n", id)
	return nil
}

// abortOK is an error of a prompt: ^C gives up quietly.
func (s *session) abortOK(err error) error {
	if errors.Is(err, errAbort) {
		s.out("Message not stored\n")
		return nil
	}
	return err
}

// edit is CompuServe's EDIT for a new message: lines until /EX (asks what
// to do) or /POST (stores it).
func (s *session) edit() (lines []string, done bool, err error) {
	s.out("Enter /EX to exit EDIT\n")
	for {
		l, err := s.input(true, s.b.Conf().IdleTimeout)
		if err != nil {
			if errors.Is(err, errIdle) {
				return nil, false, errHangup
			}
			return nil, false, err
		}
		switch strings.ToUpper(strings.TrimSpace(l)) {
		case "/EX", "/EXIT":
			return lines, false, nil
		case "/POST", "/STORE":
			return lines, true, nil
		}
		lines = append(lines, l)
		if len(lines) > 1000 {
			return lines, false, nil
		}
	}
}
