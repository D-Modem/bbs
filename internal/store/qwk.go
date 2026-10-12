package store

import "time"

// What a user's REP packets have brought, so that one uploaded twice
// posts once.

// REPSeen: a message of this hash has come in a REP of the user's.
func (s *Store) REPSeen(user int64, hash string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM qwk_seen WHERE user_id = ? AND hash = ?`, user, hash).Scan(&n)
	return n > 0, err
}

// MarkREP notes a message of this hash come in a REP of the user's.
func (s *Store) MarkREP(user int64, hash string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO qwk_seen (user_id, hash, at) VALUES (?, ?, ?)`, user, hash, now())
	return err
}

// ForgetREPs forgets what REPs brought before this.
func (s *Store) ForgetREPs(before time.Time) error {
	_, err := s.db.Exec(`DELETE FROM qwk_seen WHERE at < ?`, before.Unix())
	return err
}
