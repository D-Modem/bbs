package server

import (
	"os"
	"sync"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/confedit"
	"github.com/D-Modem/bbs/internal/imageboard"
)

// Reloader puts a running BBS's configuration file in force again: what
// may change while callers are on changes (bbs.conf's comments, and
// confedit.Live, say which); the rest is said to wait for a restart.
type Reloader struct {
	Path string
	B    *bbs.BBS
	Chan *imageboard.Server // nil: none
	// Forum puts the web forum's options in force (nil: no forum)
	Forum func(guests bool, theme string, flags bool)

	mu   sync.Mutex
	last []byte // the file as last put in force
}

// NewReloader is a Reloader for the file the BBS has started with.
func NewReloader(path string, b *bbs.BBS) *Reloader {
	data, _ := os.ReadFile(path)
	return &Reloader{Path: path, B: b, last: data}
}

// Reload reads the file again and puts it in force: the changes that took
// effect, and those that wait for a restart.  A file the BBS would not
// start with changes nothing.
func (r *Reloader) Reload() (applied, restart []string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := os.ReadFile(r.Path)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, nil, err
	}
	old, err := confedit.Parse(r.last)
	if err != nil {
		old, _ = confedit.Parse(nil)
	}
	now, err := confedit.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range confedit.Diff(old, now) {
		if c.Live() {
			applied = append(applied, c.String())
		} else {
			restart = append(restart, c.String())
		}
	}
	r.B.Reconfigure(cfg.Settings)
	r.B.ReconfigureCustom(cfg.Menus, cfg.Text)
	if r.Chan != nil {
		r.Chan.Reconfigure(cfg.Chan)
	}
	if r.Forum != nil && cfg.HTTP != nil {
		r.Forum(cfg.HTTP.Guests, cfg.HTTP.Theme, cfg.HTTP.ForumFlags)
	}
	r.last = data
	for _, w := range cfg.Warnings {
		r.B.Log.Printf("reload: warning: %s", w)
	}
	r.B.Log.Printf("reload: %s: %d changes in force, %d waiting for a restart", r.Path, len(applied), len(restart))
	for _, c := range restart {
		r.B.Log.Printf("reload: at a restart: %s", c)
	}
	return applied, restart, nil
}

// Check reads a file's contents as the BBS would start with them: the
// warnings, or why it would not start (bbs.ConfigTools' Check).
func Check(data []byte) ([]string, error) {
	cfg, err := Parse(data)
	if err != nil {
		return nil, err
	}
	return cfg.Warnings, nil
}
