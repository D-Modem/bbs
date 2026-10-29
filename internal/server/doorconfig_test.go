package server

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/door"
)

// TestDoorConfig: program doors' commands (quoted words kept whole), dirs,
// drop files, charsets and options; bad ones and a door twice refused.
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

// TestRemoteDoorConfig: telnet, rlogin, BBSLink and DoorParty doors with
// their defaults, a warning for DoorParty with no host key, and what each
// needs refused when missing or bad.
func TestRemoteDoorConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load(`
[door far]
type = telnet
address = doors.example.org:2323
[door rl]
type = rlogin
address = doors.example.org:513
server_user = guest
[door lord]
type = bbslink
syscode = SYS
authcode = AUTH
schemecode = SCHEME
door = lord
[door party]
type = doorparty
ssh_user = me
ssh_password = secret
tag = XB
door = lord
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Doors) != 4 {
		t.Fatalf("doors %v", c.Doors)
	}
	far, rl, lord, party := c.Doors[0].Remote, c.Doors[1].Remote, c.Doors[2].Remote, c.Doors[3].Remote
	if far.Kind != door.KindTelnet || far.Address != "doors.example.org:2323" || c.Doors[0].Charset != door.CP437 {
		t.Errorf("telnet %+v", far)
	}
	if rl.ServerUser != "guest" || rl.ClientUser != "{user}" || rl.Terminal != "ansi" {
		t.Errorf("rlogin %+v", rl)
	}
	if lord.Address != door.BBSLinkAddress || lord.URL != door.BBSLinkURL || lord.DoorCode != "lord" {
		t.Errorf("bbslink %+v", lord)
	}
	if party.Address != door.DoorPartyAddress || party.Target != door.DoorPartyRLogin || party.Tag != "XB" {
		t.Errorf("doorparty %+v", party)
	}
	if len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "host key is not checked") {
		t.Errorf("warnings %v", c.Warnings)
	}
	for body, want := range map[string]string{
		"[door x]\ntype = gopher\n":                                                             "type",
		"[door x]\ntype = telnet\n":                                                             "address: needed",
		"[door x]\ntype = telnet\naddress = nowhere\n":                                          "address: host:port",
		"[door x]\ntype = bbslink\nsyscode = a\n":                                               "authcode: needed",
		"[door x]\ntype = doorparty\nssh_user = a\n":                                            "ssh_password: needed",
		"[door x]\ntype = doorparty\nssh_user = a\nssh_password = b\ntag = c\nhost_key = abc\n": "host_key",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
}

// a caller on the text BBS goes through a telnet door elsewhere and back
func TestRemoteDoorSession(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write([]byte{255, 253, 24}) // DO TERMINAL-TYPE: answered, not shown
		io.WriteString(c, "Far door: press a key\r\n")
		buf := make([]byte, 16)
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			n, err := c.Read(buf)
			if err != nil || bytes.ContainsRune(buf[:n], 'k') {
				break
			}
		}
		io.WriteString(c, "Far door: bye\r\n")
	}()
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	e.b.Doors = []*door.Door{{Name: "Far door", Charset: door.CP437, TimeLimit: time.Minute,
		Remote: &door.Remote{Kind: door.KindTelnet, Address: ln.Addr().String()}}}
	d := e.dial("modem.sock")
	d.expect("Name (or NEW): ")
	d.send("alice\ralicepass1\r")
	d.expect("Command: ")
	d.send("D")
	d.expect("Door number (Enter to go back): ")
	d.send("1\r")
	d.expect("Far door: press a key")
	if bytes.Contains(d.got, []byte{255}) {
		t.Error("telnet negotiation reached the caller")
	}
	d.send("k")
	d.expect("Far door: bye")
	d.expect("Back on ")
}
