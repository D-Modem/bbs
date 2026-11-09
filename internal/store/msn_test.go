package store

import (
	"bytes"
	"encoding/hex"
	"path/filepath"
	"testing"
)

// TestMSNKeys: digests kept only when asked, made at a password's check,
// stale once the password changes, made again (same salt) when it is set.
func TestMSNKeys(t *testing.T) {
	BcryptCost = 4
	s, err := Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("alice", "password1", "", false)
	if _, _, ok, _ := s.MSNKey(u.ID); ok {
		t.Fatal("kept without SetKeepMSN")
	}
	s.SetKeepMSN(true)
	if _, err := s.CheckPassword("alice", "password1"); err != nil {
		t.Fatal(err)
	}
	salt, d, ok, err := s.MSNKey(u.ID)
	if err != nil || !ok || salt == "" || d != MSNDigest(salt, "password1") {
		t.Fatalf("key %q %q %v %v", salt, d, ok, err)
	}
	if again, _ := s.MSNSalt(u.ID); again != salt {
		t.Errorf("salt changed: %q %q", salt, again)
	}
	s.db.Exec(`UPDATE users SET password = ? WHERE id = ?`, "x", u.ID)
	if _, _, ok, _ := s.MSNKey(u.ID); ok {
		t.Error("stale digest used")
	}
	s.SetPassword(u.ID, "password2")
	if salt2, d2, ok, _ := s.MSNKey(u.ID); !ok || salt2 != salt || d2 != MSNDigest(salt, "password2") {
		t.Errorf("after set: %q %q %v", salt2, d2, ok)
	}
	if MSNDigest("prefix", "password") != "61ed83ed9d177e0c99b83a2138623a4b" {
		t.Error("digest form")
	}
}

// TestBlocks: blocking, unblocking, and who has a user as a friend.
func TestBlocks(t *testing.T) {
	BcryptCost = 4
	s, err := Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.CreateUser("alice", "pw", "", false)
	b, _ := s.CreateUser("bob", "pw", "", false)
	c, _ := s.CreateUser("carol", "pw", "", false)
	if err := s.Block(a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Block(a.ID, b.ID); err != nil {
		t.Fatal("twice:", err)
	}
	if ok, _ := s.IsBlocked(a.ID, b.ID); !ok {
		t.Error("not blocked")
	}
	if ok, _ := s.IsBlocked(b.ID, a.ID); ok {
		t.Error("blocked the other way")
	}
	bl, _ := s.Blocked(a.ID)
	if len(bl) != 1 || bl[0].Name != "bob" {
		t.Errorf("blocked %+v", bl)
	}
	s.Unblock(a.ID, b.ID)
	if ok, _ := s.IsBlocked(a.ID, b.ID); ok {
		t.Error("still blocked")
	}
	s.PutFriend(b.ID, Friend{ID: a.ID, Group: "Pals"})
	s.PutFriend(c.ID, Friend{ID: a.ID})
	of, _ := s.FriendOf(a.ID)
	if len(of) != 2 || of[0].Name != "bob" || of[0].Group != "Pals" || of[1].Name != "carol" {
		t.Errorf("friend of %+v", of)
	}
}

func TestAddressBook(t *testing.T) {
	BcryptCost = 4
	s, err := Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.CreateUser("alice", "password1", "", false)
	if err != nil {
		t.Fatal(err)
	}
	s.PutAddress(u.ID, "Jane", "jane@example.com")
	s.PutAddress(u.ID, "bob", "Bob")
	s.PutAddress(u.ID, "JANE", "jane@example.org") // the same name, another address
	s.PutAddress(u.ID, "", "nobody")
	book, err := s.Addresses(u.ID)
	if err != nil || len(book) != 2 || book[0].Name != "bob" || book[1].Name != "Jane" || book[1].Addr != "jane@example.org" {
		t.Fatalf("book: %+v %v", book, err)
	}
	s.RemoveAddress(u.ID, "jane")
	if book, _ := s.Addresses(u.ID); len(book) != 1 {
		t.Fatalf("after removing: %+v", book)
	}
}

func TestNTLMKeys(t *testing.T) {
	// the hashes of "password", as every NTLM write-up gives them
	if got := hex.EncodeToString(LMHash("password")); got != "e52cac67419a9a224a3b108f3fa6cb6d" {
		t.Errorf("LM: %s", got)
	}
	if got := hex.EncodeToString(NTHash("password")); got != "8846f7eaee8fb117ad06bdd830b7586c" {
		t.Errorf("NT: %s", got)
	}
	BcryptCost = 4
	s, err := Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("alice", "password", "", false)
	if _, _, ok, _ := s.NTLMKey(u.ID); ok {
		t.Fatal("kept without SetKeepNTLM")
	}
	s.SetKeepNTLM(true)
	s.CheckPassword("alice", "password")
	lm, nt, ok, err := s.NTLMKey(u.ID)
	if err != nil || !ok || !bytes.Equal(lm, LMHash("password")) || !bytes.Equal(nt, NTHash("password")) {
		t.Fatalf("kept: %x %x %v %v", lm, nt, ok, err)
	}
	// a password changed where it isn't seen leaves them stale
	s.db.Exec(`UPDATE users SET password = 'x' WHERE id = ?`, u.ID)
	if _, _, ok, _ := s.NTLMKey(u.ID); ok {
		t.Fatal("stale hashes used")
	}
	s.SetPassword(u.ID, "another1")
	if lm, _, ok, _ := s.NTLMKey(u.ID); !ok || !bytes.Equal(lm, LMHash("another1")) {
		t.Fatal("not kept on a new password")
	}
}
