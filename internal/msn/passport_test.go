package msn

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// login2 logs in at the Passport server by Passport 1.4; the status and
// the ticket ("" if none).
func (e *env) login2(t *testing.T, handle, pw string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://"+e.web+"/login2.srf", nil)
	req.Header.Set("Authorization", "Passport1.4 OrgVerb=GET,OrgURL=http%3A%2F%2Fmessenger%2Emsn%2Ecom,sign-in="+
		strings.ReplaceAll(handle, "@", "%40")+",pwd="+pw+",lc=1033,id=507,tw=40,fs=1,kpp=1,kv=5,ver=2.1.0173.1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	m := regexp.MustCompile(`from-PP='([^']*)'`).FindStringSubmatch(res.Header.Get("Authentication-Info"))
	if m == nil {
		return res.StatusCode, ""
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/html" {
		t.Errorf("content type %q", ct)
	}
	return res.StatusCode, m[1]
}

// rstLogin logs in at the Passport server by Passport SOAP (MSN 7.5); the
// response, and the ticket for messenger.msn.com ("" if none).
func (e *env) rstLogin(t *testing.T, handle, pw string) (string, string) {
	t.Helper()
	body := `<?xml version="1.0" encoding="utf-8"?><soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"` +
		` xmlns:wsse="http://schemas.xmlsoap.org/ws/2003/06/secext" xmlns:wsp="http://schemas.xmlsoap.org/ws/2002/12/policy"` +
		` xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/03/addressing" xmlns:wst="http://schemas.xmlsoap.org/ws/2004/04/trust">` +
		`<soap:Header><wsse:Security><wsse:UsernameToken Id="user"><wsse:Username>` + handle + `</wsse:Username>` +
		`<wsse:Password>` + pw + `</wsse:Password></wsse:UsernameToken></wsse:Security></soap:Header><soap:Body>` +
		`<ps:RequestMultipleSecurityTokens xmlns:ps="http://schemas.microsoft.com/Passport/SoapServices/PPCRL" Id="RSTS">` +
		`<wst:RequestSecurityToken Id="RST0"><wst:RequestType>http://schemas.xmlsoap.org/ws/2004/04/security/trust/Issue</wst:RequestType>` +
		`<wsp:AppliesTo><wsa:EndpointReference><wsa:Address>http://Passport.NET/tb</wsa:Address></wsa:EndpointReference></wsp:AppliesTo>` +
		`</wst:RequestSecurityToken><wst:RequestSecurityToken Id="RST1">` +
		`<wst:RequestType>http://schemas.xmlsoap.org/ws/2004/04/security/trust/Issue</wst:RequestType>` +
		`<wsp:AppliesTo><wsa:EndpointReference><wsa:Address>messenger.msn.com</wsa:Address></wsa:EndpointReference></wsp:AppliesTo>` +
		`<wsse:PolicyReference URI="?id=507"/></wst:RequestSecurityToken></ps:RequestMultipleSecurityTokens></soap:Body></soap:Envelope>`
	res, err := http.Post("http://"+e.web+"/RST.srf", "text/xml; charset=utf-8", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	m := regexp.MustCompile(`<wsse:BinarySecurityToken Id="Compact1">([^<]*)<`).FindStringSubmatch(string(b))
	if m == nil {
		return string(b), ""
	}
	return string(b), html.UnescapeString(m[1])
}

// twn signs a client in by TWN at MSNP ver (the ticket from Passport 1.4,
// or before MSNP12, or RST); the USR's answer.
func (e *env) twn(t *testing.T, name string, ver int) (*client, string) {
	t.Helper()
	handle := strings.ReplaceAll(name, " ", "") + "@bbs.test"
	cl := dial(t, e.addr)
	cl.cmd("VER", fmt.Sprintf("MSNP%d", ver), "MSNP8", "CVR0")
	if l, _ := cl.expect("VER "); l != fmt.Sprintf("VER 1 MSNP%d", ver) {
		t.Fatalf("version %q", l)
	}
	cl.cmd("CVR", "0x0409", "winnt", "5.1", "i386", "MSNMSGR", "7.5.0324", "MSMSGS", handle)
	cl.expect("CVR 2 ")
	cl.cmd("USR", "TWN", "I", handle)
	if l, _ := cl.expect("USR 3 "); !strings.HasPrefix(l, "USR 3 TWN S lc=1033,id=507,") {
		t.Fatalf("policy %q", l)
	}
	var tk string
	if ver >= 12 {
		_, tk = e.rstLogin(t, handle, "password1")
	} else {
		_, tk = e.login2(t, handle, "password1")
	}
	if tk == "" {
		t.Fatal("no ticket")
	}
	cl.cmd("USR", "TWN", "S", tk)
	l, _ := cl.expect("")
	return cl, l
}

// online12 signs a client in at MSNP ver, takes its lists, and sets it
// online with capabilities.
func (e *env) onlineAt(t *testing.T, name string, ver int) *client {
	t.Helper()
	cl, l := e.twn(t, name, ver)
	if !strings.HasPrefix(l, "USR 4 OK ") {
		t.Fatalf("%s: %q", name, l)
	}
	cl.expect("MSG Hotmail Hotmail ")
	tr := cl.cmd("SYN", "0", "0")
	cl.expect("SYN " + tr + " ")
	cl.cmd("CHG", "NLN", "1342488612")
	cl.expect("CHG ")
	return cl
}

// TestPassport: the nexus, a Passport 1.4 and an RST login, right and
// wrong; a ticket spent once, and only by its user.
func TestPassport(t *testing.T) {
	e := startWith(t, func(*Settings) {})
	res, err := http.Get("http://" + e.web + "/rdr/pprdr.asp")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if u := res.Header.Get("PassportURLs"); !strings.Contains(u, "DALogin="+e.web+"/login2.srf,") {
		t.Errorf("PassportURLs %q", u)
	}
	if code, tk := e.login2(t, "alice@bbs.test", "wrong"); code != 401 || tk != "" {
		t.Errorf("wrong password: %d %q", code, tk)
	}
	code, tk := e.login2(t, "alice@hotmail.com", "password1")
	if code != 200 || !regexp.MustCompile(`^t=[0-9a-f]+&p=[0-9a-f]+$`).MatchString(tk) {
		t.Fatalf("login: %d %q", code, tk)
	}
	if b, tk := e.rstLogin(t, "alice@bbs.test", "wrong"); tk != "" || !strings.Contains(b, "wsse:FailedAuthentication") {
		t.Errorf("RST, wrong password: %q", b)
	}
	b, rtk := e.rstLogin(t, "alice@bbs.test", "password1")
	if rtk == "" || !strings.Contains(b, "urn:passport:legacy") || !strings.Contains(b, "<psf:authstate>0x48803<") {
		t.Errorf("RST: %q", b)
	}

	// MSNP8: the ticket signs alice in, once
	cl := dial(t, e.addr)
	cl.cmd("VER", "MSNP8", "CVR0")
	cl.expect("VER 1 MSNP8")
	cl.cmd("USR", "TWN", "I", "alice@hotmail.com")
	cl.expect("USR 2 TWN S ")
	cl.cmd("USR", "TWN", "S", tk)
	if l, _ := cl.expect("USR 3 "); l != "USR 3 OK alice@hotmail.com alice 1 0" {
		t.Errorf("signed in: %q", l)
	}
	if _, p := cl.expect("MSG Hotmail Hotmail "); !strings.Contains(p, "MSPAuth: "+strings.TrimPrefix(strings.Split(tk, "&")[0], "t=")+"\r\n") {
		t.Errorf("profile %q", p)
	}
	again := dial(t, e.addr)
	again.cmd("VER", "MSNP8", "CVR0")
	again.cmd("USR", "TWN", "I", "alice@bbs.test")
	again.expect("USR 2 TWN S ")
	again.cmd("USR", "TWN", "S", tk)
	if l, _ := again.expect("911 "); l != "911 3" {
		t.Errorf("spent ticket: %q", l)
	}
	// bob with alice's ticket
	bob := dial(t, e.addr)
	bob.cmd("VER", "MSNP12", "CVR0")
	bob.cmd("USR", "TWN", "I", "bob@bbs.test")
	bob.expect("USR 2 TWN S ")
	bob.cmd("USR", "TWN", "S", rtk)
	if l, _ := bob.expect("911 "); l != "911 3" {
		t.Errorf("another's ticket: %q", l)
	}

	// MSNP12: no name in USR OK, SBS, ABCHMigrated
	c12, l := e.twn(t, "Carol Ann", 12)
	if l != "USR 4 OK CarolAnn@bbs.test 1 0" {
		t.Errorf("MSNP12: %q", l)
	}
	c12.expect("SBS 0 null")
	if _, p := c12.expect("MSG Hotmail Hotmail "); !strings.Contains(p, "ABCHMigrated: 1\r\n") {
		t.Errorf("profile %q", p)
	}
	// without the Passport server, MSNP8 is not served
	e2 := start(t)
	nc := dial(t, e2.addr)
	nc.cmd("VER", "MSNP12", "MSNP8", "CVR0")
	if l, _ := nc.expect("VER "); l != "VER 1 0" {
		t.Errorf("no Passport: %q", l)
	}
}

// TestLists8: SYN at MSNP8 (contacts once, lists as bits, numbered
// groups) and at MSNP12 (GUIDs, ADC); groups, friends, blocks and names
// changed by MSNP12 commands.
func TestLists8(t *testing.T) {
	e := startWith(t, func(*Settings) {})
	alice, bob, carol := e.user(t, "alice"), e.user(t, "bob"), e.user(t, "Carol Ann")
	if _, err := e.b.AddFriend(alice, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.b.AddFriend(bob, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.b.AddFriend(carol, "alice"); err != nil {
		t.Fatal(err)
	}
	f, _ := e.b.Store.Friends(alice.ID)
	f[0].Group = "Pals"
	e.b.Store.PutFriend(alice.ID, f[0])

	cl, _ := e.twn(t, "alice", 8)
	cl.expect("MSG Hotmail ")
	cl.cmd("SYN", "0")
	want := []string{"SYN 5 1 2 2", "GTC A", "BLP AL", "LSG 0 Other%20Contacts 0", "LSG 1 Pals 0",
		"LST CarolAnn@bbs.test Carol%20Ann 10", "LST bob@bbs.test bob 11 1"}
	for _, w := range want {
		if l, _ := cl.expect(""); l != w {
			t.Errorf("MSNP8: got %q, want %q", l, w)
		}
	}
	cl.cmd("OUT")

	cl, _ = e.twn(t, "alice", 12)
	cl.expect("MSG Hotmail ")
	tr := cl.cmd("SYN", "0", "0")
	if l, _ := cl.expect("SYN "); !regexp.MustCompile(`^SYN ` + tr + ` \S+ \S+ 2 1$`).MatchString(l) {
		t.Errorf("SYN %q", l)
	}
	pals := nameGUID("Pals")
	bobG, carolG := contactGUID(bob.ID), contactGUID(carol.ID)
	want = []string{"GTC A", "BLP AL", "PRP MFN alice", "LSG Pals " + pals,
		"LST N=CarolAnn@bbs.test F=Carol%20Ann 10 1", "LST N=bob@bbs.test F=bob C=" + bobG + " 11 1 " + pals}
	for _, w := range want {
		if l, _ := cl.expect(""); l != w {
			t.Errorf("MSNP12: got %q, want %q", l, w)
		}
	}

	// a friend added, put in a new group, out of it, removed
	tr = cl.cmd("ADC", "FL", "N=CarolAnn@bbs.test", "F=Carol%20Ann")
	if l, _ := cl.expect("ADC " + tr); l != "ADC "+tr+" FL N=CarolAnn@bbs.test F=Carol%20Ann C="+carolG {
		t.Errorf("adc %q", l)
	}
	tr = cl.cmd("ADG", "Work")
	if l, _ := cl.expect("ADG " + tr); l != "ADG "+tr+" Work "+nameGUID("Work") {
		t.Errorf("adg %q", l)
	}
	tr = cl.cmd("ADC", "FL", "C="+carolG, nameGUID("Work"))
	if l, _ := cl.expect("ADC " + tr); l != "ADC "+tr+" FL C="+carolG+" "+nameGUID("Work") {
		t.Errorf("into group %q", l)
	}
	group := func() string {
		fs, _ := e.b.Store.Friends(alice.ID)
		for _, f := range fs {
			if f.ID == carol.ID {
				return f.Group
			}
		}
		return "(none)"
	}
	if g := group(); g != "Work" {
		t.Errorf("group %q", g)
	}
	tr = cl.cmd("REM", "FL", carolG, nameGUID("Work"))
	cl.expect("REM " + tr + " FL " + carolG + " " + nameGUID("Work"))
	if g := group(); g != "" {
		t.Errorf("out of group %q", g)
	}
	tr = cl.cmd("SBP", carolG, "MFN", "Caz")
	cl.expect("SBP " + tr + " " + carolG + " MFN Caz")
	tr = cl.cmd("REM", "FL", carolG)
	cl.expect("REM " + tr + " FL " + carolG)
	if g := group(); g != "(none)" {
		t.Errorf("still a friend: %q", g)
	}

	// a block, and the name
	tr = cl.cmd("ADC", "BL", "N=bob@bbs.test")
	cl.expect("ADC " + tr + " BL N=bob@bbs.test")
	if !e.b.Blocks(alice, bob) {
		t.Error("not blocked")
	}
	tr = cl.cmd("REM", "BL", "bob@bbs.test")
	cl.expect("REM " + tr + " BL bob@bbs.test")
	tr = cl.cmd("PRP", "MFN", "Alice%20A")
	cl.expect("PRP " + tr + " MFN Alice%20A")
	tr = cl.cmd("BLP", "BL")
	cl.expect("BLP " + tr + " BL")

	// one added elsewhere comes as ADC
	if _, err := e.b.AddFriend(alice, "sysop"); err != nil {
		t.Fatal(err)
	}
	if l, _ := cl.expect("ADC 0 FL "); !strings.HasPrefix(l, "ADC 0 FL N=sysop@bbs.test F=sysop C=") {
		t.Errorf("from elsewhere %q", l)
	}
}

// TestPresence12: capabilities and display pictures in CHG and NLN,
// personal messages (UUX, UBX), capabilities in JOI and IRO, QNG's time;
// an MSNP7 client sees the same user without them.
func TestPresence12(t *testing.T) {
	e := startWith(t, func(*Settings) {})
	alice, bob := e.user(t, "alice"), e.user(t, "bob")
	e.b.AddFriend(alice, "bob")
	e.b.AddFriend(bob, "alice")
	e.b.AddFriend(e.user(t, "Carol Ann"), "bob")
	a := e.onlineAt(t, "alice", 12)
	c7 := online(t, e.addr, "CarolAnn")
	b := e.onlineAt(t, "bob", 11)
	if l, _ := a.expect("NLN "); l != "NLN NLN bob@bbs.test bob 1342488612" {
		t.Errorf("bob on %q", l)
	}
	if l, _ := c7.expect("NLN "); l != "NLN NLN bob@bbs.test bob" {
		t.Errorf("carol saw %q", l)
	}
	obj := "%3Cmsnobj%20Creator%3D%22bob%40bbs.test%22%2F%3E"
	tr := b.cmd("CHG", "BSY", "1342488612", obj)
	if l, _ := b.expect("CHG "); l != "CHG "+tr+" BSY 1342488612 "+obj {
		t.Errorf("chg %q", l)
	}
	if l, _ := a.expect("NLN "); l != "NLN BSY bob@bbs.test bob 1342488612 "+obj {
		t.Errorf("busy %q", l)
	}
	psm := "<Data><PSM>at the BBS</PSM><CurrentMedia></CurrentMedia></Data>"
	b.trid++
	b.cl(fmt.Sprintf("UUX %d %d\r\n%s", b.trid, len(psm), psm))
	b.expect(fmt.Sprintf("UUX %d 0", b.trid))
	if l, p := a.expect("UBX "); l != fmt.Sprintf("UBX bob@bbs.test %d", len(psm)) || p != psm {
		t.Errorf("ubx %q %q", l, p)
	}
	a.cmd("PNG")
	a.expect("QNG 50")

	// a conversation: capabilities in IRO (MSNP12) and JOI
	as := a.switchboard(t, "alice@bbs.test")
	as.cmd("CAL", "bob@bbs.test")
	rng, _ := b.expect("RNG ")
	bs, iro := answer(t, rng, "bob@bbs.test")
	if len(iro) != 1 || iro[0] != "IRO 1 1 1 alice@bbs.test alice" {
		t.Errorf("MSNP11 iro %q", iro) // no capabilities before MSNP12
	}
	if l, _ := as.expect("JOI "); l != "JOI bob@bbs.test bob 1342488612" {
		t.Errorf("joi %q", l)
	}
	bs.msg("N", "hi")
	if _, p := as.expect("MSG bob@bbs.test "); !strings.HasSuffix(p, "hi") {
		t.Errorf("msg %q", p)
	}
}

// cl sends raw bytes.
func (cl *client) cl(raw string) { cl.c.Write([]byte(raw)) }

// TestPassportTLS: the Passport server over HTTPS takes Windows XP's TLS
// 1.0 with 3DES, with the certificate in tls_cert for Passport's names.
func TestPassportTLS(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"nexus.passport.com", "login.passport.com", "login.live.com"}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: names[1]},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: names}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "p.crt"), filepath.Join(dir, "p.key")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600)
	httpsAddr := freeAddr(t)
	startWith(t, func(s *Settings) {
		s.HTTPSAddress, s.CertFile, s.KeyFile, s.PassportHost = httpsAddr, certFile, keyFile, "login.passport.com"
	})
	roots := x509.NewCertPool()
	c, _ := x509.ParseCertificate(der)
	roots.AddCert(c)
	var conn *tls.Conn
	for i := 0; ; i++ {
		conn, err = tls.Dial("tcp", httpsAddr, &tls.Config{RootCAs: roots, ServerName: "nexus.passport.com",
			MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS10,
			CipherSuites: []uint16{tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA}})
		if err == nil || i > 100 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	io.WriteString(conn, "GET /rdr/pprdr.asp HTTP/1.1\r\nHost: nexus.passport.com\r\nConnection: close\r\n\r\n")
	b, _ := io.ReadAll(conn)
	if !strings.HasPrefix(string(b), "HTTP/1.1 200") || !strings.Contains(string(b), "DALogin=login.passport.com/login2.srf,") {
		t.Errorf("nexus: %q", b)
	}
}
