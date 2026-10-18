package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/archive"
)

// zipOf is a ZIP of those of FILE_ID.DIZ, doom.exe, sub/FILE_ID.DIZ and
// readme.txt given, in that order, dated 1994.
func zipOf(t *testing.T, files map[string][]byte) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, n := range []string{"FILE_ID.DIZ", "doom.exe", "sub/FILE_ID.DIZ", "readme.txt"} {
		data, ok := files[n]
		if !ok {
			continue
		}
		w, err := z.CreateHeader(&zip.FileHeader{Name: n, Modified: time.Date(1994, 12, 10, 12, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	z.Close()
	return b.Bytes()
}

// TestDIZAndListing: a ZIP's top FILE_ID.DIZ, made UTF-8 and plain (no ANSI,
// no DOS end of file), is the long description and its first line the short
// one unless one was given; a ZIP and a tar.gz are listed; a file that is not
// an archive, or only named like one, is ErrNotArchive.
func TestDIZAndListing(t *testing.T) {
	s, alice, _, a := setup(t, 1<<20, 0)
	// CP437 box drawing, an ANSI color, a DOS end of file
	dizText := []byte("\r\n\xc9\xcd\xcd\xbb\r\n DOOM v1.9 \x1b[1;31mshareware\x1b[0m\r\n id Software\r\n\r\n\x1a")
	data := zipOf(t, map[string][]byte{"FILE_ID.DIZ": dizText, "doom.exe": bytes.Repeat([]byte{1}, 1000),
		"sub/FILE_ID.DIZ": []byte("not this one")})
	up, err := s.Upload(alice, a, "doom19s.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	up.Write(data)
	f, err := up.Commit()
	if err != nil {
		t.Fatal(err)
	}
	if f.LongDesc != "╔══╗\n DOOM v1.9 shareware\n id Software" {
		t.Errorf("long %q", f.LongDesc)
	}
	if f.Description != "DOOM v1.9 shareware" {
		t.Errorf("short %q", f.Description)
	}
	es, err := s.Listing(f)
	if err != nil || len(es) != 3 || es[1].Name != "doom.exe" || es[1].Size != 1000 || es[1].Modified.Year() != 1994 {
		t.Fatalf("listing %+v %v", es, err)
	}

	// a description given stays; the DIZ is still kept
	up, _ = s.Upload(alice, a, "two.zip", "my words")
	up.Write(zipOf(t, map[string][]byte{"FILE_ID.DIZ": []byte("Theirs\n")}))
	f, _ = up.Commit()
	if f.Description != "my words" || f.LongDesc != "Theirs" {
		t.Errorf("given %q %q", f.Description, f.LongDesc)
	}

	// a tar.gz
	var tb bytes.Buffer
	gz := gzip.NewWriter(&tb)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "file_id.diz", Size: 9, Mode: 0644})
	tw.Write([]byte("From tar\n"))
	tw.WriteHeader(&tar.Header{Name: "src/", Typeflag: tar.TypeDir, Mode: 0755})
	tw.Close()
	gz.Close()
	f, err = put(s, alice, a, "src.tar.gz", tb.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if es, err := s.Listing(f); err != nil || len(es) != 2 || !es[1].Dir {
		t.Errorf("tar listing %+v %v", es, err)
	}
	if f.LongDesc != "From tar" {
		t.Errorf("tar diz %q", f.LongDesc)
	}

	// not archives
	f, _ = put(s, alice, a, "notes.txt", []byte("hello"))
	if _, err := s.Listing(f); !errors.Is(err, ErrNotArchive) {
		t.Errorf("text: %v", err)
	}
	f, _ = put(s, alice, a, "fake.zip", []byte("not a zip at all"))
	if _, err := s.Listing(f); !errors.Is(err, ErrNotArchive) || f.LongDesc != "" {
		t.Errorf("fake zip: %v %q", err, f.LongDesc)
	}
	if !strings.Contains(ErrNotArchive.Error(), "ZIP") {
		t.Error(ErrNotArchive)
	}
}

// TestOldArchives: an LHA's and an ARJ's FILE_ID.DIZ, and their listings,
// as a ZIP's.
func TestOldArchives(t *testing.T) {
	s, alice, _, a := setup(t, 1<<20, 0)
	when := time.Date(1995, 3, 4, 10, 0, 0, 0, time.Local)
	for _, k := range []archive.Kind{archive.LHA, archive.ARJ, archive.ARC} {
		var b bytes.Buffer
		archive.Write(&b, k, "game."+string(k), []archive.File{
			{Name: "FILE_ID.DIZ", Data: []byte("Lord v4.00\r\nA door game\r\n"), Modified: when},
			{Name: "lord.exe", Data: bytes.Repeat([]byte{2}, 500), Modified: when}})
		name := "lord." + map[archive.Kind]string{archive.LHA: "lzh", archive.ARJ: "arj", archive.ARC: "arc"}[k]
		up, err := s.Upload(alice, a, name, "")
		if err != nil {
			t.Fatal(err)
		}
		up.Write(b.Bytes())
		f, err := up.Commit()
		if err != nil {
			t.Fatal(err)
		}
		if f.Description != "Lord v4.00" || f.LongDesc != "Lord v4.00\nA door game" {
			t.Errorf("%s: %q, %q", k, f.Description, f.LongDesc)
		}
		es, err := s.Listing(f)
		if err != nil || len(es) != 2 || !strings.EqualFold(es[1].Name, "lord.exe") || es[1].Size != 500 || es[1].Modified.Year() != 1995 {
			t.Errorf("%s: listing %+v %v", k, es, err)
		}
	}
}
