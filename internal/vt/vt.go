// Package vt identifies DEC VT terminals and those that answer as one, as
// DEC's terminals were asked: the answerback message (ENQ), the primary
// and secondary device attributes (DA1, DA2), xterm's version (XTVERSION),
// SyncTERM's CTerm attributes, and a VT52's identify (ESC Z).  It also
// writes for them: DECWriter (DEC Special Graphics and Latin-1 for what
// UTF-8 would carry) and VT52Writer (a VT52's escapes for ANSI's).
package vt

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Query asks an ANSI terminal everything at once: its answerback, DA1,
// DA2, XTVERSION, its character cell (xterm's CSI 16 t), and last its
// cursor position, which every ANSI terminal answers: once that comes,
// whatever else would has.
const Query = "\x05\x1b[c\x1b[>c\x1b[>0q\x1b[16t\x1b[6n"

// QueryVT52 is a VT52's identify; a VT52 answers ESC / and a letter.
const QueryVT52 = "\x1bZ"

var (
	da1Pattern  = regexp.MustCompile(`\x1b\[\?([0-9;]*)c`)
	da2Pattern  = regexp.MustCompile(`\x1b\[>([0-9;]*)c`)
	ctermPat    = regexp.MustCompile(`\x1b\[=([0-9;]*)c`)
	xtverPat    = regexp.MustCompile(`\x1bP>\|([^\x1b\x07]{0,80})\x1b\\`)
	cellPattern = regexp.MustCompile(`\x1b\[6;(\d+);(\d+)t`)
	cprPattern  = regexp.MustCompile(`\x1b\[(\d+);(\d+)R`)
	vt52Pattern = regexp.MustCompile(`\x1b/([A-Za-z])`)
)

// Done reports whether a reply to Query is complete (the position report
// has come).
func Done(reply []byte) bool { return cprPattern.Match(reply) }

// Info is what a terminal said of itself.
type Info struct {
	Answerback string // ENQ's answer, if any
	DA1        []int  // primary attributes: the class (1, 6, 62-65) and options
	DA2        []int  // secondary: the model, its firmware version, options
	Version    string // XTVERSION's "name(version)"
	CTerm      string // SyncTERM's CTerm version
	VT52       bool   // answered as a VT52
	CellW      int    // a character cell in pixels, if said
	CellH      int
}

// Parse reads a terminal's answers.
func Parse(reply []byte) Info {
	var in Info
	// the answerback is what came before the first escape sequence
	ab := string(reply)
	if i := strings.IndexByte(ab, 0x1b); i >= 0 {
		ab = ab[:i]
	}
	in.Answerback = printable(ab, 40)
	if m := da1Pattern.FindSubmatch(reply); m != nil {
		in.DA1 = numbers(string(m[1]))
	}
	if m := da2Pattern.FindSubmatch(reply); m != nil {
		in.DA2 = numbers(string(m[1]))
	}
	if m := xtverPat.FindSubmatch(reply); m != nil {
		in.Version = printable(string(m[1]), 60)
	}
	if m := ctermPat.FindSubmatch(reply); m != nil {
		// "CTerm" in decimal character codes, then the version
		ns := numbers(string(m[1]))
		if len(ns) >= 5 && string([]byte{byte(ns[0]), byte(ns[1]), byte(ns[2]), byte(ns[3]), byte(ns[4])}) == "CTerm" {
			var v []string
			for _, n := range ns[5:] {
				v = append(v, strconv.Itoa(n))
			}
			in.CTerm = strings.Join(v, ".")
			if in.CTerm == "" {
				in.CTerm = "?"
			}
		}
	}
	if m := cellPattern.FindSubmatch(reply); m != nil {
		h, _ := strconv.Atoi(string(m[1]))
		w, _ := strconv.Atoi(string(m[2]))
		if w >= 4 && w <= 64 && h >= 4 && h <= 128 {
			in.CellW, in.CellH = w, h
		}
	}
	if vt52Pattern.Match(reply) {
		in.VT52 = true
	}
	return in
}

func numbers(s string) []int {
	var out []int
	for _, f := range strings.Split(s, ";") {
		if f == "" {
			continue
		}
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || n > 1<<20 {
			continue
		}
		out = append(out, n)
		if len(out) == 32 {
			break
		}
	}
	return out
}

func printable(s string, max int) string {
	var sb strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f && sb.Len() < max {
			sb.WriteRune(r)
		}
	}
	return strings.TrimSpace(sb.String())
}

// Answered reports whether the terminal said anything of itself.
func (in Info) Answered() bool {
	return in.DA1 != nil || in.DA2 != nil || in.Version != "" || in.CTerm != "" || in.VT52 || in.Answerback != ""
}

// Level is the VT level the terminal works at: 1 for a VT100 or VT102,
// 2 for a VT220, 3 VT320, 4 VT420, 5 VT510 and later; 0 if it did not say.
func (in Info) Level() int {
	if len(in.DA1) == 0 {
		return 0
	}
	switch c := in.DA1[0]; {
	case c >= 61 && c <= 69:
		return c - 60
	case c == 1 || c == 6 || c == 4 || c == 7 || c == 12:
		return 1
	}
	return 0
}

// Has reports a DA1 option of a level 2 or later terminal: 1 132 columns,
// 2 printer, 3 ReGIS, 4 sixel, 6 selective erase, 7 soft characters, 8
// user defined keys, 9 national characters, 15 technical characters, 18
// windowing, 21 horizontal scrolling, 22 ANSI color, 28 rectangular
// editing.
func (in Info) Has(opt int) bool {
	if in.Level() < 2 {
		return false
	}
	for _, o := range in.DA1[1:] {
		if o == opt {
			return true
		}
	}
	return false
}

// Sixel: the terminal draws sixel graphics.
func (in Info) Sixel() bool { return in.Has(4) }

// decModels are DA2's model numbers of DEC's own terminals.
var decModels = map[int]string{
	0: "VT100", 1: "VT220", 2: "VT240", 18: "VT330", 19: "VT340", 24: "VT320",
	28: "DECterm", 32: "VT382", 41: "VT420", 61: "VT510", 64: "VT520", 65: "VT525",
}

// emulators are DA2's model numbers emulators use for themselves.
var emulators = map[int]string{77: "mintty", 82: "rxvt", 83: "screen", 84: "tmux", 85: "rxvt-unicode"}

// Hardware reports a DEC terminal by its DA2 (a model of DEC's, a small
// firmware version), not an emulator saying its name otherwise.
func (in Info) Hardware() bool {
	if in.Version != "" || in.CTerm != "" || len(in.DA2) < 2 {
		return false
	}
	_, dec := decModels[in.DA2[0]]
	return dec && in.DA2[1] < 95
}

// Mono: a DEC terminal without color (the VT100s to the VT520: bold,
// underline, blink and reverse instead).
func (in Info) Mono() bool {
	return in.Hardware() && in.DA2[0] != 65 && !in.Has(22)
}

// Name is what the terminal is, as best it said: XTVERSION's name, SyncTERM,
// the answerback, a DEC model, an emulator, or its VT class.
func (in Info) Name() string {
	switch {
	case in.Version != "":
		return in.Version
	case in.CTerm != "":
		return "SyncTERM (CTerm " + in.CTerm + ")"
	case in.VT52:
		return "VT52"
	case in.Hardware():
		return decModels[in.DA2[0]]
	}
	if len(in.DA2) >= 2 {
		p, v := in.DA2[0], in.DA2[1]
		switch {
		case emulators[p] != "":
			return emulators[p]
		case p == 41 && v >= 95:
			return fmt.Sprintf("xterm (patch %d)", v)
		case (p == 1 || p == 65) && v >= 1000:
			return "VTE"
		}
	}
	if in.Answerback != "" {
		return in.Answerback
	}
	switch l := in.Level(); {
	case l == 1 && in.DA1[0] == 6:
		return "VT102"
	case l == 1:
		return "VT100"
	case l >= 2:
		return fmt.Sprintf("VT%d00 class", l) // 62: VT200 class, ...
	}
	return ""
}

// Details is the answers, for the log.
func (in Info) Details() string {
	var parts []string
	join := func(ns []int) string {
		var s []string
		for _, n := range ns {
			s = append(s, strconv.Itoa(n))
		}
		return strings.Join(s, ";")
	}
	if in.DA1 != nil {
		parts = append(parts, "DA1 "+join(in.DA1))
	}
	if in.DA2 != nil {
		parts = append(parts, "DA2 "+join(in.DA2))
	}
	if in.Version != "" {
		parts = append(parts, "version "+in.Version)
	}
	if in.CTerm != "" {
		parts = append(parts, "CTerm "+in.CTerm)
	}
	if in.Answerback != "" {
		parts = append(parts, fmt.Sprintf("answerback %q", in.Answerback))
	}
	if in.VT52 {
		parts = append(parts, "VT52")
	}
	return strings.Join(parts, ", ")
}
