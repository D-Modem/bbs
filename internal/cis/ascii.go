package cis

import (
	"errors"
	"strconv"
	"strings"
)

// errRelog is LOG: off, and back to "User ID:".
var errRelog = errors.New("cis: LOG")

// ascii is the terminal service, from the banner to the logoff.
func (s *session) ascii() error {
	s.banner()
	top := s.pages.Get("TOP")
	switch p := s.pages.Get(s.firstGo); {
	case s.firstGo == "" || !s.known(s.firstGo):
		s.show(top)
	case p != nil && p.ID != "MAIL" && p.ID != "CB":
		s.show(p)
	default:
		// /GO:word to a forum or Mail: in it first, TOP when it is left
		s.page, s.stack, s.menus = top, []*Page{top}, []*Page{top}
		if err := s.goWord(s.firstGo); err != nil {
			return err
		}
	}
	for {
		line, err := s.prompt(s.promptFor())
		if errors.Is(err, errAbort) {
			continue
		}
		if err != nil {
			return err
		}
		if err := s.commands(line); err != nil {
			return err
		}
	}
}

// promptFor is the prompt of the page shown.
func (s *session) promptFor() string {
	switch {
	case len(s.more) > 0:
		return s.choicePrompt("Press <CR> for more !")
	case s.page.Menu():
		return s.choicePrompt("Enter choice !")
	}
	return s.choicePrompt("Last page !")
}

// commands does a line of commands, separated by ";".
func (s *session) commands(line string) error {
	if strings.TrimSpace(line) == "" {
		return s.command("")
	}
	for _, c := range strings.Split(line, ";") {
		if err := s.command(strings.TrimSpace(c)); err != nil {
			return err
		}
	}
	return nil
}

// command does one command.
func (s *session) command(c string) error {
	verb, arg, _ := strings.Cut(c, " ")
	verb, arg = strings.ToUpper(verb), strings.TrimSpace(arg)
	switch {
	case verb == "":
		switch {
		case len(s.more) > 0:
			s.pageOut(s.more)
		case !s.page.Menu():
			// the end of an article: back to its menu
			s.backToMenu()
		default:
			s.show(s.page)
		}
	case isNumber(verb):
		n, _ := strconv.Atoi(verb)
		if !s.page.Menu() || n < 1 || n > len(s.page.Items) {
			s.out(verb + " is not a choice !\n")
			return nil
		}
		return s.goWord(s.page.Items[n-1].Target)
	case verb == "T" || verb == "TOP":
		s.goTo(s.pages.Get("TOP"))
	case verb == "M" || verb == "MENU":
		s.backToMenu()
	case verb == "B" || verb == "BACK":
		if len(s.stack) > 1 {
			s.stack = s.stack[:len(s.stack)-1]
			p := s.stack[len(s.stack)-1]
			s.stack = s.stack[:len(s.stack)-1]
			s.goTo(p)
		} else {
			s.show(s.page)
		}
	case verb == "F" || verb == "FORWARD":
		if len(s.more) > 0 {
			s.pageOut(s.more)
		} else {
			s.out(s.choicePrompt("Last page") + "\n")
		}
	case verb == "R" || verb == "RESEND":
		s.show(s.page)
	case verb == "N" || verb == "NEXT", verb == "P" || verb == "PREVIOUS":
		s.sibling(verb[0] == 'N')
	case verb == "H" || verb == "HELP" || verb == "?":
		s.goTo(s.pages.Get("HELP"))
	case verb == "G" || verb == "GO":
		if arg == "" {
			s.out("GO where?  Type GO and a GO word or page ID.\n")
			return nil
		}
		if !s.known(arg) {
			s.out(strings.ToUpper(arg) + " is unrecognized !\n")
			return nil
		}
		return s.goWord(arg)
	case verb == "FIND":
		s.find(arg)
	case verb == "SET":
		s.set(arg)
	case verb == "OFF" || verb == "BYE":
		s.logoff()
		return errHangup
	case verb == "LOG" || verb == "LOGOFF":
		s.logoff()
		return errRelog
	default:
		// a GO word alone takes you there too
		if s.known(c) {
			return s.goWord(c)
		}
		s.out(strings.ToUpper(c) + " is unrecognized !\n")
	}
	return nil
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// known reports whether a GO word is a page's or a forum's.
func (s *session) known(word string) bool {
	return s.pages.Get(word) != nil || s.forum(word) != nil
}

// goWord goes to a page or into a forum.
func (s *session) goWord(word string) error {
	if f := s.forum(word); f != nil && s.pages.Get(word) == nil {
		return s.enterForum(f)
	}
	p := s.pages.Get(word)
	if p != nil && p.ID == "MAIL" {
		return s.enterMail()
	}
	if p != nil && p.ID == "CB" {
		return s.enterCB()
	}
	s.goTo(p)
	return nil
}

// goTo shows a page and remembers it for B and M.
func (s *session) goTo(p *Page) {
	if p == nil {
		return
	}
	if p.ID == "FORUMS" {
		// made afresh each time it is gone to: the forums there are now
		s.fpage = s.forumsPage()
		p = s.fpage
	}
	if p.Menu() {
		s.menus = append(s.menus, p)
	}
	s.show(p)
}

// show shows a page.
func (s *session) show(p *Page) {
	if p.ID == "FORUMS" {
		if s.fpage == nil {
			s.fpage = s.forumsPage()
		}
		p = s.fpage
	}
	s.page = p
	s.more = nil
	if len(s.stack) == 0 || s.stack[len(s.stack)-1] != p {
		s.stack = append(s.stack, p)
		if len(s.stack) > 100 {
			s.stack = s.stack[1:]
		}
	}
	s.node.Doing("CompuServe: " + p.ID)
	lines := []string{heading(p, s.width), ""}
	lines = append(lines, p.Text...)
	for i, it := range p.Items {
		lines = append(lines, padNum(i+1, len(p.Items))+" "+it.Text)
	}
	s.out("\n")
	s.pageOut(lines)
}

// padNum is a menu item's number, right-aligned to the widest.
func padNum(n, of int) string {
	w := len(strconv.Itoa(of))
	if w < 2 {
		w = 2
	}
	k := strconv.Itoa(n)
	return strings.Repeat(" ", w-len(k)) + k
}

// pageOut sends lines, a screen at a time when paged: what is left waits
// in s.more for a CR.
func (s *session) pageOut(lines []string) {
	n := len(lines)
	if s.paged && s.lines > 2 && n > s.lines-1 {
		n = s.lines - 1
	}
	for _, l := range lines[:n] {
		s.out(l + "\n")
	}
	s.more = lines[n:]
	if len(s.more) == 0 {
		s.more = nil
	}
}

// backToMenu goes back to the menu before (or the page's own menu).
func (s *session) backToMenu() {
	for len(s.menus) > 0 {
		m := s.menus[len(s.menus)-1]
		if m != s.page {
			s.show(m)
			return
		}
		s.menus = s.menus[:len(s.menus)-1]
	}
	if p := s.pages.Parent(s.page); p != nil {
		s.goTo(p)
		return
	}
	s.goTo(s.pages.Get("TOP"))
}

// sibling is N or P: the next or previous item of the menu the page is on.
func (s *session) sibling(forward bool) {
	m := s.pages.Parent(s.page)
	if len(s.menus) > 0 && s.menus[len(s.menus)-1] != s.page {
		m = s.menus[len(s.menus)-1]
	}
	if m == nil {
		s.out("There is no next item.\n")
		return
	}
	for i, it := range m.Items {
		if it.Target != s.page.ID {
			continue
		}
		j := i + 1
		if !forward {
			j = i - 1
		}
		if j < 0 || j >= len(m.Items) {
			break
		}
		s.show(s.pages.Get(m.Items[j].Target))
		return
	}
	if forward {
		s.out("There is no next item.\n")
	} else {
		s.out("There is no previous item.\n")
	}
}

// find is FIND topic: the pages that mention it.
func (s *session) find(topic string) {
	if topic == "" {
		s.goTo(s.pages.Get("FIND"))
		return
	}
	ps := s.pages.Find(topic)
	if len(ps) == 0 {
		s.out("\nNo matches were found for " + strings.ToUpper(topic) + ".\n")
		return
	}
	lines := []string{"", "Index of " + strings.ToUpper(topic), ""}
	for _, p := range ps {
		title := p.Title
		if title == "CompuServe" && len(p.Text) > 0 {
			title = strings.TrimSpace(p.Text[0])
		}
		if title == "CompuServe" {
			if m := s.pages.Parent(p); m != nil {
				for _, it := range m.Items {
					if it.Target == p.ID {
						title = it.Text
					}
				}
			}
		}
		lines = append(lines, " "+pad(title, 50)+" [GO "+p.ID+"]")
	}
	s.pageOut(lines)
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// set is SET: BRIEF, PAGED, WIDTH, LINES.
func (s *session) set(arg string) {
	f := strings.Fields(strings.ToUpper(arg))
	if len(f) != 2 {
		s.out("Type SET and an option: SET BRIEF ON, SET PAGED OFF, SET WIDTH 80, SET LINES 24\n")
		return
	}
	onOff := func(v string) (bool, bool) {
		switch v {
		case "ON", "YES", "Y":
			return true, true
		case "OFF", "NO", "N":
			return false, true
		}
		return false, false
	}
	switch opt, v := f[0], f[1]; {
	case strings.HasPrefix("BRIEF", opt) && len(opt) >= 3:
		b, ok := onOff(v)
		if !ok {
			s.out("BRIEF is ON or OFF\n")
			return
		}
		s.brief = b
	case strings.HasPrefix("PAGED", opt) && len(opt) >= 3:
		b, ok := onOff(v)
		if !ok {
			s.out("PAGED is ON or OFF\n")
			return
		}
		s.paged = b
		if b && s.lines == 0 {
			s.lines = 24
		}
	case strings.HasPrefix("WIDTH", opt) && len(opt) >= 3:
		n, err := strconv.Atoi(v)
		if err != nil || n < 40 || n > 132 {
			s.out("WIDTH is from 40 to 132\n")
			return
		}
		s.width = n
	case strings.HasPrefix("LINES", opt) && len(opt) >= 3:
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 200 {
			s.out("LINES is from 0 to 200\n")
			return
		}
		s.lines = n
	default:
		s.out(opt + " is not a terminal option\n")
		return
	}
	s.out("OK\n")
}
