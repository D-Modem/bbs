package nntp

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
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func init() { store.BcryptCost = 4 }

type site struct {
	b    *bbs.BBS
	svc  *Service
	addr string
	area *store.Area
	log  *bytes.Buffer
}

func freeAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func selfSigned(t *testing.T, dir string) (string, string) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "news"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	kd, _ := x509.MarshalECPrivateKey(key)
	c, k := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(c, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(k, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0600)
	return c, k
}

// newSite runs a BBS's news server, with users alice and sysop and an
// area General as bbs.general.
func newSite(t *testing.T, host string, set ServerSettings, peers []*Peer) *site {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.CreateUser("alice", "alicepass1", "", false)
	st.CreateUser("sysop", "sysoppass1", "", true)
	a, _ := st.CreateArea("General", "Talk", false)
	st.SetNewsgroup(a.ID, "bbs.general")
	area, _ := st.Area(a.ID)
	s := bbs.Defaults()
	s.Name = host
	s.FailDelay = 0
	var logbuf bytes.Buffer
	b := bbs.New(s, st, log.New(&logbuf, "", 0))
	b.News = bbs.NewsSettings{PathHost: host, Domain: host}
	svc := &Service{B: b, MaxArticle: 64 * 1024}
	set.Address = freeAddr(t)
	if set.TLS == "" {
		set.TLS = TLSNo
	}
	if set.TLS != TLSNo {
		set.CertFile, set.KeyFile = selfSigned(t, dir)
	}
	if set.IdleTimeout == 0 {
		set.IdleTimeout = time.Minute
	}
	srv, err := NewServer(svc, set, peers)
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
			t.Logf("%s log:\n%s", host, logbuf.String())
		}
	})
	for i := 0; ; i++ {
		if c, err := net.Dial("tcp", set.Address); err == nil {
			c.Close()
			break
		}
		if i > 100 {
			t.Fatal("never listened")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return &site{b: b, svc: svc, addr: set.Address, area: area, log: &logbuf}
}

// post posts a message as a BBS user would.
func (s *site) post(t *testing.T, user, subject, body string, replyTo int64) *store.Message {
	t.Helper()
	u, err := s.b.Store.UserByName(user)
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.b.Enter(&bbs.Caller{Via: "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer node.Leave()
	node.SetUser(u)
	id, err := node.Post(s.area, "All", subject, strings.Split(body, "\n"), replyTo)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := s.b.Store.Message(id)
	return m
}

// conv is a raw NNTP conversation.
type conv struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
}

func dialConv(t *testing.T, addr string) *conv {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cv := &conv{t: t, c: c, r: bufio.NewReader(c)}
	cv.expect("20")
	return cv
}

func (cv *conv) expect(prefix string) string {
	cv.t.Helper()
	cv.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := cv.r.ReadString('\n')
	if err != nil {
		cv.t.Fatalf("waiting for %s: %v", prefix, err)
	}
	if !strings.HasPrefix(line, prefix) {
		cv.t.Fatalf("got %q, want %s...", line, prefix)
	}
	return strings.TrimSpace(line)
}

func (cv *conv) send(s string) { fmt.Fprintf(cv.c, "%s\r\n", s) }

func (cv *conv) block() string {
	var b strings.Builder
	for {
		line, err := cv.r.ReadString('\n')
		if err != nil {
			cv.t.Fatal(err)
		}
		if line == ".\r\n" {
			return b.String()
		}
		b.WriteString(line)
	}
}

// Python's nntplib, a newsreader of someone else's making, reads, posts and
// follows a thread
func TestNewsreader(t *testing.T) {
	if exec.Command("python3", "-c", "import nntplib").Run() != nil {
		t.Skip("no nntplib")
	}
	s := newSite(t, "news.a.test", ServerSettings{Post: true, ReadUsers: true}, nil)
	first := s.post(t, "sysop", "Welcome", "Hello from the BBS.\n.a line with a dot", 0)
	host, port, _ := net.SplitHostPort(s.addr)
	script := fmt.Sprintf(`
import nntplib, io, sys
s = nntplib.NNTP(%q, %s, readermode=True, usenetrc=False)
caps = s.getcapabilities()
assert 'OVER' in caps and 'POST' in caps, caps
try:
    s.group('bbs.general')
    raise SystemExit('read without login')
except nntplib.NNTPTemporaryError as e:
    assert str(e).startswith('480'), e
s.login('alice', 'alicepass1')
resp, count, first, last, name = s.group('bbs.general')
assert count == 1 and name == 'bbs.general', (count, name)
resp, info = s.article(first)
lines = [l.decode() for l in info.lines]
assert 'Hello from the BBS.' in lines and '.a line with a dot' in lines, lines
resp, overviews = s.over((first, last))
num, ov = overviews[0]
assert ov['subject'] == 'Welcome' and ov['message-id'] == %q, ov
art = b"From: someone@else.example\r\nNewsgroups: bbs.general\r\nSubject: Re: Welcome\r\nReferences: %s\r\n\r\nA reply from a newsreader.\r\n..and a dot\r\n"
s.post(io.BytesIO(art))
resp, count, first, last, name = s.group('bbs.general')
assert count == 2, count
resp, info = s.article(last)
text = b"\n".join(info.lines).decode()
assert 'From: "alice" <alice@news.a.test>' in text, text
assert 'References: %s' in text, text
assert '.and a dot' in text, text
resp, groups = s.list()
assert groups[0].group == 'bbs.general', groups
resp, hdrs = s.xhdr('subject', '%%d-%%d' %% (first, last))
assert [h[1] for h in hdrs] == ['Welcome', 'Re: Welcome'], hdrs
s.quit()
print('ok')
`, host, port, first.MessageID, first.MessageID, first.MessageID)
	out, err := exec.Command("python3", "-W", "ignore", "-c", script).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("nntplib: %v\n%s", err, out)
	}
	// the reply is a BBS message, answering the first
	ms, _ := s.b.Store.Messages(s.area.ID, 0, 10)
	if len(ms) != 2 || ms[1].From != "alice" || ms[1].ReplyTo != first.ID {
		t.Errorf("messages %+v", ms[1])
	}
}

// two BBSs keep bbs.general in step: B pushes to A by IHAVE, A pulls
// from B, nothing comes back round
func TestPeering(t *testing.T) {
	a := newSite(t, "news.a.test", ServerSettings{Post: true}, []*Peer{
		{Name: "b", AcceptFrom: []net.IP{net.ParseIP("127.0.0.1")}}})
	b := newSite(t, "news.b.test", ServerSettings{Post: true}, []*Peer{
		{Name: "a", AcceptFrom: []net.IP{net.ParseIP("127.0.0.1")}}})
	toA := &Peer{Name: "a", Address: a.addr, TLS: "no", Groups: "bbs.*", Push: true, Pull: true,
		MaxPerRun: 100, Initial: 10}
	toB := &Peer{Name: "b", Address: b.addr, TLS: "no", Groups: "bbs.*", Push: true, Pull: true,
		MaxPerRun: 100, Initial: 10}
	// first runs set the starting points
	if _, _, err := b.svc.Sync(toA); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.svc.Sync(toB); err != nil {
		t.Fatal(err)
	}

	mb := b.post(t, "alice", "From B", "Written at B.", 0)
	pushed, _, err := b.svc.Sync(toA) // B offers it to A
	if err != nil || pushed != 1 {
		t.Fatalf("push: %d, %v", pushed, err)
	}
	ma, err := a.b.Store.MessageByMessageID(mb.MessageID)
	if err != nil || ma.Origin != "b" || ma.Subject != "From B" {
		t.Fatalf("at A: %+v, %v", ma, err)
	}
	raw, _ := a.b.Store.Raw(ma.ID)
	if !bytes.Contains(raw, []byte("Path: news.a.test!news.b.test!not-for-mail")) {
		t.Errorf("path:\n%s", raw)
	}

	// A's own post, and a reply to B's
	a.post(t, "sysop", "From A", "Written at A.", 0)
	reply := a.post(t, "alice", "Re: From B", "A reply at A.", ma.ID)
	// B pulls from A
	_, pulled, err := b.svc.Sync(toA)
	if err != nil || pulled != 2 {
		t.Fatalf("pull: %d, %v", pulled, err)
	}
	rb, err := b.b.Store.MessageByMessageID(reply.MessageID)
	if err != nil || rb.ReplyTo != mb.ID {
		t.Errorf("reply at B: %+v, %v", rb, err)
	}
	// nothing goes back: A gets nothing new from B
	pushed, pulled, err = a.svc.Sync(toB)
	if err != nil {
		t.Fatal(err)
	}
	if pushed != 0 || pulled != 0 {
		t.Errorf("round again: %d pushed, %d pulled", pushed, pulled)
	}
	n := 0
	ms, _ := a.b.Store.Messages(a.area.ID, 0, 100)
	n = len(ms)
	if n != 3 {
		t.Errorf("A has %d messages", n)
	}
}

func TestAccess(t *testing.T) {
	s := newSite(t, "news.a.test", ServerSettings{Post: true, TLS: TLSRequired}, nil)
	cv := dialConv(t, s.addr)
	cv.send("IHAVE <x@y.z>")
	cv.expect("502")
	cv.send("POST")
	cv.expect("480")
	cv.send("AUTHINFO USER alice")
	cv.expect("483")
	cv.send("GROUP bbs.general")
	cv.expect("211 0")
	cv.send("ARTICLE <nope@x.y>")
	cv.expect("430")
	cv.send("ARTICLE 1")
	cv.expect("423")
	cv.send(strings.Repeat("X", 600))
	cv.expect("501")
	cv.send("FROB")
	cv.expect("500")

	// STARTTLS, then the login and a post
	cv.send("STARTTLS")
	cv.expect("382")
	tc := tls.Client(cv.c, &tls.Config{InsecureSkipVerify: true})
	if err := tc.Handshake(); err != nil {
		t.Fatal(err)
	}
	cv.c, cv.r = tc, bufio.NewReader(tc)
	cv.send("AUTHINFO USER alice")
	cv.expect("381")
	cv.send("AUTHINFO PASS wrong")
	cv.expect("481")
	cv.send("AUTHINFO USER alice")
	cv.expect("381")
	cv.send("AUTHINFO PASS alicepass1")
	cv.expect("281")
	cv.send("POST")
	cv.expect("340")
	cv.send("From: forged@elsewhere.example\r\nNewsgroups: bbs.general\r\nSubject: hi\r\nPath: forged!path\r\n\r\nbody\r\n.")
	id := strings.Fields(cv.expect("240"))[3]
	m, err := s.b.Store.MessageByMessageID(id)
	if err != nil || m.From != "alice" {
		t.Fatalf("posted %+v, %v", m, err)
	}
	raw, _ := s.b.Store.Raw(m.ID)
	if bytes.Contains(raw, []byte("forged")) {
		t.Errorf("forged headers kept:\n%s", raw)
	}
	// a control message, and one too large
	cv.send("POST")
	cv.expect("340")
	cv.send("Newsgroups: bbs.general\r\nSubject: x\r\nControl: cancel <a@b>\r\n\r\nx\r\n.")
	cv.expect("441")
	cv.send("POST")
	cv.expect("340")
	cv.send("Newsgroups: bbs.general\r\nSubject: big\r\n\r\n" + strings.Repeat("x\r\n", 40000) + ".")
	cv.expect("441")
	cv.send("QUIT")
	cv.expect("205")
}

func TestIHAVE(t *testing.T) {
	s := newSite(t, "news.a.test", ServerSettings{}, []*Peer{
		{Name: "p", AcceptFrom: []net.IP{net.ParseIP("127.0.0.1")}}})
	cv := dialConv(t, s.addr)
	art := func(id, path, extra string) string {
		return "Path: " + path + "\r\nFrom: Bob <bob@far.example>\r\nNewsgroups: bbs.general,far.misc\r\n" +
			"Subject: far away\r\nDate: Sat, 10 Oct 2026 12:00:00 +0000\r\nMessage-ID: " + id + "\r\n" + extra +
			"\r\nA line.\r\n..dotted\r\n."
	}
	cv.send("IHAVE <1@far.example>")
	cv.expect("335")
	cv.send(art("<1@far.example>", "far.example!not-for-mail", ""))
	cv.expect("235")
	cv.send("IHAVE <1@far.example>")
	cv.expect("435")
	// one that has been here (our host in its Path)
	cv.send("IHAVE <2@far.example>")
	cv.expect("335")
	cv.send(art("<2@far.example>", "far.example!news.a.test!not-for-mail", ""))
	cv.expect("437")
	// one for no group of ours
	cv.send("IHAVE <3@far.example>")
	cv.expect("335")
	cv.send(strings.Replace(art("<3@far.example>", "far.example", ""), "bbs.general,far.misc", "far.misc", 1))
	cv.expect("437")
	// refused ones are remembered
	cv.send("IHAVE <3@far.example>")
	cv.expect("435")
	// offered as one, is another
	cv.send("IHAVE <4@far.example>")
	cv.expect("335")
	cv.send(art("<5@far.example>", "far.example", ""))
	cv.expect("437")
	m, err := s.b.Store.MessageByMessageID("<1@far.example>")
	if err != nil || m.From != "Bob" || m.Origin != "p" || !strings.Contains(m.Body, ".dotted") {
		t.Fatalf("%+v, %v", m, err)
	}
}
