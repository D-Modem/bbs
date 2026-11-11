// Package bbsca is the BBS's own certificate authority, for the names of
// the old services whose front ends it is (login.live.com,
// login.yahoo.com, api.screenname.aol.com ...): names no public CA will
// sign for it.  The CA's certificate is put on the callers' machines once
// (Package makes the files to do it), and the BBS signs each front end's
// certificate itself, renewing it as it runs out.  It does as well for a
// TLS proxy's certificate (Issue).
package bbsca

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// CertFile is cert_file's (tls_cert's) value for a certificate the BBS's
// CA signs and keeps (the front end's names; no key file).
const CertFile = "ca"

// IsCA: a cert_file is the BBS's CA's.
func IsCA(certFile string) bool { return certFile == CertFile }

// the root's certificate, in the CA's directory
const caCert = "ca.crt"

// LeafDays is how long a certificate the CA signs is good for: under the
// 398 days current browsers take, for a TLS proxy's too.
const LeafDays = 397

// renewDays: a certificate with less than this left is signed again.
const renewDays = 30

// CA is the certificate authority.
type CA struct {
	Dir  string
	Cert *x509.Certificate
	Key  crypto.Signer
	PEM  []byte // the certificate
	// Name is a sub-CA's name (services, proxy ...); "" for the root
	Name string
	// SHA1: certificates are signed with SHA-1 (as the CA's own is), for
	// Windows 95 to 2000 and XP before SP3, which know no SHA-256
	SHA1 bool
}

// Dir is the CA's directory under the BBS's data directory.
func Dir(dataDir string) string { return filepath.Join(dataDir, "ca") }

// Exists: dir has a CA.
func Exists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, caCert))
	return err == nil
}

// The CAs: a root, the one callers' machines trust (ca.crt, ca.key), which
// signs only sub-CAs (NAME-ca.crt, NAME-ca.key beside it): Services, which
// signs the certificates of the servers the BBS runs, and others for
// programs beside it (a TLS proxy's, say), each its own key.

// Services is the sub-CA that signs the BBS's own servers' certificates.
const Services = "services"

// Init makes a root CA in dir, named name (the BBS's), good for 20 years,
// and its Services sub-CA: RSA 2048, signed with SHA-1 when sha1 is set
// (for Windows before XP SP3).  An existing one is an error.
func Init(dir, name string, sha1 bool) (*CA, error) {
	if Exists(dir) {
		return nil, fmt.Errorf("%s already has a CA", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = "BBS"
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: name + " CA", Organization: []string{name}},
		NotBefore:             now.Add(-24 * time.Hour),
		NotAfter:              now.AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1, // sub-CAs, which sign only servers
	}
	if sha1 {
		tmpl.SignatureAlgorithm = x509.SHA1WithRSA
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	if err := writePair(dir, "ca", der, key); err != nil {
		return nil, err
	}
	root, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if _, _, err := root.Sub(Services); err != nil {
		return nil, err
	}
	return root, nil
}

// writePair writes base.crt and base.key in dir (the key first, private).
func writePair(dir, base string, der []byte, key *rsa.PrivateKey) error {
	kp := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(filepath.Join(dir, base+".key"), kp, 0o600); err != nil {
		return err
	}
	cp := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return os.WriteFile(filepath.Join(dir, base+".crt"), cp, 0o644)
}

var subName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Sub is the root's sub-CA name (services, proxy ...), made now if there
// is none (made: true): NAME-ca.crt and NAME-ca.key in the root's
// directory, good as long as the root, signing only servers' certificates.
func (root *CA) Sub(name string) (sub *CA, made bool, err error) {
	if root.Name != "" {
		return nil, false, errors.New("a sub-CA makes no sub-CAs")
	}
	if !subName.MatchString(name) || name == "ca" {
		return nil, false, fmt.Errorf("sub-CA %q: lower-case letters, digits and dashes", name)
	}
	base := name + "-ca"
	if _, err := os.Stat(filepath.Join(root.Dir, base+".crt")); err == nil {
		sub, err := loadPair(root.Dir, base)
		if err != nil {
			return nil, false, err
		}
		sub.Name = name
		return sub, false, nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, false, err
	}
	org := root.Cert.Subject.CommonName
	if len(root.Cert.Subject.Organization) > 0 {
		org = root.Cert.Subject.Organization[0]
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: org + " " + strings.ToUpper(name[:1]) + name[1:] + " CA", Organization: []string{org}},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              root.Cert.NotAfter.Add(-24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	if root.SHA1 {
		tmpl.SignatureAlgorithm = x509.SHA1WithRSA
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, root.Cert, &key.PublicKey, root.Key)
	if err != nil {
		return nil, false, err
	}
	if err := writePair(root.Dir, base, der, key); err != nil {
		return nil, false, err
	}
	sub, err = loadPair(root.Dir, base)
	if err != nil {
		return nil, false, err
	}
	sub.Name = name
	return sub, true, nil
}

// Subs are the names of the root's sub-CAs there are.
func (root *CA) Subs() []string {
	files, _ := filepath.Glob(filepath.Join(root.Dir, "*-ca.crt"))
	var out []string
	for _, f := range files {
		out = append(out, strings.TrimSuffix(filepath.Base(f), "-ca.crt"))
	}
	return out
}

// Load reads the root CA in dir.
func Load(dir string) (*CA, error) {
	if !Exists(dir) {
		return nil, fmt.Errorf("no CA in %s (bbs ca init makes one)", dir)
	}
	root, err := loadPair(dir, "ca")
	if err != nil {
		return nil, err
	}
	if root.Cert.MaxPathLenZero || root.Cert.MaxPathLen == 0 {
		return nil, fmt.Errorf("the CA in %s signs no sub-CAs (an earlier BBS's): move %s away, "+
			"and the BBS makes a new one", dir, dir)
	}
	return root, nil
}

// loadPair reads base.crt and base.key in dir.
func loadPair(dir, base string) (*CA, error) {
	certName, keyName := base+".crt", base+".key"
	cp, err := os.ReadFile(filepath.Join(dir, certName))
	if err != nil {
		return nil, err
	}
	kp, err := os.ReadFile(filepath.Join(dir, keyName))
	if err != nil {
		return nil, err
	}
	b, _ := pem.Decode(cp)
	if b == nil {
		return nil, fmt.Errorf("%s: not a PEM certificate", certName)
	}
	cert, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return nil, err
	}
	kb, _ := pem.Decode(kp)
	if kb == nil {
		return nil, fmt.Errorf("%s: not a PEM key", keyName)
	}
	key, err := x509.ParsePKCS1PrivateKey(kb.Bytes)
	if err != nil {
		return nil, err
	}
	return &CA{Dir: dir, Cert: cert, Key: key, PEM: cp, SHA1: cert.SignatureAlgorithm == x509.SHA1WithRSA}, nil
}

// LoadOrInit is the root CA in dir, made first if there is none.
func LoadOrInit(dir, name string) (*CA, bool, error) {
	if Exists(dir) {
		ca, err := Load(dir)
		return ca, false, err
	}
	ca, err := Init(dir, name, false)
	return ca, true, err
}

// Issue signs a certificate for names (host names, or IP addresses), the
// first its common name (for old clients that look only there), good for
// days (0: LeafDays; never past the CA's own end), for key (nil: a new
// RSA 2048 one).  Both come back as PEM.
func (ca *CA) Issue(names []string, days int, key *rsa.PrivateKey) (certPEM, keyPEM []byte, err error) {
	if len(names) == 0 {
		return nil, nil, errors.New("no names")
	}
	if ca.Name == "" {
		return nil, nil, errors.New("the root CA signs only sub-CAs: sign with one of them")
	}
	if days <= 0 {
		days = LeafDays
	}
	if key == nil {
		if key, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			return nil, nil, err
		}
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: names[0]},
		NotBefore:    now.Add(-24 * time.Hour),
		NotAfter:     now.AddDate(0, 0, days-1),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if tmpl.NotAfter.After(ca.Cert.NotAfter) {
		tmpl.NotAfter = ca.Cert.NotAfter
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	if ca.SHA1 {
		tmpl.SignatureAlgorithm = x509.SHA1WithRSA
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	// the chain: the sub-CA's certificate after it, which clients need sent
	// (they have only the root)
	certPEM = append(certPEM, ca.PEM...)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM, nil
}

// Ensure is a certificate for names kept in the CA's directory as
// label.crt and label.key: the one there, or one signed now when there is
// none, its names differ, it is not this CA's, or it runs out within 30
// days.  A new one keeps the key there (clients that pin a key, as Gemini's
// do, see no change).  days as Issue's.  renewed: it was signed now.
func (ca *CA) Ensure(label string, names []string, days int) (certFile, keyFile string, renewed bool, err error) {
	certFile, keyFile = filepath.Join(ca.Dir, label+".crt"), filepath.Join(ca.Dir, label+".key")
	if ca.good(certFile, names) {
		return certFile, keyFile, false, nil
	}
	var key *rsa.PrivateKey
	if kp, err := os.ReadFile(keyFile); err == nil {
		if b, _ := pem.Decode(kp); b != nil {
			key, _ = x509.ParsePKCS1PrivateKey(b.Bytes)
		}
	}
	cp, kp, err := ca.Issue(names, days, key)
	if err != nil {
		return "", "", false, err
	}
	// the key first: a reader of the files sees a new certificate only
	// with its key there
	if err := writeAtomic(keyFile, kp, 0o600); err != nil {
		return "", "", false, err
	}
	if err := writeAtomic(certFile, cp, 0o644); err != nil {
		return "", "", false, err
	}
	return certFile, keyFile, true, nil
}

// good: certFile is this CA's, for names, with more than renewDays left.
func (ca *CA) good(certFile string, names []string) bool {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return false
	}
	b, _ := pem.Decode(data)
	if b == nil {
		return false
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil || !bytes.Equal(c.RawIssuer, ca.Cert.RawSubject) || c.CheckSignatureFrom(ca.Cert) != nil {
		return false
	}
	if time.Until(c.NotAfter) < renewDays*24*time.Hour {
		return false
	}
	var have []string
	have = append(have, c.DNSNames...)
	for _, ip := range c.IPAddresses {
		have = append(have, ip.String())
	}
	want := slices.Clone(names)
	slices.Sort(have)
	slices.Sort(want)
	return c.Subject.CommonName == names[0] && slices.Equal(have, want)
}

// Thumbprint is the CA certificate's SHA-1, as Windows shows it.
func (ca *CA) Thumbprint() string {
	sum := sha1.Sum(ca.Cert.Raw)
	return strings.ToUpper(fmt.Sprintf("%x", sum))
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	return n.Add(n, big.NewInt(1))
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
