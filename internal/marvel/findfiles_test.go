package marvel

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/D-Modem/bbs/internal/files"
	"github.com/D-Modem/bbs/internal/store"
)

func TestQuery(t *testing.T) {
	board := &node{name: "Sports, Health and Fitness", desc: "Climbing and e-mail", app: appBBS, board: &store.Area{}}
	news := &node{name: "comp.sys.ibm", app: appBBS, board: &store.Area{Newsgroup: "comp.sys.ibm"}}
	folder := &node{name: "Cats", desc: "All about cabs", app: appDirectory}
	file := &node{name: "BALL.ZIP", desc: "a game", app: appDownload}
	all := []*node{board, news, folder, file}
	for _, c := range []struct {
		q    string
		want string // the names matched, by their first letters
	}{
		{"(SUBJ_NAME contains 'sports' & 'health')", "S"},
		{"(NAME contains 'sport')", ""},      // a whole word
		{"(NAME contains 'sport%')", "S"},    // or a wildcard
		{"(NAME_DESC contains 'climb')", ""}, // not "Climbing"
		{"(NAME_DESC contains 'climb%')", "S"},
		{"(NAME_DESC contains 'ca_')", ""}, // "cats" and "cabs" have four letters
		{"(NAME_DESC contains 'ca__')", "C"},
		{"(NAME contains 'b_ll')", "B"},
		{"(NAME contains 'health and fitness')", "S"}, // a phrase
		{"(NAME contains 'fitness and health')", ""},
		{"(SEARCH_PROPS contains 'e\\-mail')", "S"},
		{"(NAME contains 'cats' | 'ball')", "CB"},
		{"(NAME contains 'cats' , 'ball')", "CB"},
		{"(NAME contains ~ 'cats') AND (APPID = 1)", ""},
		{"(SEARCH_PROPS contains 'a%') AND (APPID <> 2 or BBS_FOLDER_FLAGS <> 0)", "SCB"},
		{"(NAME contains '%') AND (APPID = 2 AND BBS_FOLDER_FLAGS = 1)", "S"},
		{"(NAME contains '%') AND (APPID = 2 AND BBS_FOLDER_FLAGS = 0)", "c"},
		{"(NAME contains '%') AND (APPID in (6,7,11))", "B"},
		{"(NAME contains '%') AND (LOCALES contains '00000409')", "ScCB"},
		{"(PLACES contains 'seattle')", ""},
	} {
		m, err := parseQuery(c.q)
		if err != nil {
			t.Errorf("%s: %v", c.q, err)
			continue
		}
		got := ""
		for _, n := range all {
			if m(n) {
				got += n.name[:1]
			}
		}
		if got != c.want {
			t.Errorf("%s: %q, not %q", c.q, got, c.want)
		}
	}
	for _, q := range []string{"(NAME contains 'x'", "NAME = 'x'", "(NAME contains 'x) AND", "NAME ! 1"} {
		if _, err := parseQuery(q); err == nil {
			t.Errorf("%s: no error", q)
		}
	}
}

// filesServer is a server whose BBS has a file area, with a file in it of
// three blocks' size.
func filesServer(t *testing.T) (*Server, *store.Store, *store.File, []byte) {
	t.Helper()
	srv, st := newServer(t)
	var err error
	if srv.B.Files, err = files.Open(st, filepath.Join(t.TempDir(), "files"), 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	u, _ := st.UserByName("alice")
	fa, err := st.CreateFileArea("Utilities", "Programs to run", false)
	if err != nil {
		t.Fatal(err)
	}
	up, err := srv.B.Files.Upload(u, fa, "UNZIP.EXE", "An unzipper")
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 2*ftmBlockMax+1000)
	for i := range data {
		data[i] = byte(i * 7)
	}
	up.Write(data)
	f, err := up.Commit()
	if err != nil {
		t.Fatal(err)
	}
	return srv, st, f, data
}

func TestFindAndFiles(t *testing.T) {
	srv, st, f, data := filesServer(t)
	if _, err := st.CreateArea("Chatter", "Talk about anything", false); err != nil {
		t.Fatal(err)
	}

	c := signedOn(t, srv, "alice", "password1")
	c.open(2, "DIRSRV")
	nav := []string{"a", "c", "b", "e", "tp", "p", "fn", "zc", "h"}
	libs := c.treeCall(2, treeGetChildren, keyFiles, 0, nav...)
	if len(libs) != 1 || string(libs[0]["e"]) != "Utilities" || string(libs[0]["tp"]) != "File Library" {
		t.Fatalf("libraries: %v", libs)
	}
	lk, _ := keyOf(libs[0]["a"])
	fs := c.treeCall(2, treeGetChildren, lk, 0, nav...)
	if len(fs) != 1 {
		t.Fatalf("files: %v", fs)
	}
	row := fs[0]
	fk, _ := keyOf(row["a"])
	if fk != (nodeKey{uint32(f.ID), fileTag}) || binary.LittleEndian.Uint32(row["c"]) != appDownload ||
		row["b"][0] != flagLeaf || string(row["fn"]) != "UNZIP.EXE" || string(row["tp"]) != "File" ||
		binary.LittleEndian.Uint32(row["p"]) != uint32(len(data)) || binary.LittleEndian.Uint32(row["h"]) != iconFile {
		t.Fatalf("file row: %v", row)
	}

	// Find: the ids, then the rows the results window reads
	c.open(3, "FindSvc")
	r := c.call(3, 0x01, findSearch, vparam([]byte("(SUBJ_NAME contains 'unz%') AND (APPID <> 2 or BBS_FOLDER_FLAGS <> 0)\x00")),
		dparam(1), []byte{0x83, 0x83, 0x85})
	v, rest := static(t, r)
	if v[0] != 0 || v[1] != 1 || rest[0] != tagStreamEnd || !bytes.Equal(rest[1:], fk.bytes()) {
		t.Fatalf("search: %x", r)
	}
	end := c.recv()
	if h, err := parseHostBlock(end[2:]); err != nil || !bytes.Equal(h.Params, []byte{tagComplete}) {
		t.Fatalf("search's end: %x", end)
	}
	r = c.call(3, 0x01, findSearch, vparam([]byte("(NAME contains 'chatter' | 'unzip.exe')\x00")), dparam(1), []byte{0x83, 0x83, 0x85})
	if v, rest := static(t, r); v[1] != 2 || len(rest) != 1+16 {
		t.Fatalf("two found: %x", r)
	}
	c.recv()
	found := c.treeCall(2, treeGetProperties, fk, 0, "f", "c", "a", "tp", "w", "p")
	if len(found) != 1 || string(bytes.ReplaceAll(found[0]["f"], []byte{0}, nil)) != "UNZIP.EXE" ||
		string(found[0]["tp"]) != "File" || len(found[0]["w"]) != 8 {
		t.Fatalf("find row: %v", found)
	}

	// the download: what it is, then the file in blocks
	c.open(4, "FTM")
	cfi := make([]byte, cfiSize)
	copy(cfi, "DIRSRV")
	binary.LittleEndian.PutUint32(cfi[cfiFileAt:], fk.b)
	binary.LittleEndian.PutUint32(cfi[cfiFileAt+4:], fk.a)
	copy(cfi[cfiFileAt+8:], "fi")
	r = c.call(4, 0x01, ftmRequest, vparam(cfi), []byte{0x84})
	if len(r) != 2+ftmReply || r[0] != tagVar {
		t.Fatalf("request: %x", r)
	}
	rec := r[2:]
	if binary.LittleEndian.Uint32(rec) != 0 || binary.LittleEndian.Uint32(rec[0x0C:]) != uint32(len(data)) ||
		binary.LittleEndian.Uint32(rec[0x10:]) != ftmHasUnpack || binary.LittleEndian.Uint32(rec[0x14:]) != 0 {
		t.Fatalf("request reply: %x", rec)
	}
	r = c.call(4, 0x01, ftmStart, vparam(cfi), []byte{0x84, 0x85})
	if len(r) < 12 || r[0] != tagVar || r[1] != 0x88 || r[10] != tagEndStatic || r[11] != tagStreamEnd {
		t.Fatalf("start: %x", r[:min(len(r), 16)])
	}
	got := append([]byte(nil), r[12:]...)
	blocks := 1
	for {
		m := c.recv()
		h, err := parseHostBlock(m[2:])
		if err != nil || h.Method != ftmStart {
			t.Fatalf("block: %x", m[:min(len(m), 16)])
		}
		if bytes.Equal(h.Params, []byte{tagComplete}) {
			break
		}
		if h.Params[0] != tagStreamEnd || len(h.Params) > 1+ftmBlockMax {
			t.Fatalf("block of %d", len(h.Params))
		}
		got = append(got, h.Params[1:]...)
		blocks++
	}
	if !bytes.Equal(got, data) || blocks != 3 {
		t.Fatalf("%d bytes in %d blocks", len(got), blocks)
	}
	if again, _ := st.FileByID(f.ID); again.Downloads != 1 {
		t.Errorf("downloads: %d", again.Downloads)
	}

	// a file id that isn't one
	binary.LittleEndian.PutUint32(cfi[cfiFileAt+4:], 9999)
	r = c.call(4, 0x01, ftmRequest, vparam(cfi), []byte{0x84})
	if binary.LittleEndian.Uint32(r[2:]) != mapiFail {
		t.Fatalf("no such file: %x", r)
	}
}
