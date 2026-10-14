// Package imageboard is the BBS's image boards, in the manner of
// kusaba-x and of 4chan that it followed (and Futallaby, Futaba before
// them): boards by directory (/b/), anonymous posts with an image each,
// threads bumped by their replies until a bump limit, pruned off the last
// page; tripcodes, sage, greentext, >>quotes, spoilers; a catalog, and
// 4chan's read-only JSON.  It is served by the HTTP API's server at
// /chan/, staff being the BBS's sysops and each board's mods.
package imageboard

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"image/color"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

//go:embed templates/*.html static/*
var content embed.FS

// Base is where the image boards are.
const Base = "/chan"

// Sessions are the logins: the HTTP API's session tokens, as the web
// forum's are.
type Sessions interface {
	Login(r *http.Request, name, password string) (secret string, u *store.User, err error)
	User(secret string) *store.User
	Logout(secret string)
}

// Settings are how the image boards are run, all boards alike.
type Settings struct {
	Dir        string        // where images and thumbnails are kept
	MaxFile    int64         // bytes, the largest image (a board may say less or more)
	ThreadWait time.Duration // between an address's threads
	ReplyWait  time.Duration // between its posts
	MaxComment int           // characters
	Strip      bool          // JPEGs' metadata (EXIF: where a phone took it) taken out
	Style      string        // the style callers get until they choose ("": by the board, as 4chan's)
	Captcha    bool          // those not signed in answer a captcha to post (a board may say otherwise)
	// the captchas: their letters, how long one lasts, how hard
	// (CaptchaLevels: easy, normal, hard)
	CaptchaLength int
	CaptchaTTL    time.Duration
	CaptchaLevel  string
	Options       bool   // the post form has an Options line (sage, nonoko; a board may say otherwise)
	GeoIP         string // the country database (geoip.Open's: .mmdb, or a CSV of ranges), for flags
	// the credit its licence asks for, at the foot of each page ("IP
	// geolocation by DB-IP", https://db-ip.com)
	GeoCredit, GeoCreditURL string
}

// Defaults are the settings unless bbs.conf says otherwise.
func Defaults() Settings {
	return Settings{Dir: "chan", MaxFile: 4 << 20, ThreadWait: 60 * time.Second, ReplyWait: 15 * time.Second,
		MaxComment: 2000, Strip: true, Captcha: true, CaptchaLength: 6, CaptchaTTL: 10 * time.Minute,
		CaptchaLevel: "normal", Options: true}
}

// Style is a look the boards can wear: a stylesheet.
type Style struct {
	Name, Title string
}

// Styles are the boards' looks: 4chan's, then kusaba's.
var Styles = []Style{
	{"yotsuba", "Yotsuba"},
	{"yotsuba-b", "Yotsuba B"},
	{"futaba", "Futaba"},
	{"burichan", "Burichan"},
	{"photon", "Photon"},
	{"tomorrow", "Tomorrow"},
}

// KnownStyle says whether there is a style of the name.
func KnownStyle(name string) bool {
	for _, s := range Styles {
		if s.Name == name {
			return true
		}
	}
	return false
}

// Server serves the image boards.
type Server struct {
	B        *bbs.BBS
	Sessions Sessions
	S        Settings
	Secure   bool          // cookies only over TLS
	TTL      time.Duration // a login's

	tmpl     *template.Template
	static   http.Handler
	key      []byte // for tokens, passwords and secure tripcodes: kept in Dir
	spoiler  []byte
	once     sync.Once
	initErr  error
	captchas captchas
	geo      geo
}

const (
	sessionCookie = "chan_session"
	passCookie    = "chan_pass"
	nameCookie    = "chan_name"
	styleCookie   = "chan_style"
)

// reserved are the names no board may have: the image boards' own.
var reserved = map[string]bool{"static": true, "manage": true, "login": true, "logout": true, "banned": true,
	"style": true, "boards.json": true,
	"captcha": true, "captcha.png": true, "captcha.json": true}

// ValidDir says what is wrong with a board's directory ("": nothing).
func ValidDir(dir string) string {
	if dir == "" || len(dir) > 16 || reserved[dir] {
		return "a board's directory is 1 to 16 letters or digits, and not one of the image boards' own names"
	}
	for _, c := range dir {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return "a board's directory is 1 to 16 lower-case letters or digits (b, g, tech)"
		}
	}
	return ""
}

// Handler is the image boards' routes, under Base.
func (s *Server) Handler() (http.Handler, error) {
	if err := s.Init(); err != nil {
		return nil, err
	}
	return http.HandlerFunc(s.route), nil
}

// Init readies the image boards: their directory, their secret (for
// tripcodes, before anyone posts), their pages.  Handler calls it; so
// must whoever posts without the web.
func (s *Server) Init() error {
	s.once.Do(func() { s.initErr = s.init() })
	return s.initErr
}

func (s *Server) init() error {
	t, err := template.New("").Funcs(funcs).ParseFS(content, "templates/*.html")
	if err != nil {
		return err
	}
	s.tmpl = t
	if err := os.MkdirAll(s.S.Dir, 0700); err != nil {
		return err
	}
	if s.key, err = loadKey(filepath.Join(s.S.Dir, "secret")); err != nil {
		return err
	}
	static, _ := fs.Sub(content, "static")
	files := http.FileServer(http.FS(static))
	s.static = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
	s.spoiler = spoilerImage()
	return nil
}

// loadKey is the image boards' secret, made the first time: it must last,
// or secure tripcodes and posts' passwords would change.
func loadKey(p string) ([]byte, error) {
	b, err := os.ReadFile(p)
	if err == nil && len(b) == 64 {
		return hex.DecodeString(string(b))
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	k := make([]byte, 32)
	rand.Read(k)
	return k, os.WriteFile(p, []byte(hex.EncodeToString(k)), 0600)
}

// req is a request in hand.
type req struct {
	w      http.ResponseWriter
	r      *http.Request
	s      *Server
	user   *store.User // logged in (nil: anonymous)
	secret string
	pass   string // the browser's posts' password (its cookie)
}

// route finds a request's page: /chan/, /chan/b/, /chan/b/2,
// /chan/b/thread/123, /chan/b/catalog, the JSON beside them, and the
// image boards' own pages.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if v := recover(); v != nil {
			s.B.Log.Printf("chan %s %s: panic: %v", r.Method, r.URL.Path, v)
			http.Error(w, "something went wrong", http.StatusInternalServerError)
		}
	}()
	q := &req{w: w, r: r, s: s}
	if c, err := r.Cookie(sessionCookie); err == nil {
		q.secret = c.Value
		q.user = s.Sessions.User(c.Value)
	}
	if c, err := r.Cookie(passCookie); err == nil && len(c.Value) >= 8 && len(c.Value) <= 64 {
		q.pass = c.Value
	}
	hd := w.Header()
	hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
	hd.Set("X-Frame-Options", "DENY")
	hd.Set("Referrer-Policy", "same-origin")

	path := strings.TrimPrefix(r.URL.Path, Base)
	if path == "" {
		http.Redirect(w, r, Base+"/", http.StatusMovedPermanently)
		return
	}
	seg := strings.Split(strings.TrimPrefix(path, "/"), "/")
	get, post := r.Method == http.MethodGet || r.Method == http.MethodHead, r.Method == http.MethodPost
	switch {
	case len(seg) == 1 && seg[0] == "" && get:
		s.front(q)
		return
	case seg[0] == "static" && len(seg) == 2 && get:
		if seg[1] == "spoiler.png" {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Write(s.spoiler)
			return
		}
		http.StripPrefix(Base+"/static/", s.static).ServeHTTP(w, r)
		return
	case len(seg) == 1 && seg[0] == "boards.json" && get:
		s.boardsJSON(q)
		return
	case len(seg) == 1 && seg[0] == "captcha.png" && get:
		s.captchaPNG(q)
		return
	case len(seg) == 1 && seg[0] == "captcha.json" && get:
		s.captchaJSON(q)
		return
	case len(seg) == 1 && seg[0] == "login" && (get || post):
		s.login(q)
		return
	case len(seg) == 1 && seg[0] == "logout" && post:
		s.logout(q)
		return
	case len(seg) == 1 && seg[0] == "style" && post:
		s.chooseStyle(q)
		return
	case seg[0] == "manage" && len(seg) <= 2:
		sub := ""
		if len(seg) == 2 {
			sub = seg[1]
		}
		s.manage(q, sub)
		return
	case reserved[seg[0]]:
		q.notFound()
		return
	}
	b, err := s.B.Store.Board(seg[0])
	if err != nil {
		q.notFound()
		return
	}
	if b.LoginToRead && q.user == nil {
		// a board for the BBS's users: nothing of it for others
		switch last := seg[len(seg)-1]; {
		case strings.HasSuffix(last, ".json"):
			q.jsonFail(http.StatusForbidden)
		case len(seg) == 3 && (seg[1] == "src" || seg[1] == "thumb"):
			q.notFound()
		case r.Method == http.MethodGet:
			http.Redirect(w, r, Base+"/login?back="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
		default:
			q.fail(http.StatusForbidden, "Only this BBS's users post here: sign in first.")
		}
		return
	}
	if len(seg) == 1 { // /chan/b
		http.Redirect(w, r, Base+"/"+b.Dir+"/", http.StatusMovedPermanently)
		return
	}
	switch rest := seg[1:]; {
	case len(rest) == 1 && rest[0] == "" && get:
		s.index(q, b, 1)
	case len(rest) == 1 && rest[0] == "catalog" && get:
		s.catalog(q, b)
	case len(rest) == 1 && rest[0] == "catalog.json" && get:
		s.catalogJSON(q, b)
	case len(rest) == 1 && rest[0] == "threads.json" && get:
		s.threadsJSON(q, b)
	case len(rest) == 1 && rest[0] == "archive" && get:
		http.Redirect(w, r, Base+"/"+b.Dir+"/catalog", http.StatusFound)
	case len(rest) == 1 && rest[0] == "post" && post:
		s.post(q, b)
	case len(rest) == 1 && rest[0] == "delete" && post:
		s.delete(q, b)
	case len(rest) == 1 && rest[0] == "report" && (get || post):
		s.report(q, b)
	case len(rest) == 1 && get:
		name, isJSON := strings.CutSuffix(rest[0], ".json")
		name = strings.TrimSuffix(name, ".html") // kusaba's 1.html
		page, err := strconv.Atoi(name)
		switch {
		case err != nil || page < 0 || page == 0 && isJSON:
			q.notFound()
		case isJSON:
			s.pageJSON(q, b, page)
		case page <= 1:
			http.Redirect(w, r, Base+"/"+b.Dir+"/", http.StatusMovedPermanently)
		default:
			s.index(q, b, page)
		}
	case len(rest) == 2 && (rest[0] == "thread" || rest[0] == "res") && get:
		name, isJSON := strings.CutSuffix(rest[1], ".json")
		name = strings.TrimSuffix(name, ".html") // kusaba's res/123.html
		no, err := strconv.ParseInt(name, 10, 64)
		switch {
		case err != nil || no < 1:
			q.notFound()
		case isJSON:
			s.threadJSON(q, b, no)
		case rest[0] == "res":
			http.Redirect(w, r, fmt.Sprintf("%s/%s/thread/%d", Base, b.Dir, no), http.StatusMovedPermanently)
		default:
			s.thread(q, b, no)
		}
	case len(rest) == 2 && (rest[0] == "src" || rest[0] == "thumb") && get:
		s.file(q, b, rest[0], rest[1])
	default:
		if !get && !post {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q.notFound()
	}
}

// host is the caller's address.
func (q *req) host() string {
	h, _, err := net.SplitHostPort(q.r.RemoteAddr)
	if err != nil {
		return q.r.RemoteAddr
	}
	return h
}

// staff is what the user is on a board: "admin" (a sysop), "mod" (one of
// its mods), or "".
func (q *req) staff(b *store.Board) string { return StaffOf(q.user, b) }

func isMod(b *store.Board, name string) bool {
	for _, m := range strings.FieldsFunc(b.Mods, func(r rune) bool { return r == ',' || r == ' ' }) {
		if strings.EqualFold(m, name) {
			return true
		}
	}
	return false
}

// anyStaff says whether the user is staff anywhere.
func (q *req) anyStaff() bool {
	if q.user == nil {
		return false
	}
	if q.user.Sysop {
		return true
	}
	bs, _ := q.s.B.Store.Boards()
	for _, b := range bs {
		if isMod(b, q.user.Name) {
			return true
		}
	}
	return false
}

// mac is a keyed hash of what is given, in hex.
func (s *Server) mac(parts ...string) string {
	m := hmac.New(sha256.New, s.key)
	for _, p := range parts {
		m.Write([]byte(p))
		m.Write([]byte{0})
	}
	return hex.EncodeToString(m.Sum(nil))[:32]
}

// token is what the forms carry, to show they are this site's: of the
// browser's password cookie, and of its login.
func (q *req) token() string {
	return q.s.mac("form", q.pass, q.secret)
}

// checkToken checks a form's token (after its form is read).
func (q *req) checkToken() bool {
	return q.pass != "" && hmac.Equal([]byte(q.r.FormValue("tok")), []byte(q.token()))
}

// ensurePass gives the browser a password for its posts if it has none:
// what deletes them, and what its forms' tokens are made of.
func (q *req) ensurePass() {
	if q.pass != "" {
		return
	}
	b := make([]byte, 8)
	rand.Read(b)
	q.pass = hex.EncodeToString(b)
	http.SetCookie(q.w, &http.Cookie{Name: passCookie, Value: q.pass, Path: Base, MaxAge: 365 * 24 * 3600,
		HttpOnly: true, Secure: q.s.Secure, SameSite: http.SameSiteLaxMode})
}

// page is what every page's layout shows.
type page struct {
	Title    string
	BBS      string
	Boards   []*store.Board
	Board    *store.Board // nil: none
	User     *store.User
	Staff    string // on this board
	AnyStaff bool
	Style    string
	Styles   []Style
	Token    string
	Pass     string
	Path     string
	Notice   string
	Body     any
	// the country database's credit (when there is one to give)
	GeoCredit, GeoCreditURL string
}

func (q *req) render(status int, name, title string, b *store.Board, body any) {
	q.ensurePass()
	p := page{Title: title, BBS: q.s.B.Name, Board: b, User: q.user, Staff: q.staff(b), AnyStaff: q.anyStaff(),
		Style: q.style(b), Styles: Styles, Token: q.token(), Pass: q.pass, Path: q.r.URL.RequestURI(),
		Notice: q.r.URL.Query().Get("notice"), Body: body}
	if q.s.S.GeoIP != "" {
		p.GeoCredit, p.GeoCreditURL = q.s.S.GeoCredit, q.s.S.GeoCreditURL
	}
	p.Boards, _ = q.s.B.Store.Boards()
	var buf strings.Builder
	if err := q.s.tmpl.ExecuteTemplate(&buf, name, p); err != nil {
		q.s.B.Log.Printf("chan: %s: %v", name, err)
		http.Error(q.w, "the page could not be made", http.StatusInternalServerError)
		return
	}
	q.w.Header().Set("Content-Type", "text/html; charset=utf-8")
	q.w.Header().Set("Cache-Control", "no-store")
	q.w.WriteHeader(status)
	fmt.Fprint(q.w, buf.String())
}

// style is the style the caller chose, else the image boards', else the
// board's: Yotsuba B for a worksafe one, Yotsuba for the rest.
func (q *req) style(b *store.Board) string {
	if c, err := q.r.Cookie(styleCookie); err == nil && KnownStyle(c.Value) {
		return c.Value
	}
	if KnownStyle(q.s.S.Style) {
		return q.s.S.Style
	}
	if b != nil && !b.Worksafe {
		return "yotsuba"
	}
	return "yotsuba-b"
}

// thumbBG is what shows through a transparent image's thumbnail: the
// posts' colour in the board's own style.
func thumbBG(b *store.Board) color.Color {
	if b.Worksafe {
		return color.RGBA{0xd6, 0xda, 0xf0, 0xff}
	}
	return color.RGBA{0xf0, 0xe0, 0xd6, 0xff}
}

// chooseStyle keeps a caller's style.
func (s *Server) chooseStyle(q *req) {
	q.r.ParseForm()
	name := q.r.PostForm.Get("style")
	if name != "" && !KnownStyle(name) {
		q.fail(http.StatusBadRequest, "There is no such style.")
		return
	}
	c := &http.Cookie{Name: styleCookie, Value: name, Path: Base, MaxAge: 365 * 24 * 3600, HttpOnly: true,
		Secure: s.Secure, SameSite: http.SameSiteLaxMode}
	if name == "" {
		c.MaxAge = -1
	}
	http.SetCookie(q.w, c)
	q.redirect(safeBack(q.r.PostForm.Get("back")))
}

// safeBack is a page of ours to go back to.
func safeBack(p string) string {
	if !strings.HasPrefix(p, Base+"/") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\\r\n") {
		return Base + "/"
	}
	return p
}

func (q *req) redirect(to string) { http.Redirect(q.w, q.r, to, http.StatusSeeOther) }

// fail shows an error, as 4chan's: what is wrong, and the way back.
func (q *req) fail(status int, msg string) {
	q.render(status, "error.html", "Error", nil, map[string]any{"Message": msg, "Back": q.r.Referer()})
}

func (q *req) notFound() {
	q.fail(http.StatusNotFound, "There is nothing here: no such board, thread or page.")
}

// login has a BBS user sign in: staff to moderate, and anyone where only
// users post.
func (s *Server) login(q *req) {
	back := safeBack(q.r.FormValue("back"))
	if q.r.Method == http.MethodGet {
		q.render(http.StatusOK, "login.html", "Sign in", nil, map[string]any{"Back": back})
		return
	}
	q.r.ParseForm()
	if !q.checkToken() {
		q.fail(http.StatusForbidden, "That form has expired: go back, reload the page and try again.")
		return
	}
	secret, _, err := s.Sessions.Login(q.r, q.r.PostForm.Get("name"), q.r.PostForm.Get("password"))
	if err != nil {
		q.render(http.StatusUnauthorized, "login.html", "Sign in", nil, map[string]any{"Back": back, "Error": err.Error()})
		return
	}
	http.SetCookie(q.w, &http.Cookie{Name: sessionCookie, Value: secret, Path: Base, HttpOnly: true, Secure: s.Secure,
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(s.TTL)})
	q.redirect(back)
}

func (s *Server) logout(q *req) {
	q.r.ParseForm()
	if q.secret != "" && q.checkToken() {
		s.Sessions.Logout(q.secret)
	}
	http.SetCookie(q.w, &http.Cookie{Name: sessionCookie, Value: "", Path: Base, MaxAge: -1, HttpOnly: true,
		Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	q.redirect(Base + "/")
}

// file serves an image or a thumbnail: by the name it was stored as.
func (s *Server) file(q *req, b *store.Board, kind, name string) {
	var ext string
	if kind == "thumb" {
		tim, ok := strings.CutSuffix(name, "s.jpg")
		if _, err := strconv.ParseInt(tim, 10, 64); !ok || err != nil {
			q.notFound()
			return
		}
		ext = ".jpg"
	} else {
		ext = filepath.Ext(name)
		if _, err := strconv.ParseInt(strings.TrimSuffix(name, ext), 10, 64); err != nil || imageTypes[ext] == "" {
			q.notFound()
			return
		}
	}
	f, err := os.Open(filepath.Join(s.S.Dir, b.Dir, kind, name))
	if err != nil {
		q.notFound()
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		q.notFound()
		return
	}
	h := q.w.Header()
	h.Set("Content-Type", imageTypes[ext])
	h.Set("Content-Security-Policy", "default-src 'none'")
	h.Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(q.w, q.r, "", fi.ModTime(), f)
}

// paths are where a board's files are kept.
func (s *Server) srcPath(dir string, tim int64, ext string) string {
	return filepath.Join(s.S.Dir, dir, "src", strconv.FormatInt(tim, 10)+ext)
}

func (s *Server) thumbPath(dir string, tim int64) string {
	return filepath.Join(s.S.Dir, dir, "thumb", strconv.FormatInt(tim, 10)+"s.jpg")
}

// removeFiles takes away posts' files.
func (s *Server) removeFiles(dir string, ps []*store.BoardPost) {
	for _, p := range ps {
		if p.Ext != "" {
			os.Remove(s.srcPath(dir, p.Tim, p.Ext))
			os.Remove(s.thumbPath(dir, p.Tim))
		}
	}
}

// RemoveBoard takes a board away, its posts and its files.
func (s *Server) RemoveBoard(b *store.Board) error {
	if err := s.B.Store.DeleteBoard(b.ID); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.S.Dir, b.Dir))
}

// NewBoard is a board as 4chan sets one up: ten pages of ten threads, a
// thread bumped by its first 300 replies and taking 150 images, its
// posters Anonymous.
func NewBoard(dir, title string) *store.Board {
	return &store.Board{Dir: dir, Title: title, Pages: 10, PerPage: 10, BumpLimit: 300, ImageLimit: 150,
		DefaultName: "Anonymous", Worksafe: true}
}

// IsAsset says whether a request is for a stylesheet, a script, an image
// or a thumbnail: files served as they are, which a page has many of.
func IsAsset(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	seg := strings.Split(strings.TrimPrefix(r.URL.Path, Base+"/"), "/")
	return len(seg) == 2 && seg[0] == "static" || len(seg) == 3 && (seg[1] == "src" || seg[1] == "thumb")
}
