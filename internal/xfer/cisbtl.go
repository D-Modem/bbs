package xfer

// The B Plus Transport Layer: what CompuServe's HMI (CIM's binary service)
// runs on.  Packets are B Plus's, but each side numbers its own (0-9, the
// ENQ answer's DLE '0' standing for the host's 0, so its first is 1), and
// messages travel as 'M' packets (more follows) ended by an 'L' packet.
// As WinCIM 3.0.2 does it, recovered by 24high/CompuServe-Emulator
// (HMI-PROTOKOLL.md, SECURE-LOGIN.md):
//
//	H→C  ENQ
//	C→H  DLE '+' '+' DLE '0'
//	H→C  '+' 04 04 08 00 00 01    (TL, the last, must be set; a short block)
//	C→H  ACK, then its own '+'
//	...  'M'/'L' packets both ways, each ACKed

import (
	"errors"
	"time"
)

// TLParams are the '+' packet the host sends a CIM: windows of 4, 1 KB
// blocks, the checksum, the Transport Layer.  It must be short: before
// the trade, WinCIM refuses packets of more than about 8 bytes.
var TLParams = []byte{4, 4, 8, CheckSum, 0, 1}

// ErrTLFailed is an 'F' packet from the other side.
var ErrTLFailed = errors.New("cisb: the transport layer was ended")

// BTransport is the Transport Layer on a line.
type BTransport struct {
	k       *BLink
	tx      byte   // the sequence digit of the last packet sent
	rx      byte   // of the last taken (0: none yet)
	Their   []byte // the other side's '+' packet body
	queue   [][]byte
	partial []byte
	max     int // the most data a packet carries
}

// NewBTransport starts the Transport Layer as the host: ENQ, the B Plus
// answer, the trade of '+' packets.
func NewBTransport(p Port, timeout time.Duration) (*BTransport, error) {
	k := NewBLink(p)
	k.Timeout = timeout
	k.quoteAll() // quoting more than asked does no harm; less does
	t := &BTransport{k: k, tx: '0', max: 1023}
	if err := t.enq(); err != nil {
		return nil, err
	}
	if err := t.send('+', TLParams); err != nil {
		return nil, err
	}
	if t.Their == nil && len(t.queue) == 0 {
		// its '+' (unless it came while ours waited for its ACK)
		m, typ, err := t.packet()
		if err != nil {
			return nil, err
		}
		if err := t.take(typ, m); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// enq sends ENQ until the B Plus answer comes, up to 5 times.
func (t *BTransport) enq() error {
	for try := 0; try < 5; try++ {
		if err := t.k.l.write([]byte{bENQ}); err != nil {
			return err
		}
		deadline := time.Now().Add(3 * time.Second)
		var got []byte
		for time.Now().Before(deadline) {
			c, err := t.k.l.getc(time.Until(deadline))
			if errors.Is(err, ErrTimeout) {
				break
			}
			if err != nil {
				return err
			}
			got = append(got, c)
			if n := len(got); n >= 5 && string(got[n-5:]) == "\x10++\x100" {
				return nil
			}
		}
	}
	return ErrTimeout
}

// send sends one packet and waits for its ACK, sending again on a NAK or
// silence; a packet that comes meanwhile is taken (and ACKed).
func (t *BTransport) send(typ byte, body []byte) error {
	s := next(t.tx)
	b := t.k.encode(BPacket{Seq: s, Type: typ, Body: body}, CheckSum)
	errs := 0
	if err := t.k.l.write(b); err != nil {
		return err
	}
	for {
		ev, err := t.k.read(CheckSum)
		if errors.Is(err, ErrTimeout) {
			ev, err = bEvent{kind: evNAK}, nil
		}
		if err != nil {
			return err
		}
		switch ev.kind {
		case evACK:
			if ev.seq == s {
				t.tx = s
				return nil
			}
			continue // an old one
		case evPacket:
			if err := t.received(ev.pkt); err != nil {
				return err
			}
			continue
		case evENQ:
			// the other side lost its place: say where we are
			t.k.ack(t.rx)
			continue
		}
		// a NAK, a bad packet or silence: again
		if errs++; errs > t.k.maxErrors() {
			return ErrRetries
		}
		if err := t.k.l.write(b); err != nil {
			return err
		}
	}
}

// received takes a packet that came: a new one is ACKed and kept, the
// last one again is ACKed again.
func (t *BTransport) received(p BPacket) error {
	if t.rx != 0 && p.Seq == t.rx {
		return t.k.ack(p.Seq) // our ACK was lost
	}
	t.rx = p.Seq
	if err := t.k.ack(p.Seq); err != nil {
		return err
	}
	return t.take(p.Type, p.Body)
}

// take keeps a packet's data: 'M' and 'L' make up messages.
func (t *BTransport) take(typ byte, body []byte) error {
	switch typ {
	case 'M', 'N':
		t.partial = append(t.partial, body...)
	case 'L':
		m := append(t.partial, body...)
		t.partial = nil
		t.queue = append(t.queue, m)
	case 'F':
		return ErrTLFailed
	case '+':
		t.Their = body
	}
	return nil
}

// packet reads the next new packet (ACKing it).
func (t *BTransport) packet() ([]byte, byte, error) {
	for {
		ev, err := t.k.read(CheckSum)
		if err != nil {
			return nil, 0, err
		}
		switch ev.kind {
		case evPacket:
			if t.rx != 0 && ev.seq == t.rx {
				t.k.ack(ev.seq)
				continue
			}
			t.rx = ev.seq
			if err := t.k.ack(ev.seq); err != nil {
				return nil, 0, err
			}
			return ev.pkt.Body, ev.pkt.Type, nil
		case evBad:
			t.k.l.write([]byte{bNAK})
		case evENQ:
			t.k.ack(t.rx)
		}
	}
}

// Recv is the next message, waiting at most d (0: the link's timeout);
// ErrTimeout when none came.
func (t *BTransport) Recv(d time.Duration) ([]byte, error) {
	old := t.k.Timeout
	if d > 0 {
		t.k.Timeout = d
		defer func() { t.k.Timeout = old }()
	}
	for len(t.queue) == 0 {
		body, typ, err := t.packet()
		if err != nil {
			return nil, err
		}
		if err := t.take(typ, body); err != nil {
			return nil, err
		}
	}
	m := t.queue[0]
	t.queue = t.queue[1:]
	return m, nil
}

// Send sends a message, as 'M' packets and a last 'L'.
func (t *BTransport) Send(m []byte) error {
	for len(m) > t.max {
		if err := t.send('M', m[:t.max]); err != nil {
			return err
		}
		m = m[t.max:]
	}
	return t.send('L', m)
}

// Fail ends the Transport Layer with an 'F' packet.
func (t *BTransport) Fail(text string) error {
	return t.send('F', append([]byte{'E'}, text...))
}

// AnswerTL is the terminal's side of the start, for tests: the ENQ
// answer, then the trade of '+' packets.
func AnswerTL(p Port, params []byte, timeout time.Duration) (*BTransport, error) {
	k := NewBLink(p)
	k.Timeout = timeout
	k.quoteAll()
	t := &BTransport{k: k, tx: '0', max: 1023}
	for {
		c, err := k.l.getc(timeout)
		if err != nil {
			return nil, err
		}
		if c == bENQ {
			break
		}
	}
	if err := k.l.write([]byte{bDLE, '+', '+', bDLE, '0'}); err != nil {
		return nil, err
	}
	body, typ, err := t.packet()
	if err != nil {
		return nil, err
	}
	if typ != '+' {
		return nil, errors.New("cisb: no '+' packet from the host")
	}
	t.Their = body
	if err := t.send('+', params); err != nil {
		return nil, err
	}
	return t, nil
}
