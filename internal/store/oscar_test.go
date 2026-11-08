package store

import (
	"bytes"
	"path/filepath"
	"testing"
)

// TestOSCARKeys: digests kept only when asked, made at an account's
// making and a password's check, stale once the password changes, and
// made again when it is set; a screen name without its spaces finds its
// account.
func TestOSCARKeys(t *testing.T) {
	BcryptCost = 4
	s, err := Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("Carol Ann", "password1", "", false)
	if _, ok, _ := s.OSCARKeys(u.ID); ok {
		t.Fatal("kept without SetKeepOSCAR")
	}
	s.SetKeepOSCAR(true)
	if _, err := s.CheckPassword("Carol Ann", "password1"); err != nil {
		t.Fatal(err)
	}
	k, ok, err := s.OSCARKeys(u.ID)
	if err != nil || !ok || !bytes.Equal(k.Weak, OSCARWeak(k.AuthKey, "password1")) ||
		!bytes.Equal(k.Strong, OSCARStrong(k.AuthKey, "password1")) {
		t.Fatalf("keys %+v %v %v", k, ok, err)
	}
	// changed behind the store's back: stale
	s.db.Exec(`UPDATE users SET password = ? WHERE id = ?`, "x", u.ID)
	if _, ok, _ := s.OSCARKeys(u.ID); ok {
		t.Error("stale keys used")
	}
	s.SetPassword(u.ID, "password2")
	k2, ok, _ := s.OSCARKeys(u.ID)
	if !ok || k2.AuthKey != k.AuthKey || !bytes.Equal(k2.Weak, OSCARWeak(k.AuthKey, "password2")) {
		t.Errorf("after SetPassword %+v %v", k2, ok)
	}
	if v, _ := s.CreateUser("dave", "password1", "", false); v != nil {
		if _, ok, _ := s.OSCARKeys(v.ID); !ok {
			t.Error("none made with the account")
		}
	}
	for _, sn := range []string{"carolann", "CAROL ANN", "Carol Ann"} {
		if f, err := s.UserByScreenName(sn); err != nil || f.ID != u.ID {
			t.Errorf("%q: %v %v", sn, f, err)
		}
	}
	if _, err := s.UserByScreenName("carol"); err != ErrNotFound {
		t.Errorf("carol: %v", err)
	}
	// feedbag items: put, replaced, deleted, gone with the account
	s.PutFeedbag(u.ID, []FeedbagItem{{Name: "Buddies", GroupID: 1, ClassID: 1}, {Name: "bob", GroupID: 1, ItemID: 2}})
	s.PutFeedbag(u.ID, []FeedbagItem{{Name: "bobby", GroupID: 1, ItemID: 2, Attrs: []byte{1}}})
	items, _ := s.Feedbag(u.ID)
	if len(items) != 2 || items[1].Name != "bobby" || !bytes.Equal(items[1].Attrs, []byte{1}) {
		t.Errorf("items %+v", items)
	}
	s.DeleteFeedbag(u.ID, []FeedbagItem{{GroupID: 1, ItemID: 2}})
	if items, _ := s.Feedbag(u.ID); len(items) != 1 {
		t.Errorf("after delete %+v", items)
	}
	s.DeleteUser(u.ID)
	if items, _ := s.Feedbag(u.ID); len(items) != 0 {
		t.Errorf("after the account %+v", items)
	}
}
