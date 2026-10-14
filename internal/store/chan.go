package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Board is an image board: its directory (/b/), and how it is run.
type Board struct {
	ID          int64
	Dir         string // "b", as in /b/
	Title       string // "Random"
	Description string
	MaxFile     int64 // bytes, the largest image (0: the image board's)
	Pages       int   // of threads, before the oldest are pruned
	PerPage     int   // threads a page
	BumpLimit   int   // replies that bump a thread
	ImageLimit  int   // images a thread may have
	ForcedAnon  bool  // names and tripcodes are ignored
	DefaultName string
	Worksafe    bool // its default style is the blue one
	Locked      bool // no one but staff posts
	LoginToPost bool // only the BBS's users post
	TextOnly    bool // threads need no image
	Oekaki      bool // its posters can draw their pictures in the page
	Mods        string
	Sort        int
	LastNo      int64
	Created     time.Time
}

// BoardPost is a post on an image board: a thread's first (Thread 0) or a
// reply to one (Thread its first's number).
type BoardPost struct {
	ID       int64
	BoardID  int64
	No       int64
	Thread   int64 // 0: it starts a thread
	Name     string
	Trip     string // "!abcdefghij", "!!abcdefghij"
	Capcode  string // "mod", "admin"
	Email    string
	Subject  string
	Comment  string
	Password string // a hash, to delete it by
	IP       string
	UserID   int64 // who posted, if logged in (0: anonymous)
	Created  time.Time
	Bumped   time.Time
	Sticky   bool
	Closed   bool
	// its file, if it has one
	FileName    string // as uploaded, without its extension
	Ext         string // ".jpg"
	Tim         int64  // what it is stored as
	FileSize    int64
	W, H        int
	TnW, TnH    int
	MD5         string // base64, as 4chan has it
	Spoiler     bool
	FileDeleted bool
	Banned      bool  // its poster was banned for it, as it says
	Replay      bool  // its image is a drawing that carries its strokes
	DrawMS      int64 // how long the drawing took
	// for a thread's first post, when listed
	Replies, Images int
}

// HasFile says whether the post has a file still.
func (p *BoardPost) HasFile() bool { return p.Ext != "" && !p.FileDeleted }

// Ban keeps an address from posting.
type Ban struct {
	ID      int64
	IP      string // an address, or a prefix ending in . or : (a range)
	Board   string // "": every board
	Reason  string
	By      string
	Post    string // what was posted, as the banned see it
	Created time.Time
	Expires time.Time // zero: never
}

// Report is a post someone thinks staff should see.
type Report struct {
	ID      int64
	BoardID int64
	Dir     string
	No      int64
	Reason  string
	IP      string
	Created time.Time
}

const boardCols = `id, dir, title, description, max_file, pages, per_page, bump_limit, image_limit, forced_anon,
	default_name, worksafe, locked, login_to_post, text_only, mods, sort, last_no, created, oekaki`

func scanBoard(row interface{ Scan(...any) error }) (*Board, error) {
	var b Board
	var created int64
	err := row.Scan(&b.ID, &b.Dir, &b.Title, &b.Description, &b.MaxFile, &b.Pages, &b.PerPage, &b.BumpLimit,
		&b.ImageLimit, &b.ForcedAnon, &b.DefaultName, &b.Worksafe, &b.Locked, &b.LoginToPost, &b.TextOnly,
		&b.Mods, &b.Sort, &b.LastNo, &created, &b.Oekaki)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	b.Created = unix(created)
	return &b, err
}

// CreateBoard adds an image board, as b has it (ID, LastNo, Created
// aside).
func (s *Store) CreateBoard(b *Board) (*Board, error) {
	id, err := s.db.insertID(`INSERT INTO chan_boards (dir, title, description, max_file, pages, per_page, bump_limit,
		image_limit, forced_anon, default_name, worksafe, locked, login_to_post, text_only, mods, sort, created, oekaki)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, b.Dir, b.Title, b.Description, b.MaxFile, b.Pages,
		b.PerPage, b.BumpLimit, b.ImageLimit, b.ForcedAnon, b.DefaultName, b.Worksafe, b.Locked, b.LoginToPost,
		b.TextOnly, b.Mods, b.Sort, now(), b.Oekaki)
	if err != nil {
		if isUnique(err) {
			return nil, ErrExists
		}
		return nil, err
	}
	return s.BoardByID(id)
}

// UpdateBoard keeps a board's settings (not its directory or numbers).
func (s *Store) UpdateBoard(b *Board) error {
	_, err := s.db.Exec(`UPDATE chan_boards SET title = ?, description = ?, max_file = ?, pages = ?, per_page = ?,
		bump_limit = ?, image_limit = ?, forced_anon = ?, default_name = ?, worksafe = ?, locked = ?, login_to_post = ?,
		text_only = ?, mods = ?, sort = ?, oekaki = ? WHERE id = ?`, b.Title, b.Description, b.MaxFile, b.Pages, b.PerPage,
		b.BumpLimit, b.ImageLimit, b.ForcedAnon, b.DefaultName, b.Worksafe, b.Locked, b.LoginToPost, b.TextOnly,
		b.Mods, b.Sort, b.Oekaki, b.ID)
	return err
}

// DeleteBoard removes a board and its posts (its files are the caller's
// to remove).
func (s *Store) DeleteBoard(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM chan_posts WHERE board_id = ?`, `DELETE FROM chan_reports WHERE board_id = ?`,
		`DELETE FROM chan_boards WHERE id = ?`} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// BoardByID finds a board.
func (s *Store) BoardByID(id int64) (*Board, error) {
	return scanBoard(s.db.QueryRow(`SELECT `+boardCols+` FROM chan_boards WHERE id = ?`, id))
}

// Board finds a board by its directory.
func (s *Store) Board(dir string) (*Board, error) {
	return scanBoard(s.db.QueryRow(`SELECT `+boardCols+` FROM chan_boards WHERE dir = ?`, dir))
}

// Boards lists the boards, in their order.
func (s *Store) Boards() ([]*Board, error) {
	rows, err := s.db.Query(`SELECT ` + boardCols + ` FROM chan_boards ORDER BY sort, dir`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Board
	for rows.Next() {
		b, err := scanBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

const postCols = `id, board_id, num, thread, name, trip, capcode, email, subject, body, password, ip, user_id, created,
	bumped, sticky, closed, file_name, ext, tim, file_size, w, h, tn_w, tn_h, md5, spoiler, file_deleted, banned, replay, draw_ms`

func scanPost(row interface{ Scan(...any) error }, more ...any) (*BoardPost, error) {
	var p BoardPost
	var created, bumped int64
	dest := []any{&p.ID, &p.BoardID, &p.No, &p.Thread, &p.Name, &p.Trip, &p.Capcode, &p.Email, &p.Subject, &p.Comment,
		&p.Password, &p.IP, &p.UserID, &created, &bumped, &p.Sticky, &p.Closed, &p.FileName, &p.Ext, &p.Tim,
		&p.FileSize, &p.W, &p.H, &p.TnW, &p.TnH, &p.MD5, &p.Spoiler, &p.FileDeleted, &p.Banned, &p.Replay, &p.DrawMS}
	err := row.Scan(append(dest, more...)...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	p.Created, p.Bumped = unix(created), unix(bumped)
	return &p, err
}

func (s *Store) posts(q string, args ...any) ([]*BoardPost, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BoardPost
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ErrThreadGone is a reply to a thread that is not there.
var ErrThreadGone = errors.New("the thread is gone")

// AddPost numbers a post and adds it to its board (to its thread, which
// it bumps unless bump is false); its No and Created are set.
func (s *Store) AddPost(p *BoardPost, bump bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if p.Thread != 0 {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM chan_posts WHERE board_id = ? AND num = ? AND thread = 0`,
			p.BoardID, p.Thread).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrThreadGone
		}
	}
	if _, err := tx.Exec(`UPDATE chan_boards SET last_no = last_no + 1 WHERE id = ?`, p.BoardID); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT last_no FROM chan_boards WHERE id = ?`, p.BoardID).Scan(&p.No); err != nil {
		return err
	}
	t := now()
	p.Created, p.Bumped = unix(t), unix(t)
	if _, err := tx.insertID(`INSERT INTO chan_posts (board_id, num, thread, name, trip, capcode, email, subject, body,
		password, ip, user_id, created, bumped, sticky, closed, file_name, ext, tim, file_size, w, h, tn_w, tn_h, md5,
		spoiler, replay, draw_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.BoardID, p.No, p.Thread, p.Name, p.Trip, p.Capcode, p.Email, p.Subject, p.Comment, p.Password, p.IP,
		p.UserID, t, t, p.Sticky, p.Closed, p.FileName, p.Ext, p.Tim, p.FileSize, p.W, p.H, p.TnW, p.TnH, p.MD5,
		p.Spoiler, p.Replay, p.DrawMS); err != nil {
		return err
	}
	if p.Thread != 0 && bump {
		if _, err := tx.Exec(`UPDATE chan_posts SET bumped = ? WHERE board_id = ? AND num = ?`, t, p.BoardID,
			p.Thread); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ChanPost finds a post by its board and number.
func (s *Store) ChanPost(board, no int64) (*BoardPost, error) {
	return scanPost(s.db.QueryRow(`SELECT `+postCols+` FROM chan_posts WHERE board_id = ? AND num = ?`, board, no))
}

// threadCounts are a thread's replies and the images among them.
const threadCounts = `,
	(SELECT COUNT(*) FROM chan_posts r WHERE r.board_id = chan_posts.board_id AND r.thread = chan_posts.num),
	(SELECT COUNT(*) FROM chan_posts r WHERE r.board_id = chan_posts.board_id AND r.thread = chan_posts.num AND r.ext <> '')`

func (s *Store) threads(q string, args ...any) ([]*BoardPost, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BoardPost
	for rows.Next() {
		var replies, images int
		p, err := scanPost(rows, &replies, &images)
		if err != nil {
			return nil, err
		}
		p.Replies, p.Images = replies, images
		out = append(out, p)
	}
	return out, rows.Err()
}

// ChanThreads lists a board's threads, stickies and the last bumped first,
// with their counts: limit of them from offset.
func (s *Store) ChanThreads(board int64, offset, limit int) ([]*BoardPost, error) {
	return s.threads(`SELECT `+postCols+threadCounts+` FROM chan_posts WHERE board_id = ? AND thread = 0
		ORDER BY sticky DESC, bumped DESC, num DESC LIMIT ? OFFSET ?`, board, limit, offset)
}

// ChanThread is a thread's first post, with its counts.
func (s *Store) ChanThread(board, no int64) (*BoardPost, error) {
	ps, err := s.threads(`SELECT `+postCols+threadCounts+` FROM chan_posts WHERE board_id = ? AND num = ? AND thread = 0`,
		board, no)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		return nil, ErrNotFound
	}
	return ps[0], nil
}

// ChanThreadCount is how many threads a board has.
func (s *Store) ChanThreadCount(board int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM chan_posts WHERE board_id = ? AND thread = 0`, board).Scan(&n)
	return n, err
}

// ChanReplies are a thread's replies, oldest first: all of them (last 0),
// or the last ones.
func (s *Store) ChanReplies(board, thread int64, last int) ([]*BoardPost, error) {
	if last <= 0 {
		return s.posts(`SELECT `+postCols+` FROM chan_posts WHERE board_id = ? AND thread = ? ORDER BY num`, board, thread)
	}
	ps, err := s.posts(`SELECT `+postCols+` FROM chan_posts WHERE board_id = ? AND thread = ? ORDER BY num DESC LIMIT ?`,
		board, thread, last)
	for i, j := 0, len(ps)-1; i < j; i, j = i+1, j-1 {
		ps[i], ps[j] = ps[j], ps[i]
	}
	return ps, err
}

// ChanPostThreads are, of a board's post numbers, the thread each is in (its
// own for a first post): for linking quotes.  Numbers not there are not
// in it.
func (s *Store) ChanPostThreads(board int64, nos []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	for len(nos) > 0 {
		n := min(len(nos), 200)
		args := []any{board}
		for _, no := range nos[:n] {
			args = append(args, no)
		}
		rows, err := s.db.Query(`SELECT num, thread FROM chan_posts WHERE board_id = ? AND num IN (?`+
			strings.Repeat(", ?", n-1)+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var no, thread int64
			if err := rows.Scan(&no, &thread); err != nil {
				rows.Close()
				return nil, err
			}
			if thread == 0 {
				thread = no
			}
			out[no] = thread
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		nos = nos[n:]
	}
	return out, nil
}

// PostByMD5 finds a post on a board whose file has an MD5 (a duplicate).
func (s *Store) PostByMD5(board int64, md5 string) (*BoardPost, error) {
	return scanPost(s.db.QueryRow(`SELECT `+postCols+` FROM chan_posts WHERE board_id = ? AND md5 = ? AND file_deleted = ?
		ORDER BY num LIMIT 1`, board, md5, false))
}

// LastPostBy is when an address last posted (zero: never), and last made
// a thread.
func (s *Store) LastPostBy(ip string) (post, thread time.Time, err error) {
	var p, t sql.NullInt64
	err = s.db.QueryRow(`SELECT MAX(created), MAX(CASE WHEN thread = 0 THEN created END) FROM chan_posts WHERE ip = ?`,
		ip).Scan(&p, &t)
	if p.Valid {
		post = unix(p.Int64)
	}
	if t.Valid {
		thread = unix(t.Int64)
	}
	return
}

// RecentImages are the latest posts with images, across the boards.
func (s *Store) RecentImages(limit int) ([]*BoardPost, error) {
	return s.posts(`SELECT `+postCols+` FROM chan_posts WHERE ext <> '' AND file_deleted = ? AND spoiler = ?
		ORDER BY created DESC, id DESC LIMIT ?`, false, false, limit)
}

// PostsByIP are an address's posts, the newest first.
func (s *Store) PostsByIP(ip string, limit int) ([]*BoardPost, error) {
	return s.posts(`SELECT `+postCols+` FROM chan_posts WHERE ip = ? ORDER BY created DESC, id DESC LIMIT ?`, ip, limit)
}

// ChanStats are the boards' totals: posts, and the bytes of their files.
func (s *Store) ChanStats() (posts int, bytes int64, err error) {
	err = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN file_deleted = ? THEN file_size ELSE 0 END), 0)
		FROM chan_posts`, false).Scan(&posts, &bytes)
	return
}

// DeleteChanPost removes a post; a thread's first takes its replies with it.
// What was removed is returned, for their files.
func (s *Store) DeleteChanPost(board, no int64) ([]*BoardPost, error) {
	p, err := s.ChanPost(board, no)
	if err != nil {
		return nil, err
	}
	gone := []*BoardPost{p}
	if p.Thread == 0 {
		rs, err := s.ChanReplies(board, no, 0)
		if err != nil {
			return nil, err
		}
		gone = append(gone, rs...)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q, args := `DELETE FROM chan_posts WHERE board_id = ? AND num = ?`, []any{board, no}
	if p.Thread == 0 {
		q, args = q+` OR board_id = ? AND thread = ?`, append(args, board, no)
	}
	if _, err := tx.Exec(q, args...); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM chan_reports WHERE board_id = ? AND num = ?`, board, no); err != nil {
		return nil, err
	}
	return gone, tx.Commit()
}

// DeleteChanFile marks a post's file deleted.
func (s *Store) DeleteChanFile(board, no int64) error {
	_, err := s.db.Exec(`UPDATE chan_posts SET file_deleted = ? WHERE board_id = ? AND num = ?`, true, board, no)
	return err
}

// MarkChanBanned has a post say its poster was banned for it.
func (s *Store) MarkChanBanned(board, no int64) error {
	_, err := s.db.Exec(`UPDATE chan_posts SET banned = ? WHERE board_id = ? AND num = ?`, true, board, no)
	return err
}

// SetChanThread sets a thread's sticky and closed.
func (s *Store) SetChanThread(board, no int64, sticky, closed bool) error {
	_, err := s.db.Exec(`UPDATE chan_posts SET sticky = ?, closed = ? WHERE board_id = ? AND num = ? AND thread = 0`,
		sticky, closed, board, no)
	return err
}

// PruneChanThreads removes a board's threads past the first keep (stickies
// kept): what was removed is returned, for their files.
func (s *Store) PruneChanThreads(board int64, keep int) ([]*BoardPost, error) {
	all, err := s.posts(`SELECT `+postCols+` FROM chan_posts WHERE board_id = ? AND thread = 0 AND sticky = ?
		ORDER BY bumped DESC, num DESC`, board, false)
	if err != nil || len(all) <= keep {
		return nil, err
	}
	old := all[keep:]
	var gone []*BoardPost
	for _, t := range old {
		g, err := s.DeleteChanPost(board, t.No)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return gone, err
		}
		gone = append(gone, g...)
	}
	return gone, nil
}

// AddBan bans an address (or a range).
func (s *Store) AddBan(b *Ban) (int64, error) {
	var exp int64
	if !b.Expires.IsZero() {
		exp = b.Expires.Unix()
	}
	return s.db.insertID(`INSERT INTO chan_bans (ip, board, reason, by_name, post, created, expires)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, b.IP, b.Board, b.Reason, b.By, b.Post, now(), exp)
}

// DeleteBan lifts a ban.
func (s *Store) DeleteBan(id int64) error {
	return s.exec1(`DELETE FROM chan_bans WHERE id = ?`, id)
}

func (s *Store) bans(q string, args ...any) ([]*Ban, error) {
	rows, err := s.db.Query(`SELECT id, ip, board, reason, by_name, post, created, expires FROM chan_bans `+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Ban
	for rows.Next() {
		var b Ban
		var created, exp int64
		if err := rows.Scan(&b.ID, &b.IP, &b.Board, &b.Reason, &b.By, &b.Post, &created, &exp); err != nil {
			return nil, err
		}
		b.Created = unix(created)
		if exp != 0 {
			b.Expires = unix(exp)
		}
		out = append(out, &b)
	}
	return out, rows.Err()
}

// Bans lists the bans in force, the newest first.
func (s *Store) Bans() ([]*Ban, error) {
	return s.bans(`WHERE expires = 0 OR expires > ? ORDER BY id DESC`, now())
}

// BanFor is the ban in force on an address on a board (nil: none): one
// of the address, or of a range (a prefix ending in . or :) it is in.
func (s *Store) BanFor(ip, board string) (*Ban, error) {
	bs, err := s.bans(`WHERE (expires = 0 OR expires > ?) AND (board = '' OR board = ?) ORDER BY id DESC`, now(), board)
	if err != nil {
		return nil, err
	}
	for _, b := range bs {
		if b.IP == ip || (strings.HasSuffix(b.IP, ".") || strings.HasSuffix(b.IP, ":")) && strings.HasPrefix(ip, b.IP) {
			return b, nil
		}
	}
	return nil, nil
}

// AddReport reports a post (once an address).
func (s *Store) AddReport(r *Report) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chan_reports WHERE board_id = ? AND num = ? AND ip = ?`,
		r.BoardID, r.No, r.IP).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrExists
	}
	_, err := s.db.insertID(`INSERT INTO chan_reports (board_id, num, reason, ip, created) VALUES (?, ?, ?, ?, ?)`,
		r.BoardID, r.No, r.Reason, r.IP, now())
	return err
}

// Reports lists the reports, the newest first.
func (s *Store) Reports() ([]*Report, error) {
	rows, err := s.db.Query(`SELECT r.id, r.board_id, b.dir, r.num, r.reason, r.ip, r.created FROM chan_reports r
		JOIN chan_boards b ON b.id = r.board_id ORDER BY r.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Report
	for rows.Next() {
		var r Report
		var created int64
		if err := rows.Scan(&r.ID, &r.BoardID, &r.Dir, &r.No, &r.Reason, &r.IP, &created); err != nil {
			return nil, err
		}
		r.Created = unix(created)
		out = append(out, &r)
	}
	return out, rows.Err()
}

// DismissReports clears a post's reports.
func (s *Store) DismissReports(board, no int64) error {
	_, err := s.db.Exec(`DELETE FROM chan_reports WHERE board_id = ? AND num = ?`, board, no)
	return err
}
