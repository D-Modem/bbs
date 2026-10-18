package imageboard

import (
	"bytes"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

var ctokPat = regexp.MustCompile(`name="ctok" value="([A-Za-z0-9_-]+)"`)

// TestCaptcha: one not signed in answers a captcha to post, once each; one
// signed in does not.
func TestCaptcha(t *testing.T) {
	s := setup(t)
	s.srv.S.Captcha = true
	anon := s.browser()
	page := func() string {
		_, _, body := anon.get("/chan/b/")
		m := ctokPat.FindStringSubmatch(body)
		if m == nil || !strings.Contains(body, `src="/chan/captcha.png?t=`+m[1]) {
			t.Fatalf("no captcha:\n%s", body)
		}
		return m[1]
	}
	img := pngOf(30, 30, color.RGBA{255, 0, 0, 255})

	tok := page()
	resp, _ := http.Get(s.ts.URL + "/chan/captcha.png?t=" + tok)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if cfg, err := png.DecodeConfig(bytes.NewReader(b)); err != nil || cfg.Width != 200 || cfg.Height != 70 ||
		resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("captcha image: %v %+v", err, cfg)
	}
	// wrong, then right, then the same again
	if code, _, body := anon.post("b", map[string]string{"com": "x", "ctok": tok, "captcha": "nope"}, img, "a.png"); code != 403 ||
		!strings.Contains(body, "Wrong verification") {
		t.Errorf("wrong: %d", code)
	}
	tok = page()
	answer, _, _ := s.srv.openCaptcha(tok)
	spaced := strings.ToLower(answer[:3]) + " " + answer[3:]
	if code, _, body := anon.post("b", map[string]string{"com": "x", "ctok": tok, "captcha": spaced}, img, "a.png"); code != 303 {
		t.Fatalf("right: %d\n%s", code, body)
	}
	if code, _, body := anon.post("b", map[string]string{"resto": "1", "com": "again", "ctok": tok, "captcha": answer}, nil, ""); code != 403 ||
		!strings.Contains(body, "run out") {
		t.Errorf("used twice: %d", code)
	}
	if code, _, _ := anon.post("b", map[string]string{"resto": "1", "com": "x", "ctok": "forged", "captcha": "AAAAAA"}, nil, ""); code != 403 {
		t.Errorf("forged: %d", code)
	}
	if code, _, _ := anon.post("b", map[string]string{"resto": "1", "com": "x"}, nil, ""); code != 403 {
		t.Errorf("none: %d", code)
	}
	if code, _, _ := anon.get("/chan/captcha.png?t=forged"); code != 404 {
		t.Errorf("a forged token's image: %d", code)
	}
	var fresh map[string]string
	getJSON(t, s.ts.URL+"/chan/captcha.json", &fresh)
	if _, _, err := s.srv.openCaptcha(fresh["token"]); err != nil {
		t.Errorf("captcha.json: %v", err)
	}

	// signed in: no captcha; marked as a member's, or signed
	mia := s.browser()
	mia.get("/chan/login")
	mia.postForm("/chan/login", url.Values{"name": {"mia"}, "password": {"password1"}, "back": {"/chan/b/"}})
	_, _, body := mia.get("/chan/b/thread/1")
	if strings.Contains(body, "ctok") || !strings.Contains(body, "Sign it as <b>mia</b>") {
		t.Errorf("signed in: captcha or no signing")
	}
	if code, _, body := mia.post("b", map[string]string{"resto": "1", "com": "member", "name": "Fake"}, nil, ""); code != 303 {
		t.Fatalf("member: %d\n%s", code, body)
	}
	if code, _, _ := mia.post("b", map[string]string{"resto": "1", "com": "signed", "name": "Fake#tea", "sign": "on"}, nil, ""); code != 303 {
		t.Fatalf("signed: %d", code)
	}
	p2, _ := s.b.Store.ChanPost(s.bd.ID, 2)
	p3, _ := s.b.Store.ChanPost(s.bd.ID, 3)
	if p2.UserID == 0 || p2.Account != "" || p2.Name != "Fake" || p3.Account != "mia" || p3.Name != "" || p3.Trip != "" {
		t.Errorf("posts: %+v %+v", p2, p3)
	}
	_, _, body = anon.get("/chan/b/thread/1")
	for _, want := range []string{`<span class="name">Fake</span> <span class="memberMark" title="Posted by a signed-in user of this BBS">`,
		`<span class="name account">mia</span> <span class="memberMark signed" title="Signed: mia, a user of this BBS">`} {
		if !strings.Contains(body, want) {
			t.Errorf("no %s", want)
		}
	}
	if p1, _ := s.b.Store.ChanPost(s.bd.ID, 1); strings.Contains(body, `id="p1"`) && p1.UserID != 0 {
		t.Error("an anonymous post marked")
	}
	var thread struct {
		Posts []apiPost `json:"posts"`
	}
	getJSON(t, s.ts.URL+"/chan/b/thread/1.json", &thread)
	if thread.Posts[0].Member != 0 || thread.Posts[1].Member != 1 || thread.Posts[2].Account != "mia" {
		t.Errorf("json: %+v", thread.Posts)
	}
	// a forced_anon board: marked, never signed
	s.bd.ForcedAnon = true
	s.b.Store.UpdateBoard(s.bd)
	if _, _, body := mia.get("/chan/b/thread/1"); strings.Contains(body, "Sign it as") {
		t.Error("signing offered on a forced_anon board")
	}
	mia.post("b", map[string]string{"resto": "1", "com": "anon", "sign": "on"}, nil, "")
	if p4, _ := s.b.Store.ChanPost(s.bd.ID, 4); p4 == nil || p4.Account != "" || p4.UserID == 0 {
		t.Errorf("forced_anon: %+v", p4)
	}
}

// TestCaptchaTokens: a token opens to an answer of the default length, of
// none of the letters left out, ending in about ten minutes; another key
// or a changed byte does not open it.
func TestCaptchaTokens(t *testing.T) {
	s := &Server{S: Defaults()}
	s.key = []byte("0123456789abcdef0123456789abcdef")
	tok := s.NewCaptcha()
	a, ends, err := s.openCaptcha(tok)
	if err != nil || len(a) != 6 || strings.ContainsAny(a, "IOQ01W") || time.Until(ends) < 9*time.Minute {
		t.Fatalf("%q %v %v", a, ends, err)
	}
	// another key: not ours
	other := &Server{key: []byte("another key, another key, abcdef")}
	if _, _, err := other.openCaptcha(tok); err == nil {
		t.Error("opened with another key")
	}
	// tampered
	b := []byte(tok)
	b[len(b)-3] ^= 1
	if _, _, err := s.openCaptcha(string(b)); err == nil {
		t.Error("a tampered token opened")
	}
}

// TestBoardSettings: captchas as a board says over [chan], their length
// and difficulty; the Options line hidden (no sage then); a board for the
// BBS's users alone.
func TestBoardSettings(t *testing.T) {
	s := setup(t)
	s.srv.S.Captcha, s.srv.S.CaptchaLength, s.srv.S.CaptchaLevel = true, 4, "hard"
	anon := s.browser()
	img := pngOf(30, 30, color.RGBA{0, 255, 0, 255})

	_, _, body := anon.get("/chan/b/")
	tok := ctokPat.FindStringSubmatch(body)[1]
	if a, _, _ := s.srv.openCaptcha(tok); len(a) != 4 {
		t.Errorf("length: %q", a)
	}
	resp, _ := http.Get(s.ts.URL + "/chan/captcha.png?t=" + tok)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if cfg, _ := png.DecodeConfig(bytes.NewReader(b)); cfg.Width != captchaW(4) {
		t.Errorf("width %d", cfg.Width)
	}
	// the board says no: none
	s.bd.Captcha = "no"
	s.b.Store.UpdateBoard(s.bd)
	if _, _, body := anon.get("/chan/b/"); strings.Contains(body, "ctok") {
		t.Error("a captcha where the board says no")
	}
	if code, _, _ := anon.post("b", map[string]string{"com": "first"}, img, "a.png"); code != 303 {
		t.Fatalf("post: %d", code)
	}
	// [chan] says no, the board yes
	s.srv.S.Captcha, s.bd.Captcha = false, "yes"
	s.b.Store.UpdateBoard(s.bd)
	if code, _, _ := anon.post("b", map[string]string{"resto": "1", "com": "x"}, nil, ""); code != 403 {
		t.Errorf("no captcha where the board says yes: %d", code)
	}
	s.bd.Captcha = ""
	s.b.Store.UpdateBoard(s.bd)

	// no Options line: sage is not had
	s.bd.Options = "no"
	s.b.Store.UpdateBoard(s.bd)
	if _, _, body := anon.get("/chan/b/thread/1"); strings.Contains(body, `name="email"`) || strings.Contains(body, `"sage"`) {
		t.Error("the Options line shown")
	}
	before, _ := s.b.Store.ChanThread(s.bd.ID, 1)
	time.Sleep(1100 * time.Millisecond)
	anon.post("b", map[string]string{"resto": "1", "com": "saged?", "email": "sage"}, nil, "")
	after, _ := s.b.Store.ChanThread(s.bd.ID, 1)
	if after.Bumped.Equal(before.Bumped) {
		t.Error("sage taken without the Options line")
	}
	if p, _ := s.b.Store.ChanPost(s.bd.ID, 2); p.Email != "" {
		t.Errorf("email kept: %q", p.Email)
	}

	// for the BBS's users alone
	s.bd.LoginToRead = true
	s.b.Store.UpdateBoard(s.bd)
	p1, _ := s.b.Store.ChanPost(s.bd.ID, 1)
	for path, want := range map[string]int{"/chan/b/": 303, "/chan/b/thread/1": 303, "/chan/b/catalog": 303,
		"/chan/b/thread/1.json": 403, "/chan/b/catalog.json": 403, "/chan/b/src/" + itoa(p1.Tim) + ".png": 404,
		"/chan/b/thumb/" + itoa(p1.Tim) + "s.jpg": 404} {
		if code, loc, _ := anon.get(path); code != want || code == 303 && !strings.HasPrefix(loc, "/chan/login?back=") {
			t.Errorf("%s: %d %s", path, code, loc)
		}
	}
	if code, _, _ := anon.post("b", map[string]string{"resto": "1", "com": "x"}, nil, ""); code != 403 {
		t.Errorf("a guest posting: %d", code)
	}
	if _, _, body := anon.get("/chan/"); !strings.Contains(body, "(members)") || strings.Contains(body, "/chan/b/thumb/") {
		t.Error("the front page: no (members), or its images")
	}
	mia := s.browser()
	mia.get("/chan/login")
	mia.postForm("/chan/login", url.Values{"name": {"mia"}, "password": {"password1"}, "back": {"/chan/b/"}})
	if code, _, body := mia.get("/chan/b/thread/1"); code != 200 || !strings.Contains(body, "first") {
		t.Errorf("a user: %d", code)
	}
	if code, _, _ := mia.post("b", map[string]string{"resto": "1", "com": "a user"}, nil, ""); code != 303 {
		t.Errorf("a user posting: %d", code)
	}
	var boards struct {
		Boards []map[string]any `json:"boards"`
	}
	getJSON(t, s.ts.URL+"/chan/boards.json", &boards)
	if boards.Boards[0]["login"] != "all" {
		t.Errorf("boards.json: %v", boards.Boards[0])
	}
}
