package ymsg

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// web makes a request of the web half: the status and body.
func web(t *testing.T, method, u, cookie string, body []byte) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, u, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body == nil {
		req.Body = nil
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(b), r.Header
}

// offer is a Y!M 7 file offer as libyahoo2 sends it.
func offer(cl *client, to, id, name, size string) {
	cl.send(svcFileTrans15, 0, kv(1, cl.id), kv(5, to), kv(265, id), kv(222, "1"), kv(266, "1"),
		kv(302, "268"), kv(300, "268"), kv(27, name), kv(28, size), kv(301, "268"), kv(303, "268"))
}

func TestFileRelay(t *testing.T) {
	e := start(t)
	alice := signedIn(t, e, "alice", 15)
	bob := signedIn(t, e, "bob", 15)
	const id = "abcdefghijklmnopqrstuv$$"

	offer(alice, "bob", id, "hello.txt", "11")
	o := bob.expect(svcFileTrans15, nil)
	if o.get(4) != "alice" || o.get(5) != "bob" || o.get(265) != id || o.get(222) != "1" ||
		o.get(27) != "hello.txt" || o.get(28) != "11" {
		t.Fatalf("offer: %v", o.pairs)
	}
	bob.send(svcFileTrans15, 0, kv(1, "bob"), kv(5, "alice"), kv(265, id), kv(222, "3"))
	if a := alice.expect(svcFileTrans15, nil); a.get(4) != "bob" || a.get(222) != "3" || a.get(265) != id {
		t.Fatalf("accept: %v", a.pairs)
	}
	alice.send(svcFileTransInfo15, 0, kv(1, "alice"), kv(5, "bob"), kv(265, id), kv(27, "hello.txt"),
		kv(249, "3"), kv(250, "192.0.2.1"))
	info := bob.expect(svcFileTransInfo15, nil)
	tok := info.get(251)
	if info.get(4) != "alice" || info.get(250) != "127.0.0.1" || len(tok) < 20 {
		t.Fatalf("info: %v", info.pairs)
	}
	bob.send(svcFileTransAcc15, 0, kv(1, "bob"), kv(5, "alice"), kv(265, id), kv(27, "hello.txt"),
		kv(249, "3"), kv(251, tok))
	if acc := alice.expect(svcFileTransAcc15, nil); acc.get(251) != tok || acc.get(4) != "bob" {
		t.Fatalf("ready: %v", acc.pairs)
	}

	relay := "http://" + e.web + "/relay?token=" + url.QueryEscape(tok)
	if code, _, _ := web(t, "GET", relay+"&sender=alice&recver=carolann", "", nil); code != http.StatusForbidden {
		t.Errorf("someone else's: %d", code)
	}
	// the receiver's HEAD comes first, and waits for the upload
	head := make(chan int, 1)
	go func() {
		code, _, h := web(t, "HEAD", relay+"&sender=alice&recver=bob", "", nil)
		if h.Get("Content-Length") != "11" {
			code = -1
		}
		head <- code
	}()
	if code, _, _ := web(t, "POST", relay+"&sender=alice&recver=bob", "T=x; Y=y", []byte("hello there")); code != 200 {
		t.Fatalf("upload: %d", code)
	}
	if code := <-head; code != 200 {
		t.Fatalf("head: %d", code)
	}
	if code, body, _ := web(t, "GET", relay+"&sender=alice&recver=bob", "", nil); code != 200 || body != "hello there" {
		t.Fatalf("download: %d %q", code, body)
	}
	if code, _, _ := web(t, "GET", relay, "", nil); code != http.StatusNotFound {
		t.Errorf("fetched twice: %d", code)
	}
	if code, _, _ := web(t, "POST", relay, "", []byte("again")); code != http.StatusNotFound {
		t.Errorf("uploaded again: %d", code)
	}

	// declined
	offer(alice, "bob", "second", "b.txt", "3")
	bob.expect(svcFileTrans15, func(p *packet) bool { return p.get(265) == "second" })
	bob.send(svcFileTrans15, 0, kv(1, "bob"), kv(5, "alice"), kv(265, "second"), kv(222, "4"))
	alice.expect(svcFileTrans15, func(p *packet) bool { return p.get(265) == "second" && p.get(222) == "4" })
	if x := e.srv.xferByID("second", alice.sessionFor(t, e)); x != nil {
		t.Error("declined offer kept")
	}
}

// sessionFor is the server's session for a client's user.
func (cl *client) sessionFor(t *testing.T, e *env) *session {
	t.Helper()
	u, _ := e.st.UserByName(cl.id)
	ss := e.srv.sessionsOf(u.ID)
	if len(ss) == 0 {
		t.Fatal("no session")
	}
	return ss[0]
}

func TestFileRefused(t *testing.T) {
	e := start(t)
	alice := signedIn(t, e, "alice", 15)
	declined := func(to, id, why string) {
		t.Helper()
		alice.expect(svcFileTrans15, func(p *packet) bool {
			return p.get(265) == id && p.get(222) == "4" && p.get(4) == to
		})
		m := alice.expect(svcMessage, nil)
		if m.get(4) != "bbs" || !strings.Contains(m.get(14), why) {
			t.Errorf("%s: %q", id, m.get(14))
		}
	}
	offer(alice, "carolann", "1", "a.txt", "10") // not on Yahoo
	declined("carolann", "1", "isn't on Yahoo! Messenger")
	offer(alice, "nobody", "2", "a.txt", "10")
	declined("nobody", "2", "not a user")
	signedIn(t, e, "bob", 10) // too old for 0xdc
	offer(alice, "bob", "3", "a.txt", "10")
	declined("bob", "3", "too old")
	signedIn(t, e, "bob", 15)
	offer(alice, "bob", "4", "a.txt", "2000000")
	declined("bob", "4", "too big")
}

func TestFileUpload(t *testing.T) {
	e := start(t)
	alice := signedIn(t, e, "alice", 10)
	bob := signedIn(t, e, "bob", 10)
	var cookie string
	for _, c := range alice.list.all(59) {
		if v, ok := strings.CutPrefix(c, "T\t"); ok {
			cookie = "Y=whatever; T=" + strings.SplitN(v, ";", 2)[0]
		}
	}
	if cookie == "" {
		t.Fatalf("no T cookie: %v", alice.list.pairs)
	}

	// as libyahoo2 0.7 sends it: a FILETRANSFER packet, key 29, the file
	body := func(to string, file []byte) []byte {
		p := &packet{version: 10, service: svcFileTransfer}
		p.add(0, "alice").add(5, to).add(14, "for you").add(27, "notes.txt").add(28, "9")
		b := p.encode()
		b = append(b, '2', '9', 0xc0, 0x80)
		return append(b, file...)
	}
	ft := "http://" + e.web + "/notifyft"
	if code, _, _ := web(t, "POST", ft, "Y=a; T=z=forged", body("bob", []byte("some text"))); code != http.StatusForbidden {
		t.Errorf("not signed in: %d", code)
	}
	if code, _, _ := web(t, "POST", ft, cookie, body("bob", []byte("some text"))); code != 200 {
		t.Fatalf("upload: %d", code)
	}
	m := bob.expect(svcFileTransfer, nil)
	u := m.get(20)
	if m.get(4) != "alice" || m.get(14) != "for you" || m.get(27) != "notes.txt" || m.get(28) != "9" ||
		!strings.HasPrefix(u, "http://"+e.web+"/ft/") || m.get(38) == "" {
		t.Fatalf("notice: %v", m.pairs)
	}
	if code, b, _ := web(t, "GET", u, "", nil); code != 200 || b != "some text" {
		t.Fatalf("download: %d %q", code, b)
	}
	if code, _, _ := web(t, "GET", u, "", nil); code != http.StatusNotFound {
		t.Errorf("fetched twice: %d", code)
	}

	// to someone not on Yahoo: refused, and alice told why
	if code, _, _ := web(t, "POST", ft, cookie, body("carolann", []byte("some text"))); code != http.StatusForbidden {
		t.Errorf("to carolann: %d", code)
	}
	alice.expect(svcMessage, func(p *packet) bool { return strings.Contains(p.get(14), "isn't on Yahoo") })
}
