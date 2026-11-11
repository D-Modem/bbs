package bbs

import (
	"strings"

	"github.com/D-Modem/bbs/internal/ntlm"
	"github.com/D-Modem/bbs/internal/store"
)

// NTLMOffered reports whether NTLM sign-ins can be offered: only while the
// store keeps the password hashes they are checked against (while MSN is
// served).
func (b *BBS) NTLMOffered() bool { return b.Store.KeepsNTLM() }

// NTLMCheck is a check for AuthenticateBy or LoginWith: the account a
// names (by its name, its screen name, or an address's name before the
// @), if a's answer to challenge proves its password.  It gives the
// account's hashes to keys, if given, for the session key.  An account
// whose hashes aren't kept yet fails, logged as why.
func (b *BBS) NTLMCheck(who string, a *ntlm.Auth, challenge []byte, keys func(lm, nt []byte)) func() (*store.User, error) {
	return func() (*store.User, error) {
		u, err := b.ntlmUser(a.User)
		if err != nil {
			return nil, store.ErrNotFound
		}
		lm, nt, ok, err := b.Store.NTLMKey(u.ID)
		if err != nil || !ok {
			b.Log.Printf("%s: NTLM: no password hashes kept for %s yet: they are kept when the password is next typed in the clear", who, u.Name)
			return nil, store.ErrNotFound
		}
		if !a.Verify(lm, nt, challenge) {
			return nil, store.ErrNotFound
		}
		if keys != nil {
			keys(lm, nt)
		}
		return u, nil
	}
}

func (b *BBS) ntlmUser(name string) (*store.User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, store.ErrNotFound
	}
	u, err := b.Store.UserByName(name)
	if err == nil {
		return u, nil
	}
	if u, err := b.Store.UserByScreenName(name); err == nil {
		return u, nil
	}
	if local, _, ok := strings.Cut(name, "@"); ok && local != "" {
		return b.Store.UserByName(local)
	}
	return nil, err
}

// AuthenticateNTLM is a login by NTLM, as Authenticate's by password.
func (b *BBS) AuthenticateNTLM(who, source string, a *ntlm.Auth, challenge []byte) (*store.User, error) {
	return b.authenticate(who, source, a.User, b.NTLMCheck(who, a, challenge, nil))
}
