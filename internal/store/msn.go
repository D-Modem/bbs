package store

import (
	"crypto/md5"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
)

// MSN Messenger's MD5 login (MSNP2 to MSNP7: MSN 1.0 to 4.7) proves the
// password by MD5(salt + password), the salt the server's, which a bcrypt
// hash cannot check.  So each account gets a fixed salt and the digest of
// its password with it, kept whenever the password is known in the clear
// and bound to the password hash it was made with, as the AIM digests are
// (oscar.go).  A fixed salt makes the digest replayable: anyone who sees
// a login can log in again.  MSN 1.0 to 4.7 had no better way.

// MSNDigest is an MD5 login's answer: MD5(salt + password), in lowercase
// hex.
func MSNDigest(salt, pw string) string {
	h := md5.Sum([]byte(salt + pw))
	return hex.EncodeToString(h[:])
}

// SetKeepMSN says whether to keep the digests: only with an MSN server,
// as they would let anyone who read the database log in by MSN.
func (s *Store) SetKeepMSN(on bool) { s.keepMSN.Store(on) }

// MSNKey is the account's MD5 login salt and digest; ok false: no current
// digest (none kept, or the password changed since).
func (s *Store) MSNKey(id int64) (salt, digest string, ok bool, err error) {
	var bound, hash string
	err = s.db.QueryRow(`SELECT m.salt, m.digest, m.bound, u.password FROM msn_keys m
		JOIN users u ON u.id = m.user_id WHERE m.user_id = ?`, id).Scan(&salt, &digest, &bound, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return salt, digest, bound == hash && digest != "", nil
}

// MSNSalt is the salt an MD5 login gives the account: its kept one, or a
// new one kept for it (with no digest yet).
func (s *Store) MSNSalt(id int64) (string, error) {
	var salt string
	err := s.db.QueryRow(`SELECT salt FROM msn_keys WHERE user_id = ?`, id).Scan(&salt)
	if err == nil {
		return salt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	b := make([]byte, 10)
	rand.Read(b)
	salt = hex.EncodeToString(b)
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO msn_keys (user_id, salt, digest, bound) VALUES (?, ?, '', '')`, id, salt); err != nil {
		return "", err
	}
	// another login may have made one meanwhile
	err = s.db.QueryRow(`SELECT salt FROM msn_keys WHERE user_id = ?`, id).Scan(&salt)
	return salt, err
}

// KeepMSNKey makes the account's MD5 login digest of pw (its password,
// just checked or set), bound to its password hash now, if the store keeps
// them (SetKeepMSN).  Nothing is written if it is current already.
func (s *Store) KeepMSNKey(id int64, pw string) error {
	if !s.keepMSN.Load() || pw == "" {
		return nil
	}
	var hash string
	if err := s.db.QueryRow(`SELECT password FROM users WHERE id = ?`, id).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	salt, err := s.MSNSalt(id)
	if err != nil {
		return err
	}
	d := MSNDigest(salt, pw)
	if _, old, ok, err := s.MSNKey(id); err == nil && ok && old == d {
		return nil
	}
	_, err = s.db.Exec(`UPDATE msn_keys SET digest = ?, bound = ? WHERE user_id = ?`, d, hash, id)
	return err
}
