package api

import (
	"embed"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/coder/websocket"
)

// The web terminal: a page with xterm.js (embedded: no CDN), and a
// WebSocket on which Server.Terminal runs the text BBS, as for a telnet
// caller.

//go:embed web
var webFiles embed.FS

var page = template.Must(template.ParseFS(webFiles, "web/index.html"))

// TerminalProtocol is the web terminal's WebSocket subprotocol: binary
// frames, the terminal's bytes both ways.
const TerminalProtocol = "bbs.terminal"

// webAssets are the files the page loads, and their types.
var webAssets = map[string]string{
	"xterm.js": "text/javascript", "addon-fit.js": "text/javascript", "addon-image.js": "text/javascript",
	"terminal.js": "text/javascript",
	"xterm.css":   "text/css", "terminal.css": "text/css", "LICENSE.xterm": "text/plain",
}

// originPatterns are the origins whose pages may open WebSockets here,
// beside the API's own host: the CORS origin, if there is one.
func (s *Server) originPatterns() []string {
	if s.S.CORS == "" {
		return nil
	}
	if u, err := url.Parse(s.S.CORS); err == nil {
		return []string{u.Host}
	}
	return nil
}

func (s *Server) terminalOn() bool { return s.Terminal != nil && s.S.Terminal }

func pageHeaders(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	// scripts only from here; xterm.js sets styles of its own
	host := strings.ReplaceAll(r.Host, "'", "")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; "+
		"font-src 'self'; img-src 'self' data: blob:; connect-src 'self' wss://"+host+" ws://"+host+"; "+
		"frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
}

func (s *Server) terminalPage(w http.ResponseWriter, r *http.Request) {
	pageHeaders(w, r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page.Execute(w, struct{ Name string }{s.B.Name})
}

func (s *Server) terminalAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	typ, ok := webAssets[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	b, err := webFiles.ReadFile(path.Join("web", name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", typ+"; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(b)
}

// terminalWS is the terminal's line: the size from the page, then the BBS.
func (s *Server) terminalWS(w http.ResponseWriter, r *http.Request) {
	if !s.allow("ip:" + hostOf(r)) {
		w.Header().Set("Retry-After", "1")
		fail(w, http.StatusTooManyRequests, "too many requests: slow down")
		return
	}
	size := func(key string, lo, hi, def int) int {
		n, err := strconv.Atoi(r.URL.Query().Get(key))
		if err != nil || n < lo || n > hi {
			return def
		}
		return n
	}
	cols, rows := size("cols", 40, 255, 80), size("rows", 10, 200, 24)
	opts := &websocket.AcceptOptions{Subprotocols: []string{TerminalProtocol}, OriginPatterns: s.originPatterns()}
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer c.CloseNow()
	if c.Subprotocol() != TerminalProtocol {
		c.Close(websocket.StatusPolicyViolation, "subprotocol "+TerminalProtocol+" only")
		return
	}
	c.SetReadLimit(64 << 10)
	nc := websocket.NetConn(r.Context(), c, websocket.MessageBinary)
	host := hostOf(r)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() && len(s.S.TrustedProxies) == 0 {
		host = "" // a proxy in front, not trusted to say whose: as a modem server's, not throttled as one
	}
	if err := s.Terminal(nc, r.RemoteAddr, host, cols, rows); err != nil {
		s.B.Log.Printf("web terminal %s: %v", r.RemoteAddr, err)
	}
	c.Close(websocket.StatusNormalClosure, "goodbye")
}
