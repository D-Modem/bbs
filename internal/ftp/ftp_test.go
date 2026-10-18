package ftp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	client "github.com/jlaffaye/ftp"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// init makes passwords cheap to hash, for quick tests.
func init() { store.BcryptCost = 4 }

type env struct {
	addr string
	b    *bbs.BBS
	log  *bytes.Buffer
}

// freePort is a local address with a port free just now (it may be taken by
// the time it is used).
func freePort(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// selfSigned writes a certificate and key for 127.0.0.1.
func selfSigned(t *testing.T, dir string) (string, string) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "bbs"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	cert, keyf := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(keyf, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0600)
	return cert, keyf
}

// start is an FTP server on a new BBS, running until the test ends: alice,
// bob and a sysop; areas Uploads and Official (the sysop's to upload to);
// uploads of up to 1 MB; settings changed by tweak, if given, with a
// certificate made for TLS.  It returns once the server has answered and
// counted out its probe.
func start(t *testing.T, tweak func(*Settings)) *env {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.CreateUser("alice", "alicepass1", "", false)
	db.CreateUser("bob", "bobpassword", "", false)
	db.CreateUser("sysop", "sysoppass1", "", true)
	db.CreateFileArea("Uploads", "", false)
	db.CreateFileArea("Official", "", true)
	s := bbs.Defaults()
	s.FailDelay = 0
	var logbuf bytes.Buffer
	b := bbs.New(s, db, log.New(&logbuf, "", 0))
	if b.Files, err = files.Open(db, filepath.Join(dir, "files"), 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	set := Settings{Address: freePort(t), TLS: TLSNo, MaxClients: 10, MaxPerIP: 5, IdleTimeout: time.Minute}
	if tweak != nil {
		tweak(&set)
	}
	if set.TLS != TLSNo {
		set.CertFile, set.KeyFile = selfSigned(t, dir)
	}
	srv, err := New(b, set)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		<-done
		if t.Failed() {
			t.Log("log:\n" + logbuf.String())
		}
	})
	for i := 0; ; i++ {
		if c, err := net.Dial("tcp", set.Address); err == nil {
			// the greeting comes once the server has counted it
			c.SetReadDeadline(time.Now().Add(5 * time.Second))
			bufio.NewReader(c).ReadString('\n')
			c.Close()
			break
		}
		if i > 100 {
			t.Fatal("never listened")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// the probe is counted against its address until the server sees it
	// gone: wait, or a test of the limit finds it still there
	for i := 0; ; i++ {
		srv.mu.Lock()
		n := srv.clients
		srv.mu.Unlock()
		if n == 0 {
			break
		}
		if i > 100 {
			t.Fatal("the probe connection never went")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return &env{addr: set.Address, b: b, log: &logbuf}
}

// login dials the server and logs in, failing the test if either fails; the
// connection quits when the test ends.
func (e *env) login(t *testing.T, user, pass string, opts ...client.DialOption) *client.ServerConn {
	t.Helper()
	opts = append(opts, client.DialWithTimeout(5*time.Second))
	c, err := client.Dial(e.addr, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Quit() })
	if err := c.Login(user, pass); err != nil {
		t.Fatalf("login %s: %v", user, err)
	}
	return c
}

// names is a directory's entries' names, comma separated.
func names(t *testing.T, c *client.ServerConn, dir string) string {
	t.Helper()
	list, err := c.List(dir)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	var n []string
	for _, e := range list {
		n = append(n, e.Name)
	}
	return strings.Join(n, ",")
}

// TestUploadDownload: / lists the areas and /qwk; a file uploaded is listed,
// downloads as it went up, has its size by a relative path, and is recorded
// as alice's and downloaded once.
func TestUploadDownload(t *testing.T) {
	e := start(t, nil)
	c := e.login(t, "alice", "alicepass1")
	if got := names(t, c, "/"); got != "Uploads,Official,qwk" {
		t.Errorf("/ = %s", got)
	}
	if err := c.Stor("/Uploads/hello.txt", strings.NewReader("hello, world\n")); err != nil {
		t.Fatal(err)
	}
	if got := names(t, c, "/Uploads"); got != "hello.txt" {
		t.Errorf("/Uploads = %s", got)
	}
	r, err := c.Retr("/Uploads/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	r.Close()
	if string(data) != "hello, world\n" {
		t.Errorf("got %q", data)
	}
	// a relative path from inside the area works too
	if err := c.ChangeDir("Uploads"); err != nil {
		t.Fatal(err)
	}
	if size, err := c.FileSize("hello.txt"); err != nil || size != 13 {
		t.Errorf("size %d, %v", size, err)
	}
	time.Sleep(100 * time.Millisecond)
	a, _ := e.b.Store.FileAreaByName("Uploads")
	f, err := e.b.Store.FileByName(a.ID, "hello.txt")
	if err != nil || f.Downloads != 1 || f.Uploader != "alice" {
		t.Errorf("record %+v, %v", f, err)
	}
}

// TestDenied: no overwriting, uploads outside an area or into one not open
// to the user, bad names, directories made or removed, renames, appends,
// resumes, paths too deep or out of the areas; nothing of the server's own
// files; and only a file's uploader deletes it.
func TestDenied(t *testing.T) {
	e := start(t, nil)
	c := e.login(t, "alice", "alicepass1")
	if err := c.Stor("/Uploads/a.txt", strings.NewReader("a")); err != nil {
		t.Fatal(err)
	}
	for what, err := range map[string]error{
		"overwrite":       c.Stor("/Uploads/a.txt", strings.NewReader("b")),
		"root":            c.Stor("/x.txt", strings.NewReader("x")),
		"sysop-only area": c.Stor("/Official/x.txt", strings.NewReader("x")),
		"no such area":    c.Stor("/Nowhere/x.txt", strings.NewReader("x")),
		"bad name":        c.Stor("/Uploads/.hidden", strings.NewReader("x")),
		"mkdir":           c.MakeDir("/Uploads/sub"),
		"rmdir":           c.RemoveDir("/Uploads"),
		"rename":          c.Rename("/Uploads/a.txt", "/Uploads/b.txt"),
		"append":          c.Append("/Uploads/a.txt", strings.NewReader("more")),
		"resume upload":   c.StorFrom("/Uploads/c.txt", strings.NewReader("x"), 5),
		"too deep":        c.Stor("/Uploads/a/b.txt", strings.NewReader("x")),
		"traversal":       c.Stor("/Uploads/../../../tmp/evil", strings.NewReader("x")),
	} {
		if err == nil {
			t.Errorf("%s: allowed", what)
		}
	}
	// the server's own files are nowhere to be seen
	if _, err := c.Retr("/../../../../etc/passwd"); err == nil {
		t.Error("read outside the areas")
	}
	// bob may not delete alice's file; alice may
	b := e.login(t, "bob", "bobpassword")
	if err := b.Delete("/Uploads/a.txt"); err == nil {
		t.Error("bob deleted alice's file")
	}
	if err := c.Delete("/Uploads/a.txt"); err != nil {
		t.Errorf("alice: %v", err)
	}
	if got := names(t, c, "/Uploads"); got != "" {
		t.Errorf("left: %s", got)
	}
}

// TestBadLogin: a wrong password is refused and logged; anonymous is refused
// while it is off.
func TestBadLogin(t *testing.T) {
	e := start(t, nil)
	c, err := client.Dial(e.addr, client.DialWithTimeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Quit()
	if err := c.Login("alice", "wrong"); err == nil {
		t.Error("wrong password taken")
	}
	c2, _ := client.Dial(e.addr, client.DialWithTimeout(5*time.Second))
	defer c2.Quit()
	if err := c2.Login("anonymous", "me@example.com"); err == nil {
		t.Error("anonymous taken while off")
	}
	if !strings.Contains(e.log.String(), `failed login as "alice"`) {
		t.Errorf("not logged: %s", e.log)
	}
}

// TestAnonymous: anonymous, when on, downloads but neither uploads nor
// deletes.
func TestAnonymous(t *testing.T) {
	e := start(t, func(s *Settings) { s.Anonymous = true })
	up := e.login(t, "alice", "alicepass1")
	up.Stor("/Uploads/pub.txt", strings.NewReader("public"))
	c := e.login(t, "anonymous", "guest@")
	r, err := c.Retr("/Uploads/pub.txt")
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if err := c.Stor("/Uploads/anon.txt", strings.NewReader("x")); err == nil {
		t.Error("anonymous uploaded")
	}
	if err := c.Delete("/Uploads/pub.txt"); err == nil {
		t.Error("anonymous deleted")
	}
}

// TestTLSRequired: with TLS required a login in the clear is refused, and
// over explicit TLS one logs in and transfers.
func TestTLSRequired(t *testing.T) {
	e := start(t, func(s *Settings) { s.TLS = TLSRequired })
	// in the clear: refused before the password
	c, err := client.Dial(e.addr, client.DialWithTimeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login("alice", "alicepass1"); err == nil {
		t.Error("logged in without TLS")
	}
	c.Quit()
	// explicit TLS: in, and transfers work
	tc := e.login(t, "alice", "alicepass1",
		client.DialWithExplicitTLS(&tls.Config{InsecureSkipVerify: true}))
	if err := tc.Stor("/Uploads/secret.txt", strings.NewReader("over TLS")); err != nil {
		t.Fatal(err)
	}
	r, err := tc.Retr("/Uploads/secret.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	r.Close()
	if string(data) != "over TLS" {
		t.Errorf("got %q", data)
	}
}

// TestTooLarge: an upload over the size limit is refused and not kept.
func TestTooLarge(t *testing.T) {
	e := start(t, nil)
	c := e.login(t, "alice", "alicepass1")
	if err := c.Stor("/Uploads/big.bin", bytes.NewReader(make([]byte, 2<<20))); err == nil {
		t.Error("took a file over the limit")
	}
	if got := names(t, c, "/Uploads"); got != "" {
		t.Errorf("kept: %s", got)
	}
}

// TestPerIPLimit: a second connection from an address at its limit is turned
// away.
func TestPerIPLimit(t *testing.T) {
	e := start(t, func(s *Settings) { s.MaxPerIP = 1 })
	e.login(t, "alice", "alicepass1")
	c, err := client.Dial(e.addr, client.DialWithTimeout(5*time.Second))
	if err == nil {
		err = c.Login("bob", "bobpassword")
		c.Quit()
	}
	if err == nil {
		t.Error("a second connection from the address")
	}
}

// TestQWK: /qwk holds the user's packet, whose messages count as read once
// it is downloaded; a REP put there posts its replies, and nothing else may
// be; anonymous callers have no /qwk.
func TestQWK(t *testing.T) {
	e := start(t, nil)
	gen, _ := e.b.Store.CreateArea("General", "", false)
	bob, _ := e.b.Store.UserByName("bob")
	e.b.Agent(bob, "test").Post(gen, "All", "Offline", []string{"By FTP."}, 0)
	c := e.login(t, "alice", "alicepass1")
	id := e.b.BBSID()
	if got := names(t, c, "/qwk"); got != id+".QWK" {
		t.Errorf("/qwk = %s", got)
	}
	r, err := c.Retr("/qwk/" + id + ".QWK")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	r.Close()
	p, err := qwk.ReadPacket(data)
	if err != nil || len(p.Messages) != 1 || p.Messages[0].Body != "By FTP." {
		t.Fatalf("packet %+v %v", p, err)
	}
	alice, _ := e.b.Store.UserByName("alice")
	for i := 0; ; i++ { // the close is counted after the transfer's end
		if n, _ := e.b.Store.NewCount(alice.ID, gen.ID); n == 0 {
			break
		}
		if i > 100 {
			t.Fatal("the packet's messages still new")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var rep bytes.Buffer
	qwk.WriteREP(&rep, id, []qwk.Message{{Conf: int(gen.ID), To: "bob", Subject: "Re: Offline", Body: "Got it by FTP."}})
	if err := c.Stor("/qwk/"+id+".REP", &rep); err != nil {
		t.Fatal(err)
	}
	if err := c.Stor("/qwk/other.txt", strings.NewReader("x")); err == nil {
		t.Error("a file not a REP taken")
	}
	ms, _ := e.b.Store.Messages(gen.ID, 1, 10)
	if len(ms) != 1 || ms[0].From != "alice" {
		t.Errorf("replies %+v", ms)
	}
	// none for anonymous callers
	e2 := start(t, func(s *Settings) { s.Anonymous = true })
	a := e2.login(t, "anonymous", "x")
	if _, err := a.List("/qwk"); err == nil {
		t.Error("anonymous sees /qwk")
	}
}

// TestDownloadCounting: a download counts once the transfer reaches the
// file's end: one cut short does not; a resumption (REST) of one counted
// today is not counted again, and needs no downloads left when a fresh
// one is refused for the day's limit.
func TestDownloadCounting(t *testing.T) {
	e := start(t, nil)
	c := e.login(t, "alice", "alicepass1")
	if err := c.Stor("/Uploads/big.bin", bytes.NewReader(make([]byte, 300000))); err != nil {
		t.Fatal(err)
	}
	alice, _ := e.b.Store.UserByName("alice")
	fs := &fileSystem{s: &Server{B: e.b}, user: alice, who: "test"}
	downloads := func() int {
		a, _ := e.b.Store.FileAreaByName("Uploads")
		f, _ := e.b.Store.FileByName(a.ID, "big.bin")
		return f.Downloads
	}
	get := func(offset int64, n int64) error {
		t.Helper()
		h, err := fs.GetHandle("/Uploads/big.bin", os.O_RDONLY, offset)
		if err != nil {
			return err
		}
		if offset > 0 {
			h.Seek(offset, io.SeekStart)
		}
		if n < 0 {
			io.Copy(io.Discard, h)
		} else {
			io.CopyN(io.Discard, h, n)
		}
		return h.Close()
	}
	if err := get(0, 1000); err != nil || downloads() != 0 {
		t.Fatalf("cut short: %v, %d counted", err, downloads())
	}
	if err := get(0, -1); err != nil || downloads() != 1 {
		t.Fatalf("whole: %v, %d counted", err, downloads())
	}
	if err := get(5000, -1); err != nil || downloads() != 1 {
		t.Errorf("resumed: %v, %d counted", err, downloads())
	}
	conf := *e.b.Conf()
	one := bbs.AllowAll(0, "One a day")
	one.Downloads = 1
	conf.Levels = []bbs.Level{one}
	e.b.Reconfigure(conf)
	if err := get(0, -1); !errors.Is(err, os.ErrPermission) {
		t.Errorf("over the day's limit: %v", err)
	}
	if err := get(5000, -1); err != nil {
		t.Errorf("resumed over the limit: %v", err)
	}
}
