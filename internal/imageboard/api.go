package imageboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// The read-only JSON, in 4chan's API's shapes (github.com/4chan/4chan-API)
// so that its readers read these boards too: /chan/boards.json,
// /chan/b/threads.json, /chan/b/catalog.json, /chan/b/1.json and
// /chan/b/thread/123.json.  Images are at /chan/b/src/{tim}{ext}, their
// thumbnails at /chan/b/thumb/{tim}s.jpg.

type apiPost struct {
	No            int64      `json:"no"`
	Resto         int64      `json:"resto"`
	Sticky        int        `json:"sticky,omitempty"`
	Closed        int        `json:"closed,omitempty"`
	Now           string     `json:"now"`
	Time          int64      `json:"time"`
	Name          string     `json:"name,omitempty"`
	Trip          string     `json:"trip,omitempty"`
	Capcode       string     `json:"capcode,omitempty"`
	Sub           string     `json:"sub,omitempty"`
	Com           string     `json:"com,omitempty"`
	Filename      string     `json:"filename,omitempty"`
	Ext           string     `json:"ext,omitempty"`
	Fsize         int64      `json:"fsize,omitempty"`
	MD5           string     `json:"md5,omitempty"`
	W             int        `json:"w,omitempty"`
	H             int        `json:"h,omitempty"`
	TnW           int        `json:"tn_w,omitempty"`
	TnH           int        `json:"tn_h,omitempty"`
	Tim           int64      `json:"tim,omitempty"`
	FileDeleted   int        `json:"filedeleted,omitempty"`
	Replay        int        `json:"replay,omitempty"`  // an oekaki drawing, its strokes in its PNG
	DrawMS        int64      `json:"draw_ms,omitempty"` // how long it took to draw
	Archived      int        `json:"archived,omitempty"`
	ArchivedOn    int64      `json:"archived_on,omitempty"`
	DurationMS    int64      `json:"duration_ms,omitempty"` // a video's length
	PageCount     int        `json:"page_count,omitempty"`  // a PDF's pages
	Member        int        `json:"member,omitempty"`      // posted by a signed-in user of the BBS
	Account       string     `json:"account,omitempty"`     // their account's name, if they signed with it
	ID            string     `json:"id,omitempty"`
	Country       string     `json:"country,omitempty"`
	CountryName   string     `json:"country_name,omitempty"`
	Spoiler       int        `json:"spoiler,omitempty"`
	Replies       *int       `json:"replies,omitempty"`
	Images        *int       `json:"images,omitempty"`
	BumpLimit     int        `json:"bumplimit,omitempty"`
	ImageLimit    int        `json:"imagelimit,omitempty"`
	SemanticURL   string     `json:"semantic_url,omitempty"`
	LastModified  int64      `json:"last_modified,omitempty"`
	OmittedPosts  int        `json:"omitted_posts,omitempty"`
	OmittedImages int        `json:"omitted_images,omitempty"`
	LastReplies   []*apiPost `json:"last_replies,omitempty"`
}

func flag(b bool) int {
	if b {
		return 1
	}
	return 0
}

var slugPat = regexp.MustCompile(`[^a-z0-9]+`)

// semanticURL is a thread's slug, as 4chan makes them.
func semanticURL(p *store.BoardPost) string {
	s := p.Subject
	if s == "" {
		s = p.Comment
	}
	s = strings.Trim(slugPat.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	return s
}

func (q *req) apiPost(b *store.Board, v *postView) *apiPost {
	p := v.BoardPost
	a := &apiPost{No: p.No, Resto: p.Thread, Now: postDate(p.Created), Time: p.Created.Unix(),
		Name: v.Name, Trip: p.Trip, Capcode: p.Capcode, Sub: p.Subject, Com: string(v.Comment),
		Member: flag(p.UserID != 0), Account: p.Account, ID: p.PosterID, Country: p.Country,
		CountryName: p.CountryName}
	if p.Ext != "" {
		a.Filename, a.Ext, a.Fsize, a.MD5, a.W, a.H, a.TnW, a.TnH, a.Tim = p.FileName, p.Ext, p.FileSize, p.MD5, p.W,
			p.H, p.TnW, p.TnH, p.Tim
		a.Spoiler, a.FileDeleted = flag(p.Spoiler), flag(p.FileDeleted)
		if p.Replay && !p.FileDeleted {
			a.Replay, a.DrawMS = 1, p.DrawMS
		}
		if !p.FileDeleted {
			a.DurationMS, a.PageCount = p.DurationMS, p.PageCount
		}
		if p.FileDeleted {
			a.Filename, a.Ext, a.Fsize, a.MD5, a.W, a.H, a.TnW, a.TnH, a.Tim = "", "", 0, "", 0, 0, 0, 0, 0
		}
	}
	if p.Thread == 0 {
		a.Sticky, a.Closed = flag(p.Sticky), flag(p.Closed)
		if !p.Archived.IsZero() {
			a.Archived, a.ArchivedOn = 1, p.Archived.Unix()
		}
		r, i := p.Replies, p.Images
		a.Replies, a.Images = &r, &i
		a.BumpLimit, a.ImageLimit = flag(r >= b.BumpLimit), flag(i >= b.ImageLimit)
		a.SemanticURL = semanticURL(p)
		a.LastModified = p.Bumped.Unix()
	}
	return a
}

func (q *req) json(v any) {
	h := q.w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cache-Control", "no-cache")
	json.NewEncoder(q.w).Encode(v)
}

func (q *req) jsonFail(status int) {
	q.w.Header().Set("Content-Type", "application/json")
	q.w.WriteHeader(status)
	fmt.Fprintln(q.w, `{"error":"not found"}`)
}

// boardsJSON is boards.json: each board and how it is run.
func (s *Server) boardsJSON(q *req) {
	bs, err := s.B.Store.Boards()
	if err != nil {
		q.jsonFail(http.StatusInternalServerError)
		return
	}
	type cooldowns struct {
		Threads int `json:"threads"`
		Replies int `json:"replies"`
		Images  int `json:"images"`
	}
	type board struct {
		Board           string    `json:"board"`
		Title           string    `json:"title"`
		WSBoard         int       `json:"ws_board"`
		PerPage         int       `json:"per_page"`
		Pages           int       `json:"pages"`
		MaxFilesize     int64     `json:"max_filesize"`
		MaxCommentChars int       `json:"max_comment_chars"`
		ImageLimit      int       `json:"image_limit"`
		BumpLimit       int       `json:"bump_limit"`
		MetaDescription string    `json:"meta_description"`
		Cooldowns       cooldowns `json:"cooldowns"`
		ForcedAnon      int       `json:"forced_anon,omitempty"`
		TextOnly        int       `json:"text_only,omitempty"`
		Oekaki          int       `json:"oekaki,omitempty"`
		UserIDs         int       `json:"user_ids,omitempty"`
		CountryFlags    int       `json:"country_flags,omitempty"`
		Login           string    `json:"login,omitempty"`      // "post": only users post; "all": only users see it
		Captcha         int       `json:"captcha,omitempty"`    // those not signed in answer a captcha
		FileTypes       []string  `json:"file_types,omitempty"` // taken besides images: webm, mp4, pdf
		WebmAudio       int       `json:"webm_audio,omitempty"` // videos may have sound
		MaxWebmDuration int       `json:"max_webm_duration,omitempty"`
		Spoilers        int       `json:"spoilers"`
		CodeTags        int       `json:"code_tags"`
		IsArchived      int       `json:"is_archived"`
	}
	out := struct {
		Boards []board `json:"boards"`
	}{Boards: []board{}}
	for _, b := range bs {
		cd := cooldowns{int(s.S.ThreadWait.Seconds()), int(s.S.ReplyWait.Seconds()), int(s.S.ReplyWait.Seconds())}
		out.Boards = append(out.Boards, board{Board: b.Dir, Title: b.Title, WSBoard: flag(b.Worksafe), PerPage: b.PerPage,
			Pages: b.Pages, MaxFilesize: s.maxFile(b), MaxCommentChars: s.S.MaxComment, ImageLimit: b.ImageLimit,
			BumpLimit: b.BumpLimit, MetaDescription: b.Description, Cooldowns: cd, ForcedAnon: flag(b.ForcedAnon),
			TextOnly: flag(b.TextOnly), Oekaki: flag(b.Oekaki), UserIDs: flag(b.PosterIDs),
			CountryFlags: flag(b.Flags && s.S.GeoIP != ""), Login: loginOf(b), Captcha: flag(s.captchaOn(b)), Spoilers: 1, CodeTags: 1,
			IsArchived: flag(b.Archive)})
		if bd := &out.Boards[len(out.Boards)-1]; b.FileTypes != "" {
			for _, k := range FileKinds {
				if takes(b, k) {
					bd.FileTypes = append(bd.FileTypes, k)
				}
			}
			if takes(b, "webm") || takes(b, "mp4") {
				bd.WebmAudio, bd.MaxWebmDuration = flag(b.Sound), int(s.S.MaxVideo.Seconds())
			}
		}
	}
	q.json(out)
}

// allThreads are a board's threads, a page's worth at a time.
func (s *Server) allThreads(b *store.Board) ([][]*store.BoardPost, error) {
	ops, err := s.B.Store.ChanThreads(b.ID, 0, b.Pages*b.PerPage)
	if err != nil {
		return nil, err
	}
	var pages [][]*store.BoardPost
	for len(ops) > 0 {
		n := min(len(ops), b.PerPage)
		pages = append(pages, ops[:n])
		ops = ops[n:]
	}
	return pages, nil
}

// threadsJSON is threads.json: each page's threads, when each changed.
func (s *Server) threadsJSON(q *req, b *store.Board) {
	pages, err := s.allThreads(b)
	if err != nil {
		q.jsonFail(http.StatusInternalServerError)
		return
	}
	type thread struct {
		No           int64 `json:"no"`
		LastModified int64 `json:"last_modified"`
		Replies      int   `json:"replies"`
	}
	type page struct {
		Page    int      `json:"page"`
		Threads []thread `json:"threads"`
	}
	out := []page{}
	for i, ops := range pages {
		pg := page{Page: i + 1, Threads: []thread{}}
		for _, op := range ops {
			pg.Threads = append(pg.Threads, thread{op.No, op.Bumped.Unix(), op.Replies})
		}
		out = append(out, pg)
	}
	q.json(out)
}

// catalogJSON is catalog.json: each page's threads, with their last
// replies.
func (s *Server) catalogJSON(q *req, b *store.Board) {
	pages, err := s.allThreads(b)
	if err != nil {
		q.jsonFail(http.StatusInternalServerError)
		return
	}
	type page struct {
		Page    int        `json:"page"`
		Threads []*apiPost `json:"threads"`
	}
	out := []page{}
	for i, ops := range pages {
		tvs, err := q.threadViews(b, ops, 0)
		if err != nil {
			q.jsonFail(http.StatusInternalServerError)
			return
		}
		pg := page{Page: i + 1, Threads: []*apiPost{}}
		for _, tv := range tvs {
			pg.Threads = append(pg.Threads, q.apiThread(b, tv))
		}
		out = append(out, pg)
	}
	q.json(out)
}

// apiThread is a thread as an index has it: its first post, with its last
// replies.
func (q *req) apiThread(b *store.Board, tv *threadView) *apiPost {
	a := q.apiPost(b, tv.OP)
	a.OmittedPosts, a.OmittedImages = tv.OmittedPosts, tv.OmittedImages
	for _, r := range tv.Replies {
		a.LastReplies = append(a.LastReplies, q.apiPost(b, r))
	}
	return a
}

// pageJSON is a page of the index: its threads, each first post and its
// last replies, as {"threads": [{"posts": [...]}]}.
func (s *Server) pageJSON(q *req, b *store.Board, page int) {
	if page > b.Pages {
		q.jsonFail(http.StatusNotFound)
		return
	}
	ops, err := s.B.Store.ChanThreads(b.ID, (page-1)*b.PerPage, b.PerPage)
	if err != nil {
		q.jsonFail(http.StatusInternalServerError)
		return
	}
	if len(ops) == 0 && page > 1 {
		q.jsonFail(http.StatusNotFound)
		return
	}
	tvs, err := q.threadViews(b, ops, 0)
	if err != nil {
		q.jsonFail(http.StatusInternalServerError)
		return
	}
	type thread struct {
		Posts []*apiPost `json:"posts"`
	}
	out := struct {
		Threads []thread `json:"threads"`
	}{Threads: []thread{}}
	for _, tv := range tvs {
		op := q.apiPost(b, tv.OP)
		op.OmittedPosts, op.OmittedImages = tv.OmittedPosts, tv.OmittedImages
		t := thread{Posts: []*apiPost{op}}
		for _, r := range tv.Replies {
			t.Posts = append(t.Posts, q.apiPost(b, r))
		}
		out.Threads = append(out.Threads, t)
	}
	q.json(out)
}

// threadJSON is a thread: {"posts": [...]}, its first post first.
func (s *Server) threadJSON(q *req, b *store.Board, no int64) {
	op, err := s.B.Store.ChanThread(b.ID, no)
	if err != nil {
		q.jsonFail(http.StatusNotFound)
		return
	}
	tvs, err := q.threadViews(b, []*store.BoardPost{op}, no)
	if err != nil {
		q.jsonFail(http.StatusInternalServerError)
		return
	}
	out := struct {
		Posts []*apiPost `json:"posts"`
	}{Posts: []*apiPost{q.apiPost(b, tvs[0].OP)}}
	for _, r := range tvs[0].Replies {
		out.Posts = append(out.Posts, q.apiPost(b, r))
	}
	q.json(out)
}

// loginOf is who may use a board: "" anyone, "post" users post, "all"
// users alone see it.
func loginOf(b *store.Board) string {
	switch {
	case b.LoginToRead:
		return "all"
	case b.LoginToPost:
		return "post"
	}
	return ""
}

// archiveJSON is 4chan's archive.json: the archived threads' numbers,
// the oldest first.
func (s *Server) archiveJSON(q *req, b *store.Board) {
	if !b.Archive {
		q.jsonFail(http.StatusNotFound)
		return
	}
	s.expireArchive(b)
	ops, err := s.B.Store.ChanArchive(b.ID)
	if err != nil {
		q.jsonFail(http.StatusInternalServerError)
		return
	}
	nos := make([]int64, 0, len(ops))
	for i := len(ops) - 1; i >= 0; i-- {
		nos = append(nos, ops[i].No)
	}
	q.json(nos)
}
