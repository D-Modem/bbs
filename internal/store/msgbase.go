package store

import (
	"database/sql"
	"errors"
	"time"
)

// The message base's depth: areas' groups, aliases and how long messages
// are kept; threads; votes and polls; users' aliases and signatures.

// SetAreaGroup puts an area in a group ("": none).
func (s *Store) SetAreaGroup(id int64, group string) error {
	return s.exec1(`UPDATE areas SET grp = ? WHERE id = ?`, group, id)
}

// SetAreaAliases says whether an area's posters may use their aliases.
func (s *Store) SetAreaAliases(id int64, on bool) error {
	return s.exec1(`UPDATE areas SET aliases = ? WHERE id = ?`, on, id)
}

// SetAreaKeep says how many messages an area keeps, and for how many days
// (0: no limit).
func (s *Store) SetAreaKeep(id int64, msgs, days int) error {
	return s.exec1(`UPDATE areas SET max_msgs = ?, max_days = ? WHERE id = ?`, msgs, days, id)
}

// Purge takes away an area's messages beyond what it keeps: more than its
// newest MaxMsgs, older than MaxDays.  How many went.
func (s *Store) Purge(a *Area) (int, error) {
	n := 0
	if a.MaxDays > 0 {
		res, err := s.db.Exec(`DELETE FROM messages WHERE area_id = ? AND created < ?`,
			a.ID, time.Now().AddDate(0, 0, -a.MaxDays).Unix())
		if err != nil {
			return n, err
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	if a.MaxMsgs > 0 {
		var last int64
		err := s.db.QueryRow(`SELECT id FROM messages WHERE area_id = ? ORDER BY id DESC LIMIT 1 OFFSET ?`,
			a.ID, a.MaxMsgs).Scan(&last)
		if errors.Is(err, sql.ErrNoRows) {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		res, err := s.db.Exec(`DELETE FROM messages WHERE area_id = ? AND id <= ?`, a.ID, last)
		if err != nil {
			return n, err
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	if n > 0 {
		// topics whose first messages went begin again with their oldest
		if err := s.mendOrphans(a.ID); err != nil {
			return n, err
		}
	}
	return n, nil
}

// Thread is the messages of m's thread, in its area: from the message it
// all began with, each followed by its replies (depth: how far down).
func (s *Store) Thread(m *Message) ([]*Message, []int, error) {
	// up to the start (within the area, at most 1000 steps)
	root := m
	for i := 0; i < 1000 && root.ReplyTo != 0; i++ {
		p, err := s.Message(root.ReplyTo)
		if errors.Is(err, ErrNotFound) || err == nil && p.AreaID != m.AreaID {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		root = p
	}
	var out []*Message
	var depth []int
	seen := map[int64]bool{}
	var walk func(m *Message, d int) error
	walk = func(m *Message, d int) error {
		if seen[m.ID] || len(out) >= 1000 {
			return nil
		}
		seen[m.ID] = true
		out, depth = append(out, m), append(depth, d)
		kids, err := s.messageList(`SELECT `+msgCols+` FROM messages WHERE reply_to = ? AND area_id = ? ORDER BY id`,
			m.ID, m.AreaID)
		if err != nil {
			return err
		}
		for _, k := range kids {
			if err := walk(k, d+1); err != nil {
				return err
			}
		}
		return nil
	}
	return out, depth, walk(root, 0)
}

// Vote sets a user's vote on a message: 1 up, -1 down, 0 none.
func (s *Store) Vote(msg, user int64, value int) error {
	if value == 0 {
		_, err := s.db.Exec(`DELETE FROM votes WHERE message_id = ? AND user_id = ?`, msg, user)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO votes (message_id, user_id, value) VALUES (?, ?, ?)
		ON CONFLICT (message_id, user_id) DO UPDATE SET value = excluded.value`, msg, user, value)
	return err
}

// Votes are a message's votes up and down, and the user's (1, -1 or 0).
func (s *Store) Votes(msg, user int64) (up, down, mine int, err error) {
	rows, err := s.db.Query(`SELECT user_id, value FROM votes WHERE message_id = ?`, msg)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var u int64
		var v int
		if err := rows.Scan(&u, &v); err != nil {
			return 0, 0, 0, err
		}
		if v > 0 {
			up++
		} else {
			down++
		}
		if u == user {
			mine = v
		}
	}
	return up, down, mine, rows.Err()
}

// SetPoll makes a message a poll with these answers.
func (s *Store) SetPoll(msg int64, answers []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, a := range answers {
		if _, err := tx.Exec(`INSERT INTO poll_answers (message_id, idx, text) VALUES (?, ?, ?)`, msg, i, a); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Poll is a message's poll: its answers, each one's votes, and the
// user's answer (-1: none).  No answers: the message is no poll.
func (s *Store) Poll(msg, user int64) (answers []string, counts []int, mine int, err error) {
	rows, err := s.db.Query(`SELECT text FROM poll_answers WHERE message_id = ? ORDER BY idx`, msg)
	if err != nil {
		return nil, nil, -1, err
	}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return nil, nil, -1, err
		}
		answers = append(answers, t)
	}
	rows.Close()
	mine = -1
	if len(answers) == 0 {
		return nil, nil, -1, nil
	}
	counts = make([]int, len(answers))
	rows, err = s.db.Query(`SELECT user_id, idx FROM poll_votes WHERE message_id = ?`, msg)
	if err != nil {
		return nil, nil, -1, err
	}
	defer rows.Close()
	for rows.Next() {
		var u int64
		var i int
		if err := rows.Scan(&u, &i); err != nil {
			return nil, nil, -1, err
		}
		if i >= 0 && i < len(counts) {
			counts[i]++
		}
		if u == user {
			mine = i
		}
	}
	return answers, counts, mine, rows.Err()
}

// PollVote is a user's answer to a poll (again: their answer changes).
func (s *Store) PollVote(msg, user int64, idx int) error {
	_, err := s.db.Exec(`INSERT INTO poll_votes (message_id, user_id, idx) VALUES (?, ?, ?)
		ON CONFLICT (message_id, user_id) DO UPDATE SET idx = excluded.idx`, msg, user, idx)
	return err
}

// SetAlias sets a user's alias ("": none); ErrExists if it is another's
// name or alias.
func (s *Store) SetAlias(id int64, alias string) error {
	if alias != "" {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id <> ? AND (name = ? OR LOWER(alias) = LOWER(?))`,
			id, alias, alias).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrExists
		}
	}
	return s.exec1(`UPDATE users SET alias = ? WHERE id = ?`, alias, id)
}

// SetSignature sets a user's signature.
func (s *Store) SetSignature(id int64, sig string) error {
	return s.exec1(`UPDATE users SET signature = ? WHERE id = ?`, sig, id)
}
