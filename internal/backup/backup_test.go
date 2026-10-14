package backup

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// system is a BBS's files: a configuration, a database, file areas.
func system(t *testing.T, dir string) (conf, db, files string, st *store.Store) {
	conf = filepath.Join(dir, "bbs.conf")
	db = filepath.Join(dir, "bbs.db")
	files = filepath.Join(dir, "files")
	os.WriteFile(conf, []byte("[general]\nname = Test\n"), 0o640)
	os.MkdirAll(filepath.Join(files, "1"), 0o700)
	os.WriteFile(filepath.Join(files, "1", "7"), []byte("a file"), 0o600)
	var err error
	if st, err = store.Open("sqlite:" + db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.CreateUser("alice", "alicepass1", "", false)
	return
}

func TestBackupAndRestore(t *testing.T) {
	dir := t.TempDir()
	conf, db, files, st := system(t, dir)
	backups := filepath.Join(dir, "backups")
	key := filepath.Join(dir, "ssh_host_ed25519_key")
	os.WriteFile(key, []byte("the host key"), 0o600)
	p := &Plan{Dir: backups, Keep: 2, Config: conf, Store: st, Dirs: []Source{{"files", files}, {"chan", filepath.Join(dir, "none")}},
		Keys: []Source{{"key.ssh_host_ed25519_key", key}, {"key.ssh_host_rsa_key", filepath.Join(dir, "not-made-yet")}}}
	t0 := time.Date(2026, 10, 14, 3, 30, 0, 0, time.Local)
	a, err := p.Run(t0)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(a.Path) != "bbs-20261014-033000.tar.gz" || a.Size == 0 {
		t.Errorf("archive %+v", a)
	}
	m, err := Manifest(a.Path)
	if err != nil || m["database"] != "sqlite" || m["files"] != files || m["config"] != conf ||
		m["key.ssh_host_ed25519_key"] != key || m["chan"] != "" || m["key.ssh_host_rsa_key"] != "" {
		t.Errorf("manifest %v %v", m, err)
	}
	// two more: the oldest goes
	p.Run(t0.Add(time.Hour))
	p.Run(t0.Add(2 * time.Hour))
	as, _ := List(backups)
	if len(as) != 2 || !as[0].Time.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("kept %+v", as)
	}
	if _, err := p.Run(t0.Add(2 * time.Hour)); err == nil {
		t.Error("made over one there")
	}

	// the system changed since; then restored
	st.CreateUser("bob", "bobpass123", "", false)
	st.Close()
	os.WriteFile(conf, []byte("[general]\nname = Changed\n"), 0o640)
	os.WriteFile(filepath.Join(files, "1", "8"), []byte("new"), 0o600)
	os.WriteFile(key, []byte("a new key"), 0o600)
	var said []string
	now := t0.Add(5 * time.Hour)
	err = Restore(as[1].Path, Targets{Config: conf, Database: db,
		Dirs: map[string]string{"files": files, "key.ssh_host_ed25519_key": key}}, now,
		func(s string) { said = append(said, s) })
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(conf); string(data) != "[general]\nname = Test\n" {
		t.Errorf("config %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(files, "1", "7")); string(data) != "a file" {
		t.Errorf("file %q", data)
	}
	if _, err := os.Stat(filepath.Join(files, "1", "8")); err == nil {
		t.Error("a file made since is there")
	}
	stamp := ".before-restore-20261014-083000"
	if data, _ := os.ReadFile(key); string(data) != "the host key" {
		t.Errorf("key %q", data)
	}
	if fi, _ := os.Stat(key); fi.Mode().Perm() != 0o600 {
		t.Errorf("key's mode %v", fi.Mode())
	}
	if data, _ := os.ReadFile(key + stamp); string(data) != "a new key" {
		t.Errorf("the key set aside: %q", data)
	}
	if data, _ := os.ReadFile(conf + stamp); string(data) != "[general]\nname = Changed\n" {
		t.Errorf("the config set aside: %q", data)
	}
	if _, err := os.Stat(filepath.Join(files+stamp, "1", "8")); err != nil {
		t.Errorf("the files set aside: %v", err)
	}
	st2, err := store.Open("sqlite:" + db)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if _, err := st2.UserByName("alice"); err != nil {
		t.Error("alice is not there")
	}
	if _, err := st2.UserByName("bob"); err == nil {
		t.Error("bob, made since, is there")
	}
	if !strings.Contains(strings.Join(said, "\n"), "database restored to "+db) {
		t.Errorf("said %q", said)
	}
}

// TestRestoreRefuses: an archive with a name that climbs out is refused
// before anything is changed.
func TestRestoreRefuses(t *testing.T) {
	dir := t.TempDir()
	evil := filepath.Join(dir, "bbs-20260101-000000.tar.gz")
	f, _ := os.Create(evil)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range []struct{ name, body string }{{"MANIFEST", "made = now\n"}, {"files/../../escaped", "x"}} {
		tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o600, Size: int64(len(e.body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(e.body))
	}
	tw.Close()
	gz.Close()
	f.Close()
	files := filepath.Join(dir, "files")
	os.MkdirAll(files, 0o700)
	err := Restore(evil, Targets{Dirs: map[string]string{"files": files}}, time.Now(), func(string) {})
	if err == nil || !strings.Contains(err.Error(), "not a name a backup has") {
		t.Fatalf("restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escaped")); err == nil {
		t.Error("written outside")
	}
	if _, err := os.Stat(files); err != nil {
		t.Error("the files were moved")
	}
	if _, err := Manifest(filepath.Join(dir, "nothing.tar.gz")); err == nil {
		t.Error("a manifest of nothing")
	}
}

func TestClean(t *testing.T) {
	for name, ok := range map[string]bool{"files/1/7": true, "files/": true, "bbs.conf": true,
		"../x": false, "/etc/passwd": false, "files/../../x": false, "files/./x": false, "": false, "a\\b": false} {
		if clean(name) != ok {
			t.Errorf("clean(%q) = %v", name, !ok)
		}
	}
}
