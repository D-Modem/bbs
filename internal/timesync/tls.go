package timesync

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

// RootCAs is a client TLS configuration that trusts the certificates in a
// PEM file (a private NTS-KE server's) alone.
func RootCAs(file string) (*tls.Config, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(b) {
		return nil, errors.New(file + ": no certificates")
	}
	return &tls.Config{RootCAs: pool}, nil
}
