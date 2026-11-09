package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A private mail is kept once, in mail; each user who has it has an entry
// in mailbox: its recipient, its sender (their Sent copy), or its writer
// (a draft), each with their own read and starred flags and their own
// trash.  The folders are views over a user's entries, chosen by those
// flags; an entry's place in each folder it is in has a number (mail_uids:
// IMAP's UIDs), new each time it comes into that folder, so that numbers
// only grow in a folder.

// The folders.
const (
	FolderInbox   = iota // mail to the user
	FolderSent           // mail from the user
	FolderDrafts         // mail the user has not sent
	FolderStarred        // mail the user starred
	FolderUnread         // mail to the user not yet read
	FolderAll            // every mail, but drafts and the trash
	FolderTrash          // mail the user deleted, kept a while
	NumFolders
)

// FolderNames are the folders' names, as IMAP lists them.
var FolderNames = [NumFolders]string{"INBOX", "Sent", "Drafts", "Starred", "Unread", "All Mail", "Trash"}

// FolderByName is the folder of a name (in any case), or -1.
func FolderByName(name string) int {
	for i, n := range FolderNames {
		if strings.EqualFold(n, name) {
			return i
		}
	}
	if strings.EqualFold(name, "Inbox") {
		return FolderInbox
	}
	return -1
}

// The roles of an entry: what the mail is to its user.
const (
	RoleReceived = 0
	RoleSent     = 1
	RoleDraft    = 2
)

// inFolders is which folders an entry of that state is in.
func inFolders(role int, read, starred bool, deleted int64) [NumFolders]bool {
	var in [NumFolders]bool
	if deleted > 0 {
		in[FolderTrash] = true
		return in
	}
	in[FolderInbox] = role == RoleReceived
	in[FolderSent] = role == RoleSent
	in[FolderDrafts] = role == RoleDraft
	in[FolderStarred] = starred
	in[FolderUnread] = role == RoleReceived && !read
	in[FolderAll] = role != RoleDraft
	return in
}

// querier is the database or a transaction.
type querier interface {
	Exec(q string, args ...any) (sql.Result, error)
	Query(q string, args ...any) (*sql.Rows, error)
	QueryRow(q string, args ...any) *sql.Row
	insertID(q string, args ...any) (int64, error)
}

// fillMailboxes gives each mail of a database from before the mailboxes
// an entry for its recipient, and that entry its places in the folders.
func fillMailboxes(x *db) error {
	if _, err := x.Exec(`INSERT INTO mailbox (user_id, mail_id, role, "read", created)
		SELECT to_id, id, 0, "read", created FROM mail ORDER BY id`); err != nil {
		return err
	}
	rows, err := x.Query(`SELECT id, user_id, role, "read", starred, deleted FROM mailbox ORDER BY id`)
	if err != nil {
		return err
	}
	type entry struct {
		id, user, deleted int64
		role              int
		read, starred     bool
	}
	var es []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.user, &e.role, &e.read, &e.starred, &e.deleted); err != nil {
			rows.Close()
			return err
		}
		es = append(es, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, e := range es {
		in := inFolders(e.role, e.read, e.starred, e.deleted)
		for f, ok := range in {
			if ok {
				if _, err := x.Exec(`INSERT INTO mail_uids (user_id, folder, box_id) VALUES (?, ?, ?)`, e.user, f, e.id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// syncBox puts an entry in the folders its state says, and takes it out
// of the others.
func syncBox(q querier, box int64) error {
	var user, deleted int64
	var role int
	var read, starred bool
	err := q.QueryRow(`SELECT user_id, role, "read", starred, deleted FROM mailbox WHERE id = ?`, box).
		Scan(&user, &role, &read, &starred, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	want := inFolders(role, read, starred, deleted)
	rows, err := q.Query(`SELECT folder FROM mail_uids WHERE box_id = ?`, box)
	if err != nil {
		return err
	}
	var have [NumFolders]bool
	for rows.Next() {
		var f int
		if err := rows.Scan(&f); err != nil {
			rows.Close()
			return err
		}
		if f >= 0 && f < NumFolders {
			have[f] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for f := range want {
		switch {
		case want[f] && !have[f]:
			if _, err := q.Exec(`INSERT OR IGNORE INTO mail_uids (user_id, folder, box_id) VALUES (?, ?, ?)`, user, f, box); err != nil {
				return err
			}
		case !want[f] && have[f]:
			if _, err := q.Exec(`DELETE FROM mail_uids WHERE box_id = ? AND folder = ?`, box, f); err != nil {
				return err
			}
		}
	}
	return nil
}

// addBox gives user an entry for a mail.
func addBox(q querier, user, mail int64, role int, read, starred bool, deleted int64) (int64, error) {
	id, err := q.insertID(`INSERT INTO mailbox (user_id, mail_id, role, "read", starred, deleted, created)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, user, mail, role, read, starred, deleted, now())
	if err != nil {
		return 0, err
	}
	return id, syncBox(q, id)
}

// gcMail deletes a mail no one has any more, and that is not waiting to
// be sent.
func gcMail(q querier, mail int64) error {
	_, err := q.Exec(`DELETE FROM mail WHERE id = ? AND NOT EXISTS (SELECT 1 FROM mailbox WHERE mail_id = ?)
		AND NOT EXISTS (SELECT 1 FROM mail_queue WHERE mail_id = ?)`, mail, mail, mail)
	return err
}

// Outbound is a mail waiting to go to an address elsewhere.
type Outbound struct {
	ID        int64
	MailID    int64
	UserID    int64  // who sent it (0: the system, a bounce)
	Sender    string // the envelope's sender ("": a bounce, <>)
	Rcpt      string
	Attempts  int
	NextTry   time.Time
	LastError string
	Created   time.Time
}

// Delivery is what a new mail is given to.
type Delivery struct {
	To    []int64 // users here it is for
	Sent  bool    // the sender (m.FromID) keeps a copy in Sent
	Draft bool    // it is the sender's draft: not sent
	Read  bool    // the recipients' entries are read already
	Queue []*Outbound
}

// DeliverMail keeps a new mail and gives it to its users, and puts it in
// the queue for the addresses elsewhere, all at once: its id.  The mail's
// ToID is its first recipient here, or else its sender.
func (s *Store) DeliverMail(m *Message, d Delivery) (int64, error) {
	if m.ToID <= 0 {
		switch {
		case len(d.To) > 0:
			m.ToID = d.To[0]
		case m.FromID > 0:
			m.ToID = m.FromID
		default:
			return 0, errors.New("mail for no one")
		}
	}
	if m.Created.IsZero() {
		m.Created = time.Now()
	}
	t, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer t.Rollback()
	id, err := t.insertID(`INSERT INTO mail (from_id, from_name, to_id, to_name, subject, body, reply_to, created,
		ftn_msgid, raw, message_id, from_addr, to_addr)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, m.FromID, m.From, m.ToID, m.To, m.Subject, m.Body, m.ReplyTo,
		m.Created.Unix(), nullString(m.FTNMsgID), m.Raw, nullString(m.MessageID), m.FromAddr, m.ToAddr)
	if err != nil {
		return 0, err
	}
	if d.Draft {
		if _, err := addBox(t, m.FromID, id, RoleDraft, true, false, 0); err != nil {
			return 0, err
		}
	} else {
		seen := map[int64]bool{}
		for _, u := range d.To {
			if u <= 0 || seen[u] {
				continue
			}
			seen[u] = true
			if _, err := addBox(t, u, id, RoleReceived, d.Read, false, 0); err != nil {
				return 0, err
			}
		}
		if d.Sent && m.FromID > 0 {
			if _, err := addBox(t, m.FromID, id, RoleSent, true, false, 0); err != nil {
				return 0, err
			}
		}
	}
	for _, o := range d.Queue {
		if _, err := t.Exec(`INSERT INTO mail_queue (mail_id, user_id, sender, rcpt, next_try, created)
			VALUES (?, ?, ?, ?, ?, ?)`, id, o.UserID, o.Sender, o.Rcpt, now(), now()); err != nil {
			return 0, err
		}
	}
	if err := t.Commit(); err != nil {
		return 0, err
	}
	m.ID = id
	return id, nil
}

// SendMail delivers a private message to its recipient (ToID), the sender
// (FromID, if a user here) keeping a copy in Sent.
func (s *Store) SendMail(m *Message) (int64, error) {
	if m.ToID <= 0 {
		return 0, errors.New("mail for no one")
	}
	return s.DeliverMail(m, Delivery{To: []int64{m.ToID}, Sent: m.FromID > 0})
}

// mailCols are a mailbox entry (b) and its mail (m).
const mailCols = `b.id, b."read", b.starred, m.id, m.from_id, m.from_name, m.to_id, m.to_name, m.subject, m.body,
	m.reply_to, m.created, COALESCE(m.ftn_msgid, ''), COALESCE(m.message_id, ''), m.from_addr, m.to_addr`

// scanMail is the mail in a row of mailCols; ErrNotFound if there is none.
func scanMail(row interface{ Scan(...any) error }, extra ...any) (*Message, error) {
	var m Message
	var created int64
	err := row.Scan(append([]any{&m.Box, &m.Read, &m.Starred, &m.ID, &m.FromID, &m.From, &m.ToID, &m.To,
		&m.Subject, &m.Body, &m.ReplyTo, &created, &m.FTNMsgID, &m.MessageID, &m.FromAddr, &m.ToAddr}, extra...)...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err // nothing half scanned
	}
	m.Created = unix(created)
	return &m, nil
}

// mailList is the mail a query of mailCols finds.
func (s *Store) mailList(q string, args ...any) ([]*Message, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m, err := scanMail(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Inbox lists a user's mail in the Inbox, oldest first.
func (s *Store) Inbox(user int64) ([]*Message, error) {
	return s.Folder(user, FolderInbox)
}

// Folder lists a user's mail in a folder, oldest first.
func (s *Store) Folder(user int64, folder int) ([]*Message, error) {
	return s.mailList(`SELECT `+mailCols+` FROM mail_uids u JOIN mailbox b ON b.id = u.box_id JOIN mail m ON m.id = b.mail_id
		WHERE u.user_id = ? AND u.folder = ? ORDER BY m.created, m.id`, user, folder)
}

// Mail finds a mail of user's by id, one they received and have not
// deleted: no one reads another's mail.
func (s *Store) Mail(user, id int64) (*Message, error) {
	return scanMail(s.db.QueryRow(`SELECT `+mailCols+` FROM mailbox b JOIN mail m ON m.id = b.mail_id
		WHERE b.user_id = ? AND b.mail_id = ? AND b.role = ? AND b.deleted = 0 ORDER BY b.id LIMIT 1`, user, id, RoleReceived))
}

// boxesOf is the user's received entries for a mail, not in the trash.
func boxesOf(q querier, user, mail int64) ([]int64, error) {
	rows, err := q.Query(`SELECT id FROM mailbox WHERE user_id = ? AND mail_id = ? AND role = ? AND deleted = 0`,
		user, mail, RoleReceived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// onReceived changes the user's received entries for a mail, in one go.
func (s *Store) onReceived(user, mail int64, stmt string, args ...any) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	boxes, err := boxesOf(t, user, mail)
	if err != nil {
		return err
	}
	for _, b := range boxes {
		if _, err := t.Exec(stmt, append(args, b)...); err != nil {
			return err
		}
		if err := syncBox(t, b); err != nil {
			return err
		}
	}
	return t.Commit()
}

// MarkMailRead marks a mail of user's read.
func (s *Store) MarkMailRead(user, id int64) error {
	return s.onReceived(user, id, `UPDATE mailbox SET "read" = TRUE WHERE id = ?`)
}

// MarkMailUnread marks a mail of user's not yet read.
func (s *Store) MarkMailUnread(user, id int64) error {
	return s.onReceived(user, id, `UPDATE mailbox SET "read" = FALSE WHERE id = ?`)
}

// DeleteMail puts a mail of user's in their trash.
func (s *Store) DeleteMail(user, id int64) error {
	return s.onReceived(user, id, `UPDATE mailbox SET deleted = ? WHERE id = ?`, now())
}

// StarMail stars, or unstars, a mail the user received.
func (s *Store) StarMail(user, id int64, on bool) error {
	return s.onReceived(user, id, `UPDATE mailbox SET starred = ? WHERE id = ?`, on)
}

// UnreadMail counts a user's unread mail.
func (s *Store) UnreadMail(user int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM mailbox WHERE user_id = ? AND role = ? AND "read" = FALSE AND deleted = 0`,
		user, RoleReceived).Scan(&n)
	return n, err
}

// BoxEntry is a mail's place in a folder: what IMAP and POP3 list.
type BoxEntry struct {
	UID     int64 // its number in the folder
	Box     int64 // the mailbox entry
	MailID  int64
	Role    int
	Read    bool
	Starred bool
	Deleted time.Time // when it was put in the trash (zero: it is not)
	Flags   string    // IMAP's other flags, space-separated
	Created time.Time // when the mail was written or came
	Size    int64     // its raw form's (0: written here, it has none)
}

// FolderEntries lists a folder of the user's, by its numbers.
func (s *Store) FolderEntries(user int64, folder int) ([]*BoxEntry, error) {
	rows, err := s.db.Query(`SELECT u.id, b.id, b.mail_id, b.role, b."read", b.starred, b.deleted, b.flags, m.created,
		COALESCE(LENGTH(m.raw), 0)
		FROM mail_uids u JOIN mailbox b ON b.id = u.box_id JOIN mail m ON m.id = b.mail_id
		WHERE u.user_id = ? AND u.folder = ? ORDER BY u.id`, user, folder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BoxEntry
	for rows.Next() {
		var e BoxEntry
		var deleted, created int64
		if err := rows.Scan(&e.UID, &e.Box, &e.MailID, &e.Role, &e.Read, &e.Starred, &deleted, &e.Flags, &created, &e.Size); err != nil {
			return nil, err
		}
		e.Deleted, e.Created = unix(deleted), unix(created)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// MailUIDNext is the least number a mail coming into any folder now could
// have.
func (s *Store) MailUIDNext() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(id), 0) + 1 FROM mail_uids`).Scan(&n)
	return n, err
}

// BoxUID is an entry's number in a folder; ErrNotFound if it is not there.
func (s *Store) BoxUID(box int64, folder int) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT id FROM mail_uids WHERE box_id = ? AND folder = ?`, box, folder).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return n, err
}

// rawScan is a raw column scanned, whatever the driver gives.
type rawScan []byte

func (r *rawScan) Scan(v any) error {
	switch b := v.(type) {
	case nil:
		*r = nil
	case []byte:
		*r = append([]byte(nil), b...)
	case string:
		*r = []byte(b)
	default:
		return fmt.Errorf("raw: %T", v)
	}
	return nil
}

// BoxMail is the mail of a user's entry, its raw form too (nil if written
// here), and the entry.
func (s *Store) BoxMail(user, box int64) (*Message, *BoxEntry, error) {
	var e BoxEntry
	var deleted int64
	var raw rawScan
	m, err := scanMail(s.db.QueryRow(`SELECT `+mailCols+`, m.raw, b.role, b.deleted, b.flags FROM mailbox b JOIN mail m ON m.id = b.mail_id
		WHERE b.id = ? AND b.user_id = ?`, box, user), &raw, &e.Role, &deleted, &e.Flags)
	if err != nil {
		return nil, nil, err
	}
	m.Raw = raw
	e.Box, e.MailID, e.Read, e.Starred, e.Deleted, e.Created, e.Size = m.Box, m.ID, m.Read, m.Starred, unix(deleted), m.Created, int64(len(raw))
	return m, &e, nil
}

// changeBox runs stmt (its last argument the entry) on a user's entry,
// and puts the entry where it now belongs.
func (s *Store) changeBox(user, box int64, stmt string, args ...any) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	res, err := t.Exec(stmt+` AND user_id = ?`, append(args, box, user)...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// MySQL counts rows changed, not found: is it there?
		var one int
		if err := t.QueryRow(`SELECT 1 FROM mailbox WHERE id = ? AND user_id = ?`, box, user).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
	}
	if err := syncBox(t, box); err != nil {
		return err
	}
	return t.Commit()
}

// SetBoxFlags sets an entry's read and starred flags, and its others.
func (s *Store) SetBoxFlags(user, box int64, read, starred bool, flags string) error {
	return s.changeBox(user, box, `UPDATE mailbox SET "read" = ?, starred = ?, flags = ? WHERE id = ?`, read, starred, flags)
}

// TrashBox puts an entry in the trash.
func (s *Store) TrashBox(user, box int64) error {
	return s.changeBox(user, box, `UPDATE mailbox SET deleted = ? WHERE deleted = 0 AND id = ?`, now())
}

// RestoreBox takes an entry out of the trash.
func (s *Store) RestoreBox(user, box int64) error {
	return s.changeBox(user, box, `UPDATE mailbox SET deleted = 0 WHERE id = ?`)
}

// PurgeBox deletes an entry for good (and its mail, if no one else has it).
func (s *Store) PurgeBox(user, box int64) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	var mail int64
	err = t.QueryRow(`SELECT mail_id FROM mailbox WHERE id = ? AND user_id = ?`, box, user).Scan(&mail)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := t.Exec(`DELETE FROM mail_uids WHERE box_id = ?`, box); err != nil {
		return err
	}
	if _, err := t.Exec(`DELETE FROM mailbox WHERE id = ?`, box); err != nil {
		return err
	}
	if err := gcMail(t, mail); err != nil {
		return err
	}
	return t.Commit()
}

// AppendMail keeps a mail a user put in a folder of theirs (IMAP's
// APPEND): as received, sent or a draft, in the trash if deleted.  The
// new entry.
func (s *Store) AppendMail(user int64, role int, m *Message, read, starred, deleted bool, flags string) (int64, error) {
	if m.ToID <= 0 {
		m.ToID = user
	}
	if m.Created.IsZero() {
		m.Created = time.Now()
	}
	t, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer t.Rollback()
	id, err := t.insertID(`INSERT INTO mail (from_id, from_name, to_id, to_name, subject, body, reply_to, created,
		raw, message_id, from_addr, to_addr)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, m.FromID, m.From, m.ToID, m.To, m.Subject, m.Body, m.ReplyTo,
		m.Created.Unix(), m.Raw, nullString(m.MessageID), m.FromAddr, m.ToAddr)
	if err != nil {
		return 0, err
	}
	var del int64
	if deleted {
		del = now()
	}
	box, err := t.insertID(`INSERT INTO mailbox (user_id, mail_id, role, "read", starred, deleted, flags, created)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, user, id, role, read, starred, del, flags, now())
	if err != nil {
		return 0, err
	}
	if err := syncBox(t, box); err != nil {
		return 0, err
	}
	if err := t.Commit(); err != nil {
		return 0, err
	}
	m.ID, m.Box = id, box
	return box, nil
}

// SentByMessageID is the user's Sent entry of a mail by its Message-ID
// (one sent here and put in Sent again by their mail program).
func (s *Store) SentByMessageID(user int64, id string) (int64, error) {
	var box int64
	err := s.db.QueryRow(`SELECT b.id FROM mailbox b JOIN mail m ON m.id = b.mail_id
		WHERE b.user_id = ? AND b.role = ? AND m.message_id = ? ORDER BY b.id LIMIT 1`, user, RoleSent, id).Scan(&box)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return box, err
}

// PurgeTrash deletes for good what has been in the trash since before t:
// how many entries.
func (s *Store) PurgeTrash(before time.Time) (int, error) {
	rows, err := s.db.Query(`SELECT id, user_id FROM mailbox WHERE deleted > 0 AND deleted < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	type ub struct{ box, user int64 }
	var all []ub
	for rows.Next() {
		var x ub
		if err := rows.Scan(&x.box, &x.user); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, x := range all {
		if err := s.PurgeBox(x.user, x.box); err == nil {
			n++
		} else if !errors.Is(err, ErrNotFound) {
			return n, err
		}
	}
	return n, nil
}

// MailRaw is a mail's raw form (nil: written here), by its id alone: for
// the queue.
func (s *Store) MailRaw(id int64) ([]byte, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT raw FROM mail WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return raw, err
}

const queueCols = `id, mail_id, user_id, sender, rcpt, attempts, next_try, last_error, created`

func scanOutbound(row interface{ Scan(...any) error }) (*Outbound, error) {
	var o Outbound
	var next, created int64
	if err := row.Scan(&o.ID, &o.MailID, &o.UserID, &o.Sender, &o.Rcpt, &o.Attempts, &next, &o.LastError, &created); err != nil {
		return nil, err
	}
	o.NextTry, o.Created = unix(next), unix(created)
	return &o, nil
}

// MailQueue lists the queue: those due by t (all, if t is zero), soonest
// first, at most limit (0: all).
func (s *Store) MailQueue(t time.Time, limit int) ([]*Outbound, error) {
	q := `SELECT ` + queueCols + ` FROM mail_queue`
	var args []any
	if !t.IsZero() {
		q += ` WHERE next_try <= ?`
		args = append(args, t.Unix())
	}
	q += ` ORDER BY next_try, id`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Outbound
	for rows.Next() {
		o, err := scanOutbound(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// QueueDone takes a mail off the queue: sent, or given up on.
func (s *Store) QueueDone(id int64) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	var mail int64
	if err := t.QueryRow(`SELECT mail_id FROM mail_queue WHERE id = ?`, id).Scan(&mail); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := t.Exec(`DELETE FROM mail_queue WHERE id = ?`, id); err != nil {
		return err
	}
	if err := gcMail(t, mail); err != nil {
		return err
	}
	return t.Commit()
}

// QueueRetry puts a mail off till next, saying why.
func (s *Store) QueueRetry(id int64, next time.Time, why string) error {
	if len(why) > 500 {
		why = why[:500]
	}
	return s.exec1(`UPDATE mail_queue SET attempts = attempts + 1, next_try = ?, last_error = ? WHERE id = ?`,
		next.Unix(), why, id)
}
