package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/guard"
)

// TestClientIP: a trusted proxy's request is for the last hop before the
// trusted ones, by X-Forwarded-For, Forwarded or X-Real-IP (an address
// with or without a port); with no headers, no one; a bad range is refused.
func TestClientIP(t *testing.T) {
	proxies, err := ParseProxies("127.0.0.1, 10.0.0.0/8 ::1")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{S: Settings{TrustedProxies: proxies}}
	for _, c := range []struct {
		headers map[string]string
		want    string
	}{
		{map[string]string{"X-Forwarded-For": "203.0.113.5"}, "203.0.113.5"},
		// a client's own X-Forwarded-For is not believed: the last hop
		// before our proxies is
		{map[string]string{"X-Forwarded-For": "1.2.3.4, 203.0.113.5, 10.1.2.3"}, "203.0.113.5"},
		{map[string]string{"X-Forwarded-For": "10.0.0.7"}, "10.0.0.7"},
		{map[string]string{"X-Forwarded-For": "garbage, 10.0.0.7"}, "10.0.0.7"},
		{map[string]string{"Forwarded": `for=192.0.2.60;proto=http, for="[2001:db8::1]:4711"`}, "2001:db8::1"},
		{map[string]string{"X-Real-IP": "198.51.100.9"}, "198.51.100.9"},
		{map[string]string{"X-Forwarded-For": "198.51.100.10:5555"}, "198.51.100.10"},
		{map[string]string{}, ""},
	} {
		r, _ := http.NewRequest("GET", "/", nil)
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		got := ""
		if ip := s.clientIP(r); ip != nil {
			got = ip.String()
		}
		if got != c.want {
			t.Errorf("%v: %q, want %q", c.headers, got, c.want)
		}
	}
	if _, err := ParseProxies("10.0.0.0/33"); err == nil {
		t.Error("a bad range taken")
	}
}

// TestBehindProxy: through a trusted proxy each caller has their own
// rate limit, logins are logged from them, and the deny lists reach them;
// without trust, the headers are ignored.
func TestBehindProxy(t *testing.T) {
	get := func(s *site, xff string) int {
		req, _ := http.NewRequest("GET", s.ts.URL+"/api/v1/system", nil)
		req.Header.Set("X-Forwarded-For", xff)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	proxies, _ := ParseProxies("127.0.0.1 ::1")
	s := setup(t, Settings{Rate: 0.001, Burst: 1, TrustedProxies: proxies})
	if a, b := get(s, "203.0.113.5"), get(s, "203.0.113.6"); a != 200 || b != 200 {
		t.Errorf("two callers: %d %d", a, b)
	}
	if c := get(s, "203.0.113.5"); c != 429 {
		t.Errorf("the first again: %d", c)
	}
	req, _ := http.NewRequest("POST", s.ts.URL+"/api/v1/login", strings.NewReader(`{"name":"alice","password":"alicepass1"}`))
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if !strings.Contains(s.logs.String(), "alice logged in from 198.51.100.7") {
		t.Errorf("log:\n%s", s.logs)
	}
	s.b.Guard = &guard.Guard{HackLog: filepath.Join(t.TempDir(), "hack.log"), BanTime: time.Hour}
	s.b.Guard.Ban("192.0.2.66", "test")
	if c := get(s, "192.0.2.66"); c != 403 {
		t.Errorf("a banned caller: %d", c)
	}

	// not trusted: X-Forwarded-For is anyone's to send
	plain := setup(t, Settings{Rate: 0.001, Burst: 1})
	if a, b := get(plain, "203.0.113.5"), get(plain, "203.0.113.6"); a != 200 || b != 429 {
		t.Errorf("untrusted: %d %d", a, b)
	}
}
