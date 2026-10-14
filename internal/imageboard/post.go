package imageboard

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

const (
	maxName    = 75
	maxEmail   = 75
	maxSubject = 100
	maxLines   = 100
)

// field is a one-line field as kept: control characters out, cut.
func field(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0 || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

// banned shows the banned what they were banned for.
func (q *req) banned(b *store.Ban) {
	q.render(http.StatusForbidden, "banned.html", "Banned", nil, map[string]any{"Ban": b, "IP": q.host()})
}

// checkBan shows the banned page if the caller is banned on the board.
func (q *req) checkBan(dir string) bool {
	ban, err := q.s.B.Store.BanFor(q.host(), dir)
	if err != nil {
		q.fail(http.StatusInternalServerError, "The system is in trouble: try again later.")
		return true
	}
	if ban != nil {
		q.banned(ban)
		return true
	}
	return false
}

// Submission is a post as its poster wrote it: by the web's form, or on
// a terminal.
type Submission = bbs.ChanSubmission

// Refusal is a post refused: why, as the poster is told, an HTTP status
// for the web, the duplicate's link, or the ban.
type Refusal struct {
	Status int
	Msg    string
	Link   string
	Ban    *store.Ban
}

func (r *Refusal) Error() string { return r.Msg }

func refuse(status int, format string, args ...any) *Refusal {
	return &Refusal{Status: status, Msg: fmt.Sprintf(format, args...)}
}

// StaffOf is what a user is on a board: "admin" (a sysop), "mod" (one of
// its mods), or "".
func StaffOf(u *store.User, b *store.Board) string {
	switch {
	case u == nil:
		return ""
	case u.Sysop:
		return "admin"
	case b != nil && isMod(b, u.Name):
		return "mod"
	}
	return ""
}

// Submit posts a submission, checked as every post is: bans, the board's
// rules, flood control, the comment, the image (taken in, thumbnailed);
// a new thread prunes the board.  A *Refusal says why one is not posted.
func (s *Server) Submit(sub *Submission) (*store.BoardPost, error) {
	if err := s.Init(); err != nil {
		s.B.Log.Printf("chan: %v", err)
		return nil, refuse(http.StatusInternalServerError, "The image boards are in trouble: try again later.")
	}
	b, staff := sub.Board, sub.Staff
	if ban, err := s.B.Store.BanFor(sub.IP, b.Dir); err != nil {
		return nil, refuse(http.StatusInternalServerError, "The system is in trouble: try again later.")
	} else if ban != nil {
		return nil, &Refusal{Status: http.StatusForbidden, Msg: "You are banned: " + banReason(ban), Ban: ban}
	}
	switch {
	case b.Locked && staff == "":
		return nil, refuse(http.StatusForbidden, "This board is locked: only staff post here.")
	case (b.LoginToPost || b.LoginToRead) && sub.User == nil:
		return nil, refuse(http.StatusForbidden, "Only this BBS's users post here: sign in first.")
	}
	var op *store.BoardPost
	if sub.Thread != 0 {
		var err error
		if op, err = s.B.Store.ChanThread(b.ID, sub.Thread); err != nil {
			return nil, refuse(http.StatusNotFound, "That thread is not here: it has been pruned or deleted.")
		}
		if !op.Archived.IsZero() {
			return nil, refuse(http.StatusForbidden, "You cannot reply to this thread anymore: it is archived.")
		}
		if op.Closed && staff == "" {
			return nil, refuse(http.StatusForbidden, "You cannot reply to this thread anymore: it is closed.")
		}
	}

	// flood control: staff are not held to it
	if staff == "" {
		last, lastThread, err := s.B.Store.LastPostBy(sub.IP)
		if err != nil {
			return nil, refuse(http.StatusInternalServerError, "The system is in trouble: try again later.")
		}
		wait := s.S.ReplyWait - time.Since(last)
		if sub.Thread == 0 {
			wait = max(wait, s.S.ThreadWait-time.Since(lastThread))
		}
		if wait > 0 {
			return nil, refuse(http.StatusTooManyRequests, "Flood detected: wait %d seconds before posting again.",
				int(wait.Seconds())+1)
		}
	}

	p := &store.BoardPost{BoardID: b.ID, Thread: sub.Thread, IP: sub.IP, Subject: field(sub.Subject, maxSubject)}
	if !b.ForcedAnon {
		p.Name, p.Trip = splitName(field(sub.Name, maxName+40), s.key)
		p.Name = field(p.Name, maxName)
	}
	email := field(sub.Email, maxEmail)
	if !s.ShowOptions(b) {
		email = "" // no Options line: no sage, no nonoko
	}
	sage := strings.EqualFold(email, "sage")
	if !strings.EqualFold(email, "noko") && !strings.EqualFold(email, "nonoko") {
		p.Email = email
	}
	if sub.User != nil {
		p.UserID = sub.User.ID // marked as a member's post
		if sub.Sign && !b.ForcedAnon {
			// the account's own name: no one else's to type
			p.Account, p.Name, p.Trip = sub.User.Name, "", ""
		}
	}
	if staff != "" && sub.Capcode {
		p.Capcode = staff
	}
	if sub.Password != "" {
		p.Password = s.mac("pw", sub.Password)
	}
	if b.Flags {
		p.Country, p.CountryName = s.country(sub.IP)
	}
	if b.PosterIDs && sub.Thread != 0 {
		p.PosterID = s.posterID(sub.IP, b.Dir, sub.Thread)
	}
	p.Comment = cleanComment(sub.Comment)
	if n := utf8.RuneCountInString(p.Comment); n > s.S.MaxComment {
		return nil, refuse(http.StatusBadRequest, "Your comment is too long: %d characters, at most %d.", n, s.S.MaxComment)
	}
	if strings.Count(p.Comment, "\n") >= maxLines {
		return nil, refuse(http.StatusBadRequest, "Your comment has too many lines: at most %d.", maxLines)
	}

	// the image
	var pic *picture
	if len(sub.Image) > 0 {
		if int64(len(sub.Image)) > s.maxFile(b) {
			return nil, refuse(http.StatusRequestEntityTooLarge, "That file is too large: at most %s.", humanSize(s.maxFile(b)))
		}
		if op != nil && op.Images >= b.ImageLimit {
			return nil, refuse(http.StatusForbidden, "This thread has as many images as it may: reply without one.")
		}
		side := opThumb
		if sub.Thread != 0 {
			side = replyThumb
		}
		var err error
		if pic, err = s.prepareFile(b, sub.Image, side, thumbBG(b)); err != nil {
			return nil, refuse(http.StatusBadRequest, "%s", err.Error())
		}
		if dup, err := s.B.Store.PostByMD5(b.ID, pic.MD5); err == nil {
			t := dup.Thread
			if t == 0 {
				t = dup.No
			}
			return nil, &Refusal{Status: http.StatusConflict, Msg: fmt.Sprintf("Duplicate file: it is here already (No.%d).", dup.No),
				Link: fmt.Sprintf("%s/%s/thread/%d#p%d", Base, b.Dir, t, dup.No)}
		}
		name := filepath.Base(strings.ReplaceAll(sub.FileName, "\\", "/"))
		name = strings.TrimSuffix(name, filepath.Ext(name))
		if p.FileName = field(name, 100); p.FileName == "" || p.FileName == "." {
			p.FileName = map[bool]string{true: "image", false: "file"}[imageTypes[pic.Ext] != ""]
		}
		p.Ext, p.W, p.H, p.TnW, p.TnH, p.MD5 = pic.Ext, pic.W, pic.H, pic.TnW, pic.TnH, pic.MD5
		p.FileSize = int64(len(pic.Data))
		p.Replay, p.DrawMS = pic.Replay, pic.DrawMS
		p.DurationMS, p.PageCount = pic.DurationMS, pic.Pages
		p.Spoiler = sub.Spoiler
	}
	switch {
	case sub.Thread == 0 && pic == nil && !b.TextOnly:
		return nil, refuse(http.StatusBadRequest, "You forgot to upload your image: a thread starts with one.")
	case pic == nil && p.Comment == "":
		return nil, refuse(http.StatusBadRequest, "You wrote nothing: a post needs a comment or an image.")
	}

	// its files first, then the post
	if pic != nil {
		if err := s.keepFiles(b.Dir, p, pic); err != nil {
			s.B.Log.Printf("chan: /%s/: keeping an image: %v", b.Dir, err)
			return nil, refuse(http.StatusInternalServerError, "The image could not be kept: try again later.")
		}
	}
	bump := !sage && (op == nil || op.Replies < b.BumpLimit)
	if err := s.B.Store.AddPost(p, bump); err != nil {
		s.removeFiles(b.Dir, []*store.BoardPost{p})
		if errors.Is(err, store.ErrThreadGone) {
			return nil, refuse(http.StatusNotFound, "That thread is not here: it has been pruned or deleted.")
		}
		if errors.Is(err, store.ErrArchived) {
			return nil, refuse(http.StatusForbidden, "You cannot reply to this thread anymore: it is archived.")
		}
		s.B.Log.Printf("chan: /%s/: posting: %v", b.Dir, err)
		return nil, refuse(http.StatusInternalServerError, "The post could not be made: try again later.")
	}
	if b.PosterIDs && sub.Thread == 0 {
		// a thread's first post: its ID is of the thread it begins
		p.PosterID = s.posterID(sub.IP, b.Dir, p.No)
		if err := s.B.Store.SetChanPosterID(b.ID, p.No, p.PosterID); err != nil {
			s.B.Log.Printf("chan: /%s/%d: its ID: %v", b.Dir, p.No, err)
		}
	}
	if sub.Thread == 0 {
		gone, err := s.B.Store.PruneChanThreads(b.ID, b.Pages*b.PerPage, b.Archive)
		if err != nil {
			s.B.Log.Printf("chan: /%s/: pruning: %v", b.Dir, err)
		}
		s.removeFiles(b.Dir, gone)
		s.expireArchive(b)
	}
	what := "a reply in " + strconv.FormatInt(sub.Thread, 10)
	if sub.Thread == 0 {
		what = "a new thread"
	}
	s.B.Log.Printf("chan: /%s/%d, %s, from %s", b.Dir, p.No, what, sub.IP)
	return p, nil
}

// banReason is a ban's reason, as the banned are told it.
func banReason(b *store.Ban) string {
	if b.Reason == "" {
		return "no reason was given"
	}
	return b.Reason
}

// post takes the web form's post: a new thread, or a reply (resto).
func (s *Server) post(q *req, b *store.Board) {
	q.r.Body = http.MaxBytesReader(q.w, q.r.Body, s.maxFile(b)+256<<10)
	if err := q.r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			q.fail(http.StatusRequestEntityTooLarge, fmt.Sprintf("That file is too large: at most %s.", humanSize(s.maxFile(b))))
			return
		}
		q.fail(http.StatusBadRequest, "That form could not be read.")
		return
	}
	defer q.r.MultipartForm.RemoveAll()
	if !q.checkToken() {
		q.fail(http.StatusForbidden, "That form has expired: go back, reload the page and try again.")
		return
	}
	f := q.r.MultipartForm.Value
	if q.needsCaptcha(b) {
		tok, ans := "", ""
		if v := f["ctok"]; len(v) > 0 {
			tok = v[0]
		}
		if v := f["captcha"]; len(v) > 0 {
			ans = v[0]
		}
		if err := s.CheckCaptcha(tok, ans); err != nil {
			q.fail(http.StatusForbidden, err.Error()+" Go back, and type the letters shown.")
			return
		}
	}
	val := func(k string) string {
		if v := f[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	sub := &Submission{Board: b, Name: val("name"), Email: val("email"), Subject: val("sub"), Comment: val("com"),
		Password: val("pwd"), Spoiler: val("spoiler") != "", Capcode: val("capcode") != "", IP: q.host(),
		Sign: val("sign") != "",
		User: q.user, Staff: q.staff(b)}
	if v := val("resto"); v != "" {
		var err error
		if sub.Thread, err = strconv.ParseInt(v, 10, 64); err != nil || sub.Thread < 1 {
			q.fail(http.StatusBadRequest, "There is no such thread.")
			return
		}
	}
	if fhs := q.r.MultipartForm.File["upfile"]; len(fhs) > 0 && fhs[0].Size > 0 {
		fh := fhs[0]
		if fh.Size > s.maxFile(b) {
			q.fail(http.StatusRequestEntityTooLarge, fmt.Sprintf("That file is too large: at most %s.", humanSize(s.maxFile(b))))
			return
		}
		in, err := fh.Open()
		if err != nil {
			q.fail(http.StatusBadRequest, "That file could not be read.")
			return
		}
		sub.Image, err = io.ReadAll(io.LimitReader(in, s.maxFile(b)+1))
		in.Close()
		if err != nil {
			q.fail(http.StatusBadRequest, "That file could not be read.")
			return
		}
		sub.FileName = fh.Filename
	}
	p, err := s.Submit(sub)
	var no *Refusal
	switch {
	case errors.As(err, &no) && no.Ban != nil:
		q.banned(no.Ban)
		return
	case errors.As(err, &no) && no.Link != "":
		q.render(no.Status, "error.html", "Error", b, map[string]any{"Message": no.Msg, "Link": no.Link,
			"Back": q.r.Referer()})
		return
	case errors.As(err, &no):
		q.fail(no.Status, no.Msg)
		return
	case err != nil:
		q.fail(http.StatusInternalServerError, "The post could not be made: try again later.")
		return
	}
	if sub.Name != "" && !b.ForcedAnon {
		http.SetCookie(q.w, &http.Cookie{Name: nameCookie, Value: field(sub.Name, maxName+40), Path: Base,
			MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	}
	thread := sub.Thread
	if thread == 0 {
		thread = p.No
	}
	if s.ShowOptions(b) && strings.EqualFold(strings.TrimSpace(sub.Email), "nonoko") {
		q.redirect(Base + "/" + b.Dir + "/")
		return
	}
	q.redirect(fmt.Sprintf("%s/%s/thread/%d#p%d", Base, b.Dir, thread, p.No))
}

// keepFiles writes an image and its thumbnail, naming them by the time
// in milliseconds (4chan's tim), the next free one.
func (s *Server) keepFiles(dir string, p *store.BoardPost, pic *picture) error {
	for _, d := range []string{"src", "thumb"} {
		if err := os.MkdirAll(filepath.Join(s.S.Dir, dir, d), 0700); err != nil {
			return err
		}
	}
	tim := time.Now().UnixMilli()
	for {
		f, err := os.OpenFile(s.srcPath(dir, tim, pic.Ext), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if errors.Is(err, os.ErrExist) {
			tim++
			continue
		}
		if err != nil {
			return err
		}
		p.Tim = tim
		_, err = f.Write(pic.Data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.WriteFile(s.thumbPath(dir, tim), pic.Thumb, 0600)
		}
		if err != nil {
			os.Remove(s.srcPath(dir, tim, pic.Ext))
		}
		return err
	}
}

// delete deletes the posts ticked, by their password (staff need none):
// whole posts, or only their files.
func (s *Server) delete(q *req, b *store.Board) {
	q.r.Body = http.MaxBytesReader(q.w, q.r.Body, 64<<10)
	if err := q.r.ParseForm(); err != nil || !q.checkToken() {
		q.fail(http.StatusForbidden, "That form has expired: go back, reload the page and try again.")
		return
	}
	pw := q.r.PostForm.Get("pwd")
	fileOnly := q.r.PostForm.Get("onlyimgdel") != ""
	staff := q.staff(b) != ""
	var nos []int64
	for k, v := range q.r.PostForm {
		// 4chan's form: a box named by each post's number
		if n, err := strconv.ParseInt(k, 10, 64); err == nil && len(v) > 0 && v[0] == "delete" {
			nos = append(nos, n)
		}
	}
	if len(nos) == 0 {
		q.fail(http.StatusBadRequest, "Tick the posts to delete first.")
		return
	}
	if len(nos) > 50 {
		nos = nos[:50]
	}
	var done, wrong, archived int
	for _, n := range nos {
		p, err := s.B.Store.ChanPost(b.ID, n)
		if err != nil {
			continue
		}
		if !staff && (pw == "" || p.Password == "" || p.Password != s.mac("pw", pw)) {
			wrong++
			continue
		}
		if !staff && s.inArchive(b, p) {
			// what is archived stays as it was
			archived++
			continue
		}
		if err := s.deletePost(b, p, fileOnly); err == nil {
			done++
		}
	}
	if archived > 0 && done == 0 {
		q.fail(http.StatusForbidden, "That thread is archived: nothing in it can be deleted.")
		return
	}
	if wrong > 0 && done == 0 {
		q.fail(http.StatusForbidden, "Wrong password: nothing was deleted.")
		return
	}
	back := safeBack(q.r.PostForm.Get("back"))
	if back == Base+"/" {
		back = Base + "/" + b.Dir + "/"
	}
	q.redirect(back)
}

// deletePost deletes a post (a thread's first: the thread) or its file.
func (s *Server) deletePost(b *store.Board, p *store.BoardPost, fileOnly bool) error {
	if fileOnly {
		if !p.HasFile() {
			return nil
		}
		if err := s.B.Store.DeleteChanFile(b.ID, p.No); err != nil {
			return err
		}
		s.removeFiles(b.Dir, []*store.BoardPost{p})
		s.B.Log.Printf("chan: /%s/%d: its file deleted", b.Dir, p.No)
		return nil
	}
	gone, err := s.B.Store.DeleteChanPost(b.ID, p.No)
	if err != nil {
		return err
	}
	s.removeFiles(b.Dir, gone)
	s.B.Log.Printf("chan: /%s/%d deleted (%d posts)", b.Dir, p.No, len(gone))
	return nil
}

// reasons a post can be reported for.
var reportReasons = []string{"It breaks this board's rules", "It is illegal", "It is spam or advertising"}

// report has a post reported to staff.
func (s *Server) report(q *req, b *store.Board) {
	q.r.Body = http.MaxBytesReader(q.w, q.r.Body, 16<<10)
	no, err := strconv.ParseInt(q.r.FormValue("no"), 10, 64)
	if err != nil {
		q.notFound()
		return
	}
	p, err := s.B.Store.ChanPost(b.ID, no)
	if err != nil {
		q.fail(http.StatusNotFound, "That post is not here: it has been deleted.")
		return
	}
	if q.r.Method == http.MethodGet {
		q.render(http.StatusOK, "report.html", "Report /"+b.Dir+"/"+strconv.FormatInt(no, 10), b,
			map[string]any{"Post": p, "Reasons": reportReasons})
		return
	}
	if !q.checkToken() {
		q.fail(http.StatusForbidden, "That form has expired: go back, reload the page and try again.")
		return
	}
	if q.checkBan(b.Dir) {
		return
	}
	reason := q.r.PostForm.Get("reason")
	ok := false
	for _, r := range reportReasons {
		ok = ok || r == reason
	}
	if !ok {
		q.fail(http.StatusBadRequest, "Choose why it should be reported.")
		return
	}
	err = s.B.Store.AddReport(&store.Report{BoardID: b.ID, No: p.No, Reason: reason, IP: q.host()})
	if err != nil && !errors.Is(err, store.ErrExists) {
		q.fail(http.StatusInternalServerError, "The report could not be made: try again later.")
		return
	}
	q.render(http.StatusOK, "error.html", "Reported", b, map[string]any{"Message": "Thank you: staff will see it.",
		"Done": true})
}

// inArchive says whether a post is in an archived thread.
func (s *Server) inArchive(b *store.Board, p *store.BoardPost) bool {
	if p.Thread == 0 {
		return !p.Archived.IsZero()
	}
	op, err := s.B.Store.ChanPost(b.ID, p.Thread)
	return err == nil && !op.Archived.IsZero()
}
