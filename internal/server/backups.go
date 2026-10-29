package server

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/backup"
	"github.com/D-Modem/bbs/internal/bbs"
)

// Backups makes the BBS's backups ([backup]): the configuration file, the
// database and the files' directories, now or daily.
type Backups struct {
	B      *bbs.BBS
	Config string          // the configuration file ("": none)
	Dirs   []backup.Source // the files' directories, as the BBS began with them
	Keys   []backup.Source // the SSH host keys

	mu sync.Mutex // one at a time
}

// NewBackups are the backups of a BBS begun with cfg (from the file at
// path).
func NewBackups(cfg *Config, path string, b *bbs.BBS) *Backups {
	return &Backups{B: b, Config: path, Dirs: Sources(cfg), Keys: Keys(cfg)}
}

// Sources are the directories of files a configuration has.
func Sources(cfg *Config) []backup.Source {
	out := []backup.Source{{Name: "files", Path: cfg.FilesDir}, {Name: "chan", Path: cfg.Chan.Dir}}
	if cfg.Fido != nil {
		out = append(out, backup.Source{Name: "fido", Path: cfg.Fido.Dir})
	}
	if cfg.Screens != nil && cfg.Screens.Dir != "" {
		out = append(out, backup.Source{Name: "screens", Path: cfg.Screens.Dir})
	}
	if cfg.ScriptsDir != "" {
		out = append(out, backup.Source{Name: "scripts", Path: cfg.ScriptsDir})
	}
	return out
}

// Keys are the SSH listeners' host keys, key.NAME in an archive (one
// name twice: key.NAME.2).
func Keys(cfg *Config) []backup.Source {
	var out []backup.Source
	seen := map[string]bool{}
	names := map[string]int{}
	for _, l := range cfg.Listeners {
		for _, k := range l.HostKeys {
			if seen[k] {
				continue
			}
			seen[k] = true
			name := "key." + filepath.Base(k)
			if names[name]++; names[name] > 1 {
				name += "." + strconv.Itoa(names[name])
			}
			out = append(out, backup.Source{Name: name, Path: k})
		}
	}
	return out
}

// Run makes a backup now, as [backup] says.
func (k *Backups) Run() (*backup.Archive, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	s := k.B.Conf().Backup
	if s.Dir == "" {
		return nil, errors.New("no backup directory: [backup] dir")
	}
	p := &backup.Plan{Dir: s.Dir, Keep: s.Keep, Config: k.Config, Store: k.B.Store, Keys: k.Keys}
	if s.Files {
		p.Dirs = k.Dirs
	}
	start := time.Now()
	a, err := p.Run(start)
	if err != nil {
		k.B.Log.Printf("backup: %v", err)
		return nil, err
	}
	k.B.Log.Printf("backup: %s, %d bytes, in %v", a.Path, a.Size, time.Since(start).Round(time.Second))
	return a, nil
}

// List is the backups there are, the newest first.
func (k *Backups) List() ([]backup.Archive, error) {
	return backup.List(k.B.Conf().Backup.Dir)
}

// Schedule makes a backup each day at [backup]'s at (read again each
// minute: a reload changes it), until ctx is done.
func (k *Backups) Schedule(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	done := ""
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			at := k.B.Conf().Backup.At
			day := now.Format("2006-01-02")
			if at != "" && now.Format("15:04") == at && done != day {
				done = day
				k.Run()
			}
		}
	}
}
