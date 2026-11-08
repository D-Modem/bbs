package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/D-Modem/bbs/internal/bbs"
)

// The handoff: a caller's line itself, a terminal, passed to the BBS over
// a unix socket (SCM_RIGHTS) with the session header, by modemmenu's item
// with handoff = yes (or app_dmodem's profile with handoff = yes, the
// modem's pty).  The BBS reads and writes the terminal; nothing copies
// between.  What the sender had read of the line and not used comes on
// the connection right after the header; then the connection carries
// nothing more and stays open while the BBS has the line.  Closing it
// gives the line back; the sender closing it takes the line away.

// ReadHeaderFD is ReadHeader on a unix socket, and the descriptor that
// came with it (nil: none came).
func ReadHeaderFD(c *net.UnixConn, timeout time.Duration) (map[string]string, *os.File, error) {
	c.SetReadDeadline(time.Now().Add(timeout))
	defer c.SetReadDeadline(time.Time{})
	var got *os.File
	fail := func(err error) (map[string]string, *os.File, error) {
		if got != nil {
			got.Close()
		}
		return nil, nil, err
	}
	h := map[string]string{}
	var line []byte
	b := make([]byte, 1)
	oob := make([]byte, unix.CmsgSpace(4*4))
	for total := 0; ; total++ {
		if total >= headerMaxBytes {
			return fail(errors.New("header: too long"))
		}
		n, oobn, _, _, err := c.ReadMsgUnix(b, oob)
		if oobn > 0 {
			msgs, perr := unix.ParseSocketControlMessage(oob[:oobn])
			if perr != nil {
				return fail(fmt.Errorf("header: %w", perr))
			}
			for i := range msgs {
				fds, _ := unix.ParseUnixRights(&msgs[i])
				for _, fd := range fds {
					if got != nil {
						unix.Close(fd)
						return fail(errors.New("header: more than one descriptor"))
					}
					unix.CloseOnExec(fd)
					got = os.NewFile(uintptr(fd), "handoff")
				}
			}
		}
		if err != nil {
			return fail(fmt.Errorf("header: %w", err))
		}
		if n == 0 {
			return fail(errors.New("header: end of file"))
		}
		if b[0] != '\n' {
			line = append(line, b[0])
			continue
		}
		s := strings.TrimSuffix(string(line), "\r")
		line = line[:0]
		if s == "" {
			return h, got, nil
		}
		if len(h) >= headerMaxLines {
			return fail(errors.New("header: too many lines"))
		}
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return fail(fmt.Errorf("header: no = in %q", bbs.Clean(s, 40)))
		}
		h[strings.ToUpper(strings.TrimSpace(k))] = v
	}
}

// lineConn is a handed-over line as a net.Conn: the terminal's bytes
// (after what came on the connection behind the header), the
// connection's addresses; the connection closing hangs it up, and Close
// gives the line back (the terminal's flags as they came) and closes
// the connection.
type lineConn struct {
	tty   *os.File
	ctl   net.Conn
	first []byte
	flags int
	once  sync.Once
}

// newLineConn makes a lineConn of the terminal f, handed over on ctl.
func newLineConn(f *os.File, ctl net.Conn) (*lineConn, error) {
	fd := int(f.Fd())
	if _, err := unix.IoctlGetTermios(fd, unix.TCGETS); err != nil {
		f.Close()
		return nil, errors.New("the line handed over is not a terminal")
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		f.Close()
		return nil, err
	}
	// a copy of it non-blocking, so that deadlines work on it (the
	// runtime polls it); f's own goes
	nfd, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
	f.Close()
	if err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(nfd, true); err != nil {
		unix.Close(nfd)
		return nil, err
	}
	tty := os.NewFile(uintptr(nfd), "handoff")
	lc := &lineConn{tty: tty, ctl: ctl, flags: flags}
	// what came after the header: there already, sent with it
	ctl.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	buf := make([]byte, 4096)
	if n, _ := ctl.Read(buf); n > 0 {
		lc.first = append([]byte(nil), buf[:n]...)
	}
	ctl.SetReadDeadline(time.Time{})
	go func() {
		io.Copy(io.Discard, ctl)
		lc.Close()
	}()
	return lc, nil
}

// Read reads what came behind the header, then the terminal.
func (c *lineConn) Read(p []byte) (int, error) {
	if len(c.first) > 0 {
		n := copy(p, c.first)
		c.first = c.first[n:]
		return n, nil
	}
	return c.tty.Read(p)
}

// Write writes to the terminal.
func (c *lineConn) Write(p []byte) (int, error) { return c.tty.Write(p) }

// Close gives the line back: the terminal's flags restored, our copy of
// it closed, and the connection closed.
func (c *lineConn) Close() error {
	c.once.Do(func() {
		if rc, err := c.tty.SyscallConn(); err == nil {
			rc.Control(func(fd uintptr) { unix.FcntlInt(fd, unix.F_SETFL, c.flags) })
		}
		c.tty.Close()
		c.ctl.Close()
	})
	return nil
}

// LocalAddr is the connection's.
func (c *lineConn) LocalAddr() net.Addr { return c.ctl.LocalAddr() }

// RemoteAddr is the connection's.
func (c *lineConn) RemoteAddr() net.Addr { return c.ctl.RemoteAddr() }

// SetDeadline sets the terminal's.
func (c *lineConn) SetDeadline(t time.Time) error { return c.tty.SetDeadline(t) }

// SetReadDeadline sets the terminal's.
func (c *lineConn) SetReadDeadline(t time.Time) error { return c.tty.SetReadDeadline(t) }

// SetWriteDeadline sets the terminal's.
func (c *lineConn) SetWriteDeadline(t time.Time) error { return c.tty.SetWriteDeadline(t) }
