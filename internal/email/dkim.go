package email

import (
	"bytes"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/emersion/go-msgauth/dkim"
)

// sign is a mail with a DKIM-Signature of this server's key.
func (s *Service) sign(raw []byte) ([]byte, error) {
	k := s.Set.DKIM
	var out bytes.Buffer
	err := dkim.Sign(&out, bytes.NewReader(raw), &dkim.SignOptions{Domain: k.Domain, Selector: k.Selector, Signer: k.Signer,
		HeaderKeys: []string{"From", "To", "Cc", "Subject", "Date", "Message-ID", "In-Reply-To", "References",
			"MIME-Version", "Content-Type", "Content-Transfer-Encoding"}})
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// LoadDKIMKey reads a DKIM private key: PEM, RSA (PKCS #1 or #8) or
// Ed25519 (PKCS #8).
func LoadDKIMKey(path string) (crypto.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%s: no PEM key", path)
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	switch k := k.(type) {
	case *rsa.PrivateKey:
		return k, nil
	case ed25519.PrivateKey:
		return k, nil
	}
	return nil, fmt.Errorf("%s: an RSA or Ed25519 key is needed", path)
}

// DKIMRecord is the DNS TXT record that publishes a key's public half.
func DKIMRecord(k crypto.Signer) (string, error) {
	switch pub := k.Public().(type) {
	case *rsa.PublicKey:
		der, err := x509.MarshalPKIXPublicKey(pub)
		if err != nil {
			return "", err
		}
		return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der), nil
	case ed25519.PublicKey:
		return "v=DKIM1; k=ed25519; p=" + base64.StdEncoding.EncodeToString(pub), nil
	}
	return "", errors.New("an RSA or Ed25519 key is needed")
}

// NewDKIMKey makes a 2048-bit RSA key (what every receiver checks) and
// writes it to path, which must not be there yet.
func NewDKIMKey(path string) (crypto.Signer, error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	if err := pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		f.Close()
		return nil, err
	}
	return k, f.Close()
}
