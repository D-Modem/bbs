package textui

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
)

// Line speeds: a caller by telnet, SSH or the web may have what the BBS
// sends come at a modem's speed, as it did when ANSI art was drawn a line
// at a time.  Only what is sent is slowed: keys come in at once, and the
// BBS reads them as soon as it is done sending.  A Break (telnet's, SSH's)
// or Ctrl-X while a screen is coming sends the rest of it at once, up to
// the next prompt: skipped, but drawn whole, with no escape sequence cut.

// pacer is the line under a terminal, sending at most bps bits a second
// (8N1: ten bits a character); 0 is the line's own speed.
type pacer struct {
	Conn
	mu     sync.Mutex
	bps    int
	next   time.Time // when the next byte may go
	direct bool      // a file transfer's bytes: at the line's own speed
	sleep  func(time.Duration)

	skip     atomic.Bool // Break or Ctrl-X: at the line's own speed to the next prompt
	poll     func()      // looks for Ctrl-X (and a telnet Break) among the keys come
	lastPoll time.Time
	noPoll   atomic.Bool // a door reads the keys: poll is not called
}

// pollEvery is how often keys are looked at while a screen is slowed.
const pollEvery = 100 * time.Millisecond

// pace is how far ahead of the line a write may get: the bytes of this
// long go in one write.
const pace = 20 * time.Millisecond

// Write sends p at the speed set, a few bytes at a time.
func (p *pacer) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bps <= 0 || p.direct {
		return p.Conn.Write(b)
	}
	// a byte is 10 bits; this many go in each pace
	chunk := int(int64(p.bps) * int64(pace) / int64(10*time.Second))
	if chunk < 1 {
		chunk = 1
	}
	if now := time.Now(); p.next.Before(now) {
		p.next = now // no credit for the time the line was quiet
	}
	sent := 0
	for sent < len(b) {
		if p.poll != nil && !p.noPoll.Load() && time.Since(p.lastPoll) >= pollEvery {
			p.lastPoll = time.Now()
			p.poll()
		}
		if p.skip.Load() {
			m, err := p.Conn.Write(b[sent:])
			p.next = time.Now()
			return sent + m, err
		}
		n := min(chunk, len(b)-sent)
		if d := time.Until(p.next); d > 0 {
			p.sleep(d)
		}
		m, err := p.Conn.Write(b[sent : sent+n])
		sent += m
		if err != nil {
			return sent, err
		}
		p.next = p.next.Add(time.Duration(n) * 10 * time.Second / time.Duration(p.bps))
	}
	return sent, nil
}

// setDirect has writes go at the line's own speed (on: a transfer
// protocol's packets, which would time out at a slow speed's pace).
func (p *pacer) setDirect(on bool) {
	p.mu.Lock()
	p.direct = on
	p.mu.Unlock()
}

// SetLineSpeed has what is sent go at bps bit/s from now on (0: as fast
// as the line goes).
func (t *Term) SetLineSpeed(bps int) {
	t.Flush()
	t.line.mu.Lock()
	t.line.bps = bps
	t.line.mu.Unlock()
}

// LineSpeed is the speed what is sent goes at (0: the line's own).
func (t *Term) LineSpeed() int {
	t.line.mu.Lock()
	defer t.line.mu.Unlock()
	return t.line.bps
}

// lineSpeedKey is Ctrl-B: at any prompt, the line speed's picker.
const lineSpeedKey = 0x02

// speedKeys are the picker's keys: 0 full speed, then bbs.LineSpeeds.
const speedKeys = "0123456789ABCDEFGHIJ"

// PickLineSpeed asks for a line speed, below the line being typed;
// restore: that line is drawn again after (a prompt Ctrl-B was pressed
// at).  ok false if the caller pressed none of the speeds' keys.
func (t *Term) PickLineSpeed(restore bool) (bps int, ok bool, err error) {
	cur := string(t.cur)
	if cur != "" {
		t.Print("\n")
	}
	t.Println(t.C(Cyan, "Line speed") + " (now " + bbs.LineSpeedName(t.LineSpeed()) + "):")
	// the choices, as many to a line as fit
	line, width := "", 0
	for i := 0; i <= len(bbs.LineSpeeds); i++ {
		name := "full"
		if i > 0 {
			name = bbs.LineSpeedName(bbs.LineSpeeds[i-1])
		}
		w := 2 + len(name) + 2
		if width > 0 && width+w > t.Cols-1 {
			t.Println(line)
			line, width = "", 0
		}
		line += "  " + t.C(White, speedKeys[i:i+1]) + " " + name
		width += w
	}
	t.Println(line)
	t.Print("Which? ")
	t.picking = true
	r, err := t.readRune()
	t.picking = false
	if err != nil {
		return 0, false, err
	}
	if r >= 'a' && r <= 'z' {
		r -= 'a' - 'A'
	}
	i := -1
	for j := 0; j <= len(bbs.LineSpeeds); j++ {
		if rune(speedKeys[j]) == r {
			i = j
		}
	}
	if i < 0 {
		t.Println("")
	} else {
		if i > 0 {
			bps = bbs.LineSpeeds[i-1]
		}
		t.Println(speedKeys[i : i+1])
		if bps == 0 {
			t.Println(t.C(Dim, "Full speed."))
		} else {
			t.Println(t.C(Dim, "Now at "+strconv.Itoa(bps)+" bit/s."))
		}
		t.SetLineSpeed(bps)
	}
	if restore {
		t.Print(cur)
	}
	return bps, i >= 0, nil
}

// lineSpeeds is whether this caller may pick a line speed: the sysop
// lets them, and they have no modem's line (whose speed is its own).
func (s *session) lineSpeeds() bool {
	return s.b.Conf().LineSpeeds && !s.c.HasLine()
}

// startLineSpeed is the speed from the connect, before the login: the
// sysop's line_speed, with Ctrl-B to change it.
func (s *session) startLineSpeed() {
	if !s.lineSpeeds() {
		return
	}
	s.t.SetLineSpeed(s.b.Conf().LineSpeedDefault)
	s.t.OnLineSpeedKey = s.pickLineSpeed
}

// applyLineSpeed is the speed the user chose last time once they are
// logged in, or asked now if the sysop says to.
func (s *session) applyLineSpeed() error {
	if !s.lineSpeeds() {
		return nil
	}
	if bps, ok := s.n.Prefs().LineSpeed(); ok {
		s.t.SetLineSpeed(bps)
	}
	if !s.b.Conf().LineSpeedAsk {
		return nil
	}
	return s.pickSpeed(false)
}

// pickLineSpeed is the line speed's picker (Ctrl-B, the profile's B, the
// login's question); what is picked is kept for the next call.
func (s *session) pickLineSpeed() error { return s.pickSpeed(true) }

// pickSpeed is pickLineSpeed; restore: the line being typed is drawn again
// after.
func (s *session) pickSpeed(restore bool) error {
	bps, ok, err := s.t.PickLineSpeed(restore)
	if err != nil || !ok {
		return err
	}
	if s.n.User() == nil {
		return nil // not logged in yet: for this call
	}
	v := "off"
	if bps > 0 {
		v = strconv.Itoa(bps)
	}
	return s.n.SetPref("baud", v)
}

// skipKey is Ctrl-X: while a screen is slowed, the rest of it at once.
const skipKey = 0x18

// Break is the caller's Break (telnet's, SSH's): a screen being slowed to
// the line speed comes the rest of the way at once.  Any goroutine may
// call it.
func (t *Term) Break() { t.line.skip.Store(true) }

// pollSkip looks at the keys come while a screen is slowed: a Ctrl-X first
// among them is taken as Break.  Reading lets a telnet Break be seen.
func (t *Term) pollSkip() {
	if t.r.Buffered() == 0 {
		t.c.SetReadDeadline(time.Now().Add(2 * time.Millisecond))
		t.r.Peek(1)
		t.c.SetReadDeadline(time.Time{})
	}
	if t.r.Buffered() == 0 {
		return
	}
	if b, err := t.r.Peek(1); err == nil && b[0] == skipKey && !t.PETSCII {
		t.r.ReadByte()
		t.Break()
	}
}
