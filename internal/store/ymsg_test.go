package store

import (
	"path/filepath"
	"testing"
)

// TestYMSGKeys: the digests' forms, kept only when asked, stale once the
// password changes.
func TestYMSGKeys(t *testing.T) {
	// openssl passwd -1 -salt _2S43d5f password
	if got := MD5Crypt("password", "_2S43d5f"); got != "$1$_2S43d5f$Cb/MEl/9lyqcT5fwKLPnX0" {
		t.Errorf("md5crypt: %s", got)
	}
	if got := Y64([]byte{0xfb, 0xff, 0x01}); got != "._8B" {
		t.Errorf("y64: %s", got)
	}
	if got := Y64([]byte{0xff}); got != "_w--" {
		t.Errorf("y64 padding: %s", got)
	}
	BcryptCost = 4
	s, err := Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("alice", "password1", "", false)
	if _, ok, _ := s.YMSGKeys(u.ID); ok {
		t.Fatal("kept without SetKeepYMSG")
	}
	s.SetKeepYMSG(true)
	if _, err := s.CheckPassword("alice", "password1"); err != nil {
		t.Fatal(err)
	}
	k, ok, err := s.YMSGKeys(u.ID)
	if err != nil || !ok || k != YMSGDigests("password1") {
		t.Fatalf("keys %+v %v %v", k, ok, err)
	}
	s.db.Exec(`UPDATE users SET password = ? WHERE id = ?`, "x", u.ID)
	if _, ok, _ := s.YMSGKeys(u.ID); ok {
		t.Error("stale digests used")
	}
	s.SetPassword(u.ID, "password2")
	if k, ok, _ := s.YMSGKeys(u.ID); !ok || k != YMSGDigests("password2") {
		t.Errorf("after set: %+v %v", k, ok)
	}
}
