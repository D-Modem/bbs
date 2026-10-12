package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Neo-Desktop/bbs-sec/internal/api"
)

// webTerm is a server with the web terminal page, on e's BBS.
func webTerm(t *testing.T, e *env) *httptest.Server {
	srv := New(e.b, nil)
	as := &api.Server{B: e.b, S: api.Settings{SessionTTL: time.Hour, Terminal: true, Rate: 100, Burst: 100},
		Terminal: srv.WebTerminal}
	ts := httptest.NewServer(as.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// TestWebTerminal: the page and its files, and a browser's terminal on the
// WebSocket logging in to the text BBS.
func TestWebTerminal(t *testing.T) {
	e := start(t, nil, &Listener{Name: "modem", Kind: "raw", Address: "modem.sock", ANSI: "no"})
	ts := webTerm(t, e)

	resp, body := get(t, ts.URL+"/")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/terminal" {
		t.Fatalf("/: %d %v", resp.StatusCode, resp.Header)
	}
	resp, body = get(t, ts.URL+"/terminal")
	csp := resp.Header.Get("Content-Security-Policy")
	if resp.StatusCode != 200 || !strings.Contains(body, "<title>Test BBS</title>") ||
		!strings.Contains(body, `src="/terminal/xterm.js"`) || !strings.Contains(csp, "script-src 'self'") ||
		!strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(body, "<script>") {
		t.Fatalf("page: %d %q\n%s", resp.StatusCode, csp, body)
	}
	for file, typ := range map[string]string{"xterm.js": "text/javascript", "addon-fit.js": "text/javascript",
		"terminal.js": "text/javascript", "xterm.css": "text/css", "terminal.css": "text/css"} {
		resp, body := get(t, ts.URL+"/terminal/"+file)
		if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), typ) || len(body) < 100 {
			t.Errorf("%s: %d %s %d bytes", file, resp.StatusCode, resp.Header.Get("Content-Type"), len(body))
		}
	}
	for _, p := range []string{"/terminal/index.html", "/terminal/..%2fapi.go", "/terminal/nope.js"} {
		if resp, _ := get(t, ts.URL+p); resp.StatusCode != 404 {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}

	dial := func(proto, origin string) (*websocket.Conn, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h := http.Header{}
		if origin != "" {
			h.Set("Origin", origin)
		}
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/terminal/ws?cols=100&rows=30",
			&websocket.DialOptions{Subprotocols: []string{proto}, HTTPHeader: h})
		return c, err
	}
	// another site's page may not open it
	if _, err := dial(api.TerminalProtocol, "https://evil.example"); err == nil {
		t.Fatal("a WebSocket from another origin")
	}
	c, err := dial(api.TerminalProtocol, ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	d := &dialog{t: t, c: websocket.NetConn(context.Background(), c, websocket.MessageBinary)}
	d.expect("Node 1, web terminal")
	d.expect("(or NEW)")
	d.send("alice\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("Command: ")
	d.send("P")
	d.expect("Terminal:  web terminal, 100x30, ANSI on, UTF-8 on")
	d.send("Q")
	d.expect("Command: ")
	if n := e.b.Nodes(); len(n) != 1 || n[0].Via != "web terminal" || n[0].User != "alice" {
		t.Errorf("nodes: %+v", n)
	}
	d.send("GY")
	d.expect("Thanks for calling.")
	if _, err := d.c.Read(make([]byte, 64)); err == nil {
		// the rest of the goodbye, then the close
		for err == nil {
			_, err = d.c.Read(make([]byte, 64))
		}
	}
	if n := e.b.Nodes(); len(n) != 0 {
		t.Errorf("still on: %+v", n)
	}

	// another subprotocol is refused
	c, err = dial("bbs.v1.json", ts.URL)
	if err == nil {
		_, _, err = c.Read(context.Background())
		if websocket.CloseStatus(err) != websocket.StatusPolicyViolation && err == nil {
			t.Fatal("a terminal without its subprotocol")
		}
	}
}
