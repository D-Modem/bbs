package textui

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/app"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// What only a text terminal does, for internal/app.

func (s *session) Kind() string { return app.Text }

// TerminalInfo: all there is to say of a text terminal is on the profile.
func (s *session) TerminalInfo() error {
	s.Say(view.Plain, "That is for 3270 terminals.")
	return nil
}

// Terminal is the terminal's line on the profile.
func (s *session) Terminal() string {
	t := s.t
	switch {
	case t.PETSCII:
		return fmt.Sprintf("Commodore, %dx%d", t.Cols, t.Rows)
	case t.VT52:
		return fmt.Sprintf("VT52, %dx%d", t.Cols, t.Rows)
	}
	onOff := map[bool]string{true: "on", false: "off"}
	ident := ""
	if t.Ident != "" {
		ident = t.Ident + ", "
	}
	charset := ""
	if t.DEC {
		charset = ", DEC graphics"
	}
	editor := ""
	if t.ANSI {
		editor = ", editor " + map[bool]string{true: "full-screen", false: "line"}[s.fullScreen()]
	}
	return fmt.Sprintf("%s%dx%d, ANSI %s, UTF-8 %s%s, sixel %s, RIP %s%s", ident, t.Cols, t.Rows,
		onOff[t.ANSI], onOff[t.UTF8], charset, onOff[t.Sixel], onOff[t.RIP], editor)
}

// TerminalActions are what the caller may switch on the profile.
func (s *session) TerminalActions() []view.Action {
	t := s.t
	switch {
	case t.PETSCII:
		return []view.Action{{ID: "width", Label: "Width 40/80", Key: 'W'}}
	case t.VT52:
		return nil
	}
	acts := []view.Action{{ID: "ansi", Label: "ANSI on/off", Key: 'A'},
		{ID: "utf8", Label: "8-bit UTF-8 on/off", Key: '8'},
		{ID: "sixel", Label: "Sixel on/off", Key: 'S'},
		{ID: "rip", Label: "RIP on/off", Key: 'R'}}
	if t.ANSI {
		acts = append(acts, view.Action{ID: "editor", Label: "Editor full/line", Key: 'E'})
	}
	return acts
}

// applyPrefs treats the terminal as the user said to last time: ANSI and
// UTF-8 as they set them, sixel and RIP off if they turned them off (on
// only where the terminal says it has them), a Commodore's width.
func (s *session) applyPrefs() {
	t := s.t
	p := s.n.Prefs()
	switch {
	case t.PETSCII:
		if w, ok := p["width"]; ok && w != strconv.Itoa(t.Cols) {
			t.SetPETSCII(true, map[string]int{"40": 40, "80": 80}[w])
		}
		return
	case t.VT52:
		return
	}
	if on, ok := p.On("ansi"); ok {
		t.ANSI = on
		if !on {
			t.RIP = false
		}
	}
	if on, ok := p.On("utf8"); ok && on != t.UTF8 {
		t.UTF8 = on
		if on && t.DEC {
			t.DEC = false
			t.setTranslator(nil)
		}
	}
	if on, ok := p.On("sixel"); ok && !on {
		t.Sixel = false
	}
	if on, ok := p.On("rip"); ok && !on {
		t.RIP = false
	}
}

// savePrefs keeps how the terminal is now, for the next call.
func (s *session) savePrefs(id string) error {
	t := s.t
	onOff := map[bool]string{true: "on", false: "off"}
	switch id {
	case "width":
		return s.n.SetPref("width", strconv.Itoa(t.Cols))
	case "ansi":
		if err := s.n.SetPref("ansi", onOff[t.ANSI]); err != nil || t.ANSI {
			return err
		}
		return s.n.SetPref("rip", "off")
	case "editor":
		v := "line"
		if s.n.Prefs()["editor"] == "line" {
			v = "full"
		}
		return s.n.SetPref("editor", v)
	case "utf8", "sixel", "rip":
		return s.n.SetPref(id, onOff[map[string]bool{"utf8": t.UTF8, "sixel": t.Sixel, "rip": t.RIP}[id]])
	}
	return nil
}

// TerminalAction switches what was chosen, and keeps it for next time.
func (s *session) TerminalAction(id string) error {
	if err := s.switchTerminal(id); err != nil {
		return err
	}
	return s.savePrefs(id)
}

func (s *session) switchTerminal(id string) error {
	t := s.t
	if t.PETSCII != (id == "width") || t.VT52 {
		return nil
	}
	switch id {
	case "width":
		t.SetPETSCII(true, map[int]int{40: 80, 80: 40}[t.Cols])
	case "ansi":
		t.ANSI = !t.ANSI
		if !t.ANSI {
			t.RIP = false
		}
	case "utf8":
		t.UTF8 = !t.UTF8
		if t.UTF8 && t.DEC {
			t.DEC = false
			t.setTranslator(nil)
		}
	case "rip":
		// on only where the terminal says it has it
		if t.RIP {
			t.RIP = false
		} else if t.ANSI {
			on, err := t.DetectRIP(2 * time.Second)
			if err != nil {
				return err
			}
			t.RIP = on
			if !on {
				t.Println(t.C(Red, "Your terminal does not say it has RIPscrip."))
			}
		}
	case "sixel":
		// on only where the terminal can: sixel bytes are noise elsewhere
		if t.Sixel {
			t.Sixel = false
		} else if t.ANSI {
			on, err := t.DetectSixel(2 * time.Second)
			if err != nil {
				return err
			}
			t.Sixel = on
			if !on {
				t.Println(t.C(Red, "Your terminal does not say it has sixel graphics."))
			}
		}
	}
	return nil
}

// Watch shows the sysop node num's screen as it is sent, until a key or
// the caller goes.
func (s *session) Watch(num int) error {
	t := s.t
	ch, stop, err := s.n.Watch(num)
	if err != nil {
		t.Println(t.C(Red, capital(err.Error())+"."))
		return nil
	}
	defer stop()
	t.Println(t.C(Cyan, fmt.Sprintf("Watching node %d: what you see is what is sent to it.  A key stops.", num)))
	t.Flush()
	since := time.Now()
	for {
		for more := true; more; {
			select {
			case p := <-ch:
				if err := t.Raw(p); err != nil {
					return err
				}
			default:
				more = false
			}
		}
		_, err := t.RawKey(200 * time.Millisecond)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrIdle) {
			return err
		}
		if time.Since(since) > time.Second {
			since = time.Now()
			if !s.online(num) {
				t.Println("")
				t.Println(t.C(Yellow, fmt.Sprintf("Node %d's caller is gone.", num)))
				return nil
			}
		}
	}
	if t.ANSI {
		t.Print("\x1b[0m")
	}
	t.Println("")
	t.Println(t.C(Cyan, fmt.Sprintf("Watching node %d no more.", num)))
	return nil
}

// online: a caller is on the node.
func (s *session) online(num int) bool {
	for _, n := range s.b.Nodes() {
		if n.Num == num {
			return true
		}
	}
	return false
}
