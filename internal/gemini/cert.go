package gemini

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

// LoadCert loads the certificate and its key.  Nothing is made here: a
// sysop makes a certificate with bbs gemini cert, saying what is in it.
func LoadCert(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if errors.Is(err, os.ErrNotExist) {
		return tls.Certificate{}, fmt.Errorf("no certificate (%s): make one with bbs gemini cert, or set cert_file and key_file", certFile)
	}
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("certificate: %w", err)
	}
	return cert, nil
}

// CertRequest is what a sysop asks to be in a new certificate.
type CertRequest struct {
	Names []string // host names and addresses; the first is its common name
	Org   string   // organization ("": none)
	Years int      // how long it is good for
}

// Check says what is wrong with a request, if anything.
func (r CertRequest) Check() error {
	if len(r.Names) == 0 {
		return errors.New("a certificate needs a host name")
	}
	for _, n := range r.Names {
		if n == "" || strings.ContainsAny(n, " /:@") && net.ParseIP(n) == nil {
			return fmt.Errorf("%q is not a host name", n)
		}
	}
	if r.Years < 1 || r.Years > 100 {
		return errors.New("years: 1 to 100")
	}
	return nil
}

// MakeCert writes a new self-signed certificate (ECDSA P-256) and its key.
// Gemini clients trust a capsule's certificate the first time they see
// it and warn if it changes, so neither file may be there already.
func MakeCert(certFile, keyFile string, r CertRequest) error {
	if err := r.Check(); err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: r.Names[0]},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(r.Years, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if r.Org != "" {
		tmpl.Subject.Organization = []string{r.Org}
	}
	for _, n := range r.Names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := write(keyFile, "PRIVATE KEY", kb, 0600); err != nil {
		return err
	}
	if err := write(certFile, "CERTIFICATE", der, 0644); err != nil {
		os.Remove(keyFile)
		return err
	}
	return nil
}

// write writes one PEM block to a new file, not left half written.
func write(name, kind string, b []byte, mode os.FileMode) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	err = pem.Encode(f, &pem.Block{Type: kind, Bytes: b})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

// CertInfo describes the certificate for bbs check: whom it is for, until
// when, and its SHA-256 fingerprint (what TOFU clients pin), or that there
// is none yet.
func CertInfo(certFile string) string {
	b, err := os.ReadFile(certFile)
	if errors.Is(err, os.ErrNotExist) {
		return "none yet: make one with bbs gemini cert"
	}
	if err != nil {
		return err.Error()
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return "not a PEM certificate"
	}
	c, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return err.Error()
	}
	names := append([]string{}, c.DNSNames...)
	for _, ip := range c.IPAddresses {
		names = append(names, ip.String())
	}
	if len(names) == 0 {
		names = []string{c.Subject.CommonName}
	}
	return fmt.Sprintf("%s until %s, SHA-256 %s", strings.Join(names, ", "), c.NotAfter.Format("2006-01-02"), Fingerprint(c.Raw))
}

// Fingerprint is a certificate's SHA-256, as clients show it.
func Fingerprint(der []byte) string {
	return fmt.Sprintf("%X", sha256.Sum256(der))
}
