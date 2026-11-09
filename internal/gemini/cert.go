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
	"time"
)

// LoadCert loads the certificate and key; if mk is set and neither file
// is there yet, it first makes them: a self-signed certificate for host,
// good for 50 years.  Gemini clients trust a capsule's certificate on first
// use, so it is made once and kept, never made again behind the sysop's
// back: a changed certificate is what clients warn of.
func LoadCert(certFile, keyFile, host string, mk bool) (tls.Certificate, error) {
	_, cerr := os.Stat(certFile)
	_, kerr := os.Stat(keyFile)
	if mk && errors.Is(cerr, os.ErrNotExist) && errors.Is(kerr, os.ErrNotExist) {
		if err := MakeCert(certFile, keyFile, host); err != nil {
			return tls.Certificate{}, err
		}
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("certificate: %w", err)
	}
	return cert, nil
}

// MakeCert writes a new self-signed certificate for host and its key.
func MakeCert(certFile, keyFile, host string) error {
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
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(50, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
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
// when, and its SHA-256 fingerprint (what TOFU clients pin), or that it
// will be made.
func CertInfo(certFile string, mk bool) string {
	b, err := os.ReadFile(certFile)
	if errors.Is(err, os.ErrNotExist) && mk {
		return "none yet (made at the first start)"
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
	return fmt.Sprintf("%s until %s, SHA-256 %X", c.Subject.CommonName, c.NotAfter.Format("2006-01-02"), sha256.Sum256(c.Raw))
}
