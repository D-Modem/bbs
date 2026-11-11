package oscar

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// webResponse is the AIM web API's XML answer, of what the tests read.
type webResponse struct {
	StatusCode       int    `xml:"statusCode"`
	StatusDetailCode int    `xml:"statusDetailCode"`
	Token            string `xml:"data>token>a"`
	SessionSecret    string `xml:"data>sessionSecret"`
	Host             string `xml:"data>host"`
	Port             int    `xml:"data>port"`
	Cookie           string `xml:"data>cookie"`
	TLSCertName      string `xml:"data>tlsCertName"`
}

func readWeb(t *testing.T) func(*http.Response, error) webResponse {
	return func(resp *http.Response, err error) webResponse { return readWebResp(t, resp, err) }
}

func readWebResp(t *testing.T, resp *http.Response, err error) webResponse {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var w webResponse
	if err := xml.Unmarshal(b, &w); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return w
}

// TestClientLogin: clientLogin over HTTP on the OSCAR port (as Pidgin
// does it), then startOSCARSession, then the BOS with its cookie; a
// wrong password refused as libpurple reads it.
func TestClientLogin(t *testing.T) {
	e := start(t)
	base := "http://" + e.addr
	w := readWeb(t)(http.PostForm(base+"/auth/clientLogin",
		url.Values{"devId": {"pidgin"}, "f": {"xml"}, "s": {"alice"}, "pwd": {"wrong"}}))
	if w.StatusCode != 330 || w.StatusDetailCode != 3011 {
		t.Errorf("wrong password: %+v", w)
	}
	w = readWeb(t)(http.PostForm(base+"/auth/clientLogin",
		url.Values{"devId": {"pidgin"}, "f": {"xml"}, "s": {"alice"}, "pwd": {"password1"}}))
	if w.StatusCode != 200 || w.Token == "" || w.SessionSecret == "" {
		t.Fatalf("clientLogin: %+v", w)
	}
	q := url.Values{"a": {w.Token}, "f": {"xml"}, "k": {"pidgin"}, "ts": {"1"}, "useTLS": {"0"}}
	w = readWeb(t)(http.Get(base + "/aim/startOSCARSession?" + q.Encode()))
	if w.StatusCode != 200 || net.JoinHostPort(w.Host, strconv.Itoa(w.Port)) != e.addr || w.TLSCertName != "" {
		t.Fatalf("startOSCARSession: %+v", w)
	}
	ck, err := base64.StdEncoding.DecodeString(w.Cookie)
	if err != nil {
		t.Fatal(err)
	}
	alice := bos(t, e.addr, ck)
	alice.sendIM("bob", "hi", false)
	if from, _ := alice.recvIM(); from != "OnlineHost" { // bob is away: kept
		t.Errorf("from %q", from)
	}

	// a spent token is no good
	w = readWeb(t)(http.Get(base + "/aim/startOSCARSession?" + q.Encode()))
	if w.StatusCode != 401 {
		t.Errorf("spent token: %+v", w)
	}
}

// kerberosRequest is AIM 6's login request, roasted as version 4 and
// later roast it.
func kerberosRequest(sn, pw string) []byte {
	var ticket buf
	ticket.u32(0x00010000)
	ticket.u16(4)
	ticket.u32(0)
	ticket.u16(0)
	ticket.str16(unroast([]byte(pw), roastKerberos))
	ticket.u16(0)
	var meta tlvs
	meta.add(0x0002, ticket)
	var b buf
	b.u16(kerbFamily)
	b.u16(kerbRequest)
	b.u16(0)
	b.u32(7)
	b.u32(42) // request id
	b.raw(make([]byte, 16))
	b.u16(0)
	b.raw(make([]byte, 8))
	b.str16("US")
	b.str16("en")
	b.u16(0)
	b.u32(0)
	b.u8(1)
	b.str16(sn)
	b.str16("im/boss")
	b.u32(0)
	b.u16(2)
	b.raw(meta.block())
	return b
}

// kerberosAnswer reads a Kerberos login's answer: its subtype, and the
// ticket's connection TLVs.
func kerberosAnswer(t *testing.T) func(*http.Response, error) (uint16, tlvs) {
	return func(resp *http.Response, err error) (uint16, tlvs) { return kerberosResp(t, resp, err) }
}

func kerberosResp(t *testing.T, resp *http.Response, err error) (uint16, tlvs) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	h, body, err := parseSNAC(b)
	if err != nil || h.fam != kerbFamily {
		t.Fatalf("answer %x", b)
	}
	if h.sub != kerbOK {
		return h.sub, nil
	}
	r := &rd{p: body}
	if id := r.u32(); id != 42 {
		t.Errorf("request id %d", id)
	}
	r.take(8)
	r.str16()
	r.str16()
	if n := r.u16(); n != 1 {
		t.Fatalf("%d tickets", n)
	}
	r.u16()
	for range 5 {
		r.str16()
	}
	r.u8()
	r.str16()
	r.take(36)
	meta := r.tlvBlock()
	v, _ := meta.get(0x0003)
	ir := &rd{p: v}
	ir.u16()
	conn := ir.tlvBlock()
	if r.bad || ir.bad {
		t.Fatalf("short answer %x", b)
	}
	return h.sub, conn
}

// TestKerberos: AIM 6.2 to 7.x's login, by HTTP on the OSCAR port: a
// wrong password refused, a right one answered with the BOS's host and a
// cookie that signs on.
func TestKerberos(t *testing.T) {
	e := start(t)
	sub, _ := kerberosAnswer(t)(http.Post("http://"+e.addr+"/", "application/x-snac",
		strings.NewReader(string(kerberosRequest("alice", "nope")))))
	if sub != kerbFail {
		t.Errorf("wrong password: %x", sub)
	}
	sub, conn := kerberosAnswer(t)(http.Post("http://"+e.addr+"/", "application/x-snac",
		strings.NewReader(string(kerberosRequest("alice", "password1")))))
	if sub != kerbOK {
		t.Fatalf("login: %x", sub)
	}
	if conn.str(0x0005) != e.addr || conn.has(0x008e) {
		t.Errorf("connection %v", conn)
	}
	ck, _ := conn.get(0x0006)
	bos(t, e.addr, ck)
}

// selfSigned writes a certificate for 127.0.0.1 and its key; their paths.
func selfSigned(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	dir := t.TempDir()
	crt, kf := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	os.WriteFile(crt, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(kf, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	return crt, kf
}

// TestKerberosTLS: with a TLS port, the Kerberos login comes by HTTPS
// (TLS 1.0 taken, as AIM 6 speaks it) and sends the client to the TLS
// host; the BOS by TLS there; a chat room asked for by TLS is there too.
func TestKerberosTLS(t *testing.T) {
	crt, key := selfSigned(t)
	tlsAddr := freeAddr(t)
	start(t, func(s *Settings) { s.TLSAddress, s.TLSCert, s.TLSKey, s.TLSHost = tlsAddr, crt, key, tlsAddr })
	hc := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS10,
		MaxVersion: tls.VersionTLS10}}}
	sub, conn := kerberosAnswer(t)(hc.Post("https://"+tlsAddr+"/", "application/x-snac",
		strings.NewReader(string(kerberosRequest("alice", "password1")))))
	if sub != kerbOK {
		t.Fatalf("login: %x", sub)
	}
	if conn.str(0x0005) != tlsAddr || conn.str(0x008d) != "127.0.0.1" {
		t.Errorf("connection %v", conn)
	}
	if v, _ := conn.u16(0x008e); v != 2 {
		t.Errorf("SSL %d", v)
	}
	ck, _ := conn.get(0x0006)
	c, err := tls.Dial("tcp", tlsAddr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	var h tlvs
	h.add(tlvCookie, ck)
	alice := bosUp(t, clientOn(t, c, h, false))

	// a chat room, by TLS as asked
	var q buf
	q.u16(0x000d)
	var sr tlvs
	sr.add(0x008c, nil)
	q.raw(sr.bytes())
	alice.send(0x01, 0x04, q)
	res := parseTLVs(alice.expect(0x01, 0x05))
	if res.str(0x0005) != tlsAddr || res.str(0x008d) != "127.0.0.1" {
		t.Errorf("service %v", res)
	}
}
