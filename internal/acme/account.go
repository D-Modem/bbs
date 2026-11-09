package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

// user is the ACME account, one for each CA: accounts/HOST/ beside the
// certificates.
type user struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration"`
	key          crypto.PrivateKey
	dir          string
}

func (u *user) GetEmail() string                        { return u.Email }
func (u *user) GetRegistration() *registration.Resource { return u.Registration }
func (u *user) GetPrivateKey() crypto.PrivateKey        { return u.key }

// accountDir is the CA's account's directory.
func (m *Manager) accountDir() string {
	host := "ca"
	if u, err := url.Parse(m.S.CA); err == nil && u.Host != "" {
		host = strings.ReplaceAll(u.Host, ":", "_")
	}
	return filepath.Join(m.S.Dir, "accounts", host)
}

// account is the CA's account as kept, or a new key for one.
func (m *Manager) account(ctx context.Context) (*user, error) {
	dir := m.accountDir()
	u := &user{Email: m.S.Email, dir: dir}
	keyFile := filepath.Join(dir, "account.key")
	if b, err := os.ReadFile(keyFile); err == nil {
		blk, _ := pem.Decode(b)
		if blk == nil {
			return nil, fmt.Errorf("%s: not PEM", keyFile)
		}
		k, err := x509.ParseECPrivateKey(blk.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", keyFile, err)
		}
		u.key = k
	} else {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		if err := writeFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
			return nil, err
		}
		u.key = k
	}
	if b, err := os.ReadFile(filepath.Join(dir, "account.json")); err == nil {
		var kept user
		if err := json.Unmarshal(b, &kept); err != nil {
			return nil, fmt.Errorf("%s/account.json: %w", dir, err)
		}
		if kept.Email == u.Email {
			u.Registration = kept.Registration
		}
	}
	return u, nil
}

// register makes (or finds again, by its key) the account at the CA,
// agreeing to its terms, and keeps it.
func (m *Manager) register(client *lego.Client, u *user) error {
	var (
		reg *registration.Resource
		err error
	)
	kid, hmac := m.S.EABKeyID, m.S.EABHMAC
	if kid == "" && client.GetExternalAccountRequired() && m.S.CA == CAs["zerossl"] {
		if kid, hmac, err = zeroSSLEAB(m.S.Email); err != nil {
			return err
		}
	}
	if kid != "" {
		reg, err = client.Registration.RegisterWithExternalAccountBinding(registration.RegisterEABOptions{
			TermsOfServiceAgreed: true, Kid: kid, HmacEncoded: hmac})
	} else {
		if client.GetExternalAccountRequired() {
			return errors.New("the CA wants External Account Binding: set eab_kid and eab_hmac_key")
		}
		reg, err = client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	}
	if err != nil {
		return fmt.Errorf("registering the account: %w", err)
	}
	u.Registration = reg
	b, err := json.MarshalIndent(u, "", "\t")
	if err != nil {
		return err
	}
	m.Log.Printf("acme: registered %s at %s", reg.URI, m.S.CA)
	return writeFile(filepath.Join(u.dir, "account.json"), b, 0o600)
}

// zeroSSLEAB gets External Account Binding for email from ZeroSSL, as
// acme.sh does: a ZeroSSL account is made for it if there is none.
func zeroSSLEAB(email string) (kid, hmac string, err error) {
	if email == "" {
		return "", "", errors.New("zerossl needs email (or eab_kid and eab_hmac_key)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.zerossl.com/acme/eab-credentials-email",
		strings.NewReader(url.Values{"email": {email}}.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("zerossl: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var r struct {
		Success bool   `json:"success"`
		Kid     string `json:"eab_kid"`
		HMAC    string `json:"eab_hmac_key"`
	}
	if err := json.Unmarshal(body, &r); err != nil || !r.Success || r.Kid == "" {
		return "", "", fmt.Errorf("zerossl: no EAB credentials for %s: %s", email, strings.TrimSpace(string(body)))
	}
	return r.Kid, r.HMAC, nil
}

// suggested is when the CA's renewal information (ARI, RFC 9773) says to
// renew name's certificate, as last asked (zero: not known).
func (m *Manager) suggested(name string) time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ari[name]
}

// askARI asks the CA when to renew name's certificate (best effort: not
// every CA answers).
func (m *Manager) askARI(ctx context.Context, name string) {
	c := m.cert(name)
	tc := m.load(name)
	if c == nil || tc == nil || tc.Leaf == nil {
		return
	}
	m.obtain.Lock()
	client, err := m.client(ctx, c)
	m.obtain.Unlock()
	if err != nil {
		return
	}
	ri, err := client.Certificate.GetRenewalInfo(certificate.RenewalInfoRequest{Cert: tc.Leaf})
	if err != nil {
		return
	}
	if t := ri.ShouldRenewAt(m.Now(), 13*time.Hour); t != nil {
		m.mu.Lock()
		m.ari[name] = *t
		m.mu.Unlock()
	}
}
