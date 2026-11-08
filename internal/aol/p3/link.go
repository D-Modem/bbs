package p3

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// Options tune a Link; the zero value is the client's own timings.
type Options struct {
	// Timeout is how long DATA may go unacknowledged before a HEARTBEAT
	// asks (12 seconds), and Retries how many are asked before the link is
	// given up (10).
	Timeout time.Duration
	Retries int

	// Window is the most DATA unacknowledged at once (16); AckEvery the
	// most DATA taken in before it is acknowledged (8), and AckDelay how
	// long a lone DATA waits for an answer to carry its acknowledgement
	// before an ACK is sent (200ms).
	Window   int
	AckEvery int
	AckDelay time.Duration

	// InitWait is how long Serve waits for the client's INIT (30 seconds).
	InitWait time.Duration

	// Logf, if set, is told of what goes wrong on the link.
	Logf func(format string, args ...any)

	// Trace, if set, is given every packet: in, true for one from the
	// client.
	Trace func(in bool, p Packet)
}

func (o *Options) defaults() {
	if o.Timeout <= 0 {
		o.Timeout = 12 * time.Second
	}
	if o.Retries <= 0 {
		o.Retries = 10
	}
	if o.Window <= 0 || o.Window > seqRing/2 {
		o.Window = 16
	}
	if o.AckEvery <= 0 {
		o.AckEvery = 8
	}
	if o.AckDelay <= 0 {
		o.AckDelay = 200 * time.Millisecond
	}
	if o.InitWait <= 0 {
		o.InitWait = 30 * time.Second
	}
}

// Errors on a link.
var (
	ErrClosed  = errors.New("p3: link closed")
	ErrTimeout = errors.New("p3: the client stopped acknowledging")
	ErrNoInit  = errors.New("p3: the client sent no INIT")
	ErrTooBig  = fmt.Errorf("p3: more than %d bytes of data", MaxData)
)

// Link is the host's end of a P3 link: DATA in order in, DATA out within
// the window, acknowledged, NAKed and resent as P3 does.
type Link struct {
	conn io.ReadWriteCloser
	r    *Reader
	opt  Options

	mu      sync.Mutex
	room    *sync.Cond // the window has room, or the link is down
	init    Init
	tx      uint8  // the last DATA sent
	rx      uint8  // the last DATA taken in
	out     []sent // DATA sent, not yet acknowledged
	pending int    // DATA taken in, not yet acknowledged
	nakked  bool   // a NAK is out for the gap in what came in
	hbTries int    // HEARTBEATs since the last acknowledgement
	ackDue  bool   // an ACK is to go if nothing else does first
	resent  time.Time
	ackT    *time.Timer
	hbT     *time.Timer

	err error // why the link is down: set before done closes

	wq     chan []byte   // packets to write, in order
	in     chan []byte   // DATA come in, in order
	done   chan struct{} // closed when the link is down
	closed sync.Once
}

type sent struct {
	seq  uint8
	data []byte
}

// Serve starts the host's end of a link on conn, a connection on which an
// AOL client has begun P3: it waits for the client's INIT, acknowledges
// it, and the link is up.  Close the link to close conn.
func Serve(conn io.ReadWriteCloser, opt Options) (*Link, error) {
	opt.defaults()
	l := &Link{
		conn: conn, r: NewReader(conn), opt: opt,
		tx: SeqInit, rx: SeqInit,
		wq: make(chan []byte, 256), in: make(chan []byte, 64), done: make(chan struct{}),
	}
	l.room = sync.NewCond(&l.mu)
	type deadliner interface{ SetReadDeadline(time.Time) error }
	dl, _ := conn.(deadliner)
	if dl != nil {
		dl.SetReadDeadline(time.Now().Add(opt.InitWait))
	}
	for {
		p, err := l.r.Read()
		if err == ErrCRC || err == ErrLength {
			continue // the client sends its INIT again
		}
		if err != nil {
			conn.Close()
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, ErrNoInit
			}
			return nil, fmt.Errorf("%w: %v", ErrNoInit, err)
		}
		l.trace(true, p)
		if p.Kind() == INIT {
			l.init = ParseInit(p.Data)
			l.rx = p.TX
			break
		}
	}
	if dl != nil {
		dl.SetReadDeadline(time.Time{})
	}
	go l.writer()
	l.mu.Lock()
	l.send(ACK, nil)
	l.mu.Unlock()
	go l.reader()
	return l, nil
}

// Init is what the client said of itself.
func (l *Link) Init() Init {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.init
}

// Read gives the next DATA packet's data from the client.
func (l *Link) Read() ([]byte, error) {
	select {
	case d := <-l.in:
		return d, nil
	case <-l.done:
		select {
		case d := <-l.in:
			return d, nil
		default:
		}
		return nil, l.Err()
	}
}

// Write sends data to the client in one DATA packet, waiting while the
// window is full.
func (l *Link) Write(data []byte) error {
	if len(data) > MaxData {
		return ErrTooBig
	}
	data = append([]byte(nil), data...)
	l.mu.Lock()
	defer l.mu.Unlock()
	for !l.down() && len(l.out) >= l.opt.Window {
		l.room.Wait()
	}
	if l.down() {
		return l.err
	}
	l.tx = next(l.tx)
	l.out = append(l.out, sent{l.tx, data})
	if len(l.out) == 1 {
		l.armHeartbeat()
	}
	l.send(DATA, data)
	return nil
}

// WriteToken sends a token and what follows it in one DATA packet.
func (l *Link) WriteToken(token string, data []byte) error {
	if len(token) != 2 {
		return fmt.Errorf("p3: token %q is not two letters", token)
	}
	return l.Write(append([]byte(token), data...))
}

// Unacked is how many DATA packets the client has yet to acknowledge.
func (l *Link) Unacked() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.out)
}

// Close takes the link down and closes its connection, once what has been
// written has gone (or a second has passed).
func (l *Link) Close() error {
	select {
	case l.wq <- nil: // the writer closes when it comes to it
		select {
		case <-l.done:
		case <-time.After(time.Second):
		}
	case <-l.done:
	case <-time.After(time.Second):
	}
	l.fail(ErrClosed)
	return nil
}

// Done is closed when the link is down.
func (l *Link) Done() <-chan struct{} { return l.done }

// Err is why the link is down, or nil.
func (l *Link) Err() error {
	if l.down() {
		return l.err
	}
	return nil
}

func (l *Link) down() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

// fail takes the link down for err, the first time.  It is called without
// l.mu: a sender holding it may be waiting on the writer.
func (l *Link) fail(err error) {
	first := false
	l.closed.Do(func() {
		first = true
		l.err = err
		close(l.done)
		l.conn.Close()
	})
	if first {
		if err != ErrClosed {
			l.logf("link down: %v", err)
		}
		l.mu.Lock()
		l.stopTimers()
		l.room.Broadcast()
		l.mu.Unlock()
	}
}

func (l *Link) stopTimers() {
	if l.ackT != nil {
		l.ackT.Stop()
	}
	if l.hbT != nil {
		l.hbT.Stop()
	}
}

func (l *Link) logf(format string, args ...any) {
	if l.opt.Logf != nil {
		l.opt.Logf("p3: "+format, args...)
	}
}

func (l *Link) trace(in bool, p Packet) {
	if l.opt.Trace != nil {
		l.opt.Trace(in, p)
	}
}

// send numbers a packet and queues it to be written; l.mu is held.  Every
// packet carries the acknowledgement of what has come in.
func (l *Link) send(kind uint8, data []byte) {
	l.sendSeq(l.tx, kind, data)
}

func (l *Link) sendSeq(tx, kind uint8, data []byte) {
	if l.down() {
		return
	}
	p := Packet{TX: tx, RX: l.rx, Type: kind, Data: data}
	l.pending, l.ackDue = 0, false
	l.trace(false, p)
	select {
	case l.wq <- Append(nil, p):
	case <-l.done:
	}
}

func (l *Link) writer() {
	type deadliner interface{ SetWriteDeadline(time.Time) error }
	dl, _ := l.conn.(deadliner)
	for {
		select {
		case b := <-l.wq:
			if b == nil {
				l.fail(ErrClosed)
				return
			}
			if dl != nil {
				dl.SetWriteDeadline(time.Now().Add(l.opt.Timeout * time.Duration(l.opt.Retries)))
			}
			if _, err := l.conn.Write(b); err != nil {
				l.fail(err)
				return
			}
		case <-l.done:
			return
		}
	}
}

func (l *Link) reader() {
	for {
		p, err := l.r.Read()
		switch {
		case err == ErrCRC:
			l.trace(true, p)
			l.nak(NakCRC)
			continue
		case err == ErrLength:
			l.nak(NakLength)
			continue
		case err != nil:
			l.fail(err)
			return
		}
		l.trace(true, p)
		if d := l.take(p); d != nil {
			select {
			case l.in <- d:
			case <-l.done:
				return
			}
		}
	}
}

// nak asks the client to resend from the first DATA after our rx, once
// for each gap in what came in.
func (l *Link) nak(reason uint8) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.nakked {
		l.nakked = true
		l.send(NAK, []byte{reason})
	}
}

// take handles a packet from the client, and gives DATA's data to be
// read.
func (l *Link) take(p Packet) []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	if p.Kind() == INIT {
		// the client has started again (or missed our ACK): so do we
		if len(l.out) > 0 || l.rx != p.TX {
			l.logf("INIT again, %d packets unsent", len(l.out))
		}
		l.init = ParseInit(p.Data)
		l.tx, l.rx, l.out, l.nakked, l.hbTries = SeqInit, p.TX, nil, false, 0
		l.stopTimers()
		l.room.Broadcast()
		l.send(ACK, nil)
		return nil
	}
	heartbeat := l.hbTries > 0
	l.acked(p.RX)
	switch p.Kind() {
	case DATA:
		switch {
		case p.TX == next(l.rx):
			l.rx = p.TX
			l.nakked = false
			l.pending++
			if l.pending >= l.opt.AckEvery {
				l.send(ACK, nil)
			} else {
				l.armAck()
			}
			return p.Data
		case behind(p.TX, l.rx) < seqRing/2:
			// one we have: our acknowledgement went astray
			l.armAck()
		default:
			if !l.nakked {
				l.nakked = true
				l.send(NAK, []byte{NakSequence})
			}
		}
	case ACK:
		if heartbeat {
			l.resend(p.RX)
		}
	case NAK:
		if len(p.Data) > 0 && p.Data[0] != NakSequence {
			l.logf("NAK %d from rx %02x", p.Data[0], p.RX)
		}
		l.resend(p.RX)
	case HEARTBEAT:
		l.send(ACK, nil)
	case SS:
		l.send(SSR, nil)
	case SSR:
		l.resend(p.RX)
	default:
		l.logf("packet type %02x", p.Type)
	}
	return nil
}

// acked takes rx as the client's acknowledgement of our DATA up to it.
func (l *Link) acked(rx uint8) {
	for i, s := range l.out {
		if s.seq == rx {
			l.out = l.out[i+1:]
			l.hbTries = 0
			if len(l.out) > 0 {
				l.armHeartbeat()
			} else if l.hbT != nil {
				l.hbT.Stop()
			}
			l.room.Broadcast()
			return
		}
	}
}

// resend sends again all the DATA not acknowledged (go back N), but not
// twice over for one loss.
func (l *Link) resend(rx uint8) {
	if len(l.out) == 0 || time.Since(l.resent) < l.opt.AckDelay {
		return
	}
	l.resent = time.Now()
	for _, s := range l.out {
		l.sendSeq(s.seq, DATA, s.data)
	}
	l.armHeartbeat()
}

// armAck has an ACK sent soon, unless another packet carries the
// acknowledgement first.
func (l *Link) armAck() {
	if l.ackDue {
		return
	}
	l.ackDue = true
	if l.ackT == nil {
		l.ackT = time.AfterFunc(l.opt.AckDelay, func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.ackDue {
				l.send(ACK, nil)
			}
		})
	} else {
		l.ackT.Reset(l.opt.AckDelay)
	}
}
func (l *Link) armHeartbeat() {
	if l.hbT == nil {
		l.hbT = time.AfterFunc(l.opt.Timeout, l.heartbeat)
	} else {
		l.hbT.Reset(l.opt.Timeout)
	}
}

func (l *Link) heartbeat() {
	l.mu.Lock()
	if l.down() || len(l.out) == 0 {
		l.mu.Unlock()
		return
	}
	if l.hbTries >= l.opt.Retries {
		l.mu.Unlock()
		l.fail(ErrTimeout)
		return
	}
	l.hbTries++
	l.send(HEARTBEAT, nil)
	l.hbT.Reset(l.opt.Timeout)
	l.mu.Unlock()
}
