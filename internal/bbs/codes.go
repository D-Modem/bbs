package bbs

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// Sign-in codes: a caller logged in asks for one (their profile), and
// signs in with it once, within CodeTTL, where they would type a password:
// the web forum's sign-in (the code alone), the web terminal, the API.  A
// password typed nowhere, for a user whose password is another system's
// (modemmenu's) or who would rather not type it on a shared machine.

// CodeTTL is how long a sign-in code lasts.
const CodeTTL = 5 * time.Minute

// codeAlphabet: no 0 and O, no 1 and I: read off a screen and typed
const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

const codeLen = 8 // 40 bits

type signInCode struct {
	userID  int64
	expires time.Time
}

// LoginCode makes a sign-in code for u, in the place of any it had: shown
// as ABCD-EFGH; and when it ends.
func (b *BBS) LoginCode(u *store.User) (string, time.Time) {
	var raw [codeLen]byte
	rand.Read(raw[:])
	code := make([]byte, codeLen)
	for i, r := range raw {
		code[i] = codeAlphabet[int(r)%len(codeAlphabet)] // 256 is 8 x 32: even
	}
	expires := time.Now().Add(CodeTTL)
	b.codesMu.Lock()
	defer b.codesMu.Unlock()
	b.expireCodes()
	if b.codes == nil {
		b.codes = map[string]signInCode{}
	}
	for h, c := range b.codes {
		if c.userID == u.ID {
			delete(b.codes, h)
		}
	}
	b.codes[codeHash(string(code))] = signInCode{userID: u.ID, expires: expires}
	return string(code[:4]) + "-" + string(code[4:]), expires
}

// normalCode is s as a code (upper case; the dash, spaces gone), ok if it
// could be one.
func normalCode(s string) (string, bool) {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(s)))
	if len(s) != codeLen {
		return "", false
	}
	for _, r := range s {
		if !strings.ContainsRune(codeAlphabet, r) {
			return "", false
		}
	}
	return s, true
}

// codeHash is a code as kept: its SHA-256 in hex, so that the codes are not
// held as typed.
func codeHash(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:])
}

// expireCodes takes away those past their time (codesMu held).
func (b *BBS) expireCodes() {
	now := time.Now()
	for h, c := range b.codes {
		if now.After(c.expires) {
			delete(b.codes, h)
		}
	}
}

// spendCode is the user a code is for, the code spent; only if for is
// nil or says yes to the user.
func (b *BBS) spendCode(s string, accept func(*store.User) bool) *store.User {
	code, ok := normalCode(s)
	if !ok {
		return nil
	}
	h := codeHash(code)
	b.codesMu.Lock()
	defer b.codesMu.Unlock()
	b.expireCodes()
	c, ok := b.codes[h]
	if !ok {
		return nil
	}
	u, err := b.Store.UserByID(c.userID)
	if err != nil || accept != nil && !accept(u) {
		return nil
	}
	delete(b.codes, h)
	return u
}

// AuthenticateCode signs in by a code alone (the forum's sign-in), throttled,
// logged and refused as a password is.
func (b *BBS) AuthenticateCode(who, source, code string) (*store.User, error) {
	return b.authenticate(who, source, "a sign-in code", func() (*store.User, error) {
		if u := b.spendCode(code, nil); u != nil {
			b.Log.Printf("%s: %s by a sign-in code", who, u.Name)
			return u, nil
		}
		return nil, store.ErrNotFound
	})
}
