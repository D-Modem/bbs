package cis

// CIM's service: the Host-Micro Interface (HMI) over the B Plus Transport
// Layer, as WinCIM 3.0.2 speaks it (recovered by 24high/CompuServe-Emulator,
// HMI-PROTOKOLL.md, and checked there against the real client).  After the
// login and ESC I:
//
//	H→C  text with "CompuServe" in it (the banner)
//	H→C  ESC [ > 0 q                 C→H  "#1,2,3,6,8,10" CR (its modes)
//	H→C  ESC [ > 3 ; 3 p             C→H  "#3,2,1,4,2,0;614" CR
//	H→C  ACK                          (NAK: the line's sum was wrong)
//	...  the Transport Layer (xfer.BTransport), then HMI messages:
//	     <stream> <type> <fields>
//
// Fields are bytes, words and 24-bit numbers (low byte first), varints (a
// byte, or b1|0x80 and b2 for b2<<7 | b1&0x7F), NUL-ended text (never
// empty where the client needs one: WinCIM crashes on a null name) and DOS
// dates.  What this serves: the session's opening, the pages (menus as
// 0x27, articles as 0x28 and their text in pieces), the rest of a menu's
// items (0x34), Find (0x22), the directory lookup (0x0D), acks for the
// rest, and the logoff.  Forums, their libraries' lists and Mail to read
// are pages too (hmiforum.go); the CB in CIM, downloads and sending mail
// are still to come.

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/xfer"
)

// HMI message types
const (
	hmiAck       = 0x01
	hmiText      = 0x04 // a piece of an article's text
	hmiLogoff    = 0x07
	hmiLookup    = 0x0D
	hmiLookupAns = 0x0E
	hmiOpen      = 0x20
	hmiOpenAns   = 0x21
	hmiFind      = 0x22
	hmiFindAns   = 0x23
	hmiGet       = 0x24
	hmiSelect    = 0x25
	hmiPage      = 0x27
	hmiArticle   = 0x28
	hmiTextFirst = 0x29
	hmiTextNext  = 0x2A
	hmiTextEnd   = 0x2B
	hmiGetItems  = 0x34
	hmiItems     = 0x35
)

// hmiBatch is the most items a 0x35 carries: the client asks again only
// when it gets this many.
const hmiBatch = 15

// hmiPiece is the most text a 0x04 carries (WinCIM asks for 1500; it must
// fit a packet).
const hmiPiece = 700

// hmiW builds a message's fields.
type hmiW []byte

func (w hmiW) byte(v int) hmiW   { return append(w, byte(v)) }
func (w hmiW) word(v int) hmiW   { return append(w, byte(v), byte(v>>8)) }
func (w hmiW) triple(v int) hmiW { return append(w, byte(v), byte(v>>8), byte(v>>16)) }
func (w hmiW) varint(v int) hmiW {
	if v < 0x80 {
		return append(w, byte(v))
	}
	return append(w, byte(v)|0x80, byte(v>>7))
}

// text is s in Latin-1, NUL-ended; "" is a space, never a null name.
func (w hmiW) text(s string) hmiW {
	if s == "" {
		s = " "
	}
	for _, r := range s {
		if r > 0xFF || r == 0 {
			r = '?'
		}
		w = append(w, byte(r))
	}
	return append(w, 0)
}

// date is t as DOS's date and time words.
func (w hmiW) date(t time.Time) hmiW {
	y := max(t.Year()-1980, 0)
	return w.word(t.Day() | int(t.Month())<<5 | (y&0x7F)<<9).
		word(t.Second()/2 | t.Minute()<<5 | t.Hour()<<11)
}

// hmiR reads a message's fields; past the end it gives zeros.
type hmiR struct {
	b []byte
	i int
}

func (r *hmiR) byte() int {
	if r.i >= len(r.b) {
		return 0
	}
	r.i++
	return int(r.b[r.i-1])
}
func (r *hmiR) word() int { return r.byte() | r.byte()<<8 }
func (r *hmiR) varint() int {
	b := r.byte()
	if b&0x80 == 0 {
		return b
	}
	return r.byte()<<7 | b&0x7F
}
func (r *hmiR) text() string {
	j := r.i
	for j < len(r.b) && r.b[j] != 0 {
		j++
	}
	s := make([]rune, 0, j-r.i)
	for _, c := range r.b[r.i:j] {
		s = append(s, rune(c))
	}
	r.i = min(j+1, len(r.b))
	return string(s)
}

// hmi is a CIM's session, from the banner to its logoff.
func (s *session) hmi() error {
	s.node.Doing("CompuServe: CIM")
	s.banner()
	mode, params, err := s.negotiate()
	if err != nil {
		return err
	}
	s.b.Log.Printf("node %d: CompuServe: %s's CIM (%s) in mode %d (%s)", s.node.Num, s.user.Name, s.caps.Raw, mode, params)
	t, err := xfer.NewBTransport(port{s}, 5*time.Second)
	if err != nil {
		return fmt.Errorf("cis: CIM's Transport Layer: %w", err)
	}
	h := &hmiSession{s: s, t: t}
	return h.run()
}

// negotiate is the terminal mode's trade after the login: the modes the
// client has, the one chosen, its parameters and their sum.
func (s *session) negotiate() (mode int, params string, err error) {
	s.out("\x1b[>0q")
	modes, err := s.hashLine(8 * time.Second)
	if err != nil {
		return 0, "", err
	}
	mode = pickMode(modes)
	if mode < 0 {
		return 0, "", fmt.Errorf("cis: no CIM mode in %q", modes)
	}
	for try := 0; try < 3; try++ {
		s.outf("\x1b[>%d;%dp", mode, mode)
		params, err = s.hashLine(10 * time.Second)
		if err != nil {
			return 0, "", err
		}
		if paramsOK(params) {
			s.line.Write([]byte{0x06})
			return mode, params, nil
		}
		s.line.Write([]byte{0x15})
	}
	return 0, "", fmt.Errorf("cis: CIM's parameters %q: their sum is wrong", params)
}

// hashLine reads the client's next line that begins with '#', within d.
// The CR LF after its Secure Login's last block comes first, so a line
// without the '#' is passed over.
func (s *session) hashLine(d time.Duration) (string, error) {
	deadline := time.Now().Add(d)
	var b []byte
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return "", errHangup
		}
		c, err := s.getc(left)
		if errors.Is(err, errIdle) {
			return "", errHangup
		}
		if err != nil {
			return "", err
		}
		switch {
		case c == '\r' || c == '\n':
			if len(b) > 0 && b[0] == '#' {
				s.cr = c == '\r'
				return string(b), nil
			}
			b = b[:0]
		case c >= 0x20 && len(b) < 200:
			b = append(b, c)
		}
	}
}

// pickMode is the mode to use of the client's "#1,2,3,6,8,10": 3, the one
// WinCIM 3.0.2 is known to work in, if it is there.
func pickMode(line string) int {
	var have []int
	for _, f := range strings.Split(strings.TrimPrefix(line, "#"), ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil {
			have = append(have, n)
		}
	}
	for _, want := range []int{3, 2, 1, 6, 8, 10} {
		for _, n := range have {
			if n == want {
				return n
			}
		}
	}
	if len(have) > 0 {
		return have[0]
	}
	return -1
}

// paramsOK checks "#3,2,1,4,2,0;614": the number is the sum of the
// characters through the ';'.
func paramsOK(line string) bool {
	i := strings.LastIndexByte(line, ';')
	if i < 0 {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(line[i+1:]))
	if err != nil {
		return false
	}
	sum := 0
	for j := 0; j <= i; j++ {
		sum += int(line[j])
	}
	return sum&0xFFFF == n
}

// hmiSession is the HMI's state: the article being fetched.
type hmiSession struct {
	s       *session
	t       *xfer.BTransport
	settled bool // the first message has gone (twice)
	text    []byte
	// delay is the wait before each answer, as the emulator that was
	// checked against WinCIM waits.
	delay time.Duration
}

func (h *hmiSession) run() error {
	s := h.s
	if h.delay == 0 {
		h.delay = 120 * time.Millisecond
	}
	idle := s.b.Conf().IdleTimeout
	if idle <= 0 {
		idle = 5 * time.Minute
	}
	for {
		m, err := h.t.Recv(idle)
		if errors.Is(err, xfer.ErrTimeout) {
			s.b.Log.Printf("node %d: CompuServe: %s's CIM went quiet", s.node.Num, s.user.Name)
			return errHangup
		}
		if errors.Is(err, xfer.ErrTLFailed) {
			return errHangup
		}
		if err != nil {
			return err
		}
		if len(m) < 2 {
			continue
		}
		stream, typ := int(m[0]), m[1]
		if typ == hmiLogoff {
			s.b.Log.Printf("node %d: CompuServe: %s's CIM logged off", s.node.Num, s.user.Name)
			return errHangup
		}
		reply := h.answer(stream, typ, &hmiR{b: m[2:]})
		if reply == nil {
			continue
		}
		time.Sleep(h.delay)
		if err := h.t.Send(reply); err != nil {
			return err
		}
		if !h.settled {
			// WinCIM drops the first message after the Transport Layer's
			// start, however long the wait: it is sent again, once (more
			// would be taken for the answers to come)
			h.settled = true
			time.Sleep(300 * time.Millisecond)
			if err := h.t.Send(reply); err != nil {
				return err
			}
		}
	}
}

// answer is the reply to a message: stream, type, fields.
func (h *hmiSession) answer(stream int, typ byte, r *hmiR) []byte {
	msg := func(t byte, w hmiW) []byte { return append([]byte{byte(stream), t}, w...) }
	switch typ {
	case hmiOpen:
		r.word()
		p := h.lookup(r.text())
		return msg(hmiOpenAns, hmiW{}.word(0).date(h.s.now()).
			text("CompuServe Information Service").text("CIS").text(p.Title).varint(0))
	case hmiGet:
		return h.deliver(stream, h.lookup(r.text()))
	case hmiSelect:
		p := h.lookup(r.text())
		i := r.varint()
		if i >= 1 && i <= len(p.Items) {
			p = h.lookup(p.Items[i-1].Target)
		}
		return h.deliver(stream, p)
	case hmiGetItems:
		p := h.lookup(r.text())
		r.byte()
		from := r.varint()
		var w hmiW
		n := 0
		for i := max(from-1, 0); i < len(p.Items) && n < hmiBatch; i++ {
			w = w.byte(i + 1).byte(0).text(p.Items[i].Text)
			n++
		}
		return msg(hmiItems, append(hmiW{}.byte(n), w...))
	case hmiTextFirst:
		p := h.lookup(r.text())
		r.word()
		h.text = articleText(p)
		return h.piece(stream, hmiTextFirst, r.word())
	case hmiTextNext:
		return h.piece(stream, hmiTextNext, r.word())
	case hmiFind:
		r.byte()
		var w hmiW
		n := 0
		for _, p := range h.s.pages.Find(r.text()) {
			if n == 12 { // one message, in one packet
				break
			}
			w = w.text(p.Title + " - " + p.ID).text(hmiName(p))
			n++
		}
		return msg(hmiFindAns, append(hmiW{}.byte(n), w...))
	case hmiLookup:
		if r.byte()&0x80 != 0 {
			r.word()
		}
		// an empty record for each name asked for
		return msg(hmiLookupAns, make(hmiW, r.byte()))
	}
	// the keep-alive (0x0F), the session request (0x0B) and the rest: an
	// ack, always on stream 1 (the only one open from the start)
	return []byte{1, hmiAck, typ}
}

// deliver is a page as the client takes it: a menu's description, or an
// article's header (the client then fetches its text).
func (h *hmiSession) deliver(stream int, p *Page) []byte {
	if !p.Menu() {
		h.text = articleText(p)
		return append([]byte{byte(stream), hmiArticle},
			hmiW{}.text(hmiName(p)).triple(pageNumber(p)).byte(0).word(0)...)
	}
	n := min(len(p.Items), 255)
	w := hmiW{}.byte(0).text(hmiName(p)).triple(pageNumber(p)).byte(n).
		varint(0).word(0).byte(0).byte(0)
	for i, it := range p.Items[:n] {
		w = w.byte(i + 1).byte(0).text(it.Text)
	}
	return append([]byte{byte(stream), hmiPage}, w...)
}

// piece is the next piece of the article being fetched, or its end.
func (h *hmiSession) piece(stream int, asked byte, most int) []byte {
	if len(h.text) == 0 {
		h.text = nil
		return []byte{byte(stream), hmiTextEnd, 0}
	}
	if most <= 0 || most > hmiPiece {
		most = hmiPiece
	}
	n := min(most, len(h.text))
	part := h.text[:n]
	h.text = h.text[n:]
	w := append(hmiW{}.byte(int(asked)), part...)
	return append([]byte{byte(stream), hmiText}, append(w, 0)...)
}

// articleText is a page's text as WinCIM's text window wants it: Latin-1,
// lines joined by CR LF, each starting with a space (a line that does not
// is run on to the one before).
func articleText(p *Page) []byte {
	lines := p.Text
	if p.Menu() {
		lines = append(append([]string{}, lines...), "")
		for i, it := range p.Items {
			lines = append(lines, fmt.Sprintf("%2d %s", i+1, it.Text))
		}
	}
	if len(lines) == 0 {
		lines = []string{p.Title}
	}
	var b hmiW
	for i, l := range lines {
		if i > 0 {
			b = append(b, '\r', '\n')
		}
		if !strings.HasPrefix(l, " ") {
			b = append(b, ' ')
		}
		b = b.text(l)
		b = b[:len(b)-1] // its NUL
	}
	return b
}

// hmiName is a page's name in HMI: CIS: and its ID.
func hmiName(p *Page) string { return "CIS:" + p.ID }

// pageNumber is a page's number: a hash of its ID, never 0.
func pageNumber(p *Page) int {
	f := fnv.New32a()
	f.Write([]byte(p.ID))
	n := int(f.Sum32() & 0xFFFFFF)
	if n == 0 {
		n = 1
	}
	return n
}

// hmiAliases are the names WinCIM's buttons and GO box ask for that are a
// page of the service's under another ID: CIS:NEWPAGE is What's New,
// CIS:CONTENTS the Table of Contents, HOM:MAILHMI the mail (GO MAIL in
// the box).
var hmiAliases = map[string]string{
	"NEWPAGE":  "NEW",
	"CONTENTS": "TOP",
	"MAILHMI":  "MAIL",
	"HOME":     "TOP",
}

// lookup is the page a name stands for: "CIS:TOP", "HOM:FORUMS", a GO
// word, a forum's or the mailbox's own pages (hmiforum.go).  A name that
// is none gets a page saying so: the client must never be left without
// one.  Pages are made afresh each time, so they show what is there now.
func (h *hmiSession) lookup(name string) *Page {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		name = "CIS:TOP"
	}
	w := name
	if i := strings.IndexByte(w, ':'); i >= 0 && i <= 3 {
		w = w[i+1:]
	}
	if a, ok := hmiAliases[w]; ok && h.s.pages.Get(w) == nil {
		w = a
	}
	p := h.page(w)
	if p == nil {
		p = &Page{ID: w, Title: "CompuServe", Text: []string{
			" " + w + " is not a service here.",
			"",
			" GO TOP, or Find, leads to what there is.",
		}}
	}
	if p.ID == "" {
		p.ID = "TOP"
	}
	if p.Title == "" {
		p.Title = "CompuServe"
	}
	return p
}

// page is the page w names, or nil.
func (h *hmiSession) page(w string) *Page {
	s := h.s
	word, part, _ := strings.Cut(w, "/")
	if word == "MAIL" || word == "EASYPLEX" {
		return s.mailPage(part)
	}
	if f := s.forum(word); f != nil && s.pages.Get(word) == nil {
		if part != "" {
			return s.forumPart(f, part)
		}
		return s.forumPage(f)
	}
	if part != "" {
		return nil
	}
	p := s.pages.Get(w)
	if p == nil {
		return nil
	}
	switch p.ID {
	case "FORUMS":
		p = s.forumsPage()
	case "MAIL":
		return s.mailPage("")
	case "CB":
		p = &Page{ID: p.ID, Title: p.Title, Text: []string{
			" The CB Simulator is not in CIM here yet: a terminal program",
			" reaches it with GO CB.",
		}}
	}
	return p
}
