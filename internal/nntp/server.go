package nntp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/news"
	"github.com/Neo-Desktop/bbs-sec/internal/sockets"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// TLS modes of the server.
const (
	TLSNo       = "no"       // none: passwords in the clear
	TLSOptional = "optional" // STARTTLS offered
	TLSRequired = "required" // STARTTLS before AUTHINFO and POST
	TLSImplicit = "implicit" // TLS from the first byte (port 563)
)

// ServerSettings are [nntp]'s.
type ServerSettings struct {
	Address     string
	TLS         string
	CertFile    string
	KeyFile     string
	ReadUsers   bool // only logged-in users (and peers) read
	Post        bool // users may post
	MaxClients  int
	MaxPerIP    int
	IdleTimeout time.Duration
}

// Server is the NNTP server.
type Server struct {
	Svc   *Service
	Set   ServerSettings
	Peers []*Peer // those whose addresses may IHAVE (and read)

	tlsConfig *tls.Config
	mu        sync.Mutex
	clients   int
	perIP     map[string]int
	conns     map[net.Conn]bool
}

// NewServer makes the server, loading the certificate if TLS is on.
func NewServer(svc *Service, set ServerSettings, peers []*Peer) (*Server, error) {
	s := &Server{Svc: svc, Set: set, Peers: peers, perIP: map[string]int{}, conns: map[net.Conn]bool{}}
	if set.TLS != TLSNo {
		cert, err := tls.LoadX509KeyPair(set.CertFile, set.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("nntp: certificate: %w", err)
		}
		s.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}
	return s, nil
}

// Run serves until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	ln, err := sockets.Listen("tcp", s.Set.Address)
	if err != nil {
		return fmt.Errorf("nntp: %w", err)
	}
	s.Svc.B.Log.Printf("nntp: listening on %s (tls %s)", s.Set.Address, s.Set.TLS)
	go func() {
		<-ctx.Done()
		ln.Close()
		s.mu.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.mu.Unlock()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go s.serve(c)
	}
}

func hostOf(a net.Addr) string {
	if t, ok := a.(*net.TCPAddr); ok {
		return t.IP.String()
	}
	return ""
}

func (s *Server) admit(c net.Conn) (string, bool) {
	if s.Svc.B.Refuse("nntp", c.RemoteAddr()) {
		return "502 Not here", false
	}
	h := hostOf(c.RemoteAddr())
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Set.MaxClients > 0 && s.clients >= s.Set.MaxClients {
		return "400 Too many readers; try again later", false
	}
	if s.Set.MaxPerIP > 0 && s.perIP[h] >= s.Set.MaxPerIP {
		return "400 Too many connections from your address", false
	}
	s.clients++
	s.perIP[h]++
	s.conns[c] = true
	return "", true
}

func (s *Server) release(c net.Conn) {
	h := hostOf(c.RemoteAddr())
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients--
	if s.perIP[h]--; s.perIP[h] <= 0 {
		delete(s.perIP, h)
	}
	delete(s.conns, c)
}

// peerAt is the peer whose address c comes from, if any.
func (s *Server) peerAt(c net.Conn) *Peer {
	ip := net.ParseIP(hostOf(c.RemoteAddr()))
	for _, p := range s.Peers {
		for _, a := range p.AcceptFrom {
			if a.Equal(ip) {
				return p
			}
		}
	}
	return nil
}

// session is a client's connection.
type session struct {
	s     *Server
	c     net.Conn
	r     *bufio.Reader
	w     *bufio.Writer
	tls   bool
	who   string
	user  *store.User
	name  string // AUTHINFO USER, waiting for PASS
	peer  *Peer
	group *store.Area
	cur   int64
}

const maxLine = 512 // RFC 3977: a command line, CRLF included

func (s *Server) serve(c net.Conn) {
	defer c.Close()
	defer func() {
		if r := recover(); r != nil {
			s.Svc.B.Log.Printf("nntp: panic: %v", r)
		}
	}()
	if msg, ok := s.admit(c); !ok {
		c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		io.WriteString(c, msg+"\r\n")
		return
	}
	defer s.release(c)
	ss := &session{s: s, c: c, peer: s.peerAt(c), who: "nntp " + c.RemoteAddr().String()}
	if s.Set.TLS == TLSImplicit {
		tc := tls.Server(c, s.tlsConfig)
		tc.SetDeadline(time.Now().Add(30 * time.Second))
		if err := tc.Handshake(); err != nil {
			return
		}
		tc.SetDeadline(time.Time{})
		ss.c, ss.tls = tc, true
	}
	ss.r = bufio.NewReaderSize(ss.c, 4096)
	ss.w = bufio.NewWriterSize(ss.c, 16384)
	if ss.canPost() {
		ss.reply(200, s.Svc.B.Conf().Name+" news server ready, posting allowed")
	} else {
		ss.reply(201, s.Svc.B.Conf().Name+" news server ready, no posting")
	}
	for {
		ss.w.Flush()
		line, err := ss.readLine()
		if err != nil {
			if errors.Is(err, errLineTooLong) {
				ss.reply(501, "Command line too long")
				continue
			}
			return
		}
		cmd, args := splitCommand(line)
		if cmd == "QUIT" {
			ss.reply(205, "Goodbye")
			ss.w.Flush()
			return
		}
		ss.command(cmd, args)
	}
}

var errLineTooLong = errors.New("line too long")

func (ss *session) readLine() (string, error) {
	ss.c.SetReadDeadline(time.Now().Add(ss.s.Set.IdleTimeout))
	defer ss.c.SetReadDeadline(time.Time{})
	var line []byte
	for {
		chunk, isPrefix, err := ss.r.ReadLine()
		if err != nil {
			return "", err
		}
		line = append(line, chunk...)
		if len(line) > maxLine {
			for isPrefix {
				if _, isPrefix, err = ss.r.ReadLine(); err != nil {
					return "", err
				}
			}
			return "", errLineTooLong
		}
		if !isPrefix {
			return string(line), nil
		}
	}
}

func splitCommand(line string) (string, []string) {
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", nil
	}
	return strings.ToUpper(f[0]), f[1:]
}

func (ss *session) reply(code int, msg string) {
	fmt.Fprintf(ss.w, "%d %s\r\n", code, msg)
}

// lines sends a multi-line block, dot-stuffed and ended by ".".
func (ss *session) lines(data []byte) {
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		var line []byte
		if i < 0 {
			line, data = data, nil
		} else {
			line, data = data[:i], data[i+1:]
		}
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) > 0 && line[0] == '.' {
			ss.w.WriteByte('.')
		}
		ss.w.Write(line)
		ss.w.WriteString("\r\n")
	}
}

func (ss *session) end() { ss.w.WriteString(".\r\n") }

func (ss *session) canPost() bool { return ss.s.Set.Post }

// mayRead: reading needs a login, unless anyone may (peers always may)
func (ss *session) mayRead() bool {
	return !ss.s.Set.ReadUsers || ss.user != nil || ss.peer != nil
}

func (ss *session) command(cmd string, args []string) {
	switch cmd {
	case "CAPABILITIES":
		ss.capabilities()
		return
	case "MODE":
		if len(args) == 1 && strings.EqualFold(args[0], "READER") {
			if ss.canPost() {
				ss.reply(200, "Posting allowed")
			} else {
				ss.reply(201, "Posting prohibited")
			}
			return
		}
		ss.reply(501, "Unknown MODE")
		return
	case "DATE":
		ss.reply(111, time.Now().UTC().Format("20060102150405"))
		return
	case "HELP":
		ss.reply(100, "Help text follows")
		ss.w.WriteString("ARTICLE BODY CAPABILITIES DATE GROUP HDR HEAD HELP IHAVE LAST LIST LISTGROUP\r\n" +
			"MODE NEWGROUPS NEWNEWS NEXT OVER POST QUIT STAT AUTHINFO STARTTLS\r\n")
		ss.end()
		return
	case "STARTTLS":
		ss.starttls()
		return
	case "AUTHINFO":
		ss.authinfo(args)
		return
	}
	if !ss.mayRead() {
		ss.reply(480, "Authentication required")
		return
	}
	switch cmd {
	case "LIST":
		ss.list(args)
	case "GROUP":
		ss.groupCmd(args, false)
	case "LISTGROUP":
		ss.groupCmd(args, true)
	case "NEXT", "LAST":
		ss.nextLast(cmd == "NEXT")
	case "ARTICLE", "HEAD", "BODY", "STAT":
		ss.article(cmd, args)
	case "OVER", "XOVER":
		ss.over(args)
	case "HDR", "XHDR":
		ss.hdr(args)
	case "NEWGROUPS":
		ss.reply(231, "List of new newsgroups follows")
		ss.end()
	case "NEWNEWS":
		ss.newnews(args)
	case "POST":
		ss.post()
	case "IHAVE":
		ss.ihave(args)
	default:
		ss.reply(500, "Unknown command")
	}
}

func (ss *session) capabilities() {
	ss.reply(101, "Capability list:")
	caps := []string{"VERSION 2", "READER", "LIST ACTIVE NEWSGROUPS OVERVIEW.FMT", "OVER", "HDR",
		"NEWNEWS", "IMPLEMENTATION bbs-sec"}
	if ss.canPost() {
		caps = append(caps, "POST")
	}
	if ss.peer != nil {
		caps = append(caps, "IHAVE")
	}
	if ss.user == nil && (ss.tls || ss.s.Set.TLS != TLSRequired) {
		caps = append(caps, "AUTHINFO USER")
	}
	if !ss.tls && (ss.s.Set.TLS == TLSOptional || ss.s.Set.TLS == TLSRequired) {
		caps = append(caps, "STARTTLS")
	}
	for _, c := range caps {
		ss.w.WriteString(c + "\r\n")
	}
	ss.end()
}

func (ss *session) starttls() {
	switch {
	case ss.tls:
		ss.reply(502, "Already using TLS")
		return
	case ss.s.tlsConfig == nil || ss.s.Set.TLS == TLSImplicit:
		ss.reply(580, "TLS not available")
		return
	case ss.user != nil:
		ss.reply(502, "Already authenticated")
		return
	}
	ss.reply(382, "Continue with TLS negotiation")
	ss.w.Flush()
	if ss.r.Buffered() > 0 {
		// nothing may come before the handshake: an injection attempt
		ss.c.Close()
		return
	}
	tc := tls.Server(ss.c, ss.s.tlsConfig)
	tc.SetDeadline(time.Now().Add(30 * time.Second))
	if err := tc.Handshake(); err != nil {
		ss.c.Close()
		return
	}
	tc.SetDeadline(time.Time{})
	ss.c, ss.tls = tc, true
	ss.r = bufio.NewReaderSize(tc, 4096)
	ss.w = bufio.NewWriterSize(tc, 16384)
	ss.name, ss.group, ss.cur = "", nil, 0
}

func (ss *session) authinfo(args []string) {
	if len(args) < 2 {
		ss.reply(501, "AUTHINFO USER name, then AUTHINFO PASS password")
		return
	}
	if ss.user != nil {
		ss.reply(502, "Already authenticated")
		return
	}
	if !ss.tls && ss.s.Set.TLS == TLSRequired {
		ss.reply(483, "Use STARTTLS first")
		return
	}
	switch strings.ToUpper(args[0]) {
	case "USER":
		ss.name = strings.Join(args[1:], " ")
		ss.reply(381, "Password required")
	case "PASS":
		if ss.name == "" {
			ss.reply(482, "AUTHINFO USER first")
			return
		}
		source := ""
		if a, ok := ss.c.RemoteAddr().(*net.TCPAddr); ok && !a.IP.IsLoopback() {
			source = "ip:" + a.IP.String()
		}
		u, err := ss.s.Svc.B.Authenticate(ss.who, source, ss.name, strings.Join(args[1:], " "))
		ss.name = ""
		if err != nil {
			ss.reply(481, "Authentication failed")
			return
		}
		ss.user = u
		ss.s.Svc.B.Log.Printf("%s: %s logged in", ss.who, u.Name)
		ss.reply(281, "Authentication accepted")
	default:
		ss.reply(501, "AUTHINFO USER or PASS")
	}
}

// groups are the newsgroups (areas that are) this reader may see
func (ss *session) groups() []*store.Area {
	gs, err := ss.s.Svc.B.Store.Newsgroups()
	if err != nil {
		return nil
	}
	var out []*store.Area
	for _, a := range gs {
		if ss.visible(a) {
			out = append(out, a)
		}
	}
	return out
}

// visible: a peer sees every newsgroup; a reader those their access lets
// them read; a reader not logged in those that need nothing
func (ss *session) visible(a *store.Area) bool {
	switch {
	case ss.peer != nil:
		return true
	case ss.user != nil:
		return ss.s.Svc.B.CanRead(ss.user, a)
	}
	return a.ReadReq == ""
}

func status(a *store.Area, post bool) string {
	if !post || a.SysopOnly {
		return "n"
	}
	return "y"
}

func (ss *session) list(args []string) {
	kw := "ACTIVE"
	if len(args) > 0 {
		kw = strings.ToUpper(args[0])
	}
	pattern := "*"
	if len(args) > 1 {
		pattern = args[1]
	}
	switch kw {
	case "ACTIVE":
		ss.reply(215, "List of newsgroups follows")
		for _, a := range ss.groups() {
			if news.Wildmat(pattern, a.Newsgroup) {
				hi, lo := a.LastID, a.FirstID
				if a.Messages == 0 {
					hi, lo = 0, 1
				}
				fmt.Fprintf(ss.w, "%s %d %d %s\r\n", a.Newsgroup, hi, lo, status(a, ss.canPost()))
			}
		}
		ss.end()
	case "NEWSGROUPS":
		ss.reply(215, "Descriptions follow")
		for _, a := range ss.groups() {
			if news.Wildmat(pattern, a.Newsgroup) {
				fmt.Fprintf(ss.w, "%s\t%s\r\n", a.Newsgroup, oneLine(a.Description))
			}
		}
		ss.end()
	case "OVERVIEW.FMT":
		ss.reply(215, "Order of fields in overview database")
		ss.w.WriteString("Subject:\r\nFrom:\r\nDate:\r\nMessage-ID:\r\nReferences:\r\n:bytes\r\n:lines\r\n")
		ss.end()
	default:
		ss.reply(503, "That LIST is not here")
	}
}

func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\r' || r == '\n' || r == 0 {
			return ' '
		}
		return r
	}, s)
}

func (ss *session) groupCmd(args []string, list bool) {
	if len(args) == 0 {
		if !list || ss.group == nil {
			ss.reply(412, "No newsgroup selected")
			return
		}
	} else {
		a, err := ss.s.Svc.B.Store.AreaByNewsgroup(args[0])
		if err != nil || !ss.visible(a) {
			ss.reply(411, "No such newsgroup")
			return
		}
		ss.group = a
	}
	a := ss.group
	ss.cur = a.FirstID
	hi, lo := a.LastID, a.FirstID
	if a.Messages == 0 {
		hi, lo, ss.cur = 0, 1, 0
	}
	ss.reply(211, fmt.Sprintf("%d %d %d %s", a.Messages, lo, hi, a.Newsgroup))
	if !list {
		return
	}
	from, to := lo, hi
	if len(args) > 1 {
		var ok bool
		if from, to, ok = parseRange(args[1], hi); !ok {
			// the 211 has gone; the list is empty
			from, to = 1, 0
		}
	}
	for from <= to {
		ms, err := ss.s.Svc.B.Store.Articles(a.ID, from, to, 1000)
		if err != nil || len(ms) == 0 {
			break
		}
		for _, m := range ms {
			fmt.Fprintf(ss.w, "%d\r\n", m.ID)
		}
		from = ms[len(ms)-1].ID + 1
	}
	ss.end()
}

// parseRange reads "n", "n-" or "n-m".
func parseRange(r string, hi int64) (int64, int64, bool) {
	a, b, dash := strings.Cut(r, "-")
	lo, err := strconv.ParseInt(a, 10, 64)
	if err != nil || lo < 0 {
		return 0, 0, false
	}
	if !dash {
		return lo, lo, true
	}
	if b == "" {
		return lo, hi, true
	}
	up, err := strconv.ParseInt(b, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return lo, up, true
}

func (ss *session) nextLast(next bool) {
	if ss.group == nil {
		ss.reply(412, "No newsgroup selected")
		return
	}
	if ss.cur == 0 {
		ss.reply(420, "No current article")
		return
	}
	m, err := ss.s.Svc.B.Store.Neighbour(ss.group.ID, ss.cur, next)
	for err == nil && m.MessageID == "" {
		m, err = ss.s.Svc.B.Store.Neighbour(ss.group.ID, m.ID, next)
	}
	if err != nil {
		if next {
			ss.reply(421, "No next article")
		} else {
			ss.reply(422, "No previous article")
		}
		return
	}
	ss.cur = m.ID
	ss.reply(223, fmt.Sprintf("%d %s", m.ID, m.MessageID))
}

// selectArticle finds the article an ARTICLE-like command names: by
// Message-ID (number 0), by number in the group, or the current one.
func (ss *session) selectArticle(args []string) (*store.Message, int64, bool) {
	st := ss.s.Svc.B.Store
	if len(args) > 0 && strings.HasPrefix(args[0], "<") {
		m, err := st.MessageByMessageID(args[0])
		if err == nil {
			if a, err := st.Area(m.AreaID); err == nil && a.Newsgroup != "" && ss.visible(a) {
				return m, 0, true
			}
		}
		ss.reply(430, "No article with that message-id")
		return nil, 0, false
	}
	if ss.group == nil {
		ss.reply(412, "No newsgroup selected")
		return nil, 0, false
	}
	n := ss.cur
	if len(args) > 0 {
		v, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			ss.reply(501, "Bad article number")
			return nil, 0, false
		}
		n = v
	} else if n == 0 {
		ss.reply(420, "No current article")
		return nil, 0, false
	}
	m, err := st.Message(n)
	if err != nil || m.AreaID != ss.group.ID || m.MessageID == "" {
		if len(args) == 0 {
			ss.reply(420, "Current article number is invalid")
		} else {
			ss.reply(423, "No article with that number")
		}
		return nil, 0, false
	}
	ss.cur = n
	return m, n, true
}

func (ss *session) article(cmd string, args []string) {
	m, n, ok := ss.selectArticle(args)
	if !ok {
		return
	}
	head := fmt.Sprintf("%d %s", n, m.MessageID)
	if cmd == "STAT" {
		ss.reply(223, head)
		return
	}
	raw, err := ss.s.Svc.B.Store.Raw(m.ID)
	if err != nil {
		ss.reply(403, "Cannot read the article")
		return
	}
	a, err := news.Parse(raw, len(raw)+1)
	if err != nil {
		ss.reply(403, "Cannot read the article")
		return
	}
	switch cmd {
	case "ARTICLE":
		ss.reply(220, head)
		ss.lines(a.HeaderBytes())
		ss.w.WriteString("\r\n")
		ss.lines(a.Body)
	case "HEAD":
		ss.reply(221, head)
		ss.lines(a.HeaderBytes())
	case "BODY":
		ss.reply(222, head)
		ss.lines(a.Body)
	}
	ss.end()
}

// overview is an article's overview fields.
func overview(raw []byte) (map[string]string, int, int) {
	a, err := news.Parse(raw, len(raw)+1)
	if err != nil {
		return map[string]string{}, len(raw), 0
	}
	f := map[string]string{}
	for _, h := range []string{"Subject", "From", "Date", "Message-ID", "References", "Xref", "Newsgroups"} {
		f[strings.ToLower(h)] = oneLine(a.Get(h))
	}
	return f, len(raw), bytes.Count(a.Body, []byte("\n"))
}

// selectRange finds the articles OVER and HDR name: one by Message-ID,
// a range of the group, or the current article.  byID: numbers are 0.
func (ss *session) selectRange(args []string) ([]*store.Message, bool, bool) {
	st := ss.s.Svc.B.Store
	if len(args) > 0 && strings.HasPrefix(args[0], "<") {
		m, err := st.MessageByMessageID(args[0])
		if err == nil {
			if a, err2 := st.Area(m.AreaID); err2 != nil || !ss.visible(a) {
				err = store.ErrNotFound
			}
		}
		if err != nil || m.MessageID == "" {
			ss.reply(430, "No article with that message-id")
			return nil, false, false
		}
		return []*store.Message{m}, true, true
	}
	if ss.group == nil {
		ss.reply(412, "No newsgroup selected")
		return nil, false, false
	}
	lo, hi := ss.cur, ss.cur
	if len(args) > 0 {
		var ok bool
		if lo, hi, ok = parseRange(args[0], ss.group.LastID); !ok {
			ss.reply(501, "Bad range")
			return nil, false, false
		}
	} else if ss.cur == 0 {
		ss.reply(420, "No current article")
		return nil, false, false
	}
	ms, err := st.Articles(ss.group.ID, lo, hi, 10000)
	if err != nil {
		ss.reply(403, "Cannot read the overview")
		return nil, false, false
	}
	if len(ms) == 0 {
		ss.reply(423, "No articles in that range")
		return nil, false, false
	}
	return ms, false, true
}

func (ss *session) over(args []string) {
	ms, byID, ok := ss.selectRange(args)
	if !ok {
		return
	}
	ss.reply(224, "Overview information follows")
	for _, m := range ms {
		raw, _ := ss.s.Svc.B.Store.Raw(m.ID)
		f, size, lines := overview(raw)
		n := m.ID
		if byID {
			n = 0
		}
		fmt.Fprintf(ss.w, "%d\t%s\t%s\t%s\t%s\t%s\t%d\t%d\r\n", n, f["subject"], f["from"], f["date"],
			f["message-id"], f["references"], size, lines)
	}
	ss.end()
}

func (ss *session) hdr(args []string) {
	if len(args) == 0 {
		ss.reply(501, "HDR field [range|message-id]")
		return
	}
	field := strings.ToLower(args[0])
	ms, byID, ok := ss.selectRange(args[1:])
	if !ok {
		return
	}
	ss.reply(225, "Headers follow")
	for _, m := range ms {
		raw, _ := ss.s.Svc.B.Store.Raw(m.ID)
		f, size, lines := overview(raw)
		var v string
		switch field {
		case ":bytes":
			v = strconv.Itoa(size)
		case ":lines":
			v = strconv.Itoa(lines)
		default:
			if a, err := news.Parse(raw, len(raw)+1); err == nil {
				v = oneLine(a.Get(field))
			} else {
				v = f[field]
			}
		}
		n := m.ID
		if byID {
			n = 0
		}
		fmt.Fprintf(ss.w, "%d %s\r\n", n, v)
	}
	ss.end()
}

func (ss *session) newnews(args []string) {
	if len(args) < 3 {
		ss.reply(501, "NEWNEWS wildmat date time [GMT]")
		return
	}
	since, ok := parseDateTime(args[1], args[2])
	if !ok {
		ss.reply(501, "Bad date or time")
		return
	}
	var ids []int64
	for _, a := range ss.groups() {
		if news.Wildmat(args[0], a.Newsgroup) {
			ids = append(ids, a.ID)
		}
	}
	list, err := ss.s.Svc.B.Store.ArticlesSince(since, ids, 100000)
	if err != nil {
		ss.reply(403, "Cannot list")
		return
	}
	ss.reply(230, "List of new articles follows")
	for _, id := range list {
		ss.w.WriteString(id + "\r\n")
	}
	ss.end()
}

// parseDateTime reads NEWNEWS's yyyymmdd (or yymmdd) hhmmss, in UTC.
func parseDateTime(d, t string) (time.Time, bool) {
	layout := "20060102 150405"
	if len(d) == 6 {
		layout = "060102 150405"
	}
	v, err := time.Parse(layout, d+" "+t)
	return v, err == nil
}

// readArticle reads a dot-terminated article of at most max bytes; one
// too large is read to its end and refused.
func (ss *session) readArticle(max int) ([]byte, error) {
	var b bytes.Buffer
	tooBig := false
	atStart := true // at the start of a line: where dots are stuffed
	for {
		ss.c.SetReadDeadline(time.Now().Add(ss.s.Set.IdleTimeout))
		line, err := ss.r.ReadSlice('\n')
		ss.c.SetReadDeadline(time.Time{})
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
		if atStart && (bytes.Equal(line, []byte(".\r\n")) || bytes.Equal(line, []byte(".\n"))) {
			if tooBig {
				return nil, news.ErrTooLarge
			}
			return b.Bytes(), nil
		}
		if atStart && len(line) > 0 && line[0] == '.' {
			line = line[1:]
		}
		atStart = err == nil // a whole line ended in LF
		if b.Len()+len(line) > max {
			tooBig = true
			continue
		}
		b.Write(line)
	}
}

func (ss *session) post() {
	if !ss.canPost() {
		ss.reply(440, "Posting not permitted")
		return
	}
	if ss.user == nil {
		ss.reply(480, "Authentication required to post")
		return
	}
	if !ss.tls && ss.s.Set.TLS == TLSRequired {
		ss.reply(483, "Use STARTTLS first")
		return
	}
	ss.reply(340, "Send article to be posted")
	ss.w.Flush()
	raw, err := ss.readArticle(ss.s.Svc.MaxArticle)
	if err != nil {
		if errors.Is(err, news.ErrTooLarge) {
			ss.reply(441, "Article too large")
			return
		}
		ss.c.Close()
		return
	}
	m, err := ss.s.Svc.PostLocal(raw, ss.user)
	if err != nil {
		ss.reply(441, "Posting failed: "+oneLine(err.Error()))
		return
	}
	ss.s.Svc.B.Log.Printf("%s: %s posted %s", ss.who, ss.user.Name, m.MessageID)
	ss.reply(240, "Article received "+m.MessageID)
}

func (ss *session) ihave(args []string) {
	if ss.peer == nil {
		ss.reply(502, "IHAVE is for peers")
		return
	}
	if len(args) != 1 {
		ss.reply(501, "IHAVE message-id")
		return
	}
	id := args[0]
	want, err := ss.s.Svc.Wanted(id)
	if err != nil {
		ss.reply(436, "Transfer not possible; try again later")
		return
	}
	if !want {
		ss.reply(435, "Article not wanted")
		return
	}
	ss.reply(335, "Send it; end with <CR-LF>.<CR-LF>")
	ss.w.Flush()
	raw, err := ss.readArticle(ss.s.Svc.MaxArticle)
	if err != nil {
		if errors.Is(err, news.ErrTooLarge) {
			ss.s.Svc.B.Store.Remember(id)
			ss.reply(437, "Article too large")
			return
		}
		ss.c.Close()
		return
	}
	if _, err := ss.s.Svc.Accept(raw, id, ss.peer.Name); err != nil {
		switch {
		case errors.Is(err, ErrHave):
			ss.reply(435, "Duplicate")
		case errors.Is(err, news.ErrMalformed), errors.Is(err, ErrNotHere), errors.Is(err, ErrLoop),
			errors.Is(err, news.ErrTooLarge):
			ss.reply(437, "Rejected: "+oneLine(err.Error()))
		default:
			ss.s.Svc.B.Log.Printf("%s: IHAVE %s: %v", ss.who, id, err)
			ss.reply(436, "Transfer failed; try again later")
		}
		return
	}
	ss.reply(235, "Article transferred OK")
}
