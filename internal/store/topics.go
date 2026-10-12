package store

// Topics: a message that answers none in its area begins a topic, and
// the answers to it, and to them, are its (thread: the first's id).

// Topic is a topic: its first message, how many answer it, and the last.
type Topic struct {
	Root    *Message
	Replies int
	Last    *Message // the newest of it (the first, if none answer)
}

// TopicCount is how many topics an area has.
func (s *Store) TopicCount(area int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE area_id = ? AND thread = 0`, area).Scan(&n)
	return n, err
}

// Topics are an area's topics, the one written in last first.
func (s *Store) Topics(area int64, offset, limit int) ([]*Topic, error) {
	rows, err := s.db.Query(`SELECT id, (SELECT COUNT(*) FROM messages r WHERE r.thread = m.id),
		COALESCE((SELECT MAX(r.id) FROM messages r WHERE r.thread = m.id), m.id) AS last_id
		FROM messages m WHERE m.area_id = ? AND m.thread = 0 ORDER BY last_id DESC LIMIT ? OFFSET ?`, area, limit, offset)
	if err != nil {
		return nil, err
	}
	type row struct {
		id, last int64
		replies  int
	}
	var rs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.replies, &r.last); err != nil {
			rows.Close()
			return nil, err
		}
		rs = append(rs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Topic, 0, len(rs))
	for _, r := range rs {
		t := &Topic{Replies: r.replies}
		if t.Root, err = s.Message(r.id); err != nil {
			return nil, err
		}
		t.Last = t.Root
		if r.last != r.id {
			if t.Last, err = s.Message(r.last); err != nil {
				return nil, err
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// TopicOf is the first message of a message's topic.
func (s *Store) TopicOf(m *Message) int64 {
	if m.Thread != 0 {
		return m.Thread
	}
	return m.ID
}

// TopicLen is how many messages a topic has, its first among them.
func (s *Store) TopicLen(root int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE id = ? OR thread = ?`, root, root).Scan(&n)
	return n, err
}

// TopicMessages are a topic's messages in the order written, from offset.
func (s *Store) TopicMessages(root int64, offset, limit int) ([]*Message, error) {
	return s.messageList(`SELECT `+msgCols+` FROM messages WHERE id = ? OR thread = ? ORDER BY id LIMIT ? OFFSET ?`,
		root, root, limit, offset)
}

// TopicPosition is how many of a topic's messages come before id.
func (s *Store) TopicPosition(root, id int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE (id = ? OR thread = ?) AND id < ?`, root, root, id).Scan(&n)
	return n, err
}

// SetTopicRead notes that a user has read a topic up to a message.
func (s *Store) SetTopicRead(user, root, last int64) error {
	_, err := s.db.Exec(`INSERT INTO topic_read (user_id, root, last) VALUES (?, ?, ?)
		ON CONFLICT (user_id, root) DO UPDATE SET last = {greatest}(topic_read.last, excluded.last)`, user, root, last)
	return err
}

// TopicsRead are how far a user has read each of these topics.
func (s *Store) TopicsRead(user int64, roots []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(roots) == 0 {
		return out, nil
	}
	in, args := inList(roots)
	rows, err := s.db.Query(`SELECT root, last FROM topic_read WHERE user_id = ? AND root IN (`+in+`)`,
		append([]any{user}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r, l int64
		if err := rows.Scan(&r, &l); err != nil {
			return nil, err
		}
		out[r] = l
	}
	return out, rows.Err()
}

// mendOrphans: messages of an area whose topic's first message has gone
// begin topics of their own, the oldest of each the first.
func (s *Store) mendOrphans(area int64) error {
	rows, err := s.db.Query(`SELECT DISTINCT thread FROM messages m WHERE area_id = ? AND thread <> 0
		AND NOT EXISTS (SELECT 1 FROM messages r WHERE r.id = m.thread)`, area)
	if err != nil {
		return err
	}
	var gone []int64
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		gone = append(gone, t)
	}
	rows.Close()
	for _, t := range gone {
		if err := s.mendThread(t); err != nil {
			return err
		}
	}
	return rows.Err()
}

// findThreads puts the messages of a database from before topics in
// theirs: each answer in its first ancestor's of the same area.
func findThreads(x *db) error {
	rows, err := x.Query(`SELECT id, area_id, reply_to FROM messages ORDER BY id`)
	if err != nil {
		return err
	}
	type msg struct{ area, thread int64 }
	seen := map[int64]msg{}
	sets := map[int64][]int64{} // thread: its messages
	for rows.Next() {
		var id, area, reply int64
		if err := rows.Scan(&id, &area, &reply); err != nil {
			rows.Close()
			return err
		}
		m := msg{area: area}
		if p, ok := seen[reply]; ok && reply > 0 && p.area == area {
			m.thread = p.thread
			if m.thread == 0 {
				m.thread = reply
			}
			sets[m.thread] = append(sets[m.thread], id)
		}
		seen[id] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for thread, ids := range sets {
		for len(ids) > 0 {
			n := min(len(ids), 500)
			in, args := inList(ids[:n])
			if _, err := x.Exec(`UPDATE messages SET thread = ? WHERE id IN (`+in+`)`, append([]any{thread}, args...)...); err != nil {
				return err
			}
			ids = ids[n:]
		}
	}
	return nil
}

// LastMessage is an area's newest message (ErrNotFound: none).
func (s *Store) LastMessage(area int64) (*Message, error) {
	return scanMessage(s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE area_id = ? ORDER BY id DESC LIMIT 1`, area))
}

// MessagesBy are a user's newest messages in the areas, newest first.
func (s *Store) MessagesBy(user int64, areas []int64, limit int) ([]*Message, error) {
	if len(areas) == 0 {
		return nil, nil
	}
	in, args := inList(areas)
	return s.messageList(`SELECT `+msgCols+` FROM messages WHERE from_id = ? AND area_id IN (`+in+`)
		ORDER BY id DESC LIMIT ?`, append(append([]any{user}, args...), limit)...)
}
