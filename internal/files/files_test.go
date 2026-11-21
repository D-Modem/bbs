package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/store"
)

// init makes bcrypt cheap, for the tests' speed.
func init() { store.BcryptCost = 4 }

// setup is file storage in a temporary directory with the limits given, a
// user, a sysop and an area anyone may upload to.
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

// put uploads data as name, described "a file", aborting the upload if the
// write fails.
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

// TestValidName: plain names pass; empty, hidden, option-like, path, trailing
// dot or space, control, non-ASCII, shell or Windows characters and over-long
// names do not.
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

// TestUploadAndDelete: a file is kept by its ID, hashed, with nothing left
// in tmp; a name taken in another case is ErrExists; only its uploader or a
// sysop deletes it, and its contents go with it.
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

// TestLimits: a file past the most size is ErrTooLarge, past the day's quota
// ErrQuota (and a quota used up refuses an upload at once); sysops have
// none; a failed upload leaves nothing behind.
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

// TestSysopUploadArea: only a sysop uploads to a sysops' area, and a locked
// user uploads nowhere.
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

// TestStaleUploadsCleared: Open removes what an upload cut short left in tmp.
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

// TestUploadResume: an upload of a known size cut short is kept; the next
// upload of the same file takes it by Resume and goes on after it, hashed
// whole; one that does not call Resume starts again; nothing is left.
func TestUploadResume(t *testing.T) {
	s, alice, _, a := setup(t, 1<<20, 0)
	want := []byte("the whole of the file, in two parts\n")
	up, err := s.UploadResumable(alice, a, "two.txt", "", int64(len(want)))
	if err != nil {
		t.Fatal(err)
	}
	up.Write(want[:10])
	up.Abort()
	// a different size is a different file: nothing to go on from
	other, _ := s.UploadResumable(alice, a, "two.txt", "", 99)
	if n := other.Resume(); n != 0 {
		t.Errorf("other size resumed at %d", n)
	}
	other.Abort()
	up, _ = s.UploadResumable(alice, a, "two.txt", "", int64(len(want)))
	if n := up.Resume(); n != 10 {
		t.Fatalf("resumed at %d", n)
	}
	up.Write(want[10:])
	f, err := up.Commit()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(want)
	if f.Size != int64(len(want)) || f.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("file %+v", f)
	}
	if data, _ := os.ReadFile(filepath.Join(s.Dir, "1")); !bytes.Equal(data, want) {
		t.Errorf("stored %q", data)
	}
	if left, _ := filepath.Glob(filepath.Join(s.Dir, "tmp", "*")); len(left) != 0 {
		t.Errorf("tmp left %v", left)
	}
}
