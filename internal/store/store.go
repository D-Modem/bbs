// Package store keeps the BBS in a database, SQLite, PostgreSQL or MySQL
// (or MariaDB):
// users, message areas and their messages, private mail, what each user
// has read, files' records, FidoNet's and news' state, tokens and keys.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ErrNotFound: no such user, area, message or mail.
var ErrNotFound = errors.New("not found")

// ErrExists: the name is taken.
var ErrExists = errors.New("already exists")

// NoPassword is the stored password of an account that cannot log in by
// password: one an upstream menu (modemmenu) logs in.
const NoPassword = "!"

// BcryptCost is the cost of new password hashes.
var BcryptCost = 12

// User is an account.
type User struct {
	ID        int64
	Name      string
	Location  string
	Sysop     bool
	Locked    bool
	External  bool // logged in by an upstream menu, no password here
	Created   time.Time
	LastLogin time.Time
	Logins    int
	Posts     int
	Level     int    // access level, 0-255
	Flags     string // access flags, letters A-Z
	// today's (TimeDay, YYYYMMDD) time online in seconds, and downloads
	TimeDay, TimeUsed int
	DLDay, DLCount    int
	// the user's settings ("ansi=off width=80"): bbs.Prefs
	Prefs string
	// files downloaded ever, and uploaded (the files there now)
	Downloads, Uploads int
}

// Area is a message area.
type Area struct {
	ID          int64
	Name        string
	Description string
	SysopOnly   bool   // only sysops post
	ReadReq     string // the access requirement to read it ("": anyone)
	PostReq     string // and to post
	Newsgroup   string // its newsgroup ("": not one)
	EchoTag     string // its FidoNet echo ("": not one)
	Messages    int
	FirstID     int64
	LastID      int64
}

// Message is a public message in an area, or a private mail.
type Message struct {
	ID      int64
	AreaID  int64 // 0 for mail
	FromID  int64
	From    string
	ToID    int64 // mail only
	To      string
	Subject string
	Body    string
	ReplyTo int64
	Created time.Time
	Read    bool // mail only

	// a newsgroup's article: its Message-ID, where it came from ("" from
	// here, else the peer's name) and the article as it is sent
	MessageID string
	Origin    string
	Raw       []byte

	// FidoNet: its MSGID, and the packed message as it came from a link
	// (to pass on to the others)
	FTNMsgID string
	FTN      []byte
}

// Store is the database.
type Store struct {
	db   *db
	drop func() // a test's database, dropped at Close
	// a hash to check against for unknown users, so that they take as
	// long as known ones
	dummy []byte
}

// Open opens (and makes, if need be) the database at addr: a file's path
// (SQLite; "sqlite:" before it may say so), or a PostgreSQL or MySQL
// server's address: postgres://user:password@host/bbs,
// mysql://user:password@host:3306/bbs (or mariadb://).
func Open(addr string) (*Store, error) {
	var drop func()
	// (tests' plain paths only: "sqlite:" is SQLite whatever the test)
	if IsFile(addr) && !strings.HasPrefix(addr, "sqlite:") {
		test, d, err := testDatabase()
		if err != nil {
			return nil, err
		}
		if test != "" {
			addr, drop = test, d
		}
	}
	x, err := openDB(addr)
	if err != nil {
		return nil, err
	}
	if x == nil {
		path := strings.TrimPrefix(addr, "sqlite:")
		// passwords and private mail: for the BBS's user alone (SQLite
		// gives the -wal and -shm files the database's permissions)
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
		if err != nil {
			return nil, err
		}
		f.Close()
		dsn := "file:" + path + "?_pragma=busy_timeout(5000)" +
			"&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)" +
			"&_pragma=synchronous(NORMAL)"
		sdb, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		// one connection: SQLite takes one writer at a time anyway, and a
		// BBS is not busy enough to want more
		sdb.SetMaxOpenConns(1)
		x = &db{DB: sdb, d: dialects[SQLite]}
	}
	if err := x.Ping(); err != nil {
		x.Close()
		return nil, fmt.Errorf("%s: %w", Describe(addr), err)
	}
	if x.d.name == MySQL {
		if err := x.mysqlFlavor(); err != nil {
			x.Close()
			return nil, fmt.Errorf("%s: %w", Describe(addr), err)
		}
	}
	if err := migrate(x); err != nil {
		x.Close()
		return nil, fmt.Errorf("%s: %w", Describe(addr), err)
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("not a password"), BcryptCost)
	if err != nil {
		x.Close()
		return nil, err
	}
	return &Store{db: x, dummy: dummy, drop: drop}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	err := s.db.Close()
	if s.drop != nil {
		s.drop()
	}
	return err
}

func now() int64 { return time.Now().Unix() }

func unix(t int64) time.Time {
	if t == 0 {
		return time.Time{}
	}
	return time.Unix(t, 0)
}

// HashPassword returns the bcrypt hash of pw.
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), BcryptCost)
	return string(h), err
}

const userCols = `id, name, password, location, sysop, locked, created, last_login, logins, posts,
	level, flags, time_day, time_used, dl_day, dl_count, prefs, downloads,
	(SELECT COUNT(*) FROM files WHERE files.uploader_id = users.id)`

func scanUser(row interface{ Scan(...any) error }) (*User, string, error) {
	var u User
	var pw string
	var created, last int64
	err := row.Scan(&u.ID, &u.Name, &pw, &u.Location, &u.Sysop, &u.Locked,
		&created, &last, &u.Logins, &u.Posts, &u.Level, &u.Flags, &u.TimeDay, &u.TimeUsed, &u.DLDay, &u.DLCount, &u.Prefs,
		&u.Downloads, &u.Uploads)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	u.Created, u.LastLogin = unix(created), unix(last)
	u.External = pw == NoPassword
	return &u, pw, nil
}

// CreateUser adds an account.  password "" makes an external account (no
// password: only an upstream menu logs it in).
func (s *Store) CreateUser(name, password, location string, sysop bool) (*User, error) {
	hash := NoPassword
	if password != "" {
		h, err := HashPassword(password)
		if err != nil {
			return nil, err
		}
		hash = h
	}
	id, err := s.db.insertID(`INSERT INTO users (name, password, location, sysop, created)
		VALUES (?, ?, ?, ?, ?)`, name, hash, location, sysop, now())
	if err != nil {
		if isUnique(err) {
			return nil, ErrExists
		}
		return nil, err
	}
	return s.UserByID(id)
}

// UserByName finds an account by name, in any case.
func (s *Store) UserByName(name string) (*User, error) {
	u, _, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE name = ?`, name))
	return u, err
}

// UserByID finds an account by id.
func (s *Store) UserByID(id int64) (*User, error) {
	u, _, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
	return u, err
}

// CheckPassword returns the user if name exists and pw is its password,
// ErrNotFound otherwise.  It takes as long for unknown users and external
// accounts as for a wrong password.
func (s *Store) CheckPassword(name, pw string) (*User, error) {
	u, hash, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE name = ?`, name))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if u == nil || hash == NoPassword {
		bcrypt.CompareHashAndPassword(s.dummy, []byte(pw))
		return nil, ErrNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) != nil {
		return nil, ErrNotFound
	}
	return u, nil
}

// SetPassword changes a user's password.
func (s *Store) SetPassword(id int64, pw string) error {
	h, err := HashPassword(pw)
	if err != nil {
		return err
	}
	return s.exec1(`UPDATE users SET password = ? WHERE id = ?`, h, id)
}

// SetLocation changes a user's location.
func (s *Store) SetLocation(id int64, loc string) error {
	return s.exec1(`UPDATE users SET location = ? WHERE id = ?`, loc, id)
}

// SetPrefs sets a user's settings.
func (s *Store) SetPrefs(id int64, prefs string) error {
	return s.exec1(`UPDATE users SET prefs = ? WHERE id = ?`, prefs, id)
}

// SetSysop gives or takes a user's sysop rights.
func (s *Store) SetSysop(id int64, sysop bool) error {
	return s.exec1(`UPDATE users SET sysop = ? WHERE id = ?`, sysop, id)
}

// SetLocked locks a user out, or lets them in again.
func (s *Store) SetLocked(id int64, locked bool) error {
	return s.exec1(`UPDATE users SET locked = ? WHERE id = ?`, locked, id)
}

// DeleteUser removes an account and its mail; its messages stay.
func (s *Store) DeleteUser(id int64) error {
	return s.exec1(`DELETE FROM users WHERE id = ?`, id)
}

// LoggedIn counts a login.
func (s *Store) LoggedIn(id int64) error {
	return s.exec1(`UPDATE users SET logins = logins + 1, last_login = ? WHERE id = ?`, now(), id)
}

// Users lists the accounts by name.
func (s *Store) Users() ([]*User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, _, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) exec1(q string, args ...any) error {
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateArea adds a message area.
func (s *Store) CreateArea(name, desc string, sysopOnly bool) (*Area, error) {
	id, err := s.db.insertID(`INSERT INTO areas (name, description, sysop_only) VALUES (?, ?, ?)`,
		name, desc, sysopOnly)
	if err != nil {
		if isUnique(err) {
			return nil, ErrExists
		}
		return nil, err
	}
	return s.Area(id)
}

// DeleteArea removes an area and its messages.
func (s *Store) DeleteArea(id int64) error {
	return s.exec1(`DELETE FROM areas WHERE id = ?`, id)
}

const areaQuery = `SELECT a.id, a.name, a.description, a.sysop_only, COALESCE(a.newsgroup, ''), COALESCE(a.echotag, ''),
	a.read_req, a.post_req,
	(SELECT COUNT(*) FROM messages m WHERE m.area_id = a.id),
	(SELECT COALESCE(MIN(m.id), 0) FROM messages m WHERE m.area_id = a.id),
	(SELECT COALESCE(MAX(m.id), 0) FROM messages m WHERE m.area_id = a.id)
	FROM areas a`

func scanArea(row interface{ Scan(...any) error }) (*Area, error) {
	var a Area
	err := row.Scan(&a.ID, &a.Name, &a.Description, &a.SysopOnly, &a.Newsgroup, &a.EchoTag, &a.ReadReq, &a.PostReq, &a.Messages,
		&a.FirstID, &a.LastID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

// Areas lists the message areas.
func (s *Store) Areas() ([]*Area, error) {
	rows, err := s.db.Query(areaQuery + ` ORDER BY a.id`)
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

// Area finds an area by id.
func (s *Store) Area(id int64) (*Area, error) {
	return scanArea(s.db.QueryRow(areaQuery+` WHERE a.id = ?`, id))
}

// AreaByName finds an area by name.
func (s *Store) AreaByName(name string) (*Area, error) {
	return scanArea(s.db.QueryRow(areaQuery+` WHERE a.name = ?`, name))
}

const msgCols = `id, area_id, from_id, from_name, to_name, subject, body, reply_to, created,
	COALESCE(message_id, ''), origin, COALESCE(ftn_msgid, '')`

func scanMessage(row interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var created int64
	err := row.Scan(&m.ID, &m.AreaID, &m.FromID, &m.From, &m.To, &m.Subject,
		&m.Body, &m.ReplyTo, &created, &m.MessageID, &m.Origin, &m.FTNMsgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	m.Created = unix(created)
	return &m, err
}

// Post adds a message to an area.
func (s *Store) Post(m *Message) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var msgid any
	if m.MessageID != "" {
		msgid = m.MessageID
	}
	id, err := tx.insertID(`INSERT INTO messages (area_id, from_id, from_name, to_name, subject, body, reply_to, created,
		message_id, origin, raw, ftn_msgid, ftn)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, m.AreaID, m.FromID, m.From, m.To, m.Subject, m.Body, m.ReplyTo, now(),
		msgid, m.Origin, m.Raw, nullString(m.FTNMsgID), m.FTN)
	if err != nil {
		if isUnique(err) {
			return 0, ErrExists
		}
		return 0, err
	}
	if m.MessageID != "" {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO history (message_id, seen) VALUES (?, ?)`,
			m.MessageID, now()); err != nil {
			return 0, err
		}
	}
	if m.FromID > 0 {
		if _, err := tx.Exec(`UPDATE users SET posts = posts + 1 WHERE id = ?`, m.FromID); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// Message finds a message by id.
func (s *Store) Message(id int64) (*Message, error) {
	return scanMessage(s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE id = ?`, id))
}

// Messages lists up to limit messages of an area, from id after on
// (exclusive), oldest first.
func (s *Store) Messages(area, after int64, limit int) ([]*Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE area_id = ? AND id > ?
		ORDER BY id LIMIT ?`, area, after, limit)
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

// Neighbour finds the message of the same area just after id (next) or
// just before it.
func (s *Store) Neighbour(area, id int64, next bool) (*Message, error) {
	q := `SELECT ` + msgCols + ` FROM messages WHERE area_id = ? AND id < ? ORDER BY id DESC LIMIT 1`
	if next {
		q = `SELECT ` + msgCols + ` FROM messages WHERE area_id = ? AND id > ? ORDER BY id LIMIT 1`
	}
	return scanMessage(s.db.QueryRow(q, area, id))
}

// DeleteMessage removes a message.
func (s *Store) DeleteMessage(id int64) error {
	return s.exec1(`DELETE FROM messages WHERE id = ?`, id)
}

// LastRead is the last message of an area the user has read.
func (s *Store) LastRead(user, area int64) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT message_id FROM lastread WHERE user_id = ? AND area_id = ?`,
		user, area).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// SetLastRead moves the user's last read message of an area forward to id.
func (s *Store) SetLastRead(user, area, id int64) error {
	_, err := s.db.Exec(`INSERT INTO lastread (user_id, area_id, message_id) VALUES (?, ?, ?)
		ON CONFLICT (user_id, area_id) DO UPDATE SET message_id = {greatest}(lastread.message_id, excluded.message_id)`,
		user, area, id)
	return err
}

// NewCount counts the messages of an area the user has not read.
func (s *Store) NewCount(user, area int64) (int, error) {
	last, err := s.LastRead(user, area)
	if err != nil {
		return 0, err
	}
	var n int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE area_id = ? AND id > ?`,
		area, last).Scan(&n)
	return n, err
}

const mailCols = `id, from_id, from_name, to_id, to_name, subject, body, reply_to, created, "read",
	COALESCE(ftn_msgid, '')`

func scanMail(row interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var created int64
	err := row.Scan(&m.ID, &m.FromID, &m.From, &m.ToID, &m.To, &m.Subject,
		&m.Body, &m.ReplyTo, &created, &m.Read, &m.FTNMsgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	m.Created = unix(created)
	return &m, err
}

// SendMail delivers a private message.
func (s *Store) SendMail(m *Message) (int64, error) {
	id, err := s.db.insertID(`INSERT INTO mail (from_id, from_name, to_id, to_name, subject, body, reply_to, created,
		ftn_msgid)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, m.FromID, m.From, m.ToID, m.To, m.Subject, m.Body, m.ReplyTo, now(),
		nullString(m.FTNMsgID))
	if err != nil {
		return 0, err
	}
	return id, nil
}

// Inbox lists a user's mail, oldest first.
func (s *Store) Inbox(user int64) ([]*Message, error) {
	rows, err := s.db.Query(`SELECT `+mailCols+` FROM mail WHERE to_id = ? ORDER BY id`, user)
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

// Mail finds a mail of user's by id: no one reads another's mail.
func (s *Store) Mail(user, id int64) (*Message, error) {
	return scanMail(s.db.QueryRow(`SELECT `+mailCols+` FROM mail WHERE id = ? AND to_id = ?`, id, user))
}

// MarkMailRead marks a mail of user's read.
func (s *Store) MarkMailRead(user, id int64) error {
	return s.exec1(`UPDATE mail SET "read" = TRUE WHERE id = ? AND to_id = ?`, id, user)
}

// DeleteMail deletes a mail of user's.
func (s *Store) DeleteMail(user, id int64) error {
	return s.exec1(`DELETE FROM mail WHERE id = ? AND to_id = ?`, id, user)
}

// UnreadMail counts a user's unread mail.
func (s *Store) UnreadMail(user int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM mail WHERE to_id = ? AND "read" = FALSE`, user).Scan(&n)
	return n, err
}

// FileArea is an area of files.
type FileArea struct {
	ID          int64
	Name        string
	Description string
	SysopUpload bool   // only sysops upload
	DownloadReq string // the access requirement to download ("": anyone)
	UploadReq   string // and to upload
	Files       int
	Bytes       int64
}

// File is a file in a file area; its contents are kept outside the
// database, by ID (see the filestore).
type File struct {
	ID          int64
	AreaID      int64
	Name        string
	Description string
	Size        int64
	SHA256      string
	UploaderID  int64
	Uploader    string
	Created     time.Time
	Downloads   int
	LongDesc    string // more lines of description (a FILE_ID.DIZ's)
}

// CreateFileArea adds a file area.
func (s *Store) CreateFileArea(name, desc string, sysopUpload bool) (*FileArea, error) {
	id, err := s.db.insertID(`INSERT INTO file_areas (name, description, sysop_upload) VALUES (?, ?, ?)`,
		name, desc, sysopUpload)
	if err != nil {
		if isUnique(err) {
			return nil, ErrExists
		}
		return nil, err
	}
	return s.FileArea(id)
}

// DeleteFileArea removes an empty file area.
func (s *Store) DeleteFileArea(id int64) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM files WHERE area_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("the area has %d files", n)
	}
	return s.exec1(`DELETE FROM file_areas WHERE id = ?`, id)
}

const fileAreaQuery = `SELECT a.id, a.name, a.description, a.sysop_upload, a.download_req, a.upload_req,
	(SELECT COUNT(*) FROM files f WHERE f.area_id = a.id),
	(SELECT COALESCE(SUM(f.size), 0) FROM files f WHERE f.area_id = a.id)
	FROM file_areas a`

func scanFileArea(row interface{ Scan(...any) error }) (*FileArea, error) {
	var a FileArea
	err := row.Scan(&a.ID, &a.Name, &a.Description, &a.SysopUpload, &a.DownloadReq, &a.UploadReq, &a.Files, &a.Bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

// FileAreas lists the file areas.
func (s *Store) FileAreas() ([]*FileArea, error) {
	rows, err := s.db.Query(fileAreaQuery + ` ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*FileArea
	for rows.Next() {
		a, err := scanFileArea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FileArea finds a file area by id.
func (s *Store) FileArea(id int64) (*FileArea, error) {
	return scanFileArea(s.db.QueryRow(fileAreaQuery+` WHERE a.id = ?`, id))
}

// FileAreaByName finds a file area by name, in any case.
func (s *Store) FileAreaByName(name string) (*FileArea, error) {
	return scanFileArea(s.db.QueryRow(fileAreaQuery+` WHERE a.name = ?`, name))
}

const fileCols = `id, area_id, name, description, size, sha256, uploader_id, uploader, created, downloads,
	COALESCE(long_desc, '')`

func scanFile(row interface{ Scan(...any) error }) (*File, error) {
	var f File
	var created int64
	err := row.Scan(&f.ID, &f.AreaID, &f.Name, &f.Description, &f.Size, &f.SHA256,
		&f.UploaderID, &f.Uploader, &created, &f.Downloads, &f.LongDesc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	f.Created = unix(created)
	return &f, err
}

// AddFile records a file; ErrExists if the area has one of that name.
func (s *Store) AddFile(f *File) (int64, error) {
	id, err := s.db.insertID(`INSERT INTO files (area_id, name, description, size, sha256, uploader_id, uploader, created, long_desc)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, f.AreaID, f.Name, f.Description, f.Size, f.SHA256,
		f.UploaderID, f.Uploader, now(), f.LongDesc)
	if err != nil {
		if isUnique(err) {
			return 0, ErrExists
		}
		return 0, err
	}
	return id, nil
}

// Files lists an area's files by name.
func (s *Store) Files(area int64) ([]*File, error) {
	return s.fileList(`SELECT `+fileCols+` FROM files WHERE area_id = ? ORDER BY name`, area)
}

// FilesSince are the files of the areas added since t, newest first, up
// to limit.
func (s *Store) FilesSince(areas []int64, t time.Time, limit int) ([]*File, error) {
	if len(areas) == 0 {
		return nil, nil
	}
	in, args := inList(areas)
	return s.fileList(`SELECT `+fileCols+` FROM files WHERE area_id IN (`+in+`) AND created > ?
		ORDER BY created DESC, id DESC LIMIT ?`, append(args, t.Unix(), limit)...)
}

// SearchFiles are the files of the areas whose name or descriptions have
// text in them (in any case), newest first, up to limit.
func (s *Store) SearchFiles(areas []int64, text string, limit int) ([]*File, error) {
	if len(areas) == 0 {
		return nil, nil
	}
	in, args := inList(areas)
	p := likeEscape(text)
	return s.fileList(`SELECT `+fileCols+` FROM files WHERE area_id IN (`+in+`)
		AND (LOWER(name{plain}) LIKE ? ESCAPE '!' OR LOWER(description) LIKE ? ESCAPE '!' OR LOWER(COALESCE(long_desc, '')) LIKE ? ESCAPE '!')
		ORDER BY created DESC, id DESC LIMIT ?`, append(args, p, p, p, limit)...)
}

// inList is "?, ?, ?" for ids, and the ids as arguments.
func inList(ids []int64) (string, []any) {
	marks := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marks[i], args[i] = "?", id
	}
	return strings.Join(marks, ", "), args
}

func (s *Store) fileList(q string, args ...any) ([]*File, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FileByName finds a file of an area by name, in any case.
func (s *Store) FileByName(area int64, name string) (*File, error) {
	return scanFile(s.db.QueryRow(`SELECT `+fileCols+` FROM files WHERE area_id = ? AND name = ?`, area, name))
}

// FileByID finds a file by id.
func (s *Store) FileByID(id int64) (*File, error) {
	return scanFile(s.db.QueryRow(`SELECT `+fileCols+` FROM files WHERE id = ?`, id))
}

// DeleteFile removes a file's record.
func (s *Store) DeleteFile(id int64) error {
	return s.exec1(`DELETE FROM files WHERE id = ?`, id)
}

// SetFileLongDesc changes a file's longer description.
func (s *Store) SetFileLongDesc(id int64, desc string) error {
	return s.exec1(`UPDATE files SET long_desc = ? WHERE id = ?`, desc, id)
}

// SetFileDescription changes a file's description.
func (s *Store) SetFileDescription(id int64, desc string) error {
	return s.exec1(`UPDATE files SET description = ? WHERE id = ?`, desc, id)
}

// CountDownload counts a download of a file.
func (s *Store) CountDownload(id int64) error {
	_, err := s.db.Exec(`UPDATE files SET downloads = downloads + 1 WHERE id = ?`, id)
	return err
}

// UploadedSince is how many bytes a user has uploaded since t.
func (s *Store) UploadedSince(user int64, t time.Time) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(size), 0) FROM files WHERE uploader_id = ? AND created >= ?`,
		user, t.Unix()).Scan(&n)
	return n, err
}
