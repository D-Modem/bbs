package xfer

// CompuServe B, Interim ("Quick") B and B Plus, the host's side and the
// terminal's.
//
// The host starts everything: it sends ENQ while the terminal is in plain
// terminal mode, the terminal's answer says which B it speaks (DLE '0';
// DLE '+' DLE '0'; DLE '+' '+' DLE '0'), and B Plus terminals then trade
// transport parameters ('+' packets) before the first transfer.
//
// A packet is DLE 'B' <seq> <type> <body> ETX <check>, the body and check
// quoted (a control byte c goes as DLE c+0x40, a byte 0x80-0x9F as DLE
// (c&0x1F)+0x60), the check over seq, type, body and ETX before quoting.
// ACK is DLE <seq>, NAK is NAK.  Sources: Russ Ranshaw, "The CompuServe B
// Plus Protocol" (1993); Steve Wilhite's bp.c (1985); Steve Sneed's
// OOBPLUS (1991).

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"time"
)

const (
	bETX = 0x03
	bENQ = 0x05
	bDLE = 0x10
	bNAK = 0x15
	bRS  = 0x1E
)

// Check methods of B Plus.
const (
	CheckSum   = 0 // the 8-bit rotate-and-add checksum
	CheckCRC16 = 1 // CRC-16/XMODEM with a preset of 0xFFFF, high byte first
	CheckCCITT = 2 // CRC-16/X-25, low byte first
	CheckCRC32 = 3 // CRC-32 (zlib), low byte first
)

// BParams are B Plus transport parameters, in the order a '+' packet
// carries them.
type BParams struct {
	WS, WR byte    // windows, send and receive: outstanding packets less one
	BS     byte    // block size / 128 (0 means 4: 512)
	CM     byte    // check method
	DQ     byte    // obsolete quote level, unused when Q is given
	TL     byte    // 1: the Transport Layer (for CIM's HMI)
	Q      [8]byte // quote masks: 0x00-0x1F, then 0x80-0x9F, bit 7 first
	DR     byte    // download resume: 0 none, 1 Tr, 2 Tr and Tf
	UR     byte    // upload resume (CIS: none)
	FI     byte    // 1: the TI file information packet
}

// HostParams are the parameters this host offers: two packets outstanding
// each way, 1 KB blocks, CRC-16, CompuServe's own quote set, no transport
// layer, resume or file information.
var HostParams = BParams{WS: 1, WR: 1, BS: 8, CM: CheckCRC16, DQ: 1,
	Q: [8]byte{0x14, 0x00, 0xD4, 0x02, 0x00, 0x00, 0x50, 0x00}}

func (p BParams) bytes() []byte {
	b := []byte{p.WS, p.WR, p.BS, p.CM, p.DQ, p.TL}
	b = append(b, p.Q[:]...)
	return append(b, p.DR, p.UR, p.FI)
}

func parseBParams(b []byte) BParams {
	f := make([]byte, 17)
	copy(f, b)
	p := BParams{WS: f[0], WR: f[1], BS: f[2], CM: f[3], DQ: f[4], TL: f[5], DR: f[14], UR: f[15], FI: f[16]}
	copy(p.Q[:], f[6:14])
	if len(b) < 7 {
		// no quote masks: from the old quote level
		p.Q = dqMasks(p.DQ)
	}
	return p
}

// dqMasks are the quote masks of an old quote level.
func dqMasks(dq byte) [8]byte {
	switch dq {
	case 0:
		return [8]byte{0x94, 0x00, 0xD4, 0x00} // 00 03 05 10 11 13 15
	case 1:
		return [8]byte{0x14, 0x00, 0xD4, 0x00}
	case 2:
		return [8]byte{0x14, 0x00, 0xD4, 0x00, 0x00, 0x00, 0x50, 0x00}
	}
	return [8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
}

// negotiate is what both sides use after trading parameters: windows
// crossed, the least of the rest, quote masks or'ed.
func negotiate(mine, theirs BParams) BParams {
	m := func(a, b byte) byte { return min(a, b) }
	r := BParams{WS: m(mine.WS, theirs.WR), WR: m(mine.WR, theirs.WS), BS: m(blockUnits(mine.BS), blockUnits(theirs.BS)),
		CM: m(mine.CM, theirs.CM), TL: m(mine.TL, theirs.TL), DR: m(mine.DR, theirs.DR),
		UR: m(mine.UR, theirs.UR), FI: m(mine.FI, theirs.FI)}
	if r.CM > CheckCRC32 {
		r.CM = CheckSum
	}
	for i := range r.Q {
		r.Q[i] = mine.Q[i] | theirs.Q[i]
	}
	return r
}

func blockUnits(bs byte) byte {
	switch {
	case bs == 0:
		return 4
	case bs > 16:
		return 16
	}
	return bs
}

// BChecksum is B's 8-bit checksum: rotate left, add, carry around.
func BChecksum(p []byte) byte {
	ck := 0
	for _, b := range p {
		ck <<= 1
		if ck > 0xFF {
			ck = ck&0xFF + 1
		}
		ck += int(b)
		if ck > 0xFF {
			ck = ck&0xFF + 1
		}
	}
	return byte(ck)
}

// crcX25 is CRC-16/X-25 before its complement: reflected 0x8408, preset
// 0xFFFF.
func crcX25(data []byte, crc uint16) uint16 {
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// bCheck is the check value of p by method cm, as sent.
func bCheck(cm byte, p []byte) []byte {
	switch cm {
	case CheckCRC16:
		c := crc16(p, 0xFFFF)
		return []byte{byte(c >> 8), byte(c)}
	case CheckCCITT:
		c := ^crcX25(p, 0xFFFF)
		return []byte{byte(c), byte(c >> 8)}
	case CheckCRC32:
		c := crc32.ChecksumIEEE(p)
		return []byte{byte(c), byte(c >> 8), byte(c >> 16), byte(c >> 24)}
	}
	return []byte{BChecksum(p)}
}

// BPacket is a B packet: its sequence digit ('0'-'9'), type and body.
type BPacket struct {
	Seq  byte
	Type byte
	Body []byte
}

// B errors.
var (
	ErrBAborted  = errors.New("B: aborted by the other side")
	ErrBFallback = errors.New("B: the terminal fell back to terminal mode")
)

// BFailure is an F packet: the other side gave up.
type BFailure struct {
	Code byte
	Text string
}

func (f *BFailure) Error() string {
	if f.Text != "" {
		return fmt.Sprintf("B: failure %c: %s", f.Code, f.Text)
	}
	return fmt.Sprintf("B: failure %c", f.Code)
}

// BLink is one side of a B link.  Its zero timeouts are the protocol's:
// 10 seconds a character, 10 errors.
type BLink struct {
	l       *line
	Timeout time.Duration // the most to wait for a byte
	Errors  int           // the most errors in a row

	Plus     bool    // the terminal spoke Interim B or B Plus ('+' seen)
	PlusPlus bool    // ...B Plus
	Relaxed  bool    // the terminal asked for relaxed timing
	Params   BParams // negotiated: the zero value for Classic B

	quote [256]bool
	seq   byte // the sequence digit of the last packet sent or taken
	last  []byte
	// ahead is a packet read while waiting for an ACK: the other side's
	// next, which ACKs ours by implication
	ahead *BPacket
	// unacked is a packet sent whose only ACK is the other side's next
	// packet (TU): a NAK or silence while waiting for that resends it
	unacked []byte
}

// NewBLink is a B link on p, as Classic B with every control byte quoted.
func NewBLink(p Port) *BLink {
	k := &BLink{l: newLine(p), seq: '0'}
	k.setQuote([]byte{0xFF, 0xFF, 0xFF, 0xFF}, [4]byte{})
	return k
}

func (k *BLink) timeout() time.Duration {
	d := k.Timeout
	if d == 0 {
		d = 10 * time.Second
	}
	if k.Relaxed {
		d *= 2
	}
	return d
}

func (k *BLink) maxErrors() int {
	if k.Errors == 0 {
		return 10
	}
	return k.Errors
}

// setQuote sets the quote set from the masks of 0x00-0x1F (lo) and
// 0x80-0x9F (hi).  DLE is always quoted.
func (k *BLink) setQuote(lo []byte, hi [4]byte) {
	for i := 0; i < 32; i++ {
		k.quote[i] = lo[i/8]&(0x80>>(i%8)) != 0
		k.quote[0x80+i] = hi[i/8]&(0x80>>(i%8)) != 0
	}
	k.quote[bDLE] = true
	k.quote[bETX] = true
}

func (k *BLink) quoteAll() {
	for i := 0; i < 32; i++ {
		k.quote[i], k.quote[0x80+i] = true, true
	}
}

func (k *BLink) applyParams() {
	var hi [4]byte
	copy(hi[:], k.Params.Q[4:])
	k.setQuote(k.Params.Q[:4], hi)
}

// Rest is what the link has read past the end of the protocol: the
// other side's text after it, for a terminal going back to terminal mode.
func (k *BLink) Rest() []byte {
	b := append([]byte(nil), k.l.buf[k.l.r:k.l.w]...)
	k.l.r = k.l.w
	return b
}

// BlockSize is how much data a packet may carry.
func (k *BLink) BlockSize() int {
	if !k.PlusPlus {
		return 511
	}
	return int(blockUnits(k.Params.BS)) * 128
}

func next(s byte) byte {
	if s >= '9' {
		return '0'
	}
	return s + 1
}

// encode makes the bytes of a packet.
func (k *BLink) encode(p BPacket, cm byte) []byte {
	raw := append([]byte{p.Seq, p.Type}, p.Body...)
	raw = append(raw, bETX)
	var b bytes.Buffer
	b.Write([]byte{bDLE, 'B', p.Seq, p.Type})
	q := func(c byte) {
		if k.quote[c] {
			if c < 0x80 {
				b.Write([]byte{bDLE, c + 0x40})
			} else {
				b.Write([]byte{bDLE, c&0x1F + 0x60})
			}
		} else {
			b.WriteByte(c)
		}
	}
	for _, c := range p.Body {
		q(c)
	}
	b.WriteByte(bETX)
	for _, c := range bCheck(cm, raw) {
		q(c)
	}
	if cm >= CheckCCITT {
		b.WriteByte(bRS)
	}
	return b.Bytes()
}

// send sends packet p with check method cm.
func (k *BLink) send(p BPacket, cm byte) error {
	b := k.encode(p, cm)
	k.last = b
	return k.l.write(b)
}

// what the link read
const (
	evPacket = iota
	evACK
	evNAK
	evENQ
	evWACK
	evBad // a packet whose check failed
)

type bEvent struct {
	kind int
	seq  byte
	pkt  BPacket
}

// read waits for the next thing from the other side.
func (k *BLink) read(cm byte) (bEvent, error) {
	dles := 0
	for {
		c, err := k.l.getc(k.timeout())
		if err != nil {
			return bEvent{}, err
		}
		switch c {
		case bNAK:
			return bEvent{kind: evNAK}, nil
		case bENQ:
			return bEvent{kind: evENQ}, nil
		case bDLE:
			dles++
			if dles >= 4 {
				return bEvent{}, ErrBAborted
			}
			continue
		}
		if dles == 0 {
			continue // noise between packets
		}
		dles = 0
		switch {
		case c >= '0' && c <= '9':
			return bEvent{kind: evACK, seq: c}, nil
		case c == ';':
			return bEvent{kind: evWACK}, nil
		case c == '+':
			return bEvent{}, ErrBFallback
		case c == 'B':
			p, ok, err := k.readPacket(cm)
			if err != nil {
				return bEvent{}, err
			}
			if !ok {
				return bEvent{kind: evBad}, nil
			}
			return bEvent{kind: evPacket, seq: p.Seq, pkt: p}, nil
		}
	}
}

// readPacket reads a packet after its DLE 'B': ok false if its check is
// wrong.
func (k *BLink) readPacket(cm byte) (BPacket, bool, error) {
	d := k.timeout()
	getq := func() (byte, bool, error) { // a byte, unquoted; raw ETX
		c, err := k.l.getc(d)
		if err != nil {
			return 0, false, err
		}
		if c == bETX {
			return c, true, nil
		}
		if c != bDLE {
			return c, false, nil
		}
		c, err = k.l.getc(d)
		if err != nil {
			return 0, false, err
		}
		if c < 0x60 {
			return c & 0x1F, false, nil
		}
		return c&0x1F | 0x80, false, nil
	}
	var raw []byte
	limit := 2048 + 3
	for {
		c, etx, err := getq()
		if err != nil {
			return BPacket{}, false, err
		}
		raw = append(raw, c)
		if etx {
			break
		}
		if len(raw) > limit {
			return BPacket{}, false, nil
		}
	}
	n := len(bCheck(cm, nil))
	got := make([]byte, n)
	for i := range got {
		c, _, err := getq()
		if err != nil {
			return BPacket{}, false, err
		}
		got[i] = c
	}
	if len(raw) < 3 || !bytes.Equal(got, bCheck(cm, raw)) {
		return BPacket{}, false, nil
	}
	return BPacket{Seq: raw[0], Type: raw[1], Body: raw[2 : len(raw)-1]}, true, nil
}

func (k *BLink) ack(s byte) error { return k.l.write([]byte{bDLE, s}) }

// SendPacket sends a packet of typ and waits for its ACK (stop and wait).
// A packet from the other side while waiting, with the next sequence, ACKs
// it too: it is kept for ReadPacket.
func (k *BLink) SendPacket(typ byte, body []byte) error {
	return k.sendWait(typ, body, k.Params.CM)
}

func (k *BLink) sendWait(typ byte, body []byte, cm byte) error {
	s := next(k.seq)
	p := BPacket{Seq: s, Type: typ, Body: body}
	if err := k.send(p, cm); err != nil {
		return err
	}
	k.seq = s
	errs := 0
	for {
		ev, err := k.read(cm)
		if errors.Is(err, ErrTimeout) {
			ev, err = bEvent{kind: evNAK}, nil
		}
		if err != nil {
			return err
		}
		switch ev.kind {
		case evACK:
			if ev.seq == s {
				return nil
			}
			continue // an old ACK
		case evWACK:
			continue
		case evPacket:
			if ev.seq == next(s) {
				k.ahead = &ev.pkt
				return nil
			}
			if ev.pkt.Type == 'F' {
				k.ack(ev.seq)
				return failure(ev.pkt)
			}
		case evENQ:
			k.ack(k.seq)
			continue
		}
		if errs++; errs >= k.maxErrors() {
			return ErrRetries
		}
		if err := k.l.write(k.last); err != nil {
			return err
		}
	}
}

func failure(p BPacket) error {
	f := &BFailure{Code: 'E'}
	if len(p.Body) > 0 {
		f.Code, f.Text = p.Body[0], string(p.Body[1:])
	}
	return f
}

// ReadPacket waits for the other side's next packet and ACKs it.  A
// repeat of the last is ACKed again and dropped; a bad one is NAKed.
func (k *BLink) ReadPacket() (BPacket, error) {
	return k.readCM(k.Params.CM)
}

func (k *BLink) readCM(cm byte) (BPacket, error) {
	if p := k.ahead; p != nil {
		k.ahead = nil
		k.seq = p.Seq
		return *p, k.ack(p.Seq)
	}
	errs := 0
	for {
		ev, err := k.read(cm)
		if err != nil && !errors.Is(err, ErrTimeout) {
			return BPacket{}, err
		}
		if err == nil {
			switch ev.kind {
			case evPacket:
				switch ev.seq {
				case next(k.seq):
					k.seq = ev.seq
					return ev.pkt, k.ack(ev.seq)
				case k.seq:
					k.ack(ev.seq)
					continue
				}
				if ev.pkt.Type == 'F' {
					k.ack(ev.seq)
					return BPacket{}, failure(ev.pkt)
				}
				// out of order: one before it was lost.  The sender
				// resends from there when it hears nothing.
				if errs++; errs >= 4*k.maxErrors() {
					return BPacket{}, ErrRetries
				}
				continue
			case evENQ:
				k.ack(k.seq)
				continue
			case evACK, evWACK:
				continue
			}
		}
		if errs++; errs >= 4*k.maxErrors() {
			return BPacket{}, ErrRetries
		}
		if k.unacked != nil && (err != nil || ev.kind == evNAK) {
			if err := k.l.write(k.unacked); err != nil {
				return BPacket{}, err
			}
			continue
		}
		if err := k.l.write([]byte{bNAK}); err != nil {
			return BPacket{}, err
		}
	}
}

// Fail sends an F packet (code: A abort, E error, I I/O, M missing...),
// which ends the link.
func (k *BLink) Fail(code byte, text string) error {
	return k.SendPacket('F', append([]byte{code}, text...))
}

// Start is the host's ENQ: up to 5 of them, until the terminal answers.
// Then B Plus terminals are sent the host's parameters, and theirs read.
func (k *BLink) Start(host BParams) error {
	var err error
	for try := 0; try < 5; try++ {
		if err = k.l.write([]byte{bENQ}); err != nil {
			return err
		}
		if err = k.enqReply(); err == nil {
			break
		}
		if !errors.Is(err, ErrTimeout) && !errors.Is(err, ErrProtocol) {
			return err
		}
	}
	if err != nil {
		return err
	}
	if !k.Plus {
		return nil
	}
	return k.hostPlus(host)
}

// enqReply reads [DLE '+' ['+'] ['R']] DLE digit.
func (k *BLink) enqReply() error {
	d := k.timeout()
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	for {
		c, err := k.l.getc(d)
		if err != nil {
			return err
		}
		if c != bDLE {
			continue
		}
		c, err = k.l.getc(d)
		if err != nil {
			return err
		}
		if c >= '0' && c <= '9' {
			k.seq = c
			return nil
		}
		if c != '+' {
			return ErrProtocol
		}
		k.Plus = true
		for {
			c, err = k.l.getc(d)
			if err != nil {
				return err
			}
			switch c {
			case '+':
				k.PlusPlus = true
				continue
			case 'R':
				k.Relaxed = true
				continue
			}
			break
		}
		if c != bDLE {
			return ErrProtocol
		}
		if c, err = k.l.getc(d); err != nil {
			return err
		}
		if c < '0' || c > '9' {
			return ErrProtocol
		}
		k.seq = c
		return nil
	}
}

// hostPlus trades '+' packets: the host's, with the standard check and
// everything quoted; then the terminal's.
func (k *BLink) hostPlus(host BParams) error {
	if k.PlusPlus {
		k.quoteAll()
	}
	err := k.sendWait('+', host.bytes(), CheckSum)
	if err != nil {
		return err
	}
	p, err := k.readCM(CheckSum)
	if err != nil {
		return err
	}
	if p.Type != '+' {
		return ErrProtocol
	}
	k.Params = negotiate(host, parseBParams(p.Body))
	k.applyParams()
	return nil
}

// AnswerENQ is the terminal's side of the host's ENQ: it says it speaks
// B Plus when params is non-nil, else Classic B; then trades parameters.
func (k *BLink) AnswerENQ(params *BParams) error {
	for {
		c, err := k.l.getc(k.timeout())
		if err != nil {
			return err
		}
		if c == bENQ {
			break
		}
	}
	k.seq = '0'
	if params == nil {
		return k.l.write([]byte{bDLE, '0'})
	}
	k.Plus, k.PlusPlus = true, true
	if err := k.l.write([]byte{bDLE, '+', '+', bDLE, '0'}); err != nil {
		return err
	}
	k.quoteAll()
	p, err := k.readNoAck(CheckSum)
	if err != nil {
		return err
	}
	if p.Type != '+' {
		return ErrProtocol
	}
	host := parseBParams(p.Body)
	// our parameters ACK theirs
	if err := k.sendWait('+', params.bytes(), CheckSum); err != nil {
		return err
	}
	k.Params = negotiate(*params, host)
	k.applyParams()
	return nil
}

// readNoAck reads the next packet without ACKing it: the reply does.
func (k *BLink) readNoAck(cm byte) (BPacket, error) {
	errs := 0
	for {
		ev, err := k.read(cm)
		if err != nil && !errors.Is(err, ErrTimeout) {
			return BPacket{}, err
		}
		if err == nil && ev.kind == evPacket && ev.seq == next(k.seq) {
			k.seq = ev.seq
			return ev.pkt, nil
		}
		if err == nil && ev.kind == evPacket && ev.pkt.Type == 'F' {
			k.ack(ev.seq)
			return BPacket{}, failure(ev.pkt)
		}
		if errs++; errs >= k.maxErrors() {
			return BPacket{}, ErrRetries
		}
		k.l.write([]byte{bNAK})
	}
}

// sendWindow sends data packets of typ, as many outstanding as the
// window lets; a NAK or a timeout resends all not yet ACKed.
func (k *BLink) sendWindow(typ byte, r io.Reader) (int64, error) {
	win := int(k.Params.WS) + 1
	if !k.PlusPlus || win < 1 {
		win = 1
	}
	if win > 9 {
		win = 9
	}
	type sent struct {
		seq byte
		raw []byte
	}
	var out []sent
	var total int64
	eof := false
	buf := make([]byte, k.BlockSize())
	errs := 0
	resync := false
	for !eof || len(out) > 0 {
		for !resync && !eof && len(out) < win {
			n, err := io.ReadFull(r, buf)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				eof = true
				if n == 0 {
					break
				}
			} else if err != nil {
				k.Fail('I', "read error")
				return total, err
			}
			s := next(k.seq)
			b := k.encode(BPacket{Seq: s, Type: typ, Body: buf[:n]}, k.Params.CM)
			if err := k.l.write(b); err != nil {
				return total, err
			}
			k.seq = s
			out = append(out, sent{s, b})
			total += int64(n)
		}
		if len(out) == 0 {
			break
		}
		ev, err := k.read(k.Params.CM)
		if errors.Is(err, ErrTimeout) {
			ev, err = bEvent{kind: evNAK, seq: 'T'}, nil
		}
		if err != nil {
			return total, err
		}
		switch ev.kind {
		case evACK:
			for i, o := range out {
				if o.seq == ev.seq {
					out = out[i+1:]
					errs = 0
					break
				}
			}
			if resync {
				// the receiver's last good packet: resend those after it
				resync = false
				for _, o := range out {
					if err := k.l.write(o.raw); err != nil {
						return total, err
					}
				}
			}
			continue
		case evWACK:
			continue
		case evPacket:
			if ev.pkt.Type == 'F' {
				k.ack(ev.seq)
				return total, failure(ev.pkt)
			}
		case evENQ:
			k.ack(k.seq)
			continue
		case evNAK:
			if resync && !timedOut(ev) {
				continue // more NAKs of the same loss
			}
		}
		if errs++; errs >= k.maxErrors() {
			return total, ErrRetries
		}
		// resynchronize: ENQ asks for the receiver's last good packet
		resync = true
		if err := k.l.write([]byte{bENQ}); err != nil {
			return total, err
		}
	}
	return total, nil
}

// timedOut: the NAK stands for a timeout.
func timedOut(ev bEvent) bool { return ev.seq == 'T' }

// SendFile is a host's download: TD (binary, or ASCII), the data, TC.
func (k *BLink) SendFile(name string, binary bool, r io.Reader) (int64, error) {
	dt := byte('A')
	if binary {
		dt = 'B'
	}
	if err := k.SendPacket('T', append([]byte{'D', dt}, name...)); err != nil {
		return 0, err
	}
	if k.ahead != nil {
		// the terminal answered TD with a packet: Tr (resume, which we do
		// not offer) or F
		p := *k.ahead
		k.ahead = nil
		k.seq = p.Seq
		k.ack(p.Seq)
		if p.Type == 'F' {
			return 0, failure(p)
		}
	}
	n, err := k.sendWindow('N', r)
	if err != nil {
		return n, err
	}
	return n, k.close()
}

// close sends TC.  Every data packet has been ACKed by then; if only the
// TC's ACK is lost, the other side has the file and has left protocol
// mode, so no answer is not a failure.
func (k *BLink) close() error {
	err := k.SendPacket('T', []byte{'C'})
	if errors.Is(err, ErrRetries) || errors.Is(err, ErrTimeout) {
		return nil
	}
	return err
}

// ReceiveFile is a host's upload: TU (binary or ASCII) names the file to
// the terminal, which sends N packets and TC.  The terminal does not ACK
// TU: its first packet does.
func (k *BLink) ReceiveFile(name string, binary bool, w io.Writer, max int64) (int64, error) {
	dt := byte('A')
	if binary {
		dt = 'B'
	}
	s := next(k.seq)
	if err := k.send(BPacket{Seq: s, Type: 'T', Body: append([]byte{'U', dt}, name...)}, k.Params.CM); err != nil {
		return 0, err
	}
	k.seq = s
	k.unacked = k.last
	defer func() { k.unacked = nil }()
	var total int64
	for {
		p, err := k.ReadPacket()
		k.unacked = nil
		if err != nil {
			return total, err
		}
		switch {
		case p.Type == 'N':
			if max > 0 && total+int64(len(p.Body)) > max {
				k.Fail('C', "file too large")
				return total, errors.New("B: upload too large")
			}
			if _, err := w.Write(p.Body); err != nil {
				k.Fail('I', "write error")
				return total, err
			}
			total += int64(len(p.Body))
		case p.Type == 'T' && len(p.Body) > 0 && p.Body[0] == 'C':
			return total, nil
		case p.Type == 'F':
			return total, failure(p)
		default:
			k.Fail('N', "unexpected packet")
			return total, ErrProtocol
		}
	}
}

// Terminal is the terminal's side of transfers the host starts: it reads
// T packets and does them.  open gives a writer for a download (an error
// refuses it); upload gives the data of a file the host asks for.  It
// returns after a transfer completes.
func (k *BLink) Terminal(open func(name string, binary bool) (io.Writer, error),
	upload func(name string) (io.Reader, error)) error {
	p, err := k.readNoAck(k.Params.CM)
	if err != nil {
		return err
	}
	if p.Type != 'T' || len(p.Body) < 1 {
		k.ack(p.Seq)
		return ErrProtocol
	}
	switch p.Body[0] {
	case 'D':
		name := ""
		bin := false
		if len(p.Body) > 1 {
			bin = p.Body[1] != 'A'
			name = string(p.Body[2:])
		}
		w, err := open(name, bin)
		if err != nil {
			k.ack(p.Seq)
			k.Fail('E', err.Error())
			return err
		}
		k.ack(p.Seq)
		for {
			q, err := k.ReadPacket()
			if err != nil {
				return err
			}
			switch q.Type {
			case 'N':
				if _, err := w.Write(q.Body); err != nil {
					return err
				}
			case 'T':
				return nil
			default:
				return ErrProtocol
			}
		}
	case 'U':
		name := ""
		if len(p.Body) > 1 {
			name = string(p.Body[2:])
		}
		r, err := upload(name)
		if err != nil {
			k.Fail('M', err.Error())
			return err
		}
		// our first N packet ACKs the TU
		if _, err := k.sendWindow('N', r); err != nil {
			return err
		}
		return k.close()
	}
	k.ack(p.Seq)
	return ErrProtocol
}
