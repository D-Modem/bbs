package server

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// ptyConn is a pty's master as a net.Conn, for a dialog: the caller's
// end of a line whose terminal was handed over.
type ptyConn struct{ *os.File }

// LocalAddr is none.
func (ptyConn) LocalAddr() net.Addr { return nil }

// RemoteAddr is none.
func (ptyConn) RemoteAddr() net.Addr { return nil }

// openPty is a new pty, raw: its master (non-blocking, for deadlines) and
// its terminal; the test is skipped where there is none.
func openPty(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Skip("no pty:", err)
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Skip("no pty:", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Skip("no pty:", err)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skip("no pty:", err)
	}
	if tio, err := unix.IoctlGetTermios(int(s.Fd()), unix.TCGETS); err == nil {
		tio.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
		tio.Oflag &^= unix.OPOST
		tio.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
		unix.IoctlSetTermios(int(s.Fd()), unix.TCSETS, tio)
	}
	return os.NewFile(uintptr(fd), "ptmx"), s
}

// handOver is a caller whose line (a pty's terminal) is handed to the
// listener at name with header, as modemmenu's handoff item does: the
// dialog on the line, and the connection
func (e *env) handOver(name, header, after string) (*dialog, *net.UnixConn) {
	m, s := openPty(e.t)
	c, err := net.Dial("unix", filepath.Join(e.dir, name))
	if err != nil {
		e.t.Fatal(err)
	}
	uc := c.(*net.UnixConn)
	if _, _, err := uc.WriteMsgUnix([]byte(header+"\n"+after), unix.UnixRights(int(s.Fd())), nil); err != nil {
		e.t.Fatal(err)
	}
	s.Close()
	e.t.Cleanup(func() { m.Close(); c.Close() })
	return &dialog{t: e.t, c: ptyConn{m}}, uc
}

// TestHandoff: callers whose lines are handed over (SCM_RIGHTS) with the
// header, all on at once, each on its own terminal; logging off gives the
// line back (the connection closes), and the sender closing the
// connection takes it away (the BBS lets go of the terminal)
func TestHandoff(t *testing.T) {
	const callers = 4
	e := start(t, nil, &Listener{Name: "menu", Kind: "raw", Address: "handoff.sock", ANSI: "no",
		Header: true, Handoff: true, TrustUser: true, CreateUsers: true})
	var ds []*dialog
	var cs []*net.UnixConn
	for i := 0; i < callers; i++ {
		d, c := e.handOver("handoff.sock", fmt.Sprintf("MODEM_USER=caller%d\nMODEM_RATE=26400/31200\n", i), "")
		d.expect(fmt.Sprintf("Welcome, caller%d.", i))
		d.expect("Command: ")
		ds, cs = append(ds, d), append(cs, c)
	}
	// who's on: every caller, each on a line of their own
	ds[0].send("W")
	for i := 0; i < callers; i++ {
		ds[0].expect(fmt.Sprintf("caller%d", i))
	}
	ds[0].expect("(its number, Enter for none): ")
	ds[0].send("\r")
	ds[0].expect("Command: ")

	// logging off: the line given back, the connection closed
	for i := 0; i < callers-1; i++ {
		ds[i].send("GY")
		ds[i].expect("Thanks for calling.")
		cs[i].SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := cs[i].Read(make([]byte, 1)); err == nil || os.IsTimeout(err) {
			t.Errorf("caller %d: the connection still open after logging off: %v", i, err)
		}
	}
	// the sender takes the last line away: the BBS lets go of the
	// terminal (its master reads the end once no one has the terminal)
	last := ds[callers-1]
	cs[callers-1].Close()
	m := last.c.(ptyConn)
	m.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 256)
	for {
		_, err := m.Read(buf)
		if err == nil {
			continue
		}
		if os.IsTimeout(err) {
			t.Error("the BBS still has the terminal after the connection closed")
		}
		break
	}
}

// TestHandoffWithoutLine: a handoff listener's header without a line is
// an ordinary call, its bytes on the connection
func TestHandoffWithoutLine(t *testing.T) {
	e := start(t, nil, &Listener{Name: "menu", Kind: "raw", Address: "handoff.sock", ANSI: "no",
		Header: true, Handoff: true, TrustUser: true, CreateUsers: true})
	d := e.dial("handoff.sock")
	d.send("MODEM_USER=dave\n\n")
	d.expect("Welcome, dave.")
}
