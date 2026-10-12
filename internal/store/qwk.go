package store

import (
	"database/sql"
	"errors"
	"time"
)

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

// SetQWKNode makes an account a QWK network node's, or not.
func (s *Store) SetQWKNode(user int64, on bool) error {
	return s.exec1(`UPDATE users SET qwk_node = ? WHERE id = ?`, on, user)
}

// QWK networks: the messages that came by one, by the number they had at
// its hub.

// QWKMessage is the message that came by a network as that number.
func (s *Store) QWKMessage(net string, number int) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT message_id FROM qwk_msgs WHERE net = ? AND number = ?`, net, number).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// QWKNumber is the number a message that came by a network had there.
func (s *Store) QWKNumber(net string, id int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT number FROM qwk_msgs WHERE net = ? AND message_id = ?`, net, id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return n, err
}

// MarkQWKMessage notes that a message came by a network as that number.
func (s *Store) MarkQWKMessage(net string, number int, id int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO qwk_msgs (net, number, message_id) VALUES (?, ?, ?)`, net, number, id)
	return err
}

// LocalSince are the messages written here (by no network or peer) after
// id in the areas, oldest first, up to limit.
func (s *Store) LocalSince(after int64, areas []int64, limit int) ([]*Message, error) {
	if len(areas) == 0 {
		return nil, nil
	}
	in, args := inList(areas)
	args = append([]any{after}, args...)
	args = append(args, limit)
	return s.messageList(`SELECT `+msgCols+` FROM messages WHERE id > ? AND origin = '' AND area_id IN (`+in+`)
		ORDER BY id LIMIT ?`, args...)
}
