package app

import (
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// The image boards (internal/imageboard's, at /chan/ on the web), on the
// terminals: the boards, their threads, the posts with their pictures
// (where the terminal can show them), and posting: replies, and threads
// with an image sent by a transfer protocol.

// maxChanUpload is the most an image sent from a terminal may be (the
// board's own limit is the image boards' to say).
const maxChanUpload = 32 << 20

func (s *Session) hasBoards() bool {
	if s.B.ImageBoards == nil {
		return false
	}
	bs, err := s.B.Store.Boards()
	return err == nil && len(bs) > 0
}

// chanStaff is what the user is on a board: "admin", "mod" or "".
func chanStaff(u *store.User, b *store.Board) string {
	if u.Sysop {
		return "admin"
	}
	for _, m := range strings.FieldsFunc(b.Mods, func(r rune) bool { return r == ',' || r == ' ' }) {
		if strings.EqualFold(m, u.Name) {
			return "mod"
		}
	}
	return ""
}

// imageBoards lists the boards.
func (s *Session) imageBoards() error {
	for {
		s.N.Doing("Image boards")
		bs, err := s.B.Store.Boards()
		if err != nil {
			return err
		}
		t := &view.Table{Title: "Image boards", Empty: "There are no image boards.", Pick: "Board number",
			Columns: []view.Column{{Title: "Board", Width: 8}, {Title: "Title", Width: 24},
				{Title: "Threads", Width: 7, Right: true}, {Title: "About", Wide: true}}}
		for i, b := range bs {
			n, _ := s.B.Store.ChanThreadCount(b.ID)
			about := b.Description
			if !b.Worksafe {
				about = "(NSFW) " + about
			}
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1),
				Cells: []string{"/" + b.Dir + "/", b.Title, strconv.Itoa(n), about}})
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case view.Pick:
			i := c.Row
			if i < 0 {
				// a board by its directory, as well as its number
				for j, b := range bs {
					if strings.EqualFold(strings.Trim(c.Num, "/"), b.Dir) {
						i = j
					}
				}
			}
			if i < 0 || i >= len(bs) {
				s.UI.Say(view.Bad, "No such board.")
				continue
			}
			if err := s.chanBoard(bs[i]); err != nil {
				return err
			}
		}
	}
}

// chanBoard lists a board's threads, the last bumped first.
func (s *Session) chanBoard(b *store.Board) error {
	off := 0
	for {
		s.N.Doing("On /" + b.Dir + "/")
		per := s.UI.PageSize()
		ops, err := s.B.Store.ChanThreads(b.ID, off, per+1)
		if err != nil {
			return err
		}
		more := len(ops) > per
		if more {
			ops = ops[:per]
		}
		total, _ := s.B.Store.ChanThreadCount(b.ID)
		t := &view.Table{Title: "/" + b.Dir + "/ - " + b.Title, Empty: "There are no threads here yet.",
			Note: []view.Line{view.T(view.Dim, fmt.Sprintf("%d threads, the last bumped first", total))},
			Pick: "Thread number", More: more, Less: off > 0,
			Columns: []view.Column{{Title: "No.", Width: 8, Right: true}, {Title: "R", Width: 4, Right: true},
				{Title: "I", Width: 3, Right: true, Wide: true}, {Title: "Subject"},
				{Title: "Bumped", Width: 8, Wide: true}}}
		if b.Description != "" {
			t.Note = append([]view.Line{view.L(b.Description)}, t.Note...)
		}
		if s.mayPost(b, nil) {
			t.Actions = append(t.Actions, view.Action{ID: "new", Label: "New thread", Key: 'N', PF: 5, Short: "New"})
		}
		for _, op := range ops {
			subj := chanTeaser(op)
			if op.Sticky {
				subj = "[Sticky] " + subj
			}
			if op.Closed {
				subj = "[Closed] " + subj
			}
			t.Rows = append(t.Rows, view.Row{Num: strconv.FormatInt(op.No, 10), Hot: op.Sticky,
				Cells: []string{strconv.FormatInt(op.No, 10), strconv.Itoa(op.Replies), strconv.Itoa(op.Images),
					subj, ago(op.Bumped)}})
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case view.More:
			if more {
				off += per
			}
		case view.Less:
			off = max(off-per, 0)
		case "new":
			if _, err := s.chanPost(b, nil, ""); err != nil {
				return err
			}
		case view.Pick:
			var op *store.BoardPost
			if c.Row >= 0 {
				op = ops[c.Row]
			} else if n, err := strconv.ParseInt(strings.TrimLeft(c.Num, ">#"), 10, 64); err == nil {
				op, err = s.B.Store.ChanThread(b.ID, n)
				if err != nil {
					// a reply's number: its thread
					if p, err := s.B.Store.ChanPost(b.ID, n); err == nil {
						op, _ = s.B.Store.ChanThread(b.ID, p.Thread)
					}
				}
			}
			if op == nil {
				s.UI.Say(view.Bad, "No such thread here.")
				continue
			}
			if err := s.chanThread(b, op, 0); err != nil {
				return err
			}
		}
	}
}

// chanTeaser is a thread's subject, or the start of its comment.
func chanTeaser(p *store.BoardPost) string {
	if p.Subject != "" {
		return p.Subject
	}
	t := strings.Join(strings.Fields(p.Comment), " ")
	if t == "" && p.Ext != "" {
		switch p.Ext {
		case ".webm", ".mp4":
			return "(a video)"
		case ".pdf":
			return "(a PDF)"
		}
		return "(an image)"
	}
	if r := []rune(t); len(r) > 60 {
		t = string(r[:60]) + "..."
	}
	return t
}

// ago is how long ago, shortly.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// mayPost says whether the user may post on a board (in a thread: nil, a
// new one).
func (s *Session) mayPost(b *store.Board, op *store.BoardPost) bool {
	staff := chanStaff(s.User, b) != ""
	return !(b.Locked && !staff) && !(op != nil && op.Closed && !staff)
}

// chanThread reads a thread a post at a time, from post i.
func (s *Session) chanThread(b *store.Board, op *store.BoardPost, i int) error {
	for {
		s.N.Doing("Reading /" + b.Dir + "/")
		op2, err := s.B.Store.ChanThread(b.ID, op.No)
		if err != nil {
			s.UI.Say(view.Plain, "That thread is gone: pruned or deleted.")
			return nil
		}
		op = op2
		replies, err := s.B.Store.ChanReplies(b.ID, op.No, 0)
		if err != nil {
			return err
		}
		posts := append([]*store.BoardPost{op}, replies...)
		i = max(0, min(i, len(posts)-1))
		p := posts[i]
		vm := s.chanMessage(b, op, p, posts, i)
		vm.Actions = []view.Action{{ID: "next", Label: "Next", Key: 'N', PF: 11},
			{ID: "prev", Label: "Previous", Key: 'P', PF: 10, Short: "Prev"},
			{ID: "last", Label: "Last", Key: 'L', PF: 9}}
		if s.mayPost(b, op) {
			vm.Actions = append(vm.Actions, view.Action{ID: "reply", Label: "Reply", Key: 'R', PF: 4})
		}
		if p.HasFile() {
			vm.Actions = append(vm.Actions, view.Action{ID: "view", Label: "View the image", Key: 'V', PF: 2, Short: "Image"})
		}
		vm.Default = "next"
		act, err := s.UI.Message(vm)
		if err != nil {
			return err
		}
		switch act {
		case view.Back:
			return nil
		case "next":
			if i == len(posts)-1 {
				s.UI.Say(view.Plain, "That was the last post in the thread.")
				return nil
			}
			i++
		case "prev":
			if i == 0 {
				s.UI.Say(view.Plain, "That is the first post.")
			}
			i--
		case "last":
			i = len(posts) - 1
		case "reply":
			n, err := s.chanPost(b, op, ">>"+strconv.FormatInt(p.No, 10))
			if err != nil {
				return err
			}
			if n > 0 {
				i = len(posts) // the reply, the thread being read again
			}
		case "view":
			if err := s.chanImage(b, p); err != nil {
				return err
			}
		}
	}
}

// chanMessage is a post as a message to read.
func (s *Session) chanMessage(b *store.Board, op, p *store.BoardPost, posts []*store.BoardPost, i int) *view.Message {
	name := p.Name
	if name == "" && p.Capcode == "" {
		name = b.DefaultName
	}
	if p.Trip != "" {
		name += " " + p.Trip
	}
	switch {
	case p.Account != "":
		name = p.Account + " [signed]" // a user's own name, as no one else can post it
	case p.UserID != 0:
		name += " [member]" // posted by a signed-in user
	}
	if p.PosterID != "" {
		name += " (ID: " + p.PosterID + ")"
	}
	if p.Country != "" {
		name += " [" + p.Country + "]"
	}
	if p.Capcode != "" {
		name += " ## " + map[string]string{"admin": "Admin", "mod": "Mod"}[p.Capcode]
	}
	where := fmt.Sprintf("/%s/ No.%d  (%d of %d)", b.Dir, p.No, i+1, len(posts))
	if op.Sticky {
		where += "  Sticky"
	}
	if op.Closed {
		where += "  Closed"
	}
	subject := p.Subject
	if subject == "" && p.Thread == 0 {
		subject = chanTeaser(p)
	}
	to := fmt.Sprintf("Thread No.%d", op.No)
	if p.Thread == 0 {
		to = fmt.Sprintf("/%s/ (%d replies, %d images)", b.Dir, op.Replies, op.Images)
	}
	body := p.Comment
	if p.FileDeleted {
		body = "[File deleted.]\n\n" + body
	}
	// the posts that quote this one, as the web shows them under it
	var back []string
	for _, o := range posts {
		if o.No != p.No && strings.Contains(o.Comment, ">>"+strconv.FormatInt(p.No, 10)) &&
			quotes(o.Comment, p.No) {
			back = append(back, ">>"+strconv.FormatInt(o.No, 10))
		}
	}
	if len(back) > 0 {
		body += "\n\nReplies: " + strings.Join(back, " ")
	}
	if p.Banned {
		body += "\n\n(USER WAS BANNED FOR THIS POST)"
	}
	m := &view.Message{Where: where, From: name, To: to, Subject: subject, Date: p.Created, Body: body}
	if p.HasFile() {
		m.Picture = &view.Picture{Alt: chanFileInfo(p), Load: func(w, h int) (image.Image, error) {
			return s.B.ImageBoards.Picture(b, p, false, w, h)
		}}
	}
	return m
}

// quotes says whether a comment quotes post no (>>no, not >>no5).
func quotes(comment string, no int64) bool {
	q := ">>" + strconv.FormatInt(no, 10)
	for rest := comment; ; {
		i := strings.Index(rest, q)
		if i < 0 {
			return false
		}
		rest = rest[i+len(q):]
		if rest == "" || rest[0] < '0' || rest[0] > '9' {
			return true
		}
	}
}

// chanFileInfo is what a post's file is, in words.
func chanFileInfo(p *store.BoardPost) string {
	size := fmt.Sprintf("%d KB", p.FileSize>>10)
	if p.FileSize >= 1<<20 {
		size = fmt.Sprintf("%.1f MB", float64(p.FileSize)/(1<<20))
	}
	var s string
	switch p.Ext {
	case ".pdf":
		s = fmt.Sprintf("%s%s, a PDF, %s", p.FileName, p.Ext, size)
		if p.PageCount > 0 {
			s += fmt.Sprintf(", %d page%s", p.PageCount, map[bool]string{true: "", false: "s"}[p.PageCount == 1])
		}
	case ".webm", ".mp4":
		d := p.DurationMS / 1000
		s = fmt.Sprintf("%s%s, a video, %dx%d, %d:%02d, %s", p.FileName, p.Ext, p.W, p.H, d/60, d%60, size)
	default:
		s = fmt.Sprintf("%s%s, %dx%d, %s", p.FileName, p.Ext, p.W, p.H, size)
	}
	if p.Spoiler {
		s += ", a spoiler: V to see it"
	}
	if p.Replay {
		s += ", drawn in " + (time.Duration(p.DrawMS/1000) * time.Second).String()
	}
	return s
}

// chanImage shows a post's image as large as the screen allows.
func (s *Session) chanImage(b *store.Board, p *store.BoardPost) error {
	_, err := s.UI.Panel(&view.Panel{Title: fmt.Sprintf("/%s/ No.%d", b.Dir, p.No),
		Picture: &view.Picture{Alt: strings.Replace(chanFileInfo(p), ", a spoiler: V to see it", ", a spoiler", 1),
			Load: func(w, h int) (image.Image, error) { return s.B.ImageBoards.Picture(b, p, true, w, h) }}})
	return err
}

// chanPost writes a post: a reply to op (beginning with quote), or (op
// nil) a new thread.  The post's number (0: none made).
func (s *Session) chanPost(b *store.Board, op *store.BoardPost, quote string) (int64, error) {
	if !s.mayPost(b, op) {
		s.UI.Say(view.Bad, "You may not post there.")
		return 0, nil
	}
	sub := &bbs.ChanSubmission{Board: b, User: s.User, Staff: chanStaff(s.User, b), IP: s.posterAddress()}
	var err error
	if op != nil {
		sub.Thread = op.No
	}
	// signed with the account's name, or a name (and a tripcode)
	if !b.ForcedAnon {
		if sub.Sign, err = s.UI.Confirm("Sign it as "+s.User.Name+"? (otherwise it is marked as a member's)", false); err != nil {
			return 0, err
		}
	}
	name := s.chanName
	if name == "" {
		name = b.DefaultName
	}
	fields := []view.Field{}
	if !b.ForcedAnon && !sub.Sign {
		fields = append(fields, view.Field{Name: "name", Label: "Name", Value: name, Width: 60,
			Prompt: "Name (Name#password for a tripcode)"})
	}
	if s.B.ImageBoards.ShowOptions(b) {
		fields = append(fields, view.Field{Name: "options", Label: "Options", Value: "none", Width: 20,
			Prompt: "Options (sage: do not bump the thread)"})
	}
	where := fmt.Sprintf("/%s/, a new thread", b.Dir)
	if op != nil {
		where = fmt.Sprintf("/%s/, thread No.%d", b.Dir, op.No)
	}
	vals := map[string]string{}
	if len(fields) > 0 {
		var ok bool
		var err2 error
		vals, ok, err2 = s.UI.Form(&view.Form{Title: "Post to " + where, Fields: fields, Submit: "Next"})
		if err2 != nil || !ok {
			return 0, err2
		}
	}
	if v := strings.TrimSpace(vals["name"]); v != "" && v != b.DefaultName {
		sub.Name = v
		s.chanName = v
	}
	if o := strings.TrimSpace(vals["options"]); o != "" && !strings.EqualFold(o, "none") {
		sub.Email = o
	}
	if sub.Staff != "" {
		if sub.Capcode, err = s.UI.Confirm("Post as ## "+map[string]string{"admin": "Admin", "mod": "Mod"}[sub.Staff]+"?", false); err != nil {
			return 0, err
		}
	}

	// an image, by a transfer protocol
	needImage := op == nil && !b.TextOnly
	if s.UI.Transfers() {
		q := "Send an image with it?"
		if needImage {
			q = "A thread starts with an image: send it now?"
		}
		yes, err := s.UI.Confirm(q, needImage)
		if err != nil {
			return 0, err
		}
		if yes {
			data, err := s.UI.ReceiveData(maxChanUpload)
			if err != nil {
				return 0, err
			}
			if len(data) == 0 {
				s.UI.Say(view.Bad, "No image came.")
			} else {
				sub.Image, sub.FileName = data, "upload"
				if sub.Spoiler, err = s.UI.Confirm("Is it a spoiler?", false); err != nil {
					return 0, err
				}
			}
		}
	}
	if needImage && sub.Image == nil {
		if !s.UI.Transfers() {
			s.UI.Say(view.Bad, "A thread here starts with an image, which this terminal cannot send: reply to one instead.")
			return 0, nil
		}
		s.UI.Say(view.Bad, "A thread starts with an image: nothing posted.")
		return 0, nil
	}

	var posted *store.BoardPost
	var body []string
	if quote != "" {
		body = []string{quote}
	}
	sent, err := s.UI.Edit(&view.Draft{Title: "Post to " + where, To: where, NoTo: true, AnySubject: true,
		Body: body, Send: func(_, subject string, lines []string) error {
			sub.Subject, sub.Comment = subject, strings.Join(lines, "\n")
			p, err := s.B.ImageBoards.Submit(sub)
			if err != nil {
				return errors.New(strings.TrimSuffix(err.Error(), "."))
			}
			posted = p
			return nil
		}})
	if err != nil || !sent || posted == nil {
		return 0, err
	}
	s.UI.Say(view.Good, fmt.Sprintf("Posted: /%s/ No.%d.", b.Dir, posted.No))
	return posted.No, nil
}

// posterAddress is whom flood control and bans count the caller as: their
// address, or (a modem's caller, a socket's) their account.
func (s *Session) posterAddress() string {
	if src, ok := strings.CutPrefix(s.N.Caller.Source, "ip:"); ok && src != "" {
		return src
	}
	return "bbs:" + s.User.Name
}
