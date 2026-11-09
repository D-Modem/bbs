package cis

// Forums, libraries and Mail in CIM, as pages: WinCIM 3.0.2 asks for them
// with ordinary page requests (CIS:FORUMS, HOM:MAILHMI) and shows menus
// and articles, as 24high/CompuServe-Emulator measured it (POST.md).  The
// pages here:
//
//	CIS:FORUMS              the forums
//	CIS:<GO>                a forum: its sections and libraries
//	CIS:<GO>/S<n>           a section's messages, newest first
//	CIS:<GO>/M<number>      a message (read: it counts as read)
//	CIS:<GO>/L<n>           a library's files
//	CIS:MAIL                the mailbox, newest first
//	CIS:MAIL/<id>           a letter (read: it is marked read)
//
// Item kind 0 is a selectable item; kind 1 is drawn as a label the client
// will not select, so every item here is kind 0.

import (
	"fmt"
	"strconv"
	"strings"
)

// hmiList is how many messages a section's page lists: the item count is
// a byte.
const hmiList = 200

// forumPage is a forum: one item for each section, then each library.
func (s *session) forumPage(f *Forum) *Page {
	p := &Page{ID: f.Go, Title: f.Name}
	for i, a := range f.Sections {
		p.Items = append(p.Items, Item{
			Text:   fmt.Sprintf("Messages: %s (%d)", sectionName(a), a.Messages),
			Target: fmt.Sprintf("%s/S%d", f.Go, i),
		})
	}
	for i, l := range f.Libs {
		p.Items = append(p.Items, Item{
			Text:   "Library: " + l.Name,
			Target: fmt.Sprintf("%s/L%d", f.Go, i),
		})
	}
	if len(p.Items) == 0 {
		p.Text = []string{" " + f.Name + " has nothing in it yet."}
	}
	return p
}

// forumPart is one of a forum's pages below it: "S0", "M1234", "L2".
func (s *session) forumPart(f *Forum, part string) *Page {
	if len(part) < 2 {
		return nil
	}
	n, err := strconv.ParseInt(part[1:], 10, 64)
	if err != nil || n < 0 {
		return nil
	}
	st := &forumState{f: f, read: map[int64]int64{}}
	switch part[0] {
	case 'S':
		if n >= int64(len(f.Sections)) {
			return nil
		}
		a := f.Sections[n]
		p := &Page{ID: fmt.Sprintf("%s/S%d", f.Go, n), Title: f.Name}
		ms, _ := s.b.Store.Messages(a.ID, 0, 5000)
		for i := len(ms) - 1; i >= 0 && len(p.Items) < hmiList; i-- {
			m := ms[i]
			p.Items = append(p.Items, Item{
				Text:   fmt.Sprintf("%s  (%s)  #%d", m.Subject, m.From, m.ID),
				Target: fmt.Sprintf("%s/M%d", f.Go, m.ID),
			})
		}
		if len(p.Items) == 0 {
			p.Text = []string{" There are no messages in " + sectionName(a) + "."}
		}
		return p
	case 'M':
		m, err := s.b.Store.Message(n)
		if err != nil || f.section(m.AreaID) < 0 {
			return nil
		}
		replies := s.replies(m)
		lines := append(s.header(st, m, replies), "")
		body := strings.Split(strings.TrimRight(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n"), "\n")
		lines = append(lines, body...)
		if r := replyLine(replies); r != nil {
			lines = append(append(lines, ""), r...)
		}
		if lr, _ := s.b.Store.LastRead(s.user.ID, m.AreaID); m.ID > lr {
			s.b.Store.SetLastRead(s.user.ID, m.AreaID, m.ID)
		}
		return &Page{ID: fmt.Sprintf("%s/M%d", f.Go, m.ID), Title: m.Subject, Text: lines}
	case 'L':
		if n >= int64(len(f.Libs)) {
			return nil
		}
		l := f.Libs[n]
		p := &Page{ID: fmt.Sprintf("%s/L%d", f.Go, n), Title: l.Name,
			Text: []string{" " + l.Name, ""}}
		fs := s.libFiles(l, libQuery{})
		for _, file := range fs {
			p.Text = append(p.Text, fmt.Sprintf(" %-12s %8d  %s  %s", file.Name, file.Size,
				file.Created.Format("02-Jan-06"), file.Description))
		}
		if len(fs) == 0 {
			p.Text = append(p.Text, " There are no files here.")
		}
		p.Text = append(p.Text, "", " Downloads are not in CIM here yet: a terminal program",
			" downloads from this library with GO "+f.Go+", then LIB "+strconv.FormatInt(n, 10)+".")
		return p
	}
	return nil
}

// mailPage is the mailbox, or one letter of it ("MAIL/<id>").
func (s *session) mailPage(id string) *Page {
	all, err := s.b.Store.Inbox(s.user.ID)
	if err != nil {
		s.b.Log.Printf("node %d: CompuServe: mail: %v", s.node.Num, err)
	}
	if id != "" {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return nil
		}
		for _, m := range all {
			if m.ID == n {
				s.b.Store.MarkMailRead(s.user.ID, m.ID)
				return &Page{ID: "MAIL/" + id, Title: m.Subject, Text: mailText(m)}
			}
		}
		return nil
	}
	p := &Page{ID: "MAIL", Title: "CompuServe Mail"}
	for i := len(all) - 1; i >= 0 && len(p.Items) < hmiList; i-- {
		m := all[i]
		mark := ""
		if !m.Read {
			mark = "* "
		}
		p.Items = append(p.Items, Item{
			Text:   fmt.Sprintf("%s%s  (%s)", mark, m.Subject, fromLine(m)),
			Target: "MAIL/" + strconv.FormatInt(m.ID, 10),
		})
	}
	if len(p.Items) == 0 {
		p.Text = []string{" No messages pending."}
	}
	return p
}
