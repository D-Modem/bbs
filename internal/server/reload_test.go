package server

import (
	"bytes"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
)

const reloadConf = `[general]
; the system's name
name = Test BBS
fail_delay = 0

[listen modem]
type = raw
address = 127.0.0.1:2323
`

// TestReload: a file read again puts what may change while callers are
// on in force, and says what waits; a file the BBS would not start with
// changes nothing.
func TestReload(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bbs.conf")
	if err := writeFile(p, reloadConf); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	b := bbs.New(cfg.Settings, nil, log.New(io.Discard, "", 0))
	r := NewReloader(p, b)
	writeFile(p, strings.Replace(reloadConf, "name = Test BBS", "name = New BBS\nidle_timeout = 900", 1)+
		"max_per_ip = 2\n\n[strings]\nwelcome = Hi, @USER@.\n\n[level 10]\ntime = 30\n")
	applied, restart, err := r.Reload()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"~[general] name = New BBS (was Test BBS)", "+[general] idle_timeout = 900",
		"+[strings] welcome = Hi, @USER@.", "+[level 10] time = 30"} {
		if !containsAny(applied, want) {
			t.Errorf("applied %q: no %q", applied, want)
		}
	}
	if len(restart) != 1 || restart[0] != "+[listen modem] max_per_ip = 2" {
		t.Errorf("restart %q", restart)
	}
	if b.Conf().Name != "New BBS" || b.Conf().IdleTimeout.Seconds() != 900 || len(b.Conf().Levels) != 1 {
		t.Errorf("not in force: %+v", b.Conf())
	}
	if b.Settings.Name != "Test BBS" {
		t.Error("the settings begun with were changed")
	}

	writeFile(p, strings.Replace(reloadConf, "fail_delay = 0", "max_nodes = lots", 1))
	if _, _, err := r.Reload(); err == nil || !strings.Contains(err.Error(), "max_nodes") {
		t.Fatalf("a bad file: %v", err)
	}
	if b.Conf().Name != "New BBS" {
		t.Error("a bad file changed something")
	}
	// put right: the changes are from the last in force
	writeFile(p, reloadConf)
	if applied, _, _ := r.Reload(); !containsAny(applied, "name = Test BBS (was New BBS)") {
		t.Errorf("applied %q", applied)
	}
}

// configEnv is a BBS run from a file of its own, its Configuration on
// the sysop's menu.
func configEnv(t *testing.T, ansi string) (*env, string) {
	var p string
	e := startFull(t, ui3270.Graphics{}, nil, func(b *bbs.BBS, dir string) {
		p = filepath.Join(dir, "bbs.conf")
		if err := writeFile(p, reloadConf); err != nil {
			t.Fatal(err)
		}
		r := NewReloader(p, b)
		b.Config = &bbs.ConfigTools{Path: p, Check: Check, Reload: r.Reload}
	}, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: ansi, VT: "no", RIP: "no", Sixel: "no"})
	return e, p
}

// saved: the name is written, kept in bbs.conf.bak, and in force for the
// next caller.
func saved(t *testing.T, e *env, p string) {
	if e.b.Conf().Name != "Renamed BBS" {
		t.Errorf("name in force: %q", e.b.Conf().Name)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "; the system's name\nname = Renamed BBS\n") {
		t.Errorf("written:\n%s", data)
	}
	if old, _ := os.ReadFile(p + ".bak"); string(old) != reloadConf {
		t.Errorf("bbs.conf.bak:\n%s", old)
	}
	c := e.dial("modem.sock")
	c.expect("Renamed BBS")
}

// TestConfigScreens: on a terminal without ANSI, the sysop changes the
// system's name on the Configuration's numbered screens and saves.
func TestConfigScreens(t *testing.T) {
	e, p := configEnv(t, "no")
	s := login(e, "sysop", "sysoppass1")
	s.send("*")
	s.expect("[F] Configuration")
	s.send("F")
	s.expect("Board configuration >")
	s.expect("[S]ave and exit, [X] Exit without saving")
	s.send("1\r")
	s.expect("BBS name")
	s.send("1\r")
	s.expect("BBS name: Test BBS")
	s.send("\x15Renamed BBS\r")
	s.expect("1 change not saved")
	if data, _ := os.ReadFile(p); string(data) != reloadConf {
		t.Fatal("written before the save")
	}
	s.send("S\r")
	s.expect("In force now:")
	s.expect("~[general] name = Renamed BBS")
	saved(t, e, p)
}

// TestConfigScreensANSI: on an ANSI terminal, the full screen: the arrow
// keys, Enter to change a value, F10 to save and leave.
func TestConfigScreensANSI(t *testing.T) {
	e, p := configEnv(t, "yes")
	s := ansiLogin(e)
	s.send("*")
	s.expect("Configuration")
	s.send("F")
	s.expect("F10 save+exit")
	s.send("\x1b[B\x1b[A\r") // down, up: Board configuration
	s.expect("Configuration > Board configuration")
	s.send("\r")
	s.expect("BBS name: ")
	s.send("\x15Renamed BBS\r")
	s.expect("1 change not saved")
	s.send("\x1b[21~") // F10
	s.expect("In force now:")
	saved(t, e, p)
}

// TestConfigQuit: F9 leaves without saving, after a yes.
func TestConfigQuit(t *testing.T) {
	e, p := configEnv(t, "yes")
	s := ansiLogin(e)
	s.send("*F")
	s.expect("F10 save+exit")
	s.send("\r\r")
	s.expect("BBS name: ")
	s.send("\x15Not Saved\r")
	s.expect("1 change not saved")
	s.send("\x1b[20~") // F9
	s.expect("Leave without saving 1 changes?")
	s.send("y")
	s.expect("Sysop tools")
	if data, _ := os.ReadFile(p); string(data) != reloadConf || e.b.Conf().Name == "Not Saved" {
		t.Errorf("saved:\n%s", data)
	}
}

// ansiLogin logs the sysop in on an ANSI terminal (the prompts in color).
func ansiLogin(e *env) *dialog {
	d := e.dial("modem.sock")
	d.expect("(or NEW)")
	d.send("sysop\r")
	d.expect("Password")
	d.send("sysoppass1\r")
	d.expect("Command")
	return d
}

// TestConfig3270: on a 3270, S by a menu opens it, a value is typed over
// and sent with Enter, PF10 saves.
func TestConfig3270(t *testing.T) {
	if _, err := exec.LookPath("s3270"); err != nil {
		t.Skip("no s3270")
	}
	addr := freeTCP(t)
	var p string
	e := startFull(t, ui3270.Graphics{}, nil, func(b *bbs.BBS, dir string) {
		p = filepath.Join(dir, "bbs.conf")
		writeFile(p, reloadConf)
		r := NewReloader(p, b)
		b.Config = &bbs.ConfigTools{Path: p, Check: Check, Reload: r.Reload}
	}, &Listener{Name: "telnet", Kind: "telnet", Address: addr, TN3270: true},
		&Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	script := strings.Join([]string{
		"Connect(" + addr + ")", "Wait(10,InputField)",
		`String("sysop")`, "Tab()", `String("sysoppass1")`, "Enter()", "Wait(5,InputField)",
		`String("*")`, "Enter()", "Wait(5,InputField)",
		`String("f")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		`String("s")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		"EraseEOF()", `String("Renamed BBS")`, "Enter()", "Wait(5,InputField)", "Ascii()",
		"PF(10)", "Wait(5,InputField)", "Ascii()", "Quit()", ""}, "\n")
	cmd := exec.Command("s3270", "-model", "3279-2")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("s3270: %v\n%s", err, out)
	}
	for _, want := range []string{"Board configuration >", "PF10=Save+exit", "BBS name", "1 change not saved",
		"In force now:"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	if os.Getenv("SHOW3270") != "" {
		t.Logf("%s", out)
	}
	if e.b.Conf().Name != "Renamed BBS" {
		t.Errorf("name in force: %q", e.b.Conf().Name)
	}
}

// TestSignInCodeMade: the profile's Sign-in code shows one, good for the
// caller's sign-in elsewhere.
func TestSignInCodeMade(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	s := login(e, "alice", "alicepass1")
	s.send("P")
	s.expect("Sign-in [C]ode (for the web)")
	s.send("C")
	s.expect("Your sign-in code, good once, until ")
	s.expect("    ")
	buf := make([]byte, 9)
	s.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(io.MultiReader(bytes.NewReader(s.got), s.c), buf); err != nil {
		t.Fatal(err)
	}
	code := string(buf)
	u, err := e.b.AuthenticateCode("test", "src", code)
	if err != nil || u.Name != "alice" {
		t.Fatalf("code %q: %+v %v", code, u, err)
	}
}
