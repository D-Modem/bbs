package store

import "strings"

// ScanOff are the areas a user has left out of their new scan.
func (s *Store) ScanOff(user int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT area_id FROM scan_off WHERE user_id = ?`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SetScan puts an area in a user's new scan, or leaves it out.
func (s *Store) SetScan(user, area int64, on bool) error {
	if on {
		_, err := s.db.Exec(`DELETE FROM scan_off WHERE user_id = ? AND area_id = ?`, user, area)
		return err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO scan_off (user_id, area_id) VALUES (?, ?)`, user, area)
	return err
}

// messageList is the messages a query of msgCols finds.
func (s *Store) messageList(q string, args ...any) ([]*Message, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MessagesTo are the newest messages in the areas addressed to name (in
// any case), up to limit, newest first.
func (s *Store) MessagesTo(name string, limit int) ([]*Message, error) {
	return s.messageList(`SELECT `+msgCols+` FROM messages WHERE LOWER(to_name) = LOWER(?)
		ORDER BY id DESC LIMIT ?`, name, limit)
}

// likePrefix makes text a LIKE pattern that matches what begins with it,
// in its case.
func likePrefix(text string) string {
	r := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return r.Replace(text) + "%"
}

// likeEscape makes text a LIKE pattern that matches it anywhere.
func likeEscape(text string) string {
	r := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return "%" + r.Replace(strings.ToLower(text)) + "%"
}

// SearchMessages are the newest messages of the areas whose subject,
// body or author has text in it (in any case), up to limit, newest first.
func (s *Store) SearchMessages(areas []int64, text string, limit int) ([]*Message, error) {
	if len(areas) == 0 {
		return nil, nil
	}
	in, args := inList(areas)
	p := likeEscape(text)
	args = append(args, p, p, p, limit)
	return s.messageList(`SELECT `+msgCols+` FROM messages WHERE area_id IN (`+in+`)
		AND (LOWER(subject) LIKE ? ESCAPE '!' OR LOWER(body) LIKE ? ESCAPE '!' OR LOWER(from_name) LIKE ? ESCAPE '!')
		ORDER BY id DESC LIMIT ?`, args...)
}
