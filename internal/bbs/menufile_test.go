package bbs

import (
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/config"
)

// TestParseMenuFile: a menu file's menu, items, chains and special keys;
// what it refuses.
func TestParseMenuFile(t *testing.T) {
	parse := func(body string) (*Menu, error) {
		cf, err := config.Parse(strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		return ParseMenuFile("msg", cf)
	}
	m, err := parse(`
[menu]
title = {cyan}Messages{reset}
prompt = "@USER@> "
require = 20
fail = main
[item r]
label = Read
do = areas
[item new]
label = New, then mail
do = areas ; mail.read
[item @first]
do = screen:msgnews
[item x]
hidden = yes
do = menu:Extras
`)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "msg" || m.Prompt != "@USER@> " || m.Fail != "main" || len(m.Items) != 4 || !m.Words() {
		t.Fatalf("%+v", m)
	}
	if it := m.Item("new"); it == nil || len(it.Do) != 2 || it.Do[1] != (Command{Name: "mail"}) {
		t.Errorf("NEW: %+v", it)
	}
	if it := m.Special("@FIRST"); it == nil || it.Do[0] != (Command{Name: "screen", Arg: "msgnews"}) || m.Item("@first") != nil {
		t.Errorf("@FIRST: %+v", it)
	}
	if it := m.Item("X"); it == nil || !it.Hidden || it.Do[0] != (Command{Name: "menu.gosub", Arg: "extras"}) ||
		it.Action != "menu" || it.Arg != "extras" {
		t.Errorf("X: %+v", it)
	}
	for body, want := range map[string]string{
		"[item a]\nlabel = A\n":                                "no command",
		"[item a]\nlabel = A\ndo = nothing\n":                  "no such command",
		"[item a]\ndo = areas\n":                               "needs a label",
		"[item a]\nlabel = A\ndo = areas now\n":                "takes no data",
		"[item a]\nlabel = A\ndo = door\n":                     "needs a name",
		"[item a]\nlabel = A\ndo = screen ../x\n":              "a screen's name",
		"[item @last]\ndo = areas\n":                           "special keys",
		"[item toolongforakey]\nlabel = A\ndo = areas\n":       "a word of 2 to 12",
		"[item a-b]\nlabel = A\ndo = areas\n":                  "a word of 2 to 12",
		"[item a]\nlabel = A\ndo = areas\n[item A]\ndo = mail": "given twice",
		"[item a]\nlabel = A\ndo = areas\ncolour = red\n":      "has no colour",
		"[menu]\nsize = 2\n[item a]\nlabel = A\ndo = areas\n":  "has no size",
		"[door x]\n":              "[menu] and [item KEY]",
		"[menu]\ntitle = Empty\n": "no items",
	} {
		if _, err := parse(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}
