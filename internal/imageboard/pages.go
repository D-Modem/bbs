package imageboard

import (
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

var funcs = template.FuncMap{
	"size":  humanSize,
	"date":  postDate,
	"iso":   func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
	"add":   func(a, b int) int { return a + b },
	"short": shortName,
	"dur":   drawTime,
	"clock": clock,
	"kind":  fileKind,
	"flag":  flagOf,
	"idc":   idClass,
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out
	},
}

// postDate is when a post was made, as 4chan writes it.
func postDate(t time.Time) string { return t.Local().Format("01/02/06(Mon)15:04:05") }

// drawTime is how long a drawing took: 45s, 3m12s, 1h05m.
func drawTime(ms int64) string {
	s := ms / 1000
	switch {
	case s >= 3600:
		return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
	case s >= 60:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
}

// humanSize is a file's size as 4chan shows it.
func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// shortName is a long file name cut, as 4chan cuts them.
func shortName(s string) string {
	r := []rune(s)
	if len(r) > 30 {
		return string(r[:25]) + "(...)"
	}
	return s
}

// postView is a post as a page shows it.
type postView struct {
	*store.BoardPost
	Dir       string
	Name      string // its name, or the board's default
	Thread    int64  // its thread's number (its own, for a first post)
	OP        bool
	Comment   template.HTML
	Cut       bool // the comment is cut, on an index
	Backlinks []int64
	SrcURL    string
	ThumbURL  string
	Staff     bool // the viewer may moderate it
	Index     bool // shown on a board's index (a thread's first gets [Reply])
	Draw      bool // its board is an oekaki board: its drawing can be drawn over
	IDCount   int  // the posts by its poster's ID in the thread (on a thread's page)
	Play      bool // its file is Flash, and Ruffle is here to play it
}

// threadView is a thread as a board's index or its own page shows it.
type threadView struct {
	OP            *postView
	Replies       []*postView
	OmittedPosts  int
	OmittedImages int
	BumpLimit     bool
	ImageLimit    bool
	Teaser        template.HTML // the catalog's
}

// links finds what a page's quotes point at, all in a few queries.
type links struct {
	q      *req
	board  *store.Board
	local  map[int64]int64
	others map[string]map[int64]int64
	boards map[string]bool
}

// thread is the thread post no of this board is in.
func (l *links) thread(no int64) (int64, bool) {
	t, ok := l.local[no]
	return t, ok
}

// cross is the thread post no of board dir is in; for no 0, whether the
// board is there.
func (l *links) cross(dir string, no int64) (int64, bool) {
	if no == 0 {
		return 0, l.boards[dir]
	}
	t, ok := l.others[dir][no]
	return t, ok
}

// newLinks looks up the quotes of the posts.
func (q *req) newLinks(b *store.Board, posts []*store.BoardPost) *links {
	l := &links{q: q, board: b, local: map[int64]int64{}, others: map[string]map[int64]int64{}, boards: map[string]bool{}}
	var nos []int64
	others := map[string][]int64{}
	for _, p := range posts {
		n, c := quotes(p.Comment)
		nos = append(nos, n...)
		for d, ns := range c {
			others[d] = append(others[d], ns...)
		}
	}
	if len(nos) > 0 {
		l.local, _ = q.s.B.Store.ChanPostThreads(b.ID, nos)
	}
	for d, ns := range others {
		ob, err := q.s.B.Store.Board(d)
		if err != nil {
			continue
		}
		l.boards[d] = true
		l.others[d], _ = q.s.B.Store.ChanPostThreads(ob.ID, ns)
	}
	return l
}

// view makes a post's view: here is the thread being shown (0: an
// index), cut the lines an index shows of a comment (0: all of it).
func (q *req) view(b *store.Board, p *store.BoardPost, l *links, here int64, cut int) *postView {
	v := &postView{BoardPost: p, Dir: b.Dir, Name: p.Name, Thread: p.Thread, OP: p.Thread == 0}
	if v.OP {
		v.Thread = p.No
	}
	if p.Account != "" {
		v.Name = p.Account
	} else if v.Name == "" && p.Capcode == "" {
		v.Name = b.DefaultName
	}
	text := p.Comment
	if cut > 0 {
		text, v.Cut = abbreviate(text, cut)
	}
	v.Comment = markup(text, b.Dir, here, l)
	if p.HasFile() {
		v.SrcURL = fmt.Sprintf("%s/%s/src/%d%s", Base, b.Dir, p.Tim, p.Ext)
		v.ThumbURL = fmt.Sprintf("%s/%s/thumb/%ds.jpg", Base, b.Dir, p.Tim)
		v.Play = fileKind(p.Ext) == "flash" && q.s.conf().Ruffle != ""
	}
	v.Staff = q.staff(b) != ""
	v.Index = here == 0
	v.Draw = b.Oekaki
	return v
}

// front is the image boards' front page: the boards, the latest images,
// and how much there is.
func (s *Server) front(q *req) {
	recent, _ := s.B.Store.RecentImages(8)
	boards, _ := s.B.Store.Boards()
	byID := map[int64]*store.Board{}
	for _, b := range boards {
		byID[b.ID] = b
	}
	type thumb struct {
		Dir      string
		URL      string
		Thumb    string
		Title    string
		TnW, TnH int
	}
	var thumbs []thumb
	for _, p := range recent {
		b := byID[p.BoardID]
		if b == nil || !b.Worksafe || b.LoginToRead && q.user == nil {
			continue // as 4chan's front page: worksafe boards' only
		}
		t := p.Thread
		if t == 0 {
			t = p.No
		}
		w, h := fit(p.TnW, p.TnH, 150)
		thumbs = append(thumbs, thumb{Dir: b.Dir, URL: fmt.Sprintf("%s/%s/thread/%d#p%d", Base, b.Dir, t, p.No),
			Thumb: fmt.Sprintf("%s/%s/thumb/%ds.jpg", Base, b.Dir, p.Tim), Title: b.Title, TnW: w, TnH: h})
	}
	posts, bytes, _ := s.B.Store.ChanStats()
	q.render(http.StatusOK, "front.html", s.B.Conf().Name, nil, map[string]any{"Boards": boards, "Recent": thumbs,
		"Posts": posts, "Bytes": bytes})
}

// replies shown under each thread on an index: 5, and 1 under a sticky.
const (
	indexReplies  = 5
	stickyReplies = 1
	indexLines    = 15
)

// index is a page of a board: its threads, the last bumped first, each
// with its last replies.
func (s *Server) index(q *req, b *store.Board, page int) {
	threads, err := s.B.Store.ChanThreads(b.ID, (page-1)*b.PerPage, b.PerPage)
	if err != nil {
		q.fail(http.StatusInternalServerError, "The board could not be read.")
		return
	}
	total, _ := s.B.Store.ChanThreadCount(b.ID)
	pages := max(1, min(b.Pages, (total+b.PerPage-1)/b.PerPage))
	if page > pages {
		q.notFound()
		return
	}
	views, err := q.threadViews(b, threads, 0)
	if err != nil {
		q.fail(http.StatusInternalServerError, "The board could not be read.")
		return
	}
	q.render(http.StatusOK, "board.html", "/"+b.Dir+"/ - "+b.Title, b, map[string]any{"Threads": views,
		"Page": page, "Pages": pages, "Form": q.form(b, nil)})
}

// threadViews are threads with their last replies (here 0), or all of
// them (here the thread).
func (q *req) threadViews(b *store.Board, ops []*store.BoardPost, here int64) ([]*threadView, error) {
	var all []*store.BoardPost
	replies := map[int64][]*store.BoardPost{}
	for _, op := range ops {
		n := indexReplies
		if op.Sticky {
			n = stickyReplies
		}
		if here != 0 {
			n = 0
		}
		rs, err := q.s.B.Store.ChanReplies(b.ID, op.No, n)
		if err != nil {
			return nil, err
		}
		replies[op.No] = rs
		all = append(all, op)
		all = append(all, rs...)
	}
	l := q.newLinks(b, all)
	cut := indexLines
	if here != 0 {
		cut = 0
	}
	var out []*threadView
	for _, op := range ops {
		tv := &threadView{OP: q.view(b, op, l, here, cut), BumpLimit: op.Replies >= b.BumpLimit,
			ImageLimit: op.Images >= b.ImageLimit}
		images := 0
		for _, r := range replies[op.No] {
			tv.Replies = append(tv.Replies, q.view(b, r, l, here, cut))
			if r.Ext != "" {
				images++
			}
		}
		tv.OmittedPosts = op.Replies - len(tv.Replies)
		tv.OmittedImages = op.Images - images
		if here != 0 {
			backlinks(tv)
			idCounts(tv)
		}
		out = append(out, tv)
	}
	return out, nil
}

// idCounts are, for each post, how many in the thread are by its ID.
func idCounts(tv *threadView) {
	posts := append([]*postView{tv.OP}, tv.Replies...)
	n := map[string]int{}
	for _, p := range posts {
		if p.PosterID != "" {
			n[p.PosterID]++
		}
	}
	for _, p := range posts {
		p.IDCount = n[p.PosterID]
	}
}

// backlinks are, under each post's number, the posts in the thread that
// quote it.
func backlinks(tv *threadView) {
	posts := append([]*postView{tv.OP}, tv.Replies...)
	byNo := map[int64]*postView{}
	for _, p := range posts {
		byNo[p.No] = p
	}
	for _, p := range posts {
		nos, _ := quotes(p.BoardPost.Comment)
		seen := map[int64]bool{}
		for _, n := range nos {
			if t := byNo[n]; t != nil && !seen[n] && n != p.No {
				seen[n] = true
				t.Backlinks = append(t.Backlinks, p.No)
			}
		}
	}
}

// thread is a thread's own page: all of it, and the form to reply.
func (s *Server) thread(q *req, b *store.Board, no int64) {
	op, err := s.B.Store.ChanThread(b.ID, no)
	if err != nil {
		// a reply's number: to its post in its thread
		if p, err := s.B.Store.ChanPost(b.ID, no); err == nil {
			http.Redirect(q.w, q.r, fmt.Sprintf("%s/%s/thread/%d#p%d", Base, b.Dir, p.Thread, p.No), http.StatusFound)
			return
		}
		q.fail(http.StatusNotFound, "That thread is not here: it has been pruned or deleted.")
		return
	}
	if !op.Archived.IsZero() && s.conf().ArchiveFor > 0 && time.Since(op.Archived) > s.conf().ArchiveFor {
		// its time in the archive is up
		s.expireArchive(b)
		q.fail(http.StatusNotFound, "That thread is not here: it was archived, and has been deleted since.")
		return
	}
	views, err := q.threadViews(b, []*store.BoardPost{op}, no)
	if err != nil {
		q.fail(http.StatusInternalServerError, "The thread could not be read.")
		return
	}
	title := "/" + b.Dir + "/ - "
	if op.Subject != "" {
		title += op.Subject
	} else {
		title += firstWords(op.Comment, b.Title)
	}
	q.render(http.StatusOK, "board.html", title, b, map[string]any{"Threads": views, "Thread": views[0],
		"Form": q.form(b, op)})
}

// firstWords are a comment's first words, for a thread's title.
func firstWords(s, otherwise string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 50 {
		s = string(r[:50]) + "..."
	}
	if s == "" {
		return otherwise
	}
	return s
}

// formView is the post form.
type formView struct {
	Reply     *store.BoardPost // the thread replied to (nil: a new thread)
	Name      string
	Quote     string
	Closed    bool // no one but staff may reply
	Archived  bool // no one may reply: the thread is archived
	CanPost   bool
	NeedLogin bool
	Capcode   string // the staff's
	MaxFile   int64
	Image     bool   // an image is needed
	Oekaki    bool   // the drawing board is offered
	Captcha   string // a captcha's token, for one who is not signed in ("": none)
	Options   bool   // the Options line (sage, nonoko) is shown
	Account   string // the signed-in user's name, to sign with ("": none, or a forced_anon board)
	Accept    string // the file input's MIME types
	Types     string // the file types taken, in words: "JPG, PNG, GIF, WebP and WebM"
	VideoRule string // what videos may be: "Videos may be up to 2:00 long, without sound."
}

// form is the post form for board b, replying to op (nil: a new thread):
// whether the caller may post and why not, the name they used last, a
// quote asked for, the files taken, and a captcha if they need one.
func (q *req) form(b *store.Board, op *store.BoardPost) formView {
	f := formView{Reply: op, MaxFile: q.s.maxFile(b), Capcode: q.staff(b), CanPost: true}
	if c, err := q.r.Cookie(nameCookie); err == nil {
		f.Name = c.Value
	}
	if n := q.r.URL.Query().Get("quote"); n != "" && len(n) < 13 {
		f.Quote = ">>" + strings.TrimLeft(n, ">") + "\n"
	}
	switch {
	case op != nil && !op.Archived.IsZero():
		f.Archived, f.CanPost = true, false
	case op != nil && op.Closed && f.Capcode == "":
		f.Closed, f.CanPost = true, false
	case b.Locked && f.Capcode == "":
		f.CanPost = false
	case (b.LoginToPost || b.LoginToRead) && q.user == nil:
		f.CanPost, f.NeedLogin = false, true
	}
	f.Image = op == nil && !b.TextOnly
	f.Oekaki = b.Oekaki
	f.Accept, f.Types = "image/jpeg,image/png,image/gif,image/webp", "JPG, PNG, GIF"
	words := []string{"WebP"}
	video := false
	for _, k := range FileKinds {
		if takes(b, k) {
			f.Accept += "," + mediaTypes["."+k].mime
			words = append(words, kindNames[k])
			video = video || k == "webm" || k == "mp4" || k == "flv"
		}
	}
	for i, w := range words {
		if i == len(words)-1 {
			f.Types += " and " + w
		} else {
			f.Types += ", " + w
		}
	}
	switch {
	case !video:
	case q.s.conf().MaxVideo > 0 && b.Sound:
		f.VideoRule = "Videos may be up to " + clock(q.s.conf().MaxVideo.Milliseconds()) + " long."
	case q.s.conf().MaxVideo > 0:
		f.VideoRule = "Videos may be up to " + clock(q.s.conf().MaxVideo.Milliseconds()) + " long, without sound."
	case !b.Sound:
		f.VideoRule = "Videos may not have sound."
	}
	if q.user != nil && !b.ForcedAnon {
		f.Account = q.user.Name
	}
	f.Options = q.s.ShowOptions(b)
	if f.CanPost && q.needsCaptcha(b) {
		f.Captcha = q.s.NewCaptcha()
	}
	return f
}

// maxFile is the largest image a board takes.
func (s *Server) maxFile(b *store.Board) int64 {
	if b.MaxFile > 0 {
		return b.MaxFile
	}
	return s.conf().MaxFile
}

// catalog is a board's threads at a glance: each first post's thumbnail,
// its counts, and the start of its comment.
func (s *Server) catalog(q *req, b *store.Board) {
	ops, err := s.B.Store.ChanThreads(b.ID, 0, b.Pages*b.PerPage)
	if err != nil {
		q.fail(http.StatusInternalServerError, "The board could not be read.")
		return
	}
	if q.r.URL.Query().Get("order") == "new" {
		sort.SliceStable(ops, func(i, j int) bool { return ops[i].No > ops[j].No })
	}
	l := q.newLinks(b, ops)
	var views []*threadView
	for _, op := range ops {
		v := q.view(b, op, l, 0, 0)
		text, _ := abbreviate(op.Comment, 4)
		if r := []rune(text); len(r) > 200 {
			text = string(r[:200]) + "..."
		}
		w, h := fit(op.TnW, op.TnH, 150)
		v.TnW, v.TnH = w, h
		views = append(views, &threadView{OP: v, Teaser: markup(text, b.Dir, 0, l),
			BumpLimit: op.Replies >= b.BumpLimit, ImageLimit: op.Images >= b.ImageLimit})
	}
	q.render(http.StatusOK, "catalog.html", "/"+b.Dir+"/ - "+b.Title+" - Catalog", b, map[string]any{"Threads": views,
		"New": q.r.URL.Query().Get("order") == "new"})
}

// archiveRow is an archived thread as the archive lists it.
type archiveRow struct {
	No       int64
	Subject  string
	Excerpt  string
	Archived time.Time
	Replies  int
}

// archive is a board's archive: its threads pruned from its pages, the
// last archived first, kept [chan] archive_days.
func (s *Server) archive(q *req, b *store.Board) {
	if !b.Archive {
		q.fail(http.StatusNotFound, "This board keeps no archive.")
		return
	}
	s.expireArchive(b)
	ops, err := s.B.Store.ChanArchive(b.ID)
	if err != nil {
		q.fail(http.StatusInternalServerError, "The archive could not be read.")
		return
	}
	rows := make([]archiveRow, len(ops))
	for i, op := range ops {
		text := strings.Join(strings.Fields(op.Comment), " ")
		if r := []rune(text); len(r) > 120 {
			text = string(r[:120]) + "..."
		}
		rows[i] = archiveRow{No: op.No, Subject: op.Subject, Excerpt: text, Archived: op.Archived, Replies: op.Replies}
	}
	keep := ""
	if s.conf().ArchiveFor > 0 {
		keep = keepFor(s.conf().ArchiveFor)
	}
	q.render(http.StatusOK, "archive.html", "/"+b.Dir+"/ - "+b.Title+" - Archive", b, map[string]any{"Rows": rows,
		"Keep": keep})
}

// keepFor is how long the archive keeps a thread, in words.
func keepFor(d time.Duration) string {
	switch {
	case d >= 48*time.Hour && d%(24*time.Hour) == 0:
		return strconv.Itoa(int(d/(24*time.Hour))) + " days"
	case d >= 2*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " hours"
	}
	return d.String()
}
