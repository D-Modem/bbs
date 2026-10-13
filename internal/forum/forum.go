// Package forum is the BBS's message areas as a web forum, in the manner
// of the bulletin boards of the 2010s: categories of forums, topics, posts
// with their authors beside them.  It is served by the HTTP API's server
// at /forum/, on the same accounts, areas and messages as the terminals:
// a post here is a post there.
package forum

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

//go:embed templates/*.html themes/*/*.html static/*
var content embed.FS

// Sessions are the logins: the HTTP API's session tokens.
type Sessions interface {
	// Login checks a name and password (throttled and logged as any
	// login) and gives a session's secret.
	Login(r *http.Request, name, password string) (secret string, u *store.User, err error)
	// User is a session's user (nil: none, or ended).
	User(secret string) *store.User
	// Logout ends a session.
	Logout(secret string)
}

// Theme is a look the forum can wear: its own stylesheet, and templates
// of its own in themes/<Name>/ for the pages it lays out differently
// (the rest are its parent's, else the default's).
type Theme struct {
	Name   string // as in bbs.conf's forum_theme
	Title  string // as the theme chooser shows it
	Parent string // a theme whose templates this one wears too, under its own ("": none)
}

// Themes are the forum's looks; the first is the default.
var Themes = []Theme{
	{"ipb3", "Default (IPB 3 style)", ""},
	{"ipb5", "Fresh (IPB 5 style)", ""},
	{"ipb4", "Modern (IPB 4 style)", ""},
	{"ipb2", "Classic (IPB 2 style)", ""},
	{"ipb1", "Retro (IPB 1 style)", ""},
	{"ikon", "Grey (Ikonboard style)", ""},
	{"vb6", "Clean (vBulletin 6 style)", "vb5"},
	{"vb5", "Connect (vBulletin 5 style)", ""},
	{"vb4", "Steel (vBulletin 4 style)", ""},
	{"vb3", "Blue (vBulletin 3 style)", ""},
	{"vb2", "Purple (vBulletin 2 style)", ""},
	{"vblite", "Lavender (vBulletin Lite style)", ""},
	{"xf23", "Light (XenForo 2.3 style)", "xf2"},
	{"xf2", "Xen (XenForo 2 style)", ""},
	{"xf1", "Classic (XenForo 1 style)", ""},
	{"mybb18", "Flat (MyBB 1.8 style)", "mybb16"},
	{"mybb16", "Classic (MyBB 1.6 style)", "mybb12"},
	{"mybb12", "Original (MyBB 1.2 style)", ""},
	{"pun14", "Oxygen 2 (PunBB 1.4 style)", ""},
	{"flux15", "Air (FluxBB 1.5 style)", "pun12"},
	{"pun12", "Oxygen (PunBB 1.2 style)", ""},
	{"yabb25", "Silver (YaBB 2.5 style)", "yabb2"},
	{"yabb2", "Default (YaBB 2 style)", "yabb1"},
	{"yabb1", "Gold (YaBB 1 style)", ""},
	{"smf30", "Natural (SMF 3.0 style)", "smf21"},
	{"smf21", "Simple (SMF 2.1 style)", "smf20"},
	{"smf20", "Curve (SMF 2.0 style)", ""},
	{"smf1", "Core (SMF 1 style)", ""},
	{"phpbb1", "Dark (phpBB 1 style)", ""},
	{"phpbb2", "Silver (phpBB 2 style)", ""},
	{"phpbb30", "prosilver (phpBB 3.0 style)", ""},
	{"phpbb31", "prosilver (phpBB 3.1 style)", ""},
	{"phpbb32", "prosilver (phpBB 3.2 style)", "phpbb31"},
	{"phpbb33", "prosilver (phpBB 3.3 style)", "phpbb32"},
}

// lineage is a theme's ancestors and itself, the eldest first: the order
// their templates are laid one over another.
func lineage(th Theme) []string {
	names := []string{th.Name}
	for p := th.Parent; p != ""; {
		names = append([]string{p}, names...)
		next := ""
		for _, t := range Themes {
			if t.Name == p {
				next = t.Parent
			}
		}
		p = next
	}
	return names
}

// KnownTheme says whether there is a theme of the name.
func KnownTheme(name string) bool {
	for _, t := range Themes {
		if t.Name == name {
			return true
		}
	}
	return false
}

// Server serves the forum.
type Server struct {
	B        *bbs.BBS
	Sessions Sessions
	Guests   bool          // those not logged in read the areas anyone may
	Secure   bool          // the cookie only over TLS
	Terminal bool          // the web terminal is there to link to
	TTL      time.Duration // a login's
	Theme    string        // the theme callers get until they choose ("": the first)

	tmpl map[string]*template.Template // by theme
	key  []byte                        // for forms' CSRF tokens
}

// Base is where the forum is.
const Base = "/forum"

const (
	cookieName  = "bbs_forum"
	themeCookie = "bbs_forum_theme"
	perPage     = 20
)

// Handler is the forum's routes, under Base.
func (s *Server) Handler() (http.Handler, error) {
	if s.tmpl == nil {
		base, err := template.New("").Funcs(funcs).ParseFS(content, "templates/*.html")
		if err != nil {
			return nil, err
		}
		s.tmpl = map[string]*template.Template{}
		for _, th := range Themes {
			t, err := base.Clone()
			if err != nil {
				return nil, err
			}
			for _, name := range lineage(th) {
				if own, _ := fs.Glob(content, "themes/"+name+"/*.html"); len(own) > 0 {
					if t, err = t.ParseFS(content, own...); err != nil {
						return nil, fmt.Errorf("theme %s: %w", name, err)
					}
				}
			}
			s.tmpl[th.Name] = t
		}
		s.key = make([]byte, 32)
		rand.Read(s.key)
	}
	static, _ := fs.Sub(content, "static")
	mux := http.NewServeMux()
	mux.Handle("GET "+Base+"/static/", http.StripPrefix(Base+"/static/", staticFiles(static)))
	for pattern, h := range map[string]func(*req){
		"GET " + Base + "/{$}":               s.index,
		"GET " + Base + "/area/{id}":         s.area,
		"POST " + Base + "/area/{id}/read":   s.markRead,
		"GET " + Base + "/area/{id}/new":     s.newTopic,
		"POST " + Base + "/area/{id}/new":    s.newTopic,
		"GET " + Base + "/topic/{id}":        s.topic,
		"POST " + Base + "/topic/{id}/reply": s.reply,
		"GET " + Base + "/post/{id}":         s.findPost,
		"POST " + Base + "/post/{id}/like":   s.like,
		"POST " + Base + "/post/{id}/answer": s.answer,
		"POST " + Base + "/post/{id}/delete": s.deletePost,
		"GET " + Base + "/user/{name}":       s.profile,
		"GET " + Base + "/online":            s.online,
		"GET " + Base + "/search":            s.search,
		"GET " + Base + "/login":             s.login,
		"POST " + Base + "/login":            s.login,
		"POST " + Base + "/logout":           s.logout,
		"GET " + Base + "/register":          s.register,
		"POST " + Base + "/register":         s.register,
		"POST " + Base + "/theme":            s.chooseTheme,
	} {
		mux.HandleFunc(pattern, s.wrap(h))
	}
	mux.HandleFunc(Base, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, Base+"/", http.StatusMovedPermanently)
	})
	return mux, nil
}

func staticFiles(f fs.FS) http.Handler {
	files := http.FileServer(http.FS(f))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}

// req is a request in hand: who asks, and the page being made.
type req struct {
	w      http.ResponseWriter
	r      *http.Request
	s      *Server
	user   *store.User // nil: a guest
	secret string
}

func (s *Server) wrap(h func(*req)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.B.Log.Printf("forum %s %s: panic: %v", r.Method, r.URL.Path, v)
				http.Error(w, "something went wrong", http.StatusInternalServerError)
			}
		}()
		q := &req{w: w, r: r, s: s}
		if c, err := r.Cookie(cookieName); err == nil {
			q.secret = c.Value
			q.user = s.Sessions.User(c.Value)
		}
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "same-origin")
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
			if err := r.ParseForm(); err != nil {
				q.fail(http.StatusBadRequest, "That form could not be read.")
				return
			}
			// a logged-in user's forms carry their token (logins and
			// sign-ups have no session yet)
			if q.user != nil && !hmac.Equal([]byte(r.PostForm.Get("csrf")), []byte(q.csrf())) {
				q.fail(http.StatusForbidden, "That form has expired: go back, reload the page and try again.")
				return
			}
		}
		h(q)
	}
}

// csrf is the token a logged-in user's forms carry: their session's.
func (q *req) csrf() string {
	if q.secret == "" {
		return ""
	}
	m := hmac.New(sha256.New, q.s.key)
	m.Write([]byte(q.secret))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

// host is the caller's address.
func (q *req) host() string {
	h, _, err := net.SplitHostPort(q.r.RemoteAddr)
	if err != nil {
		return q.r.RemoteAddr
	}
	return h
}

// node is a node for the user's doing something as on the BBS (posting,
// voting): not in who's online, as the API's requests are.
func (q *req) node() *bbs.Node {
	n := q.s.B.Agent(q.user, "web")
	n.Caller.Source = "ip:" + q.host()
	return n
}

// canRead: the user (or a guest) may read the area.
func (q *req) canRead(a *store.Area) bool {
	if q.user == nil {
		return q.s.Guests && a.ReadReq == ""
	}
	return q.s.B.CanRead(q.user, a)
}

func (q *req) canPost(a *store.Area) bool {
	return q.user != nil && q.s.B.CanPost(q.user, a)
}

// areas are the areas the user may read.
func (q *req) areas() ([]*store.Area, error) {
	all, err := q.s.B.Store.Areas()
	if err != nil {
		return nil, err
	}
	var out []*store.Area
	for _, a := range all {
		if q.canRead(a) {
			out = append(out, a)
		}
	}
	return out, nil
}

// page is what every page's layout shows.
type page struct {
	Title    string
	BBS      string
	User     *store.User
	CSRF     string
	Crumbs   []crumb
	Tab      string // forums, online, search
	Terminal bool
	Register bool
	Notice   string
	Now      time.Time
	Online   int
	Theme    string  // the theme in use
	Themes   []Theme // to choose from
	Path     string  // this page's, to come back to
	Page     string  // which page it is (its template's name), for layouts that differ by page
	Body     any     // the page's own
}

type crumb struct {
	Name, URL string
}

// render shows a page.
func (q *req) render(status int, name, title string, crumbs []crumb, body any) {
	p := page{Title: title, BBS: q.s.B.Name, User: q.user, CSRF: q.csrf(), Crumbs: crumbs, Terminal: q.s.Terminal,
		Register: q.user == nil && q.s.B.NewUsers, Now: time.Now(), Body: body, Notice: q.r.URL.Query().Get("notice"),
		Theme: q.theme(), Themes: Themes, Path: q.r.URL.RequestURI(), Page: name}
	switch {
	case strings.HasPrefix(name, "online"):
		p.Tab = "online"
	case strings.HasPrefix(name, "search"):
		p.Tab = "search"
	default:
		p.Tab = "forums"
	}
	for _, n := range q.s.B.Nodes() {
		if n.User != "" {
			p.Online++
		}
	}
	var buf strings.Builder
	if err := q.s.tmpl[p.Theme].ExecuteTemplate(&buf, name, p); err != nil {
		q.s.B.Log.Printf("forum: %s: %v", name, err)
		http.Error(q.w, "the page could not be made", http.StatusInternalServerError)
		return
	}
	q.w.Header().Set("Content-Type", "text/html; charset=utf-8")
	q.w.Header().Set("Cache-Control", "no-store")
	q.w.WriteHeader(status)
	fmt.Fprint(q.w, buf.String())
}

// theme is the theme the caller chose, or the forum's.
func (q *req) theme() string {
	if c, err := q.r.Cookie(themeCookie); err == nil && KnownTheme(c.Value) {
		return c.Value
	}
	if KnownTheme(q.s.Theme) {
		return q.s.Theme
	}
	return Themes[0].Name
}

// chooseTheme keeps the caller's choice of theme (in a cookie: guests
// choose too) and goes back to the page they chose it on.
func (s *Server) chooseTheme(q *req) {
	name := q.r.PostForm.Get("theme")
	if !KnownTheme(name) {
		q.fail(http.StatusBadRequest, "There is no such theme.")
		return
	}
	http.SetCookie(q.w, &http.Cookie{Name: themeCookie, Value: name, Path: Base, MaxAge: 365 * 24 * 3600,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	q.redirect(safeBack(q.r.PostForm.Get("back")))
}

// fail shows an error page.
func (q *req) fail(status int, msg string) {
	q.render(status, "error.html", "Error", nil, map[string]any{"Message": msg, "Status": status})
}

// needLogin sends a guest to the login page, back here after.
func (q *req) needLogin() bool {
	if q.user != nil {
		return false
	}
	back := q.r.URL.Path
	if q.r.Method != http.MethodGet {
		back = q.r.Referer()
		if !strings.HasPrefix(back, "/") {
			back = Base + "/"
		}
	}
	http.Redirect(q.w, q.r, Base+"/login?back="+urlEscape(back), http.StatusSeeOther)
	return true
}

// back sends the user on after a form.
func (q *req) redirect(to string) { http.Redirect(q.w, q.r, to, http.StatusSeeOther) }

// errNotFound pages
var errNoArea = errors.New("there is no such forum, or it is not open to you")

// setCookie gives the browser its session.
func (q *req) setCookie(secret string) {
	c := &http.Cookie{Name: cookieName, Value: secret, Path: Base, HttpOnly: true, Secure: q.s.Secure,
		SameSite: http.SameSiteLaxMode}
	if secret == "" {
		c.MaxAge = -1
	} else {
		c.Expires = time.Now().Add(q.s.TTL)
	}
	http.SetCookie(q.w, c)
}
