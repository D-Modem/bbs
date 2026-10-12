// Package qwk reads and writes QWK mail packets, the offline readers'
// format: a BBSID.QWK (ZIP) of CONTROL.DAT, MESSAGES.DAT (128-byte blocks)
// and index files going to the reader, a BBSID.REP of BBSID.MSG coming
// back.  Text is CP437, its lines parted by 0xE3; names and subjects
// longer than a header holds go as QWKE kludge lines ("Subject: ...") at
// the head of the text.
package qwk

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const (
	block   = 128
	lineEnd = 0xE3 // a line's end in QWK text
	active  = 0xE1
	killed  = 0xE2
)

// Message is a message in a packet.
type Message struct {
	Conf    int // conference: 0 private mail, else the area's number
	Number  int // its number on the BBS (in a REP: 0)
	Private bool
	Read    bool
	To      string
	From    string
	Subject string
	Date    time.Time
	Ref     int // the number it answers (0: none)
	Body    string
}

// Conference is a conference listed in CONTROL.DAT.
type Conference struct {
	Number int
	Name   string
}

// Packet is a QWK packet's contents.
type Packet struct {
	BBSID    string // up to 8 letters and digits: the packet's name
	BBSName  string
	Location string
	Phone    string
	Sysop    string
	User     string
	Created  time.Time
	Confs    []Conference
	Messages []Message
	Welcome  string // HELLO's text, if any
}

var idPattern = regexp.MustCompile(`^[A-Z0-9]{1,8}$`)

// ValidID: a BBSID is 1 to 8 capital letters and digits.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// MakeID makes a BBSID of a name: its letters and digits, in capitals,
// up to 8 ("BBS" if none).
func MakeID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') && b.Len() < 8 {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "BBS"
	}
	return b.String()
}

// cp437 is text in CP437 ('?' for what it lacks), control characters
// out.
func cp437(s string) []byte {
	enc := charmap.CodePage437.NewEncoder()
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r < ' ' || r == 0x7f {
			continue
		}
		b, err := enc.Bytes([]byte(string(r)))
		if err != nil || len(b) != 1 {
			out = append(out, '?')
			continue
		}
		out = append(out, b[0])
	}
	return out
}

// text is CP437 (or UTF-8, from a reader that writes it) as a string.
func text(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for _, c := range b {
		sb.WriteRune(charmap.CodePage437.DecodeByte(c))
	}
	return sb.String()
}

// field is s in CP437, cut or padded with spaces to n bytes.
func field(s string, n int) []byte {
	b := cp437(s)
	if len(b) > n {
		b = b[:n]
	}
	return append(b, bytes.Repeat([]byte{' '}, n-len(b))...)
}

// fits: s goes into a field of n bytes as it is.
func fits(s string, n int) bool { return len(cp437(s)) <= n }

// header is a message's 128-byte header for its text of blocks blocks.
func header(m *Message, num string, logical int, blocks int) []byte {
	h := make([]byte, 0, block)
	status := byte(' ')
	switch {
	case m.Private && m.Read:
		status = '+'
	case m.Private:
		status = '*'
	case m.Read:
		status = '-'
	}
	h = append(h, status)
	h = append(h, field(num, 7)...)
	d := m.Date
	if d.IsZero() {
		d = time.Now()
	}
	h = append(h, field(d.Format("01-02-06"), 8)...)
	h = append(h, field(d.Format("15:04"), 5)...)
	h = append(h, field(m.To, 25)...)
	h = append(h, field(m.From, 25)...)
	h = append(h, field(m.Subject, 25)...)
	h = append(h, field("", 12)...)
	ref := ""
	if m.Ref > 0 {
		ref = strconv.Itoa(m.Ref)
	}
	h = append(h, field(ref, 8)...)
	h = append(h, field(strconv.Itoa(blocks+1), 6)...)
	h = append(h, active)
	h = append(h, byte(m.Conf), byte(m.Conf>>8))
	h = append(h, byte(logical), byte(logical>>8))
	h = append(h, ' ')
	return h
}

// body is a message's text in blocks: QWKE kludges for what the header
// cuts short, then the lines.
func body(m *Message) []byte {
	var t []byte
	for _, k := range []struct{ name, v string }{{"To", m.To}, {"From", m.From}, {"Subject", m.Subject}} {
		if !fits(k.v, 25) {
			t = append(t, cp437(k.name+": "+k.v)...)
			t = append(t, lineEnd)
		}
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n"), "\n")
	for _, l := range lines {
		// CP437's π is the line end
		t = append(t, bytes.ReplaceAll(cp437(l), []byte{lineEnd}, []byte{'?'})...)
		t = append(t, lineEnd)
	}
	if pad := len(t) % block; pad != 0 {
		t = append(t, bytes.Repeat([]byte{' '}, block-pad)...)
	}
	return t
}

// mbf is n as a Microsoft Binary Format single, as index files have.
func mbf(n int) []byte {
	f := float32(n)
	if f == 0 {
		return []byte{0, 0, 0, 0}
	}
	bits := math.Float32bits(f)
	sign := bits >> 31
	exp := (bits >> 23) & 0xff
	mant := bits & 0x7fffff
	exp += 2 // IEEE bias 127, MBF 129
	v := exp<<24 | sign<<23 | mant
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

// Write makes the packet: a ZIP of CONTROL.DAT, MESSAGES.DAT, an index
// file for each conference with messages, PERSONAL.NDX, DOOR.ID and HELLO.
func (p *Packet) Write(w io.Writer) error {
	var msgs bytes.Buffer
	msgs.Write(field("Produced by bbs-sec.  QWK packets are a format of Sparkware's Qmail.", block))
	ndx := map[int]*bytes.Buffer{}
	personal := &bytes.Buffer{}
	for i := range p.Messages {
		m := &p.Messages[i]
		at := msgs.Len()/block + 1 // the header's block, from 1
		b := body(m)
		msgs.Write(header(m, strconv.Itoa(m.Number), i+1, len(b)/block))
		msgs.Write(b)
		rec := append(mbf(at), byte(m.Conf))
		if ndx[m.Conf] == nil {
			ndx[m.Conf] = &bytes.Buffer{}
		}
		ndx[m.Conf].Write(rec)
		if strings.EqualFold(m.To, p.User) {
			personal.Write(rec)
		}
	}
	created := p.Created
	if created.IsZero() {
		created = time.Now()
	}
	var ctl bytes.Buffer
	line := func(s string) { ctl.Write(cp437(s)); ctl.WriteString("\r\n") }
	line(p.BBSName)
	line(p.Location)
	line(p.Phone)
	line(p.Sysop + ", Sysop")
	line("00000," + p.BBSID)
	line(created.Format("01-02-2006,15:04:05"))
	line(strings.ToUpper(p.User))
	line("")
	line("0")
	line(strconv.Itoa(len(p.Messages)))
	line(strconv.Itoa(len(p.Confs) - 1))
	for _, c := range p.Confs {
		line(strconv.Itoa(c.Number))
		line(c.Name)
	}
	line("HELLO")
	line("NEWS")
	line("GOODBYE")

	z := zip.NewWriter(w)
	put := func(name string, data []byte) error {
		f, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: created})
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}
	if err := put("CONTROL.DAT", ctl.Bytes()); err != nil {
		return err
	}
	if err := put("MESSAGES.DAT", msgs.Bytes()); err != nil {
		return err
	}
	for conf, b := range ndx {
		if err := put(fmt.Sprintf("%03d.NDX", conf), b.Bytes()); err != nil {
			return err
		}
	}
	if personal.Len() > 0 {
		if err := put("PERSONAL.NDX", personal.Bytes()); err != nil {
			return err
		}
	}
	door := "DOOR = bbs-sec\r\nVERSION = 1\r\nSYSTEM = bbs-sec\r\nCONTROLNAME = " + p.BBSID + "\r\nMIXEDCASE = YES\r\n"
	if err := put("DOOR.ID", []byte(door)); err != nil {
		return err
	}
	if p.Welcome != "" {
		if err := put("HELLO", cp437Lines(p.Welcome)); err != nil {
			return err
		}
	}
	return z.Close()
}

func cp437Lines(s string) []byte {
	var out []byte
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		out = append(out, cp437(l)...)
		out = append(out, '\r', '\n')
	}
	return out
}

// Limits on what a REP may hold.
const (
	MaxREP      = 16 << 20 // bytes, the archive
	MaxMessages = 1000
	maxText     = 64 << 10 // bytes of one message's text
)

// ErrNotREP: what came is not a REP packet for this BBS.
var ErrNotREP = errors.New("not a REP packet")

// ReadREP reads a REP packet (a ZIP holding BBSID.MSG, or the .MSG file
// itself) for the BBS of bbsid: the messages in it.
func ReadREP(data []byte, bbsid string) ([]Message, error) {
	if len(data) > MaxREP {
		return nil, fmt.Errorf("a REP packet is at most %d bytes", MaxREP)
	}
	msg := data
	if bytes.HasPrefix(data, []byte("PK")) {
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, ErrNotREP
		}
		msg = nil
		for _, f := range z.File {
			if strings.EqualFold(f.Name, bbsid+".MSG") {
				r, err := f.Open()
				if err != nil {
					return nil, err
				}
				msg, err = io.ReadAll(io.LimitReader(r, MaxREP+1))
				r.Close()
				if err != nil {
					return nil, err
				}
			}
		}
		if msg == nil {
			return nil, fmt.Errorf("%w: no %s.MSG in it", ErrNotREP, bbsid)
		}
	}
	if len(msg) < block {
		return nil, ErrNotREP
	}
	if got := strings.TrimSpace(string(msg[:8])); !strings.EqualFold(got, bbsid) {
		return nil, fmt.Errorf("%w: it is for %q, not %q", ErrNotREP, got, bbsid)
	}
	var out []Message
	for at := block; at+block <= len(msg); {
		h := msg[at : at+block]
		n, err := strconv.Atoi(strings.TrimSpace(string(h[116:122])))
		if err != nil || n < 1 || at+n*block > len(msg)+block {
			return out, fmt.Errorf("message %d: a bad header", len(out)+1)
		}
		end := min(at+n*block, len(msg))
		raw := msg[at+block : end]
		at += n * block
		if h[122] == killed {
			continue
		}
		if len(out) >= MaxMessages {
			return out, fmt.Errorf("a REP packet holds at most %d messages", MaxMessages)
		}
		conf := int(h[123]) | int(h[124])<<8
		// in a REP, the number field is the conference (QWKE readers
		// also put it where packets have it)
		if c, err := strconv.Atoi(strings.TrimSpace(string(h[1:8]))); err == nil && c >= 0 && c < 65536 {
			conf = c
		}
		m := Message{Conf: conf, Private: h[0] == '*' || h[0] == '+',
			To: strings.TrimSpace(text(h[21:46])), From: strings.TrimSpace(text(h[46:71])),
			Subject: strings.TrimSpace(text(h[71:96]))}
		m.Ref, _ = strconv.Atoi(strings.TrimSpace(string(h[108:116])))
		if d, err := time.Parse("01-02-0615:04", string(h[8:16])+string(h[16:21])); err == nil {
			m.Date = d
		}
		if len(raw) > maxText {
			raw = raw[:maxText]
		}
		parseText(&m, raw)
		out = append(out, m)
	}
	return out, nil
}

// parseText sets m's body from its text in a packet, taking QWKE kludge
// lines at its head for the header's fields.
func parseText(m *Message, raw []byte) {
	raw = bytes.TrimRight(raw, " \x00\x1a")
	var lines []string
	kludges := true
	for _, l := range bytes.Split(raw, []byte{lineEnd}) {
		s := strings.TrimRight(text(bytes.ReplaceAll(l, []byte{'\r'}, nil)), " ")
		if kludges {
			if k, v, ok := strings.Cut(s, ": "); ok && (k == "To" || k == "From" || k == "Subject") {
				switch k {
				case "To":
					m.To = v
				case "From":
					m.From = v
				case "Subject":
					m.Subject = v
				}
				continue
			}
			kludges = false
		}
		lines = append(lines, s)
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	m.Body = strings.Join(lines, "\n")
}

// WriteREP makes a REP packet of messages for the BBS of bbsid (as a
// reader does; for tests and for calling a hub).
func WriteREP(w io.Writer, bbsid string, ms []Message) error {
	var b bytes.Buffer
	b.Write(field(bbsid, block))
	for i := range ms {
		m := ms[i]
		t := body(&m)
		h := header(&m, strconv.Itoa(m.Conf), 0, len(t)/block)
		b.Write(h)
		b.Write(t)
	}
	z := zip.NewWriter(w)
	f, err := z.Create(bbsid + ".MSG")
	if err != nil {
		return err
	}
	if _, err := f.Write(b.Bytes()); err != nil {
		return err
	}
	return z.Close()
}

// ReadPacket reads a QWK packet (as a reader does; for tests and for
// what a hub sends).
func ReadPacket(data []byte) (*Packet, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("not a QWK packet (no ZIP)")
	}
	files := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, 64<<20))
		r.Close()
		if err != nil {
			return nil, err
		}
		files[strings.ToUpper(f.Name)] = b
	}
	ctl, ok := files["CONTROL.DAT"]
	if !ok {
		return nil, errors.New("not a QWK packet (no CONTROL.DAT)")
	}
	ls := strings.Split(strings.ReplaceAll(text(ctl), "\r", ""), "\n")
	get := func(i int) string {
		if i < len(ls) {
			return strings.TrimSpace(ls[i])
		}
		return ""
	}
	p := &Packet{BBSName: get(0), Location: get(1), Phone: get(2), Sysop: strings.TrimSuffix(get(3), ", Sysop"),
		User: get(6)}
	if _, id, ok := strings.Cut(get(4), ","); ok {
		p.BBSID = strings.TrimSpace(id)
	}
	if t, err := time.Parse("01-02-2006,15:04:05", get(5)); err == nil {
		p.Created = t
	}
	nconf, _ := strconv.Atoi(get(10))
	for i := 0; i <= nconf && 11+2*i+1 < len(ls); i++ {
		n, _ := strconv.Atoi(get(11 + 2*i))
		p.Confs = append(p.Confs, Conference{Number: n, Name: get(12 + 2*i)})
	}
	msg := files["MESSAGES.DAT"]
	for at := block; at+block <= len(msg); {
		h := msg[at : at+block]
		n, err := strconv.Atoi(strings.TrimSpace(string(h[116:122])))
		if err != nil || n < 1 {
			return p, fmt.Errorf("message %d: a bad header", len(p.Messages)+1)
		}
		end := min(at+n*block, len(msg))
		raw := msg[at+block : end]
		at += n * block
		m := Message{Conf: int(h[123]) | int(h[124])<<8, Private: h[0] == '*' || h[0] == '+', Read: h[0] == '-' || h[0] == '+',
			To: strings.TrimSpace(text(h[21:46])), From: strings.TrimSpace(text(h[46:71])),
			Subject: strings.TrimSpace(text(h[71:96]))}
		m.Number, _ = strconv.Atoi(strings.TrimSpace(string(h[1:8])))
		m.Ref, _ = strconv.Atoi(strings.TrimSpace(string(h[108:116])))
		if d, err := time.Parse("01-02-0615:04", string(h[8:16])+string(h[16:21])); err == nil {
			m.Date = d
		}
		parseText(&m, raw)
		p.Messages = append(p.Messages, m)
	}
	return p, nil
}
