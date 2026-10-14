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

// post takes a post: a new thread, or a reply (resto).
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
	if q.checkBan(b.Dir) {
		return
	}
	staff := q.staff(b)
	f := q.r.MultipartForm.Value
	val := func(k string) string {
		if v := f[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	switch {
	case b.Locked && staff == "":
		q.fail(http.StatusForbidden, "This board is locked: only staff post here.")
		return
	case b.LoginToPost && q.user == nil:
		q.fail(http.StatusForbidden, "Only this BBS's users post here: sign in first.")
		return
	}
	var resto int64
	if v := val("resto"); v != "" {
		var err error
		if resto, err = strconv.ParseInt(v, 10, 64); err != nil || resto < 1 {
			q.fail(http.StatusBadRequest, "There is no such thread.")
			return
		}
	}
	var op *store.BoardPost
	if resto != 0 {
		var err error
		if op, err = s.B.Store.ChanThread(b.ID, resto); err != nil {
			q.fail(http.StatusNotFound, "That thread is not here: it has been pruned or deleted.")
			return
		}
		if op.Closed && staff == "" {
			q.fail(http.StatusForbidden, "You cannot reply to this thread anymore: it is closed.")
			return
		}
	}

	// flood control: staff are not held to it
	if staff == "" {
		last, lastThread, err := s.B.Store.LastPostBy(q.host())
		if err != nil {
			q.fail(http.StatusInternalServerError, "The system is in trouble: try again later.")
			return
		}
		wait := s.S.ReplyWait - time.Since(last)
		if resto == 0 {
			wait = max(wait, s.S.ThreadWait-time.Since(lastThread))
		}
		if wait > 0 {
			q.fail(http.StatusTooManyRequests, fmt.Sprintf("Flood detected: wait %d seconds before posting again.",
				int(wait.Seconds())+1))
			return
		}
	}

	typed := val("name")
	p := &store.BoardPost{BoardID: b.ID, Thread: resto, IP: q.host(), Subject: field(val("sub"), maxSubject)}
	if !b.ForcedAnon {
		p.Name, p.Trip = splitName(field(typed, maxName+40), s.key)
		p.Name = field(p.Name, maxName)
	}
	email := field(val("email"), maxEmail)
	sage := strings.EqualFold(email, "sage")
	nonoko := strings.EqualFold(email, "nonoko")
	if !strings.EqualFold(email, "noko") && !nonoko {
		p.Email = email
	}
	if q.user != nil {
		p.UserID = q.user.ID
	}
	if staff != "" && val("capcode") != "" {
		p.Capcode = staff
	}
	if pw := val("pwd"); pw != "" {
		p.Password = s.mac("pw", pw)
	}
	p.Comment = cleanComment(val("com"))
	if n := utf8.RuneCountInString(p.Comment); n > s.S.MaxComment {
		q.fail(http.StatusBadRequest, fmt.Sprintf("Your comment is too long: %d characters, at most %d.", n, s.S.MaxComment))
		return
	}
	if strings.Count(p.Comment, "\n") >= maxLines {
		q.fail(http.StatusBadRequest, fmt.Sprintf("Your comment has too many lines: at most %d.", maxLines))
		return
	}

	// the image
	var pic *picture
	if fhs := q.r.MultipartForm.File["upfile"]; len(fhs) > 0 && fhs[0].Size > 0 {
		fh := fhs[0]
		if fh.Size > s.maxFile(b) {
			q.fail(http.StatusRequestEntityTooLarge, fmt.Sprintf("That file is too large: at most %s.", humanSize(s.maxFile(b))))
			return
		}
		if op != nil && op.Images >= b.ImageLimit {
			q.fail(http.StatusForbidden, "This thread has as many images as it may: reply without one.")
			return
		}
		in, err := fh.Open()
		if err != nil {
			q.fail(http.StatusBadRequest, "That file could not be read.")
			return
		}
		data, err := io.ReadAll(io.LimitReader(in, s.maxFile(b)+1))
		in.Close()
		if err != nil || int64(len(data)) > s.maxFile(b) {
			q.fail(http.StatusBadRequest, "That file could not be read.")
			return
		}
		side := opThumb
		if resto != 0 {
			side = replyThumb
		}
		if pic, err = prepare(data, side, thumbBG(b), s.S.Strip); err != nil {
			q.fail(http.StatusBadRequest, err.Error())
			return
		}
		if dup, err := s.B.Store.PostByMD5(b.ID, pic.MD5); err == nil {
			t := dup.Thread
			if t == 0 {
				t = dup.No
			}
			q.render(http.StatusConflict, "error.html", "Error", b, map[string]any{
				"Message": "Duplicate file: it is here already.",
				"Link":    fmt.Sprintf("%s/%s/thread/%d#p%d", Base, b.Dir, t, dup.No), "Back": q.r.Referer()})
			return
		}
		name := filepath.Base(strings.ReplaceAll(fh.Filename, "\\", "/"))
		name = strings.TrimSuffix(name, filepath.Ext(name))
		if p.FileName = field(name, 100); p.FileName == "" {
			p.FileName = "image"
		}
		p.Ext, p.W, p.H, p.TnW, p.TnH, p.MD5 = pic.Ext, pic.W, pic.H, pic.TnW, pic.TnH, pic.MD5
		p.FileSize = int64(len(pic.Data))
		p.Spoiler = val("spoiler") != ""
	}
	switch {
	case resto == 0 && pic == nil && !b.TextOnly:
		q.fail(http.StatusBadRequest, "You forgot to upload your image: a thread starts with one.")
		return
	case pic == nil && p.Comment == "":
		q.fail(http.StatusBadRequest, "You wrote nothing: a post needs a comment or an image.")
		return
	}

	// its files first, then the post
	if pic != nil {
		if err := s.keepFiles(b.Dir, p, pic); err != nil {
			s.B.Log.Printf("chan: /%s/: keeping an image: %v", b.Dir, err)
			q.fail(http.StatusInternalServerError, "The image could not be kept: try again later.")
			return
		}
	}
	bump := !sage && (op == nil || op.Replies < b.BumpLimit)
	if err := s.B.Store.AddPost(p, bump); err != nil {
		s.removeFiles(b.Dir, []*store.BoardPost{p})
		if errors.Is(err, store.ErrThreadGone) {
			q.fail(http.StatusNotFound, "That thread is not here: it has been pruned or deleted.")
			return
		}
		s.B.Log.Printf("chan: /%s/: posting: %v", b.Dir, err)
		q.fail(http.StatusInternalServerError, "The post could not be made: try again later.")
		return
	}
	if resto == 0 {
		gone, err := s.B.Store.PruneChanThreads(b.ID, b.Pages*b.PerPage)
		if err != nil {
			s.B.Log.Printf("chan: /%s/: pruning: %v", b.Dir, err)
		}
		s.removeFiles(b.Dir, gone)
	}
	what := "a reply in " + strconv.FormatInt(resto, 10)
	if resto == 0 {
		what = "a new thread"
	}
	s.B.Log.Printf("chan: /%s/%d, %s, from %s", b.Dir, p.No, what, q.host())
	if typed != "" && !b.ForcedAnon {
		http.SetCookie(q.w, &http.Cookie{Name: nameCookie, Value: field(typed, maxName+40), Path: Base,
			MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	}
	thread := resto
	if thread == 0 {
		thread = p.No
	}
	if nonoko {
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
	var done, wrong int
	for _, n := range nos {
		p, err := s.B.Store.ChanPost(b.ID, n)
		if err != nil {
			continue
		}
		if !staff && (pw == "" || p.Password == "" || p.Password != s.mac("pw", pw)) {
			wrong++
			continue
		}
		if err := s.deletePost(b, p, fileOnly); err == nil {
			done++
		}
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
