package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
)

// withScripts gives a BBS the sample scripts and a menu with them.
func withScripts(t *testing.T) func(*bbs.BBS, string) {
	return func(b *bbs.BBS, top string) {
		dir, err := filepath.Abs(filepath.Join("..", "..", "scripts.sample"))
		if err != nil {
			t.Fatal(err)
		}
		b.ScriptsDir = dir
		item := func(k, v string) bbs.MenuItem {
			it, err := bbs.ParseMenuItem(k, v)
			if err != nil {
				t.Fatal(err)
			}
			return it
		}
		b.Menus = map[string]*bbs.Menu{"main": {Name: "main", Title: "Main", Items: []bbs.MenuItem{
			item("o", "script:oneliners One-liners"), item("p", "script:poll Poll"), item("g", "logoff Goodbye")}}}
	}
}

// TestScriptOnBoth: the sample one-liners script on a text terminal and a
// 3270: the same screens, drawn by each.
func TestScriptOnBoth(t *testing.T) {
	addr := freeTCP(t)
	e := startFull(t, ui3270.Graphics{}, nil, withScripts(t),
		&Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"},
		&Listener{Name: "telnet", Kind: "telnet", Address: addr, TN3270: true})
	d := login(e, "alice", "alicepass1")
	d.send("O")
	d.expect("One-liners")
	d.expect("No one has left a line yet.")
	d.expect("[A]dd a line, [Q]uit: ")
	d.send("A")
	d.expect("Your line (Enter to give up): ")
	d.send("hello from a text terminal\r")
	d.expect("Added.")
	d.expect("alice:     hello from a text terminal")
	d.expect("[A]dd a line, [Q]uit: ")
	d.send("Q")
	d.expect("Command: ")

	if _, err := exec.LookPath("s3270"); err != nil {
		t.Skip("no s3270")
	}
	script := strings.Join([]string{
		"Connect(" + addr + ")", "Wait(10,InputField)",
		`String("sysop")`, "Tab()", `String("sysoppass1")`, "Enter()", "Wait(5,InputField)",
		`String("o")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		"PF(5)", "Wait(5,InputField)", "Ascii()",
		`String("and one from a 3270")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		"PF(3)", "Wait(5,InputField)", "Quit()", ""}, "\n")
	cmd := exec.Command("s3270", "-model", "3279-2")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("s3270: %v\n%s", err, out)
	}
	for _, want := range []string{"PF5=Add  PF3=Back", "alice:", "hello from a text terminal", "Your line",
		"Added.", "sysop:", "and one from a 3270"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the 3270 did not show %q:\n%s", want, out)
		}
	}
	v, _, _ := e.b.Store.ScriptGet("oneliners", 0, "lines")
	if !strings.Contains(v, `"text":"and one from a 3270"`) || !strings.Contains(v, `"who":"sysop"`) {
		t.Errorf("kept %s", v)
	}
}

// TestScriptsConfig: scripts_dir and script: menu items; a script not
// there, a bad name, no such directory and a script that does not compile
// refused.
func TestScriptsConfig(t *testing.T) {
	dir := t.TempDir()
	scripts := filepath.Join(dir, "scripts")
	os.Mkdir(scripts, 0755)
	os.WriteFile(filepath.Join(scripts, "hello.js"), []byte(`ui.say("hi");`), 0644)
	load := func(body string) (*Config, error) {
		p := filepath.Join(dir, "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load("[general]\nscripts_dir = " + scripts + "\n[menu main]\nh = script:hello Hello\n")
	if err != nil || c.ScriptsDir != scripts || c.Menus["main"].Items[0].Action != "script" {
		t.Fatalf("%+v %v", c, err)
	}
	for body, want := range map[string]string{
		"[menu main]\nh = script:hello Hello\n":                                      "no scripts_dir",
		"[general]\nscripts_dir = " + scripts + "\n[menu main]\nh = script:nope N\n": "there is no nope.js",
		"[general]\nscripts_dir = " + scripts + "\n[menu main]\nh = script:../x X\n": "a script's name",
		"[general]\nscripts_dir = " + filepath.Join(dir, "nope") + "\n":              "is not a directory",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
	os.WriteFile(filepath.Join(scripts, "broken.js"), []byte(`function (`), 0644)
	if _, err := load("[general]\nscripts_dir = " + scripts + "\n"); err == nil || !strings.Contains(err.Error(), "broken.js") {
		t.Errorf("a broken script: %v", err)
	}
}
