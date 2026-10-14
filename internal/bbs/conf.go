package bbs

import "github.com/Neo-Desktop/bbs-sec/internal/backup"

// Conf is the settings in force: those the BBS began with, or the last
// Reconfigure's.  It is not to be changed: Reconfigure makes another.
func (b *BBS) Conf() *Settings {
	if s := b.live.Load(); s != nil {
		return s
	}
	return &b.Settings
}

// Reconfigure puts settings in force while callers are on: each reads
// them whole, before or after (a session's timeouts and limits taken at
// its start keep).
func (b *BBS) Reconfigure(s Settings) {
	b.live.Store(&s)
}

// custom is the sysop's menus and strings, put in force while the BBS
// runs.
type custom struct {
	menus map[string]*Menu
	text  map[string]string
}

// Menu is the sysop's menu of a name (nil: none).
func (b *BBS) Menu(name string) *Menu {
	if c := b.custom.Load(); c != nil {
		return c.menus[name]
	}
	return b.Menus[name]
}

func (b *BBS) text() map[string]string {
	if c := b.custom.Load(); c != nil {
		return c.text
	}
	return b.Text
}

// ReconfigureCustom puts the sysop's menus and strings in force while
// callers are on (a caller in a menu has it until they leave it).
func (b *BBS) ReconfigureCustom(menus map[string]*Menu, text map[string]string) {
	b.custom.Store(&custom{menus, text})
}

// ConfigTools let the sysop's Configuration screens edit the file the BBS
// runs by, and put it in force.
type ConfigTools struct {
	Path string
	// Check reads a file as the BBS would start with it: its warnings,
	// or why it would not start
	Check func(data []byte) (warnings []string, err error)
	// Reload puts the file in force again (nil: the BBS is not running):
	// the changes that took effect, and those that wait for a restart
	Reload func() (applied, restart []string, err error)
	// Backup makes a backup now; Backups lists those there are, the
	// newest first (nil: none here)
	Backup  func() (*backup.Archive, error)
	Backups func() ([]backup.Archive, error)
}
