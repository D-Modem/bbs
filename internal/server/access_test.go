package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
)

// TestValidation: a new user starts at new_user_level, may not chat or
// post and is told they wait; the sysop validates them from the BBS; then
// they may.
func TestValidation(t *testing.T) {
	e := start(t, func(s *bbs.Settings) {
		s.NewUserLevel, s.ValidatedLevel = 10, 20
		newbie := bbs.AllowAll(10, "New user")
		newbie.Post, newbie.Chat, newbie.Mail = false, false, false
		newbie.Time = 30 * time.Minute
		s.Levels = []bbs.Level{newbie, bbs.AllowAll(20, "Member")}
	}, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	members, _ := e.b.Store.CreateArea("Members", "For members", false)
	e.b.Store.SetAreaRequire(members.ID, "20", "")

	d := e.dial("modem.sock")
	d.expect("Name (or NEW): ")
	d.send("new\r")
	d.expect("give up): ")
	d.send("Zed\r")
	d.expect("Password (8 or more characters): ")
	d.send("zedpassword\rzedpassword\r")
	d.expect("calling from? ")
	d.send("Mars\r")
	d.expect("Welcome, Zed.")
	d.expect("You have 30 minutes today.")
	d.expect("waits for the sysop's validation")
	d.expect("Command: ")
	if strings.Contains(string(d.got), "[C] Chat") {
		t.Errorf("chat offered: %q", d.got)
	}
	d.send("M")
	d.expect("General")
	d.expect("Area number")
	if strings.Contains(string(d.got), "Members") {
		t.Errorf("Members shown: %q", d.got)
	}
	d.send("Q\r")
	d.expect("Command: ")
	d.send("P")
	d.expect("Access:    level 10 (New user)")
	d.expect("Today:     30 minutes left")
	d.send("Q")
	d.expect("Command: ")

	s := login(e, "sysop", "sysoppass1")
	s.send("V")
	s.expect("Zed")
	s.expect("[V]alidate (level 20)")
	s.send("V")
	s.expect("Validated.")
	s.expect("No one else is waiting.")
	if u, _ := e.b.Store.UserByName("zed"); u.Level != 20 {
		t.Errorf("level %d", u.Level)
	}

	// Zed's next menu has what members have
	d.send("\r")
	d.expect("[C] Chat")
	d.send("M")
	d.expect("Members")
}

// TestLevelConfig: [level N] sections, new_user_level and validated_level,
// and what doors, menus and menu items require; a bad level, one given
// twice, levels the wrong way round and a bad requirement are refused.
func TestLevelConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen t]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load(`[general]
new_user_level = 10
validated_level = 20
[level 20]
name = Member
time = 120
downloads = 20
[level 10]
name = New user
post = no
mail = no
[door lord]
command = /bin/true
require = 30 L
[menu main]
title = Main
m = areas Message areas
x = menu:extra Extras | 50 X
g = logoff Goodbye
[menu extra]
require = 50
w = who Who
`)
	if err != nil {
		t.Fatal(err)
	}
	s := c.Settings
	if s.NewUserLevel != 10 || s.ValidatedLevel != 20 || len(s.Levels) != 2 || s.Levels[0].Name != "New user" ||
		s.Levels[0].Post || !s.Levels[0].Chat || s.Levels[1].Time != 120*time.Minute || s.Levels[1].Downloads != 20 {
		t.Errorf("%+v", s)
	}
	if c.Doors[0].Require != "30 L" || c.Menus["extra"].Require != "50" || c.Menus["main"].Items[1].Require != "50 X" {
		t.Errorf("requires: %q %q %+v", c.Doors[0].Require, c.Menus["extra"].Require, c.Menus["main"].Items)
	}
	for body, want := range map[string]string{
		"[level 300]\n":            "[level 300]",
		"[level x]\n":              "[level x]",
		"[level 10]\n[level 10]\n": "given twice",
		"[level 10]\ntime = -1\n":  "time",
		"[general]\nnew_user_level = 30\nvalidated_level = 20\n": "below",
		"[door d]\ncommand = /bin/true\nrequire = 9x\n":          "require",
		"[menu main]\nm = areas Areas | !\n":                     "flags",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
}
