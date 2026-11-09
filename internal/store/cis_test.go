package store

import (
	"encoding/hex"
	"path/filepath"
	"testing"
)

func TestCISKey(t *testing.T) {
	// the vector recovered from WinCIM 3.0.2's CCTN240C.DLL
	if k := hex.EncodeToString(CISKeyOf("Test123")); k != "70ab737140a38266c9d3bfc2aae504ae" {
		t.Errorf("K = %s", k)
	}
	BcryptCost = 4
	s, err := Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("alice", "password1", "", false)
	if _, ok, _ := s.CISKey(u.ID); ok {
		t.Fatal("kept without SetKeepCIS")
	}
	s.SetKeepCIS(true)
	if _, err := s.CheckPassword("alice", "password1"); err != nil {
		t.Fatal(err)
	}
	if k, ok, err := s.CISKey(u.ID); err != nil || !ok || string(k) != string(CISKeyOf("PASSWORD1")) {
		t.Fatalf("key %x %v %v", k, ok, err)
	}
	if err := s.SetPassword(u.ID, "password2"); err != nil {
		t.Fatal(err)
	}
	if k, ok, _ := s.CISKey(u.ID); !ok || string(k) != string(CISKeyOf("password2")) {
		t.Error("not kept on a change")
	}
	// changed behind the store's back: stale
	s.db.Exec(`UPDATE users SET password = 'x' WHERE id = ?`, u.ID)
	if _, ok, _ := s.CISKey(u.ID); ok {
		t.Error("a stale key is current")
	}
}
