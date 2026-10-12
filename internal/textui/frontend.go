package textui

import (
	"fmt"
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
	return fmt.Sprintf("%s%dx%d, ANSI %s, UTF-8 %s%s, sixel %s, RIP %s", ident, t.Cols, t.Rows,
		onOff[t.ANSI], onOff[t.UTF8], charset, onOff[t.Sixel], onOff[t.RIP])
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
	return []view.Action{{ID: "ansi", Label: "ANSI on/off", Key: 'A'},
		{ID: "utf8", Label: "8-bit UTF-8 on/off", Key: '8'},
		{ID: "sixel", Label: "Sixel on/off", Key: 'S'},
		{ID: "rip", Label: "RIP on/off", Key: 'R'}}
}

// TerminalAction switches what was chosen.
func (s *session) TerminalAction(id string) error {
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
