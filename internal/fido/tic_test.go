package fido

import (
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// TestFileEchoes: a node hatches a file into a file echo; its hub (create
// = yes) makes the file area, takes the file in and passes it on to
// another node, which has the area; a dupe and a file that does not
// match its TIC are not taken.
func TestFileEchoes(t *testing.T) {
	n200l := &Link{Name: "n200", Addr: mustAddr("21:1/200"), Password: "pw", Echoes: "*", FileEchoes: "*",
		TicPassword: "TICPW", Create: true}
	n300l := &Link{Name: "n300", Addr: mustAddr("21:1/300"), Password: "pw", Echoes: "*", FileEchoes: "*"}
	hub := newSystem(t, "21:1/100", n200l, n300l)
	up200 := &Link{Name: "hub", Addr: mustAddr("21:1/100"), Host: hub.host, Password: "pw", Echoes: "*",
		FileEchoes: "*", TicPassword: "TICPW"}
	n200 := newSystem(t, "21:1/200", up200)
	up300 := &Link{Name: "hub", Addr: mustAddr("21:1/100"), Host: hub.host, Password: "pw", Echoes: "*",
		FileEchoes: "*"}
	n300 := newSystem(t, "21:1/300", up300)

	mk := func(s *system, name string) *store.FileArea {
		a, err := s.f.B.Store.CreateFileArea(name, "Node files", false)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.f.B.Store.SetFileEcho(a.ID, "NODEFILES"); err != nil {
			t.Fatal(err)
		}
		return a
	}
	a200, a300 := mk(n200, "Uploads"), mk(n300, "Nodefiles")
	for _, s := range []*system{hub, n200, n300} {
		s.f.ScanAll() // marks where hatching starts
	}

	// a file uploaded at the node goes out to the hub
	src := filepath.Join(t.TempDir(), "README.TXT")
	os.WriteFile(src, []byte("Hello from the node.\r\n"), 0600)
	if _, err := n200.f.B.Files.Put(a200, src, "README.TXT", "A greeting", "", "alice", ""); err != nil {
		t.Fatal(err)
	}
	if n, err := n200.f.Scan(up200); err != nil || n != 1 {
		t.Fatalf("hatched %d: %v", n, err)
	}
	n200.call(t, up200, hub)
	ha, err := hub.f.B.Store.FileAreaByEcho("NODEFILES")
	if err != nil {
		t.Fatalf("no file area made at the hub: %v", err)
	}
	fs, _ := hub.f.B.Store.Files(ha.ID)
	if len(fs) != 1 || fs[0].Name != "README.TXT" || fs[0].Description != "A greeting" || fs[0].Origin != "ftn:n200" ||
		fs[0].Uploader != "21:1/200" {
		t.Fatalf("at the hub: %+v", fs)
	}
	if !strings.Contains(hub.log.String(), "1 files") {
		t.Errorf("not counted:\n%s", hub.log)
	}

	// passed on to the other node, and not hatched again
	if n, _ := hub.f.Scan(n300l); n != 0 {
		t.Errorf("the hub hatched %d", n)
	}
	n300.call(t, up300, hub)
	fs, _ = n300.f.B.Store.Files(a300.ID)
	if len(fs) != 1 || fs[0].Name != "README.TXT" || fs[0].Origin != "ftn:hub" {
		t.Fatalf("at n300: %+v\n%s", fs, n300.log)
	}
	b, _ := os.ReadFile(n300.f.B.Files.Path(fs[0]))
	if string(b) != "Hello from the node.\r\n" {
		t.Errorf("contents %q", b)
	}
	// n300 does not send it back: the hub saw it
	if n, _ := n300.f.Scan(up300); n != 0 {
		t.Errorf("n300 hatched %d", n)
	}
	// nor does the hub send it back to n200, which hatched it
	if ents, _ := os.ReadDir(hub.f.outbound(n200l)); len(visible(ents)) != 0 {
		t.Errorf("hub's outbound for n200: %v", visible(ents))
	}

	// a TIC dropped in the hub's inbound from n200 by hand: wrong CRC, then
	// right, then the same again
	in := hub.f.inbound(n200l)
	data := []byte("other data")
	drop := func(crc uint32) {
		os.WriteFile(filepath.Join(in, "OTHER.ZIP"), data, 0600)
		tic := &ftn.TIC{Area: "NODEFILES", Origin: mustAddr("21:1/200"), From: mustAddr("21:1/200"), File: "OTHER.ZIP",
			Desc: "Other", Size: int64(len(data)), CRC: crc, HasCRC: true, Pw: "TICPW",
			SeenBy: []ftn.Addr{mustAddr("21:1/200")}}
		os.WriteFile(filepath.Join(in, "0000abcd.tic"), tic.Bytes(), 0600)
	}
	drop(1234)
	st, _ := hub.f.Toss(n200l)
	if st.Bad != 1 || !exists(filepath.Join(hub.f.S.Dir, "bad", "n200-OTHER.ZIP")) {
		t.Fatalf("a wrong CRC: %+v", st)
	}
	drop(crc32.ChecksumIEEE(data))
	if st, _ = hub.f.Toss(n200l); st.Files != 1 {
		t.Fatalf("taken: %+v\n%s", st, hub.log)
	}
	drop(crc32.ChecksumIEEE(data))
	if st, _ = hub.f.Toss(n200l); st.Dupes != 1 || st.Files != 0 {
		t.Fatalf("a dupe: %+v", st)
	}
	if ents, _ := os.ReadDir(in); len(visible(ents)) != 0 {
		t.Errorf("left in the inbound: %v", visible(ents))
	}
	// a wrong password
	drop(crc32.ChecksumIEEE(data))
	n200l.TicPassword = "OTHER"
	if st, _ = hub.f.Toss(n200l); st.Bad != 1 || !strings.Contains(hub.log.String(), "wrong TIC password") {
		t.Fatalf("a wrong password: %+v", st)
	}
}

func visible(ents []os.DirEntry) []string {
	var out []string
	for _, e := range ents {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out
}
