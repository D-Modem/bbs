package textui

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPacer: at a line speed, what is sent takes as long as at a modem's
// (ten bits a character); transfers' bytes and full speed do not wait.
func TestPacer(t *testing.T) {
	c := &keysConn{in: bytes.NewReader(nil)}
	term := NewTerm(c)
	data := bytes.Repeat([]byte("x"), 480)

	timed := func() time.Duration {
		start := time.Now()
		term.Print(string(data))
		term.Flush()
		return time.Since(start)
	}
	if d := timed(); d > 100*time.Millisecond {
		t.Errorf("full speed took %v", d)
	}
	term.SetLineSpeed(9600) // 960 characters a second: half a second
	if d := timed(); d < 400*time.Millisecond || d > 900*time.Millisecond {
		t.Errorf("480 bytes at 9600 took %v, not about 500ms", d)
	}
	p, _ := term.Port()
	start := time.Now()
	p.Write(data)
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("a transfer's bytes took %v", d)
	}
	if c.out.Len() != 3*len(data) {
		t.Errorf("sent %d bytes, not %d", c.out.Len(), 3*len(data))
	}
	if term.LineSpeed() != 9600 {
		t.Errorf("speed %d", term.LineSpeed())
	}
}

// keysConn is a line with keys typed ahead, keeping what is written.
type keysConn struct {
	in  *bytes.Reader
	out bytes.Buffer
}

func (c *keysConn) Read(p []byte) (int, error) {
	if c.in.Len() == 0 {
		time.Sleep(time.Millisecond)
		return 0, os.ErrDeadlineExceeded
	}
	return c.in.Read(p)
}
func (c *keysConn) Write(p []byte) (int, error)     { return c.out.Write(p) }
func (c *keysConn) SetReadDeadline(time.Time) error { return nil }

// TestSkip: Ctrl-X typed, or a Break, while a screen comes slowly sends the
// rest of it at once, all of it, and the next key read is slow again.
func TestSkip(t *testing.T) {
	screen := strings.Repeat("ANSI art line\x1b[1;36m!\x1b[0m\r\n", 120) // 3000 bytes: 25 s at 1200
	c := &keysConn{in: bytes.NewReader([]byte{0x18, 'x'})}
	term := NewTerm(c)
	term.SetLineSpeed(1200)
	start := time.Now()
	term.Raw([]byte(screen))
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Ctrl-X: the screen took %v", d)
	}
	if !strings.Contains(c.out.String(), screen) {
		t.Errorf("sent %d bytes, not the whole screen", c.out.Len())
	}
	if r, err := term.readRune(); err != nil || r != 'x' {
		t.Errorf("the key after Ctrl-X: %q %v", r, err)
	}
	if term.line.skip.Load() {
		t.Error("still skipping after a key was read")
	}

	// a Break from another goroutine (SSH's)
	c2 := &keysConn{in: bytes.NewReader(nil)}
	term = NewTerm(c2)
	term.SetLineSpeed(1200)
	go func() {
		time.Sleep(200 * time.Millisecond)
		term.Break()
	}()
	start = time.Now()
	term.Raw([]byte(screen))
	if d := time.Since(start); d < 150*time.Millisecond || d > 2*time.Second {
		t.Errorf("Break: the screen took %v", d)
	}
	if c2.out.String() != screen {
		t.Errorf("sent %d bytes, not the screen", c2.out.Len())
	}
}
