package store

import (
	"fmt"
	"time"
)

// What Times counts: a table and its time column.
var timeColumns = map[string]string{
	"calls":    "SELECT at FROM calls WHERE at >= ?",
	"posts":    "SELECT created FROM messages WHERE created >= ?",
	"users":    "SELECT created FROM users WHERE created >= ?",
	"uploads":  "SELECT created FROM files WHERE created >= ?",
	"mail":     "SELECT created FROM mail WHERE created >= ?",
	"messages": "SELECT created FROM messages WHERE created >= ?",
}

// Times are when what happened since t: calls, posts, users (made),
// uploads, mail.
func (s *Store) Times(what string, since time.Time) ([]time.Time, error) {
	q, ok := timeColumns[what]
	if !ok {
		return nil, fmt.Errorf("no times of %q", what)
	}
	rows, err := s.db.Query(q, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var at int64
		if err := rows.Scan(&at); err != nil {
			return nil, err
		}
		out = append(out, unix(at))
	}
	return out, rows.Err()
}

// Totals are how many users, messages, mail, files and calls (kept) there
// are.
func (s *Store) Totals() (map[string]int, error) {
	out := map[string]int{}
	for k, q := range map[string]string{"users": "SELECT COUNT(*) FROM users", "messages": "SELECT COUNT(*) FROM messages",
		"mail": "SELECT COUNT(*) FROM mail", "files": "SELECT COUNT(*) FROM files", "calls": "SELECT COUNT(*) FROM calls"} {
		var n int
		if err := s.db.QueryRow(q).Scan(&n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, nil
}
