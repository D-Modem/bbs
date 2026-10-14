// Package finger answers finger (RFC 1288): who is on the BBS, and what
// there is to know of a user.  It forwards nothing (user@host).
package finger

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/tcpserv"
)

// Settings are the [finger] section's.
type Settings struct {
	Address    string
	Online     bool // an empty query lists who is on
	Users      bool // a name tells of that user
	MaxClients int
	MaxPerIP   int
}

// Server answers finger queries.
type Server struct {
	B *bbs.BBS
	S Settings
}

// Run listens until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.S.Address)
	if err != nil {
		return fmt.Errorf("finger: %w", err)
	}
	return s.Serve(ctx, ln)
}

// Serve answers on ln until ctx is done.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	ts := &tcpserv.Server{Name: "finger", MaxClients: s.S.MaxClients, MaxPerIP: s.S.MaxPerIP,
		Refuse:  func(c net.Conn) bool { return s.B.Refuse("finger", c.RemoteAddr()) },
		Timeout: 30 * time.Second, Log: s.B.Log, Handle: s.handle,
		Busy: func(c net.Conn) { c.Write([]byte("Too busy; try again later.\r\n")) }}
	return ts.Serve(ctx, ln)
}

func (s *Server) handle(c net.Conn) {
	q, err := tcpserv.ReadLine(c, 256)
	if err != nil {
		return
	}
	q = strings.TrimSpace(q)
	// /W asks for more: what there is, it gets anyway
	if strings.HasPrefix(strings.ToUpper(q), "/W") {
		q = strings.TrimSpace(q[2:])
	}
	var out []string
	switch {
	case strings.Contains(q, "@"):
		out = []string{"Finger forwarding is not done here."}
	case q == "":
		out = s.online()
	default:
		out = s.user(q)
	}
	var b strings.Builder
	for _, l := range out {
		b.WriteString(strings.ToValidUTF8(l, "?") + "\r\n")
	}
	c.Write([]byte(b.String()))
}

// online lists the callers on.
func (s *Server) online() []string {
	if !s.S.Online {
		return []string{"Who is on " + s.B.Conf().Name + " is not told by finger."}
	}
	ns := s.B.Nodes()
	on := 0
	for _, n := range ns {
		if n.User != "" {
			on++
		}
	}
	out := []string{fmt.Sprintf("%s: %d %s on", s.B.Conf().Name, on, map[bool]string{true: "caller", false: "callers"}[on == 1]), ""}
	if on == 0 {
		return out[:1]
	}
	out = append(out, fmt.Sprintf("%4s  %-24s %-12s %5s  %s", "Node", "User", "Via", "On", "Doing"))
	for _, n := range ns {
		if n.User == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%4d  %s %s %5s  %s", n.Num, bbs.Fit(n.User, 24),
			bbs.Fit(n.Via, 12), bbs.Since(n.Since), bbs.Trunc(n.Activity, 30)))
	}
	return out
}

// user tells of one user.
func (s *Server) user(name string) []string {
	if !s.S.Users {
		return []string{"Users are not told of by finger here."}
	}
	if utf8.RuneCountInString(name) > bbs.MaxNameLen {
		return []string{"No such user here."}
	}
	u, err := s.B.Store.UserByName(name)
	if err != nil || u.Locked {
		return []string{"No such user here."}
	}
	out := []string{"User:         " + u.Name}
	if u.Location != "" {
		out = append(out, "From:         "+u.Location)
	}
	if u.Sysop {
		out = append(out, "              (the sysop)")
	}
	out = append(out, "Member since: "+u.Created.Format("2006-01-02"))
	on := ""
	for _, n := range s.B.Nodes() {
		if strings.EqualFold(n.User, u.Name) {
			on = fmt.Sprintf("on now, node %d: %s", n.Num, n.Activity)
		}
	}
	switch {
	case on != "":
		out = append(out, "Last call:    "+on)
	case !u.LastLogin.IsZero():
		out = append(out, "Last call:    "+u.LastLogin.Format("2006-01-02 15:04"))
	default:
		out = append(out, "Last call:    never")
	}
	out = append(out, fmt.Sprintf("Calls:        %d", u.Logins), fmt.Sprintf("Posts:        %d", u.Posts))
	return out
}
