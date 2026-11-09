package ymsg

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/sockets"
)

// The web half: what a Yahoo client asks of Yahoo's web servers, once
// their names are pointed here.
//
//	/capacity (vcs1.msg.yahoo.com): where the pager is, for Y!M 9 and later
//	/config/pwtoken_get, /config/pwtoken_login (login.yahoo.com, HTTPS):
//	    the YMSG 16 login: the password, then the token it gave
//	/ycontent/ (insider.msg.yahoo.com): the chat room list

// serveWeb serves the web half until ctx is done.
func (s *Server) serveWeb(ctx context.Context) error {
	errs := make(chan error, 2)
	var srvs []*http.Server
	start := func(addr string, cfg *tls.Config) error {
		ln, err := sockets.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("ymsg: %w", err)
		}
		if cfg != nil {
			ln = tls.NewListener(ln, cfg)
		}
		hs := &http.Server{Handler: s, ReadHeaderTimeout: 30 * time.Second, WriteTimeout: time.Minute,
			ErrorLog: nil}
		srvs = append(srvs, hs)
		go func() {
			if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs <- fmt.Errorf("ymsg: web: %w", err)
			}
		}()
		return nil
	}
	if s.S.HTTPAddress != "" {
		if err := start(s.S.HTTPAddress, nil); err != nil {
			return err
		}
	}
	if s.S.HTTPSAddress != "" {
		cert, err := tls.LoadX509KeyPair(s.S.CertFile, s.S.KeyFile)
		if err != nil {
			return fmt.Errorf("ymsg: tls_cert, tls_key: %w", err)
		}
		if err := start(s.S.HTTPSAddress, tlsConfig(cert)); err != nil {
			return err
		}
	}
	select {
	case <-ctx.Done():
	case err := <-errs:
		for _, hs := range srvs {
			hs.Close()
		}
		return err
	}
	for _, hs := range srvs {
		hs.Close()
	}
	return nil
}

// tlsConfig takes the old clients: Windows XP's TLS 1.0, with RSA key
// exchange and 3DES or RC4 (all it has), as well as current ones.
func tlsConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS10,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305, tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA, tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_RSA_WITH_RC4_128_SHA,
		}}
}

// ServeHTTP answers the web half.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if s.B.Refuse("ymsg", addr(r.RemoteAddr)) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	switch {
	case r.URL.Path == "/capacity":
		w.Write([]byte("COLO_CAPACITY=1\r\nCS_IP_ADDRESS=" + s.pagerHost(r) + "\r\n"))
	case r.URL.Path == "/config/pwtoken_get":
		s.pwtokenGet(w, r, host)
	case r.URL.Path == "/config/pwtoken_login":
		s.pwtokenLogin(w, r)
	case strings.HasPrefix(r.URL.Path, "/ycontent"):
		s.roomList(w, r)
	default:
		http.NotFound(w, r)
	}
}

// addr is an address as a net.Addr.
type addr string

func (a addr) Network() string { return "tcp" }
func (a addr) String() string  { return string(a) }

// pagerHost is the pager's IP address for a client: Host's, or the one
// it asked on.
func (s *Server) pagerHost(r *http.Request) string {
	h := s.S.Host
	if h == "" {
		if la, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
			h, _, _ = net.SplitHostPort(la.String())
		}
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.String()
	}
	if ips, err := net.LookupIP(h); err == nil {
		for _, ip := range ips {
			if ip.To4() != nil {
				return ip.String()
			}
		}
	}
	return h
}

// body writes a login answer as Yahoo's did: CRLF lines, chunked (one
// library reads the chunk's length line as part of it).
func body(w http.ResponseWriter, lines ...string) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	w.Write([]byte(strings.Join(lines, "\r\n") + "\r\n"))
}

// pwtokenGet checks a YMSG 16 client's password: the token for
// pwtoken_login, or Yahoo's error number.
func (s *Server) pwtokenGet(w http.ResponseWriter, r *http.Request, host string) {
	q := r.URL.Query()
	login, pw := strings.TrimSpace(q.Get("login")), q.Get("passwd")
	if login == "" || pw == "" {
		body(w, "100")
		return
	}
	name := login
	if t := s.resolve(login); t != nil {
		name = t.Name
	}
	u, err := s.B.Authenticate("ymsg", "ip:"+host, name, pw)
	if err == nil {
		err = s.may(u)
	}
	if err != nil {
		switch {
		case errors.Is(err, bbs.ErrThrottled):
			body(w, "1213")
		case errors.Is(err, bbs.ErrLocked), errors.Is(err, bbs.ErrNotAllowed):
			body(w, "1218")
		default:
			body(w, "1212")
		}
		return
	}
	tk := randomString(48)
	s.mu.Lock()
	s.expire()
	s.tokens[tk] = &token{u: u, expires: time.Now().Add(tokenLife)}
	s.mu.Unlock()
	body(w, "0", "ymsgr="+tk, "partnerid="+randomString(16))
}

// pwtokenLogin spends a token: the crumb and cookies the client proves on
// the pager.
func (s *Server) pwtokenLogin(w http.ResponseWriter, r *http.Request) {
	tk := r.URL.Query().Get("token")
	s.mu.Lock()
	s.expire()
	t := s.tokens[tk]
	delete(s.tokens, tk)
	if t != nil {
		t.crumb = randomString(16)
		t.y = "v=1&n=" + randomString(26) + "&l=" + idOf(t.u) + "/o&p=" + randomString(20) + "&r=" + randomString(3) + "&lg=us&intl=us"
		t.t = "z=" + randomString(24) + "&a=" + randomString(12) + "&sk=" + randomString(24) + "&d=" + randomString(30)
		t.expires = time.Now().Add(tokenLife)
		s.cookies[t.t] = t
	}
	s.mu.Unlock()
	if t == nil {
		body(w, "100")
		return
	}
	const tail = "; path=/; domain=.yahoo.com"
	w.Header().Add("Set-Cookie", "Y="+t.y+tail)
	w.Header().Add("Set-Cookie", "T="+t.t+tail)
	w.Header().Add("Set-Cookie", "B="+randomString(20)+"&b=3&s="+randomString(2)+tail)
	body(w, "0", "crumb="+t.crumb, "Y="+t.y+tail, "T="+t.t+tail, "cookievalidfor=86400")
}

// cookie is the login a T cookie is from (nil: none, or expired), spent.
func (s *Server) cookie(v string) *token {
	v = strings.TrimPrefix(strings.TrimSpace(v), "T=")
	if i := strings.IndexByte(v, ';'); i >= 0 {
		v = v[:i]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	t := s.cookies[v]
	delete(s.cookies, v)
	return t
}

// expire forgets old tokens (s.mu held).
func (s *Server) expire() {
	now := time.Now()
	for k, t := range s.tokens {
		if now.After(t.expires) {
			delete(s.tokens, k)
		}
	}
	for k, t := range s.cookies {
		if now.After(t.expires) {
			delete(s.cookies, k)
		}
	}
}

// roomCategory is the one category the room list has.
const roomCategory = "1600000002"

// roomList is the chat room browser's: one category, the BBS's rooms in
// it (those in use, and the permanent ones).
func (s *Server) roomList(w http.ResponseWriter, r *http.Request) {
	type lobby struct {
		Count   int `xml:"count,attr"`
		Users   int `xml:"users,attr"`
		Voices  int `xml:"voices,attr"`
		Webcams int `xml:"webcams,attr"`
	}
	type room struct {
		Type  string `xml:"type,attr"`
		ID    int    `xml:"id,attr"`
		Name  string `xml:"name,attr"`
		Topic string `xml:"topic,attr"`
		Lobby lobby  `xml:"lobby"`
	}
	type category struct {
		ID   string `xml:"id,attr"`
		Name string `xml:"name,attr"`
	}
	type content struct {
		XMLName    xml.Name   `xml:"content"`
		Time       int64      `xml:"time,attr"`
		Categories []category `xml:"chatCategories>category,omitempty"`
		Rooms      []room     `xml:"chatRooms>room,omitempty"`
	}
	c := content{Time: time.Now().Unix()}
	q := r.URL.RawQuery
	if strings.Contains(q, "chatroom_") {
		for i, ri := range s.B.PublicRooms() {
			if strings.HasPrefix(ri.Name, "yc-") {
				continue // conferences
			}
			c.Rooms = append(c.Rooms, room{Type: "yahoo", ID: i + 1, Name: ri.Name, Topic: ri.Topic,
				Lobby: lobby{Count: 1, Users: len(ri.Users)}})
		}
	} else {
		c.Categories = []category{{ID: roomCategory, Name: s.S.System + " chat rooms"}}
	}
	w.Header().Set("Content-Type", "text/xml")
	b, _ := xml.Marshal(c)
	w.Write([]byte(xml.Header))
	w.Write(b)
}
