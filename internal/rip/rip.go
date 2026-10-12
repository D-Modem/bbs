// Package rip writes RIPscrip 1.54 (TeleGrafix's Remote Imaging Protocol),
// the vector graphics and mouse buttons of RIPterm and SyncTERM: an ANSI
// terminal that also draws on a 640x350, 16-color EGA screen.
//
// A RIP command line begins with "!" in the first column; its commands
// follow, each after a "|": a command letter (level 1 commands after a
// "1") and its parameters as MegaNums, base-36 numbers of fixed widths.
package rip

import (
	"fmt"
	"regexp"
	"strings"
)

// Screen size, in pixels and (in the 8x14 font) characters.
const (
	Width  = 640
	Height = 350
	Cols   = 80
	Rows   = 25
)

// EGA colors.
const (
	Black = iota
	Blue
	Green
	Cyan
	Red
	Magenta
	Brown
	LightGray
	DarkGray
	LightBlue
	LightGreen
	LightCyan
	LightRed
	LightMagenta
	Yellow
	White
)

// Query is what asks a terminal whether it has RIP; one that has answers
// with Version's pattern ("RIPSCRIP015400").  It must be followed by an
// escape sequence (others take the "!" as the start of one).
const Query = "\x1b[!"

// Version reads a terminal's answer to Query.
var Version = regexp.MustCompile(`RIPSCRIP(\d{6})`)

// Meganum writes n in base 36, width digits.
func Meganum(n, width int) string {
	const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if n < 0 {
		n = 0
	}
	b := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		b[i] = digits[n%36]
		n /= 36
	}
	return string(b)
}

// escape protects RIP's own characters in a text parameter, and drops
// control characters.
func escape(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '!' || r == '|' || r == '\\':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case r < 0x20 || r == 0x7f:
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// Script is a RIP scene being built: a line per command.
type Script struct{ sb strings.Builder }

func (s *Script) cmd(name string, args ...any) *Script {
	s.sb.WriteString("!|" + name)
	for _, a := range args {
		switch v := a.(type) {
		case mn:
			s.sb.WriteString(Meganum(v.n, v.w))
		case string:
			s.sb.WriteString(v)
		default:
			panic(fmt.Sprintf("rip: %T", a))
		}
	}
	s.sb.WriteString("\r\n")
	return s
}

type mn struct{ n, w int }

func m2(n int) mn { return mn{n, 2} }

// String is the scene: CR LF ended lines, each beginning with "!".
func (s *Script) String() string { return s.sb.String() }

// Reset is RIP_RESET_WINDOWS: the text window the whole screen, the
// graphics cleared, mouse fields gone, the palette as it was.
func (s *Script) Reset() *Script { return s.cmd("*") }

// TextWindow is RIP_TEXT_WINDOW, in characters (size 2: the 8x14 font,
// 80x25), with wrapping.
func (s *Script) TextWindow(x0, y0, x1, y1 int) *Script {
	return s.cmd("w", m2(x0), m2(y0), m2(x1), m2(y1), "1", "2")
}

// EraseWindow is RIP_ERASE_WINDOW: the text window cleared.
func (s *Script) EraseWindow() *Script { return s.cmd("e") }

// Color is RIP_COLOR: lines' and text's color.
func (s *Script) Color(c int) *Script { return s.cmd("c", m2(c)) }

// FillStyle is RIP_FILL_STYLE: solid (pattern 1) in a color.
func (s *Script) FillStyle(color int) *Script { return s.cmd("S", m2(1), m2(color)) }

// Bar is RIP_BAR: a rectangle filled in the fill style, no border.
func (s *Script) Bar(x0, y0, x1, y1 int) *Script {
	return s.cmd("B", m2(x0), m2(y0), m2(x1), m2(y1))
}

// Rectangle is RIP_RECTANGLE: its outline, in the color.
func (s *Script) Rectangle(x0, y0, x1, y1 int) *Script {
	return s.cmd("R", m2(x0), m2(y0), m2(x1), m2(y1))
}

// Line is RIP_LINE.
func (s *Script) Line(x0, y0, x1, y1 int) *Script {
	return s.cmd("L", m2(x0), m2(y0), m2(x1), m2(y1))
}

// Font is RIP_FONT_STYLE: font 0 (the 8x8 bitmap) or a stroked one
// (1 triplex, 2 small, 3 sans serif, 4 gothic), horizontal, size 1-10.
func (s *Script) Font(font, size int) *Script {
	return s.cmd("Y", m2(font), m2(0), m2(size), m2(0))
}

// Text is RIP_TEXT_XY: text at a point, in the color and font.
func (s *Script) Text(x, y int, text string) *Script {
	return s.cmd("@", m2(x), m2(y), escape(text))
}

// KillMouseFields is RIP_KILL_MOUSE_FIELDS.
func (s *Script) KillMouseFields() *Script { return s.cmd("1K") }

// Button flags (RIP_BUTTON_STYLE).
const (
	Invertable     = 2
	Chisel         = 8
	Recessed       = 16
	Dropshadow     = 32
	Plain          = 256
	Bevel          = 512
	Mouse          = 1024
	UnderlineHot   = 2048
	CenterVertical = 8192
)

// ButtonStyle is RIP_BUTTON_STYLE for the buttons after it: their size
// from their own corners, the label centered, beveled.
func (s *Script) ButtonStyle(flags, label, surface, bright, dark, underline int) *Script {
	return s.cmd("1B", m2(0), m2(0), m2(2), mn{flags, 4}, m2(2), m2(label), m2(Black),
		m2(bright), m2(dark), m2(surface), m2(0), m2(0), m2(underline), m2(0), "000000")
}

// Button is RIP_BUTTON: a button in the style, labeled, that sends send
// when clicked or when its hot key is pressed (0: none).
func (s *Script) Button(x0, y0, x1, y1 int, hot rune, label, send string) *Script {
	return s.cmd("1U", m2(x0), m2(y0), m2(x1), m2(y1), m2(int(hot)), "0", "0",
		"<>"+escape(label)+"<>"+escape(send))
}

// End is RIP_NO_MORE: the scene is complete.
func (s *Script) End() *Script { return s.cmd("#") }

// Item is a menu's choice.
type Item struct {
	Key   string // what it sends: its key
	Label string
}

// Menu is a scene of a title and a button for each item, two columns of
// them, with the text window below for the prompt; the rows the text
// window begins at.
func Menu(title string, items []Item) (string, int) {
	s := &Script{}
	s.Reset()
	s.FillStyle(Blue).Bar(0, 0, Width-1, Height-1)
	s.FillStyle(DarkGray).Bar(0, 0, Width-1, 27)
	s.Color(White).Font(1, 2).Text(12, 2, title)
	s.Color(LightCyan).Line(0, 28, Width-1, 28)
	const top, h, gap, w = 40, 26, 8, 300
	rows := (len(items) + 1) / 2
	s.ButtonStyle(Invertable|Plain|Bevel|Mouse|UnderlineHot|CenterVertical, Black, LightGray, White, DarkGray, Red)
	for i, it := range items {
		col, row := i/rows, i%rows
		x := 14 + col*(w+12)
		y := top + row*(h+gap)
		hot := rune(0)
		if r := []rune(it.Key); len(r) == 1 {
			hot = r[0]
		}
		s.Button(x, y, x+w, y+h, hot, "["+it.Key+"] "+it.Label, it.Key)
	}
	// text below the buttons, in 14-pixel rows
	textRow := min((top+rows*(h+gap)+13)/14+1, Rows-3)
	s.FillStyle(Black).Bar(0, textRow*14, Width-1, Height-1)
	s.TextWindow(0, textRow, Cols-1, Rows-1)
	s.End()
	return s.String(), textRow
}
