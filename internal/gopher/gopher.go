// Package gopher serves the BBS by gopher (RFC 1436): what the system is,
// who is on, and the message areas and file areas the sysop opens to it.
// Nothing is reached but what the BBS's store has: selectors are not
// paths.
package gopher

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/news"
	"github.com/Neo-Desktop/bbs-sec/internal/sockets"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/tcpserv"
)

// Settings are the [gopher] section's.
type Settings struct {
	Address    string
	Host       string // the host name menus give
	Port       int    // and port
	Areas      string // wildmat of the message areas shown ("": none)
	Files      bool   // file areas listed and downloadable
	MaxClients int
	MaxPerIP   int
}

// Server serves gopher.
type Server struct {
	B *bbs.BBS
	S Settings
}

// PerPage is how many messages a menu lists.
const PerPage = 50

// Run listens until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	ln, err := sockets.Listen("tcp", s.S.Address)
	if err != nil {
		return fmt.Errorf("gopher: %w", err)
	}
	return s.Serve(ctx, ln)
}

// Serve answers on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	ts := &tcpserv.Server{Name: "gopher", MaxClients: s.S.MaxClients, MaxPerIP: s.S.MaxPerIP,
		Refuse:  func(c net.Conn) bool { return s.B.Refuse("gopher", c.RemoteAddr()) },
		Timeout: 5 * time.Minute, Log: s.B.Log, Handle: s.handle,
		Busy: func(c net.Conn) { c.Write([]byte("3Too busy; try again later.\t\terror.host\t1\r\n.\r\n")) }}
	return ts.Serve(ctx, ln)
}

// item is a menu line.
type item struct {
	kind     byte
	text     string
	selector string
}

// clean makes a menu's text one line with no tabs.
func clean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, s)
	return bbs.Trunc(s, max)
}

// menu sends a gopher menu: a line for each item, on this server's host and
// port (info and error lines on error.host), then the ending ".".
func (s *Server) menu(c io.Writer, items []item) {
	var b strings.Builder
	for _, it := range items {
		host, port := s.S.Host, s.S.Port
		sel := it.selector
		if it.kind == 'i' || it.kind == '3' {
			host, port, sel = "error.host", 1, ""
		}
		fmt.Fprintf(&b, "%c%s\t%s\t%s\t%d\r\n", it.kind, clean(it.text, 120), sel, host, port)
	}
	b.WriteString(".\r\n")
	c.Write([]byte(b.String()))
}

// text sends a text document: lines CR LF, a lone "." doubled, "." to end.
func text(c io.Writer, lines []string) {
	var b strings.Builder
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		if strings.HasPrefix(l, ".") {
			l = "." + l
		}
		b.WriteString(l + "\r\n")
	}
	b.WriteString(".\r\n")
	c.Write([]byte(b.String()))
}

// notFound sends a menu of one error line.
func notFound(s *Server, c io.Writer) {
	s.menu(c, []item{{'3', "Not here.", ""}})
}

// handle answers one request: it reads the selector (anything after a tab
// dropped) and sends what it names, or an error menu for anything not open
// to gopher.
func (s *Server) handle(c net.Conn) {
	sel, err := tcpserv.ReadLine(c, 255)
	if err != nil {
		return
	}
	// gopher+ and search requests carry more after a tab: not used here
	sel, _, _ = strings.Cut(sel, "\t")
	parts := strings.Split(strings.Trim(sel, "/"), "/")
	id := func(i int) int64 {
		if i >= len(parts) {
			return 0
		}
		n, err := strconv.ParseInt(parts[i], 10, 64)
		if err != nil || n < 1 {
			return -1
		}
		return n
	}
	switch {
	case sel == "" || sel == "/":
		s.root(c)
	case parts[0] == "about" && len(parts) == 1:
		text(c, s.about())
	case parts[0] == "who" && len(parts) == 1:
		text(c, s.who())
	case parts[0] == "areas" && len(parts) == 1 && s.S.Areas != "":
		s.areas(c)
	case parts[0] == "area" && (len(parts) == 2 || len(parts) == 3):
		if a := s.area(id(1)); a != nil && id(2) >= 0 {
			s.messages(c, a, id(2))
			return
		}
		notFound(s, c)
	case parts[0] == "msg" && len(parts) == 2:
		s.message(c, id(1))
	case parts[0] == "files" && len(parts) == 1 && s.S.Files:
		s.fileAreas(c)
	case parts[0] == "files" && len(parts) == 2 && s.S.Files:
		s.files(c, id(1))
	case parts[0] == "file" && len(parts) == 2 && s.S.Files:
		s.download(c, id(1))
	default:
		notFound(s, c)
	}
}

// root sends the top menu: the system's name and where it is, about and
// who's online, and the message and file areas where they are shown.
func (s *Server) root(c io.Writer) {
	items := []item{{'i', s.B.Conf().Name, ""}}
	if s.B.Conf().Location != "" {
		items = append(items, item{'i', s.B.Conf().Location, ""})
	}
	items = append(items, item{'i', "", ""},
		item{'0', "About this system", "/about"},
		item{'0', "Who's online", "/who"})
	if s.S.Areas != "" {
		items = append(items, item{'1', "Message areas", "/areas"})
	}
	if s.S.Files && s.B.Files != nil {
		items = append(items, item{'1', "File areas", "/files"})
	}
	s.menu(c, items)
}

// about is what the system is: its name, place, sysop, how many users, and
// the message of the day.
func (s *Server) about() []string {
	out := []string{s.B.Conf().Name}
	if s.B.Conf().Location != "" {
		out = append(out, s.B.Conf().Location)
	}
	out = append(out, "Sysop: "+s.B.Conf().Sysop, "")
	if us, err := s.B.Store.Users(); err == nil {
		out = append(out, fmt.Sprintf("%d users", len(us)))
	}
	if motd := s.B.MOTDText(); motd != "" {
		out = append(out, "")
		for _, l := range strings.Split(strings.TrimRight(motd, "\n"), "\n") {
			out = append(out, bbs.Clean(l, 200))
		}
	}
	return out
}

// who is the callers on now, a line each under a heading.
func (s *Server) who() []string {
	out := []string{fmt.Sprintf("%4s  %-24s %-12s %5s  %s", "Node", "User", "Via", "On", "Doing")}
	on := 0
	for _, n := range s.B.Nodes() {
		if n.User == "" {
			continue
		}
		on++
		out = append(out, fmt.Sprintf("%4d  %s %s %5s  %s", n.Num, bbs.Fit(n.User, 24),
			bbs.Fit(n.Via, 12), bbs.Since(n.Since), bbs.Trunc(n.Activity, 30)))
	}
	if on == 0 {
		return []string{"No one is on just now."}
	}
	return out
}

// open reports whether a message area is shown by gopher.
func (s *Server) open(a *store.Area) bool {
	return s.S.Areas != "" && news.Wildmat(strings.ToLower(s.S.Areas), strings.ToLower(a.Name))
}

// area is the message area of the id, if gopher shows it (nil: not).
func (s *Server) area(id int64) *store.Area {
	if id < 1 {
		return nil
	}
	a, err := s.B.Store.Area(id)
	if err != nil || !s.open(a) {
		return nil
	}
	return a
}

// areas sends the menu of the message areas gopher shows.
func (s *Server) areas(c io.Writer) {
	as, _ := s.B.Store.Areas()
	items := []item{{'i', "Message areas of " + s.B.Conf().Name, ""}, {'i', "", ""}}
	for _, a := range as {
		if s.open(a) {
			items = append(items, item{'1', fmt.Sprintf("%s (%d)  %s", a.Name, a.Messages, a.Description),
				fmt.Sprintf("/area/%d", a.ID)})
		}
	}
	s.menu(c, items)
}

// messages lists an area's messages after one id, oldest first.
func (s *Server) messages(c io.Writer, a *store.Area, after int64) {
	ms, _ := s.B.Store.Messages(a.ID, after, PerPage)
	items := []item{{'i', a.Name + ": " + a.Description, ""}, {'i', "", ""}}
	for _, m := range ms {
		items = append(items, item{'0', fmt.Sprintf("#%-5d %s  %s, %s", m.ID, bbs.Trunc(m.Subject, 40),
			bbs.Trunc(m.From, 20), m.Created.Format("2006-01-02")), fmt.Sprintf("/msg/%d", m.ID)})
	}
	if len(ms) == 0 {
		items = append(items, item{'i', "(no messages)", ""})
	}
	if len(ms) == PerPage {
		items = append(items, item{'1', "More...", fmt.Sprintf("/area/%d/%d", a.ID, ms[len(ms)-1].ID)})
	}
	s.menu(c, items)
}

// message sends a message, its headers first, if its area is shown.
func (s *Server) message(c io.Writer, id int64) {
	if id < 1 {
		notFound(s, c)
		return
	}
	m, err := s.B.Store.Message(id)
	if err != nil || s.area(m.AreaID) == nil {
		notFound(s, c)
		return
	}
	a := s.area(m.AreaID)
	out := []string{"Area:    " + a.Name, "From:    " + m.From, "To:      " + m.To,
		"Subject: " + m.Subject, "Date:    " + m.Created.Format("2006-01-02 15:04"), ""}
	out = append(out, strings.Split(m.Body, "\n")...)
	text(c, out)
}

// fileAreas sends the menu of every file area (all of them: download
// requirements are not looked at).
func (s *Server) fileAreas(c io.Writer) {
	if s.B.Files == nil {
		notFound(s, c)
		return
	}
	fas, _ := s.B.Store.FileAreas()
	items := []item{{'i', "File areas of " + s.B.Conf().Name, ""}, {'i', "", ""}}
	for _, fa := range fas {
		if openFiles(fa) {
			items = append(items, item{'1', fmt.Sprintf("%s (%d)  %s", fa.Name, fa.Files, fa.Description),
				fmt.Sprintf("/files/%d", fa.ID)})
		}
	}
	s.menu(c, items)
}

// openFiles reports whether a file area is gopher's: gopher has no login,
// so only an area anyone may download from (no download requirement).
func openFiles(fa *store.FileArea) bool {
	return bbs.MustRequire(fa.DownloadReq).Allows(nil)
}

// kind is a file's gopher type, by its name.
func kind(name string) byte {
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".gif"):
		return 'g'
	case strings.HasSuffix(n, ".png"), strings.HasSuffix(n, ".jpg"), strings.HasSuffix(n, ".jpeg"):
		return 'I'
	}
	return '9'
}

// files sends the menu of a file area's files, each typed by its name.
func (s *Server) files(c io.Writer, id int64) {
	if s.B.Files == nil || id < 1 {
		notFound(s, c)
		return
	}
	fa, err := s.B.Store.FileArea(id)
	if err != nil || !openFiles(fa) {
		notFound(s, c)
		return
	}
	fs, _ := s.B.Store.Files(fa.ID)
	items := []item{{'i', fa.Name + ": " + fa.Description, ""}, {'i', "", ""}}
	for _, f := range fs {
		items = append(items, item{kind(f.Name), fmt.Sprintf("%-24s %9d  %s", f.Name, f.Size,
			bbs.Trunc(f.Description, 40)), fmt.Sprintf("/file/%d", f.ID)})
	}
	if len(fs) == 0 {
		items = append(items, item{'i', "(no files)", ""})
	}
	s.menu(c, items)
}

// download sends a file's bytes as they are, if its area is gopher's,
// counting a download if all of it went.
func (s *Server) download(c io.Writer, id int64) {
	if s.B.Files == nil || id < 1 {
		notFound(s, c)
		return
	}
	f, err := s.B.Store.FileByID(id)
	if err != nil {
		notFound(s, c)
		return
	}
	if fa, err := s.B.Store.FileArea(f.AreaID); err != nil || !openFiles(fa) {
		notFound(s, c)
		return
	}
	r, err := s.B.Files.OpenFile(f)
	if err != nil {
		notFound(s, c)
		return
	}
	defer r.Close()
	if n, err := io.Copy(c, r); err == nil && n == f.Size {
		s.B.Store.CountDownload(f.ID)
	}
}
