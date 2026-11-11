package store

import (
	"database/sql"
	"errors"

	"github.com/D-Modem/bbs/internal/ntlm"
)

// MSN's clients from 1996 on (MSN Classic's on Windows 95 OSR2, and MSN
// 2.x's Program Viewer, mail and news) sign in with NTLM: it proves the
// password by DES over the server's challenge, keyed with the
// password's LM or NT hash, which a bcrypt hash cannot give.  So both
// hashes are kept as CompuServe's key is (cis.go): whenever the password
// is known in the clear, bound to the password hash they were made with.
// They are password equivalents (the LM hash a weak one), kept only when
// an MSN Classic front end or [security] ntlm asks for them.

// SetKeepNTLM says whether to keep the hashes: only when asked, as they
// would let anyone who read the database sign in by them.
func (s *Store) SetKeepNTLM(on bool) { s.keepNTLM.Store(on) }

// KeepsNTLM reports whether the hashes are kept, and so whether NTLM
// sign-ins can be offered.
func (s *Store) KeepsNTLM() bool { return s.keepNTLM.Load() }

// NTLMKey is the account's LM and NT hashes, if current ones are kept.
func (s *Store) NTLMKey(id int64) (lm, nt []byte, ok bool, err error) {
	var bound, hash string
	err = s.db.QueryRow(`SELECT n.lm, n.nt, n.bound, u.password FROM ntlm_keys n
		JOIN users u ON u.id = n.user_id WHERE n.user_id = ?`, id).Scan(&lm, &nt, &bound, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	return lm, nt, bound == hash && len(lm) == 16 && len(nt) == 16, nil
}

// KeepNTLMKey keeps the account's hashes of pw (its password, just checked
// or set), bound to its password hash now, if the store keeps them.
func (s *Store) KeepNTLMKey(id int64, pw string) error {
	if !s.keepNTLM.Load() || pw == "" {
		return nil
	}
	var hash string
	if err := s.db.QueryRow(`SELECT password FROM users WHERE id = ?`, id).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	lm, nt := ntlm.LMHash(pw), ntlm.NTHash(pw)
	if ol, on, ok, err := s.NTLMKey(id); err == nil && ok && string(ol) == string(lm) && string(on) == string(nt) {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO ntlm_keys (user_id, lm, nt, bound) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET lm = excluded.lm, nt = excluded.nt, bound = excluded.bound`, id, lm, nt, hash)
	return err
}
