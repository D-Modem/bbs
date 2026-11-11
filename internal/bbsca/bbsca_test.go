package bbsca

import (
	"archive/zip"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func verify(t *testing.T, ca *CA, certFile, keyFile, name string) *x509.Certificate {
	t.Helper()
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: name}); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return leaf
}

func TestCA(t *testing.T) {
	dir := t.TempDir()
	ca, err := Init(dir, "Test BBS", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir, "Test BBS", false); err == nil {
		t.Error("a second CA made over the first")
	}
	if ca.Cert.Subject.CommonName != "Test BBS CA" || !ca.Cert.IsCA {
		t.Errorf("CA %v", ca.Cert.Subject)
	}
	names := []string{"login.live.com", "nexus.passport.com", "192.168.77.11"}
	crt, key, renewed, err := ca.Ensure("msn", names, 0)
	if err != nil || !renewed {
		t.Fatalf("%v %v", renewed, err)
	}
	leaf := verify(t, ca, crt, key, "nexus.passport.com")
	if leaf.Subject.CommonName != "login.live.com" || len(leaf.IPAddresses) != 1 {
		t.Errorf("leaf %v %v", leaf.Subject, leaf.IPAddresses)
	}
	if d := time.Until(leaf.NotAfter); d > LeafDays*24*time.Hour {
		t.Errorf("good for %v", d)
	}
	// kept while good; the key kept when names change
	if _, _, renewed, _ := ca.Ensure("msn", names, 0); renewed {
		t.Error("signed again though good")
	}
	oldKey, _ := os.ReadFile(key)
	if _, _, renewed, _ := ca.Ensure("msn", names[:2], 0); !renewed {
		t.Error("names changed, not signed again")
	}
	if newKey, _ := os.ReadFile(key); !bytes.Equal(oldKey, newKey) {
		t.Error("key not kept")
	}
	// loaded again: the same CA
	again, err := Load(dir)
	if err != nil || again.Thumbprint() != ca.Thumbprint() {
		t.Fatalf("load %v", err)
	}
}

func TestSHA1CA(t *testing.T) {
	ca, err := Init(t.TempDir(), "Old BBS", true)
	if err != nil {
		t.Fatal(err)
	}
	cp, _, err := ca.Issue([]string{"login.yahoo.com"}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := pem.Decode(cp)
	c, _ := x509.ParseCertificate(b.Bytes)
	if c.SignatureAlgorithm != x509.SHA1WithRSA || ca.Cert.SignatureAlgorithm != x509.SHA1WithRSA {
		t.Errorf("signed %v by %v", c.SignatureAlgorithm, ca.Cert.SignatureAlgorithm)
	}
}

func TestPackage(t *testing.T) {
	ca, err := Init(t.TempDir(), "Retro BBS!", false)
	if err != nil {
		t.Fatal(err)
	}
	z, err := ca.Package("Retro BBS!")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		files[f.Name] = string(b)
	}
	for _, n := range []string{"Retro-BBS-CA.cer", "Retro-BBS-CA.crt", "install-ca.inf", "install-ca.reg", "remove-ca.reg",
		"install-ca.bat", "Retro-BBS-CA.mobileconfig", "README.txt"} {
		if _, ok := files[n]; !ok {
			t.Errorf("no %s", n)
		}
	}
	if files["Retro-BBS-CA.cer"] != string(ca.Cert.Raw) {
		t.Error(".cer is not the certificate")
	}
	// the .reg's blob: the hash property, then the certificate
	reg := files["install-ca.reg"]
	if !strings.HasPrefix(reg, "REGEDIT4\r\n") || !strings.Contains(reg, `\ROOT\Certificates\`+ca.Thumbprint()+"]") {
		t.Fatalf("reg:\n%s", reg)
	}
	h := reg[strings.Index(reg, "hex:")+4:]
	h = strings.NewReplacer(",", "", "\\", "", "\r", "", "\n", "", " ", "").Replace(h)
	blob, err := hex.DecodeString(h)
	if err != nil {
		t.Fatal(err)
	}
	if id := binary.LittleEndian.Uint32(blob); id != 3 {
		t.Errorf("first property %d", id)
	}
	n := binary.LittleEndian.Uint32(blob[8:])
	rest := blob[12+n:]
	if id, l := binary.LittleEndian.Uint32(rest), binary.LittleEndian.Uint32(rest[8:]); id != 32 ||
		!bytes.Equal(rest[12:12+l], ca.Cert.Raw) {
		t.Errorf("certificate property %d", id)
	}
	inf := files["install-ca.inf"]
	if !strings.Contains(inf, `[DefaultInstall]`) || !strings.Contains(inf, ca.Thumbprint()) {
		t.Errorf("inf:\n%s", inf)
	}
	if !strings.Contains(files["Retro-BBS-CA.mobileconfig"], "com.apple.security.root") {
		t.Error("mobileconfig")
	}
}
