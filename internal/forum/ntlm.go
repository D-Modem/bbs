package forum

import (
	"encoding/base64"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/ntlm"
	"github.com/D-Modem/bbs/internal/store"
)

// Signing in by NTLM, as MSN 2.x's Program Viewer (Internet Explorer 3
// inside) signed in to MSN's pages: its member's password never sent,
// only an answer to a challenge.  The exchange is HTTP's (MS-NTHT): a 401
// asking for NTLM (or MSN, MSN's name for it), the NEGOTIATE in an
// Authorization header, a 401 with the CHALLENGE, then the AUTHENTICATE,
// all on one connection.

// NTLMSessions are Sessions that can sign in by NTLM.
type NTLMSessions interface {
	// LoginNTLM checks an AUTHENTICATE's answer to challenge (throttled
	// and logged as any login) and gives a session's secret.
	LoginNTLM(r *http.Request, a *ntlm.Auth, challenge []byte) (secret string, u *store.User, err error)
}

// ntlmWaits are the exchanges waiting for their AUTHENTICATE, by
// connection (its remote address and port).
type ntlmWaits struct {
	mu sync.Mutex
	m  map[string]ntlmWait
}

type ntlmWait struct {
	x      *ntlm.Exchange
	scheme string
	at     time.Time
}

func (w *ntlmWaits) put(conn string, x ntlmWait) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.m == nil {
		w.m = map[string]ntlmWait{}
	}
	for k, v := range w.m {
		if time.Since(v.at) > time.Minute {
			delete(w.m, k)
		}
	}
	if len(w.m) < 4096 {
		w.m[conn] = x
	}
}

func (w *ntlmWaits) take(conn string) (ntlmWait, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	x, ok := w.m[conn]
	delete(w.m, conn)
	return x, ok && time.Since(x.at) <= time.Minute
}

// ntlmOn reports whether NTLM sign-ins are offered here.
func (s *Server) ntlmOn() bool {
	_, ok := s.Sessions.(NTLMSessions)
	return ok && s.B.NTLMOffered()
}

var ntlmPage = template.Must(template.New("").Parse(`<html><head><title>Sign In</title></head><body>
<p>{{.Text}}</p>
<p><a href="/forum/login?back={{.Back}}">Sign in with your name and password</a></p>
</body></html>`))

// loginNTLM is /forum/login/ntlm: the exchange, then back where the
// caller was, signed in.
func (s *Server) loginNTLM(q *req) {
	back := safeBack(q.r.FormValue("back"))
	ns, ok := s.Sessions.(NTLMSessions)
	if !ok || !s.B.NTLMOffered() {
		q.redirect(Base + "/login?back=" + urlEscape(back))
		return
	}
	if q.user != nil {
		q.redirect(back)
		return
	}
	ask := func(text string, challenge []byte, scheme string) {
		h := q.w.Header()
		if challenge != nil {
			h.Add("WWW-Authenticate", scheme+" "+base64.StdEncoding.EncodeToString(challenge))
		} else {
			h.Add("WWW-Authenticate", "NTLM")
			h.Add("WWW-Authenticate", "MSN")
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		q.w.WriteHeader(http.StatusUnauthorized)
		ntlmPage.Execute(q.w, map[string]string{"Text": text, "Back": back})
	}
	scheme, token, _ := strings.Cut(q.r.Header.Get("Authorization"), " ")
	if !strings.EqualFold(scheme, "NTLM") && !strings.EqualFold(scheme, "MSN") {
		ask("Signing in with your MSN password...", nil, "")
		return
	}
	in, err := base64.StdEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		ask("That sign-in could not be read.", nil, "")
		return
	}
	conn := q.r.RemoteAddr
	switch ntlm.Type(in) {
	case ntlm.TypeNegotiate:
		x := &ntlm.Exchange{Target: "BBS"}
		c, err := x.Challenge(in)
		if err != nil {
			ask("That sign-in could not be read.", nil, "")
			return
		}
		s.ntlm.put(conn, ntlmWait{x, scheme, time.Now()})
		ask("Signing in...", c, scheme)
		return
	case ntlm.TypeAuthenticate:
		wait, ok := s.ntlm.take(conn)
		if !ok {
			ask("That sign-in took too long: try again.", nil, "")
			return
		}
		a, c, err := wait.x.Authenticate(in)
		if err != nil {
			ask("That sign-in could not be read.", nil, "")
			return
		}
		secret, _, err := ns.LoginNTLM(q.r, a, c)
		if err != nil {
			ask(capital(err.Error())+".", nil, "")
			return
		}
		q.setCookie(secret)
		q.redirect(back)
		return
	}
	ask("That sign-in could not be read.", nil, "")
}
