// Package telnet is the server side of the telnet protocol, enough for a
// BBS: echo and suppress go-ahead for character-at-a-time input, the
// terminal type and window size, binary, and telling TN3270 clients from
// the rest so they can be handed to go3270.
package telnet

import (
	"bufio"
	"errors"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Commands and options.
const (
	SE   = 240
	NOP  = 241
	GA   = 249
	SB   = 250
	WILL = 251
	WONT = 252
	DO   = 253
	DONT = 254
	IAC  = 255

	OptBinary = 0
	OptEcho   = 1
	OptSGA    = 3
	OptTType  = 24
	OptEOR    = 25
	OptNAWS   = 31

	ttypeIs   = 0
	ttypeSend = 1
)

const (
	maxSub   = 256  // longest subnegotiation kept
	maxEarly = 4096 // data kept while negotiating
)

// Conn is a telnet connection: Read returns the client's data with the
// commands taken out (and answered), Write escapes IAC.
type Conn struct {
	net.Conn
	r   *bufio.Reader
	wmu sync.Mutex

	mu      sync.Mutex
	us, him [256]bool // options in effect: we do, the client does
	askUs   [256]bool // a WILL or WONT of ours is unanswered
	askHim  [256]bool // a DO or DONT of ours is unanswered
	ttype   string
	cols    int
	rows    int
	gotType bool
	sawIAC  bool

	state  int
	cmd    byte
	sub    []byte
	lastCR bool
	early  []byte // data that came while negotiating
	out    []byte // answers to send, in order
}

const (
	stData = iota
	stIAC
	stOpt
	stSub
	stSubIAC
)

// New wraps a connection.
func New(c net.Conn) *Conn {
	return &Conn{Conn: c, r: bufio.NewReaderSize(c, 4096)}
}

// TermType is the terminal type the client gave ("" if none).
func (c *Conn) TermType() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ttype
}

// Size is the window size the client gave (0, 0 if none).
func (c *Conn) Size() (cols, rows int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cols, c.rows
}

// Spoke reports whether the client has sent any telnet command: false for
// a raw client (a terminal program on a modem, netcat) on a telnet port.
func (c *Conn) Spoke() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sawIAC
}

// Binary reports whether both directions are binary (8-bit clean).
func (c *Conn) Binary() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.us[OptBinary] && c.him[OptBinary]
}

// Read returns the client's data.
func (c *Conn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.mu.Lock()
	if len(c.early) > 0 {
		n := copy(p, c.early)
		c.early = c.early[n:]
		c.mu.Unlock()
		return n, nil
	}
	c.mu.Unlock()
	n := 0
	for n == 0 || (n < len(p) && c.r.Buffered() > 0) {
		b, err := c.r.ReadByte()
		if err != nil {
			if n > 0 {
				return n, nil
			}
			return 0, err
		}
		d, ok := c.input(b)
		if err := c.flush(); err != nil {
			return n, err
		}
		if ok {
			p[n] = d
			n++
		}
	}
	return n, nil
}

// flush sends the answers input queued.
func (c *Conn) flush() error {
	c.mu.Lock()
	out := c.out
	c.out = nil
	c.mu.Unlock()
	if len(out) == 0 {
		return nil
	}
	_, err := c.send(out)
	return err
}

// Write sends data, IAC doubled.
func (c *Conn) Write(p []byte) (int, error) {
	out := p
	for i, b := range p {
		if b == IAC {
			out = make([]byte, 0, len(p)+8)
			out = append(out, p[:i]...)
			for _, b := range p[i:] {
				out = append(out, b)
				if b == IAC {
					out = append(out, IAC)
				}
			}
			break
		}
	}
	if _, err := c.send(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// send writes b to the connection as it is, one writer at a time, so that
// answers to the client are not cut into the BBS's output.
func (c *Conn) send(b []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.Conn.Write(b)
}

// input runs one byte through the protocol: the data byte, if it is one.
func (c *Conn) input(b byte) (byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.state {
	case stData:
		if b == IAC {
			c.sawIAC = true
			c.state = stIAC
			return 0, false
		}
		// CR NUL is a bare CR; CR LF is passed as it is
		// CR NUL is a bare CR in text; in binary mode (a file transfer's)
		// a NUL is a NUL
		if b == 0 && c.lastCR && !c.him[OptBinary] {
			c.lastCR = false
			return 0, false
		}
		c.lastCR = b == '\r'
		return b, true
	case stIAC:
		switch b {
		case IAC:
			c.state = stData
			c.lastCR = false
			return IAC, true
		case WILL, WONT, DO, DONT:
			c.cmd, c.state = b, stOpt
		case SB:
			c.sub, c.state = c.sub[:0], stSub
		default: // NOP, GA, AYT, ...: nothing to do
			c.state = stData
		}
	case stOpt:
		c.option(c.cmd, b)
		c.state = stData
	case stSub:
		if b == IAC {
			c.state = stSubIAC
		} else if len(c.sub) < maxSub {
			c.sub = append(c.sub, b)
		}
	case stSubIAC:
		switch b {
		case SE:
			c.subneg()
			c.state = stData
		case IAC:
			if len(c.sub) < maxSub {
				c.sub = append(c.sub, IAC)
			}
			c.state = stSub
		default:
			// a broken subnegotiation: drop it
			c.state = stData
		}
	}
	return 0, false
}

// what the client may do, and what we will do when asked
func acceptHim(opt byte) bool {
	return opt == OptTType || opt == OptNAWS || opt == OptSGA || opt == OptBinary
}

// acceptUs: we will echo, suppress go-ahead and send binary when the client
// asks.
func acceptUs(opt byte) bool {
	return opt == OptEcho || opt == OptSGA || opt == OptBinary
}

// option handles WILL, WONT, DO and DONT, so that no answer is sent to an
// answer (RFC 854's rule against loops).  c.mu is held.
func (c *Conn) option(cmd, opt byte) {
	switch cmd {
	case WILL:
		switch {
		case c.askHim[opt]:
			c.askHim[opt] = false
			c.him[opt] = true
		case c.him[opt]:
			return
		case acceptHim(opt):
			c.him[opt] = true
			c.queue(IAC, DO, opt)
		default:
			c.queue(IAC, DONT, opt)
		}
		if opt == OptTType && c.him[opt] {
			c.queue(IAC, SB, OptTType, ttypeSend, IAC, SE)
		}
	case WONT:
		if c.askHim[opt] {
			c.askHim[opt] = false
		} else if c.him[opt] {
			c.queue(IAC, DONT, opt)
		}
		c.him[opt] = false
		if opt == OptTType {
			c.gotType = true // refused: nothing is coming
		}
	case DO:
		switch {
		case c.askUs[opt]:
			c.askUs[opt] = false
			c.us[opt] = true
		case c.us[opt]:
		case acceptUs(opt):
			c.us[opt] = true
			c.queue(IAC, WILL, opt)
		default:
			c.queue(IAC, WONT, opt)
		}
	case DONT:
		if c.askUs[opt] {
			c.askUs[opt] = false
		} else if c.us[opt] {
			c.queue(IAC, WONT, opt)
		}
		c.us[opt] = false
	}
}

// queue an answer, sent once the byte that asked is handled.  c.mu is held.
func (c *Conn) queue(b ...byte) {
	if len(c.out) < 1024 {
		c.out = append(c.out, b...)
	}
}

// subneg takes a finished subnegotiation: the first terminal type given
// (printable ASCII, 40 characters at most, upper-cased), or a window size
// (each of its dimensions only if it is a sane one).  c.mu is held.
func (c *Conn) subneg() {
	if len(c.sub) == 0 {
		return
	}
	switch c.sub[0] {
	case OptTType:
		if len(c.sub) >= 2 && c.sub[1] == ttypeIs && !c.gotType {
			t := make([]byte, 0, 40)
			for _, b := range c.sub[2:] {
				if b > ' ' && b < 0x7f && len(t) < 40 {
					t = append(t, b)
				}
			}
			c.ttype = strings.ToUpper(string(t))
			c.gotType = true
		}
	case OptNAWS:
		if len(c.sub) == 5 {
			w := int(c.sub[1])<<8 | int(c.sub[2])
			h := int(c.sub[3])<<8 | int(c.sub[4])
			if w >= 20 && w <= 500 {
				c.cols = w
			}
			if h >= 5 && h <= 300 {
				c.rows = h
			}
		}
	}
}

// ask sends a request of ours: DO or DONT for the client's options, WILL or
// WONT for ours, unless it is in effect (or asked) already.
func (c *Conn) ask(cmd, opt byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.askLocked(cmd, opt)
}

// askLocked is ask with c.mu held: the request queued, to go at the next
// flush.
func (c *Conn) askLocked(cmd, opt byte) {
	switch cmd {
	case DO:
		if c.him[opt] || c.askHim[opt] {
			return
		}
		c.askHim[opt] = true
	case DONT:
		if !c.him[opt] || c.askHim[opt] {
			return
		}
		c.askHim[opt] = true
		c.him[opt] = false
	case WILL:
		if c.us[opt] || c.askUs[opt] {
			return
		}
		c.askUs[opt] = true
	case WONT:
		if !c.us[opt] || c.askUs[opt] {
			return
		}
		c.askUs[opt] = true
		c.us[opt] = false
	}
	c.queue(IAC, cmd, opt)
}

// pump reads and handles the client's bytes until done says so or the
// deadline passes; data is kept for Read.
func (c *Conn) pump(deadline time.Time, done func() bool) error {
	if err := c.flush(); err != nil {
		return err
	}
	c.Conn.SetReadDeadline(deadline)
	defer c.Conn.SetReadDeadline(time.Time{})
	for {
		c.mu.Lock()
		ok := done()
		c.mu.Unlock()
		if ok {
			return nil
		}
		b, err := c.r.ReadByte()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return nil
			}
			return err
		}
		d, ok := c.input(b)
		if err := c.flush(); err != nil {
			return err
		}
		if ok {
			c.mu.Lock()
			if len(c.early) < maxEarly {
				c.early = append(c.early, d)
			}
			c.mu.Unlock()
		}
	}
}

// ProbeTermType asks the client for its terminal type and waits up to
// timeout for it.  "" means the client gave none: it refused, or it does
// not speak telnet at all.
func (c *Conn) ProbeTermType(timeout time.Duration) (string, error) {
	c.ask(DO, OptTType)
	err := c.pump(time.Now().Add(timeout), func() bool { return c.gotType })
	return c.TermType(), err
}

var tn3270Pattern = regexp.MustCompile(`^IBM-(327[789]|DYNAMIC)`)

// Is3270 reports whether a terminal type is a 3270's (IBM-3278-2-E,
// IBM-DYNAMIC, ...).  IBM-3151 and other ASCII IBM terminals are not.
func Is3270(ttype string) bool {
	return tn3270Pattern.MatchString(strings.ToUpper(ttype))
}

// StartNVT sets the connection up for a text BBS: the server echoes,
// character at a time, and asks for the window size (waiting up to
// timeout for it).  With binary, 8-bit data is asked for both ways.
func (c *Conn) StartNVT(binary bool, timeout time.Duration) error {
	c.ask(WILL, OptEcho)
	c.ask(WILL, OptSGA)
	c.ask(DO, OptSGA)
	c.ask(DO, OptNAWS)
	if binary {
		c.ask(WILL, OptBinary)
		c.ask(DO, OptBinary)
	}
	return c.pump(time.Now().Add(timeout), func() bool {
		return !c.askHim[OptNAWS] && !c.askUs[OptEcho] && !c.askHim[OptSGA] &&
			!c.askHim[OptBinary] && !c.askUs[OptBinary]
	})
}

// Handoff3270 undoes the terminal type option (go3270 negotiates it again
// from the start) and returns the connection for go3270, the client's
// pending bytes included.  The Conn must not be used after.
func (c *Conn) Handoff3270(timeout time.Duration) (net.Conn, error) {
	c.ask(DONT, OptTType)
	if err := c.pump(time.Now().Add(timeout), func() bool { return !c.askHim[OptTType] }); err != nil {
		return nil, err
	}
	// let stragglers come and be answered
	if err := c.pump(time.Now().Add(100*time.Millisecond), func() bool { return false }); err != nil {
		return nil, err
	}
	return &rawConn{c.Conn, c.r}, nil
}

// rawConn is the connection under a Conn, its buffered bytes first.
type rawConn struct {
	net.Conn
	r *bufio.Reader
}

// Read reads what the Conn had buffered, then the connection.
func (r *rawConn) Read(p []byte) (int, error) { return r.r.Read(p) }
