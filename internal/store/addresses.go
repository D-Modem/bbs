package store

import (
	"strings"
	"time"
)

// Address is an entry in a user's own address book: a name they give an
// address, kept for every front end that has an address book.
type Address struct {
	Name  string
	Addr  string // as the user gave it (a member's name, an e-mail address, ...)
	Added time.Time
}

// Addresses is a user's address book, by name.
func (s *Store) Addresses(user int64) ([]Address, error) {
	rows, err := s.db.Query(`SELECT name, addr, added FROM address_book WHERE user_id = ? ORDER BY name`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Address
	for rows.Next() {
		var a Address
		var at int64
		if err := rows.Scan(&a.Name, &a.Addr, &at); err != nil {
			return nil, err
		}
		a.Added = unix(at)
		out = append(out, a)
	}
	return out, rows.Err()
}

// PutAddress adds a name to a user's address book, or gives a name already
// there (in any case) the new address.
func (s *Store) PutAddress(user int64, name, addr string) error {
	name, addr = strings.TrimSpace(name), strings.TrimSpace(addr)
	if name == "" || addr == "" {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO address_book (user_id, name, addr, added) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, name) DO UPDATE SET addr = excluded.addr`, user, name, addr, now())
	return err
}

// RemoveAddress takes a name out of a user's address book.
func (s *Store) RemoveAddress(user int64, name string) error {
	_, err := s.db.Exec(`DELETE FROM address_book WHERE user_id = ? AND name = ?`, user, strings.TrimSpace(name))
	return err
}
