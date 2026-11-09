package store

import (
	"strings"
	"time"
)

// Chat is a private line one user sent another, kept: by IRC, AIM, ICQ
// or the BBS's own chat.  Delivered: the one it was to has had it.
type Chat struct {
	ID        int64
	FromID    int64
	From      string
	ToID      int64
	To        string
	Text      string
	Sent      time.Time
	Delivered bool
}

const chatCols = `id, from_id, from_name, to_id, to_name, body, created, delivered`

func scanChats(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Chat, error) {
	var out []Chat
	for rows.Next() {
		var c Chat
		var at int64
		if err := rows.Scan(&c.ID, &c.FromID, &c.From, &c.ToID, &c.To, &c.Text, &at, &c.Delivered); err != nil {
			return nil, err
		}
		c.Sent = unix(at)
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddChat keeps a line; its id.
func (s *Store) AddChat(c *Chat) (int64, error) {
	at := c.Sent
	if at.IsZero() {
		at = time.Now()
	}
	return s.db.insertID(`INSERT INTO chats (from_id, from_name, to_id, to_name, body, created, delivered)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, c.FromID, c.From, c.ToID, c.To, c.Text, at.Unix(), c.Delivered)
}

// KeptChats are the lines to a user they have not had yet, oldest first.
func (s *Store) KeptChats(to int64) ([]Chat, error) {
	rows, err := s.db.Query(`SELECT `+chatCols+` FROM chats WHERE to_id = ? AND delivered = FALSE ORDER BY id`, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChats(rows)
}

// ChatsDelivered marks lines as had.
func (s *Store) ChatsDelivered(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := s.db.Exec(`UPDATE chats SET delivered = TRUE WHERE id IN (?`+strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	return err
}

// ChatsBetween are the last lines (at most limit) between two users, either
// way, oldest first.
func (s *Store) ChatsBetween(a, b int64, limit int) ([]Chat, error) {
	rows, err := s.db.Query(`SELECT `+chatCols+` FROM chats WHERE (from_id = ? AND to_id = ?) OR (from_id = ? AND to_id = ?)
		ORDER BY id DESC LIMIT ?`, a, b, b, a, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cs, err := scanChats(rows)
	for i, j := 0, len(cs)-1; i < j; i, j = i+1, j-1 {
		cs[i], cs[j] = cs[j], cs[i]
	}
	return cs, err
}

// ChatPartner is one a user has talked with: their last line's time, and
// how many lines there are.
type ChatPartner struct {
	ID    int64
	Name  string
	Last  time.Time
	Lines int
}

// ChatPartners are those a user has talked with, the latest first (at most
// limit).
func (s *Store) ChatPartners(user int64, limit int) ([]ChatPartner, error) {
	rows, err := s.db.Query(`SELECT p.other, u.name, MAX(p.created), COUNT(*) FROM
		(SELECT to_id AS other, created FROM chats WHERE from_id = ?
		 UNION ALL SELECT from_id AS other, created FROM chats WHERE to_id = ?) p
		JOIN users u ON u.id = p.other
		GROUP BY p.other, u.name ORDER BY MAX(p.created) DESC LIMIT ?`, user, user, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatPartner
	for rows.Next() {
		var p ChatPartner
		var at int64
		if err := rows.Scan(&p.ID, &p.Name, &at, &p.Lines); err != nil {
			return nil, err
		}
		p.Last = unix(at)
		out = append(out, p)
	}
	return out, rows.Err()
}
