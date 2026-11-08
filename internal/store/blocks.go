package store

// A user's blocked list: BBS users who may not reach them nor see them
// online, the one list every front end that has one shares (MSN's Block
// List; AIM's deny list keeps its own, in the feedbag, for now).

// Blocked are the users a user blocks, by name (Group and Alias unused).
func (s *Store) Blocked(user int64) ([]Friend, error) {
	return s.friendRows(`SELECT b.blocked_id, u.name, '', '', b.added
		FROM blocks b JOIN users u ON u.id = b.blocked_id WHERE b.user_id = ? ORDER BY u.name`, user)
}

// IsBlocked reports whether user blocks other.
func (s *Store) IsBlocked(user, other int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM blocks WHERE user_id = ? AND blocked_id = ?`, user, other).Scan(&n)
	return n > 0, err
}

// Block puts other on user's blocked list.
func (s *Store) Block(user, other int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO blocks (user_id, blocked_id, added) VALUES (?, ?, ?)`, user, other, now())
	return err
}

// Unblock takes other off user's blocked list.
func (s *Store) Unblock(user, other int64) error {
	_, err := s.db.Exec(`DELETE FROM blocks WHERE user_id = ? AND blocked_id = ?`, user, other)
	return err
}

// FriendOf are the users who have user on their friends list, by name
// (Group and Alias theirs).
func (s *Store) FriendOf(user int64) ([]Friend, error) {
	return s.friendRows(`SELECT f.user_id, u.name, f.grp, f.alias, f.added
		FROM friends f JOIN users u ON u.id = f.user_id WHERE f.friend_id = ? ORDER BY u.name`, user)
}

// friendRows reads (id, name, group, alias, added) rows.
func (s *Store) friendRows(q string, args ...any) ([]Friend, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Friend
	for rows.Next() {
		var f Friend
		var at int64
		if err := rows.Scan(&f.ID, &f.Name, &f.Group, &f.Alias, &at); err != nil {
			return nil, err
		}
		f.Added = unix(at)
		out = append(out, f)
	}
	return out, rows.Err()
}
