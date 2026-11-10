package textui

import (
	"errors"
	"net"
	"testing"
	"time"
)

// TestHoldIdle: the sysop's configuration waits past the idle timeout
// for a key; the timeout is back after it
func TestHoldIdle(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()
	term := NewTerm(a)
	term.Idle = 20 * time.Millisecond
	s := &session{t: term}
	restore := s.HoldIdle()
	go func() {
		time.Sleep(100 * time.Millisecond)
		b.Write([]byte("x"))
	}()
	if k, err := term.EditKey(); err != nil || k != 'x' {
		t.Fatalf("held: %q, %v; want 'x'", k, err)
	}
	restore()
	if term.Idle != 20*time.Millisecond {
		t.Fatalf("Idle %v after restore", term.Idle)
	}
	if _, err := term.EditKey(); !errors.Is(err, ErrIdle) {
		t.Fatalf("restored: %v, want ErrIdle", err)
	}
}
