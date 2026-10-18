package backup

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Targets are where a restore puts what an archive has: this system's
// places, as its configuration says (not the archive's: it may be another
// machine's).
type Targets struct {
	Config   string            // the configuration file ("": not restored)
	Database string            // the database's address ("": not restored)
	Dirs     map[string]string // each directory's (or key file's) place, by its name (one not here: not restored)
	// Owner: what is restored (but the configuration) is given to this
	// user and group (a restore as root for a BBS that runs as its own)
	Owner    bool
	UID, GID int
}

// own gives p (and all under it) to the owner, if there is one.
func (to Targets) own(p string) error {
	if !to.Owner {
		return nil
	}
	return filepath.WalkDir(p, func(q string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(q, to.UID, to.GID)
	})
}

// Restore puts an archive back, the BBS stopped.  Each file or directory
// there now is renamed aside first (NAME.before-restore-STAMP), never
// written over; a server's database must be new and empty, and the
// archive's is copied into it.  say is told what is done.
func Restore(archive string, to Targets, now time.Time, say func(string)) error {
	if _, err := Manifest(archive); err != nil {
		return err
	}
	// first: every name in it one that stays where it is put
	hasDB := false
	if err := walk(archive, func(h *tar.Header, r io.Reader) error {
		if !clean(h.Name) {
			return fmt.Errorf("%s: %q: not a name a backup has", archive, h.Name)
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return fmt.Errorf("%s: %q: not a file or a directory", archive, h.Name)
		}
		hasDB = hasDB || h.Name == "database.db"
		return nil
	}); err != nil {
		return err
	}
	// a server's database is copied into, so it must be new: said before
	// anything is unpacked or changed
	if hasDB && to.Database != "" && !store.IsFile(to.Database) {
		if err := emptyServer(to.Database); err != nil {
			return err
		}
	}
	stamp := ".before-restore-" + now.Format("20060102-150405")
	work, err := os.MkdirTemp("", "bbs-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	// each thing read into a staging place beside where it goes (a rename
	// then puts it there), the database into work
	staged := map[string]string{} // top-level name: its staging path
	stage := func(top string, file bool) (string, error) {
		if s, ok := staged[top]; ok {
			return s, nil
		}
		var s string
		switch {
		case top == "bbs.conf":
			s = to.Config + ".restore-tmp"
		case top == "database.db":
			s = filepath.Join(work, "database.db")
		default:
			s = to.Dirs[top] + ".restore-tmp"
			os.RemoveAll(s) // a restore's that stopped half way
			if file {
				break
			}
			if err := os.MkdirAll(s, 0o700); err != nil {
				return "", err
			}
		}
		staged[top] = s
		return s, nil
	}
	defer func() {
		for top, s := range staged {
			if top != "database.db" {
				os.RemoveAll(s)
			}
		}
	}()
	err = walk(archive, func(h *tar.Header, r io.Reader) error {
		top, rest, _ := strings.Cut(strings.TrimSuffix(h.Name, "/"), "/")
		switch {
		case top == manifest:
			return nil
		case top == "bbs.conf" && to.Config == "", top == "database.db" && to.Database == "":
			return nil
		case top != "bbs.conf" && top != "database.db" && to.Dirs[top] == "":
			return nil
		}
		s, err := stage(top, rest == "" && h.Typeflag == tar.TypeReg)
		if err != nil {
			return err
		}
		dst := s
		if rest != "" {
			dst = filepath.Join(s, filepath.FromSlash(rest))
		}
		if h.Typeflag == tar.TypeDir {
			return os.MkdirAll(dst, 0o700)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(h.Mode).Perm()|0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, r); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	})
	if err != nil {
		return err
	}
	// the database first: a server's may refuse (not empty), and then
	// nothing else is changed
	if s, ok := staged["database.db"]; ok {
		if err := restoreDB(s, to.Database, stamp, say); err != nil {
			return fmt.Errorf("the database: %w", err)
		}
		if store.IsFile(to.Database) {
			if err := to.own(strings.TrimPrefix(to.Database, "sqlite:")); err != nil {
				return err
			}
		}
	}
	for top, s := range staged {
		if top == "database.db" {
			continue
		}
		dst := to.Config
		if top != "bbs.conf" {
			dst = to.Dirs[top]
		}
		if err := aside(dst, stamp, say); err != nil {
			return err
		}
		if err := os.Rename(s, dst); err != nil {
			return err
		}
		if top != "bbs.conf" {
			if err := to.own(dst); err != nil {
				return err
			}
		}
		say(fmt.Sprintf("%s restored to %s", top, dst))
	}
	return nil
}

// clean: a relative name of the archive's that goes nowhere above it
func clean(name string) bool {
	n := strings.TrimSuffix(name, "/")
	return n != "" && !strings.HasPrefix(n, "/") && !strings.Contains(n, "\\") &&
		path.Clean(n) == n && n != ".." && !strings.HasPrefix(n, "../")
}

// aside renames what is at p (if anything) to p+stamp.
func aside(p, stamp string, say func(string)) error {
	if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := os.Rename(p, p+stamp); err != nil {
		return err
	}
	say(fmt.Sprintf("%s was there: now %s", p, p+stamp))
	return nil
}

// emptyServer checks that the database server's database at addr is new and
// empty, as a restore into it needs: what to do if it is not.
func emptyServer(addr string) error {
	db, err := store.Open(addr)
	if err != nil {
		return fmt.Errorf("the database: %w", err)
	}
	defer db.Close()
	empty, err := db.Empty()
	if err != nil {
		return fmt.Errorf("the database: %w", err)
	}
	if !empty {
		return fmt.Errorf("the database %s has users: a server's database is restored into a new, empty one "+
			"(made as for bbs db copy). Make one, point bbs.conf's database at it, and restore again; "+
			"nothing has been changed", store.Describe(addr))
	}
	return nil
}

// restoreDB puts the snapshot back as the database at addr: a SQLite file
// (with its -wal and -shm) set aside and the snapshot copied in its place;
// any other engine's, copied into, so it must be empty.
func restoreDB(snap, addr, stamp string, say func(string)) error {
	if store.IsFile(addr) {
		p := strings.TrimPrefix(addr, "sqlite:")
		for _, x := range []string{p, p + "-wal", p + "-shm"} {
			if err := aside(x, stamp, say); err != nil {
				return err
			}
		}
		if err := copyFile(snap, p); err != nil {
			return err
		}
		say("database restored to " + p)
		return nil
	}
	src, err := store.Open("sqlite:" + snap)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := store.Open(addr)
	if err != nil {
		return err
	}
	defer dst.Close()
	if err := store.Copy(src, dst, nil); err != nil {
		return err
	}
	say("database restored into " + store.Describe(addr))
	return nil
}

// copyFile copies src to dst, a file that must not be there yet (mode 0600).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
