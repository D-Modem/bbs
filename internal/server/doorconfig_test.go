package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/door"
)

func TestDoorConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load(`
[door lord]
description = Legend of the Red Dragon
command = docker run --rm -i -v /srv/doors:/doors -v {dir}:/node bbs-dosemu2 'D:\LORD' 'LORD.EXE /PE:\'
dir = /var/lib/bbs/doors/node{node}
drop_files = door.sys
time_limit = 30
single = yes
cleanup = docker rm -f door-{node}
[door native]
command = /usr/games/adventure
charset = utf-8
pty = yes
door32_comm = local
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Doors) != 2 {
		t.Fatal(c.Doors)
	}
	l := c.Doors[0]
	if l.Command[len(l.Command)-2] != `D:\LORD` || l.Command[len(l.Command)-1] != `LORD.EXE /PE:\` ||
		l.TimeLimit != 30*time.Minute || !l.Single || l.Charset != "cp437" || len(l.DropFiles) != 1 ||
		l.CommType != 1 || strings.Join(l.Cleanup, " ") != "docker rm -f door-{node}" {
		t.Fatalf("%+v", l)
	}
	n := c.Doors[1]
	if !n.PTY || n.Charset != "utf-8" || n.CommType != 0 || len(n.DropFiles) != 4 || n.Dir != "doors/node{node}" {
		t.Fatalf("%+v", n)
	}
	for body, want := range map[string]string{
		"[door x]\n":                                     "command",
		"[door x]\ncommand = a 'b\n":                     "quote",
		"[door x]\ncommand = a\ndir = /tmp/x\n":          "{node}",
		"[door x]\ncommand = a\ndrop_files = pcb.sys\n":  "drop_files",
		"[door x]\ncommand = a\ncharset = koi8\n":        "charset",
		"[door x]\ncommand = a\n[door X]\ncommand = b\n": "twice",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}

// TestDoorSession: a caller opens a door from the menu, plays, and comes
// back to the BBS.
func TestDoorSession(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	sh := filepath.Join(e.dir, "door.sh")
	os.WriteFile(sh, []byte("#!/bin/sh\nprintf 'In the door, %s\\r\\n' \"$(sed -n 10p DOOR.SYS | tr -d '\\r')\"\n"+
		"read -r k\nprintf 'Bye from the door\\r\\n'\n"), 0755)
	e.b.Doors = []*door.Door{{Name: "Test door", Description: "A script", Command: []string{sh},
		Dir: filepath.Join(e.dir, "node{node}"), DropFiles: door.DropFiles, Charset: door.CP437,
		TimeLimit: time.Minute, CommType: 1},
		{Name: "Sysop door", Command: []string{"true"}, Dir: e.dir + "/{node}", SysopOnly: true}}
	d := e.dial("modem.sock")
	d.expect("Name (or NEW): ")
	d.send("alice\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("[D] Doors")
	d.expect("Command: ")
	d.send("D")
	d.expect("1 Test door")
	d.expect("Door number (Enter to go back): ")
	d.send("2\r")
	d.expect("No such door.") // the sysop's is not shown to alice
	d.expect("Door number (Enter to go back): ")
	d.send("1\r")
	d.expect("In the door, alice")
	d.send("x\n") // a CR LF would be a CR, as DOS doors want: sh wants LF
	d.expect("Bye from the door")
	d.expect("Back on ")
	d.expect("Door number (Enter to go back): ")
}
