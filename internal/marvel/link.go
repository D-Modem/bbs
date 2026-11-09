package marvel

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// A link carries whole pipe messages each way: a message begins with its
// routing word (a pipe's number, 0 for a pipe open, 0xFFFF for control).

// maxPacket is the most wire bytes a packet may take: what the transport
// parameters offer, and the client's own default.
const maxPacket = 1024

// The Go-Back-N link's timing: a packet not acknowledged in rto is sent
// again, with everything after it, up to retries times; then the sender
// stops resending (the client's own timers decide whether the call lives).
var (
	rto     = 6 * time.Second
	retries = 12
)

// window is the most packets sent and not yet acknowledged: what the
// transport parameters offer.  The rest wait their turn.
const window = 16

// pipe frame header bits
const (
	frameAlways   = 0x80
	frameLength   = 0x10 // a one-byte length follows (not in continuation form)
	frameContinue = 0x20 // the rest of the packet is this frame
	frameLast     = 0x40 // the message ends in this frame
	frameIndex    = 0x0F // the receiver's reassembly buffer
)

// frameHeader is a continuation-form frame header for reassembly buffer idx.
func frameHeader(idx int, last bool) byte {
	h := byte(frameAlways | frameContinue | idx&frameIndex)
	if last {
		h |= frameLast
	}
	return headerByte(h)
}

// reassembler puts the client's messages back together from its frames.
type reassembler struct {
	owed    [16]int    // bytes a message still owes, by reassembly index
	buf     [16][]byte // what it has so far
	pending []byte     // a stuffed payload ending in a split escape
}

// packet takes one data packet's stuffed payload, and returns the messages
// it completes.
func (r *reassembler) packet(st []byte) [][]byte {
	var payloads [][]byte
	if len(r.pending) > 0 {
		// the escape's second byte is the first after this packet's own
		// frame header
		if len(st) < 2 {
			return nil
		}
		payloads = append(payloads, unstuff(append(r.pending, st[1])))
		r.pending = nil
		st = append([]byte{st[0]}, st[2:]...)
	}
	if len(st) > 0 {
		if st[len(st)-1] == escape {
			r.pending = append([]byte(nil), st...)
		} else {
			payloads = append(payloads, unstuff(st))
		}
	}
	var msgs [][]byte
	for _, p := range payloads {
		msgs = append(msgs, r.frames(p)...)
	}
	return msgs
}

// frames reads the frames of one unstuffed payload.
func (r *reassembler) frames(p []byte) [][]byte {
	var msgs [][]byte
	for len(p) > 0 {
		h := unheaderByte(p[0])
		idx := int(h & frameIndex)
		pos := 1
		var data []byte
		cont := h&frameContinue != 0
		if cont {
			data = p[1:]
		} else {
			if len(p) < 2 {
				return msgs
			}
			n := int(unheaderByte(p[1]) & 0x7F)
			pos = 2 + n
			data = p[2:min(2+n, len(p))]
			if pos > len(p) {
				pos = len(p)
			}
		}
		var content []byte
		consumed := 0
		if owed := r.owed[idx]; owed > 0 {
			content = data[:min(owed, len(data))]
			consumed = len(content)
			r.owed[idx] = owed - len(content)
		} else {
			if len(data) < 2 {
				return msgs
			}
			n := int(binary.LittleEndian.Uint16(data))
			content = data[2:min(2+n, len(data))]
			consumed = 2 + len(content)
			r.owed[idx] = n - len(content)
		}
		r.buf[idx] = append(r.buf[idx], content...)
		if r.owed[idx] == 0 {
			msgs = append(msgs, r.buf[idx])
			r.buf[idx] = nil
		}
		if cont {
			pos += consumed
		}
		p = p[pos:]
	}
	return msgs
}

// framePackets splits one message into packets of at most maxPacket wire
// bytes, from sequence number seq: the first frame carries the message's
// whole length, the last has frameLast.
func framePackets(msg []byte, idx int, seq, ack byte) [][]byte {
	one := append([]byte{frameHeader(idx, true), 0, 0}, msg...)
	binary.LittleEndian.PutUint16(one[1:], uint16(len(msg)))
	if p := dataPacket(seq, ack, one); len(p) <= maxPacket {
		return [][]byte{p}
	}
	var out [][]byte
	var prefix [2]byte
	binary.LittleEndian.PutUint16(prefix[:], uint16(len(msg)))
	for pos, first := 0, true; pos < len(msg); first = false {
		// header, CRC and terminator around the stuffed frame
		size := 2 + 1 + 4 + 1
		if first {
			size += stuffedLen(prefix[:])
		}
		n := 0
		for pos+n < len(msg) {
			w := 1
			if _, ok := stuffed[msg[pos+n]]; ok {
				w = 2
			}
			if size+w > maxPacket {
				break
			}
			size += w
			n++
		}
		last := pos+n == len(msg)
		frame := []byte{frameHeader(idx, last)}
		if first {
			frame = append(frame, prefix[:]...)
		}
		frame = append(frame, msg[pos:pos+n]...)
		out = append(out, dataPacket(seq, ack, frame))
		seq = (seq + 1) & 0x7F
		pos += n
	}
	return out
}

func stuffedLen(b []byte) int {
	n := len(b)
	for _, x := range b {
		if _, ok := stuffed[x]; ok {
			n++
		}
	}
	return n
}

// reassemblyIndex is the buffer a message goes in: its pipe's number
// (one message at a time is in flight on a pipe), 0 for control.
func reassemblyIndex(msg []byte) int {
	if len(msg) < 2 {
		return 0
	}
	r := binary.LittleEndian.Uint16(msg)
	if r == 0xFFFF {
		return 0
	}
	return int(r & frameIndex)
}

// link is a connection's framing, either kind.
type link interface {
	// send sends one whole message.
	send(msg []byte) error
	// recv waits for the next whole message.
	recv() ([]byte, error)
	close() error
}

// selectLink is the serial framing, with sequence numbers, ACKs and
// retransmission.
type selectLink struct {
	c   io.ReadWriteCloser
	rd  *bufio.Reader
	log func(string, ...any)

	mu      sync.Mutex
	seq     byte // the next sequence number to send
	ack     byte // the next one expected from the client
	unacked []sent
	waiting [][]byte // packets made but not yet sent: the window is full
	timer   *time.Timer
	closed  bool

	reasm reassembler
	queue [][]byte // messages received but not yet taken
}

type sent struct {
	seq   byte
	pkt   []byte
	at    time.Time
	tries int
}

func newSelectLink(c io.ReadWriteCloser, rd *bufio.Reader, logf func(string, ...any)) *selectLink {
	return &selectLink{c: c, rd: rd, log: logf}
}

func (l *selectLink) send(msg []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return net.ErrClosed
	}
	pkts := framePackets(msg, reassemblyIndex(msg), l.seq, l.ack)
	l.seq = (l.seq + byte(len(pkts))) & 0x7F
	l.waiting = append(l.waiting, pkts...)
	err := l.fill()
	l.arm()
	return err
}

// fill sends what waits, as far as the window allows.
func (l *selectLink) fill() error {
	for len(l.waiting) > 0 && len(l.unacked) < window {
		p := l.waiting[0]
		l.waiting = l.waiting[1:]
		l.unacked = append(l.unacked, sent{seq: unheaderByte(p[0]) & 0x7F, pkt: p, at: time.Now()})
		if _, err := l.c.Write(p); err != nil {
			return err
		}
	}
	return nil
}

// arm starts the retransmission timer if anything waits for an ACK.
func (l *selectLink) arm() {
	if len(l.unacked) == 0 || l.timer != nil {
		return
	}
	l.timer = time.AfterFunc(rto, l.timeout)
}

// timeout sends everything unacknowledged again (Go-Back-N).
func (l *selectLink) timeout() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.timer = nil
	if l.closed || len(l.unacked) == 0 {
		return
	}
	if l.unacked[0].tries >= retries {
		l.log("no ACK after %d tries; not resending", retries)
		l.unacked, l.waiting = nil, nil
		return
	}
	l.resend()
	l.arm()
}

// resend sends the unacknowledged packets again, in order.
func (l *selectLink) resend() {
	for i := range l.unacked {
		l.unacked[i].tries++
		l.unacked[i].at = time.Now()
		l.c.Write(l.unacked[i].pkt)
	}
}

// acked drops the packets the client has: all those before ack.
func (l *selectLink) acked(ack byte) {
	moved := false
	for len(l.unacked) > 0 {
		d := (ack - l.unacked[0].seq) & 0x7F
		if d == 0 || d > 64 {
			break
		}
		l.unacked = l.unacked[1:]
		moved = true
	}
	if !moved {
		return
	}
	// the line is moving: the timer starts again for what is left
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	l.fill()
	l.arm()
}

func (l *selectLink) recv() ([]byte, error) {
	for len(l.queue) == 0 {
		raw, err := l.rd.ReadBytes(terminator)
		if err != nil {
			return nil, err
		}
		raw = raw[:len(raw)-1]
		if len(raw) == 0 {
			continue // a bare CR between packets
		}
		p, err := parsePacket(raw)
		if err != nil {
			l.log("dropped a packet: %v", err)
			continue
		}
		l.mu.Lock()
		l.acked(p.ack)
		switch p.kind {
		case 'N':
			l.resend()
			l.mu.Unlock()
			continue
		case 'A':
			l.mu.Unlock()
			continue
		}
		if p.seq != l.ack {
			// a packet sent again, or one after a gap: say what is wanted
			l.c.Write(ackPacket(l.ack))
			l.mu.Unlock()
			continue
		}
		l.ack = (l.ack + 1) & 0x7F
		l.c.Write(ackPacket(l.ack))
		l.mu.Unlock()
		l.queue = append(l.queue, l.reasm.packet(p.stuffed)...)
	}
	m := l.queue[0]
	l.queue = l.queue[1:]
	return m, nil
}

func (l *selectLink) close() error {
	l.mu.Lock()
	l.closed = true
	if l.timer != nil {
		l.timer.Stop()
	}
	l.mu.Unlock()
	return l.c.Close()
}

// straightLink is the TCP framing.
type straightLink struct {
	c   io.ReadWriteCloser
	mu  sync.Mutex
	buf []byte
	q   [][]byte
}

func (l *straightLink) send(msg []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := l.c.Write(straightRecord(msg))
	return err
}

func (l *straightLink) recv() ([]byte, error) {
	var b [4096]byte
	for len(l.q) == 0 {
		n, err := l.c.Read(b[:])
		l.buf = append(l.buf, b[:n]...)
		msgs, rest, perr := takeStraight(l.buf)
		l.q, l.buf = append(l.q, msgs...), append([]byte(nil), rest...)
		if perr != nil {
			return nil, perr
		}
		if len(l.q) > 0 {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	m := l.q[0]
	l.q = l.q[1:]
	return m, nil
}

func (l *straightLink) close() error { return l.c.Close() }

var errHandshake = errors.New("no MSN client")
