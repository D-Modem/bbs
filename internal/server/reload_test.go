package server

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestConfigScreens: the sysop changes the system's name on the
// Configuration screens; it is written, kept in bbs.conf.bak, and in force
// for the next caller.
func TestConfigScreens(t *testing.T) {
	var p string
	e := startFull(t, ui3270.Graphics{}, nil, func(b *bbs.BBS, dir string) {
		p = filepath.Join(dir, "bbs.conf")
		if err := writeFile(p, reloadConf); err != nil {
			t.Fatal(err)
		}
		r := NewReloader(p, b)
		b.Config = &bbs.ConfigTools{Path: p, Check: Check, Reload: r.Reload}
	}, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	s := login(e, "sysop", "sysoppass1")
	s.send("*")
	s.expect("[F] Configuration")
	s.send("F")
	s.expect("[W] Write and put in force")
	s.send("S")
	s.expect("[listen modem]")
	s.send("1\r")
	s.expect("-- More --")
	s.send("\r")
	s.expect("[D]elete section")
	s.send("1\r")
	s.expect("Takes effect: at once, when written")
	s.expect("the system's name, its sysop's") // the sample's help
	s.send("E")
	s.expect("name: Test BBS")
	s.send("\x15Renamed BBS\r")
	s.expect("Value:     Renamed BBS")
	s.send("Q")
	s.expect("-- More --")
	s.send("Q")
	s.expect("[D]elete section")
	s.send("Q\r")
	s.expect("[N]ew section")
	s.send("Q\r")
	s.expect("View changes (1)")
	s.send("V")
	s.expect("~[general] name = Renamed BBS (was Test BBS)")
	s.send("  ")
	s.expect("Configuration: ")
	s.send("W")
	s.expect("In force now:")
	s.expect("~[general] name = Renamed BBS")

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
