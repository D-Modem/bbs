package email

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/sockets"
)

// TLS modes of a server.
const (
	TLSNo       = "no"       // none: passwords in the clear
	TLSOptional = "optional" // STARTTLS (STLS) offered
	TLSRequired = "required" // STARTTLS before a login, or (SMTP) before mail
	TLSImplicit = "implicit" // TLS from the first byte (993, 995, 465)
)

// ServerSettings are a mail server's section's: [pop3], [imap], [smtp],
// [mx], [lmtp].
type ServerSettings struct {
	Address     string // host:port, or a unix socket's path (LMTP)
	TLS         string
	CertFile    string
	KeyFile     string
	MaxClients  int
	MaxPerIP    int
	IdleTimeout time.Duration
	DNSBL       []string // [mx]: DNS blocklists the sending IP is looked up in
}

// tlsConfig is a server's certificate, if TLS is on.
func tlsConfig(set ServerSettings) (*tls.Config, error) {
	if set.TLS == "" || set.TLS == TLSNo {
		return nil, nil
	}
	cfg, err := certs.Config(set.CertFile, set.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("certificate: %w", err)
	}
	return cfg, nil
}

// isUnix reports whether an address is a unix socket's path.
func isUnix(addr string) bool { return strings.HasPrefix(addr, "/") || strings.HasPrefix(addr, "./") }

// limited is a listener that lets in only those the BBS's guard and the
// server's limits allow, telling the others why (busy: the protocol's
// words), and makes TLS implicit ones TLS.
type limited struct {
	net.Listener
	b       *bbs.BBS
	service string
	set     ServerSettings
	tls     *tls.Config
	busy    func(why string) string

	mu      sync.Mutex
	clients int
	perIP   map[string]int
}

// listen listens as a server's settings say.
func (s *Service) listen(service string, set ServerSettings, tc *tls.Config, busy func(string) string) (*limited, error) {
	network := "tcp"
	if isUnix(set.Address) {
		network = "unix"
	}
	ln, err := sockets.Listen(network, set.Address)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", service, err)
	}
	l := &limited{Listener: ln, b: s.B, service: service, set: set, busy: busy, perIP: map[string]int{}}
	if set.TLS == TLSImplicit {
		l.tls = tc
	}
	return l, nil
}

// hostOf is a TCP address's IP ("" for another kind of address).
func hostOf(a net.Addr) string {
	if t, ok := a.(*net.TCPAddr); ok {
		return t.IP.String()
	}
	return ""
}

// Accept is the next connection let in.
func (l *limited) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return nil, err
		}
		if why := l.admit(c); why != "" {
			go func() {
				c.SetWriteDeadline(time.Now().Add(5 * time.Second))
				io.WriteString(c, l.busy(why))
				c.Close()
			}()
			continue
		}
		cc := &counted{Conn: c, l: l}
		if l.tls != nil {
			return tls.Server(cc, l.tls), nil
		}
		return cc, nil
	}
}

// admit counts a connection in, or is why not.
func (l *limited) admit(c net.Conn) string {
	if l.b.Refuse(l.service, c.RemoteAddr()) {
		return "not here"
	}
	h := hostOf(c.RemoteAddr())
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.set.MaxClients > 0 && l.clients >= l.set.MaxClients {
		return "too many connections; try again later"
	}
	if h != "" && l.set.MaxPerIP > 0 && l.perIP[h] >= l.set.MaxPerIP {
		return "too many connections from your address"
	}
	l.clients++
	if h != "" {
		l.perIP[h]++
	}
	return ""
}

// release counts a connection out.
func (l *limited) release(c net.Conn) {
	h := hostOf(c.RemoteAddr())
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clients--
	if h != "" {
		if l.perIP[h]--; l.perIP[h] <= 0 {
			delete(l.perIP, h)
		}
	}
}

// counted is a connection admitted, counted out when closed.
type counted struct {
	net.Conn
	l    *limited
	once sync.Once
}

func (c *counted) Close() error {
	c.once.Do(func() { c.l.release(c.Conn) })
	return c.Conn.Close()
}

// Addr is where a server listens.
func (x *POP3Server) Addr() net.Addr { return x.ln.Addr() }

// Addr is where a server listens.
func (x *IMAPServer) Addr() net.Addr { return x.ln.Addr() }

// Addr is where a server listens.
func (x *SMTPServer) Addr() net.Addr { return x.ln.Addr() }
