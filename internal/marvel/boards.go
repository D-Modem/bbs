package marvel

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// The "BBS" service is MSN's bulletin-board reader (BBSNAV): it lists a
// board's messages, opens one as a news article, takes new posts and
// replies, and deletes the caller's own.  A board is a BBS message area.
//
// Its node ids: (0, area) is the board, (message, area) a message.  All
// of a board's messages are its direct children; a reply names what it
// answers in _P.
type boards struct {
	c       *conn
	uploads map[byte]*upload
	next    byte
	streams map[byte]*stream
}

// the BBS service's interfaces: the tree reads and edits, as DIRSRV's, and
// the message channel
const ifaceMessage = 0x0B

// message channel methods
const (
	msgArticle    = 0x00
	msgPostStart  = 0x02
	msgPostAppend = 0x03
	msgPostCommit = 0x04
	msgPostAbort  = 0x07
)

// _F, a board node's flags
const (
	boardMSN          = 0x0800 // an MSN board (not a newsgroup); format 0: plain text
	boardNoChildren   = 0x1000
	boardReadOnly     = 0x2000
	boardNoAttachment = 0x4000
)

// replyBudget is the most a reply may hold: a pipe message's length is a
// 16-bit number, and the pipe's header and the host block's ride along.
const replyBudget = 60000

func newBoards(c *conn) *boards {
	return &boards{c: c, uploads: map[byte]*upload{}, next: 1, streams: map[byte]*stream{}}
}

func (s *boards) ifaces() []iface {
	return []iface{mosIID(0x27, ifaceTree), mosIID(0x28, ifaceEdit), mosIID(0x2F, ifaceMessage)}
}

func (s *boards) call(h hostBlock) ([]byte, bool) {
	if h.oneWay() {
		s.chunk(h)
		return nil, false
	}
	ps, _ := parseParams(h.Params)
	switch h.Iface {
	case ifaceEdit:
		return editCall(s.c, h.Method, ps, true)
	case ifaceMessage:
		switch h.Method {
		case msgArticle:
			return s.article(ps), true
		case msgPostStart:
			return s.postStart(ps), true
		case msgPostAppend, msgPostCommit:
			return s.postMore(ps, h.Method == msgPostCommit), true
		case msgPostAbort:
			for _, p := range ps {
				if p.Tag == 0x01 {
					delete(s.uploads, byte(p.Int))
				}
			}
			return reply{}.endStatic(), true
		}
	default:
		switch h.Method {
		case treeGetProperties, treeGetChildren:
			r := parseTreeRequest(ps)
			if h.Method == treeGetProperties {
				var recs [][]byte
				for _, k := range r.nodes {
					recs = append(recs, s.record(r.props, k))
				}
				return treeReply(recs), true
			}
			return treeReply(s.children(r)), true
		case treeGoWord:
			return goWordReply(s.c.tree(), ps), true
		case treeGetShabby:
			return shabbyReply(ps), true
		}
	}
	s.c.logf("BBS: no method %#x.%#x", h.Iface, h.Method)
	return nil, false
}

// area is the area with id if the caller may read it.
func (c *conn) area(id uint32) *store.Area {
	u := c.signedOn()
	a, err := c.s.B.Store.Area(int64(id))
	if err != nil || !c.s.B.CanRead(u, a) {
		return nil
	}
	return a
}

// message is a message of a readable area.
func (c *conn) message(k nodeKey) (*store.Area, *store.Message) {
	a := c.area(k.b)
	if a == nil || k.a == 0 {
		return a, nil
	}
	m, err := c.s.B.Store.Message(int64(k.a))
	if err != nil || m.AreaID != a.ID {
		return a, nil
	}
	return a, m
}

func (s *boards) record(names []string, k nodeKey) []byte {
	a, m := s.c.message(k)
	if a == nil {
		t := s.c.tree()
		return record(t.dirProps(names, t.get(keyNowhere), false))
	}
	if m == nil {
		return record(s.boardProps(names, a, false))
	}
	parent := uint32(0)
	if m.ReplyTo > 0 {
		if p, err := s.c.s.B.Store.Message(m.ReplyTo); err == nil && p.AreaID == a.ID {
			parent = uint32(p.ID)
		}
	}
	return record(s.messageProps(names, a, m, parent, false))
}

// children is a board's messages, the newest that fit in a reply, oldest
// first.
func (s *boards) children(r treeRequest) [][]byte {
	k := r.nodes[0]
	a := s.c.area(k.b)
	if a == nil || k.a != 0 {
		t := s.c.tree()
		return [][]byte{record(t.dirProps(r.props, t.get(keyNowhere), true))}
	}
	var head [][]byte
	if r.flags == 1 {
		head = append(head, record(s.boardProps(r.props, a, true)))
	}
	// newest first; a few hundred is all a reply holds
	msgs, err := s.c.s.B.Store.Newest(a.ID, 1000)
	if err != nil {
		s.c.logf("BBS: %v", err)
	}
	ids := map[int64]bool{}
	for _, m := range msgs {
		ids[m.ID] = true
	}
	size := 32
	for _, h := range head {
		size += len(h)
	}
	var recs [][]byte
	for _, m := range msgs {
		parent := uint32(0)
		if ids[m.ReplyTo] {
			parent = uint32(m.ReplyTo)
		}
		rec := record(s.messageProps(r.props, a, m, parent, true))
		if size+len(rec) > replyBudget {
			break
		}
		size += len(rec)
		recs = append(recs, rec)
	}
	for i, j := 0, len(recs)-1; i < j; i, j = i+1, j-1 {
		recs[i], recs[j] = recs[j], recs[i]
	}
	return append(head, recs...)
}

func (s *boards) flags(a *store.Area) uint16 {
	f := uint16(boardMSN | boardNoAttachment)
	if !s.c.s.B.CanPost(s.c.signedOn(), a) {
		f |= boardReadOnly
	}
	return f
}

func (s *boards) boardProps(names []string, a *store.Area, list bool) []prop {
	n := boardNode(a)
	var last time.Time
	if m, err := s.c.s.B.Store.LastMessage(a.ID); err == nil {
		last = m.Created
	}
	n.modified = last
	t := s.c.tree()
	out := make([]prop, 0, len(names))
	for _, name := range names {
		var p prop
		switch name {
		case "b":
			p = prop{ptByte, name, u8(0)}
		case "_a":
			p = prop{ptAnsi, name, sz(s.c.s.B.Conf().Sysop)}
		case "_D":
			p = prop{ptDword, name, u32(unixTime(last))}
		case "_F":
			p = prop{ptWord, name, u16(s.flags(a))}
		default:
			p = bbsCommon(name, func() prop { return t.dirProp(name, n, list) })
		}
		out = append(out, p)
	}
	return out
}

func (s *boards) messageProps(names []string, a *store.Area, m *store.Message, parent uint32, list bool) []prop {
	n := &node{key: nodeKey{uint32(m.ID), uint32(a.ID)}, name: m.Subject, app: appBBS, modified: m.Created,
		gen: uint32(m.ID)}
	out := make([]prop, 0, len(names))
	for _, name := range names {
		var p prop
		switch name {
		case "b":
			p = prop{ptByte, name, u8(flagLeaf)}
		case "_a":
			p = prop{ptAnsi, name, sz(m.From)}
		case "p":
			p = prop{ptDword, name, u32(uint32(len(m.Body)))}
		case "_D":
			p = prop{ptDword, name, u32(unixTime(m.Created))}
		case "_P":
			p = prop{ptDword, name, u32(parent)}
		case "_F":
			p = prop{ptWord, name, u16(s.flags(a) | boardNoChildren)}
		case "tp":
			p = prop{ptWide, name, sz("Message")}
			if list {
				p.typ = ptAnsi
			}
		case "on":
			p = prop{ptWide, name, sz(m.From)}
		default:
			p = bbsCommon(name, func() prop {
				var t tree
				return t.dirProp(name, n, list)
			})
		}
		out = append(out, p)
	}
	return out
}

// bbsCommon is a property every BBS node answers alike, or the directory's.
func bbsCommon(name string, dir func() prop) prop {
	switch name {
	case "c":
		return prop{ptDword, name, u32(appBBS)}
	case "_P", "_r", "_f":
		return prop{ptDword, name, u32(0)}
	case "_t":
		return prop{ptByte, name, u8(0)}
	case "_I":
		return prop{ptWord, name, u16(0)}
	case "q":
		// here a dword array, which the Properties dialog shows as a
		// language's name
		return prop{ptDwords, name, append(u32(1), u32(lcidEnglishUS)...)}
	}
	return dir()
}

func unixTime(t time.Time) uint32 {
	if t.IsZero() {
		return 0
	}
	return uint32(t.Unix())
}

// article is a message as the reader wants it: news headers, a blank
// line, the body.
func (s *boards) article(ps []param) []byte {
	var k nodeKey
	for _, p := range ps {
		if p.Tag == 0x04 {
			k, _ = keyOf(p.Data)
			break
		}
	}
	a, m := s.c.message(k)
	if m == nil {
		// a status but 0 the reader takes as a broken connection; an
		// article saying so is kinder
		m = &store.Message{Subject: "(no such message)", Body: "This message is not here any more.", Created: time.Now()}
		if a == nil {
			a = &store.Area{Name: "BBS"}
		}
	}
	return reply{}.dword(0).blob(s.articleBytes(a, m))
}

func (s *boards) articleBytes(a *store.Area, m *store.Message) []byte {
	body := ansi(strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n"))
	id := m.MessageID
	if id == "" {
		id = fmt.Sprintf("<%d.%d@bbs>", m.ID, m.AreaID)
	}
	var b bytes.Buffer
	for _, h := range [][2]string{
		{"Path", "msn"},
		{"From", m.From},
		{"Newsgroups", a.Name},
		{"Subject", m.Subject},
		{"Date", m.Created.Format(time.RFC1123Z)},
		{"Message-ID", id},
		{"X-MOS-Format", "TEXT"},
		{"X-MOS-Size", strconv.Itoa(len(body))},
		{"X-MOS-Attach", "0"},
	} {
		b.Write(ansi(h[0] + ": " + strings.Map(noBreaks, h[1]) + "\n"))
	}
	b.WriteByte('\n')
	if room := replyBudget - b.Len(); len(body) > room {
		body = append(body[:room-40:room-40], "\r\n[the rest is too long to show]\r\n"...)
	}
	b.Write(body)
	return b.Bytes()
}

func noBreaks(r rune) rune {
	if r == '\r' || r == '\n' {
		return ' '
	}
	return r
}

// A post arrives in pieces: method 2 opens it (where it goes, its size, the
// first piece) and is answered with a handle; 3 adds a piece, 4 adds the
// last.  A piece may be a reference to a stream of one-way chunks, which
// can still be arriving when the commit comes.
type upload struct {
	parent, area uint32
	total        int
	pieces       []piece
	committed    bool
}

type piece struct {
	data   []byte
	ref    bool
	stream byte
}

type stream struct {
	data []byte
	done bool
}

func pieceOf(p param) piece {
	if p.Tag == 0x05 {
		return piece{ref: true, stream: p.Stream}
	}
	return piece{data: append([]byte(nil), p.Data...)}
}

func (s *boards) postStart(ps []param) []byte {
	var fields []param
	var total uint32
	gotTotal := false
	for _, p := range ps {
		switch p.Tag {
		case 0x04, 0x05:
			fields = append(fields, p)
		case 0x03:
			if !gotTotal {
				total, gotTotal = p.Int, true
			}
		}
	}
	if len(fields) < 3 || !gotTotal || fields[0].Tag != 0x04 {
		return reply{}.byte_(0).endStatic()
	}
	k, _ := keyOf(fields[0].Data)
	h := s.next
	if s.next++; s.next == 0 {
		s.next = 1
	}
	s.uploads[h] = &upload{parent: k.a, area: k.b, total: int(total), pieces: []piece{pieceOf(fields[2])}}
	return reply{}.byte_(h).endStatic()
}

func (s *boards) postMore(ps []param, commit bool) []byte {
	var h byte
	var pc *piece
	for _, p := range ps {
		switch p.Tag {
		case 0x01:
			h = byte(p.Int)
		case 0x04, 0x05:
			if pc == nil {
				x := pieceOf(p)
				pc = &x
			}
		}
	}
	u := s.uploads[h]
	if u == nil {
		return reply{}.byte_(0).endStatic()
	}
	if pc != nil {
		u.pieces = append(u.pieces, *pc)
	}
	if !commit {
		return reply{}.byte_(1).endStatic()
	}
	u.committed = true
	if !s.ready(u) {
		return reply{}.byte_(1).endStatic() // stored when its streams end
	}
	delete(s.uploads, h)
	if err := s.store(u); err != nil {
		s.c.logf("post: %v", err)
		return reply{}.byte_(0).endStatic()
	}
	return reply{}.byte_(1).endStatic()
}

func (s *boards) ready(u *upload) bool {
	for _, p := range u.pieces {
		if st := s.streams[p.stream]; p.ref && (st == nil || !st.done) {
			return false
		}
	}
	return true
}

// chunk takes a one-way frame of a stream; a stream's last may complete a
// committed post.
func (s *boards) chunk(h hostBlock) {
	st := s.streams[h.Method]
	if st == nil {
		st = &stream{}
		s.streams[h.Method] = st
	}
	if len(st.data) < 1<<20 {
		st.data = append(st.data, h.Params...)
	}
	if h.Iface != classChunkLast {
		return
	}
	st.done = true
	for id, u := range s.uploads {
		if u.committed && s.ready(u) {
			delete(s.uploads, id)
			if err := s.store(u); err != nil {
				s.c.logf("post: %v", err)
			}
		}
	}
}

// store posts a finished upload.
func (s *boards) store(u *upload) error {
	var art []byte
	for _, p := range u.pieces {
		if p.ref {
			art = append(art, s.streams[p.stream].data...)
			delete(s.streams, p.stream)
		} else {
			art = append(art, p.data...)
		}
	}
	if u.total > 0 && u.total < len(art) {
		art = art[:u.total] // attachments follow: this board takes none
	}
	head, body, _ := bytes.Cut(art, []byte("\n\n"))
	hdr := map[string]string{}
	for _, l := range strings.Split(unansi(head), "\n") {
		if k, v, ok := strings.Cut(l, ": "); ok {
			hdr[strings.ToLower(k)] = strings.TrimSpace(v)
		}
	}
	text, err := bodyText(hdr["x-mos-format"], body)
	if err != nil {
		return err
	}
	parent := u.parent
	if v, err := strconv.ParseUint(hdr["x-mos-parent"], 10, 32); err == nil {
		parent = uint32(v)
	}
	a := s.c.area(u.area)
	if a == nil {
		return fmt.Errorf("no area %d to post in", u.area)
	}
	s.c.mu.Lock()
	n := s.c.node
	s.c.mu.Unlock()
	if n == nil || s.c.signedOn() == nil {
		return fmt.Errorf("not signed on")
	}
	to := "All"
	var replyTo int64
	if parent != 0 {
		if m, err := s.c.s.B.Store.Message(int64(parent)); err == nil && m.AreaID == a.ID {
			to, replyTo = m.From, m.ID
		}
	}
	subject := hdr["subject"]
	if subject == "" {
		subject = "(No subject)"
	}
	_, err = n.Post(a, to, subject, strings.Split(text, "\n"), replyTo)
	return err
}

// bodyText is an uploaded body as plain text.
func bodyText(format string, body []byte) (string, error) {
	switch strings.ToUpper(format) {
	case "RTFCOMP":
		raw, err := unlzfu(body)
		if err != nil {
			return "", err
		}
		body = raw
		fallthrough
	case "RTF":
		return strings.TrimRight(rtfText(body), "\n"), nil
	}
	return strings.ReplaceAll(unansi(body), "\r\n", "\n"), nil
}

// deleteMessage is the reader's delete: a ticket, then the message's id.
func (c *conn) deleteMessage(ps []param) uint32 {
	var vars [][]byte
	for _, p := range ps {
		if p.Tag == 0x04 {
			vars = append(vars, p.Data)
		}
	}
	if len(vars) != 2 {
		return refused
	}
	k, ok := keyOf(vars[1])
	if !ok {
		return refused
	}
	_, m := c.message(k)
	c.mu.Lock()
	n := c.node
	c.mu.Unlock()
	if m == nil || n == nil {
		return refused
	}
	if err := n.DeleteMessage(m); err != nil {
		c.logf("delete: %v", err)
		return refused
	}
	return 0
}
