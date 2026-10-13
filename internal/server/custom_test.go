package server

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/screens"
	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
)

func TestCustomConfig(t *testing.T) {
	dir := t.TempDir()
	load := func(body string) (*Config, error) {
		p := filepath.Join(dir, "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load(`
[general]
screens_dir = ` + dir + `
[door lord]
command = /bin/true
[menu main]
title = @BBS@ main menu
screen = mainmenu
m = areas Message areas
l = door:LORD Legend of the Red Dragon
x = menu:extras More...
g = logoff Goodbye
[menu Extras]
b = screen:bulletins Bulletins
q = back Back
[strings]
command_prompt = "Your choice, @USER@: "
pause = (more)
`)
	if err != nil {
		t.Fatal(err)
	}
	m := c.Menus["main"]
	if m == nil || m.Screen != "mainmenu" || len(m.Items) != 4 || m.Items[1].Key != "L" ||
		m.Items[1].Action != "door" || m.Items[1].Arg != "LORD" || m.Items[2].Label != "More..." {
		t.Fatalf("%+v", m)
	}
	if c.Menus["extras"] == nil || c.Text["command_prompt"] != "Your choice, @USER@: " || c.Screens.Dir != dir {
		t.Fatalf("%+v %+v", c.Menus, c.Text)
	}
	for body, want := range map[string]string{
		"[menu main]\nm = nothing Hi\n":                  "no such action",
		"[menu main]\nm = areas\n":                       "a label",
		"[menu main]\nmm = areas Areas\n":                "one character",
		"[menu main]\nm = menu:nope Nope\n":              "there is no [menu nope]",
		"[menu main]\nm = door:nope Nope\n":              "there is no [door nope]",
		"[menu main]\nm = screen:bulletins Bulletins\n":  "no screens_dir",
		"[menu main]\nm = screen:../x X\n":               "a screen's name",
		"[menu main]\ntitle = x\n":                       "no items",
		"[menu]\nm = areas A\n":                          "needs a name",
		"[strings]\nnope = x\n":                          "no such string",
		"[menu a]\nm = areas A\n[menu A]\nm = areas A\n": "given twice",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
	c, err = load("[general]\nscreens_dir = " + filepath.Join(dir, "nope") + "\n")
	if err != nil || !strings.Contains(strings.Join(c.Warnings, "\n"), "is not a directory") {
		t.Errorf("%v %v", c.Warnings, err)
	}
}

// customize gives a BBS screens, menus and strings (startFull's prep).
func customize(t *testing.T, files map[string]string) func(*bbs.BBS, string) {
	return func(b *bbs.BBS, top string) { customizeBBS(t, b, top, files) }
}

func customizeBBS(t *testing.T, b *bbs.BBS, top string, files map[string]string) {
	dir := filepath.Join(top, "screens")
	os.Mkdir(dir, 0755)
	for name, body := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(body), 0644)
	}
	b.Screens = &screens.Set{Dir: dir}
	item := func(k, v string) bbs.MenuItem {
		it, err := bbs.ParseMenuItem(k, v)
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	b.Menus = map[string]*bbs.Menu{
		"main": {Name: "main", Title: "@BBS@ main", Items: []bbs.MenuItem{
			item("m", "areas Message areas"), item("b", "screen:bulletin Bulletin"),
			item("x", "menu:extras Extras"), item("g", "logoff Goodbye")}},
		"extras": {Name: "extras", Title: "Extras", Prompt: "Extras> ", Items: []bbs.MenuItem{
			item("w", "who Who's online"), item("q", "back Back")}},
	}
	b.Text = map[string]string{"name_prompt": "Who are you? ", "new_name_prompt": "Who are you (or NEW)? ",
		"password_prompt": "Secret word: ", "pause": "(more)", "logoff_confirm": "Leave now, @USER@?"}
}

// TestCustomText: the sysop's screens, menus and strings on the text BBS.
func TestCustomText(t *testing.T) {
	e := startFull(t, ui3270.Graphics{}, nil, customize(t, map[string]string{
		"logon.ans":    "\x1b[1;36m\xc9\xcd\xcd\xbb @BBS@ \xc8\xcd\xcd\xbc\x1b[0m\r\n\x1aSAUCE00 not shown",
		"welcome.asc":  "Hello @USER@, @NEWMAIL@ new mail, node @NODE:3@.\n",
		"bulletin.asc": "Bulletin one\n@PAUSE@Bulletin two\n",
		"goodbye.asc":  "Come back soon, @USER@!\n",
	}), &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "yes", UTF8: "no"})
	d := e.dial("modem.sock")
	d.expect("\xc9\xcd\xcd\xbb Test BBS \xc8\xcd\xcd\xbc") // CP437, as it is
	d.expect("Who are you (or NEW)? ")
	d.send("alice\r")
	d.expect("Secret word: ")
	d.send("alicepass1\r")
	d.expect("Hello alice, 0 new mail, node 1  .")
	d.expect("Test BBS main")
	d.expect("[B]")
	d.expect("Command: ")
	d.send("B")
	d.expect("Bulletin one")
	d.expect("(more)")
	d.send(" ")
	d.expect("Bulletin two")
	d.expect("(more)")
	d.send(" ")
	d.expect("Command: ")
	d.send("X")
	d.expect("Extras> ")
	d.send("Q")
	d.expect("Test BBS main")
	d.expect("Command: ")
	d.send("G")
	d.expect("Leave now, alice?")
	d.send("Y")
	d.expect("Come back soon, alice!")
	if strings.Contains(string(d.got), "SAUCE") {
		t.Error("SAUCE shown")
	}
}

// TestCustomTags: {tags} in a text screen, the strings and a menu's
// prompt, as ANSI on an ANSI terminal, dropped on a plain one; a
// caller's braces are not tags.
func TestCustomTags(t *testing.T) {
	files := map[string]string{
		"welcome.asc":  "{clear}\n{yellow}{bright}Hello @USER@{reset}, node @NODE@.\n",
		"bulletin.asc": "{red}Red{default} plain {on blue}{white}box{reset}\n{pause}\n{cyan}after\n",
	}
	tags := func(b *bbs.BBS, top string) {
		customizeBBS(t, b, top, files)
		b.Text["command_prompt"] = "{green}Your choice{default}: "
		b.Menus["extras"].Prompt = "{magenta}Extras{reset}> "
	}
	e := startFull(t, ui3270.Graphics{}, nil, tags,
		&Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "yes", UTF8: "no"},
		&Listener{Name: "plain", Kind: "raw", Address: "plain.sock", ANSI: "no", UTF8: "no"})

	d := e.dial("modem.sock")
	d.expect("Who are you (or NEW)? ")
	d.send("alice\r")
	d.expect("Secret word: ")
	d.send("alicepass1\r")
	d.expect("\x1b[2J\x1b[H\x1b[33m\x1b[1mHello alice\x1b[0m, node 1.")
	d.expect("\x1b[32mYour choice\x1b[0m: ")
	d.send("B")
	d.expect("\x1b[31mRed\x1b[0m plain \x1b[44m\x1b[37mbox\x1b[0m")
	d.expect("(more)")
	d.send(" ")
	d.expect("\x1b[36mafter\r\n\x1b[0m") // plain again after the screen
	d.expect("(more)")
	d.send(" ")
	d.expect("\x1b[32mYour choice\x1b[0m: ")
	d.send("X")
	d.expect("\x1b[35mExtras\x1b[0m> ")
	if strings.ContainsAny(string(d.got), "{\x1e") {
		t.Errorf("a tag or a mark shown: %q", d.got)
	}

	p := e.dial("plain.sock")
	p.expect("Who are you (or NEW)? ")
	p.send("alice\r")
	p.expect("Secret word: ")
	p.send("alicepass1\r")
	p.expect("Hello alice, node 2.")
	p.expect("Your choice: ")
	p.send("B")
	p.expect("Red plain box")
	p.expect("(more)")
	p.send(" ")
	p.expect("after")
	p.expect("(more)")
	p.send(" ")
	p.expect("Your choice: ")
	if strings.ContainsAny(string(p.got), "{\x1e") {
		t.Errorf("a tag or a mark shown: %q", p.got)
	}
}

// TestCustom3270: the same on a 3270 (s3270): a .3270 logon screen in
// colors, the welcome screen, the sysop's main menu, a screen from it.
func TestCustom3270(t *testing.T) {
	if _, err := exec.LookPath("s3270"); err != nil {
		t.Skip("no s3270")
	}
	addr := freeTCP(t)
	startFull(t, ui3270.Graphics{}, nil, customize(t, map[string]string{
		"logon.3270":   "{yellow}{bright}Welcome to @BBS@{normal} {blue}on a mainframe\n",
		"welcome.3270": "{green}Hello @USER@.\n",
		"bulletin.asc": "{yellow}A plain bulletin{blink}\n", // a text terminal's tags: a 3270 has blink none
		"goodbye.3270": "{white}So long, @USER@.\n",
	}), &Listener{Name: "telnet", Kind: "telnet", Address: addr, TN3270: true})
	script := strings.Join([]string{
		"Connect(" + addr + ")", "Wait(10,InputField)", "Ascii()",
		`String("alice")`, "Tab()", `String("alicepass1")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		"Enter()", "Wait(5,InputField)", "Ascii()",
		`String("b")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		"Enter()", "Wait(5,InputField)",
		`String("g")`, "Enter()", "Wait(3,Seconds)", "Ascii()", "Quit()", ""}, "\n")
	cmd := exec.Command("s3270", "-model", "3279-2")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("s3270: %v\n%s", err, out)
	}
	for _, want := range []string{"Welcome to Test BBS", "on a mainframe", "Hello alice.",
		"Test BBS main", "B  Bulletin", "A plain bulletin", "So long, alice."} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "{") {
		t.Errorf("a tag shown:\n%s", out)
	}
}

func freeTCP(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}
