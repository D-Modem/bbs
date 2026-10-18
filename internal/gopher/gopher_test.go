package gopher

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

type site struct {
	b    *bbs.BBS
	addr string
	file *store.File
}

// setup is a gopher server on a new BBS, serving until the test ends: areas
// General and Private with a message each, a file area with a binary file;
// the message areas shown are those matching areas, file areas if withFiles.
func setup(t *testing.T, areas string, withFiles bool) *site {
	t.Helper()
	store.BcryptCost = 4
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := bbs.New(bbs.Defaults(), st, log.New(&bytes.Buffer{}, "", 0))
	b.Settings.Name = "Test BBS"
	u, _ := st.CreateUser("alice", "password1", "", true)
	pub, _ := st.CreateArea("General", "Talk", false)
	priv, _ := st.CreateArea("Private", "Members only", false)
	st.Post(&store.Message{AreaID: pub.ID, FromID: u.ID, From: "alice", To: "All", Subject: "Hello\tgopher",
		Body: "First line\n.\n.dot line"})
	st.Post(&store.Message{AreaID: priv.ID, FromID: u.ID, From: "alice", To: "All", Subject: "Secret", Body: "hush"})
	s := &site{b: b}
	if b.Files, err = files.Open(st, filepath.Join(dir, "files"), 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	fa, _ := st.CreateFileArea("Uploads", "Things", false)
	up, err := b.Files.Upload(u, fa, "readme.bin", "A file")
	if err != nil {
		t.Fatal(err)
	}
	up.Write([]byte("\x00binary\r\n.\r\n"))
	if s.file, err = up.Commit(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go (&Server{B: b, S: Settings{Host: "gopher.test", Port: p, Areas: areas, Files: withFiles}}).Serve(ctx, ln)
	s.addr = ln.Addr().String()
	return s
}

// get sends a selector and is all that comes back.
func (s *site) get(t *testing.T, sel string) string {
	t.Helper()
	c, err := net.Dial("tcp", s.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte(sel + "\r\n"))
	b, _ := io.ReadAll(c)
	return string(b)
}

// TestGopher: the root menu, the areas shown (not others), a message with
// its tabs cleaned and its dots doubled, refusals of what is not shown and of
// nonsense, a file sent as it is and counted; and with nothing opened, no
// areas or files at all.
func TestGopher(t *testing.T) {
	s := setup(t, "general", true)
	root := s.get(t, "")
	for _, want := range []string{"iTest BBS\t\terror.host\t1\r\n", "0About this system\t/about\tgopher.test\t",
		"1Message areas\t/areas\tgopher.test\t", "1File areas\t/files\t", "\r\n.\r\n"} {
		if !strings.Contains(root, want) {
			t.Errorf("root lacks %q:\n%s", want, root)
		}
	}
	areas := s.get(t, "/areas")
	if !strings.Contains(areas, "1General (1)  Talk\t/area/1\t") || strings.Contains(areas, "Private") {
		t.Errorf("areas:\n%s", areas)
	}
	list := s.get(t, "/area/1")
	if !strings.Contains(list, "0#1     Hello gopher  alice") {
		t.Errorf("area: tab not cleaned, or no message:\n%s", list)
	}
	msg := s.get(t, "/msg/1")
	if !strings.Contains(msg, "Subject: Hello\tgopher") && !strings.Contains(msg, "Subject: Hello") {
		t.Errorf("message:\n%s", msg)
	}
	if !strings.Contains(msg, "\r\nFirst line\r\n..\r\n..dot line\r\n.\r\n") {
		t.Errorf("dots not doubled:\n%q", msg)
	}
	// the area not opened to gopher, and nonsense
	for _, sel := range []string{"/msg/2", "/area/2", "/area/1/x", "/msg/-1", "../../etc/passwd", "/file/99"} {
		if got := s.get(t, sel); !strings.HasPrefix(got, "3Not here.") {
			t.Errorf("%s: %q", sel, got)
		}
	}
	files := s.get(t, "/files/1")
	if !strings.Contains(files, "9readme.bin") {
		t.Errorf("files:\n%s", files)
	}
	if got := s.get(t, "/file/1"); got != "\x00binary\r\n.\r\n" {
		t.Errorf("download %q", got)
	}
	if f, _ := s.b.Store.FileByID(s.file.ID); f.Downloads != 1 {
		t.Errorf("downloads %d", f.Downloads)
	}

	// nothing opened: no areas, no files
	s = setup(t, "", false)
	root = s.get(t, "")
	if strings.Contains(root, "Message areas") || strings.Contains(root, "File areas") {
		t.Errorf("closed root:\n%s", root)
	}
	for _, sel := range []string{"/areas", "/area/1", "/msg/1", "/files", "/file/1"} {
		if got := s.get(t, sel); !strings.HasPrefix(got, "3Not here.") {
			t.Errorf("closed %s: %q", sel, got)
		}
	}
}

// TestCurl: curl as the gopher client.
func TestCurl(t *testing.T) {
	if out, err := exec.Command("curl", "-V").Output(); err != nil || !strings.Contains(string(out), "gopher") {
		t.Skip("no curl with gopher")
	}
	s := setup(t, "*", true)
	out, err := exec.Command("curl", "-sS", "gopher://"+s.addr+"/1/areas").Output()
	if err != nil || !strings.Contains(string(out), "General (1)") || !strings.Contains(string(out), "Private (1)") {
		t.Errorf("curl menu: %v\n%s", err, out)
	}
	out, err = exec.Command("curl", "-sS", "gopher://"+s.addr+"/0/msg/1").Output()
	if err != nil || !strings.Contains(string(out), "First line") {
		t.Errorf("curl text: %v\n%s", err, out)
	}
	out, err = exec.Command("curl", "-sS", "gopher://"+s.addr+"/9/file/1").Output()
	if err != nil || string(out) != "\x00binary\r\n.\r\n" {
		t.Errorf("curl binary: %v %q", err, out)
	}
}

// TestGopherRestrictedFiles: a file area with a download requirement is not
// gopher's (gopher has no login): not listed, its files neither listed nor
// sent nor counted.
func TestGopherRestrictedFiles(t *testing.T) {
	s := setup(t, "general", true)
	if err := s.b.Store.SetFileAreaRequire(s.file.AreaID, "20", ""); err != nil {
		t.Fatal(err)
	}
	if got := s.get(t, "/files"); strings.Contains(got, "Uploads") {
		t.Errorf("a restricted area listed:\n%s", got)
	}
	for _, sel := range []string{fmt.Sprintf("/files/%d", s.file.AreaID), fmt.Sprintf("/file/%d", s.file.ID)} {
		if got := s.get(t, sel); !strings.HasPrefix(got, "3Not here.") {
			t.Errorf("%s: %q", sel, got)
		}
	}
	if f, _ := s.b.Store.FileByID(s.file.ID); f.Downloads != 0 {
		t.Errorf("downloads %d", f.Downloads)
	}
}
