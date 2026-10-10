// Package door runs door programs for callers: a DOS door under dosemu2
// (in Docker), or any program that talks to a terminal on its standard
// input and output.  The BBS writes the drop files a door reads, and joins
// the caller's line to the door until it ends, the caller hangs up, or the
// time runs out.
package door

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// Charsets of a door's output.
const (
	CP437 = "cp437" // DOS: shown as UTF-8 to UTF-8 terminals
	UTF8  = "utf-8" // already
	Raw   = "raw"   // passed as it is, whatever the terminal
)

// Door is a door program.
type Door struct {
	Name        string
	Description string
	Command     []string // the program and its arguments; see Expand
	Dir         string   // where the drop files go (expanded): a node's own
	DropFiles   []string
	Charset     string
	TimeLimit   time.Duration
	SysopOnly   bool
	Single      bool     // one caller at a time
	PTY         bool     // run it on a pseudo-terminal, not pipes
	Cleanup     []string // run after it (expanded), e.g. docker rm -f
	CommType    int      // DOOR32.SYS's: 1 serial (a FOSSIL), 2 telnet

	mu sync.Mutex
}

// Line is the caller's line.
type Line interface {
	io.ReadWriter
	SetReadDeadline(time.Time) error
}

// Split splits a command line into words: spaces separate them, '...'
// and "..." quote (a backslash is itself everywhere, for DOS paths, but
// \" inside "...").
func Split(s string) ([]string, error) {
	var out []string
	var w strings.Builder
	in := false
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				w.WriteByte(c)
			}
		case quote == '"':
			if c == '\\' && i+1 < len(s) && s[i+1] == '"' {
				w.WriteByte('"')
				i++
			} else if c == '"' {
				quote = 0
			} else {
				w.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			in = true
		case c == ' ' || c == '\t':
			if in {
				out = append(out, w.String())
				w.Reset()
				in = false
			}
		default:
			w.WriteByte(c)
			in = true
		}
	}
	if quote != 0 {
		return nil, errors.New("an unclosed quote")
	}
	if in {
		out = append(out, w.String())
	}
	return out, nil
}

// Expand fills in a word: {node} the node number, {dir} the drop files'
// directory, {userid} the user's number, {minutes} the minutes left,
// {rows} and {cols} the screen, {baud} the line's rate, {uid} and {gid}
// the BBS's own (for docker run --user, so the door writes as the BBS).
func Expand(w string, in *Info, dir string) string {
	return strings.NewReplacer(
		"{node}", fmt.Sprint(in.Node),
		"{dir}", dir,
		"{userid}", fmt.Sprint(in.UserID),
		"{minutes}", fmt.Sprint(int(in.TimeLeft/time.Minute)),
		"{rows}", fmt.Sprint(in.Rows),
		"{cols}", fmt.Sprint(in.Cols),
		"{baud}", fmt.Sprint(in.Baud),
		"{uid}", fmt.Sprint(os.Getuid()),
		"{gid}", fmt.Sprint(os.Getgid()),
	).Replace(w)
}

// ErrBusy: a single-caller door in use.
var ErrBusy = errors.New("someone else is in this door: try again later")

// Result says how a door ended.
type Result struct {
	TimeUp  bool // the time limit
	HungUp  bool // the caller went
	Idle    bool // the caller typed nothing for too long
	ExitErr error
}

// Run runs the door for a caller on line until it ends.  utf8 says the
// caller's terminal is UTF-8 (so a CP437 door is translated); idle is how
// long the caller may type nothing (0: no limit).  Errors are of starting
// it; how it ended is in the Result.
func (d *Door) Run(line Line, in *Info, utf8Term bool, idle time.Duration, logf func(string, ...any)) (Result, error) {
	var res Result
	if d.Single {
		if !d.mu.TryLock() {
			return res, ErrBusy
		}
		defer d.mu.Unlock()
	}
	in.TimeLeft = d.TimeLimit
	in.CommType = d.CommType
	if in.ComPort == 0 {
		in.ComPort = 1
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	dir := Expand(d.Dir, in, "")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return res, err
	}
	if err := WriteDropFiles(dir, in, d.DropFiles); err != nil {
		return res, err
	}
	args := make([]string, len(d.Command))
	for i, w := range d.Command {
		args[i] = Expand(w, in, dir)
	}
	defer func() {
		if len(d.Cleanup) == 0 {
			return
		}
		ca := make([]string, len(d.Cleanup))
		for i, w := range d.Cleanup {
			ca[i] = Expand(w, in, dir)
		}
		c := exec.Command(ca[0], ca[1:]...)
		c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
		done := make(chan struct{})
		go func() { c.Run(); close(done) }()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			if c.Process != nil {
				c.Process.Kill()
			}
		}
	}()

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), fmt.Sprintf("BBS_NODE=%d", in.Node), "BBS_DROPDIR="+dir,
		fmt.Sprintf("BBS_USERID=%d", in.UserID), fmt.Sprintf("BBS_MINUTES=%d", int(in.TimeLeft/time.Minute)))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	errlog, err := os.OpenFile(dir+"/door.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return res, err
	}
	defer errlog.Close()
	cmd.Stderr = errlog

	var toDoor io.WriteCloser
	var fromDoor io.ReadCloser
	if d.PTY {
		master, slave, err := openPTY(in.Rows, in.Cols)
		if err != nil {
			return res, err
		}
		defer master.Close()
		cmd.Stdin, cmd.Stdout = slave, slave
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		if err := cmd.Start(); err != nil {
			slave.Close()
			return res, err
		}
		slave.Close()
		toDoor, fromDoor = master, master
	} else {
		w, err := cmd.StdinPipe()
		if err != nil {
			return res, err
		}
		r, err := cmd.StdoutPipe()
		if err != nil {
			return res, err
		}
		if err := cmd.Start(); err != nil {
			return res, err
		}
		toDoor, fromDoor = w, r
	}
	logf("door %s: started %q", d.Name, args)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	pgid := cmd.Process.Pid
	stop := func() {
		syscall.Kill(-pgid, syscall.SIGTERM)
		select {
		case err := <-exited:
			exited <- err
		case <-time.After(5 * time.Second):
			syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}

	// the door's output to the caller
	outDone := make(chan struct{})
	go func() {
		defer close(outDone)
		buf := make([]byte, 4096)
		var conv []byte
		for {
			n, err := fromDoor.Read(buf)
			if n > 0 {
				out := buf[:n]
				if utf8Term && d.Charset == CP437 {
					conv = conv[:0]
					for _, c := range out {
						if c < 0x80 {
							conv = append(conv, c)
						} else {
							conv = utf8.AppendRune(conv, charmap.CodePage437.DecodeByte(c))
						}
					}
					out = conv
				}
				if _, err := line.Write(out); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// the caller's input to the door
	deadline := time.Now().Add(d.TimeLimit)
	lastKey := time.Now()
	buf := make([]byte, 1024)
	var pending []byte // a UTF-8 character cut in two
	lastCR := false
	finished := false
	for !finished {
		select {
		case err := <-exited:
			res.ExitErr = err
			finished = true
			continue
		default:
		}
		if d.TimeLimit > 0 && time.Now().After(deadline) {
			res.TimeUp = true
			stop()
			break
		}
		if idle > 0 && time.Since(lastKey) > idle {
			res.Idle = true
			stop()
			break
		}
		line.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := line.Read(buf)
		if n > 0 {
			lastKey = time.Now()
			var out []byte
			for _, c := range buf[:n] {
				// a terminal's CR LF is the door's CR
				if c == '\n' && lastCR {
					lastCR = false
					continue
				}
				lastCR = c == '\r'
				out = append(out, c)
			}
			if utf8Term && d.Charset == CP437 {
				out = toCP437(append(pending, out...), &pending)
			}
			if len(out) > 0 {
				toDoor.Write(out)
			}
		}
		if err != nil {
			var ne interface{ Timeout() bool }
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			res.HungUp = true
			stop()
			break
		}
	}
	line.SetReadDeadline(time.Time{})
	if !d.PTY {
		toDoor.Close() // a pty's master is its output too: closed after
	}
	if !finished {
		res.ExitErr = <-exited
	}
	// what it wrote last, unless something it left behind holds the pipe
	select {
	case <-outDone:
	case <-time.After(2 * time.Second):
		fromDoor.Close()
		<-outDone
	}
	logf("door %s: ended (time up %v, hung up %v, idle %v, %v)", d.Name, res.TimeUp, res.HungUp, res.Idle, res.ExitErr)
	return res, nil
}

// toCP437 turns UTF-8 typed by the caller into CP437; an incomplete
// character at the end is kept in pending for the next read.
func toCP437(b []byte, pending *[]byte) []byte {
	var out []byte
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size <= 1 {
			if !utf8.FullRune(b) {
				break
			}
			b = b[1:]
			continue
		}
		b = b[size:]
		if r < 0x80 {
			out = append(out, byte(r))
		} else if c, ok := charmap.CodePage437.EncodeRune(r); ok {
			out = append(out, c)
		}
	}
	*pending = append((*pending)[:0], b...)
	return out
}
