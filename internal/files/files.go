// Package files keeps the BBS's file areas: the files' records are in the
// database (store), their contents in a directory, each under its record's
// ID.  No name a user gives is ever a path on disk.
package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Errors of uploads and deletions.
var (
	ErrDenied   = errors.New("permission denied")
	ErrExists   = errors.New("a file of that name is there already")
	ErrTooLarge = errors.New("the file is larger than allowed")
	ErrQuota    = errors.New("today's upload allowance is used up")
)

// Store is the file areas' storage.
type Store struct {
	DB         *store.Store
	Dir        string // the files, by ID, and tmp/ for uploads under way
	MaxSize    int64  // the largest file taken
	DailyQuota int64  // bytes a user may upload a day (0: no limit); sysops are not limited

	mu        sync.Mutex
	uploading map[int64]int64 // bytes of uploads under way, by user
}

// Open makes the storage in dir.
func Open(db *store.Store, dir string, maxSize, quota int64) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0700); err != nil {
		return nil, err
	}
	// what an upload cut short by a crash left
	old, _ := filepath.Glob(filepath.Join(dir, "tmp", "up-*"))
	for _, f := range old {
		os.Remove(f)
	}
	return &Store{DB: db, Dir: dir, MaxSize: maxSize, DailyQuota: quota,
		uploading: map[int64]int64{}}, nil
}

func (s *Store) path(id int64) string {
	return filepath.Join(s.Dir, strconv.FormatInt(id, 10))
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._+()\[\]!,#&'@~=-]*$`)

// ValidName checks a file's (or a file area's) name: 1 to 64 of letters,
// digits, spaces and . _ + ( ) [ ] ! , # & ' @ ~ = -, starting with a letter
// or digit, not ending in a space or a dot.  No slashes, no control
// characters, nothing a shell or Windows minds.
func ValidName(name string) string {
	switch {
	case len(name) == 0 || len(name) > 64:
		return "A name is 1 to 64 characters long."
	case !namePattern.MatchString(name):
		return "Use letters, digits, spaces and . _ + ( ) [ ] ! , # & ' @ ~ = -, starting with a letter or digit."
	case strings.HasSuffix(name, " ") || strings.HasSuffix(name, "."):
		return "A name cannot end in a space or a dot."
	case strings.Contains(name, ".."):
		return "A name cannot have two dots together."
	}
	return ""
}

// CanUpload reports whether u may upload into a.
func CanUpload(u *store.User, a *store.FileArea) bool {
	return u != nil && !u.Locked && (u.Sysop || !a.SysopUpload)
}

// CanDelete reports whether u may delete f: its uploader, or a sysop.
func CanDelete(u *store.User, f *store.File) bool {
	return u != nil && (u.Sysop || u.ID == f.UploaderID)
}

// OpenFile opens a file's contents.
func (s *Store) OpenFile(f *store.File) (*os.File, error) {
	return os.Open(s.path(f.ID))
}

// Delete removes a file, if u may.
func (s *Store) Delete(u *store.User, f *store.File) error {
	if !CanDelete(u, f) {
		return ErrDenied
	}
	return s.Remove(f)
}

// Remove takes a file away, whoever may: the system's own doing.
func (s *Store) Remove(f *store.File) error {
	if err := s.DB.DeleteFile(f.ID); err != nil {
		return err
	}
	if err := os.Remove(s.path(f.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Upload is a file being uploaded: written to a temporary file, then put
// in its place by Commit, or thrown away by Abort.
type Upload struct {
	s     *Store
	user  *store.User
	area  *store.FileArea
	name  string
	desc  string
	tmp   *os.File
	hash  hash.Hash
	size  int64
	limit int64
	quota bool // the limit is the day's allowance, not the size limit
	done  bool
}

// Upload starts an upload of name into area a by u, of up to the size
// limit and what is left of u's daily allowance.
func (s *Store) Upload(u *store.User, a *store.FileArea, name, desc string) (*Upload, error) {
	if !CanUpload(u, a) {
		return nil, ErrDenied
	}
	if msg := ValidName(name); msg != "" {
		return nil, errors.New(msg)
	}
	if _, err := s.DB.FileByName(a.ID, name); err == nil {
		return nil, ErrExists
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	limit, quota := s.MaxSize, false
	if s.DailyQuota > 0 && !u.Sysop {
		done, err := s.DB.UploadedSince(u.ID, time.Now().Add(-24*time.Hour))
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		done += s.uploading[u.ID]
		s.mu.Unlock()
		left := s.DailyQuota - done
		if left <= 0 {
			return nil, ErrQuota
		}
		if left < limit {
			limit, quota = left, true
		}
	}
	tmp, err := os.CreateTemp(filepath.Join(s.Dir, "tmp"), "up-*")
	if err != nil {
		return nil, err
	}
	return &Upload{s: s, user: u, area: a, name: name, desc: desc, tmp: tmp,
		hash: sha256.New(), limit: limit, quota: quota}, nil
}

// Write writes to the upload, failing once it passes its limit.
func (up *Upload) Write(p []byte) (int, error) {
	if up.done {
		return 0, os.ErrClosed
	}
	if up.size+int64(len(p)) > up.limit {
		if up.quota {
			return 0, ErrQuota
		}
		return 0, ErrTooLarge
	}
	n, err := up.tmp.Write(p)
	up.hash.Write(p[:n])
	up.size += int64(n)
	up.s.mu.Lock()
	up.s.uploading[up.user.ID] += int64(n)
	up.s.mu.Unlock()
	return n, err
}

func (up *Upload) finish() {
	up.done = true
	up.s.mu.Lock()
	if up.s.uploading[up.user.ID] -= up.size; up.s.uploading[up.user.ID] <= 0 {
		delete(up.s.uploading, up.user.ID)
	}
	up.s.mu.Unlock()
}

// Abort throws the upload away.
func (up *Upload) Abort() {
	if up.done {
		return
	}
	up.finish()
	up.tmp.Close()
	os.Remove(up.tmp.Name())
}

// Commit records the file and puts it in its place.
func (up *Upload) Commit() (*store.File, error) {
	if up.done {
		return nil, os.ErrClosed
	}
	up.finish()
	tmp := up.tmp.Name()
	fail := func(err error) (*store.File, error) {
		os.Remove(tmp)
		return nil, err
	}
	if err := up.tmp.Sync(); err != nil {
		up.tmp.Close()
		return fail(err)
	}
	if err := up.tmp.Close(); err != nil {
		return fail(err)
	}
	f := &store.File{AreaID: up.area.ID, Name: up.name, Description: up.desc, Size: up.size,
		SHA256:     hex.EncodeToString(up.hash.Sum(nil)),
		UploaderID: up.user.ID, Uploader: up.user.Name}
	// an archive describes itself: its FILE_ID.DIZ
	if d := diz(tmp, up.name); d != "" {
		f.LongDesc = d
		if f.Description == "" {
			f.Description = short(d)
		}
	}
	id, err := up.s.DB.AddFile(f)
	if errors.Is(err, store.ErrExists) {
		return fail(ErrExists)
	}
	if err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp, up.s.path(id)); err != nil {
		up.s.DB.DeleteFile(id)
		return fail(err)
	}
	return up.s.DB.FileByID(id)
}

// Import adds a file from disk to an area as u (the sysop's command).
func (s *Store) Import(u *store.User, a *store.FileArea, path, name, desc string) (*store.File, error) {
	in, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	up, err := s.Upload(u, a, name, desc)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(up, in); err != nil {
		up.Abort()
		return nil, err
	}
	return up.Commit()
}

// Path is where a file's contents are kept.
func (s *Store) Path(f *store.File) string { return s.path(f.ID) }

// Put adds a file from disk to an area as it came from elsewhere (a file
// echo, from origin): no user's limits but the size limit, a file of the
// same name replaced (not one uploaded here), the description its own or
// else the archive's FILE_ID.DIZ.
func (s *Store) Put(a *store.FileArea, path, name, desc, longDesc, uploader, origin string) (*store.File, error) {
	if msg := ValidName(name); msg != "" {
		return nil, errors.New(msg)
	}
	in, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Join(s.Dir, "tmp"), "put-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(in, s.MaxSize+1))
	if err == nil {
		err = tmp.Close()
	} else {
		tmp.Close()
	}
	if err != nil {
		return nil, err
	}
	if n > s.MaxSize {
		return nil, ErrTooLarge
	}
	f := &store.File{AreaID: a.ID, Name: name, Description: desc, LongDesc: longDesc, Size: n,
		SHA256: hex.EncodeToString(h.Sum(nil)), Uploader: uploader, Origin: origin}
	if f.LongDesc == "" {
		f.LongDesc = diz(tmp.Name(), name)
	}
	if f.Description == "" {
		f.Description = short(f.LongDesc)
	}
	if old, err := s.DB.FileByName(a.ID, name); err == nil {
		if old.Origin == "" && origin != "" {
			return nil, errors.New("a file here is called " + name)
		}
		if err := s.DB.DeleteFile(old.ID); err != nil {
			return nil, err
		}
		os.Remove(s.path(old.ID))
	}
	id, err := s.DB.AddFile(f)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), s.path(id)); err != nil {
		s.DB.DeleteFile(id)
		return nil, err
	}
	return s.DB.FileByID(id)
}

// Size is n bytes for people: 512 B, 1.5 KB, 23 MB.
func Size(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
}
