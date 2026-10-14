package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/backup"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/server"
)

// backupCmd makes a backup, lists them, or puts one back.
func backupCmd(cfg *server.Config, path string, args []string) {
	words, flags := split(args)
	switch {
	case len(words) == 0:
		st := open(cfg)
		defer st.Close()
		b := bbs.New(cfg.Settings, st, nil)
		a, err := server.NewBackups(cfg, path, b).Run()
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s  %s\n", a.Path, size(a.Size))
	case words[0] == "list" && len(words) == 1:
		as, err := backup.List(cfg.Settings.Backup.Dir)
		if err != nil {
			fatal(err)
		}
		if len(as) == 0 {
			fmt.Println("no backups in", cfg.Settings.Backup.Dir)
		}
		for _, a := range as {
			fmt.Printf("%s  %s  %s\n", a.Time.Format("2006-01-02 15:04"), size(a.Size), a.Path)
		}
	case words[0] == "restore" && len(words) == 2:
		m, err := backup.Manifest(words[1])
		if err != nil {
			fatal(err)
		}
		to := backup.Targets{Config: path, Database: cfg.Database, Dirs: map[string]string{}}
		if a := cfg.RunAs; a != nil && os.Geteuid() == 0 {
			to.Owner, to.UID, to.GID = true, a.UID, a.GID
		}
		sources := append(server.Sources(cfg), server.Keys(cfg)...)
		for _, s := range sources {
			to.Dirs[s.Name] = s.Path
		}
		fmt.Printf("%s: made %s (database %s)\n", words[1], m["made"], m["database"])
		fmt.Printf("  configuration -> %s\n  database -> %s\n", or(to.Config, "(none: no -config)"), cfg.Database)
		for _, s := range sources {
			if m[s.Name] != "" {
				fmt.Printf("  %s -> %s\n", s.Name, s.Path)
			}
		}
		if !flags["yes"] {
			fmt.Println("with the BBS stopped, again with -yes to put it back (what is there now is renamed aside)")
			os.Exit(2)
		}
		err = backup.Restore(words[1], to, time.Now(), func(s string) { fmt.Println(s) })
		if err != nil {
			fatal(err)
		}
		fmt.Println("restored")
	default:
		usage()
	}
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
}
