package bbs

import (
	"bytes"
	"errors"
	"log"
	"path/filepath"
	"testing"

	"github.com/D-Modem/bbs/internal/store"
)

// fakeMenu is modemmenu's auth socket: alice's password is "menupass".
type fakeMenu struct {
	asked []string
	down  bool
}

// Check notes the user asked for, and is yes for alice and carol with
// "menupass" (an error if down).
func (f *fakeMenu) Check(user, pw string) (bool, string, error) {
	f.asked = append(f.asked, user)
	if f.down {
		return false, "", errors.New("connection refused")
	}
	return (user == "alice" || user == "carol") && pw == "menupass", "ldap", nil
}

// TestMenuAuth: an external account's password is modemmenu's; one with
// its own here is not asked of it; a new one is made when modemmenu knows
// the user (create_users); modemmenu down is trouble, not a failure.
func TestMenuAuth(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := Defaults()
	s.FailDelay = 0
	var logs bytes.Buffer
	b := New(s, st, log.New(&logs, "", 0))
	menu := &fakeMenu{}
	b.MenuAuth = menu
	st.CreateUser("alice", "", "", false)         // external: modemmenu made it
	st.CreateUser("bob", "bobpass123", "", false) // a password of its own
	if _, err := b.Authenticate("test", "src", "ALICE", "menupass"); err != nil {
		t.Errorf("alice by modemmenu: %v", err)
	}
	if menu.asked[0] != "alice" {
		t.Errorf("asked as %q, not the account's name", menu.asked[0])
	}
	if _, err := b.Authenticate("test", "src", "alice", "wrong"); !errors.Is(err, ErrBadLogin) {
		t.Errorf("alice, a wrong password: %v", err)
	}
	menu.asked = nil
	if _, err := b.Authenticate("test", "src", "bob", "menupass"); !errors.Is(err, ErrBadLogin) || len(menu.asked) != 0 {
		t.Errorf("bob: %v, asked %v", err, menu.asked)
	}
	if _, err := b.Authenticate("test", "src", "bob", "bobpass123"); err != nil || len(menu.asked) != 0 {
		t.Errorf("bob's own: %v", err)
	}
	// carol: modemmenu's, not here yet
	if _, err := b.Authenticate("test", "src", "carol", "menupass"); !errors.Is(err, ErrBadLogin) {
		t.Errorf("carol made without create_users: %v", err)
	}
	b.MenuAuthCreate = true
	u, err := b.Authenticate("test", "src", "carol", "menupass")
	if err != nil || !u.External {
		t.Fatalf("carol: %+v %v", u, err)
	}
	if _, err := b.Authenticate("test", "src", "dave", "menupass"); !errors.Is(err, ErrBadLogin) {
		t.Errorf("dave: %v", err)
	}
	if _, err := st.UserByName("dave"); err == nil {
		t.Error("dave was made")
	}
	// down: trouble, said; not counted against the source
	menu.down = true
	_, err = b.Authenticate("test", "src2", "alice", "menupass")
	if err == nil || errors.Is(err, ErrBadLogin) {
		t.Errorf("modemmenu down: %v", err)
	}
	if b.throttled("src2") {
		t.Error("counted")
	}
	if !bytes.Contains(logs.Bytes(), []byte("new external user carol (modemmenu, ldap)")) {
		t.Errorf("log:\n%s", logs.String())
	}
}
