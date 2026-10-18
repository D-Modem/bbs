package ftn

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Message attributes (FTS-0001), those a packed message carries.
const (
	AttrPrivate     = 0x0001
	AttrCrash       = 0x0002
	AttrFileAttach  = 0x0010
	AttrHold        = 0x0200
	AttrFileRequest = 0x0800
	AttrReturnRcpt  = 0x1000
	AttrIsReturn    = 0x2000
	AttrAuditReq    = 0x4000
	AttrFileUpdate  = 0x8000
)

// ProductCode is ours in packet headers: 0xFE, no code assigned (FTSC's
// list is for products that ask).
const ProductCode = 0xFE

// Header is a packet's header.
type Header struct {
	Orig, Dest Addr
	Date       time.Time
	Password   string // up to 8 characters
	Product    uint16
	Version    [2]byte // major, minor
	TwoPlus    bool    // read: the header was Type 2+
}

// Message is a packed message.  Its texts (names, subject, lines) are in
// the message's character set, as they came: Decode them by its CHRS.
type Message struct {
	OrigNode, DestNode, OrigNet, DestNet int // the header's 2D addresses
	Attr, Cost                           uint16
	DateTime                             string // as written: "01 Jan 86  02:34:56"
	To, From, Subject                    string

	Area    string   // AREA: of an echomail message ("" for netmail)
	Kludges []Kludge // ^A lines but PATH, in order
	Lines   []string // the text: tear and origin lines too
	SeenBy  []Net2D
	Path    []Net2D
}

// Kludge is a ^A line: "\x01MSGID: 2:5020/1 1234abcd" is {MSGID, 2:5020/1 1234abcd}.
type Kludge struct{ Name, Value string }

// Limits on what is read.
const (
	maxName    = 36 // with the NUL
	maxSubject = 72
	maxDate    = 20
)

// Errors reading packets.
var (
	ErrPacket = errors.New("not a Type 2 packet")
	// ErrTooBig is a message longer than allowed: skipped, the packet goes on
	ErrTooBig = errors.New("message too big")
)

const headerLen = 58

// WriteHeader writes a Type 2+ header (FSC-0039).
func WriteHeader(w io.Writer, h *Header) error {
	b := make([]byte, headerLen)
	le := binary.LittleEndian
	d := h.Date
	if d.IsZero() {
		d = time.Now()
	}
	le.PutUint16(b[0:], uint16(h.Orig.Node))
	le.PutUint16(b[2:], uint16(h.Dest.Node))
	le.PutUint16(b[4:], uint16(d.Year()))
	le.PutUint16(b[6:], uint16(d.Month()-1))
	le.PutUint16(b[8:], uint16(d.Day()))
	le.PutUint16(b[10:], uint16(d.Hour()))
	le.PutUint16(b[12:], uint16(d.Minute()))
	le.PutUint16(b[14:], uint16(d.Second()))
	le.PutUint16(b[16:], 0)
	le.PutUint16(b[18:], 2)
	le.PutUint16(b[20:], uint16(h.Orig.Net))
	le.PutUint16(b[22:], uint16(h.Dest.Net))
	prod := h.Product
	if prod == 0 {
		prod = ProductCode
	}
	b[24] = byte(prod)
	b[25] = h.Version[0]
	copy(b[26:34], h.Password)
	le.PutUint16(b[34:], uint16(h.Orig.Zone))
	le.PutUint16(b[36:], uint16(h.Dest.Zone))
	le.PutUint16(b[40:], 0x0100) // capability word, byte-swapped
	b[42] = byte(prod >> 8)
	b[43] = h.Version[1]
	le.PutUint16(b[44:], 0x0001) // Type 2+
	le.PutUint16(b[46:], uint16(h.Orig.Zone))
	le.PutUint16(b[48:], uint16(h.Dest.Zone))
	le.PutUint16(b[50:], uint16(h.Orig.Point))
	le.PutUint16(b[52:], uint16(h.Dest.Point))
	_, err := w.Write(b)
	return err
}

// Reader reads a packet.
type Reader struct {
	r   *bufio.Reader
	max int
	end bool
}

// NewReader reads a packet's header; Next reads its messages, of up to
// max bytes of text each.
func NewReader(r io.Reader, max int) (*Reader, *Header, error) {
	br := bufio.NewReader(r)
	b := make([]byte, headerLen)
	if _, err := io.ReadFull(br, b); err != nil {
		return nil, nil, ErrPacket
	}
	le := binary.LittleEndian
	u := func(o int) int { return int(le.Uint16(b[o:])) }
	if u(18) != 2 {
		return nil, nil, ErrPacket
	}
	h := &Header{}
	h.Orig = Addr{Node: u(0), Net: u(20)}
	h.Dest = Addr{Node: u(2), Net: u(22)}
	mon := u(6)
	if mon > 11 {
		mon = 0
	}
	h.Date = time.Date(u(4), time.Month(mon+1), u(8), u(10), u(12), u(14), 0, time.UTC)
	h.Password = strings.TrimRight(string(b[26:34]), "\x00 ")
	h.Product = uint16(b[24]) | uint16(b[42])<<8
	h.Version = [2]byte{b[25], b[43]}
	cw := le.Uint16(b[44:])
	cv := le.Uint16(b[40:])
	if cw == cv>>8|cv<<8 && cw&1 == 1 {
		h.TwoPlus = true
		h.Orig.Zone, h.Dest.Zone = u(46), u(48)
		h.Orig.Point, h.Dest.Point = u(50), u(52)
		// FSC-0048: a point's net is -1, its boss's net in auxNet
		if h.Orig.Point != 0 && h.Orig.Net == 0xFFFF {
			h.Orig.Net = u(38)
		}
	}
	if h.Orig.Zone == 0 {
		h.Orig.Zone = u(34)
	}
	if h.Dest.Zone == 0 {
		h.Dest.Zone = u(36)
	}
	return &Reader{r: br, max: max}, h, nil
}

// cstring reads up to max bytes to a NUL (which is read, not returned).
func cstring(r *bufio.Reader, max int) (string, error) {
	var sb strings.Builder
	for {
		c, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if c == 0 {
			return sb.String(), nil
		}
		if sb.Len() >= max {
			return "", ErrPacket
		}
		sb.WriteByte(c)
	}
}

// Next reads the next message: io.EOF after the last.  ErrTooBig is a
// message skipped; the packet may go on.
func (p *Reader) Next() (*Message, error) {
	if p.end {
		return nil, io.EOF
	}
	var t [2]byte
	if _, err := io.ReadFull(p.r, t[:]); err != nil {
		// no terminating 0000: what there was is had
		p.end = true
		return nil, io.EOF
	}
	switch binary.LittleEndian.Uint16(t[:]) {
	case 0:
		p.end = true
		return nil, io.EOF
	case 2:
	default:
		p.end = true
		return nil, fmt.Errorf("%w: message type %x", ErrPacket, t)
	}
	b := make([]byte, 12)
	if _, err := io.ReadFull(p.r, b); err != nil {
		p.end = true
		return nil, ErrPacket
	}
	le := binary.LittleEndian
	m := &Message{OrigNode: int(le.Uint16(b[0:])), DestNode: int(le.Uint16(b[2:])),
		OrigNet: int(le.Uint16(b[4:])), DestNet: int(le.Uint16(b[6:])),
		Attr: le.Uint16(b[8:]), Cost: le.Uint16(b[10:])}
	// the date is 20 bytes, fixed; some write it shorter, NUL-ended
	var err error
	fail := func(e error) (*Message, error) {
		p.end = true
		if errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF) {
			e = fmt.Errorf("%w: cut short", ErrPacket)
		}
		return nil, e
	}
	if m.DateTime, err = cstring(p.r, maxDate); err != nil {
		return fail(err)
	}
	if n := len(m.DateTime) + 1; n < maxDate {
		// a fixed 20-byte field, its rest NULs: skip those
		if pk, _ := p.r.Peek(maxDate - n); len(pk) == maxDate-n && bytes.Count(pk, []byte{0}) == len(pk) {
			p.r.Discard(maxDate - n)
		}
	}
	if m.To, err = cstring(p.r, maxName); err != nil {
		return fail(err)
	}
	if m.From, err = cstring(p.r, maxName); err != nil {
		return fail(err)
	}
	if m.Subject, err = cstring(p.r, maxSubject); err != nil {
		return fail(err)
	}
	var text []byte
	big := false
	for {
		c, err := p.r.ReadByte()
		if err != nil {
			return fail(err)
		}
		if c == 0 {
			break
		}
		if len(text) < p.max {
			text = append(text, c)
		} else {
			big = true
		}
	}
	if big {
		return nil, ErrTooBig
	}
	m.parseText(text)
	return m, nil
}

// ParseMessage reads a packed message alone, as WriteMessage writes it.
func ParseMessage(b []byte, max int) (*Message, error) {
	p := &Reader{r: bufio.NewReader(bytes.NewReader(b)), max: max}
	m, err := p.Next()
	if err == io.EOF {
		return nil, ErrPacket
	}
	return m, err
}

// parseText splits a message's text into its parts.
func (m *Message) parseText(text []byte) {
	text = bytes.ReplaceAll(text, []byte{'\n'}, nil)
	lines := strings.Split(string(text), "\r")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "AREA:") {
		m.Area = strings.ToUpper(strings.TrimSpace(lines[0][5:]))
		lines = lines[1:]
	}
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "\x01"):
			name, val := splitKludge(l[1:])
			if name == "PATH" {
				m.Path = append(m.Path, ParseNet2D(val)...)
				continue
			}
			m.Kludges = append(m.Kludges, Kludge{name, val})
		case strings.HasPrefix(l, "SEEN-BY:"):
			m.SeenBy = append(m.SeenBy, ParseNet2D(l[8:])...)
		default:
			m.Lines = append(m.Lines, l)
		}
	}
	for len(m.Lines) > 0 && strings.TrimSpace(m.Lines[len(m.Lines)-1]) == "" {
		m.Lines = m.Lines[:len(m.Lines)-1]
	}
}

// splitKludge is a ^A line's name (upper case) and value, split at the first
// colon or space (a space after a colon is dropped).
func splitKludge(l string) (string, string) {
	if i := strings.IndexAny(l, ": "); i >= 0 {
		name := l[:i]
		val := l[i+1:]
		if l[i] == ':' {
			val = strings.TrimPrefix(val, " ")
		}
		return strings.ToUpper(name), val
	}
	return strings.ToUpper(l), ""
}

// Kludge is the value of the first ^A line of that name ("" if none).
func (m *Message) Kludge(name string) string {
	for _, k := range m.Kludges {
		if k.Name == name {
			return k.Value
		}
	}
	return ""
}

// SetKludge sets (or adds, after the others) a ^A line.
func (m *Message) SetKludge(name, val string) {
	for i, k := range m.Kludges {
		if k.Name == name {
			m.Kludges[i].Value = val
			return
		}
	}
	m.Kludges = append(m.Kludges, Kludge{name, val})
}

// Text is the message's text as packed: AREA:, kludges, lines, SEEN-BY,
// PATH, each line ended by CR.
func (m *Message) Text() []byte {
	var b bytes.Buffer
	if m.Area != "" {
		b.WriteString("AREA:" + m.Area + "\r")
	}
	for _, k := range m.Kludges {
		switch k.Name {
		case "INTL", "FMPT", "TOPT":
			b.WriteString("\x01" + k.Name + " " + k.Value + "\r")
		default:
			b.WriteString("\x01" + k.Name + ": " + k.Value + "\r")
		}
	}
	for _, l := range m.Lines {
		b.WriteString(l + "\r")
	}
	for _, l := range FormatNet2D("SEEN-BY: ", m.SeenBy, 70) {
		b.WriteString(l + "\r")
	}
	for _, l := range FormatNet2D("\x01PATH: ", m.Path, 70) {
		b.WriteString(l + "\r")
	}
	return b.Bytes()
}

// cut is s made short enough for a NUL-ended field of max bytes.
func cut(s string, max int) string {
	if len(s) >= max {
		return s[:max-1]
	}
	return s
}

// WriteMessage writes a packed message.
func WriteMessage(w io.Writer, m *Message) error {
	var b bytes.Buffer
	le := binary.LittleEndian
	for _, v := range []uint16{2, uint16(m.OrigNode), uint16(m.DestNode), uint16(m.OrigNet),
		uint16(m.DestNet), m.Attr, m.Cost} {
		b.Write(le.AppendUint16(nil, v))
	}
	dt := make([]byte, maxDate)
	copy(dt, cut(m.DateTime, maxDate))
	b.Write(dt)
	for _, s := range []string{cut(m.To, maxName), cut(m.From, maxName), cut(m.Subject, maxSubject)} {
		b.WriteString(strings.ReplaceAll(s, "\x00", ""))
		b.WriteByte(0)
	}
	b.Write(bytes.ReplaceAll(m.Text(), []byte{0}, nil))
	b.WriteByte(0)
	_, err := w.Write(b.Bytes())
	return err
}

// WriteEnd ends a packet.
func WriteEnd(w io.Writer) error {
	_, err := w.Write([]byte{0, 0})
	return err
}

// FormatDate is a message's date as FTS-0001 writes it.
func FormatDate(t time.Time) string { return t.Format("02 Jan 06  15:04:05") }

// ParseDate reads a message's date: FTS-0001's "01 Jan 86  02:34:56", or
// SEAdog's "Mon  1 Jan 86 02:34".  Zero if neither.
func ParseDate(s string) time.Time {
	f := strings.Fields(s)
	if len(f) == 5 && len(f[0]) == 3 {
		f = f[1:] // SEAdog: a weekday first
	}
	if len(f) != 4 {
		return time.Time{}
	}
	day := 0
	fmt.Sscanf(f[0], "%d", &day)
	mon := strings.Index("JanFebMarAprMayJunJulAugSepOctNovDec", f[1])
	if mon < 0 || mon%3 != 0 || len(f[1]) != 3 || day < 1 || day > 31 {
		return time.Time{}
	}
	year := 0
	if _, err := fmt.Sscanf(f[2], "%d", &year); err != nil {
		return time.Time{}
	}
	if year < 100 {
		if year < 80 {
			year += 2000
		} else {
			year += 1900
		}
	}
	var hh, mm, ss int
	if n, _ := fmt.Sscanf(f[3], "%d:%d:%d", &hh, &mm, &ss); n < 2 || hh > 23 || mm > 59 || ss > 59 {
		return time.Time{}
	}
	return time.Date(year, time.Month(mon/3+1), day, hh, mm, ss, 0, time.UTC)
}
