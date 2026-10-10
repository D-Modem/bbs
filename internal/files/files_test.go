package files

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func init() { store.BcryptCost = 4 }

func setup(t *testing.T, max, quota int64) (*Store, *store.User, *store.User, *store.FileArea) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	alice, _ := db.CreateUser("alice", "alicepass1", "", false)
	sysop, _ := db.CreateUser("sysop", "sysoppass1", "", true)
	a, err := db.CreateFileArea("Uploads", "", false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(db, filepath.Join(dir, "files"), max, quota)
	if err != nil {
		t.Fatal(err)
	}
	return s, alice, sysop, a
}

func put(s *Store, u *store.User, a *store.FileArea, name string, data []byte) (*store.File, error) {
	up, err := s.Upload(u, a, name, "a file")
	if err != nil {
		return nil, err
	}
	if _, err := up.Write(data); err != nil {
		up.Abort()
		return nil, err
	}
	return up.Commit()
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"a", "README.TXT", "game v1.2 (dos).zip", "x_y-z+[1]"} {
		if msg := ValidName(ok); msg != "" {
			t.Errorf("%q: %s", ok, msg)
		}
	}
	for _, bad := range []string{"", ".hidden", "-rf", "a/b", `a\b`, "..", "a..b", "trail.", "trail ",
		"nul\x00", "tab\t", "é.txt", "a:b", "a*b", strings.Repeat("x", 65)} {
		if ValidName(bad) == "" {
			t.Errorf("took %q", bad)
		}
	}
}

func TestUploadAndDelete(t *testing.T) {
	s, alice, sysop, a := setup(t, 1<<20, 0)
	f, err := put(s, alice, a, "hello.txt", []byte("hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Size != 6 || f.SHA256 != "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03" {
		t.Errorf("file %+v", f)
	}
	// stored by ID, nothing in tmp
	if data, _ := os.ReadFile(filepath.Join(s.Dir, "1")); !bytes.Equal(data, []byte("hello\n")) {
		t.Errorf("stored %q", data)
	}
	if left, _ := filepath.Glob(filepath.Join(s.Dir, "tmp", "*")); len(left) != 0 {
		t.Errorf("tmp left %v", left)
	}
	if _, err := put(s, alice, a, "HELLO.TXT", []byte("x")); !errors.Is(err, ErrExists) {
		t.Errorf("same name, other case: %v", err)
	}
	// only the uploader or a sysop deletes
	bob, _ := s.DB.CreateUser("bob", "bobpassword", "", false)
	if err := s.Delete(bob, f); !errors.Is(err, ErrDenied) {
		t.Errorf("bob deleted: %v", err)
	}
	if err := s.Delete(sysop, f); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "1")); !errors.Is(err, os.ErrNotExist) {
		t.Error("contents left")
	}
}

func TestLimits(t *testing.T) {
	s, alice, sysop, a := setup(t, 10, 15)
	if _, err := put(s, alice, a, "big", make([]byte, 11)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: %v", err)
	}
	if _, err := put(s, alice, a, "one", make([]byte, 10)); err != nil {
		t.Fatal(err)
	}
	// 5 bytes of today's 15 left
	if _, err := put(s, alice, a, "two", make([]byte, 6)); !errors.Is(err, ErrQuota) {
		t.Errorf("over quota: %v", err)
	}
	if _, err := put(s, alice, a, "three", make([]byte, 5)); err != nil {
		t.Errorf("within quota: %v", err)
	}
	if _, err := s.Upload(alice, a, "four", ""); !errors.Is(err, ErrQuota) {
		t.Errorf("quota used up: %v", err)
	}
	// sysops have no allowance
	if _, err := put(s, sysop, a, "five", make([]byte, 10)); err != nil {
		t.Errorf("sysop: %v", err)
	}
	// nothing of the failed ones is left
	if left, _ := filepath.Glob(filepath.Join(s.Dir, "tmp", "*")); len(left) != 0 {
		t.Errorf("tmp left %v", left)
	}
}

func TestSysopUploadArea(t *testing.T) {
	s, alice, sysop, _ := setup(t, 100, 0)
	a, _ := s.DB.CreateFileArea("Official", "", true)
	if _, err := s.Upload(alice, a, "x", ""); !errors.Is(err, ErrDenied) {
		t.Errorf("alice uploaded: %v", err)
	}
	if _, err := put(s, sysop, a, "x", []byte("x")); err != nil {
		t.Error(err)
	}
	locked := *alice
	locked.Locked = true
	if CanUpload(&locked, a) {
		t.Error("a locked user may upload")
	}
}

func TestStaleUploadsCleared(t *testing.T) {
	s, _, _, _ := setup(t, 100, 0)
	stale := filepath.Join(s.Dir, "tmp", "up-123")
	os.WriteFile(stale, []byte("half"), 0600)
	if _, err := Open(s.DB, s.Dir, 100, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Error("stale upload kept")
	}
}
