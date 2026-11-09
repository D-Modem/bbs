package textui

import (
	"bytes"
	"testing"
	"time"
)

// TestPacer: at a line speed, what is sent takes as long as at a modem's
// (ten bits a character); transfers' bytes and full speed do not wait.
func TestPacer(t *testing.T) {
	c := &bufConn{}
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
	if c.Len() != 3*len(data) {
		t.Errorf("sent %d bytes, not %d", c.Len(), 3*len(data))
	}
	if term.LineSpeed() != 9600 {
		t.Errorf("speed %d", term.LineSpeed())
	}
}
