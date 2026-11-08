package store

import (
	"crypto/md5"
	"database/sql"
	"errors"
	"unicode"
)

// CompuServe's Secure Login (WinCIM's "+" User ID) proves the password by
// MD5 digests of a key made from it, K = MD5(UTF-16BE(lowercase password)),
// which a bcrypt hash cannot give.  So K is kept, like the AIM digests
// (oscar.go): whenever the password is known in the clear, and bound to
// the password hash it was made with, so a password changed some other way
// leaves it stale and unused.

// CISKeyOf is the Secure Login key of a password.
func CISKeyOf(pw string) []byte {
	var b []byte
	for _, r := range pw {
		r = unicode.ToLower(r)
		if r > 0xFFFF {
			r = 0xFFFD
		}
		b = append(b, byte(r>>8), byte(r))
	}
	k := md5.Sum(b)
	return k[:]
}

// SetKeepCIS says whether to keep the keys: only with a CompuServe
// listener, as they would let anyone who read the database log in by CIM.
func (s *Store) SetKeepCIS(on bool) { s.keepCIS.Store(on) }

// CISKey is the account's Secure Login key, if a current one is kept.
func (s *Store) CISKey(id int64) (k []byte, ok bool, err error) {
	var bound, hash string
	err = s.db.QueryRow(`SELECT c.k, c.bound, u.password FROM cis_keys c
		JOIN users u ON u.id = c.user_id WHERE c.user_id = ?`, id).Scan(&k, &bound, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return k, bound == hash && len(k) == md5.Size, nil
}

// KeepCISKey keeps the account's key of pw (its password, just checked or
// set), bound to its password hash now, if the store keeps them.
func (s *Store) KeepCISKey(id int64, pw string) error {
	if !s.keepCIS.Load() || pw == "" {
		return nil
	}
	var hash string
	if err := s.db.QueryRow(`SELECT password FROM users WHERE id = ?`, id).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	k := CISKeyOf(pw)
	if old, ok, err := s.CISKey(id); err == nil && ok && string(old) == string(k) {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO cis_keys (user_id, k, bound) VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET k = excluded.k, bound = excluded.bound`, id, k, hash)
	return err
}
