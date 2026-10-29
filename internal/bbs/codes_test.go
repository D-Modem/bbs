package bbs

import (
	"bytes"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// TestSignInCodes: a sign-in code signs in its own user only, once, before it
// expires, typed loosely; a new one ends the last; a locked user's opens
// nothing; guessing is throttled; and no code goes in the log.
func TestSignInCodes(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := Defaults()
	s.FailDelay = 0
	var logs bytes.Buffer
	b := New(s, st, log.New(&logs, "", 0))
	alice, _ := st.CreateUser("alice", "", "", false) // external: no password here
	bob, _ := st.CreateUser("bob", "bobpass123", "", false)

	code, until := b.LoginCode(alice)
	if len(code) != 9 || code[4] != '-' || time.Until(until) > CodeTTL || time.Until(until) < CodeTTL-time.Minute {
		t.Fatalf("code %q until %v", code, until)
	}
	// another's name: refused, and the code not spent
	if _, err := b.Authenticate("test", "src", "bob", code); !errors.Is(err, ErrBadLogin) {
		t.Errorf("bob with alice's code: %v", err)
	}
	// typed for the password, in lower case, without the dash
	if u, err := b.Authenticate("test", "src", "Alice", strings.ToLower(strings.ReplaceAll(code, "-", ""))); err != nil || u.ID != alice.ID {
		t.Fatalf("alice by her code: %+v %v", u, err)
	}
	if _, err := b.Authenticate("test", "src", "alice", code); !errors.Is(err, ErrBadLogin) {
		t.Errorf("a code twice: %v", err)
	}

	// alone (the forum's sign-in); a new one ends the last
	old, _ := b.LoginCode(bob)
	code, _ = b.LoginCode(bob)
	if _, err := b.AuthenticateCode("test", "src", old); !errors.Is(err, ErrBadLogin) {
		t.Errorf("an old code: %v", err)
	}
	if u, err := b.AuthenticateCode("test", "src", code); err != nil || u.ID != bob.ID {
		t.Errorf("bob's code: %+v %v", u, err)
	}
	// a password still works; a code past its time does not
	if _, err := b.Authenticate("test", "src", "bob", "bobpass123"); err != nil {
		t.Errorf("bob's password: %v", err)
	}
	code, _ = b.LoginCode(bob)
	b.codesMu.Lock()
	for h, c := range b.codes {
		c.expires = time.Now().Add(-time.Second)
		b.codes[h] = c
	}
	b.codesMu.Unlock()
	if _, err := b.AuthenticateCode("test", "src", code); !errors.Is(err, ErrBadLogin) {
		t.Errorf("an expired code: %v", err)
	}
	// a locked account's code opens nothing
	code, _ = b.LoginCode(alice)
	st.SetLocked(alice.ID, true)
	if _, err := b.AuthenticateCode("test", "src", code); !errors.Is(err, ErrLocked) {
		t.Errorf("locked: %v", err)
	}
	// guessing counts as failed logins: throttled
	for i := 0; i < s.ThrottleFails; i++ {
		b.AuthenticateCode("test", "guesser", "AAAA-AAAA")
	}
	if _, err := b.AuthenticateCode("test", "guesser", "AAAA-AAAA"); !errors.Is(err, ErrThrottled) {
		t.Errorf("guessing: %v", err)
	}
	if strings.Contains(logs.String(), strings.ReplaceAll(code, "-", "")) || strings.Contains(logs.String(), code) {
		t.Error("a code in the log")
	}
}

// TestNormalCode: a code is taken in any case, with a dash or spaces, and not
// with the wrong length or letters not in the alphabet.
func TestNormalCode(t *testing.T) {
	for in, want := range map[string]string{"abcd-efgh": "ABCDEFGH", " ABCD EFGH ": "ABCDEFGH", "ABCDEFG": "",
		"ABCD-EFG0": "", "ABCD-EFGI": "", "password1": ""} {
		got, ok := normalCode(in)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
}
