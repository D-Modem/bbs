package store

import "time"

// ChatRoom is a permanent chat room: kept open when no one is in it.
type ChatRoom struct {
	Name      string
	Topic     string
	CreatedBy string
	Created   time.Time
}

// ChatRooms are the permanent chat rooms, by name.
func (s *Store) ChatRooms() ([]*ChatRoom, error) {
	rows, err := s.db.Query(`SELECT name, topic, created_by, created FROM chat_rooms ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ChatRoom
	for rows.Next() {
		var r ChatRoom
		var created int64
		if err := rows.Scan(&r.Name, &r.Topic, &r.CreatedBy, &created); err != nil {
			return nil, err
		}
		r.Created = unix(created)
		out = append(out, &r)
	}
	return out, rows.Err()
}

// AddChatRoom makes a room permanent; ErrExists if it is already.
func (s *Store) AddChatRoom(name, topic, by string) error {
	_, err := s.db.Exec(`INSERT INTO chat_rooms (name, topic, created_by, created) VALUES (?, ?, ?, ?)`,
		name, topic, by, now())
	if isUnique(err) {
		return ErrExists
	}
	return err
}

// DeleteChatRoom makes a room permanent no more.
func (s *Store) DeleteChatRoom(name string) error {
	return s.exec1(`DELETE FROM chat_rooms WHERE name = ?`, name)
}

// SetChatTopic sets a permanent room's topic.  (Not exec1: MySQL counts
// no rows changed when the topic is the same.)
func (s *Store) SetChatTopic(name, topic string) error {
	_, err := s.db.Exec(`UPDATE chat_rooms SET topic = ? WHERE name = ?`, topic, name)
	return err
}
