package forum

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/ntlm"
	"github.com/D-Modem/bbs/internal/store"
)

// LoginNTLM checks an NTLM sign-in on the BBS and starts a session.
func (s *sessions) LoginNTLM(r *http.Request, a *ntlm.Auth, challenge []byte) (string, *store.User, error) {
	u, err := s.b.AuthenticateNTLM("forum", "test", a, challenge)
	if err != nil {
		return "", nil, err
	}
	return s.session(u)
}

// TestNTLMLogin: the exchange in headers on one connection, as Internet
// Explorer has it, and a session at its end.
func TestNTLMLogin(t *testing.T) {
	s := setup(t)
	st := s.b.Store
	if _, _, body := s.browser().get("/forum/login"); strings.Contains(body, "/forum/login/ntlm") {
		t.Error("offered while the hashes aren't kept")
	}
	st.SetKeepNTLM(true)
	alice, _ := st.UserByName("alice")
	st.SetPassword(alice.ID, "password1")
	if _, _, body := s.browser().get("/forum/login"); !strings.Contains(body, "/forum/login/ntlm") {
		t.Error("not offered")
	}

	try := func(pw string) (int, string, string) {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar, Transport: &http.Transport{MaxConnsPerHost: 1}}
		do := func(auth string) *http.Response {
			req, _ := http.NewRequest("GET", s.ts.URL+"/forum/login/ntlm?back=/forum/online", nil)
			if auth != "" {
				req.Header.Set("Authorization", auth)
			}
			resp, err := c.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return resp
		}
		r := do("")
		if r.StatusCode != 401 || strings.Join(r.Header.Values("WWW-Authenticate"), ",") != "NTLM,MSN" {
			t.Fatalf("asked: %d %v", r.StatusCode, r.Header.Values("WWW-Authenticate"))
		}
		r = do("MSN " + base64.StdEncoding.EncodeToString(ntlm.ClientNegotiate(ntlm.FlagUnicode|ntlm.FlagNTLM)))
		scheme, tok, _ := strings.Cut(r.Header.Get("WWW-Authenticate"), " ")
		ch, err := base64.StdEncoding.DecodeString(tok)
		if r.StatusCode != 401 || scheme != "MSN" || err != nil || ntlm.Type(ch) != ntlm.TypeChallenge {
			t.Fatalf("challenge: %d %q", r.StatusCode, r.Header.Get("WWW-Authenticate"))
		}
		r = do("MSN " + base64.StdEncoding.EncodeToString(ntlm.ClientAuthenticate(ntlm.FlagUnicode|ntlm.FlagNTLM, "", "alice", nil, ntlm.DESL(ntlm.NTHash(pw), ch[24:32]))))
		var name string
		for _, ck := range jar.Cookies(r.Request.URL) {
			if ck.Name == cookieName && ck.Value != "" {
				name = "signed in"
			}
		}
		return r.StatusCode, r.Request.URL.Path, name
	}
	if code, _, in := try("wrong"); code != 401 || in != "" {
		t.Errorf("a wrong password: %d %q", code, in)
	}
	if code, at, in := try("password1"); code != 200 || at != "/forum/online" || in == "" {
		t.Errorf("signed in: %d %s %q", code, at, in)
	}
}
