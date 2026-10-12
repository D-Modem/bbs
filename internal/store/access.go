package store

import "time"

// Today is the day as the daily counts keep it: YYYYMMDD, local time.
func Today() int {
	y, m, d := time.Now().Date()
	return y*10000 + int(m)*100 + d
}

// SetLevel sets a user's access level.
func (s *Store) SetLevel(id int64, level int) error {
	return s.exec1(`UPDATE users SET level = ? WHERE id = ?`, level, id)
}

// SetFlags sets a user's access flags.
func (s *Store) SetFlags(id int64, flags string) error {
	return s.exec1(`UPDATE users SET flags = ? WHERE id = ?`, flags, id)
}

// AddTime adds to a user's time online today (a new day starts at 0).
func (s *Store) AddTime(id int64, secs int) error {
	day := Today()
	return s.exec1(`UPDATE users SET time_used = CASE WHEN time_day = ? THEN time_used + ? ELSE ? END,
		time_day = ? WHERE id = ?`, day, secs, secs, day, id)
}

// CountUserDownload counts a download of the user's today.
func (s *Store) CountUserDownload(id int64) error {
	day := Today()
	return s.exec1(`UPDATE users SET dl_count = CASE WHEN dl_day = ? THEN dl_count + 1 ELSE 1 END,
		dl_day = ? WHERE id = ?`, day, day, id)
}

// TimeToday is the user's time online today, in seconds.
func (u *User) TimeToday() int {
	if u.TimeDay != Today() {
		return 0
	}
	return u.TimeUsed
}

// DownloadsToday is the user's downloads today.
func (u *User) DownloadsToday() int {
	if u.DLDay != Today() {
		return 0
	}
	return u.DLCount
}

// SetAreaRequire sets what reading and posting in an area require.
func (s *Store) SetAreaRequire(id int64, read, post string) error {
	return s.exec1(`UPDATE areas SET read_req = ?, post_req = ? WHERE id = ?`, read, post, id)
}

// SetFileAreaRequire sets what downloading and uploading in a file area
// require.
func (s *Store) SetFileAreaRequire(id int64, download, upload string) error {
	return s.exec1(`UPDATE file_areas SET download_req = ?, upload_req = ? WHERE id = ?`, download, upload, id)
}

// UsersBelow lists the users whose level is under level (and are not
// sysops or locked): those waiting to be validated.
func (s *Store) UsersBelow(level int) ([]*User, error) {
	rows, err := s.db.Query(`SELECT `+userCols+` FROM users WHERE level < ? AND sysop = 0 AND locked = 0 ORDER BY id`, level)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, _, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
