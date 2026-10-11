package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// migrate brings an older database up to date: columns added since.
func migrate(db *sql.DB) error {
	add := []struct{ table, column, decl string }{
		{"areas", "newsgroup", "TEXT"},
		{"messages", "message_id", "TEXT"},
		{"messages", "origin", "TEXT NOT NULL DEFAULT ''"},
		{"messages", "raw", "BLOB"},
		{"areas", "echotag", "TEXT"},
		{"messages", "ftn_msgid", "TEXT"},
		{"messages", "ftn", "BLOB"},
		{"mail", "ftn_msgid", "TEXT"},
	}
	for _, a := range add {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`,
			a.table, a.column).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, a.table, a.column, a.decl)); err != nil {
				return err
			}
		}
	}
	if _, err := db.Exec(tokenSchema); err != nil {
		return err
	}
	if _, err := db.Exec(sshKeySchema); err != nil {
		return err
	}
	_, err := db.Exec(`
CREATE UNIQUE INDEX IF NOT EXISTS areas_newsgroup ON areas(newsgroup) WHERE newsgroup IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS messages_message_id ON messages(message_id) WHERE message_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS history (
	message_id TEXT PRIMARY KEY,
	seen       INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS peer_groups (
	peer      TEXT NOT NULL,
	newsgroup TEXT NOT NULL,
	last      INTEGER NOT NULL,
	PRIMARY KEY (peer, newsgroup)
);
CREATE TABLE IF NOT EXISTS peer_push (
	peer    TEXT PRIMARY KEY,
	last_id INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS messages_created ON messages(created);
CREATE UNIQUE INDEX IF NOT EXISTS areas_echotag ON areas(echotag) WHERE echotag IS NOT NULL;
CREATE INDEX IF NOT EXISTS messages_ftn_msgid ON messages(ftn_msgid) WHERE ftn_msgid IS NOT NULL;
CREATE TABLE IF NOT EXISTS netmail_out (
	id        INTEGER PRIMARY KEY,
	from_id   INTEGER NOT NULL,
	from_name TEXT NOT NULL,
	to_name   TEXT NOT NULL,
	to_addr   TEXT NOT NULL,
	subject   TEXT NOT NULL,
	body      TEXT NOT NULL,
	msgid     TEXT NOT NULL,
	reply     TEXT NOT NULL DEFAULT '',
	created   INTEGER NOT NULL,
	packed    INTEGER NOT NULL DEFAULT 0
);
`)
	return err
}

// SetNewsgroup makes an area a newsgroup ("": not one any more).
func (s *Store) SetNewsgroup(area int64, group string) error {
	var g any
	if group != "" {
		g = group
	}
	_, err := s.db.Exec(`UPDATE areas SET newsgroup = ? WHERE id = ?`, g, area)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrExists
	}
	return err
}

// AreaByNewsgroup finds an area by its newsgroup.
func (s *Store) AreaByNewsgroup(group string) (*Area, error) {
	return scanArea(s.db.QueryRow(areaQuery+` WHERE a.newsgroup = ?`, group))
}

// Newsgroups lists the areas that are newsgroups, by name.
func (s *Store) Newsgroups() ([]*Area, error) {
	rows, err := s.db.Query(areaQuery + ` WHERE a.newsgroup IS NOT NULL ORDER BY a.newsgroup`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Area
	for rows.Next() {
		a, err := scanArea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MessageByMessageID finds an article by its Message-ID.
func (s *Store) MessageByMessageID(id string) (*Message, error) {
	return scanMessage(s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE message_id = ?`, id))
}

// Raw is an article as it is sent.
func (s *Store) Raw(id int64) ([]byte, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT raw FROM messages WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return raw, err
}

// Seen reports whether a Message-ID has been here: taken, or turned away.
func (s *Store) Seen(id string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM history WHERE message_id = ?`, id).Scan(&n)
	return n > 0, err
}

// Remember notes a Message-ID as seen (an article refused, so that it is
// not offered again and again).
func (s *Store) Remember(id string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO history (message_id, seen) VALUES (?, ?)`, id, now())
	return err
}

// ExpireHistory forgets Message-IDs seen before t that are no longer here.
func (s *Store) ExpireHistory(t time.Time) error {
	_, err := s.db.Exec(`DELETE FROM history WHERE seen < ? AND message_id NOT IN
		(SELECT message_id FROM messages WHERE message_id IS NOT NULL)`, t.Unix())
	return err
}

// Articles lists up to limit articles of an area numbered from lo to hi
// (inclusive; article numbers are message ids), without their raw form.
func (s *Store) Articles(area, lo, hi int64, limit int) ([]*Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE area_id = ? AND id >= ? AND id <= ?
		AND message_id IS NOT NULL ORDER BY id LIMIT ?`, area, lo, hi, limit)
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

// ArticlesSince lists the Message-IDs of articles that came since t, in
// the given areas.
func (s *Store) ArticlesSince(t time.Time, areas []int64, limit int) ([]string, error) {
	if len(areas) == 0 {
		return nil, nil
	}
	q := `SELECT message_id FROM messages WHERE created >= ? AND message_id IS NOT NULL AND area_id IN (?` +
		strings.Repeat(",?", len(areas)-1) + `) ORDER BY id LIMIT ?`
	args := []any{t.Unix()}
	for _, a := range areas {
		args = append(args, a)
	}
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PeerLast is the last article number of a peer's newsgroup we have taken.
func (s *Store) PeerLast(peer, group string) (int64, bool, error) {
	var n int64
	err := s.db.QueryRow(`SELECT last FROM peer_groups WHERE peer = ? AND newsgroup = ?`, peer, group).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return n, err == nil, err
}

// SetPeerLast records it.
func (s *Store) SetPeerLast(peer, group string, last int64) error {
	_, err := s.db.Exec(`INSERT INTO peer_groups (peer, newsgroup, last) VALUES (?, ?, ?)
		ON CONFLICT (peer, newsgroup) DO UPDATE SET last = excluded.last`, peer, group, last)
	return err
}

// PeerPushed is the last message id offered to a peer.
func (s *Store) PeerPushed(peer string) (int64, bool, error) {
	var n int64
	err := s.db.QueryRow(`SELECT last_id FROM peer_push WHERE peer = ?`, peer).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return n, err == nil, err
}

// SetPeerPushed records it.
func (s *Store) SetPeerPushed(peer string, id int64) error {
	_, err := s.db.Exec(`INSERT INTO peer_push (peer, last_id) VALUES (?, ?)
		ON CONFLICT (peer) DO UPDATE SET last_id = excluded.last_id`, peer, id)
	return err
}

// ToPush lists articles after id in the given areas, not from the peer
// itself, for offering to it.
func (s *Store) ToPush(peer string, after int64, areas []int64, limit int) ([]*Message, error) {
	if len(areas) == 0 {
		return nil, nil
	}
	q := `SELECT ` + msgCols + ` FROM messages WHERE id > ? AND message_id IS NOT NULL AND origin != ?
		AND area_id IN (?` + strings.Repeat(",?", len(areas)-1) + `) ORDER BY id LIMIT ?`
	args := []any{after, peer}
	for _, a := range areas {
		args = append(args, a)
	}
	args = append(args, limit)
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

// MaxMessageID is the highest message id there is.
func (s *Store) MaxMessageID() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM messages`).Scan(&n)
	return n, err
}
