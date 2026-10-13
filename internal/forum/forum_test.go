package forum

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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
	u, err := s.b.Authenticate("forum", "test", name, pw)
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
	t  *testing.T
	b  *bbs.BBS
	ts *httptest.Server
	ar map[string]*store.Area
	fs *Server
}

func setup(t *testing.T) *site {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := bbs.Defaults()
	s.Name, s.FailDelay = "Test Board", 0
	b := bbs.New(s, st, log.New(&bytes.Buffer{}, "", 0))
	for _, u := range []struct {
		name  string
		sysop bool
	}{{"alice", false}, {"bob", false}, {"sysop", true}} {
		if _, err := st.CreateUser(u.name, "password1", "Here", u.sysop); err != nil {
			t.Fatal(err)
		}
	}
	ar := map[string]*store.Area{}
	for _, n := range []string{"General", "Secret", "News"} {
		a, _ := st.CreateArea(n, n+" talk", n == "News")
		ar[n] = a
	}
	st.SetAreaRequire(ar["Secret"].ID, "sysop", "")
	ar["Secret"], _ = st.Area(ar["Secret"].ID)
	st.SetAreaAliases(ar["General"].ID, true)
	fs := &Server{B: b, Sessions: &sessions{b: b, m: map[string]int64{}}, Guests: true, TTL: time.Hour}
	h, err := fs.Handler()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return &site{t: t, b: b, ts: ts, ar: ar, fs: fs}
}

// browser is a client with its cookies.
type browser struct {
	s *site
	c *http.Client
}

func (s *site) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{s: s, c: &http.Client{Jar: jar}}
}

// get fetches a page: its status, final URL and body.
func (br *browser) get(path string) (int, string, string) {
	br.s.t.Helper()
	resp, err := br.c.Get(br.s.ts.URL + path)
	if err != nil {
		br.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Request.URL.RequestURI(), string(b)
}

var csrfField = regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`)

// post sends a form, with the CSRF token of the page it came from.
func (br *browser) post(from, path string, form url.Values) (int, string, string) {
	br.s.t.Helper()
	if from != "" {
		_, _, page := br.get(from)
		if m := csrfField.FindStringSubmatch(page); m != nil && form.Get("csrf") == "" {
			form.Set("csrf", m[1])
		}
	}
	resp, err := br.c.PostForm(br.s.ts.URL+path, form)
	if err != nil {
		br.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Request.URL.RequestURI(), string(b)
}

func (br *browser) login(name string) {
	br.s.t.Helper()
	code, at, body := br.post("", "/forum/login", url.Values{"name": {name}, "password": {"password1"}})
	if code != 200 || at != "/forum/" || !strings.Contains(body, "Signed in as") {
		br.s.t.Fatalf("login %s: %d %s", name, code, at)
	}
}

func has(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s: no %q", what, w)
		}
	}
}

func hasNot(t *testing.T, what, body string, nots ...string) {
	t.Helper()
	for _, w := range nots {
		if strings.Contains(body, w) {
			t.Errorf("%s: %q there", what, w)
		}
	}
}

func TestGuestsAndLogin(t *testing.T) {
	s := setup(t)
	guest := s.browser()
	code, _, body := guest.get("/forum/")
	if code != 200 {
		t.Fatalf("index %d", code)
	}
	has(t, "index", body, "Test Board", "General", "News", "Sign In", "Board Statistics", "3</strong> members")
	hasNot(t, "index", body, "Secret") // only the sysop's
	resp, _ := http.Get(s.ts.URL + "/forum/")
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP %q", csp)
	}
	resp.Body.Close()
	if code, at, _ := guest.get("/forum/area/" + itoa(s.ar["Secret"].ID)); code != 200 || !strings.HasPrefix(at, "/forum/login") {
		t.Errorf("secret area: %d %s", code, at)
	}
	if code, _, _ := guest.get("/forum/area/999"); code != 404 {
		t.Errorf("no area: %d", code)
	}
	// a guest's post goes to the login
	if _, at, _ := guest.post("", "/forum/area/"+itoa(s.ar["General"].ID)+"/new", url.Values{"subject": {"x"}}); !strings.HasPrefix(at, "/forum/login") {
		t.Errorf("guest post: %s", at)
	}
	_, _, body = guest.post("", "/forum/login", url.Values{"name": {"alice"}, "password": {"wrong"}})
	has(t, "bad login", body, "class=\"message error\"", `value="alice"`)
	// back only to the forum's own pages
	_, at, _ := guest.post("", "/forum/login", url.Values{"name": {"alice"}, "password": {"password1"}, "back": {"//evil.example/"}})
	if at != "/forum/" {
		t.Errorf("back to %s", at)
	}
	_, _, body = guest.get("/forum/")
	has(t, "signed in", body, "Signed in as <a href=\"/forum/user/alice\"")
	guest.post("/forum/", "/forum/logout", url.Values{})
	_, _, body = guest.get("/forum/")
	has(t, "signed out", body, "Sign In")
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func mustUser(s *site, name string) *store.User {
	u, err := s.b.Store.UserByName(name)
	if err != nil {
		s.t.Fatal(err)
	}
	return u
}

func TestPostingAndReading(t *testing.T) {
	s := setup(t)
	alice, bob, sysop := s.browser(), s.browser(), s.browser()
	alice.login("alice")
	bob.login("bob")
	sysop.login("sysop")
	gen := "/forum/area/" + itoa(s.ar["General"].ID)
	long := "A Courier HST, which I bought used from a sysop who was moving to ISDN that spring. See https://example.org/hst for the specs."

	// a form without its token is refused
	if code, _, _ := alice.post("", gen+"/new", url.Values{"subject": {"x"}, "body": {"y"}}); code != 403 {
		t.Errorf("no csrf: %d", code)
	}
	code, at, body := alice.post(gen+"/new", gen+"/new", url.Values{"subject": {"Modems <b>rule</b>"},
		"body": {"My first was 2400 baud.\nSee https://example.org/a?b=1 <script>alert(1)</script>\n" + long},
		"poll": {"Hayes\nUSR\n"}})
	if code != 200 || !strings.HasPrefix(at, "/forum/topic/") {
		t.Fatalf("new topic: %d %s %s", code, at, body)
	}
	topic := at
	// a paragraph longer than a terminal's line is wrapped, not cut
	has(t, "wrapped", body, `<a href="https://example.org/hst" rel="nofollow ugc noopener" class="bbc_url">https://example.org/hst</a> for the specs.`)
	has(t, "topic", body, "Modems &lt;b&gt;rule&lt;/b&gt;", "&lt;script&gt;", `<a href="https://example.org/a?b=1" rel="nofollow ugc noopener"`,
		"Poll", "Hayes", "Reply to this topic", "Delete")
	hasNot(t, "topic", body, "<script>alert")
	if n, _ := s.b.Store.TopicCount(s.ar["General"].ID); n != 1 {
		t.Errorf("topics %d", n)
	}

	// bob: unread, quotes alice, likes, votes
	_, _, body = bob.get(gen)
	has(t, "bob's list", body, `<tr class="unread">`, "Started by alice")
	_, _, body = bob.get(topic + "?quote=" + strings.TrimPrefix(topic, "/forum/topic/"))
	has(t, "quote", body, "alice wrote:\n&gt; My first was 2400 baud.")
	hasNot(t, "bob's view", body, "Delete")
	_, at, body = bob.post(topic, topic+"/reply", url.Values{"body": {"alice wrote:\n> My first was 2400 baud.\n\nMine was 300."}})
	if !strings.HasPrefix(at, topic) {
		t.Fatalf("reply went to %s", at)
	}
	has(t, "reply", body, `<p class="citation">alice said:</p><blockquote class="quote">My first was 2400 baud.<br></blockquote>Mine was 300.`)
	_, _, body = bob.get(gen)
	hasNot(t, "bob's list after reading", body, `<tr class="unread">`)
	root := strings.TrimPrefix(topic, "/forum/topic/")
	bob.post(topic, "/forum/post/"+root+"/like", url.Values{"like": {"1"}})
	bob.post(topic, "/forum/post/"+root+"/answer", url.Values{"answer": {"1"}})
	_, _, body = alice.get(gen)
	has(t, "alice's list", body, `<tr class="unread">`, "1</strong> reply")
	_, _, body = alice.get(topic)
	has(t, "alice's view", body, "1 person likes this")
	_, _, body = bob.get(topic)
	has(t, "bob's vote", body, `<li class="mine"><span class="answer">USR</span>`)
	if code, _, _ := alice.post(topic, "/forum/post/"+root+"/like", url.Values{"like": {"1"}}); code != 400 {
		t.Errorf("liking one's own: %d", code)
	}
	if code, _, _ := bob.post(topic, "/forum/post/"+root+"/answer", url.Values{"answer": {"9"}}); code != 400 {
		t.Errorf("no such answer: %d", code)
	}
	_, _, body = alice.get(gen)
	hasNot(t, "alice's list after reading", body, `<tr class="unread">`)

	// under an alias: the one behind it not shown
	s.b.Store.SetAlias(mustUser(s, "bob").ID, "Mystery")
	_, _, body = bob.post(topic, topic+"/reply", url.Values{"body": {"guess who"}, "alias": {"on"}})
	has(t, "alias", body, `<span class="author-name">Mystery</span>`)
	_, _, body = alice.get("/forum/user/bob")
	hasNot(t, "bob's profile", body, "guess who")
	has(t, "bob's profile", body, "Re: Modems")

	// deleting: the author or the sysop, no one else
	ms, _ := s.b.Store.TopicMessages(atoi(root), 0, 10)
	reply := itoa(ms[1].ID)
	if code, _, _ := alice.post(topic, "/forum/post/"+reply+"/delete", url.Values{}); code != 403 {
		t.Errorf("alice deletes bob's: %d", code)
	}
	if _, at, _ := sysop.post(topic, "/forum/post/"+reply+"/delete", url.Values{}); at != topic {
		t.Errorf("sysop's delete went to %s", at)
	}
	if n, _ := s.b.Store.TopicLen(atoi(root)); n != 2 {
		t.Errorf("left %d", n)
	}

	// posting where one may not
	news := "/forum/area/" + itoa(s.ar["News"].ID)
	if code, _, _ := alice.get(news + "/new"); code != 403 {
		t.Errorf("news: %d", code)
	}
	_, _, body = alice.get(news)
	hasNot(t, "news", body, "Start New Topic")
	_, _, body = alice.post(gen+"/new", gen+"/new", url.Values{"subject": {""}, "body": {"x"}})
	has(t, "no subject", body, "A message needs a subject.")

	// search, online, the post finder
	_, _, body = bob.get("/forum/search?q=baud")
	has(t, "search", body, "Modems &lt;b&gt;rule&lt;/b&gt;", "My first was 2400 baud.")
	_, _, body = bob.get("/forum/search?q=ab")
	has(t, "short search", body, "3 letters or more")
	if _, at, _ := bob.get("/forum/post/" + itoa(ms[2].ID)); !strings.Contains(at, "/forum/topic/"+root) {
		t.Errorf("finder to %s", at)
	}
	_, _, body = bob.get("/forum/online")
	has(t, "online", body, "No one is on just now.")
}

func TestRegister(t *testing.T) {
	s := setup(t)
	s.b.NewUsers = true
	br := s.browser()
	_, _, body := br.post("", "/forum/register", url.Values{"name": {"newbie"}, "password": {"password9"}, "password2": {"password8"}})
	has(t, "differ", body, "The passwords differ.")
	_, at, body := br.post("", "/forum/register", url.Values{"name": {"newbie"}, "password": {"password9"}, "password2": {"password9"}, "location": {"Mars"}})
	if !strings.HasPrefix(at, "/forum/") {
		t.Fatalf("register to %s", at)
	}
	has(t, "registered", body, "Your account is made", "Signed in as")
	if u, err := s.b.Store.UserByName("newbie"); err != nil || u.Location != "Mars" {
		t.Errorf("user %+v %v", u, err)
	}
	s.b.NewUsers = false
	if code, _, _ := s.browser().get("/forum/register"); code != 403 {
		t.Errorf("closed: %d", code)
	}
}

func TestRenderBody(t *testing.T) {
	got := string(renderBody("Bob wrote:\n> one <i>\n BW> two\n\nthree & www.example.org."))
	want := `<p class="citation">Bob said:</p><blockquote class="quote">one &lt;i&gt;<br>two<br></blockquote>three &amp; <a href="http://www.example.org" rel="nofollow ugc noopener" class="bbc_url">www.example.org</a>.`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if s := snippet("Bob wrote:\n> quoted\nmine here", 40); s != "mine here" {
		t.Errorf("snippet %q", s)
	}
	p := newPages("/forum/area/1", 205, 6)
	if p.Total != 11 || p.URL(1) != "/forum/area/1" || p.URL(3) != "/forum/area/1?page=3" {
		t.Errorf("pages %+v", p)
	}
	if got := p.Shown(); len(got) != 9 || got[1] != 0 || got[2] != 4 || got[8] != 11 {
		t.Errorf("shown %v", got)
	}
	if groupDigits(1234567) != "1,234,567" {
		t.Error(groupDigits(1234567))
	}
}

func TestThemes(t *testing.T) {
	s := setup(t)
	alice, guest := s.browser(), s.browser()
	alice.login("alice")
	gen := "/forum/area/" + itoa(s.ar["General"].ID)
	_, topic, _ := alice.post(gen+"/new", gen+"/new", url.Values{"subject": {"Old skins"},
		"body": {"Remember these?"}, "poll": {"Yes\nNo"}})
	alice.post(topic, topic+"/reply", url.Values{"body": {"alice wrote:\n> Remember these?\n\nI do."}})
	pages := []string{"/forum/", gen, gen + "/new", topic, "/forum/user/alice", "/forum/online",
		"/forum/search?q=skins", "/forum/area/999"}

	// the default theme, and the chooser offering both
	_, _, body := guest.get("/forum/")
	has(t, "default", body, "/forum/static/ipb3.css", `<option value="ipb2">`, `<option value="ipb3" selected>`)

	for _, th := range Themes {
		css := "/forum/static/" + th.Name + ".css"
		if code, _, _ := guest.get(css); code != 200 {
			t.Errorf("%s: %d", css, code)
		}
		// chosen by a guest (no token) and by a member (with one): back
		// to the page it was chosen on
		code, at, _ := guest.post("", "/forum/theme", url.Values{"theme": {th.Name}, "back": {gen}})
		if code != 200 || at != gen {
			t.Errorf("guest choosing %s: %d %s", th.Name, code, at)
		}
		if code, _, _ := alice.post("", "/forum/theme", url.Values{"theme": {th.Name}, "back": {gen}}); code != 403 {
			t.Errorf("member choosing %s without a token: %d", th.Name, code)
		}
		alice.post(gen, "/forum/theme", url.Values{"theme": {th.Name}, "back": {gen}})
		for _, who := range []*browser{guest, alice} {
			for _, p := range pages {
				code, _, body := who.get(p)
				if code >= 500 || !strings.Contains(body, css) {
					t.Errorf("%s %s: %d, no %s", th.Name, p, code, css)
				}
				has(t, th.Name+" "+p, body, `<option value="`+th.Name+`" selected>`)
			}
		}
		_, _, body := alice.get(topic)
		has(t, th.Name+" topic", body, "Remember these?", "I do.", `class="quote"`, `name="answer"`)
		// each laid out its own way
		own := map[string][2][]string{
			"ipb3":   {{"Signed in as", "Reply to this topic"}, {"Board Statistics", "Create Account"}},
			"ipb2":   {{"Logged in as:", "Fast Reply", "Member No.:", `class="postsep"`}, {"Welcome Guest", "Last Post Info", `id="gfooter"`}},
			"ipb1":   {{"Logged in as:", "Posted on", `class="postlinksbar"`, `class="activeuserstrip"`}, {"Welcome Guest", "Last Post Info", `class="pformstrip"`}},
			"phpbb1": {{"Author</td>", "Posted:", "Reply to:", "Logout</button></form>]"}, {"Our users have posted a total of", "You are not logged in.", "= New Posts since your last visit."}},
			"ikon":   {{"Log out</button>", "Total Posts:", "Post a reply", `class="bottomline"`}, {"Guest: ", "Forum Info", "welcomes its newest member", "You must be logged in to view the post markers"}},
			"ipb5":   {{"Reply to this topic", `class="ipsEntry ipsBox`, "Go to topic listing", `class="ipsReact"`}, {"Who's Online", "Forum Statistics", "Sign Up", `class="ipsForumIcon`}},
			"vb4":    {{"Reply With Quote", "Post Quick Reply", `class="postbitlegacy`, "Join Date"}, {"What's Going On?", "Threads / Posts", `id="navbar_loginform"`, "Icon Legend"}},
			"vb3":    {{"Quick Reply", "Join Date:", `class="bigusername"`, "Post Quick Reply"}, {"What's Going On?", "Currently Active Users", "Statistics", `id="navbar_username"`}},
			"vb2":    {{"Post Reply", `class="tborder"`, "Registered:"}, {"Currently Active Users", "Login!", "Last Post"}},
			"vblite": {{"Posted by:", "post reply", `class="vbtn"`}, {"The time now is", `class="bulb`, "Last Post"}},
		}[th.Name]
		has(t, th.Name+" own topic", body, own[0]...)
		_, _, body = guest.get("/forum/")
		has(t, th.Name+" own index", body, own[1]...)
	}

	// no such theme; an outside page to go back to
	if code, _, _ := guest.post("", "/forum/theme", url.Values{"theme": {"nope"}}); code != 400 {
		t.Errorf("unknown theme: %d", code)
	}
	if _, at, _ := guest.post("", "/forum/theme", url.Values{"theme": {"ipb3"}, "back": {"//evil.example/"}}); at != "/forum/" {
		t.Errorf("went back to %s", at)
	}

	// the forum's own choice, until a caller makes theirs
	s.fs.Theme = "ipb2"
	_, _, body = s.browser().get("/forum/")
	has(t, "forum's theme", body, "/forum/static/ipb2.css")
	_, _, body = guest.get("/forum/")
	has(t, "guest's own", body, "/forum/static/ipb3.css")
}

func TestThemeLineage(t *testing.T) {
	for _, th := range Themes {
		if th.Parent != "" && !KnownTheme(th.Parent) {
			t.Errorf("%s: no parent %s", th.Name, th.Parent)
		}
		if th.Name == "phpbb33" {
			if got := strings.Join(lineage(th), " "); got != "phpbb31 phpbb32 phpbb33" {
				t.Errorf("phpbb33's lineage: %s", got)
			}
		}
	}
}

func TestAgo(t *testing.T) {
	now := time.Now()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 12, 0, 0, 0, now.Location())
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-10 * time.Second), "Just now"},
		{now.Add(-5 * time.Minute), "5 minutes ago"},
		{now.Add(-1 * time.Minute), "1 minute ago"},
		{today.AddDate(0, 0, -1), "Yesterday at 12:00 PM"},
		{today.AddDate(-2, 0, 0), today.AddDate(-2, 0, 0).Format("January 2, 2006")},
		{time.Time{}, "never"},
	} {
		if got := ago(c.t); got != c.want {
			t.Errorf("ago(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}
