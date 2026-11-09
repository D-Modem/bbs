// Package gemini serves the BBS by Gemini (gemini://, TLS, port 1965): the
// gopher server's capsule (what the system is, who is on, the message and
// file areas the sysop opens to it) and finger's users, as gemtext.  As
// with gopher, paths name records in the BBS's store, not files on disk.
package gemini

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/finger"
	"github.com/D-Modem/bbs/internal/gopher"
	"github.com/D-Modem/bbs/internal/sockets"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/tcpserv"
)

// Settings are the [gemini] section's.
type Settings struct {
	Address    string
	Host       string // the name bbs gemini cert offers for a certificate
	CertFile   string // the certificate and its key (PEM)
	KeyFile    string
	Areas      string // wildmat of the message areas shown ("": none)
	Files      bool   // file areas listed and downloadable
	Users      bool   // /users: finger's answer for a user
	MaxClients int
	MaxPerIP   int
}

// Server serves Gemini.
type Server struct {
	B *bbs.BBS
	S Settings

	g *gopher.Server // what is shown, and how
	f *finger.Server
}

// Gemini's status codes used here.
const (
	statusInput    = 10
	statusOK       = 20
	statusRedirect = 31
	statusBusy     = 41
	statusNotFound = 51
	statusNoProxy  = 53
	statusBad      = 59
)

// MaxRequest is the longest URL a request may have (bytes).
const MaxRequest = 1024

// Run listens until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	cert, err := LoadCert(s.S.CertFile, s.S.KeyFile)
	if err != nil {
		return fmt.Errorf("gemini: %w", err)
	}
	ln, err := sockets.Listen("tcp", s.S.Address)
	if err != nil {
		return fmt.Errorf("gemini: %w", err)
	}
	return s.Serve(ctx, ln, cert)
}

// Serve answers on ln, with TLS by cert, until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener, cert tls.Certificate) error {
	s.g = &gopher.Server{B: s.B, S: gopher.Settings{Areas: s.S.Areas, Files: s.S.Files}}
	s.f = &finger.Server{B: s.B, S: finger.Settings{Users: true}}
	tl := tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	ts := &tcpserv.Server{Name: "gemini", MaxClients: s.S.MaxClients, MaxPerIP: s.S.MaxPerIP,
		Refuse:  func(c net.Conn) bool { return s.B.Refuse("gemini", c.RemoteAddr()) },
		Timeout: 5 * time.Minute, Log: s.B.Log, Handle: s.handle,
		Busy: func(c net.Conn) { header(c, statusBusy, "Too busy; try again later.") }}
	return ts.Serve(ctx, tl)
}

// header sends a response's header line.
func header(c io.Writer, status int, meta string) {
	fmt.Fprintf(c, "%d %s\r\n", status, meta)
}

// page is a gemtext document being made.
type page struct{ strings.Builder }

// line adds a line of text, made safe: one line, valid UTF-8, and not
// taken for a link, heading or preformatting toggle.  Quotes (">") and
// list items ("* ") are let be: they mean the same in gemtext.
func (p *page) line(s string) {
	s = strings.ToValidUTF8(strings.TrimRight(s, "\r"), "?")
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	if strings.HasPrefix(s, "=>") || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "```") {
		s = " " + s
	}
	p.WriteString(s + "\n")
}

// heading adds a heading (level 1 to 3).
func (p *page) heading(level int, s string) {
	p.WriteString(strings.Repeat("#", level) + " " + one(s) + "\n")
}

// link adds a link line.
func (p *page) link(to, text string) {
	p.WriteString("=> " + to + " " + one(text) + "\n")
}

// pre adds lines as preformatted text (tables: who is on, a user).
func (p *page) pre(alt string, lines []string) {
	p.WriteString("```" + one(alt) + "\n")
	for _, l := range lines {
		l = strings.ToValidUTF8(strings.TrimRight(l, "\r"), "?")
		if strings.HasPrefix(l, "```") {
			l = " " + l
		}
		p.WriteString(l + "\n")
	}
	p.WriteString("```\n")
}

// one is text for a heading or link: one line, valid UTF-8.
func one(s string) string {
	s = strings.ToValidUTF8(s, "?")
	return strings.Join(strings.Fields(s), " ")
}

// send sends a gemtext page.
func (p *page) send(c io.Writer) {
	header(c, statusOK, "text/gemini; charset=utf-8")
	io.WriteString(c, p.String())
}

// handle answers one request: it reads the URL and sends what its path
// names, or 51 for anything not open to Gemini.
func (s *Server) handle(c net.Conn) {
	req, err := tcpserv.ReadLine(c, MaxRequest+1)
	if err != nil {
		if tc, ok := c.(*tls.Conn); ok && tc.ConnectionState().HandshakeComplete {
			header(c, statusBad, "Bad request")
		}
		return
	}
	if len(req) > MaxRequest || !utf8.ValidString(req) {
		header(c, statusBad, "Bad request")
		return
	}
	u, err := url.Parse(req)
	if err != nil || !u.IsAbs() || u.Host == "" || u.User != nil || u.Fragment != "" {
		header(c, statusBad, "Bad request")
		return
	}
	if !strings.EqualFold(u.Scheme, "gemini") {
		header(c, statusNoProxy, "Only gemini:// is served here")
		return
	}
	p := u.Path
	if p == "" {
		header(c, statusRedirect, "/")
		return
	}
	if p != "/" && strings.HasSuffix(p, "/") {
		header(c, statusRedirect, strings.TrimRight(p, "/"))
		return
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
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
	case p == "/":
		s.root(c)
	case p == "/about":
		s.about(c)
	case p == "/who":
		s.who(c)
	case p == "/users" && s.S.Users:
		s.user(c, u.RawQuery)
	case strings.HasPrefix(p, "/~") && len(parts) == 1 && s.S.Users:
		s.user(c, url.QueryEscape(parts[0][1:]))
	case p == "/areas" && s.S.Areas != "":
		s.areas(c)
	case parts[0] == "area" && (len(parts) == 2 || len(parts) == 3):
		if a := s.g.Area(id(1)); a != nil && id(2) >= 0 {
			s.messages(c, a, id(2))
			return
		}
		notFound(c)
	case parts[0] == "msg" && len(parts) == 2:
		s.message(c, id(1))
	case p == "/files" && s.S.Files:
		s.fileAreas(c)
	case parts[0] == "files" && len(parts) == 2 && s.S.Files:
		s.files(c, id(1))
	case parts[0] == "file" && (len(parts) == 2 || len(parts) == 3) && s.S.Files:
		s.download(c, id(1))
	default:
		notFound(c)
	}
}

func notFound(c io.Writer) { header(c, statusNotFound, "Not here") }

// root is the capsule's home: the system's name and where it is, about
// and who's online, users, and the areas where they are shown.
func (s *Server) root(c io.Writer) {
	var p page
	p.heading(1, s.B.Conf().Name)
	if s.B.Conf().Location != "" {
		p.line(s.B.Conf().Location)
	}
	p.line("")
	p.link("/about", "About this system")
	p.link("/who", "Who's online")
	if s.S.Users {
		p.link("/users", "Look up a user")
	}
	if s.S.Areas != "" {
		p.link("/areas", "Message areas")
	}
	if s.S.Files && s.B.Files != nil {
		p.link("/files", "File areas")
	}
	p.send(c)
}

// about is gopher's About this system.
func (s *Server) about(c io.Writer) {
	var p page
	p.heading(1, "About "+s.B.Conf().Name)
	for _, l := range s.g.About() {
		p.line(l)
	}
	p.line("")
	p.link("/", "Back")
	p.send(c)
}

// who is gopher's Who's online, as a table.
func (s *Server) who(c io.Writer) {
	var p page
	p.heading(1, "Who's online")
	w := s.g.Who()
	if len(w) == 1 {
		p.line(w[0])
	} else {
		p.pre("who is on", w)
	}
	p.line("")
	p.link("/", "Back")
	p.send(c)
}

// user asks for a name (an empty query), or tells finger's answer for it.
func (s *Server) user(c io.Writer, query string) {
	name, err := url.QueryUnescape(query)
	if err != nil {
		header(c, statusBad, "Bad request")
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		header(c, statusInput, "User name")
		return
	}
	var p page
	p.heading(1, name)
	p.pre("user", s.f.User(name))
	p.line("")
	p.link("/users", "Look up another")
	p.link("/", "Back")
	p.send(c)
}

// areas lists the message areas Gemini shows.
func (s *Server) areas(c io.Writer) {
	as, _ := s.B.Store.Areas()
	var p page
	p.heading(1, "Message areas of "+s.B.Conf().Name)
	for _, a := range as {
		if s.g.Area(a.ID) != nil {
			p.link(fmt.Sprintf("/area/%d", a.ID), fmt.Sprintf("%s (%d): %s", a.Name, a.Messages, a.Description))
		}
	}
	p.line("")
	p.link("/", "Back")
	p.send(c)
}

// messages lists an area's messages after one id, oldest first.
func (s *Server) messages(c io.Writer, a *store.Area, after int64) {
	ms, _ := s.B.Store.Messages(a.ID, after, gopher.PerPage)
	var p page
	p.heading(1, a.Name)
	if a.Description != "" {
		p.line(a.Description)
	}
	p.line("")
	for _, m := range ms {
		p.link(fmt.Sprintf("/msg/%d", m.ID), fmt.Sprintf("#%d %s (%s, %s)", m.ID, bbs.Trunc(m.Subject, 60),
			bbs.Trunc(m.From, 30), m.Created.Format("2006-01-02")))
	}
	if len(ms) == 0 {
		p.line("(no messages)")
	}
	if len(ms) == gopher.PerPage {
		p.link(fmt.Sprintf("/area/%d/%d", a.ID, ms[len(ms)-1].ID), "More...")
	}
	p.line("")
	p.link("/areas", "Message areas")
	p.send(c)
}

// message sends a message, its headers first, if its area is shown.
func (s *Server) message(c io.Writer, id int64) {
	if id < 1 {
		notFound(c)
		return
	}
	m, err := s.B.Store.Message(id)
	if err != nil {
		notFound(c)
		return
	}
	a := s.g.Area(m.AreaID)
	if a == nil {
		notFound(c)
		return
	}
	var p page
	p.heading(1, m.Subject)
	p.line("From: " + m.From)
	p.line("To: " + m.To)
	p.line("Date: " + m.Created.Format("2006-01-02 15:04"))
	p.line("")
	for _, l := range strings.Split(m.Body, "\n") {
		p.line(l)
	}
	p.line("")
	p.link(fmt.Sprintf("/area/%d", a.ID), "Back to "+a.Name)
	p.send(c)
}

// fileAreas lists the file areas anyone may download from.
func (s *Server) fileAreas(c io.Writer) {
	if s.B.Files == nil {
		notFound(c)
		return
	}
	fas, _ := s.B.Store.FileAreas()
	var p page
	p.heading(1, "File areas of "+s.B.Conf().Name)
	for _, fa := range fas {
		if gopher.OpenFiles(fa) {
			p.link(fmt.Sprintf("/files/%d", fa.ID), fmt.Sprintf("%s (%d): %s", fa.Name, fa.Files, fa.Description))
		}
	}
	p.line("")
	p.link("/", "Back")
	p.send(c)
}

// fileArea is the file area of the id, if Gemini shows it (nil: not).
func (s *Server) fileArea(id int64) *store.FileArea {
	if s.B.Files == nil || id < 1 {
		return nil
	}
	fa, err := s.B.Store.FileArea(id)
	if err != nil || !gopher.OpenFiles(fa) {
		return nil
	}
	return fa
}

// files lists a file area's files.  Links end in the file's name, so
// clients save it by that name.
func (s *Server) files(c io.Writer, id int64) {
	fa := s.fileArea(id)
	if fa == nil {
		notFound(c)
		return
	}
	fs, _ := s.B.Store.Files(fa.ID)
	var p page
	p.heading(1, fa.Name)
	if fa.Description != "" {
		p.line(fa.Description)
	}
	p.line("")
	for _, f := range fs {
		p.link(fmt.Sprintf("/file/%d/%s", f.ID, url.PathEscape(f.Name)),
			fmt.Sprintf("%s (%s): %s", f.Name, size(f.Size), bbs.Trunc(f.Description, 50)))
	}
	if len(fs) == 0 {
		p.line("(no files)")
	}
	p.line("")
	p.link("/files", "File areas")
	p.send(c)
}

// size is a file's size for people.
func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// mimeType is what a file is, by its name.
func mimeType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".nfo", ".diz", ".asc", ".ans":
		return "text/plain"
	case ".gmi":
		return "text/gemini"
	}
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// download sends a file's bytes as they are, if its area is Gemini's,
// counting a download if all of it went.
func (s *Server) download(c io.Writer, id int64) {
	if s.B.Files == nil || id < 1 {
		notFound(c)
		return
	}
	f, err := s.B.Store.FileByID(id)
	if err != nil || s.fileArea(f.AreaID) == nil {
		notFound(c)
		return
	}
	r, err := s.B.Files.OpenFile(f)
	if err != nil {
		notFound(c)
		return
	}
	defer r.Close()
	header(c, statusOK, mimeType(f.Name))
	if n, err := io.Copy(c, r); err == nil && n == f.Size {
		s.B.Store.CountDownload(f.ID)
	}
}
