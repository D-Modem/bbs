package oscar

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// The later AIM logins, over HTTP(S) on the OSCAR port (and the TLS
// port, if there is one):
//
//	POST /                       AIM 6.2 to 7.x's Kerberos-style login: a
//	                             SNAC (0x050C/0x0002) with the password,
//	                             answered by one with the BOS host and cookie
//	POST /auth/clientLogin       the password, for a token (Pidgin, AIM
//	                             Express, the phones' AIM; api.screenname.aol.com)
//	GET  /aim/startOSCARSession  the token, for the BOS host and cookie
//	                             (api.oscar.aol.com)
//
// A client that asks for TLS is sent to tls_host for the BOS; the others
// to the plain one.

// isHTTP: a connection's first byte is an HTTP request's.
func isHTTP(b byte) bool { return b == 'G' || b == 'P' || b == 'H' || b == 'O' }

// web is the HTTP server the sniffed connections are handed to.
type web struct {
	once sync.Once
	ln   *connListener
	hs   *http.Server
}

// connListener is a listener of connections handed to it.
type connListener struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func (l *connListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *connListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *connListener) Addr() net.Addr { return &net.TCPAddr{} }

// heldConn is a connection the HTTP server has; closed tells its handler
// it is done with it.
type heldConn struct {
	net.Conn
	once   sync.Once
	closed chan struct{}
}

func (c *heldConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

// serveHTTP has the web server answer c, until it is done with it.
func (s *Server) serveHTTP(c net.Conn) {
	s.web.once.Do(func() {
		s.web.ln = &connListener{ch: make(chan net.Conn), done: make(chan struct{})}
		s.web.hs = &http.Server{Handler: s.webHandler(), ReadHeaderTimeout: 30 * time.Second,
			WriteTimeout: time.Minute, IdleTimeout: time.Minute}
		go s.web.hs.Serve(s.web.ln)
	})
	h := &heldConn{Conn: c, closed: make(chan struct{})}
	select {
	case s.web.ln.ch <- h:
	case <-s.web.ln.done:
		return
	}
	<-h.closed
}

// stopWeb ends the web server.
func (s *Server) stopWeb() {
	if s.web.hs != nil {
		s.web.ln.Close()
		s.web.hs.Close()
	}
}

// webHandler answers the logins.
func (s *Server) webHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /{$}", s.kerberos)
	mux.HandleFunc("POST /auth/clientLogin", s.clientLogin)
	mux.HandleFunc("GET /aim/startOSCARSession", s.startOSCARSession)
	mux.HandleFunc("POST /aim/startOSCARSession", s.startOSCARSession)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s: AIM and ICQ\n", s.B.Conf().Name)
	})
	return mux
}

// remoteHost is a request's client address, without its port.
func remoteHost(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// tlsHost is where a client that asked for TLS connects (host:port), ""
// if none is set up.
func (s *Server) tlsHost() string {
	if s.S.TLSHost != "" {
		return s.S.TLSHost
	}
	if s.S.TLSAddress == "" {
		return ""
	}
	_, port, _ := net.SplitHostPort(s.S.TLSAddress)
	host := ""
	if s.S.Host != "" {
		host, _, _ = net.SplitHostPort(s.S.Host)
	}
	if host == "" {
		return ""
	}
	return net.JoinHostPort(host, port)
}

// plainHost is where a client that came by HTTP connects in the clear.
func (s *Server) plainHost(r *http.Request) string {
	if s.S.Host != "" {
		return s.S.Host
	}
	if a, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if _, port, _ := net.SplitHostPort(s.S.Address); port != "" {
			if h, _, err := net.SplitHostPort(a.String()); err == nil && h != "" && h != "::" && h != "0.0.0.0" {
				return net.JoinHostPort(h, port)
			}
		}
	}
	host, _, _ := net.SplitHostPort(r.Host)
	if host == "" {
		host = r.Host
	}
	_, port, _ := net.SplitHostPort(s.S.Address)
	return net.JoinHostPort(host, port)
}

// webLogin checks a password sent in the clear, by HTTP; the cookie for
// the BOS (tls: for a client to come by TLS).
func (s *Server) webLogin(host, sn, pw, clientID string, tls bool) (*store.User, []byte, error) {
	u, err := s.passwordLogin(host, strings.TrimSpace(sn), pw)
	if err != nil {
		if errors.Is(err, bbs.ErrBadLogin) && s.resolve(sn) == nil {
			err = errNoUser
		}
		return nil, nil, err
	}
	icq := isUIN(strings.TrimSpace(sn))
	name := u.Name
	if icq {
		name = s.uinOf(u.ID)
	}
	ck := s.newCookie(&cookie{user: u, sn: name, icq: icq, clientID: clientID, tls: tls})
	s.B.Log.Printf("oscar: %s: %s signed on (%s)", host, u.Name, clientID)
	return u, ck, nil
}

// apiResponse is the AIM web API's answer, as XML or JSON.
type apiResponse struct {
	XMLName          xml.Name `json:"-" xml:"response"`
	StatusCode       int      `json:"statusCode" xml:"statusCode"`
	StatusText       string   `json:"statusText" xml:"statusText"`
	StatusDetailCode int      `json:"statusDetailCode,omitempty" xml:"statusDetailCode,omitempty"`
	Data             any      `json:"data,omitempty" xml:"data,omitempty"`
}

// sendAPI writes an answer in the format asked for (f=xml, f=json; the
// default is XML, as libpurple asks for).
func sendAPI(w http.ResponseWriter, r *http.Request, resp apiResponse) {
	if resp.StatusText == "" {
		resp.StatusText = http.StatusText(resp.StatusCode)
	}
	if resp.StatusCode == 200 {
		resp.StatusText = "OK"
	}
	switch f := strings.ToLower(r.FormValue("f")); f {
	case "json", "jsonp":
		b, _ := json.Marshal(map[string]any{"response": resp})
		if cb := r.FormValue("c"); cb != "" && validCallback(cb) {
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprintf(w, "%s(%s);", cb, b)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	default:
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		b, _ := xml.Marshal(resp)
		w.Write([]byte(xml.Header))
		w.Write(b)
	}
}

// validCallback: a JSONP callback's name, nothing more.
func validCallback(s string) bool {
	if len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '$') {
			return false
		}
	}
	return true
}

// clientLoginData is clientLogin's answer.
type clientLoginData struct {
	Token struct {
		A         string `json:"a" xml:"a"`
		ExpiresIn int    `json:"expiresIn" xml:"expiresIn"`
	} `json:"token" xml:"token"`
	LoginID       string `json:"loginId" xml:"loginId"`
	ScreenName    string `json:"screenName" xml:"screenName"`
	SessionSecret string `json:"sessionSecret" xml:"sessionSecret"`
	HostTime      int64  `json:"hostTime" xml:"hostTime"`
}

// clientLogin is POST /auth/clientLogin: s (the screen name), pwd; a token
// for startOSCARSession.
func (s *Server) clientLogin(w http.ResponseWriter, r *http.Request) {
	host := remoteHost(r)
	if s.B.Refuse("oscar", addrOf(r.RemoteAddr)) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}
	sn := r.PostFormValue("s")
	pw := r.PostFormValue("pwd")
	if sn == "" || pw == "" {
		sendAPI(w, r, apiResponse{StatusCode: 400, StatusText: "Missing parameter: s and pwd"})
		return
	}
	clientID := "clientLogin"
	if d := r.PostFormValue("devId"); d != "" {
		clientID += " (" + bbs.Trunc(d, 40) + ")"
	}
	u, ck, err := s.webLogin(host, sn, pw, clientID, false)
	if err != nil {
		switch {
		case errors.Is(err, bbs.ErrThrottled):
			sendAPI(w, r, apiResponse{StatusCode: 430, StatusText: "Rate limited: try again later"})
		case errors.Is(err, bbs.ErrLocked), errors.Is(err, bbs.ErrNotAllowed):
			sendAPI(w, r, apiResponse{StatusCode: 401, StatusDetailCode: 3019, StatusText: "Account suspended"})
		case errors.Is(err, bbs.ErrBadLogin), errors.Is(err, errNoUser):
			sendAPI(w, r, apiResponse{StatusCode: 330, StatusDetailCode: 3011, StatusText: "Incorrect screen name or password"})
		default:
			s.B.Log.Printf("oscar: %s: clientLogin: %v", host, err)
			sendAPI(w, r, apiResponse{StatusCode: 500, StatusText: "Temporarily unavailable"})
		}
		return
	}
	secret := make([]byte, 16)
	rand.Read(secret)
	var d clientLoginData
	d.Token.A = base64.URLEncoding.EncodeToString(ck)
	d.Token.ExpiresIn = int(cookieLife / time.Second)
	d.LoginID, d.ScreenName = sn, u.Name
	d.SessionSecret = base64.StdEncoding.EncodeToString(secret)
	d.HostTime = time.Now().Unix()
	sendAPI(w, r, apiResponse{StatusCode: 200, Data: d})
}

// oscarSessionData is startOSCARSession's answer.
type oscarSessionData struct {
	Host        string `json:"host" xml:"host"`
	Port        int    `json:"port" xml:"port"`
	Cookie      string `json:"cookie" xml:"cookie"`
	TLSCertName string `json:"tlsCertName,omitempty" xml:"tlsCertName,omitempty"`
}

// startOSCARSession is GET /aim/startOSCARSession: a (clientLogin's token),
// useTLS; the BOS host and cookie.  The request's signature (keyed by the
// password and the session secret) is not checked: the token is the
// cookie, which is secret enough.
func (s *Server) startOSCARSession(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimSpace(r.FormValue("a"))
	ck, err := base64.URLEncoding.DecodeString(tok)
	if err != nil {
		ck, err = base64.StdEncoding.DecodeString(tok)
	}
	var k *cookie
	if err == nil {
		k = s.peekCookie(ck)
	}
	if k == nil || k.service != 0 {
		sendAPI(w, r, apiResponse{StatusCode: 401, StatusText: "Authentication required: the token is unknown or expired"})
		return
	}
	useTLS := boolParam(r.FormValue("useTLS"))
	target := s.plainHost(r)
	if useTLS {
		if t := s.tlsHost(); t != "" {
			target = t
			s.markTLS(ck)
		} else {
			useTLS = false // in the clear, as a client that asked for TLS takes when none is named
		}
	}
	h, p, _ := net.SplitHostPort(target)
	port, _ := strconv.Atoi(p)
	d := oscarSessionData{Host: h, Port: port, Cookie: base64.StdEncoding.EncodeToString(ck)}
	if useTLS {
		d.TLSCertName = h
	}
	sendAPI(w, r, apiResponse{StatusCode: 200, Data: d})
}

func boolParam(v string) bool {
	v = strings.ToLower(v)
	return v == "1" || v == "true" || v == "yes"
}

// peekCookie is a cookie, not spent (nil: none such, or expired).
func (s *Server) peekCookie(b []byte) *cookie {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.cookies[string(b)]
	if k == nil || time.Now().After(k.expires) {
		return nil
	}
	return k
}

// markTLS notes that a cookie's client comes by TLS.
func (s *Server) markTLS(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k := s.cookies[string(b)]; k != nil {
		k.tls = true
	}
}

// Kerberos login (AIM 6.2 to 7.x)
const (
	kerbFamily  = 0x050c
	kerbRequest = 0x0002
	kerbOK      = 0x0003
	kerbFail    = 0x0004
	kerbBadAuth = 0x0401
)

// roastKerberos is the Kerberos login's roasting table (its version 4 and
// later).
var roastKerberos = []byte{0x76, 0x91, 0xc5, 0xe7, 0xd0, 0xd9, 0x95, 0xdd, 0x9e, 0x2f, 0xea, 0xd8, 0x6b, 0x21, 0xc2, 0xbc}

// kerbRequestBody is what a Kerberos login request says, of what is used.
type kerbRequestBody struct {
	id       uint32
	sn       string
	password string
}

// parseKerberos reads a Kerberos login request (after its SNAC header).
func parseKerberos(body []byte) (kerbRequestBody, error) {
	r := &rd{p: body}
	var k kerbRequestBody
	k.id = r.u32()
	r.take(16) // the client's address, its COOL version, zero
	r.tlvBlock()
	r.take(8) // locale flags
	r.str16() // country
	r.str16() // language
	r.tlvBlock()
	r.u32()
	r.u8() // auth type
	k.sn = r.str16()
	r.str16() // the service ("im/boss")
	r.u32()
	r.u16()
	meta := r.tlvBlock()
	if r.bad {
		return k, errors.New("a short Kerberos login request")
	}
	v, ok := meta.get(0x0002)
	if !ok {
		return k, errors.New("no ticket request in the Kerberos login")
	}
	t := &rd{p: v}
	t.u32() // marker
	version := t.u16()
	t.u32() // flags
	t.u16()
	pw := t.take(int(t.u16()))
	if t.bad {
		return k, errors.New("a short Kerberos ticket request")
	}
	if version >= 4 {
		k.password = unroast(pw, roastKerberos)
	} else {
		k.password = string(pw)
	}
	return k, nil
}

// kerberos is POST /: AIM 6.2 to 7.x's login.  The answer is a SNAC: the
// login's ticket, with where the BOS is and the cookie, or a refusal.
func (s *Server) kerberos(w http.ResponseWriter, r *http.Request) {
	host := remoteHost(r)
	if s.B.Refuse("oscar", addrOf(r.RemoteAddr)) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}
	body, err := readBody(r, 64<<10)
	if err != nil {
		http.Error(w, "a body too long", http.StatusRequestEntityTooLarge)
		return
	}
	h, rest, err := parseSNAC(body)
	if err != nil || h.fam != kerbFamily || h.sub != kerbRequest {
		http.Error(w, "not a Kerberos login request", http.StatusBadRequest)
		return
	}
	req, err := parseKerberos(rest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tlsTo := s.tlsHost()
	_, ck, err := s.webLogin(host, req.sn, req.password, "AIM 6/7 (Kerberos login)", tlsTo != "")
	var b buf
	sub := uint16(kerbOK)
	if err != nil {
		if !errors.Is(err, bbs.ErrBadLogin) && !errors.Is(err, errNoUser) && !errors.Is(err, bbs.ErrThrottled) &&
			!errors.Is(err, bbs.ErrLocked) && !errors.Is(err, bbs.ErrNotAllowed) {
			s.B.Log.Printf("oscar: %s: Kerberos login: %v", host, err)
		}
		sub = kerbFail
		msg := "Incorrect screen name or password."
		switch {
		case errors.Is(err, bbs.ErrThrottled):
			msg = "Too many tries: wait a while, then try again."
		case errors.Is(err, bbs.ErrLocked), errors.Is(err, bbs.ErrNotAllowed):
			msg = "This account may not use AIM."
		}
		b.u32(req.id)
		b.str16(req.sn)
		b.u16(kerbBadAuth)
		b.str16(msg)
		b.u32(0)
		b.u16(0) // no TLVs
	} else {
		target := tlsTo
		if target == "" {
			target = s.plainHost(r)
		}
		var conn tlvs
		conn.addStr(0x0005, target)
		conn.add(0x0006, ck)
		if tlsTo != "" {
			certName, _, _ := net.SplitHostPort(tlsTo)
			conn.addU16(0x008e, 0x0002) // by TLS
			conn.addStr(0x008d, certName)
		}
		var info buf
		info.u16(1)
		info.raw(conn.block())
		var meta tlvs
		meta.add(0x0003, info)
		now := uint32(time.Now().Unix())
		b.u32(req.id)
		b.u32(now)
		b.u32(0)
		b.str16(req.sn)
		b.str16("AOL")
		b.u16(1) // one ticket: the BOS's
		b.u16(5) // Kerberos 5
		b.str16("")
		b.str16("AOL")
		b.str16("im/boss")
		b.str16("AOL")
		b.str16(req.sn)
		b.u8(0)     // key version
		b.str16("") // no session key
		b.u32(0)
		b.u32(0)
		b.u32(now) // authenticated
		b.u32(now) // valid from
		b.u32(now + 24*3600)
		b.u32(0)
		b.u32(0x60000000)
		b.u32(0x40000000)
		b.u32(0)
		b.raw(meta.block())
		b.u16(0) // no extensions
	}
	var out buf
	out.u16(kerbFamily)
	out.u16(sub)
	out.u16(0)
	out.u32(h.req)
	out.raw(b)
	w.Header().Set("Content-Type", "application/x-snac")
	w.Write(out)
}

// readBody reads a request's body, up to max bytes.
func readBody(r *http.Request, max int64) ([]byte, error) {
	var b bytes.Buffer
	if _, err := b.ReadFrom(io.LimitReader(r.Body, max+1)); err != nil {
		return nil, err
	}
	if int64(b.Len()) > max {
		return nil, errors.New("too long")
	}
	return b.Bytes(), nil
}

// addrOf is a request's remote address as a net.Addr.
func addrOf(s string) net.Addr {
	a, err := net.ResolveTCPAddr("tcp", s)
	if err != nil {
		return &net.TCPAddr{}
	}
	return a
}

// legacyTLS takes old clients' TLS: 1.0, with RSA key exchange and 3DES
// or RC4 (all AIM 6's NSS has), as well as current ones.
func legacyTLS(get func(*tls.ClientHelloInfo) (*tls.Certificate, error)) *tls.Config {
	return &tls.Config{GetCertificate: get, MinVersion: tls.VersionTLS10,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305, tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA, tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_RSA_WITH_RC4_128_SHA,
		}}
}
