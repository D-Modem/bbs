package store

import (
	"time"
)

// Friend is one of the BBS users on a user's friends list: the one list
// every front end shows as its own (AIM and ICQ's buddy list, IRC's,
// the BBS's).  Group and Alias are the user's own, for lists that have
// them ("": none).
type Friend struct {
	ID    int64  // the friend's user id
	Name  string // their name (read)
	Group string
	Alias string
	Added time.Time
}

// Friends is a user's friends list, by name.
func (s *Store) Friends(user int64) ([]Friend, error) {
	rows, err := s.db.Query(`SELECT f.friend_id, u.name, f.grp, f.alias, f.added
		FROM friends f JOIN users u ON u.id = f.friend_id WHERE f.user_id = ? ORDER BY u.name`, user)
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

// IsFriend reports whether friend is on user's list.
func (s *Store) IsFriend(user, friend int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM friends WHERE user_id = ? AND friend_id = ?`, user, friend).Scan(&n)
	return n > 0, err
}

// PutFriend adds a friend to a user's list, or sets their group and alias.
func (s *Store) PutFriend(user int64, f Friend) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	var n int
	if err := t.QueryRow(`SELECT COUNT(*) FROM friends WHERE user_id = ? AND friend_id = ?`, user, f.ID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		_, err = t.Exec(`UPDATE friends SET grp = ?, alias = ? WHERE user_id = ? AND friend_id = ?`, f.Group, f.Alias, user, f.ID)
	} else {
		_, err = t.Exec(`INSERT INTO friends (user_id, friend_id, grp, alias, added) VALUES (?, ?, ?, ?, ?)`,
			user, f.ID, f.Group, f.Alias, now())
	}
	if err != nil {
		return err
	}
	return t.Commit()
}

// RemoveFriend takes a friend off a user's list (and its place in any
// front end's list).
func (s *Store) RemoveFriend(user, friend int64) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	for _, q := range []string{`DELETE FROM friends WHERE user_id = ? AND friend_id = ?`,
		`DELETE FROM oscar_buddies WHERE user_id = ? AND friend_id = ?`} {
		if _, err := t.Exec(q, user, friend); err != nil {
			return err
		}
	}
	return t.Commit()
}

// OSCARBuddy is a friend's place in a user's AIM/ICQ buddy list: the name
// as the client wrote it, its group and item ids, its TLVs.
type OSCARBuddy struct {
	FriendID int64
	SN       string
	GroupID  uint16
	ItemID   uint16
	Attrs    []byte
}

// OSCARBuddies are the places of a user's friends in their buddy list, by
// friend.
func (s *Store) OSCARBuddies(user int64) (map[int64]OSCARBuddy, error) {
	rows, err := s.db.Query(`SELECT friend_id, sn, group_id, item_id, attrs FROM oscar_buddies WHERE user_id = ?`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]OSCARBuddy{}
	for rows.Next() {
		var b OSCARBuddy
		var g, i int64
		if err := rows.Scan(&b.FriendID, &b.SN, &g, &i, &b.Attrs); err != nil {
			return nil, err
		}
		b.GroupID, b.ItemID = uint16(g), uint16(i)
		out[b.FriendID] = b
	}
	return out, rows.Err()
}

// PutOSCARBuddy records a friend's place in a user's buddy list.
func (s *Store) PutOSCARBuddy(user int64, b OSCARBuddy) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	attrs := b.Attrs
	if attrs == nil {
		attrs = []byte{}
	}
	if _, err := t.Exec(`DELETE FROM oscar_buddies WHERE user_id = ? AND friend_id = ?`, user, b.FriendID); err != nil {
		return err
	}
	if _, err := t.Exec(`INSERT INTO oscar_buddies (user_id, friend_id, sn, group_id, item_id, attrs) VALUES (?, ?, ?, ?, ?, ?)`,
		user, b.FriendID, b.SN, int64(b.GroupID), int64(b.ItemID), attrs); err != nil {
		return err
	}
	return t.Commit()
}
