package store

import (
	"database/sql"
	"errors"
)

// Script data: what scripts keep, by script, and for a user (0: for all).

// ScriptGet is a script's value of name (ok false if it has none).
func (s *Store) ScriptGet(script string, user int64, name string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM script_data WHERE script = ? AND user_id = ? AND name = ?`,
		script, user, name).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// ScriptSet keeps a script's value of name.
func (s *Store) ScriptSet(script string, user int64, name, value string) error {
	_, err := s.db.Exec(`INSERT INTO script_data (script, user_id, name, value) VALUES (?, ?, ?, ?)
		ON CONFLICT (script, user_id, name) DO UPDATE SET value = excluded.value`, script, user, name, value)
	return err
}

// ScriptDelete forgets a script's value of name.
func (s *Store) ScriptDelete(script string, user int64, name string) error {
	_, err := s.db.Exec(`DELETE FROM script_data WHERE script = ? AND user_id = ? AND name = ?`, script, user, name)
	return err
}

// ScriptNames are a script's names beginning with prefix, in order, at
// most limit.
func (s *Store) ScriptNames(script string, user int64, prefix string, limit int) ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM script_data WHERE script = ? AND user_id = ? AND name LIKE ? ESCAPE '!'
		ORDER BY name LIMIT ?`, script, user, likePrefix(prefix), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ScriptCount is how many values a script keeps for a user (or all).
func (s *Store) ScriptCount(script string, user int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM script_data WHERE script = ? AND user_id = ?`, script, user).Scan(&n)
	return n, err
}
