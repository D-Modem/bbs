package ui3270

import (
	"fmt"
	"strings"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/app"
	"github.com/Neo-Desktop/bbs-sec/internal/door"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// What only a 3270 does, for internal/app.

func (u *ui) Kind() string { return app.TN3270 }

// ShowScreen: a 3270's display files always wait for Enter.
func (u *ui) ShowScreen(name string, wait bool) (bool, error) { return u.screen(name) }

func (u *ui) Chat(room, note string) error { return u.chat(room, note) }

// Doors are ANSI programs: none here.
func (u *ui) Doors() []*door.Door          { return nil }
func (u *ui) OpenDoor(*door.Door) error    { return nil }
func (u *ui) Transfers() bool              { return false }
func (u *ui) Download([]app.FileRef) error { return nil }
func (u *ui) Upload(*store.FileArea) error { return nil }

func (u *ui) SendData(string, []byte) (bool, error) { return false, nil }
func (u *ui) ReceiveData(int64) ([]byte, error)     { return nil, nil }

// DataCommand runs an IND$FILE transfer of a file made for the caller.
func (u *ui) DataCommand(cmd string, get func(string) ([]byte, func(), error), put func(string, []byte) string) (string, error) {
	u.user = u.n.User()
	ind, ok := go3270.ParseIndFile(cmd)
	if !ok {
		return "Commands here are IND$FILE GET and PUT, from your emulator's file transfer.", nil
	}
	return u.indData(ind, get, put)
}

// FileCommand runs an IND$FILE transfer typed on a file area's command line.
func (u *ui) FileCommand(a *store.FileArea, cmd string) (string, error) {
	u.user = u.n.User()
	ind, ok := go3270.ParseIndFile(cmd)
	if !ok {
		return "Commands here are IND$FILE GET and PUT, from your emulator's file transfer.", nil
	}
	return u.indFile(a, ind)
}

// Terminal is the terminal's line on the profile.
func (u *ui) Terminal() string {
	rows, cols := u.dev.AltDimensions()
	cp := "default"
	if c := u.dev.Codepage(); c != nil {
		cp = c.ID()
	}
	pictures := map[string]string{picNone: "off", picVector: "vector graphics", picSymbols: "programmed symbols",
		picBlocks: "blocks"}[u.pictureMode()]
	return fmt.Sprintf("%s, %dx%d, code page %s, pictures %s", strings.TrimSpace(u.dev.TerminalType()), cols, rows,
		cp, pictures)
}

// TerminalActions: pictures on and off, where the terminal can show them.
func (u *ui) TerminalActions() []view.Action {
	off := u.noPictures
	u.noPictures = false
	can := u.pictureMode() != picNone
	u.noPictures = off
	if !can {
		return nil
	}
	return []view.Action{{ID: "pictures", Label: "Pictures on/off", Key: 'M', PF: 11, Short: "Pictures"}}
}

// TerminalAction switches pictures, and keeps it for next time.
func (u *ui) TerminalAction(id string) error {
	if id != "pictures" {
		return nil
	}
	u.noPictures = !u.noPictures
	return u.n.SetPref("pictures", map[bool]string{true: "off", false: "on"}[u.noPictures])
}

// applyPrefs: pictures as the caller left them last time.
func (u *ui) applyPrefs() {
	if on, ok := u.n.Prefs().On("pictures"); ok {
		u.noPictures = !on
	}
}
func (u *ui) TerminalInfo() error { return u.terminal() }

// Watch: a 3270 shows a node's screen no more than it shows a door's.
func (u *ui) Watch(int) error {
	u.Say(view.Bad, "Watching a node is for text terminals.")
	return nil
}

// Logoff: the goodbye screen.
func (u *ui) Logoff() (bool, error) {
	u.bye()
	return true, nil
}
