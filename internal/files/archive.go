package files

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/Neo-Desktop/bbs-sec/internal/archive"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// ErrNotArchive: the file is not an archive the BBS looks into.
var ErrNotArchive = errors.New("not an archive the BBS can look into (ZIP, ARC, ARJ, LHA, tar, tar.gz)")

// Entry is a file in an archive.
type Entry struct {
	Name     string
	Size     int64
	Modified time.Time
	Dir      bool
}

// MaxEntries is the most of an archive listed.
const MaxEntries = 2000

// maxDIZ is the most of a FILE_ID.DIZ read.
const maxDIZ = 4096

// archiveKind is how a file of that name is read: "zip", "arc", "arj",
// "lha", "tar", "tgz" or "".
func archiveKind(name string) string {
	n := strings.ToLower(name)
	if k := archive.KindByName(n); k != "" {
		return string(k)
	}
	switch {
	case strings.HasSuffix(n, ".tar"):
		return "tar"
	case strings.HasSuffix(n, ".tar.gz"), strings.HasSuffix(n, ".tgz"):
		return "tgz"
	}
	return ""
}

// IsArchive: Listing reads files of this name.
func IsArchive(name string) bool { return archiveKind(name) != "" }

// walk calls fn for each file in the archive at p (named name), with a
// reader of its contents, until fn says stop or MaxEntries.
func walk(p, name string, fn func(e Entry, open func() (io.Reader, error)) bool) error {
	switch archiveKind(name) {
	case "zip", "arc", "arj", "lha":
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		fi, err := in.Stat()
		if err != nil {
			return err
		}
		i := 0
		err = archive.Walk(in, fi.Size(), func(e archive.Entry, open archive.Opener) bool {
			if i++; i > MaxEntries {
				return false
			}
			return fn(Entry{Name: e.Name, Size: e.Size, Modified: e.Modified, Dir: e.Dir}, func() (io.Reader, error) {
				b, err := open(maxDIZ * 4)
				if err != nil {
					return nil, err
				}
				return bytes.NewReader(b), nil
			})
		})
		if err != nil && i == 0 {
			return ErrNotArchive
		}
		return nil
	case "tar", "tgz":
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		var r io.Reader = in
		if archiveKind(name) == "tgz" {
			gz, err := gzip.NewReader(in)
			if err != nil {
				return ErrNotArchive
			}
			defer gz.Close()
			r = gz
		}
		tr := tar.NewReader(r)
		for i := 0; i < MaxEntries; i++ {
			h, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				if i == 0 {
					return ErrNotArchive
				}
				return nil // what there was of it
			}
			e := Entry{Name: h.Name, Size: h.Size, Modified: h.ModTime, Dir: h.Typeflag == tar.TypeDir}
			if !fn(e, func() (io.Reader, error) { return tr, nil }) {
				return nil
			}
		}
		return nil
	}
	return ErrNotArchive
}

// Listing is what is in a file that is an archive (at most MaxEntries).
func (s *Store) Listing(f *store.File) ([]Entry, error) {
	var out []Entry
	err := walk(s.path(f.ID), f.Name, func(e Entry, _ func() (io.Reader, error)) bool {
		e.Name = cleanLine(e.Name)
		if e.Modified.Year() < 1980 {
			e.Modified = time.Time{} // none: a ZIP's zero is 1979-11-30
		}
		out = append(out, e)
		return true
	})
	return out, err
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b.`)

// cleanLine is a line fit to show: no escapes or control characters, no
// spaces at the end.
func cleanLine(s string) string {
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimRight(s, " ")
}

// diz is the FILE_ID.DIZ in the archive at p (or ""): its lines, decoded
// from CP437 (or as UTF-8 where they are), cleaned, at most 20.
func diz(p, name string) string {
	var text string
	walk(p, name, func(e Entry, open func() (io.Reader, error)) bool {
		if !strings.EqualFold(path.Base(e.Name), "FILE_ID.DIZ") || strings.Contains(strings.Trim(e.Name, "/"), "/") {
			return true
		}
		r, err := open()
		if err != nil {
			return false
		}
		b, _ := io.ReadAll(io.LimitReader(r, maxDIZ))
		if c, ok := r.(io.Closer); ok {
			c.Close()
		}
		if utf8.Valid(b) {
			text = string(b)
		} else {
			var sb strings.Builder
			for _, c := range b {
				sb.WriteRune(charmap.CodePage437.DecodeByte(c))
			}
			text = sb.String()
		}
		return false
	})
	if text == "" {
		return ""
	}
	text = strings.TrimSuffix(text, "\x1a") // DOS's end of file
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		lines = append(lines, cleanLine(l))
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 20 {
		lines = lines[:20]
	}
	return strings.Join(lines, "\n")
}

// short is a description's first line that says something, cut to 60.
func short(long string) string {
	for _, l := range strings.Split(long, "\n") {
		if l = strings.TrimSpace(l); strings.IndexFunc(l, unicode.IsLetter) >= 0 {
			if r := []rune(l); len(r) > 60 {
				l = string(r[:60])
			}
			return l
		}
	}
	return ""
}
