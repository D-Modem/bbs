// Package termid is CompuServe's terminal identification: the host sends
// ESC I and a terminal that knows it answers with what it is and can do,
//
//	#<product>[:<version>[:<os>]],<code>,<code>,...,+<checksum>CR
//
// (#WINCIM:3.0.1:WN6,PB,...,AP,+4657), the checksum the sum of the
// characters from '#' through '+' in decimal.  Older terminals give no
// checksum (#IB1,PB,DT); a VIDTEX answers RS 4.  Codes this host uses:
// PB (B protocol), AP (the application protocol: CIM's HMI), CA (ANSI
// cursor), AC (ANSI colour), CC (VIDTEX/VT52 cursor), GF (GIF), GH and GM
// (RLE graphics).
package termid

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// Caps is what a terminal said of itself.
type Caps struct {
	Raw      string // the reply as sent, without its CR
	Product  string // WINCIM, OZ3, IB1...
	Version  string
	OS       string
	Codes    []string // the capability codes, as sent
	Checksum int      // -1 if none was sent
	Vidtex   bool     // an old VIDTEX's RS 4
}

// Has reports whether the terminal gave code (or one beginning with it:
// "SS" for "SS7o").
func (c *Caps) Has(code string) bool {
	if c == nil {
		return false
	}
	for _, k := range c.Codes {
		if k == code || len(code) == 2 && strings.HasPrefix(k, code) {
			return true
		}
	}
	return false
}

// AP is a terminal speaking the application protocol: a CIM client.
func (c *Caps) AP() bool { return c.Has("AP") }

// B is a terminal claiming the B protocol (the ENQ test decides).
func (c *Caps) B() bool { return c.Has("PB") }

// Sum is the checksum of a reply: its characters through '+'.
func Sum(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		n += int(s[i])
		if s[i] == '+' && (i+1 == len(s) || s[i+1] >= '0' && s[i+1] <= '9') {
			break
		}
	}
	return n
}

// ErrBadChecksum is a reply whose checksum does not add up.
var ErrBadChecksum = errors.New("termid: bad checksum")

// Parse reads a reply (CR removed).  A reply with a checksum that does not
// add up is returned with ErrBadChecksum: the host asks again, and takes
// it if it comes twice the same.
func Parse(s string) (*Caps, error) {
	s = strings.TrimRight(s, "\r\n")
	c := &Caps{Raw: s, Checksum: -1}
	if strings.HasPrefix(s, "\x1e4") || s == "\x1e" {
		c.Vidtex = true
		return c, nil
	}
	if !strings.HasPrefix(s, "#") {
		return nil, errors.New("termid: not an identification")
	}
	body := s[1:]
	if i := strings.LastIndex(body, ",+"); i >= 0 || strings.HasPrefix(body, "+") {
		tail := body[i+2:]
		if i < 0 {
			tail = body[1:]
		}
		if n, err := strconv.Atoi(tail); err == nil {
			c.Checksum = n
			body = body[:max(i, 0)]
		} else if tail == "" {
			body = body[:max(i, 0)]
		}
	}
	parts := strings.Split(body, ",")
	id := strings.SplitN(parts[0], ":", 3)
	c.Product = id[0]
	if len(id) > 1 {
		c.Version = id[1]
	}
	if len(id) > 2 {
		c.OS = id[2]
	}
	for _, p := range parts[1:] {
		if p = strings.TrimSpace(p); p != "" {
			c.Codes = append(c.Codes, p)
		}
	}
	if c.Checksum >= 0 && Sum(s) != c.Checksum {
		return c, ErrBadChecksum
	}
	return c, nil
}

// Line is what Probe reads and writes: a caller's line with deadlines.
type Line interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	SetReadDeadline(time.Time) error
}

// Probe sends ESC I and reads the answer, up to tries times, each waiting
// wait.  nil, nil: no answer (a plain terminal).  A reply whose checksum
// is wrong is asked for again and taken when it comes twice the same.
// What came that was not an answer is returned in junk (the caller's
// typing, to be put back).
func Probe(l Line, r *bufio.Reader, tries int, wait time.Duration) (caps *Caps, junk []byte, err error) {
	prev := ""
	for try := 0; try < tries; try++ {
		if _, err := l.Write([]byte("\x1bI")); err != nil {
			return nil, junk, err
		}
		reply, j, err := readReply(l, r, wait)
		junk = append(junk, j...)
		if err != nil {
			return nil, junk, err
		}
		if reply == "" {
			return nil, junk, nil
		}
		c, perr := Parse(reply)
		if perr == nil {
			return c, junk, nil
		}
		if errors.Is(perr, ErrBadChecksum) && reply == prev {
			return c, junk, nil
		}
		prev = reply
	}
	return nil, junk, nil
}

// readReply reads a "#..." or RS reply to CR, within wait; "" if none.
func readReply(l Line, r *bufio.Reader, wait time.Duration) (string, []byte, error) {
	deadline := time.Now().Add(wait)
	l.SetReadDeadline(deadline)
	defer l.SetReadDeadline(time.Time{})
	var b []byte
	var junk []byte
	started := false
	for len(b) < 512 {
		c, err := r.ReadByte()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return "", append(junk, b...), nil
			}
			return "", junk, err
		}
		if !started {
			if c == '#' || c == 0x1e {
				started = true
			} else {
				junk = append(junk, c)
				continue
			}
		}
		if c == '\r' || c == '\n' {
			return string(b), junk, nil
		}
		b = append(b, c)
		if len(b) == 2 && b[0] == 0x1e {
			// VIDTEX: RS 4 and a CR, which may not come
			l.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		}
	}
	return string(b), junk, nil
}
