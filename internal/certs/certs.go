// Package certs is where every TLS server of the BBS gets its certificate:
// a certificate and key file of the sysop's, read again when the files
// change (a renewal by certbot or acme.sh), or one the BBS's own ACME
// client keeps ([acme], internal/acme).
//
// A server's cert_file names which:
//
//	/etc/bbs/bbs.crt   that file, with key_file beside it
//	acme               the ACME certificate for the name the client asks
//	                   for (SNI), or the first one
//	acme:NAME          the [acme NAME] certificate
//
// The server configuration fills in "acme" for a TLS server given no
// files when [acme] is set (Default).
package certs

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// ACME is cert_file's value for the ACME client's certificates.
const ACME = "acme"

// ALPNProto is TLS-ALPN-01's protocol (RFC 8737).
const ALPNProto = "acme-tls/1"

// IsACME reports whether a cert_file is the ACME client's.
func IsACME(certFile string) bool {
	return certFile == ACME || strings.HasPrefix(certFile, ACME+":")
}

// Name is the [acme NAME] a cert_file of the ACME client's names ("" for
// "acme": the one for the name asked for, or the first).
func Name(certFile string) string {
	_, n, _ := strings.Cut(certFile, ":")
	return n
}

// Manager is the ACME client, as the TLS servers see it.
type Manager interface {
	// Certificate is the certificate called name ("": the one for
	// hello's server name, or the first).
	Certificate(name string, hello *tls.ClientHelloInfo) (*tls.Certificate, error)
	// Challenge is the TLS-ALPN-01 certificate for a name being
	// validated now, or nil.
	Challenge(serverName string) *tls.Certificate
	// Has reports whether there is a certificate called name.
	Has(name string) bool
}

var (
	mu  sync.RWMutex
	mgr Manager
)

// SetManager makes m the ACME client the servers use (nil: none).
func SetManager(m Manager) {
	mu.Lock()
	mgr = m
	mu.Unlock()
}

func manager() Manager {
	mu.RLock()
	defer mu.RUnlock()
	return mgr
}

// Default is the cert_file and key_file a TLS server uses: those given, or
// the ACME client's when neither is and acme is on.
func Default(certFile, keyFile string, acme bool) (string, string) {
	if certFile == "" && keyFile == "" && acme {
		return ACME, ""
	}
	return certFile, keyFile
}

// Check tells whether a cert_file and key_file will do: both given and
// loading, or the ACME client's (acmeNames: the [acme NAME]s there are,
// nil: no [acme]).
func Check(certFile, keyFile string, acmeNames []string) error {
	if IsACME(certFile) {
		if acmeNames == nil {
			return errors.New("cert_file = acme needs an [acme] section")
		}
		if n := Name(certFile); n != "" && !slices.Contains(acmeNames, n) {
			return fmt.Errorf("cert_file = %s: there is no [acme %s]", certFile, n)
		}
		return nil
	}
	if certFile == "" || keyFile == "" {
		return errors.New("needs cert_file and key_file (or an [acme] section)")
	}
	_, err := tls.LoadX509KeyPair(certFile, keyFile)
	return err
}

// Source is a server's certificate, as GetCertificate wants it.
type Source struct {
	CertFile, KeyFile string

	mu      sync.Mutex
	cert    *tls.Certificate
	mod     time.Time
	checked time.Time
}

// New is the certificate named by cert_file and key_file, loaded now if
// they are files.
func New(certFile, keyFile string) (*Source, error) {
	s := &Source{CertFile: certFile, KeyFile: keyFile}
	if IsACME(certFile) {
		if manager() == nil {
			return nil, errors.New("cert_file = acme, but the ACME client is not running")
		}
		return s, nil
	}
	if _, err := s.file(); err != nil {
		return nil, err
	}
	return s, nil
}

// GetCertificate is the certificate for hello.
func (s *Source) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if IsACME(s.CertFile) {
		m := manager()
		if m == nil {
			return nil, errors.New("no ACME client")
		}
		return m.Certificate(Name(s.CertFile), hello)
	}
	return s.file()
}

// file is the certificate from the files, read again when cert_file has
// changed (looked at no more than every few seconds); the last good one
// stays while a new one does not load.
func (s *Source) file() (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.cert != nil && now.Sub(s.checked) < 5*time.Second {
		return s.cert, nil
	}
	s.checked = now
	fi, err := os.Stat(s.CertFile)
	if err == nil && s.cert != nil && fi.ModTime().Equal(s.mod) {
		return s.cert, nil
	}
	c, err := tls.LoadX509KeyPair(s.CertFile, s.KeyFile)
	if err != nil {
		if s.cert != nil {
			return s.cert, nil
		}
		return nil, err
	}
	s.cert = &c
	if fi != nil {
		s.mod = fi.ModTime()
	}
	return s.cert, nil
}

// Config is TLS 1.2 and later with the certificate from cert_file and
// key_file; it also answers TLS-ALPN-01 challenges for the ACME client.
// The caller may change the rest (MinVersion, CipherSuites, NextProtos).
func Config(certFile, keyFile string) (*tls.Config, error) {
	s, err := New(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return s.Config(), nil
}

// Config is s as a TLS configuration (see the function Config).
func (s *Source) Config() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: s.GetCertificate,
		GetConfigForClient: challengeConfig}
}

// challengeConfig answers a CA's TLS-ALPN-01 validation: a client asking
// only for acme-tls/1, for a name being validated now.
func challengeConfig(hello *tls.ClientHelloInfo) (*tls.Config, error) {
	if !slices.Contains(hello.SupportedProtos, ALPNProto) {
		return nil, nil
	}
	m := manager()
	if m == nil {
		return nil, nil
	}
	c := m.Challenge(hello.ServerName)
	if c == nil {
		return nil, nil
	}
	return &tls.Config{Certificates: []tls.Certificate{*c}, NextProtos: []string{ALPNProto},
		MinVersion: tls.VersionTLS12}, nil
}

// ChallengePath is where HTTP-01 is answered (RFC 8555 8.3).
const ChallengePath = "/.well-known/acme-challenge/"

// WrapHTTP answers the ACME client's HTTP-01 challenges on a web server of
// the BBS's, before next sees the request.
func WrapHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, ChallengePath) {
			if h, ok := manager().(http.Handler); ok {
				h.ServeHTTP(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Legacy is a TLS configuration that takes the old clients the chat's
// front ends serve (Yahoo! and MSN Messenger on Windows XP): TLS 1.0, with
// RSA key exchange and 3DES or RC4 (all XP has), as well as current ones.
func Legacy(get func(*tls.ClientHelloInfo) (*tls.Certificate, error)) *tls.Config {
	return &tls.Config{GetCertificate: get, MinVersion: tls.VersionTLS10,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305, tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA, tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_RSA_WITH_RC4_128_SHA,
		}}
}
