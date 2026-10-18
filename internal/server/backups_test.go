package server

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/backup"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// TestBackupConfig: [backup]'s defaults (beside the database, 7 kept, the
// files taken), the keys set, and an at = that is not HH:MM refused.
func TestBackupConfig(t *testing.T) {
	dir := t.TempDir()
	load := func(body string) (*Config, error) {
		p := filepath.Join(dir, "bbs.conf")
		writeFile(p, "[general]\ndatabase = "+filepath.Join(dir, "bbs.db")+"\n[listen t]\ntype = telnet\naddress = :2300\n"+body)
		return Load(p)
	}
	c, err := load("")
	if err != nil {
		t.Fatal(err)
	}
	if b := c.Settings.Backup; b.Dir != filepath.Join(dir, "backups") || b.Keep != 7 || !b.Files || b.At != "" {
		t.Errorf("defaults %+v", b)
	}
	c, err = load("[backup]\ndir = /srv/b\nat = 03:30\nkeep = 0\nfiles = no\n")
	if b := c.Settings.Backup; err != nil || b.Dir != "/srv/b" || b.At != "03:30" || b.Keep != 0 || b.Files {
		t.Errorf("%+v %v", b, err)
	}
	if _, err := load("[backup]\nat = 3pm\n"); err == nil || !strings.Contains(err.Error(), "at") {
		t.Errorf("at = 3pm: %v", err)
	}
}

// TestBackups: a backup of a running BBS as [backup] says (the files
// left out when it says so), listed.
func TestBackups(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bbs.conf")
	files := filepath.Join(dir, "files")
	os.MkdirAll(files, 0o700)
	os.WriteFile(filepath.Join(files, "1"), []byte("x"), 0o600)
	writeFile(p, "[general]\ndatabase = "+filepath.Join(dir, "bbs.db")+"\nfiles_dir = "+files+
		"\n[listen t]\ntype = telnet\naddress = :2300\n[listen s]\ntype = ssh\naddress = :2222\n[backup]\nkeep = 1\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b := bbs.New(cfg.Settings, st, log.New(io.Discard, "", 0))
	for _, kp := range DefaultHostKeys(dir) {
		if _, _, err := LoadHostKey(kp); err != nil { // made
			t.Fatal(err)
		}
	}
	k := NewBackups(cfg, p, b)
	a, err := k.Run()
	if err != nil {
		t.Fatal(err)
	}
	m, _ := backup.Manifest(a.Path)
	if m["files"] != files || m["config"] != p || m["database"] != "sqlite" {
		t.Errorf("manifest %v", m)
	}
	// files = no, put in force by a reload: the next has none
	s := cfg.Settings
	s.Backup.Files = false
	b.Reconfigure(s)
	os.Rename(a.Path, filepath.Join(filepath.Dir(a.Path), "bbs-20000101-000000.tar.gz")) // older: kept 1 takes it away
	a2, err := k.Run()
	if err != nil {
		t.Fatal(err)
	}
	// the files left out; the host keys not
	if m, _ := backup.Manifest(a2.Path); m["files"] != "" || m["key.ssh_host_ed25519_key"] == "" || m["key.ssh_host_rsa_key"] == "" {
		t.Errorf("manifest %v", m)
	}
	if as, _ := k.List(); len(as) != 1 || as[0].Path != a2.Path {
		t.Errorf("list %+v", as)
	}
}
