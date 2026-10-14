package imageboard

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// sessions are logins kept in memory (the API's are tested there).
type sessions struct {
	b  *bbs.BBS
	mu sync.Mutex
	m  map[string]int64
}

func (s *sessions) Login(r *http.Request, name, pw string) (string, *store.User, error) {
	u, err := s.b.Authenticate("chan", "test", name, pw)
	if err != nil {
		return "", nil, err
	}
	var k [16]byte
	rand.Read(k[:])
	secret := hex.EncodeToString(k[:])
	s.mu.Lock()
	s.m[secret] = u.ID
	s.mu.Unlock()
	return secret, u, nil
}

func (s *sessions) User(secret string) *store.User {
	s.mu.Lock()
	id, ok := s.m[secret]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	u, _ := s.b.Store.UserByID(id)
	return u
}

func (s *sessions) Logout(secret string) {
	s.mu.Lock()
	delete(s.m, secret)
	s.mu.Unlock()
}

type site struct {
	t   *testing.T
	b   *bbs.BBS
	ts  *httptest.Server
	srv *Server
	bd  *store.Board
	log *bytes.Buffer
}

func setup(t *testing.T) *site {
	t.Helper()
	store.BcryptCost = 4
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := bbs.Defaults()
	s.Name, s.FailDelay = "Test Board", 0
	lb := &bytes.Buffer{}
	b := bbs.New(s, st, log.New(lb, "", 0))
	st.CreateUser("sysop", "password1", "Here", true)
	st.CreateUser("mia", "password1", "Here", false)
	bd, err := st.CreateBoard(NewBoard("b", "Random"))
	if err != nil {
		t.Fatal(err)
	}
	set := Defaults()
	set.Dir, set.ThreadWait, set.ReplyWait, set.Captcha = filepath.Join(dir, "chan"), 0, 0, false
	srv := &Server{B: b, Sessions: &sessions{b: b, m: map[string]int64{}}, S: set, TTL: time.Hour}
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("log:\n%s", lb)
		}
	})
	return &site{t: t, b: b, ts: ts, srv: srv, bd: bd, log: lb}
}

type browser struct {
	s   *site
	c   *http.Client
	tok string
}

func (s *site) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{s: s, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
}

var tokPat = regexp.MustCompile(`name="tok" value="([0-9a-f]+)"`)

// get fetches a page: its status, where it sends on to, and its body.
func (br *browser) get(path string) (int, string, string) {
	br.s.t.Helper()
	resp, err := br.c.Get(br.s.ts.URL + path)
	if err != nil {
		br.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if m := tokPat.FindSubmatch(b); m != nil {
		br.tok = string(m[1])
	}
	return resp.StatusCode, resp.Header.Get("Location"), string(b)
}

func (br *browser) postForm(path string, v url.Values) (int, string, string) {
	br.s.t.Helper()
	if v.Get("tok") == "" {
		v.Set("tok", br.tok)
	}
	resp, err := br.c.PostForm(br.s.ts.URL+path, v)
	if err != nil {
		br.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Location"), string(b)
}

// post posts to a board as its form does: fields, and an image (nil:
// none).
func (br *browser) post(dir string, fields map[string]string, file []byte, fileName string) (int, string, string) {
	br.s.t.Helper()
	if br.tok == "" {
		br.get("/chan/" + dir + "/")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("tok", br.tok)
	for k, v := range fields {
		w.WriteField(k, v)
	}
	if file != nil {
		fw, _ := w.CreateFormFile("upfile", fileName)
		fw.Write(file)
	}
	w.Close()
	req, _ := http.NewRequest("POST", br.s.ts.URL+"/chan/"+dir+"/post", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := br.c.Do(req)
	if err != nil {
		br.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Location"), string(b)
}

// pngOf is a w by h PNG, half transparent, of a colour.
func pngOf(w, h int, c color.RGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.Set(x, y, c)
			}
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func jpegWithExif() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	j := b.Bytes()
	exif := []byte("Exif\x00\x00GPS-SECRET-PLACE")
	seg := append([]byte{0xff, 0xe1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}, exif...)
	return append(append(append([]byte{}, j[:2]...), seg...), j[2:]...)
}

func TestImageBoard(t *testing.T) {
	s := setup(t)
	anon := s.browser()

	if code, _, body := anon.get("/chan/"); code != 200 || !strings.Contains(body, "/b/ - Random") {
		t.Fatalf("front page %d:\n%s", code, body)
	}
	if code, _, body := anon.get("/chan/b/"); code != 200 || !strings.Contains(body, "New Thread") ||
		!strings.Contains(body, "yotsuba-b.css") {
		t.Fatalf("board %d:\n%s", code, body)
	}

	// a thread needs an image
	if code, _, body := anon.post("b", map[string]string{"com": "hello"}, nil, ""); code != 400 ||
		!strings.Contains(body, "You forgot to upload your image") {
		t.Fatalf("no image: %d", code)
	}
	// not a forged form
	other := s.browser()
	other.tok = "00000000000000000000000000000000"
	if code, _, _ := other.post("b", map[string]string{"com": "x"}, pngOf(10, 10, color.RGBA{255, 0, 0, 255}), "a.png"); code != 403 {
		t.Fatalf("forged: %d", code)
	}
	// not a file that is not an image
	if code, _, body := anon.post("b", map[string]string{"com": "x"}, []byte("<svg onload=alert(1)>"), "x.png"); code != 400 ||
		!strings.Contains(body, "not an image") {
		t.Fatalf("not an image: %d", code)
	}

	img1 := pngOf(600, 300, color.RGBA{255, 0, 0, 255})
	code, loc, body := anon.post("b", map[string]string{"name": "Anon#tea", "sub": "First", "pwd": "secret1",
		"com": ">implying\n>>1 and >>99 and >>>/b/1\n[spoiler]hidden[/spoiler] https://example.org/x."}, img1, "red square.png")
	if code != 303 || loc != "/chan/b/thread/1#p1" {
		t.Fatalf("a new thread: %d %q\n%s", code, loc, body)
	}
	p1, err := s.b.Store.ChanPost(s.bd.ID, 1)
	if err != nil || p1.Ext != ".png" || p1.W != 600 || p1.H != 300 || p1.TnW != 250 || p1.TnH != 125 || p1.Trip != "!WokonZwxw2" ||
		p1.Name != "Anon" || p1.FileName != "red square" {
		t.Fatalf("the post: %+v", p1)
	}
	code, _, body = anon.get("/chan/b/thread/1")
	for _, want := range []string{`<span class="postertrip">!WokonZwxw2</span>`, `<span class="quote">&gt;implying</span>`,
		`<a href="#p1" class="quotelink" data-no="1">&gt;&gt;1 (OP)</a>`, `<span class="deadlink">&gt;&gt;99</span>`,
		`<a href="/chan/b/thread/1#p1" class="quotelink">&gt;&gt;&gt;/b/1</a>`, `<s>hidden</s>`,
		`<a href="https://example.org/x" rel="nofollow noreferrer noopener" target="_blank">https://example.org/x</a>.`,
		`/chan/b/thumb/` + itoa(p1.Tim) + `s.jpg`, `width="250" height="125"`, "Posting mode: Reply", "red square.png"} {
		if !strings.Contains(body, want) {
			t.Errorf("thread page: no %s", want)
		}
	}
	if t.Failed() {
		t.Fatalf("%d\n%s", code, body)
	}
	// the files, served as what they are
	resp, _ := http.Get(s.ts.URL + "/chan/b/src/" + itoa(p1.Tim) + ".png")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("image: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp.Body.Close()
	resp, _ = http.Get(s.ts.URL + "/chan/b/thumb/" + itoa(p1.Tim) + "s.jpg")
	tb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if th, err := jpeg.DecodeConfig(bytes.NewReader(tb)); err != nil || th.Width != 250 || th.Height != 125 {
		t.Errorf("thumbnail: %v %+v", err, th)
	}
	for _, bad := range []string{"/chan/b/src/..%2fsecret", "/chan/b/src/1.svg", "/chan/b/thumb/1.png", "/chan/../bbs.db"} {
		if code, _, _ := anon.get(bad); code != 404 && code != 301 && code != 400 {
			t.Errorf("%s: %d", bad, code)
		}
	}

	// flood control
	s.srv.S.ReplyWait = time.Hour
	if code, _, body := anon.post("b", map[string]string{"resto": "1", "com": "too soon"}, nil, ""); code != 429 ||
		!strings.Contains(body, "Flood detected") {
		t.Fatalf("flood: %d", code)
	}
	s.srv.S.ReplyWait = 0

	// replies: one quoting the first (a backlink), one saged
	if code, loc, _ := anon.post("b", map[string]string{"resto": "1", "com": ">>1\nagreed", "pwd": "secret2"}, nil, ""); code != 303 ||
		loc != "/chan/b/thread/1#p2" {
		t.Fatalf("reply: %d %s", code, loc)
	}
	before, _ := s.b.Store.ChanThread(s.bd.ID, 1)
	time.Sleep(1100 * time.Millisecond)
	anon.post("b", map[string]string{"resto": "1", "com": "quiet", "email": "sage"}, nil, "")
	after, _ := s.b.Store.ChanThread(s.bd.ID, 1)
	if !after.Bumped.Equal(before.Bumped) || after.Replies != 2 {
		t.Errorf("sage bumped it: %v %v, %d replies", before.Bumped, after.Bumped, after.Replies)
	}
	_, _, body = anon.get("/chan/b/thread/1")
	if !strings.Contains(body, `<div class="backlink"><span><a href="#p2" class="quotelink" data-no="2">&gt;&gt;2</a></span>`) {
		t.Errorf("no backlink:\n%s", body)
	}
	// the same image again
	if code, _, body := anon.post("b", map[string]string{"resto": "1"}, img1, "again.png"); code != 409 ||
		!strings.Contains(body, "/chan/b/thread/1#p1") {
		t.Errorf("duplicate: %d", code)
	}
	// a JPEG's EXIF is taken out
	code, _, _ = anon.post("b", map[string]string{"resto": "1", "spoiler": "on"}, jpegWithExif(), "phone.jpg")
	p4, _ := s.b.Store.ChanPost(s.bd.ID, 4)
	if code != 303 || p4 == nil || !p4.Spoiler {
		t.Fatalf("jpeg: %d %+v", code, p4)
	}
	kept, _ := os.ReadFile(s.srv.srcPath("b", p4.Tim, ".jpg"))
	if bytes.Contains(kept, []byte("GPS-SECRET")) || len(kept) == 0 {
		t.Error("the EXIF was kept")
	}

	// the index: the thread with its last replies; the catalog
	_, _, body = anon.get("/chan/b/")
	if !strings.Contains(body, `[<a href="/chan/b/thread/1" class="replylink">Reply</a>]`) || !strings.Contains(body, "spoiler.png") {
		t.Errorf("index:\n%s", body)
	}
	if code, _, body := anon.get("/chan/b/catalog"); code != 200 || !strings.Contains(body, "R: <b>3</b> / I: <b>1</b>") {
		t.Errorf("catalog %d:\n%s", code, body)
	}

	// JSON as 4chan's
	var boards struct {
		Boards []struct {
			Board string `json:"board"`
			Pages int    `json:"pages"`
		} `json:"boards"`
	}
	getJSON(t, s.ts.URL+"/chan/boards.json", &boards)
	if len(boards.Boards) != 1 || boards.Boards[0].Board != "b" || boards.Boards[0].Pages != 10 {
		t.Errorf("boards.json: %+v", boards)
	}
	var thread struct {
		Posts []apiPost `json:"posts"`
	}
	getJSON(t, s.ts.URL+"/chan/b/thread/1.json", &thread)
	if len(thread.Posts) != 4 || thread.Posts[0].No != 1 || thread.Posts[0].Tim != p1.Tim || thread.Posts[0].MD5 != p1.MD5 ||
		*thread.Posts[0].Replies != 3 || thread.Posts[0].SemanticURL != "first" || thread.Posts[1].Resto != 1 ||
		thread.Posts[3].Spoiler != 1 {
		t.Errorf("thread.json: %+v", thread.Posts)
	}
	var catalog []struct {
		Page    int       `json:"page"`
		Threads []apiPost `json:"threads"`
	}
	getJSON(t, s.ts.URL+"/chan/b/catalog.json", &catalog)
	if len(catalog) != 1 || len(catalog[0].Threads) != 1 || len(catalog[0].Threads[0].LastReplies) != 3 {
		t.Errorf("catalog.json: %+v", catalog)
	}
	var page struct {
		Threads []struct {
			Posts []apiPost `json:"posts"`
		} `json:"threads"`
	}
	getJSON(t, s.ts.URL+"/chan/b/1.json", &page)
	if len(page.Threads) != 1 || len(page.Threads[0].Posts) != 4 {
		t.Errorf("1.json: %+v", page)
	}

	// deleting: by its password, not by another's
	if code, _, _ := other.postForm("/chan/b/delete", url.Values{"2": {"delete"}, "pwd": {"wrong"}, "tok": {otherTok(other)}}); code != 403 {
		t.Errorf("deleted with the wrong password: %d", code)
	}
	if code, _, _ := anon.postForm("/chan/b/delete", url.Values{"2": {"delete"}, "pwd": {"secret2"}}); code != 303 {
		t.Errorf("delete: %d", code)
	}
	if _, err := s.b.Store.ChanPost(s.bd.ID, 2); err == nil {
		t.Error("not deleted")
	}
	anon.postForm("/chan/b/delete", url.Values{"1": {"delete"}, "pwd": {"secret1"}, "onlyimgdel": {"on"}})
	if p, _ := s.b.Store.ChanPost(s.bd.ID, 1); !p.FileDeleted {
		t.Error("its file not deleted")
	}
	if _, err := os.Stat(s.srv.srcPath("b", p1.Tim, ".png")); err == nil {
		t.Error("the image is still there")
	}
	if _, _, body := anon.get("/chan/b/thread/1"); !strings.Contains(body, "File deleted.") {
		t.Error("not shown deleted")
	}

	// reports, and staff: sticky, close, ban
	anon.get("/chan/b/report?no=3")
	if code, _, body := anon.postForm("/chan/b/report", url.Values{"no": {"3"}, "reason": {reportReasons[2]}}); code != 200 ||
		!strings.Contains(body, "staff will see it") {
		t.Errorf("report: %d", code)
	}
	if code, _, _ := anon.get("/chan/manage"); code != 303 {
		t.Errorf("manage without a login: %d", code)
	}
	mod := s.browser()
	mod.get("/chan/login")
	if code, _, _ := mod.postForm("/chan/login", url.Values{"name": {"sysop"}, "password": {"password1"}, "back": {"/chan/manage"}}); code != 303 {
		t.Fatalf("login: %d", code)
	}
	code, _, body = mod.get("/chan/manage")
	if code != 200 || !strings.Contains(body, "/b/3") || !strings.Contains(body, reportReasons[2]) {
		t.Fatalf("reports %d:\n%s", code, body)
	}
	mod.get("/chan/manage/post?b=b&no=1")
	mod.postForm("/chan/manage/post", url.Values{"b": {"b"}, "no": {"1"}, "action": {"close"}})
	mod.postForm("/chan/manage/post", url.Values{"b": {"b"}, "no": {"1"}, "action": {"sticky"}})
	if p, _ := s.b.Store.ChanPost(s.bd.ID, 1); !p.Closed || !p.Sticky {
		t.Errorf("not closed and sticky: %+v", p)
	}
	if code, _, _ := anon.post("b", map[string]string{"resto": "1", "com": "closed?"}, nil, ""); code != 403 {
		t.Errorf("reply to a closed thread: %d", code)
	}
	// staff may, as ## Admin
	if code, _, _ := mod.post("b", map[string]string{"resto": "1", "com": "Closed.", "capcode": "on"}, nil, ""); code != 303 {
		t.Errorf("staff reply: %d", code)
	}
	_, _, body = anon.get("/chan/b/thread/1")
	if !strings.Contains(body, `<strong class="capcode">## Admin</strong>`) || !strings.Contains(body, "Sticky") {
		t.Error("no capcode or sticky")
	}
	// a ban, said under the post: the poster is shown why
	mod.postForm("/chan/manage/post", url.Values{"b": {"b"}, "no": {"3"}, "action": {"ban"}, "reason": {"Being quiet"},
		"days": {"1"}, "public": {"on"}})
	if _, _, body := anon.get("/chan/b/thread/1"); !strings.Contains(body, "(USER WAS BANNED FOR THIS POST)") {
		t.Error("the ban is not said")
	}
	code, _, body = anon.post("b", map[string]string{"com": "hi"}, pngOf(5, 5, color.RGBA{0, 0, 255, 255}), "b.png")
	if code != 403 || !strings.Contains(body, "You are banned") || !strings.Contains(body, "Being quiet") {
		t.Errorf("banned: %d\n%s", code, body)
	}
	if code, _, body := mod.get("/chan/manage/bans"); code != 200 || !strings.Contains(body, "Being quiet") {
		t.Errorf("bans %d", code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func otherTok(br *browser) string {
	br.get("/chan/b/")
	return br.tok
}

func getJSON(t *testing.T, u string, v any) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("%s: %d %s", u, resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("%s: %v", u, err)
	}
}

// TestPrune: past its pages, a board's oldest threads go, files and all;
// stickies stay.
func TestPrune(t *testing.T) {
	s := setup(t)
	s.bd.Pages, s.bd.PerPage = 1, 2
	s.b.Store.UpdateBoard(s.bd)
	br := s.browser()
	var tims []int64
	for i := 0; i < 3; i++ {
		code, _, body := br.post("b", map[string]string{"com": "thread"}, pngOf(20+i, 20, color.RGBA{uint8(i * 50), 0, 0, 255}), "x.png")
		if code != 303 {
			t.Fatalf("thread %d: %d\n%s", i, code, body)
		}
		p, _ := s.b.Store.ChanPost(s.bd.ID, int64(i+1))
		tims = append(tims, p.Tim)
	}
	if _, err := s.b.Store.ChanPost(s.bd.ID, 1); err == nil {
		t.Error("the oldest thread was not pruned")
	}
	if _, err := os.Stat(s.srv.srcPath("b", tims[0], ".png")); err == nil {
		t.Error("its image was not removed")
	}
	if n, _ := s.b.Store.ChanThreadCount(s.bd.ID); n != 2 {
		t.Errorf("%d threads", n)
	}
	if code, _, _ := br.get("/chan/b/2"); code != 404 {
		t.Errorf("page 2: %d", code)
	}
}
