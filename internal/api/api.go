// Package api is the BBS over HTTP: a JSON API under /api/v1, a stream of
// events (server-sent events), WebSockets in JSON and protobuf, and
// Prometheus metrics.  It acts through the BBS as callers do: the same
// checks, the same cleaning, the same logs.
package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/fido"
	"github.com/Neo-Desktop/bbs-sec/internal/imageboard"
	"github.com/Neo-Desktop/bbs-sec/internal/nntp"
	"github.com/Neo-Desktop/bbs-sec/internal/qwknet"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Scopes a token may have.
var Scopes = []string{"read", "post", "mail", "chat", "files", "sysop"}

// Settings are the [http] section's.
type Settings struct {
	Address    string
	TLS        bool
	CertFile   string
	KeyFile    string
	Anonymous  bool          // /system and /who without a token
	CORS       string        // an origin allowed to call from a browser ("": none)
	Metrics    string        // the metrics' own address ("": none)
	MaxPerIP   int           // connections at once from one address
	Rate       float64       // requests a second, per token (and per address before a login)
	Burst      int           // and at once
	SessionTTL time.Duration // of a login's token
	MaxBody    int64         // bytes of a JSON request
	Terminal   bool          // the web terminal page (if Server.Terminal is set)
	Forum      bool          // the web forum, /forum/
	Guests     bool          // the forum's guests read the areas anyone may
	Theme      string        // the forum's theme until a caller chooses ("": the default)
	ForumFlags bool          // the forum shows posters' countries' flags, where known
	Chan       bool          // the image boards, /chan/
	// TrustedProxies are reverse proxies believed when they say whose a
	// request is (X-Forwarded-For)
	TrustedProxies []*net.IPNet
	// SecureCookies: the forum's and image boards' cookies are sent over
	// HTTPS only (TLS here, or at a proxy in front)
	SecureCookies bool
	// ChanSettings are how the image boards are run
	ChanSettings imageboard.Settings
}

// Server serves the API.
type Server struct {
	B     *bbs.BBS
	S     Settings
	Fido  *fido.Fido    // nil: no FidoNet
	News  *nntp.Service // nil: no news
	Peers []*nntp.Peer  // the news peers
	// QWKNet calls the QWK networks' hubs (nil: no [qwknet])
	QWKNet *qwknet.Runner
	// Chan is the image boards, shared with the terminals (nil: made here)
	Chan *imageboard.Server
	Ctx  context.Context // the server's, for work started by a request
	// Terminal runs the text BBS on a web terminal's line (c a WebSocket
	// as a stream; host the caller's address, "" for a proxy's), cols x
	// rows: the listeners' server's WebTerminal
	Terminal func(c net.Conn, remote, host string, cols, rows int) error

	mu      sync.Mutex
	limits  map[string]*bucket
	streams map[int64][]*bbs.Node // a token's open streams' nodes
	perIP   map[string]int
}

// Token is a token's secret, and its hash as stored.
func NewToken() (secret, hash string) {
	var b [32]byte
	rand.Read(b[:])
	secret = "bbs_" + base64.RawURLEncoding.EncodeToString(b[:])
	return secret, HashToken(secret)
}

// HashToken is how a token is kept.
func HashToken(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// ParseScopes checks a list of scopes (commas or spaces).
func ParseScopes(s string) (string, error) {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		f = strings.ToLower(f)
		ok := false
		for _, k := range Scopes {
			ok = ok || f == k
		}
		if !ok {
			return "", fmt.Errorf("%q is not a scope (%s)", f, strings.Join(Scopes, ", "))
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return "", errors.New("no scopes")
	}
	return strings.Join(out, " "), nil
}

// who is a request's caller: a token's user, or nobody.
type who struct {
	user   *store.User
	token  *store.Token
	scopes map[string]bool
}

func (w *who) has(scope string) bool { return w != nil && w.scopes[scope] }

type ctxKey struct{}

func whoOf(r *http.Request) *who {
	w, _ := r.Context().Value(ctxKey{}).(*who)
	return w
}

// apiError is an error's JSON.
type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

var codes = map[int]string{
	http.StatusBadRequest: "bad_request", http.StatusUnauthorized: "unauthorized",
	http.StatusForbidden: "forbidden", http.StatusNotFound: "not_found",
	http.StatusConflict: "conflict", http.StatusTooManyRequests: "too_many",
	http.StatusRequestEntityTooLarge: "too_large", http.StatusServiceUnavailable: "unavailable",
	http.StatusInternalServerError: "internal", http.StatusMethodNotAllowed: "bad_method",
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg, Code: codes[status]})
}

// decode reads a JSON request body, no bigger than the limit, with no
// fields the type has not.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, s.S.MaxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			fail(w, http.StatusRequestEntityTooLarge, "the request is too big")
		} else {
			fail(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		}
		return false
	}
	return true
}

// bucket is a rate limit's token bucket.
type bucket struct {
	tokens float64
	last   time.Time
}

// allow takes one from key's bucket.
func (s *Server) allow(key string) bool {
	if s.S.Rate <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	b := s.limits[key]
	if b == nil {
		if len(s.limits) > 100000 {
			s.limits = map[string]*bucket{}
		}
		b = &bucket{tokens: float64(s.S.Burst), last: now}
		s.limits[key] = b
	}
	b.tokens = min(float64(s.S.Burst), b.tokens+now.Sub(b.last).Seconds()*s.S.Rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func hostOf(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// authenticate finds the request's token: "Authorization: Bearer ...".
func (s *Server) authenticate(r *http.Request) (*who, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return nil, nil
	}
	secret, ok := strings.CutPrefix(h, "Bearer ")
	if !ok {
		return nil, errors.New("Authorization must be Bearer")
	}
	return s.byToken(strings.TrimSpace(secret))
}

var errBadToken = errors.New("no such token, or it has expired")

// byToken finds who a token is.
func (s *Server) byToken(secret string) (*who, error) {
	if !strings.HasPrefix(secret, "bbs_") || len(secret) > 100 {
		return nil, errBadToken
	}
	t, err := s.B.Store.TokenByHash(HashToken(secret))
	if err != nil {
		return nil, errBadToken
	}
	u, err := s.B.Store.UserByID(t.UserID)
	if err != nil || u.Locked {
		return nil, errBadToken
	}
	s.B.Store.TouchToken(t.ID)
	w := &who{user: u, token: t, scopes: map[string]bool{}}
	for _, sc := range strings.Fields(t.Scopes) {
		// sysop needs a sysop, whatever the token says
		if sc != "sysop" || u.Sysop {
			w.scopes[sc] = true
		}
	}
	return w, nil
}

// route is a handler and what it needs: a scope ("": none, "any": a
// login), or anonymous access by the settings.
type route struct {
	scope string
	anon  bool // without a token when [http] anonymous = yes
	h     func(http.ResponseWriter, *http.Request)
}

func (s *Server) wrap(rt route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.B.Log.Printf("http %s %s: panic: %v", r.Method, r.URL.Path, v)
				fail(w, http.StatusInternalServerError, "something went wrong")
			}
		}()
		s.cors(w, r)
		wh, err := s.authenticate(r)
		key := "ip:" + hostOf(r)
		if wh != nil {
			key = fmt.Sprintf("token:%d", wh.token.ID)
		}
		if !s.allow(key) {
			w.Header().Set("Retry-After", "1")
			fail(w, http.StatusTooManyRequests, "too many requests: slow down")
			return
		}
		if err != nil {
			time.Sleep(s.B.FailDelay)
			fail(w, http.StatusUnauthorized, err.Error())
			return
		}
		switch {
		case wh == nil && rt.anon && s.S.Anonymous:
		case wh == nil && rt.scope != "":
			w.Header().Set("WWW-Authenticate", `Bearer realm="bbs"`)
			fail(w, http.StatusUnauthorized, "a token is needed (Authorization: Bearer ...)")
			return
		case wh == nil && rt.anon:
			fail(w, http.StatusUnauthorized, "a token is needed (Authorization: Bearer ...)")
			return
		case rt.scope != "" && rt.scope != "any" && !wh.has(rt.scope):
			fail(w, http.StatusForbidden, "the token has no "+rt.scope+" scope")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, wh)
		rt.h(w, r.WithContext(ctx))
	}
}

// cors answers a browser on the allowed origin.
func (s *Server) cors(w http.ResponseWriter, r *http.Request) {
	if s.S.CORS == "" || r.Header.Get("Origin") != s.S.CORS {
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", s.S.CORS)
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
	h.Set("Access-Control-Max-Age", "600")
	h.Add("Vary", "Origin")
}

// Handler is the API's routes.
func (s *Server) Handler() http.Handler {
	if s.limits == nil {
		s.limits = map[string]*bucket{}
		s.streams = map[int64][]*bbs.Node{}
		s.perIP = map[string]int{}
	}
	if s.S.MaxBody == 0 {
		s.S.MaxBody = 64 << 10
	}
	if s.Ctx == nil {
		s.Ctx = context.Background()
	}
	mux := http.NewServeMux()
	for pattern, rt := range s.routes() {
		mux.Handle(pattern, s.wrap(rt))
	}
	mux.HandleFunc("OPTIONS /api/", func(w http.ResponseWriter, r *http.Request) {
		s.cors(w, r)
		w.WriteHeader(http.StatusNoContent)
	})
	if s.S.Forum {
		fh, err := s.forum()
		if err != nil {
			s.B.Log.Printf("http: the forum: %v", err)
		} else {
			mux.Handle("/forum/", fh)
			mux.Handle("/forum", fh)
			mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/forum/", http.StatusFound)
			})
		}
	}
	if s.S.Chan {
		ch, err := s.imageboard()
		if err != nil {
			s.B.Log.Printf("http: the image boards: %v", err)
		} else {
			mux.Handle("/chan/", ch)
			mux.Handle("/chan", ch)
		}
	}
	if s.terminalOn() {
		mux.HandleFunc("GET /terminal", s.terminalPage)
		mux.HandleFunc("GET /terminal/ws", s.terminalWS)
		mux.HandleFunc("GET /terminal/{file}", s.terminalAsset)
		if !s.S.Forum {
			mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/terminal", http.StatusFound)
			})
		}
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "no such endpoint: see /api/v1/openapi.json")
	})
	return securityHeaders(s.realAddr(mux))
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// limitListener holds connections from one address to a number.
type limitListener struct {
	net.Listener
	s *Server
}

type limitConn struct {
	net.Conn
	s    *Server
	host string
	once sync.Once
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.s.B.Refuse("http", c.RemoteAddr()) {
			c.Close()
			continue
		}
		h, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		if ip := net.ParseIP(h); ip != nil && l.s.trusted(ip) {
			// a proxy's connections are many callers': not limited
			return c, nil
		}
		l.s.mu.Lock()
		if l.s.S.MaxPerIP > 0 && l.s.perIP[h] >= l.s.S.MaxPerIP {
			l.s.mu.Unlock()
			c.Close()
			continue
		}
		l.s.perIP[h]++
		l.s.mu.Unlock()
		return &limitConn{Conn: c, s: l.s, host: h}, nil
	}
}

func (c *limitConn) Close() error {
	c.once.Do(func() {
		c.s.mu.Lock()
		if c.s.perIP[c.host]--; c.s.perIP[c.host] <= 0 {
			delete(c.s.perIP, c.host)
		}
		c.s.mu.Unlock()
	})
	return c.Conn.Close()
}

// Run serves until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.S.Address)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	return s.Serve(ctx, ln)
}

// Serve serves on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.Ctx = ctx
	h := s.Handler()
	hs := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute,
		MaxHeaderBytes: 16 << 10, ErrorLog: s.B.Log,
		BaseContext: func(net.Listener) context.Context { return ctx }}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(sh)
	}()
	go s.expire(ctx)
	if s.S.Metrics != "" {
		go s.serveMetrics(ctx)
	}
	l := &limitListener{Listener: ln, s: s}
	s.B.Log.Printf("http: listening on %s (tls %v)", ln.Addr(), s.S.TLS)
	var err error
	if s.S.TLS {
		err = hs.ServeTLS(l, s.S.CertFile, s.S.KeyFile)
	} else {
		err = hs.Serve(l)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// expire forgets old sessions now and then.
func (s *Server) expire(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		s.B.Store.ExpireTokens()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
