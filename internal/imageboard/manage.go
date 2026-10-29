package imageboard

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// manage is the staff's pages: reports, a post's moderation, bans.
func (s *Server) manage(q *req, sub string) {
	if q.user == nil {
		http.Redirect(q.w, q.r, Base+"/login?back="+Base+"/manage", http.StatusSeeOther)
		return
	}
	if !q.anyStaff() {
		q.fail(http.StatusForbidden, "That is for the image boards' staff.")
		return
	}
	if q.r.Method == http.MethodPost {
		q.r.Body = http.MaxBytesReader(q.w, q.r.Body, 64<<10)
		if err := q.r.ParseForm(); err != nil || !q.checkToken() {
			q.fail(http.StatusForbidden, "That form has expired: go back, reload the page and try again.")
			return
		}
	} else if q.r.Method != http.MethodGet && q.r.Method != http.MethodHead {
		http.Error(q.w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch sub {
	case "":
		s.reports(q)
	case "post":
		s.modPost(q)
	case "bans":
		s.bans(q)
	default:
		q.notFound()
	}
}

// boardsModded are the boards the user may moderate.
func (q *req) boardsModded() map[int64]*store.Board {
	out := map[int64]*store.Board{}
	bs, _ := q.s.B.Store.Boards()
	for _, b := range bs {
		if q.staff(b) != "" {
			out[b.ID] = b
		}
	}
	return out
}

// reports lists what has been reported on the user's boards.
func (s *Server) reports(q *req) {
	mine := q.boardsModded()
	all, err := s.B.Store.Reports()
	if err != nil {
		q.fail(http.StatusInternalServerError, "The reports could not be read.")
		return
	}
	type item struct {
		*store.Report
		Post  *store.BoardPost
		Count int
	}
	var items []*item
	byPost := map[string]*item{}
	for _, r := range all {
		if mine[r.BoardID] == nil {
			continue
		}
		key := fmt.Sprintf("%d/%d", r.BoardID, r.No)
		if it := byPost[key]; it != nil {
			it.Count++
			continue
		}
		p, err := s.B.Store.ChanPost(r.BoardID, r.No)
		if err != nil {
			continue
		}
		it := &item{Report: r, Post: p, Count: 1}
		byPost[key] = it
		items = append(items, it)
	}
	q.render(http.StatusOK, "manage.html", "Manage", nil, map[string]any{"Reports": items})
}

// modPost is a post as staff see it, and what they can do to it.
func (s *Server) modPost(q *req) {
	b, err := s.B.Store.Board(q.r.FormValue("b"))
	if err != nil || q.staff(b) == "" {
		q.fail(http.StatusForbidden, "That board is not yours to moderate.")
		return
	}
	no, _ := strconv.ParseInt(q.r.FormValue("no"), 10, 64)
	p, err := s.B.Store.ChanPost(b.ID, no)
	if err != nil {
		q.fail(http.StatusNotFound, "That post is not here: it has been deleted.")
		return
	}
	if q.r.Method == http.MethodPost {
		s.modAction(q, b, p)
		return
	}
	l := q.newLinks(b, []*store.BoardPost{p})
	v := q.view(b, p, l, 0, 0)
	byIP, _ := s.B.Store.PostsByIP(p.IP, 50)
	var thread *store.BoardPost
	if p.Thread == 0 {
		thread = p
	}
	q.render(http.StatusOK, "modpost.html", "Manage /"+b.Dir+"/"+strconv.FormatInt(no, 10), b,
		map[string]any{"Post": v, "Thread": thread, "ByIP": byIP, "Range": ipRange(p.IP)})
}

// ipRange is the range an address is banned by with "range": its /24
// (IPv4) or /48 (IPv6), as a prefix.
func ipRange(ip string) string {
	a := net.ParseIP(ip)
	if a == nil {
		return ip
	}
	if v4 := a.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.", v4[0], v4[1], v4[2])
	}
	parts := strings.Split(a.String(), ":")
	if len(parts) >= 3 {
		return strings.Join(parts[:3], ":") + ":"
	}
	return ip
}

// modAction does what staff chose to a post.
func (s *Server) modAction(q *req, b *store.Board, p *store.BoardPost) {
	f := q.r.PostForm
	back := fmt.Sprintf("%s/manage/post?b=%s&no=%d", Base, b.Dir, p.No)
	who := q.user.Name
	switch f.Get("action") {
	case "delete", "deletefile":
		if err := s.deletePost(b, p, f.Get("action") == "deletefile"); err != nil {
			q.fail(http.StatusInternalServerError, "It could not be deleted.")
			return
		}
		s.B.Log.Printf("chan: %s deleted /%s/%d%s", who, b.Dir, p.No, map[bool]string{true: "'s file"}[f.Get("action") == "deletefile"])
		if f.Get("action") == "delete" {
			back = Base + "/manage"
		}
	case "sticky", "unsticky", "close", "open":
		if p.Thread != 0 {
			q.fail(http.StatusBadRequest, "Only a thread's first post is made sticky or closed.")
			return
		}
		sticky, closed := p.Sticky, p.Closed
		switch f.Get("action") {
		case "sticky", "unsticky":
			sticky = f.Get("action") == "sticky"
		default:
			closed = f.Get("action") == "close"
		}
		if err := s.B.Store.SetChanThread(b.ID, p.No, sticky, closed); err != nil {
			q.fail(http.StatusInternalServerError, "It could not be changed.")
			return
		}
		s.B.Log.Printf("chan: %s made /%s/%d %s", who, b.Dir, p.No, f.Get("action"))
	case "ban":
		ban := &store.Ban{IP: p.IP, Reason: field(f.Get("reason"), 200), By: who, Post: p.Comment}
		if f.Get("range") != "" {
			ban.IP = ipRange(p.IP)
		}
		if f.Get("everywhere") == "" || q.staff(nil) != "admin" {
			ban.Board = b.Dir
		}
		days, err := strconv.Atoi(f.Get("days"))
		if err != nil || days < 0 || days > 3650 {
			q.fail(http.StatusBadRequest, "A ban is for 0 (for ever) to 3650 days.")
			return
		}
		if days > 0 {
			ban.Expires = time.Now().Add(time.Duration(days) * 24 * time.Hour)
		}
		if _, err := s.B.Store.AddBan(ban); err != nil {
			q.fail(http.StatusInternalServerError, "The ban could not be made.")
			return
		}
		s.B.Log.Printf("chan: %s banned %s (/%s/%d) for %d days: %s", who, ban.IP, b.Dir, p.No, days, ban.Reason)
		if f.Get("delete") != "" {
			s.deletePost(b, p, false)
			back = Base + "/manage"
		} else if f.Get("public") != "" {
			s.B.Store.MarkChanBanned(b.ID, p.No) // "(USER WAS BANNED FOR THIS POST)"
		}
		s.B.Store.DismissReports(b.ID, p.No)
	case "dismiss":
		s.B.Store.DismissReports(b.ID, p.No)
		back = Base + "/manage"
	case "purge":
		// every post of the address on this board
		ps, _ := s.B.Store.PostsByIP(p.IP, 500)
		n := 0
		for _, o := range ps {
			if o.BoardID == b.ID {
				if s.deletePost(b, o, false) == nil {
					n++
				}
			}
		}
		s.B.Log.Printf("chan: %s deleted %d posts of %s on /%s/", who, n, p.IP, b.Dir)
		back = Base + "/manage"
	default:
		q.fail(http.StatusBadRequest, "There is no such thing to do.")
		return
	}
	q.redirect(back)
}

// bans lists the bans in force, and lifts or adds them.
func (s *Server) bans(q *req) {
	admin := q.staff(nil) == "admin"
	mine := map[string]bool{}
	for _, b := range q.boardsModded() {
		mine[b.Dir] = true
	}
	mayLift := func(b *store.Ban) bool { return admin || b.Board != "" && mine[b.Board] }
	if q.r.Method == http.MethodPost {
		f := q.r.PostForm
		switch {
		case f.Get("lift") != "":
			id, _ := strconv.ParseInt(f.Get("lift"), 10, 64)
			all, _ := s.B.Store.Bans()
			for _, b := range all {
				if b.ID == id && mayLift(b) {
					s.B.Store.DeleteBan(id)
					s.B.Log.Printf("chan: %s lifted the ban on %s", q.user.Name, b.IP)
				}
			}
		case f.Get("ip") != "":
			ip := strings.TrimSpace(f.Get("ip"))
			if net.ParseIP(ip) == nil && !(strings.HasSuffix(ip, ".") || strings.HasSuffix(ip, ":")) {
				q.fail(http.StatusBadRequest, "An address (192.0.2.7), or a range ending in . or : (192.0.2.)")
				return
			}
			board := f.Get("board")
			if !admin && !mine[board] {
				q.fail(http.StatusForbidden, "Ban on a board you moderate.")
				return
			}
			days, err := strconv.Atoi(f.Get("days"))
			if err != nil || days < 0 || days > 3650 {
				q.fail(http.StatusBadRequest, "A ban is for 0 (for ever) to 3650 days.")
				return
			}
			ban := &store.Ban{IP: ip, Board: board, Reason: field(f.Get("reason"), 200), By: q.user.Name}
			if days > 0 {
				ban.Expires = time.Now().Add(time.Duration(days) * 24 * time.Hour)
			}
			if _, err := s.B.Store.AddBan(ban); err != nil {
				q.fail(http.StatusInternalServerError, "The ban could not be made.")
				return
			}
			s.B.Log.Printf("chan: %s banned %s", q.user.Name, ip)
		}
		q.redirect(Base + "/manage/bans")
		return
	}
	all, err := s.B.Store.Bans()
	if err != nil {
		q.fail(http.StatusInternalServerError, "The bans could not be read.")
		return
	}
	type item struct {
		*store.Ban
		CanLift bool
	}
	var items []item
	for _, b := range all {
		if admin || mine[b.Board] {
			items = append(items, item{b, mayLift(b)})
		}
	}
	var dirs []string
	for d := range mine {
		dirs = append(dirs, d)
	}
	q.render(http.StatusOK, "bans.html", "Bans", nil, map[string]any{"Bans": items, "Admin": admin, "Mine": dirs})
}
