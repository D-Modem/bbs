package rip

import (
	"strings"
	"testing"
)

// TestMeganum: numbers in base 36 to their width, a negative as 0.
func TestMeganum(t *testing.T) {
	for _, c := range []struct {
		n, w int
		want string
	}{{0, 2, "00"}, {35, 2, "0Z"}, {36, 2, "10"}, {639, 2, "HR"}, {349, 2, "9P"}, {1295, 2, "ZZ"},
		{3842, 4, "02YQ"}, {-5, 2, "00"}} {
		if got := Meganum(c.n, c.w); got != c.want {
			t.Errorf("%d/%d: %s, want %s", c.n, c.w, got, c.want)
		}
	}
}

// TestScript: commands as RIP has them, text escaped and controls dropped;
// and the version is found in a terminal's answer.
func TestScript(t *testing.T) {
	s := (&Script{}).Reset().Color(White).Text(10, 20, "a|b!c\\d\x1b").Bar(0, 0, 639, 349).
		Button(1, 2, 3, 4, 'M', "[M] Mail", "M").End()
	want := "!|*\r\n!|c0F\r\n!|@0A0Ka\\|b\\!c\\\\d\r\n!|B0000HR9P\r\n!|1U010203042500<>[M] Mail<>M\r\n!|#\r\n"
	if s.String() != want {
		t.Errorf("%q\nwant %q", s.String(), want)
	}
	if m := Version.FindStringSubmatch("junk RIPSCRIP015400 more"); m == nil || m[1] != "015400" {
		t.Errorf("version: %v", m)
	}
}

// TestMenu: a menu is RIP lines only: its title, a button a choice (in
// columns of three), and the text window put below them.
func TestMenu(t *testing.T) {
	items := []Item{{"M", "Message areas"}, {"E", "E-mail"}, {"W", "Who's online"}, {"G", "Goodbye"}, {"P", "Profile"}}
	s, row := Menu("Main menu", items)
	for _, want := range []string{"!|*\r\n", "!|@0C02Main menu\r\n", "!|1B0000020" /* style */, "<>[M] Message areas<>M\r\n",
		"<>[G] Goodbye<>G\r\n", "!|w00" + Meganum(row, 2) + "270O12\r\n", "!|#\r\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q in\n%s", want, s)
		}
	}
	// every line a RIP line
	for _, l := range strings.Split(strings.TrimSuffix(s, "\r\n"), "\r\n") {
		if !strings.HasPrefix(l, "!|") {
			t.Errorf("line %q", l)
		}
	}
	// three rows of buttons: the first column has M E W, the second G P
	if !strings.Contains(s, "!|1U0E14"+Meganum(14+300, 2)) || row < 10 || row > 22 {
		t.Errorf("layout: row %d\n%s", row, s)
	}
}
