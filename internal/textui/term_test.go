package textui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/screens"
)

type bufConn struct{ bytes.Buffer }

// SetReadDeadline does nothing: a buffer has no deadline.
func (*bufConn) SetReadDeadline(time.Time) error { return nil }

// TestCode: the sysop's tags as each terminal's escape sequences.
func TestCode(t *testing.T) {
	ansi := NewTerm(&bufConn{})
	ansi.ANSI = true
	mono := NewTerm(&bufConn{})
	mono.ANSI, mono.Mono = true, true
	plain := NewTerm(&bufConn{})
	vt52 := NewTerm(&bufConn{})
	vt52.SetVT52()
	for _, c := range []struct {
		tag                     string
		ansi, mono, plain, vt52 string
	}{
		{"red", "\x1b[31m", "", "", ""},
		{"pink", "\x1b[35m", "", "", ""},
		{"turquoise", "\x1b[36m", "", "", ""},
		{"on blue", "\x1b[44m", "", "", ""},
		{"bright", "\x1b[1m", "\x1b[1m", "", ""},
		{"normal", "\x1b[22m", "\x1b[22m", "", ""},
		{"underline", "\x1b[4m", "\x1b[4m", "", ""},
		{"reset", "\x1b[0m", "\x1b[0m", "", ""},
		{"clear", "\x1b[2J\x1b[H", "\x1b[2J\x1b[H", "\r\n", "\x1b[2J\x1b[H"},
		{"eol", "\x1b[K", "\x1b[K", "", "\x1b[K"},
	} {
		for _, x := range []struct {
			t    *Term
			want string
		}{{ansi, c.ansi}, {mono, c.mono}, {plain, c.plain}, {vt52, c.vt52}} {
			if got := x.t.Code(c.tag); got != x.want {
				t.Errorf("%s: %q, want %q", c.tag, got, x.want)
			}
		}
	}

	// Print turns marks into the terminal's codes, on a Commodore too
	c := &bufConn{}
	pet := NewTerm(c)
	pet.SetPETSCII(true, 40)
	c.Reset()
	pet.Print(screens.Mark("red") + "HI" + screens.Mark("reset"))
	pet.Flush()
	// red (0x1c), the letters in PETSCII, then the default light grey
	if out := c.String(); strings.ContainsAny(out, "\x1b\x1e\x1f") || len(out) != 4 || out[0] != 0x1c {
		t.Errorf("PETSCII: %q", out)
	}
	c2 := &bufConn{}
	a := NewTerm(c2)
	a.ANSI = true
	a.Print(screens.Mark("green") + "go" + screens.Mark("default"))
	a.Flush()
	if got := c2.String(); got != "\x1b[32mgo\x1b[0m" {
		t.Errorf("ANSI: %q", got)
	}
}
