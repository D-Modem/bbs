package store

import (
	"crypto/md5"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

// OSCAR (AIM and ICQ) logins: a BUCP login proves the password by an MD5
// digest, which a bcrypt hash cannot check.  So each account gets a fixed
// auth key and the two digests clients send, kept whenever the password is
// known in the clear (made, changed, or checked at a login), and "bound"
// to the password hash they were made with: a password changed some other
// way leaves them stale, and they are not used.

// oscarMagic ends every BUCP digest.
const oscarMagic = "AOL Instant Messenger (SM)"

// OSCARWeak is the digest of AIM clients before 5.x: MD5(key, password,
// magic).
func OSCARWeak(key, pw string) []byte {
	h := md5.New()
	io.WriteString(h, key)
	io.WriteString(h, pw)
	io.WriteString(h, oscarMagic)
	return h.Sum(nil)
}

// OSCARStrong is the digest of clients that say so (TLV 0x4C): MD5(key,
// MD5(password), magic).
func OSCARStrong(key, pw string) []byte {
	p := md5.Sum([]byte(pw))
	h := md5.New()
	io.WriteString(h, key)
	h.Write(p[:])
	io.WriteString(h, oscarMagic)
	return h.Sum(nil)
}

// SetKeepOSCAR says whether to keep the digests: only with an OSCAR
// server, as they would let anyone who read the database log in by AIM.
func (s *Store) SetKeepOSCAR(on bool) { s.keepOSCAR.Store(on) }

// OSCARKeys are an account's: the auth key it is challenged with and the
// digests a BUCP login must match.
type OSCARKeys struct {
	AuthKey      string
	Weak, Strong []byte
}

// OSCARKeys are the account's, if any are current (ok false: none, or the
// password changed since).
func (s *Store) OSCARKeys(id int64) (k OSCARKeys, ok bool, err error) {
	var bound, hash string
	err = s.db.QueryRow(`SELECT k.auth_key, k.weak, k.strong, k.bound, u.password FROM oscar_keys k
		JOIN users u ON u.id = k.user_id WHERE k.user_id = ?`, id).Scan(&k.AuthKey, &k.Weak, &k.Strong, &bound, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return OSCARKeys{}, false, nil
	}
	if err != nil {
		return OSCARKeys{}, false, err
	}
	return k, bound == hash, nil
}

// OSCARAuthKey is the key a BUCP challenge gives the account: its kept
// one, or a new one kept for it (with no digests yet).
func (s *Store) OSCARAuthKey(id int64) (string, error) {
	var key string
	err := s.db.QueryRow(`SELECT auth_key FROM oscar_keys WHERE user_id = ?`, id).Scan(&key)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	key = newAuthKey()
	_, err = s.db.Exec(`INSERT OR IGNORE INTO oscar_keys (user_id, auth_key, weak, strong, bound) VALUES (?, ?, ?, ?, ?)`,
		id, key, []byte{}, []byte{}, "")
	if err != nil {
		return "", err
	}
	// another login may have made one meanwhile
	err = s.db.QueryRow(`SELECT auth_key FROM oscar_keys WHERE user_id = ?`, id).Scan(&key)
	return key, err
}

// newAuthKey is a random auth key: ten digits, as AOL's were.
func newAuthKey() string {
	b := make([]byte, 5)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// KeepOSCARKeys makes the account's digests of pw (its password, just
// checked or set), bound to its password hash now, if the store keeps
// them (SetKeepOSCAR).  Nothing is written if they are current already.
func (s *Store) KeepOSCARKeys(id int64, pw string) error {
	if !s.keepOSCAR.Load() || pw == "" {
		return nil
	}
	var hash string
	if err := s.db.QueryRow(`SELECT password FROM users WHERE id = ?`, id).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	key, err := s.OSCARAuthKey(id)
	if err != nil {
		return err
	}
	weak, strong := OSCARWeak(key, pw), OSCARStrong(key, pw)
	if k, ok, err := s.OSCARKeys(id); err == nil && ok && string(k.Weak) == string(weak) {
		return nil
	}
	_, err = s.db.Exec(`UPDATE oscar_keys SET weak = ?, strong = ?, bound = ? WHERE user_id = ?`,
		weak, strong, hash, id)
	return err
}

// UserByScreenName finds an account by an AIM screen name: its name in any
// case, with or without its spaces ("carolann" is Carol Ann).
func (s *Store) UserByScreenName(sn string) (*User, error) {
	u, err := s.UserByName(sn)
	if !errors.Is(err, ErrNotFound) {
		return u, err
	}
	norm := strings.ToLower(strings.ReplaceAll(sn, " ", ""))
	if norm == "" {
		return nil, ErrNotFound
	}
	u, _, err = scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE LOWER(REPLACE(name, ' ', '')) = ?
		ORDER BY id LIMIT 1`, norm))
	return u, err
}

// FeedbagItem is one of a user's server-stored buddy list (SSI) items: a
// group, a buddy, a permit or deny entry, a preference.  Attrs are its
// TLVs, as the client sent them.
type FeedbagItem struct {
	Name    string
	GroupID uint16
	ItemID  uint16
	ClassID uint16
	Attrs   []byte
}

// Feedbag is a user's items, in order of group and item.
func (s *Store) Feedbag(user int64) ([]FeedbagItem, error) {
	rows, err := s.db.Query(`SELECT name, group_id, item_id, class_id, attrs FROM feedbag WHERE user_id = ?
		ORDER BY group_id, item_id`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FeedbagItem
	for rows.Next() {
		var it FeedbagItem
		var g, i, c int64
		if err := rows.Scan(&it.Name, &g, &i, &c, &it.Attrs); err != nil {
			return nil, err
		}
		it.GroupID, it.ItemID, it.ClassID = uint16(g), uint16(i), uint16(c)
		out = append(out, it)
	}
	return out, rows.Err()
}

// PutFeedbag adds items or replaces them (by group and item id).
func (s *Store) PutFeedbag(user int64, items []FeedbagItem) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	for _, it := range items {
		attrs := it.Attrs
		if attrs == nil {
			attrs = []byte{}
		}
		if _, err := t.Exec(`DELETE FROM feedbag WHERE user_id = ? AND group_id = ? AND item_id = ?`,
			user, int64(it.GroupID), int64(it.ItemID)); err != nil {
			return err
		}
		if _, err := t.Exec(`INSERT INTO feedbag (user_id, group_id, item_id, class_id, name, attrs) VALUES (?, ?, ?, ?, ?, ?)`,
			user, int64(it.GroupID), int64(it.ItemID), int64(it.ClassID), it.Name, attrs); err != nil {
			return err
		}
	}
	return t.Commit()
}

// DeleteFeedbag removes items (by group and item id).
func (s *Store) DeleteFeedbag(user int64, items []FeedbagItem) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	for _, it := range items {
		if _, err := t.Exec(`DELETE FROM feedbag WHERE user_id = ? AND group_id = ? AND item_id = ?`,
			user, int64(it.GroupID), int64(it.ItemID)); err != nil {
			return err
		}
	}
	return t.Commit()
}
