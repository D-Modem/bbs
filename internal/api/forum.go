package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/forum"
	"github.com/Neo-Desktop/bbs-sec/internal/imageboard"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// The web forum's logins are the API's sessions: a browser's cookie holds
// a session token, as a program's Authorization header does.

type forumSessions struct{ s *Server }

func (f forumSessions) Login(r *http.Request, name, password string) (string, *store.User, error) {
	s := f.s
	u, err := s.B.Authenticate("forum", "ip:"+hostOf(r), name, password)
	if err != nil {
		return "", nil, err
	}
	scopes := "read post mail chat files"
	secret, hash := NewToken()
	if _, err := s.B.Store.AddToken(&store.Token{UserID: u.ID, Name: "forum login from " + hostOf(r), Hash: hash,
		Scopes: scopes, Session: true, Expires: time.Now().Add(s.S.SessionTTL)}); err != nil {
		return "", nil, errors.New("the system is in trouble; try again later")
	}
	s.B.Store.LoggedIn(u.ID)
	s.B.Stats.Logins.Add(1)
	s.B.Log.Printf("forum: %s signed in from %s", u.Name, hostOf(r))
	return secret, u, nil
}

func (f forumSessions) User(secret string) *store.User {
	w, err := f.s.byToken(secret)
	if err != nil || w == nil || !w.token.Session {
		return nil // an API key is no browser's cookie
	}
	return w.user
}

func (f forumSessions) Logout(secret string) {
	if w, err := f.s.byToken(secret); err == nil && w != nil && w.token.Session {
		f.s.B.Store.RevokeToken(w.token.ID, w.user.ID)
	}
}

// forum is the web forum's handler, its requests counted against the
// caller's address as the API's are.
func (s *Server) forum() (http.Handler, error) {
	f := &forum.Server{B: s.B, Sessions: forumSessions{s}, Guests: s.S.Guests, Secure: s.S.SecureCookies,
		Terminal: s.terminalOn(), TTL: s.S.SessionTTL, Theme: s.S.Theme}
	h, err := f.Handler()
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allow("ip:" + hostOf(r)) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests: slow down", http.StatusTooManyRequests)
			return
		}
		h.ServeHTTP(w, r)
	}), nil
}

// imageboard is the image boards' handler, its pages counted as the
// forum's are (not their images: a thread can have a hundred); their
// logins are the forum's sessions.
func (s *Server) imageboard() (http.Handler, error) {
	ib := s.Chan
	if ib == nil {
		ib = &imageboard.Server{B: s.B, S: s.S.ChanSettings, Secure: s.S.SecureCookies, TTL: s.S.SessionTTL}
	}
	ib.Sessions = forumSessions{s}
	h, err := ib.Handler()
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !imageboard.IsAsset(r) && !s.allow("ip:"+hostOf(r)) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests: slow down", http.StatusTooManyRequests)
			return
		}
		h.ServeHTTP(w, r)
	}), nil
}
