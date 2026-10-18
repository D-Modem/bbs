package store

import (
	"database/sql"
	"errors"
	"time"
)

// SSHKey is a public key a user logs in with by SSH.
type SSHKey struct {
	ID          int64
	UserID      int64
	User        string // the user's name (read)
	Type        string // ssh-ed25519, ssh-rsa, ...
	Fingerprint string // SHA256:...
	Key         string // the authorized_keys line: type, base64, comment
	Comment     string
	Created     time.Time
	LastUsed    time.Time
}

// ErrKeyTaken: the key is someone's already (a key logs in one user).
var ErrKeyTaken = errors.New("that key is already in use")

// AddSSHKey records a user's key.
func (s *Store) AddSSHKey(k *SSHKey) (int64, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ssh_keys WHERE fingerprint = ?`, k.Fingerprint).Scan(&n); err != nil {
		return 0, err
	}
	if n > 0 {
		return 0, ErrKeyTaken
	}
	id, err := s.db.insertID(`INSERT INTO ssh_keys (user_id, type, fingerprint, "key", comment, created)
		VALUES (?, ?, ?, ?, ?, ?)`, k.UserID, k.Type, k.Fingerprint, k.Key, k.Comment, now())
	if err != nil {
		return 0, err
	}
	return id, nil
}

const sshKeyCols = `k.id, k.user_id, u.name, k.type, k.fingerprint, k."key", k.comment, k.created, k.last_used`

// scanSSHKey is the key in a row of sshKeyCols; ErrNotFound if there is
// none.
func scanSSHKey(row interface{ Scan(...any) error }) (*SSHKey, error) {
	var k SSHKey
	var created, last int64
	err := row.Scan(&k.ID, &k.UserID, &k.User, &k.Type, &k.Fingerprint, &k.Key, &k.Comment, &created, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	k.Created, k.LastUsed = unix(created), unix(last)
	return &k, err
}

// SSHKeyByFingerprint finds a key.
func (s *Store) SSHKeyByFingerprint(fp string) (*SSHKey, error) {
	return scanSSHKey(s.db.QueryRow(`SELECT `+sshKeyCols+` FROM ssh_keys k JOIN users u ON u.id = k.user_id
		WHERE k.fingerprint = ?`, fp))
}

// TouchSSHKey notes a key's use.
func (s *Store) TouchSSHKey(id int64) error {
	_, err := s.db.Exec(`UPDATE ssh_keys SET last_used = ? WHERE id = ?`, now(), id)
	return err
}

// SSHKeys lists the keys (of a user, or all if user is 0).
func (s *Store) SSHKeys(user int64) ([]*SSHKey, error) {
	q := `SELECT ` + sshKeyCols + ` FROM ssh_keys k JOIN users u ON u.id = k.user_id`
	args := []any{}
	if user != 0 {
		q += ` WHERE k.user_id = ?`
		args = append(args, user)
	}
	rows, err := s.db.Query(q+` ORDER BY k.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SSHKey
	for rows.Next() {
		k, err := scanSSHKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RemoveSSHKey deletes a key (of a user, or anyone's if user is 0).
func (s *Store) RemoveSSHKey(id, user int64) error {
	if user == 0 {
		return s.exec1(`DELETE FROM ssh_keys WHERE id = ?`, id)
	}
	return s.exec1(`DELETE FROM ssh_keys WHERE id = ? AND user_id = ?`, id, user)
}
