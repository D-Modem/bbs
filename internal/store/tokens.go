package store

import (
	"database/sql"
	"errors"
	"time"
)

// Token is an API token: a key a user made, or a login's session.  Only
// its hash is kept.
type Token struct {
	ID       int64
	UserID   int64
	User     string // the user's name (read)
	Name     string
	Hash     string
	Scopes   string // space separated
	Session  bool
	Created  time.Time
	Expires  time.Time // zero: never
	LastUsed time.Time
}

// AddToken records a token.
func (s *Store) AddToken(t *Token) (int64, error) {
	exp := int64(0)
	if !t.Expires.IsZero() {
		exp = t.Expires.Unix()
	}
	id, err := s.db.insertID(`INSERT INTO api_tokens (user_id, name, hash, scopes, session, created, expires)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, t.UserID, t.Name, t.Hash, t.Scopes, t.Session, now(), exp)
	if err != nil {
		return 0, err
	}
	return id, nil
}

const tokenCols = `t.id, t.user_id, u.name, t.name, t.hash, t.scopes, t.session, t.created, t.expires, t.last_used`

func scanToken(row interface{ Scan(...any) error }) (*Token, error) {
	var t Token
	var created, expires, last int64
	err := row.Scan(&t.ID, &t.UserID, &t.User, &t.Name, &t.Hash, &t.Scopes, &t.Session, &created, &expires, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	t.Created, t.Expires, t.LastUsed = unix(created), unix(expires), unix(last)
	return &t, err
}

// TokenByHash finds a token, if it has not expired.
func (s *Store) TokenByHash(hash string) (*Token, error) {
	return scanToken(s.db.QueryRow(`SELECT `+tokenCols+` FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.hash = ? AND (t.expires = 0 OR t.expires > ?)`, hash, now()))
}

// TouchToken notes a token's use.
func (s *Store) TouchToken(id int64) error {
	_, err := s.db.Exec(`UPDATE api_tokens SET last_used = ? WHERE id = ?`, now(), id)
	return err
}

// Tokens lists the tokens (of a user, or all if user is 0), sessions too.
func (s *Store) Tokens(user int64) ([]*Token, error) {
	q := `SELECT ` + tokenCols + ` FROM api_tokens t JOIN users u ON u.id = t.user_id`
	args := []any{}
	if user != 0 {
		q += ` WHERE t.user_id = ?`
		args = append(args, user)
	}
	rows, err := s.db.Query(q+` ORDER BY t.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeToken deletes a token (of a user, or anyone's if user is 0).
func (s *Store) RevokeToken(id, user int64) error {
	if user == 0 {
		return s.exec1(`DELETE FROM api_tokens WHERE id = ?`, id)
	}
	return s.exec1(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, user)
}

// ExpireTokens deletes the tokens past their time.
func (s *Store) ExpireTokens() error {
	_, err := s.db.Exec(`DELETE FROM api_tokens WHERE expires != 0 AND expires <= ?`, now())
	return err
}
