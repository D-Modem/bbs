package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SetEchoTag makes an area a FidoNet echo ("": not one any more).
func (s *Store) SetEchoTag(area int64, tag string) error {
	_, err := s.db.Exec(`UPDATE areas SET echotag = ? WHERE id = ?`, nullString(tag), area)
	if err != nil && isUnique(err) {
		return ErrExists
	}
	return err
}

// AreaByEchoTag finds an area by its echo tag.
func (s *Store) AreaByEchoTag(tag string) (*Area, error) {
	return scanArea(s.db.QueryRow(areaQuery+` WHERE a.echotag = ?`, tag))
}

// EchoAreas lists the areas that are echoes, by tag.
func (s *Store) EchoAreas() ([]*Area, error) {
	rows, err := s.db.Query(areaQuery + ` WHERE a.echotag IS NOT NULL ORDER BY a.echotag`)
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

// FTN is a message's packed form as it came from a link (nil: written
// here, or it came another way).
func (s *Store) FTN(id int64) ([]byte, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT ftn FROM messages WHERE id = ?`, id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

// SetFTNMsgID records the MSGID a message was given when it was first
// sent to FidoNet.
func (s *Store) SetFTNMsgID(id int64, msgid string) error {
	_, err := s.db.Exec(`UPDATE messages SET ftn_msgid = ? WHERE id = ?`, msgid, id)
	return err
}

// MessageByFTNMsgID finds a message by its MSGID.
func (s *Store) MessageByFTNMsgID(msgid string) (*Message, error) {
	return scanMessage(s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE ftn_msgid = ? ORDER BY id LIMIT 1`, msgid))
}

// ToExport lists messages after id in the given areas, not from origin,
// for sending on.
func (s *Store) ToExport(origin string, after int64, areas []int64, limit int) ([]*Message, error) {
	if len(areas) == 0 {
		return nil, nil
	}
	q := `SELECT ` + msgCols + ` FROM messages WHERE id > ? AND origin != ?
		AND area_id IN (?` + strings.Repeat(",?", len(areas)-1) + `) ORDER BY id LIMIT ?`
	args := []any{after, origin}
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

// Netmail is a FidoNet private message waiting to be sent.
type Netmail struct {
	ID      int64
	FromID  int64
	From    string
	To      string
	ToAddr  string
	Subject string
	Body    string
	MsgID   string
	Reply   string // the MSGID it answers
	Created time.Time
}

// QueueNetmail queues a netmail.
func (s *Store) QueueNetmail(n *Netmail) (int64, error) {
	id, err := s.db.insertID(`INSERT INTO netmail_out (from_id, from_name, to_name, to_addr, subject, body, msgid, reply, created)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, n.FromID, n.From, n.To, n.ToAddr, n.Subject, n.Body, n.MsgID, n.Reply, now())
	if err != nil {
		return 0, err
	}
	return id, nil
}

// PendingNetmail lists netmail not yet packed.
func (s *Store) PendingNetmail(limit int) ([]*Netmail, error) {
	rows, err := s.db.Query(`SELECT id, from_id, from_name, to_name, to_addr, subject, body, msgid, reply, created
		FROM netmail_out WHERE packed = 0 ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Netmail
	for rows.Next() {
		var n Netmail
		var created int64
		if err := rows.Scan(&n.ID, &n.FromID, &n.From, &n.To, &n.ToAddr, &n.Subject, &n.Body,
			&n.MsgID, &n.Reply, &created); err != nil {
			return nil, err
		}
		n.Created = unix(created)
		out = append(out, &n)
	}
	return out, rows.Err()
}

// NetmailPacked marks a netmail packed (it is in a link's outbound).
func (s *Store) NetmailPacked(id int64) error {
	_, err := s.db.Exec(`UPDATE netmail_out SET packed = ? WHERE id = ?`, now(), id)
	return err
}
