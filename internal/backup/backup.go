// Package backup keeps copies of the BBS: the configuration file, the
// database (as a SQLite file, whatever its engine, taken whole at one
// moment while callers are on) and the directories of files, in one
// gzipped tar archive a backup, the oldest taken away past a number kept.
// Restore puts an archive back where this system's configuration says,
// what is there renamed aside, never written over.
package backup

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Source is a directory of the BBS's files, or a file of its own (an SSH
// host key): its name in the archive, and where it is.
type Source struct {
	Name string // files, chan, fido, screens, scripts; key.ssh_host_ed25519_key
	Path string
}

// Plan is what a backup takes, and where it goes.
type Plan struct {
	Dir    string       // where the archives are kept
	Keep   int          // archives kept (0: all)
	Config string       // the configuration file ("": none)
	Store  *store.Store // the database (nil: none)
	Dirs   []Source     // the files' directories (none: the files are not taken)
	Keys   []Source     // files taken whatever Dirs says: the SSH host keys
}

// Archive is a backup made.
type Archive struct {
	Path string
	Time time.Time
	Size int64
}

// Name is an archive's name for a time.
func Name(t time.Time) string { return "bbs-" + t.Format("20060102-150405") + ".tar.gz" }

const manifest = "MANIFEST"

// Run makes a backup now: written beside its final name and renamed to it
// when whole, so a backup there is a backup made.  Then the oldest past
// Keep are taken away.
func (p *Plan) Run(now time.Time) (*Archive, error) {
	if p.Dir == "" {
		return nil, errors.New("no backup directory")
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return nil, err
	}
	final := filepath.Join(p.Dir, Name(now))
	if _, err := os.Stat(final); err == nil {
		return nil, fmt.Errorf("%s is there already", final)
	}
	tmp, err := os.CreateTemp(p.Dir, ".backup-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if err := p.write(tmp, now); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return nil, err
	}
	fi, err := os.Stat(final)
	if err != nil {
		return nil, err
	}
	if p.Keep > 0 {
		if err := Prune(p.Dir, p.Keep); err != nil {
			return nil, fmt.Errorf("made %s, but the old ones: %w", final, err)
		}
	}
	return &Archive{Path: final, Time: now, Size: fi.Size()}, nil
}

// write writes the backup to w as a gzipped tar: the manifest first, then
// the configuration, a snapshot of the database, and the directories and
// keys (those not there yet left out, of the manifest too).
func (p *Plan) write(w io.Writer, now time.Time) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	var m strings.Builder
	fmt.Fprintf(&m, "bbs backup\nmade = %s\n", now.Format(time.RFC3339))
	if p.Config != "" {
		fmt.Fprintf(&m, "config = %s\n", p.Config)
	}
	if p.Store != nil {
		fmt.Fprintf(&m, "database = %s\n", p.Store.Engine())
	}
	for _, d := range append(append([]Source(nil), p.Dirs...), p.Keys...) {
		if _, err := os.Stat(d.Path); err == nil {
			fmt.Fprintf(&m, "%s = %s\n", d.Name, d.Path)
		}
	}
	if err := addBytes(tw, manifest, []byte(m.String()), now); err != nil {
		return err
	}
	if p.Config != "" {
		if err := addFile(tw, "bbs.conf", p.Config); err != nil {
			return fmt.Errorf("the configuration: %w", err)
		}
	}
	if p.Store != nil {
		dir, err := os.MkdirTemp(p.Dir, ".snapshot-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		snap := filepath.Join(dir, "database.db")
		if err := p.Store.Snapshot(snap); err != nil {
			return fmt.Errorf("the database: %w", err)
		}
		if err := addFile(tw, "database.db", snap); err != nil {
			return fmt.Errorf("the database: %w", err)
		}
	}
	for _, d := range append(append([]Source(nil), p.Dirs...), p.Keys...) {
		if err := addDir(tw, d.Name, d.Path, p.Dir); err != nil {
			return fmt.Errorf("%s: %w", d.Name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// addBytes adds data to the archive as a file name, made at t.
func addBytes(tw *tar.Writer, name string, data []byte, t time.Time) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: t,
		Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// addFile adds the file src to the archive as name, its mode and time kept:
// as much as it had when opened, though it grows.
func addFile(tw *tar.Writer, name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: int64(fi.Mode().Perm()), Size: fi.Size(),
		ModTime: fi.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	// a file that grows while it is read: what it had at the start
	_, err = io.CopyN(tw, f, fi.Size())
	return err
}

// addDir adds a directory's files and directories under name (not one it
// is in itself: a backup directory kept among the files); or a file, as
// name.
func addDir(tw *tar.Writer, name, dir, skip string) error {
	fi, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil // nothing there yet
	}
	if err == nil && fi.Mode().IsRegular() {
		return addFile(tw, name, dir)
	}
	skipAbs, _ := filepath.Abs(skip)
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if abs, _ := filepath.Abs(p); abs == skipAbs {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		arc := path.Join(name, filepath.ToSlash(rel))
		switch {
		case d.IsDir():
			fi, err := d.Info()
			if err != nil {
				return err
			}
			return tw.WriteHeader(&tar.Header{Name: arc + "/", Mode: int64(fi.Mode().Perm()), ModTime: fi.ModTime(),
				Typeflag: tar.TypeDir})
		case d.Type().IsRegular():
			return addFile(tw, arc, p)
		}
		return nil // sockets, links: not the BBS's files
	})
}

// List is the archives in dir, the newest first.
func List(dir string) ([]Archive, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Archive
	for _, e := range ents {
		t, ok := timeOf(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Archive{Path: filepath.Join(dir, e.Name()), Time: t, Size: fi.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

// timeOf is when the archive name says it was made, and whether name is an
// archive's (as Name makes them).
func timeOf(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, "bbs-") || !strings.HasSuffix(name, ".tar.gz") {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("20060102-150405", strings.TrimSuffix(strings.TrimPrefix(name, "bbs-"), ".tar.gz"), time.Local)
	return t, err == nil
}

// Prune takes away all but the keep newest archives in dir.
func Prune(dir string, keep int) error {
	as, err := List(dir)
	if err != nil {
		return err
	}
	for i := keep; i < len(as); i++ {
		if err := os.Remove(as[i].Path); err != nil {
			return err
		}
	}
	return nil
}

// Manifest is what an archive says it has: name = value lines (made,
// config, database, and each directory's name and where it was).
func Manifest(archive string) (map[string]string, error) {
	out := map[string]string{}
	err := walk(archive, func(h *tar.Header, r io.Reader) error {
		if h.Name != manifest {
			return nil
		}
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			if k, v, ok := strings.Cut(sc.Text(), " = "); ok {
				out[k] = v
			}
		}
		return errStop
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, err
	}
	if out["made"] == "" {
		return nil, fmt.Errorf("%s: not a BBS backup", archive)
	}
	return out, nil
}

var errStop = errors.New("stop")

// walk calls f with each file of the gzipped tar archive in turn, stopping
// at the first error f gives (errStop to stop early) and returning it.
func walk(archive string, f func(h *tar.Header, r io.Reader) error) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("%s: %w", archive, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", archive, err)
		}
		if err := f(h, tr); err != nil {
			return err
		}
	}
}
