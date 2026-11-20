package app

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/view"
)

// TestCommandsDone: each command that a menu can name is one that the
// session does, and no other.
func TestCommandsDone(t *testing.T) {
	for name := range bbs.Commands {
		if _, ok := menuCommands[name]; !ok {
			t.Errorf("%s: in bbs.Commands, not done", name)
		}
	}
	for name := range menuCommands {
		if _, ok := bbs.Commands[name]; !ok {
			t.Errorf("%s: done, not in bbs.Commands", name)
		}
	}
	if classic["main"] == nil {
		t.Fatal("no built-in main menu")
	}
}

// menuFake is the fake, which keeps the menus that it was shown whole.
type menuFake struct {
	*fake
	menus []*view.Menu
}

func (f *menuFake) Menu(m *view.Menu) (string, error) {
	f.menus = append(f.menus, m)
	return f.fake.Menu(m)
}

// runMenusFor is run, with the menus kept.
func runMenusFor(t *testing.T, b *bbs.BBS, n *bbs.Node, script ...any) *menuFake {
	t.Helper()
	f := &menuFake{fake: &fake{t: t, b: b, script: script}}
	if err := Run(b, n, f); err != ErrLogoff {
		t.Fatalf("Run: %v; shown %q", err, f.shown)
	}
	if len(f.script) > 0 {
		t.Errorf("left of the script: %v; shown %q", f.script, f.shown)
	}
	return f
}

// labels is the labels of a menu's items.
func labels(m *view.Menu) []string {
	var out []string
	for _, it := range m.Items {
		out = append(out, it.Label)
	}
	return out
}

// TestBuiltinMain: the Classic main menu shows only what the caller can
// use: no file areas without files_dir, the sysop's tools for the sysop
// alone; Q logs off, but is not in the list.
func TestBuiltinMain(t *testing.T) {
	b, alice, sysop := setup(t)
	f := runMenusFor(t, b, alice, "Q")
	m := f.menus[0]
	if m.Title != "Main menu" || !slices.Contains(m.Hidden, "Q") || m.Line {
		t.Fatalf("%+v", m)
	}
	got := strings.Join(labels(m), ",")
	if got != "Message areas,E-mail,Who's online,User list,Your profile,System info,Chat,Page the sysop,"+
		"Offline mail (QWK),Last callers,Feedback to the sysop,Goodbye" {
		t.Errorf("alice's main menu: %s", got)
	}
	f = runMenusFor(t, b, sysop)
	if l := labels(f.menus[0]); !slices.Contains(l, "Sysop tools") {
		t.Errorf("the sysop's main menu: %q", l)
	}
}

// parseMenu is a menu file's menu.
func parseMenu(t *testing.T, name, body string) *bbs.Menu {
	t.Helper()
	cf, err := config.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	m, err := bbs.ParseMenuFile(name, cf)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestMenuStack: gosub, goto, return, main and PF3 move between menus; a
// chain stops at a command that moves; @FIRST is done once when a menu
// opens, @EVERY each time it shows; a word is a key; a menu that the
// caller may not have goes to its fail menu.
func TestMenuStack(t *testing.T) {
	b, alice, _ := setup(t)
	b.Menus = map[string]*bbs.Menu{
		"main": parseMenu(t, "main", `
[menu]
title = Main
[item X]
label = Extras
do = menu.gosub extras ; logoff
[item S]
label = Secret
do = menu.gosub secret
[item G]
label = Goodbye
do = logoff
`),
		"extras": parseMenu(t, "extras", `
[menu]
title = Extras
[item @first]
do = screen:first
[item @every]
do = screen:every
[item JUMP]
label = Jump
do = menu.goto other
[item R]
label = Back
do = menu.return
`),
		"other": parseMenu(t, "other", `
[menu]
title = Other
[item M]
label = Main
do = menu.main
`),
		"secret": parseMenu(t, "secret", `
[menu]
title = Secret
require = 200
fail = other
[item R]
hidden = yes
do = menu.return
`),
	}
	f := runMenusFor(t, b, alice,
		"X", "R", // into Extras (no logoff after it), back
		"X", "jump", "M", // Extras, Other in its place, Main
		"X", view.Back, // Extras, PF3 back
		"S", view.Back, // Secret is not for alice: Other in its place
		"G")
	var titles []string
	for _, m := range f.menus {
		titles = append(titles, m.Title)
	}
	want := "Main Extras Main Extras Other Main Extras Main Other Main"
	if got := strings.Join(titles, " "); got != want {
		t.Errorf("menus %s, want %s", got, want)
	}
	if !f.menus[1].Line || f.menus[0].Line {
		t.Error("Extras has a word key: the caller types a line; Main has none")
	}
	// the screens are not there: each is "nothing to show"; @first and
	// @every three times each (Extras opened three times), and "not for
	// you" once
	if n := strings.Count(strings.Join(f.said, "\n"), "There is nothing to show."); n != 3+3 {
		t.Errorf("%d screens shown; said %q", n, f.said)
	}
	has(t, "said", f.said, "That menu is not for you.")
}

// TestMenuNotes: @NOTE@ in a label is what its command adds: the count of
// new mail.
func TestMenuNotes(t *testing.T) {
	b, alice, sysop := setup(t)
	m := &store.Message{FromID: sysop.User().ID, From: "sysop", ToID: alice.User().ID, To: "alice",
		Subject: "Hi", Body: "Hello.", Created: time.Now()}
	if _, err := b.Store.SendMail(m); err != nil {
		t.Fatal(err)
	}
	f := runMenusFor(t, b, alice)
	if l := labels(f.menus[0]); !slices.Contains(l, "E-mail (1 new)") {
		t.Errorf("%q", l)
	}
}
