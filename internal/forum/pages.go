package forum

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func (q *req) pageNum() int {
	n, _ := strconv.Atoi(q.r.URL.Query().Get("page"))
	if n < 1 {
		n = 1
	}
	return n
}

func (q *req) id() (int64, bool) {
	id, err := strconv.ParseInt(q.r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// theArea is the area of the path's id, if the user may read it.
func (q *req) theArea() *store.Area {
	id, ok := q.id()
	if !ok {
		q.fail(http.StatusNotFound, errNoArea.Error())
		return nil
	}
	a, err := q.s.B.Store.Area(id)
	if err != nil || !q.canRead(a) {
		if q.user == nil && err == nil {
			q.needLogin()
			return nil
		}
		q.fail(http.StatusNotFound, "There is no such forum, or it is not open to you.")
		return nil
	}
	return a
}

// theMessage is the message of the path's id, and its area, if the user
// may read it.
func (q *req) theMessage() (*store.Message, *store.Area) {
	id, ok := q.id()
	var m *store.Message
	var a *store.Area
	var err error
	if ok {
		if m, err = q.s.B.Store.Message(id); err == nil {
			a, err = q.s.B.Store.Area(m.AreaID)
		}
	}
	if !ok || err != nil || !q.canRead(a) {
		if q.user == nil && err == nil && ok {
			q.needLogin()
			return nil, nil
		}
		q.fail(http.StatusNotFound, "There is no such post, or it is not open to you.")
		return nil, nil
	}
	return m, a
}

// --- the board's index

type forumRow struct {
	Area      *store.Area
	Topics    int
	Replies   int
	Unread    bool
	Last      *store.Message
	LastTopic *store.Message
}

type category struct {
	Name   string
	Forums []forumRow
}

type boardStats struct {
	Posts, Topics, Members int
	Newest                 *store.User
}

func (s *Server) index(q *req) {
	areas, err := q.areas()
	if err != nil {
		q.fail(http.StatusInternalServerError, err.Error())
		return
	}
	sort.SliceStable(areas, func(i, j int) bool { return areas[i].Group < areas[j].Group })
	var cats []*category
	var st boardStats
	for _, a := range areas {
		name := a.Group
		if name == "" {
			name = "Message Areas"
		}
		if len(cats) == 0 || cats[len(cats)-1].Name != name {
			cats = append(cats, &category{Name: name})
		}
		row := forumRow{Area: a}
		row.Topics, _ = s.B.Store.TopicCount(a.ID)
		row.Replies = a.Messages - row.Topics
		if last, err := s.B.Store.LastMessage(a.ID); err == nil {
			row.Last = last
			row.LastTopic = last
			if root := s.B.Store.TopicOf(last); root != last.ID {
				if r, err := s.B.Store.Message(root); err == nil {
					row.LastTopic = r
				}
			}
		}
		if q.user != nil {
			n, _ := s.B.Store.NewCount(q.user.ID, a.ID)
			row.Unread = n > 0
		}
		st.Posts += a.Messages
		st.Topics += row.Topics
		cats[len(cats)-1].Forums = append(cats[len(cats)-1].Forums, row)
	}
	if us, err := s.B.Store.Users(); err == nil {
		st.Members = len(us)
		for _, u := range us {
			if st.Newest == nil || u.Created.After(st.Newest.Created) {
				st.Newest = u
			}
		}
	}
	q.render(http.StatusOK, "index.html", s.B.Name, nil, map[string]any{
		"Categories": cats, "Stats": st, "Online": onlineUsers(s.B)})
}

// onlineUsers are the callers on now: each user once.
func onlineUsers(b *bbs.BBS) []bbs.NodeInfo {
	seen := map[string]bool{}
	var out []bbs.NodeInfo
	for _, n := range b.Nodes() {
		if n.User == "" || seen[strings.ToLower(n.User)] {
			continue
		}
		seen[strings.ToLower(n.User)] = true
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].User) < strings.ToLower(out[j].User) })
	return out
}

// --- a forum: its topics

type topicRow struct {
	*store.Topic
	Unread bool
	Pages  int
}

func (s *Server) area(q *req) {
	a := q.theArea()
	if a == nil {
		return
	}
	count, err := s.B.Store.TopicCount(a.ID)
	if err != nil {
		q.fail(http.StatusInternalServerError, err.Error())
		return
	}
	pg := newPages(Base+"/area/"+strconv.FormatInt(a.ID, 10), count, q.pageNum())
	ts, err := s.B.Store.Topics(a.ID, (pg.Cur-1)*perPage, perPage)
	if err != nil {
		q.fail(http.StatusInternalServerError, err.Error())
		return
	}
	rows := make([]topicRow, len(ts))
	read := map[int64]int64{}
	var lastRead int64
	if q.user != nil {
		roots := make([]int64, len(ts))
		for i, t := range ts {
			roots[i] = t.Root.ID
		}
		read, _ = s.B.Store.TopicsRead(q.user.ID, roots)
		lastRead, _ = s.B.Store.LastRead(q.user.ID, a.ID)
	}
	for i, t := range ts {
		rows[i] = topicRow{Topic: t, Pages: (t.Replies + perPage) / perPage}
		if q.user != nil {
			rows[i].Unread = t.Last.ID > max(lastRead, read[t.Root.ID])
		}
	}
	q.render(http.StatusOK, "area.html", a.Name, []crumb{{a.Name, ""}}, map[string]any{
		"Area": a, "Topics": rows, "Pages": pg, "Count": count, "CanPost": q.canPost(a)})
}

// markRead marks a forum read.
func (s *Server) markRead(q *req) {
	if q.needLogin() {
		return
	}
	a := q.theArea()
	if a == nil {
		return
	}
	if last, err := s.B.Store.LastMessage(a.ID); err == nil {
		s.B.Store.SetLastRead(q.user.ID, a.ID, last.ID)
	}
	q.redirect(Base + "/area/" + strconv.FormatInt(a.ID, 10))
}

// --- a topic: its posts

type postView struct {
	M         *store.Message
	Author    *store.User // nil: someone elsewhere, or posting under an alias
	Where     string      // where someone elsewhere is ("": here)
	Num       int
	Likes     int
	Liked     bool
	CanLike   bool
	CanDelete bool
}

type pollAnswer struct {
	Index int
	Text  string
	Count int
	Pct   int
	Mine  bool
}

type pollView struct {
	ID      int64
	Answers []pollAnswer
	Total   int
	Voted   bool
}

func (s *Server) topic(q *req) {
	m, a := q.theMessage()
	if m == nil {
		return
	}
	root := s.B.Store.TopicOf(m)
	if root != m.ID {
		q.redirect(Base + "/post/" + strconv.FormatInt(m.ID, 10))
		return
	}
	count, _ := s.B.Store.TopicLen(root)
	pg := newPages(Base+"/topic/"+strconv.FormatInt(root, 10), count, q.pageNum())
	ms, err := s.B.Store.TopicMessages(root, (pg.Cur-1)*perPage, perPage)
	if err != nil {
		q.fail(http.StatusInternalServerError, err.Error())
		return
	}
	users := map[int64]*store.User{}
	var uid int64
	if q.user != nil {
		uid = q.user.ID
	}
	posts := make([]postView, len(ms))
	for i, pm := range ms {
		pv := postView{M: pm, Num: (pg.Cur-1)*perPage + i + 1}
		if pm.FromID > 0 {
			u, ok := users[pm.FromID]
			if !ok {
				u, _ = s.B.Store.UserByID(pm.FromID)
				users[pm.FromID] = u
			}
			// under an alias, the one behind it is not shown
			if u != nil && strings.EqualFold(u.Name, pm.From) {
				pv.Author = u
			}
		}
		if pm.FromID == 0 {
			pv.Where = origin(pm.Origin)
		}
		up, _, mine, _ := s.B.Store.Votes(pm.ID, uid)
		pv.Likes, pv.Liked = up, mine > 0
		pv.CanLike = q.user != nil && pm.FromID != uid
		pv.CanDelete = q.user != nil && (q.user.Sysop || pm.FromID == uid)
		posts[i] = pv
	}
	var poll *pollView
	if answers, counts, mine, err := s.B.Store.Poll(root, uid); err == nil && answers != nil {
		poll = &pollView{ID: root, Voted: mine >= 0}
		for _, c := range counts {
			poll.Total += c
		}
		for i, t := range answers {
			pa := pollAnswer{Index: i, Text: t, Count: counts[i], Mine: i == mine}
			if poll.Total > 0 {
				pa.Pct = counts[i] * 100 / poll.Total
			}
			poll.Answers = append(poll.Answers, pa)
		}
	}
	if q.user != nil && len(ms) > 0 {
		s.B.Store.SetTopicRead(q.user.ID, root, ms[len(ms)-1].ID)
	}
	quote := ""
	var quoteID int64
	if qid, err := strconv.ParseInt(q.r.URL.Query().Get("quote"), 10, 64); err == nil {
		if qm, err := s.B.Store.Message(qid); err == nil && s.B.Store.TopicOf(qm) == root {
			quote = quoteOf(qm)
			quoteID = qm.ID
		}
	}
	q.render(http.StatusOK, "topic.html", m.Subject, []crumb{{a.Name, Base + "/area/" + strconv.FormatInt(a.ID, 10)},
		{m.Subject, ""}}, map[string]any{
		"Area": a, "Root": m, "Posts": posts, "Pages": pg, "Count": count, "CanPost": q.canPost(a), "Poll": poll,
		"Quote": quote, "QuoteID": quoteID, "Aliases": a.Aliases && q.user != nil && q.user.Alias != ""})
}

// origin says where a message came from that was written elsewhere.
func origin(o string) string {
	switch {
	case o == "":
		return "a visitor"
	case strings.HasPrefix(o, "qwk:"):
		return "QWK network " + strings.TrimPrefix(o, "qwk:")
	case strings.Contains(o, ":") || strings.Contains(o, "/"):
		return "FidoNet"
	}
	return "Usenet (" + o + ")"
}

// quoteOf is a message quoted, as an answer begins.
func quoteOf(m *store.Message) string {
	var b strings.Builder
	b.WriteString(m.From + " wrote:\n")
	for _, l := range strings.Split(m.Body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "---") {
			break // the signature
		}
		b.WriteString("> " + l + "\n")
	}
	return b.String() + "\n"
}

// findPost goes to a post in its topic.
func (s *Server) findPost(q *req) {
	m, _ := q.theMessage()
	if m == nil {
		return
	}
	root := s.B.Store.TopicOf(m)
	pos, _ := s.B.Store.TopicPosition(root, m.ID)
	url := Base + "/topic/" + strconv.FormatInt(root, 10)
	if p := pos/perPage + 1; p > 1 {
		url += "?page=" + strconv.Itoa(p)
	}
	q.redirect(url + "#entry" + strconv.FormatInt(m.ID, 10))
}

// signed is a body with the user's signature under it, as on the BBS.
func signed(u *store.User, body string) []string {
	lines := bbs.WrapBody(body)
	if u.Signature != "" {
		lines = append(append(lines, ""), strings.Split(u.Signature, "\n")...)
	}
	return lines
}

func (s *Server) reply(q *req) {
	if q.needLogin() {
		return
	}
	m, a := q.theMessage()
	if m == nil {
		return
	}
	root := s.B.Store.TopicOf(m)
	if root != m.ID {
		q.fail(http.StatusBadRequest, "Replies go to a topic.")
		return
	}
	body := strings.TrimSpace(q.r.PostForm.Get("body"))
	parent := m
	if pid, err := strconv.ParseInt(q.r.PostForm.Get("reply_to"), 10, 64); err == nil {
		if pm, err := s.B.Store.Message(pid); err == nil && s.B.Store.TopicOf(pm) == root {
			parent = pm
		}
	}
	subject := m.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	id, err := q.node().PostWith(a, parent.From, subject, signed(q.user, body), parent.ID,
		bbs.PostOpts{Alias: q.r.PostForm.Get("alias") == "on"})
	if err != nil {
		q.fail(http.StatusBadRequest, capital(err.Error())+".")
		return
	}
	q.redirect(Base + "/post/" + strconv.FormatInt(id, 10))
}

func (s *Server) newTopic(q *req) {
	if q.needLogin() {
		return
	}
	a := q.theArea()
	if a == nil {
		return
	}
	if !q.canPost(a) {
		q.fail(http.StatusForbidden, "You may not start topics in this forum.")
		return
	}
	form := map[string]any{"Area": a, "Aliases": a.Aliases && q.user.Alias != ""}
	if q.r.Method == http.MethodPost {
		subject, body := strings.TrimSpace(q.r.PostForm.Get("subject")), strings.TrimSpace(q.r.PostForm.Get("body"))
		opts := bbs.PostOpts{Alias: q.r.PostForm.Get("alias") == "on"}
		var poll []string
		for _, l := range strings.Split(q.r.PostForm.Get("poll"), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				poll = append(poll, l)
			}
		}
		if len(poll) > 0 {
			opts.Poll = poll
		}
		id, err := q.node().PostWith(a, "All", subject, signed(q.user, body), 0, opts)
		if err == nil {
			q.redirect(Base + "/topic/" + strconv.FormatInt(id, 10))
			return
		}
		form["Error"] = capital(err.Error()) + "."
		form["Subject"], form["Body"], form["Poll"] = subject, body, q.r.PostForm.Get("poll")
	}
	q.render(http.StatusOK, "post.html", "Start New Topic", []crumb{{a.Name, Base + "/area/" + strconv.FormatInt(a.ID, 10)},
		{"Start New Topic", ""}}, form)
}

func (s *Server) like(q *req) {
	if q.needLogin() {
		return
	}
	m, _ := q.theMessage()
	if m == nil {
		return
	}
	if m.FromID == q.user.ID {
		q.fail(http.StatusBadRequest, "You cannot like your own post.")
		return
	}
	v := 0
	if q.r.PostForm.Get("like") == "1" {
		v = 1
	}
	if err := s.B.Store.Vote(m.ID, q.user.ID, v); err != nil {
		q.fail(http.StatusInternalServerError, err.Error())
		return
	}
	q.redirect(Base + "/post/" + strconv.FormatInt(m.ID, 10))
}

func (s *Server) answer(q *req) {
	if q.needLogin() {
		return
	}
	m, _ := q.theMessage()
	if m == nil {
		return
	}
	answers, _, _, err := s.B.Store.Poll(m.ID, q.user.ID)
	i, e := strconv.Atoi(q.r.PostForm.Get("answer"))
	if err != nil || answers == nil || e != nil || i < 0 || i >= len(answers) {
		q.fail(http.StatusBadRequest, "Choose one of the poll's answers.")
		return
	}
	if err := s.B.Store.PollVote(m.ID, q.user.ID, i); err != nil {
		q.fail(http.StatusInternalServerError, err.Error())
		return
	}
	q.redirect(Base + "/topic/" + strconv.FormatInt(m.ID, 10))
}

func (s *Server) deletePost(q *req) {
	if q.needLogin() {
		return
	}
	m, a := q.theMessage()
	if m == nil {
		return
	}
	root := s.B.Store.TopicOf(m)
	if err := q.node().DeleteMessage(m); err != nil {
		q.fail(http.StatusForbidden, capital(err.Error())+".")
		return
	}
	if root == m.ID {
		// the topic's first gone: the next begins it, if any; to the forum
		q.redirect(Base + "/area/" + strconv.FormatInt(a.ID, 10))
		return
	}
	q.redirect(Base + "/topic/" + strconv.FormatInt(root, 10))
}

// --- people

func (s *Server) profile(q *req) {
	u, err := s.B.Store.UserByName(q.r.PathValue("name"))
	if err != nil || q.user == nil && !s.Guests {
		if err == nil {
			q.needLogin()
			return
		}
		q.fail(http.StatusNotFound, "There is no such member.")
		return
	}
	areas, _ := q.areas()
	ids := make([]int64, len(areas))
	for i, a := range areas {
		ids[i] = a.ID
	}
	recent, _ := s.B.Store.MessagesBy(u.ID, ids, 10)
	// what was posted under an alias is not theirs to show
	var shown []*store.Message
	for _, m := range recent {
		if strings.EqualFold(m.From, u.Name) {
			shown = append(shown, m)
		}
	}
	var on []bbs.NodeInfo
	for _, n := range s.B.Nodes() {
		if strings.EqualFold(n.User, u.Name) {
			on = append(on, n)
		}
	}
	q.render(http.StatusOK, "profile.html", u.Name, []crumb{{u.Name, ""}}, map[string]any{
		"U": u, "Recent": shown, "On": on})
}

func (s *Server) online(q *req) {
	nodes := s.B.Nodes()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Num < nodes[j].Num })
	q.render(http.StatusOK, "online.html", "Online Users", []crumb{{"Online Users", ""}}, map[string]any{"Nodes": nodes})
}

type hit struct {
	M    *store.Message
	Area *store.Area
}

func (s *Server) search(q *req) {
	text := strings.TrimSpace(q.r.URL.Query().Get("q"))
	body := map[string]any{"Q": text}
	if text != "" {
		if len([]rune(text)) < 3 {
			body["Error"] = "Search for 3 letters or more."
		} else {
			areas, _ := q.areas()
			byID := map[int64]*store.Area{}
			ids := make([]int64, len(areas))
			for i, a := range areas {
				ids[i], byID[a.ID] = a.ID, a
			}
			ms, err := s.B.Store.SearchMessages(ids, text, 50)
			if err != nil {
				q.fail(http.StatusInternalServerError, err.Error())
				return
			}
			var hits []hit
			for _, m := range ms {
				hits = append(hits, hit{m, byID[m.AreaID]})
			}
			body["Hits"] = hits
			body["Searched"] = true
		}
	}
	q.render(http.StatusOK, "search.html", "Search", []crumb{{"Search", ""}}, body)
}

// --- logging in and out, signing up

// safeBack is where to go after a login: a page of the forum's.
func safeBack(back string) string {
	if !strings.HasPrefix(back, Base+"/") || strings.HasPrefix(back, "//") || strings.ContainsAny(back, "\\\r\n") {
		return Base + "/"
	}
	return back
}

func (s *Server) login(q *req) {
	back := safeBack(q.r.FormValue("back"))
	if q.user != nil {
		q.redirect(back)
		return
	}
	form := map[string]any{"Back": back}
	if q.r.Method == http.MethodPost {
		name := strings.TrimSpace(q.r.PostForm.Get("name"))
		secret, _, err := s.Sessions.Login(q.r, name, q.r.PostForm.Get("password"))
		if err == nil {
			q.setCookie(secret)
			q.redirect(back)
			return
		}
		form["Error"] = capital(err.Error()) + "."
		form["Name"] = name
	}
	q.render(http.StatusOK, "login.html", "Sign In", []crumb{{"Sign In", ""}}, form)
}

func (s *Server) logout(q *req) {
	if q.secret != "" {
		s.Sessions.Logout(q.secret)
	}
	q.setCookie("")
	q.redirect(Base + "/")
}

func (s *Server) register(q *req) {
	if !s.B.NewUsers {
		q.fail(http.StatusForbidden, "New members are not being taken.")
		return
	}
	if q.user != nil {
		q.redirect(Base + "/")
		return
	}
	form := map[string]any{}
	if q.r.Method == http.MethodPost {
		f := q.r.PostForm
		name, loc := strings.TrimSpace(f.Get("name")), strings.TrimSpace(f.Get("location"))
		var err error
		if f.Get("password") != f.Get("password2") {
			err = errors.New("the passwords differ")
		} else {
			n := s.B.Agent(nil, "web")
			n.Caller.Source = "ip:" + q.host()
			if _, err = n.Register(name, f.Get("password"), loc); err == nil {
				var secret string
				if secret, _, err = s.Sessions.Login(q.r, name, f.Get("password")); err == nil {
					q.setCookie(secret)
					q.redirect(Base + "/?notice=welcome")
					return
				}
			}
		}
		form["Error"] = capital(err.Error()) + "."
		form["Name"], form["Location"] = name, loc
	}
	q.render(http.StatusOK, "register.html", "Create Account", []crumb{{"Create Account", ""}}, form)
}

// capital is s with its first letter upper case (an error as a sentence).
func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.TrimSuffix(s[1:], ".")
}
