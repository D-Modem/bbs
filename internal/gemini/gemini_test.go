package gemini

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/files"
	"github.com/D-Modem/bbs/internal/store"
)

type site struct {
	b    *bbs.BBS
	addr string
	file *store.File
}

// setup is a Gemini server on a new BBS, with a certificate made for it,
// serving until the test ends: areas General and Private with a message
// each, a file area with a binary file; the message areas shown are those
// matching areas, file areas if withFiles.
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
	st.Post(&store.Message{AreaID: pub.ID, FromID: u.ID, From: "alice", To: "All", Subject: "Hello gemini",
		Body: "First line\n=> gemini://evil.example/ a link\n```\n# not a heading\n> a quote"})
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
	cf, kf := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	if err := MakeCert(cf, kf, CertRequest{Names: []string{"gemini.test"}, Years: 1}); err != nil {
		t.Fatal(err)
	}
	cert, err := LoadCert(cf, kf)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go (&Server{B: b, S: Settings{Areas: areas, Files: withFiles, Users: true}}).Serve(ctx, ln, cert)
	s.addr = ln.Addr().String()
	return s
}

// get sends a request line and is all that comes back.
func (s *site) get(t *testing.T, req string) string {
	t.Helper()
	c, err := tls.Dial("tcp", s.addr, &tls.Config{InsecureSkipVerify: true, ServerName: "gemini.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte(req + "\r\n"))
	b, _ := io.ReadAll(c)
	return string(b)
}

// url is a request for a path.
func (s *site) url(p string) string { return "gemini://" + s.addr + p }

// TestGemini: the home page, the areas shown (not others), a message whose
// lines cannot be taken for links, headings or preformatting, users as
// finger tells of them, refusals of what is not shown and of nonsense, a
// file sent as it is and counted; and with nothing opened, no areas or
// files at all.
func TestGemini(t *testing.T) {
	s := setup(t, "general", true)
	root := s.get(t, s.url("/"))
	for _, want := range []string{"20 text/gemini; charset=utf-8\r\n# Test BBS\n", "=> /about About this system\n",
		"=> /who Who's online\n", "=> /users Look up a user\n", "=> /areas Message areas\n", "=> /files File areas\n"} {
		if !strings.Contains(root, want) {
			t.Errorf("root lacks %q:\n%s", want, root)
		}
	}
	if got := s.get(t, s.url("")); got != "31 /\r\n" {
		t.Errorf("no path: %q", got)
	}
	if got := s.get(t, s.url("/areas/")); got != "31 /areas\r\n" {
		t.Errorf("trailing slash: %q", got)
	}
	areas := s.get(t, s.url("/areas"))
	if !strings.Contains(areas, "=> /area/1 General (1): Talk\n") || strings.Contains(areas, "Private") {
		t.Errorf("areas:\n%s", areas)
	}
	if list := s.get(t, s.url("/area/1")); !strings.Contains(list, "=> /msg/1 #1 Hello gemini (alice, ") {
		t.Errorf("area:\n%s", list)
	}
	msg := s.get(t, s.url("/msg/1"))
	for _, want := range []string{"# Hello gemini\n", "From: alice\n", "\nFirst line\n => gemini://evil.example/ a link\n ```\n # not a heading\n> a quote\n"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
	if got := s.get(t, s.url("/users")); got != "10 User name\r\n" {
		t.Errorf("users: %q", got)
	}
	if got := s.get(t, s.url("/users?alice")); !strings.Contains(got, "User:         alice") {
		t.Errorf("alice:\n%s", got)
	}
	if got := s.get(t, s.url("/~alice")); !strings.Contains(got, "User:         alice") {
		t.Errorf("~alice:\n%s", got)
	}
	if got := s.get(t, s.url("/users?nobody")); !strings.Contains(got, "No such user here.") {
		t.Errorf("nobody:\n%s", got)
	}
	// the area not opened to Gemini, and nonsense
	for _, p := range []string{"/msg/2", "/area/2", "/area/1/x", "/msg/-1", "/../../etc/passwd", "/file/99"} {
		if got := s.get(t, s.url(p)); got != "51 Not here\r\n" {
			t.Errorf("%s: %q", p, got)
		}
	}
	for _, req := range []string{"/", "gemini://user@" + s.addr + "/", "gemini:///", s.url("/#x"), s.url("/" + strings.Repeat("a", 1100))} {
		if got := s.get(t, req); !strings.HasPrefix(got, "59 ") {
			t.Errorf("%.40s: %q", req, got)
		}
	}
	if got := s.get(t, "https://"+s.addr+"/"); !strings.HasPrefix(got, "53 ") {
		t.Errorf("proxy: %q", got)
	}
	files := s.get(t, s.url("/files/1"))
	if !strings.Contains(files, "=> /file/1/readme.bin readme.bin (12 bytes): A file\n") {
		t.Errorf("files:\n%s", files)
	}
	if got := s.get(t, s.url("/file/1/readme.bin")); got != "20 application/octet-stream\r\n\x00binary\r\n.\r\n" {
		t.Errorf("download %q", got)
	}
	if f, _ := s.b.Store.FileByID(s.file.ID); f.Downloads != 1 {
		t.Errorf("downloads %d", f.Downloads)
	}
	// a restricted file area is not Gemini's
	s.b.Store.SetFileAreaRequire(s.file.AreaID, "20", "")
	for _, p := range []string{fmt.Sprintf("/files/%d", s.file.AreaID), fmt.Sprintf("/file/%d", s.file.ID)} {
		if got := s.get(t, s.url(p)); got != "51 Not here\r\n" {
			t.Errorf("restricted %s: %q", p, got)
		}
	}

	// nothing opened: no areas, no files
	s = setup(t, "", false)
	root = s.get(t, s.url("/"))
	if strings.Contains(root, "Message areas") || strings.Contains(root, "File areas") {
		t.Errorf("closed root:\n%s", root)
	}
	for _, p := range []string{"/areas", "/area/1", "/msg/1", "/files", "/file/1"} {
		if got := s.get(t, s.url(p)); got != "51 Not here\r\n" {
			t.Errorf("closed %s: %q", p, got)
		}
	}
}

// TestCert: nothing is made by loading; a certificate has the names,
// organization and years asked for; one there is never overwritten; bad
// requests are refused.
func TestCert(t *testing.T) {
	dir := t.TempDir()
	c, k := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	if _, err := LoadCert(c, k); err == nil || !strings.Contains(err.Error(), "bbs gemini cert") {
		t.Errorf("no certificate: %v", err)
	}
	if got := CertInfo(c); !strings.Contains(got, "bbs gemini cert") {
		t.Errorf("info %q", got)
	}
	req := CertRequest{Names: []string{"bbs.example.org", "gemini.example.org", "192.0.2.7"}, Org: "Test BBS", Years: 5}
	if err := MakeCert(c, k, req); err != nil {
		t.Fatal(err)
	}
	cert, err := LoadCert(c, k)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := x509.ParseCertificate(cert.Certificate[0])
	if x.Subject.CommonName != "bbs.example.org" || strings.Join(x.DNSNames, ",") != "bbs.example.org,gemini.example.org" ||
		len(x.IPAddresses) != 1 || x.Subject.Organization[0] != "Test BBS" || x.NotAfter.Year() != time.Now().Year()+5 {
		t.Errorf("certificate %v %v %v %v %v", x.Subject, x.DNSNames, x.IPAddresses, x.Subject.Organization, x.NotAfter)
	}
	if got := CertInfo(c); !strings.HasPrefix(got, "bbs.example.org, gemini.example.org, 192.0.2.7 until ") ||
		!strings.Contains(got, "SHA-256 "+Fingerprint(x.Raw)) {
		t.Errorf("info %q", got)
	}
	if fi, _ := os.Stat(k); fi.Mode().Perm() != 0600 {
		t.Errorf("key mode %v", fi.Mode())
	}
	if err := MakeCert(c, k, req); err == nil {
		t.Error("made over one there")
	}
	if again, _ := LoadCert(c, k); !bytes.Equal(again.Certificate[0], cert.Certificate[0]) {
		t.Error("changed")
	}
	for _, bad := range []CertRequest{{Years: 1}, {Names: []string{"a b"}, Years: 1}, {Names: []string{"x"}, Years: 0},
		{Names: []string{"x"}, Years: 101}} {
		if err := MakeCert(filepath.Join(dir, "c2"), filepath.Join(dir, "k2"), bad); err == nil {
			t.Errorf("%+v made", bad)
		}
	}
}
