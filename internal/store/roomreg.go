package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Registered chat rooms (ChanServ's): a room a user owns, its settings
// and topic, and its access list: who has which flags there.

// RoomReg is a registered room.
type RoomReg struct {
	Name       string
	FounderID  int64
	Founder    string // the founder's name ("": the account is gone)
	Registered time.Time
	LastUsed   time.Time
	Settings   string // words: "guard private topiclock"
	Descr      string
	URL        string
	EntryMsg   string
	Topic      string
	TopicBy    string
	TopicAt    time.Time
	// the channel modes: the letters on ("ntm"), the key and the limit
	Modes string
	Key   string
	Limit int
	// the ban, quiet, exception and invite-exception lists
	Lists []*RoomListEntry
}

// RoomListEntry is a mask on one of a room's lists: kind b (bans), q
// (quiets), e (exceptions) or I (invite exceptions).
type RoomListEntry struct {
	Room  string
	Kind  string
	Mask  string
	SetBy string
	SetAt time.Time
}

// List is the room's entries of a kind.
func (r *RoomReg) List(kind string) []*RoomListEntry {
	var out []*RoomListEntry
	for _, e := range r.Lists {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// Has: the room has that setting on.
func (r *RoomReg) Has(setting string) bool {
	for _, w := range strings.Fields(r.Settings) {
		if w == setting {
			return true
		}
	}
	return false
}

// RoomAccess is one on a registered room's access list.
type RoomAccess struct {
	Room   string
	UserID int64
	Name   string // the user's name
	Flags  string
	SetBy  string
	SetAt  time.Time
}

const roomRegCols = `r.name, r.founder_id, COALESCE(u.name, ''), r.registered, r.last_used, r.settings, r.descr, r.url, r.entrymsg, r.topic, r.topic_by, r.topic_at, r.modes, r.mode_key, r.mode_limit`

func scanRoomReg(sc interface{ Scan(...any) error }) (*RoomReg, error) {
	var r RoomReg
	var reg, used, tat int64
	if err := sc.Scan(&r.Name, &r.FounderID, &r.Founder, &reg, &used, &r.Settings, &r.Descr, &r.URL, &r.EntryMsg, &r.Topic, &r.TopicBy, &tat, &r.Modes, &r.Key, &r.Limit); err != nil {
		return nil, err
	}
	r.Registered, r.LastUsed, r.TopicAt = unix(reg), unix(used), unix(tat)
	return &r, nil
}

// RoomRegs are the registered rooms, by name.
func (s *Store) RoomRegs() ([]*RoomReg, error) {
	rows, err := s.db.Query(`SELECT ` + roomRegCols + ` FROM room_reg r LEFT JOIN users u ON u.id = r.founder_id ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RoomReg
	for rows.Next() {
		r, err := scanRoomReg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	lists, err := s.roomLists(``)
	if err != nil {
		return nil, err
	}
	byName := map[string]*RoomReg{}
	for _, r := range out {
		byName[r.Name] = r
	}
	for _, e := range lists {
		if r := byName[e.Room]; r != nil {
			r.Lists = append(r.Lists, e)
		}
	}
	return out, nil
}

// RoomReg is a registered room; ErrNotFound if it is not registered.
func (s *Store) RoomReg(name string) (*RoomReg, error) {
	r, err := scanRoomReg(s.db.QueryRow(`SELECT `+roomRegCols+` FROM room_reg r LEFT JOIN users u ON u.id = r.founder_id WHERE r.name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.Lists, err = s.roomLists(`WHERE room = ?`, name)
	return r, err
}

// roomLists are rooms' list entries, oldest first.
func (s *Store) roomLists(where string, args ...any) ([]*RoomListEntry, error) {
	rows, err := s.db.Query(`SELECT room, kind, mask, set_by, set_at FROM room_lists `+where+` ORDER BY set_at, mask`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RoomListEntry
	for rows.Next() {
		var e RoomListEntry
		var at int64
		if err := rows.Scan(&e.Room, &e.Kind, &e.Mask, &e.SetBy, &at); err != nil {
			return nil, err
		}
		e.SetAt = unix(at)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// SetRoomModes sets a registered room's modes, key and limit.
func (s *Store) SetRoomModes(name, modes, key string, limit int) error {
	_, err := s.db.Exec(`UPDATE room_reg SET modes = ?, mode_key = ?, mode_limit = ? WHERE name = ?`, modes, key, limit, name)
	return err
}

// AddRoomList puts a mask on a room's list; ErrExists if it is on it.
func (s *Store) AddRoomList(name, kind, mask, by string) error {
	_, err := s.db.Exec(`INSERT INTO room_lists (room, kind, mask, set_by, set_at) VALUES (?, ?, ?, ?, ?)`, name, kind, mask, by, now())
	if isUnique(err) {
		return ErrExists
	}
	return err
}

// DelRoomList takes a mask off a room's list; ErrNotFound if it is not on it.
func (s *Store) DelRoomList(name, kind, mask string) error {
	return s.exec1(`DELETE FROM room_lists WHERE room = ? AND kind = ? AND mask = ?`, name, kind, mask)
}

// RegisterRoom registers a room to its founder, who is put on its access
// list with flags; ErrExists if it is registered already.
func (s *Store) RegisterRoom(name string, founder int64, flags, by string) error {
	t := now()
	_, err := s.db.Exec(`INSERT INTO room_reg (name, founder_id, registered, last_used) VALUES (?, ?, ?, ?)`, name, founder, t, t)
	if isUnique(err) {
		return ErrExists
	}
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM room_access WHERE room = ?`, name) // left over
	_, _ = s.db.Exec(`DELETE FROM room_lists WHERE room = ?`, name)
	return s.SetRoomAccess(name, founder, flags, by)
}

// DropRoom unregisters a room, and empties its access list.
func (s *Store) DropRoom(name string) error {
	if err := s.exec1(`DELETE FROM room_reg WHERE name = ?`, name); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM room_access WHERE room = ?`, name); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM room_lists WHERE room = ?`, name)
	return err
}

// SetRoomFounder gives a registered room to another user.
func (s *Store) SetRoomFounder(name string, founder int64) error {
	_, err := s.db.Exec(`UPDATE room_reg SET founder_id = ? WHERE name = ?`, founder, name)
	return err
}

// SetRoomSettings sets a registered room's settings (words).
func (s *Store) SetRoomSettings(name, settings string) error {
	_, err := s.db.Exec(`UPDATE room_reg SET settings = ? WHERE name = ?`, settings, name)
	return err
}

// SetRoomText sets one of a registered room's texts: "descr", "url" or
// "entrymsg".
func (s *Store) SetRoomText(name, field, text string) error {
	switch field {
	case "descr", "url", "entrymsg":
	default:
		return ErrNotFound
	}
	_, err := s.db.Exec(`UPDATE room_reg SET `+field+` = ? WHERE name = ?`, text, name)
	return err
}

// SetRoomRegTopic sets a registered room's topic.
func (s *Store) SetRoomRegTopic(name, topic, by string) error {
	_, err := s.db.Exec(`UPDATE room_reg SET topic = ?, topic_by = ?, topic_at = ? WHERE name = ?`, topic, by, now(), name)
	return err
}

// RoomUsed notes that a registered room was used now.
func (s *Store) RoomUsed(name string) error {
	_, err := s.db.Exec(`UPDATE room_reg SET last_used = ? WHERE name = ?`, now(), name)
	return err
}

const roomAccessCols = `a.room, a.user_id, COALESCE(u.name, ''), a.flags, a.set_by, a.set_at`

func (s *Store) roomAccessList(where string, args ...any) ([]*RoomAccess, error) {
	rows, err := s.db.Query(`SELECT `+roomAccessCols+` FROM room_access a LEFT JOIN users u ON u.id = a.user_id WHERE `+where+` ORDER BY a.room, a.set_at, a.user_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RoomAccess
	for rows.Next() {
		var a RoomAccess
		var at int64
		if err := rows.Scan(&a.Room, &a.UserID, &a.Name, &a.Flags, &a.SetBy, &at); err != nil {
			return nil, err
		}
		a.SetAt = unix(at)
		out = append(out, &a)
	}
	return out, rows.Err()
}

// RoomAccessList is a registered room's access list, oldest first.
func (s *Store) RoomAccessList(name string) ([]*RoomAccess, error) {
	return s.roomAccessList(`a.room = ?`, name)
}

// RoomsOf are the access entries a user has, in every room.
func (s *Store) RoomsOf(user int64) ([]*RoomAccess, error) {
	return s.roomAccessList(`a.user_id = ?`, user)
}

// RoomFlags are a user's flags in a room ("": none).
func (s *Store) RoomFlags(name string, user int64) (string, error) {
	var flags string
	err := s.db.QueryRow(`SELECT flags FROM room_access WHERE room = ? AND user_id = ?`, name, user).Scan(&flags)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return flags, err
}

// SetRoomAccess sets a user's flags in a room ("": off the list).
func (s *Store) SetRoomAccess(name string, user int64, flags, by string) error {
	if flags == "" {
		_, err := s.db.Exec(`DELETE FROM room_access WHERE room = ? AND user_id = ?`, name, user)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO room_access (room, user_id, flags, set_by, set_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (room, user_id) DO UPDATE SET flags = excluded.flags, set_by = excluded.set_by, set_at = excluded.set_at`,
		name, user, flags, by, now())
	return err
}

// roomsGone takes a deleted user off the access lists.  A room they
// founded goes to the oldest entry with F, else with f, else the oldest
// entry; with none left, it is dropped.
func (s *Store) roomsGone(user int64) error {
	if _, err := s.db.Exec(`DELETE FROM room_access WHERE user_id = ?`, user); err != nil {
		return err
	}
	var rooms []string
	rows, err := s.db.Query(`SELECT name FROM room_reg WHERE founder_id = ?`, user)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		rooms = append(rooms, n)
	}
	rows.Close()
	for _, name := range rooms {
		list, err := s.RoomAccessList(name)
		if err != nil {
			return err
		}
		var heir *RoomAccess
		for _, want := range []string{"F", "f", ""} {
			for _, a := range list {
				if a.Name != "" && strings.Contains(a.Flags, want) {
					heir = a
					break
				}
			}
			if heir != nil {
				break
			}
		}
		if heir == nil {
			if err := s.DropRoom(name); err != nil {
				return err
			}
			continue
		}
		if err := s.SetRoomFounder(name, heir.UserID); err != nil {
			return err
		}
	}
	return nil
}
