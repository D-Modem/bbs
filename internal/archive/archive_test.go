package archive

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The testdata archives were made by the real programs (arc 5.21q, ARJ32
// 3.10, jlha 0.1.6) from msgs.pkt, small.txt and rle.bin, and an empty
// file.
func originals(t *testing.T) map[string][]byte {
	t.Helper()
	m := map[string][]byte{"empty.pkt": {}}
	for _, n := range []string{"msgs.pkt", "small.txt", "rle.bin"} {
		b, err := os.ReadFile(filepath.Join("testdata", n))
		if err != nil {
			t.Fatal(err)
		}
		m[n] = b
	}
	return m
}

// TestRead: archives made by the real ARC, ARJ and LHA programs are detected
// by kind, and each entry unpacks to the original file, its size and date as
// the header says, with the methods each was packed by.
func TestRead(t *testing.T) {
	want := originals(t)
	for _, c := range []struct {
		file    string
		kind    Kind
		names   string
		methods string
	}{
		{"crunch.arc", ARC, "empty.pkt msgs.pkt small.txt", "crunched packed stored"},
		{"squash.arc", ARC, "msgs.pkt small.txt", "packed squashed"},
		{"store.arc", ARC, "rle.bin small.txt", "stored"},
		{"mixed.arc", ARC, "rle.bin sq.bin", "crunched"},
		{"m0.arj", ARJ, "rle.bin small.txt", "stored"},
		{"m1.arj", ARJ, "msgs.pkt rle.bin small.txt", "method 1"},
		{"m2.arj", ARJ, "msgs.pkt rle.bin small.txt", "method 2"},
		{"m3.arj", ARJ, "msgs.pkt rle.bin small.txt", "method 3"},
		{"m4.arj", ARJ, "msgs.pkt rle.bin small.txt", "method 4"},
		{"empty.arj", ARJ, "empty.pkt small.txt", "method 1 stored"},
		{"lh0.lzh", LHA, "rle.bin small.txt", "-lh0-"},
		{"lh5.lzh", LHA, "msgs.pkt rle.bin small.txt", "-lh5-"},
		{"lh5l0.lzh", LHA, "msgs.pkt small.txt", "-lh5-"},
		{"lh5l1.lzh", LHA, "msgs.pkt small.txt", "-lh5-"},
		{"lh6.lzh", LHA, "msgs.pkt rle.bin small.txt", "-lh6-"},
		{"lh7.lzh", LHA, "msgs.pkt rle.bin small.txt", "-lh7-"},
	} {
		b, err := os.ReadFile(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatal(err)
		}
		if k := Detect(b); k != c.kind {
			t.Errorf("%s: detected as %q", c.file, k)
		}
		var names []string
		methods := map[string]bool{}
		err = WalkBytes(b, func(e Entry, open Opener) bool {
			names = append(names, e.Name)
			methods[e.Method] = true
			data, err := open(1 << 20)
			if err != nil {
				t.Errorf("%s: %s (%s): %v", c.file, e.Name, e.Method, err)
				return true
			}
			if int64(len(data)) != e.Size {
				t.Errorf("%s: %s: %d bytes, the header says %d", c.file, e.Name, len(data), e.Size)
			}
			if w, ok := want[e.Name]; ok && !bytes.Equal(data, w) {
				t.Errorf("%s: %s (%s) unpacks wrong", c.file, e.Name, e.Method)
			}
			if e.Modified.Year() != time.Now().Year() && e.Modified.Year() < 2020 {
				t.Errorf("%s: %s: dated %v", c.file, e.Name, e.Modified)
			}
			return true
		})
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
		}
		sort.Strings(names)
		if got := strings.Join(names, " "); got != c.names {
			t.Errorf("%s: entries %q, want %q", c.file, got, c.names)
		}
		var ms []string
		for m := range methods {
			ms = append(ms, m)
		}
		sort.Strings(ms)
		if got := strings.Join(ms, " "); got != c.methods {
			t.Errorf("%s: methods %q, want %q", c.file, got, c.methods)
		}
	}
}

// TestWrite: each kind written, then read back.
func TestWrite(t *testing.T) {
	want := originals(t)
	files := []File{{Name: "msgs.pkt", Data: want["msgs.pkt"], Modified: time.Now()},
		{Name: "small.txt", Data: want["small.txt"], Modified: time.Now()},
		{Name: "empty.pkt", Data: nil, Modified: time.Now()}}
	for _, k := range []Kind{ZIP, ARC, ARJ, LHA} {
		var b bytes.Buffer
		if err := Write(&b, k, "out."+string(k), files); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		if got := Detect(b.Bytes()); got != k {
			t.Errorf("%s: detected as %q", k, got)
		}
		n := 0
		err := WalkBytes(b.Bytes(), func(e Entry, open Opener) bool {
			name := strings.ToLower(e.Name)
			data, err := open(1 << 20)
			if err != nil || !bytes.Equal(data, want[name]) {
				t.Errorf("%s: %s: %v", k, e.Name, err)
			}
			n++
			return true
		})
		if err != nil || n != 3 {
			t.Errorf("%s: %d entries, %v", k, n, err)
		}
	}
}

// TestHostile: damage, lies about sizes and truncation end in errors,
// never in a panic or a great deal of memory.
func TestHostile(t *testing.T) {
	ents, _ := filepath.Glob("testdata/*.*")
	for _, p := range ents {
		b, _ := os.ReadFile(p)
		if Detect(b) == "" {
			continue
		}
		for _, cut := range []int{len(b) / 3, len(b) - 7, 40} {
			if cut <= 0 || cut >= len(b) {
				continue
			}
			WalkBytes(b[:cut], func(e Entry, open Opener) bool { open(1 << 20); return true })
		}
		for i := 30; i < len(b); i += 97 { // a byte changed here and there
			x := append([]byte(nil), b...)
			x[i] ^= 0x5A
			WalkBytes(x, func(e Entry, open Opener) bool { open(1 << 20); return true })
		}
	}
	// a size past the limit is refused before anything is unpacked
	b, _ := os.ReadFile("testdata/m1.arj")
	err := WalkBytes(b, func(e Entry, open Opener) bool {
		if e.Name == "msgs.pkt" {
			if _, err := open(100); !errors.Is(err, ErrTooBig) {
				t.Errorf("a big entry: %v", err)
			}
		}
		return true
	})
	if err != nil {
		t.Error(err)
	}
	// a damaged entry fails its CRC
	b, _ = os.ReadFile("testdata/store.arc")
	b[len(b)-10] ^= 1
	bad := false
	WalkBytes(b, func(e Entry, open Opener) bool {
		if _, err := open(1 << 20); errors.Is(err, ErrChecksum) {
			bad = true
		}
		return true
	})
	if !bad {
		t.Error("a damaged entry passed")
	}
	if err := WalkBytes([]byte("not an archive at all"), func(Entry, Opener) bool { return true }); !errors.Is(err, ErrFormat) {
		t.Errorf("text: %v", err)
	}
}

// TestSqueezed: ARC's method 4, which arc 5.21 no longer chooses, from a
// tree made by hand: a is 0, b is 10, the end is 11 (first bit lowest).
func TestSqueezed(t *testing.T) {
	sq := []byte{2, 0, // two nodes
		0x9E, 0xFF, 1, 0, // node 0: a (-98), node 1
		0x9D, 0xFF, 0xFF, 0xFE, // node 1: b (-99), the end (-257)
		0x34} // 0 0 10 11: a a b, the end
	var arc []byte
	h := make([]byte, 29)
	h[0], h[1] = 0x1A, 4
	copy(h[2:], "AAB.TXT")
	put32(h[15:], uint32(len(sq)))
	put16(h[19:], 0x5B4D)
	put16(h[23:], crc16([]byte("aab")))
	put32(h[25:], 3)
	arc = append(append(append(arc, h...), sq...), 0x1A, 0)
	got := ""
	err := WalkBytes(arc, func(e Entry, open Opener) bool {
		b, err := open(100)
		if err != nil || e.Method != "squeezed" {
			t.Errorf("%s: %v", e.Method, err)
		}
		got = string(b)
		return true
	})
	if err != nil || got != "aab" {
		t.Errorf("%q, %v", got, err)
	}
}

// FuzzWalk: no archive, however made, panics or unpacks past its limit.
func FuzzWalk(f *testing.F) {
	ents, _ := filepath.Glob("testdata/*.*")
	for _, p := range ents {
		if b, _ := os.ReadFile(p); Detect(b) != "" && len(b) < 2000 {
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		WalkBytes(b, func(e Entry, open Opener) bool {
			if data, err := open(1 << 16); err == nil && len(data) > 1<<16 {
				t.Fatalf("%d bytes past the limit", len(data))
			}
			return true
		})
	})
}
