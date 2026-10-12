package store

import "time"

// Call is a login, for the last callers and the statistics.
type Call struct {
	ID       int64
	UserID   int64
	Name     string
	Location string
	Via      string
	Node     int
	At       time.Time
}

// keepCalls is how many calls are kept.
const keepCalls = 10000

// AddCall records a login, forgetting the oldest beyond keepCalls.
func (s *Store) AddCall(c *Call) error {
	id, err := s.db.insertID(`INSERT INTO calls (user_id, name, location, via, node, at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.UserID, c.Name, c.Location, c.Via, c.Node, now())
	if err != nil {
		return err
	}
	if id%100 == 0 {
		_, err = s.db.Exec(`DELETE FROM calls WHERE id <= ?`, id-keepCalls)
	}
	return err
}

// LastCalls are the latest calls, newest first.
func (s *Store) LastCalls(limit int) ([]*Call, error) {
	rows, err := s.db.Query(`SELECT id, user_id, name, location, via, node, at FROM calls
		ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Call
	for rows.Next() {
		var c Call
		var at int64
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.Location, &c.Via, &c.Node, &at); err != nil {
			return nil, err
		}
		c.At = unix(at)
		out = append(out, &c)
	}
	return out, rows.Err()
}
