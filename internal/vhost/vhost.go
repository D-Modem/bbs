// Package vhost lets the chat's front ends share a port by the name a
// client asked for.  With the BBS's DNS server answering for the old
// services' names, MSN Messenger's Passport (login.live.com ...), Yahoo!
// Messenger's login (login.yahoo.com) and AIM's later logins
// (api.screenname.aol.com, kdc.uas.aol.com) all come to port 443 of one
// address, and Yahoo!'s web half and others to port 80.  A Port takes the
// connections there and hands each to its front end: by TLS's server name
// (SNI) for the certificate, then by the request's Host (or, for a client
// that came by address, its path) for an HTTP request, and to the front
// end that takes other connections (OSCAR over TLS) for the rest.
//
// Windows XP's clients send no server name, so they all get the first
// front end's certificate: give the front ends sharing a TLS port one
// certificate with all their names.
package vhost

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/oldtls"
	"github.com/D-Modem/bbs/internal/tcpserv"
)

// Route is one front end's part of a shared port.
type Route struct {
	Name  string   // for the log
	Names []string // the host names it answers (lower case; "*.domain": every name under it)
	// Paths are the paths (prefixes, or exact with a final "$"; in any
	// case) it
	// serves, for a request whose Host is none of the Names (a client
	// that was given the BBS's address)
	Paths []string
	Cert  func(*tls.ClientHelloInfo) (*tls.Certificate, error) // its certificate, on a TLS port
	HTTP  http.Handler                                         // its web requests
	Conn  func(net.Conn)                                       // its other connections (nil: none)
	SSLv2 bool                                                 // it takes SSL 2.0-format hellos (AIM 6.2 to 7.5)
}

// Port is a shared port.
type Port struct {
	Address    string
	TLS        bool
	Routes     []*Route
	MaxClients int
	MaxPerIP   int
	Log        *log.Logger
	Refuse     func(net.Conn) bool // the sysop's deny list and bans (nil: none)

	once sync.Once
	ln   *connListener
	hs   *http.Server
}

// Serve answers on ln until ctx is done.
func (p *Port) Serve(ctx context.Context, ln net.Listener) error {
	if p.Log == nil {
		p.Log = log.New(io.Discard, "", 0)
	}
	p.once.Do(p.init)
	go p.hs.Serve(p.ln)
	defer func() {
		p.ln.Close()
		p.hs.Close()
	}()
	ts := &tcpserv.Server{Name: "shared " + p.Address, MaxClients: p.MaxClients, MaxPerIP: p.MaxPerIP,
		Log: p.Log, Refuse: p.Refuse, Handle: p.Handle}
	return ts.Serve(ctx, ln)
}

func (p *Port) init() {
	p.ln = &connListener{ch: make(chan net.Conn), done: make(chan struct{})}
	p.hs = &http.Server{Handler: certs.WrapHTTP(p), ReadHeaderTimeout: 30 * time.Second,
		WriteTimeout: time.Minute, IdleTimeout: time.Minute,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if h, ok := c.(*heldConn); ok && h.tls != nil {
				return context.WithValue(ctx, tlsKey{}, h.tls)
			}
			return ctx
		}}
}

type tlsKey struct{}

// Handle serves one connection on the port, until it is done.
func (p *Port) Handle(c net.Conn) {
	p.once.Do(p.init)
	c.SetDeadline(time.Now().Add(30 * time.Second))
	var state *tls.ConnectionState
	if p.TLS {
		r := bufio.NewReader(c)
		first, err := r.Peek(3)
		if err != nil {
			return
		}
		var tc interface {
			net.Conn
			Handshake() error
		}
		if oldtls.IsSSLv2Hello(first) {
			rt := p.sslv2()
			if rt == nil {
				p.logf("%s: an SSL 2.0-format hello, which nothing here takes", c.RemoteAddr())
				return
			}
			tc = oldtls.Server(&peeked{c, r}, &oldtls.Config{GetCertificate: rt.Cert})
		} else {
			tc = tls.Server(&peeked{c, r}, legacyTLS(p.certificate))
		}
		if err := tc.Handshake(); err != nil {
			p.logf("%s: TLS: %v", c.RemoteAddr(), err)
			return
		}
		if t, ok := tc.(*tls.Conn); ok {
			st := t.ConnectionState()
			state = &st
		}
		c = tc
	}
	// an HTTP request, or (with a front end for them) something else
	r := bufio.NewReaderSize(c, 4096)
	other := p.other()
	wait := 30 * time.Second
	if other != nil {
		wait = time.Second // OSCAR's server speaks first
	}
	c.SetReadDeadline(time.Now().Add(wait))
	first, err := r.Peek(1)
	c.SetDeadline(time.Time{})
	var ne net.Error
	switch {
	case err == nil && isHTTP(first[0]):
		h := &heldConn{Conn: &peeked{c, r}, tls: state, closed: make(chan struct{})}
		select {
		case p.ln.ch <- h:
		case <-p.ln.done:
			c.Close()
			return
		}
		<-h.closed
	case other != nil && (err == nil || errors.As(err, &ne) && ne.Timeout()):
		other.Conn(&peeked{c, r})
	}
}

// ServeHTTP hands a request to its front end.
func (p *Port) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		if st, ok := r.Context().Value(tlsKey{}).(*tls.ConnectionState); ok {
			r.TLS = st
		}
	}
	rt := p.route(r.Host, r.URL.Path)
	if rt == nil || rt.HTTP == nil {
		http.NotFound(w, r)
		return
	}
	rt.HTTP.ServeHTTP(w, r)
}

// route is the front end for a request to host for path: by name, then
// by path, then the first with a web server.
func (p *Port) route(host, path string) *Route {
	if rt := p.byName(host); rt != nil && rt.HTTP != nil {
		return rt
	}
	path = strings.ToLower(path)
	for _, rt := range p.Routes {
		if rt.HTTP == nil {
			continue
		}
		for _, pre := range rt.Paths {
			pre = strings.ToLower(pre)
			if exact, ok := strings.CutSuffix(pre, "$"); ok {
				if path == exact {
					return rt
				}
			} else if strings.HasPrefix(path, pre) {
				return rt
			}
		}
	}
	for _, rt := range p.Routes {
		if rt.HTTP != nil {
			return rt
		}
	}
	return nil
}

// byName is the front end whose name host is (nil: none).
func (p *Port) byName(host string) *Route {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return nil
	}
	for _, rt := range p.Routes {
		for _, n := range rt.Names {
			if n == host || strings.HasPrefix(n, "*.") && strings.HasSuffix(host, n[1:]) {
				return rt
			}
		}
	}
	return nil
}

// certificate is the certificate for a TLS hello: its server name's
// front end's, or with none (Windows XP), the first front end's.
func (p *Port) certificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if rt := p.byName(hello.ServerName); rt != nil && rt.Cert != nil {
		return rt.Cert(hello)
	}
	for _, rt := range p.Routes {
		if rt.Cert != nil {
			return rt.Cert(hello)
		}
	}
	return nil, errors.New("no certificate")
}

func (p *Port) sslv2() *Route {
	for _, rt := range p.Routes {
		if rt.SSLv2 && rt.Cert != nil {
			return rt
		}
	}
	return nil
}

func (p *Port) other() *Route {
	for _, rt := range p.Routes {
		if rt.Conn != nil {
			return rt
		}
	}
	return nil
}

func (p *Port) logf(format string, args ...any) {
	if p.Log != nil {
		p.Log.Printf("shared "+p.Address+": "+format, args...)
	}
}

// legacyTLS takes the old clients the front ends serve (TLS 1.0, RSA key
// exchange, 3DES and RC4: Windows XP's and AIM 6's) as well as current ones.
func legacyTLS(get func(*tls.ClientHelloInfo) (*tls.Certificate, error)) *tls.Config {
	cfg := certs.Legacy(get)
	cfg.CipherSuites = append(cfg.CipherSuites,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA)
	return cfg
}

// isHTTP: b begins an HTTP request (GET, POST, HEAD, OPTIONS, PUT ...).
func isHTTP(b byte) bool { return b >= 'A' && b <= 'Z' }

// peeked is a connection whose first bytes were read into r.
type peeked struct {
	net.Conn
	r *bufio.Reader
}

func (p *peeked) Read(b []byte) (int, error) { return p.r.Read(b) }

// heldConn tells when the web server is done with a connection.
type heldConn struct {
	net.Conn
	tls    *tls.ConnectionState
	once   sync.Once
	closed chan struct{}
}

func (c *heldConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
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

// claimed are the addresses a Port serves, which the front ends leave
// alone.
var (
	mu      sync.Mutex
	claimed = map[string]bool{}
)

// Claim marks an address as a shared Port's.
func Claim(addr string) {
	mu.Lock()
	defer mu.Unlock()
	claimed[Normal(addr)] = true
}

// Claimed: addr is a shared Port's, so a front end does not listen there
// itself.
func Claimed(addr string) bool {
	mu.Lock()
	defer mu.Unlock()
	return claimed[Normal(addr)]
}

// Normal is an address with every-address hosts (0.0.0.0, ::) written as
// none, so ":443" and "0.0.0.0:443" are the same port.
func Normal(addr string) string {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if h == "0.0.0.0" || h == "::" {
		h = ""
	}
	return net.JoinHostPort(h, p)
}
