package msn

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/sockets"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/vhost"
)

// Passport: MSN Messenger 5.0 and later sign in by giving the password to
// a Passport server over HTTPS, which hands them a ticket; the ticket is
// what they give the notification server (USR TWN S).  The BBS is that
// server too, once the clients' Passport names are pointed at it:
//
//	/rdr/pprdr.asp (nexus.passport.com): where to log in (PassportURLs)
//	/login2.srf (login.passport.com, Passport 1.4): MSN 5.0 to 7.0
//	/RST.srf (login.live.com, Passport SOAP): MSN 7.5

// ticketLife: a ticket is given to the notification server within this.
const ticketLife = 10 * time.Minute

// ticket is one handed out, for a user.
type ticket struct {
	user    int64
	expires time.Time
}

// newTicket is a Passport compact token ("t=...&p=...") for u.
func (s *Server) newTicket(u *store.User) string {
	t := randomHex(24)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.tickets {
		if now.After(v.expires) {
			delete(s.tickets, k)
		}
	}
	s.tickets[t] = &ticket{user: u.ID, expires: now.Add(ticketLife)}
	return "t=" + t + "&p=" + randomHex(16)
}

// takeTicket spends the ticket in a compact token (0: none such, or
// expired); the user's id.
func (s *Server) takeTicket(token string) int64 {
	q, err := url.ParseQuery(token)
	t := ""
	if err == nil {
		t = q.Get("t")
	}
	if t == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.tickets[t]
	delete(s.tickets, t)
	if k == nil || time.Now().After(k.expires) {
		return 0
	}
	return k.user
}

// passportOn: the Passport server is set up, so MSNP8 and later are
// served.
func (s *Server) passportOn() bool { return s.S.HTTPSAddress != "" || s.S.HTTPAddress != "" }

// maxVer is the highest protocol version served.
func (s *Server) maxVer() int {
	if s.passportOn() {
		return maxVersion
	}
	return 7
}

// serveWeb serves the Passport server until ctx is done.
func (s *Server) serveWeb(ctx context.Context) error {
	errs := make(chan error, 2)
	var srvs []*http.Server
	start := func(addr string, cfg *tls.Config) error {
		if vhost.Claimed(addr) {
			return nil // a port shared with other front ends, which hands us ours
		}
		ln, err := sockets.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("msn: %w", err)
		}
		if cfg != nil {
			ln = tls.NewListener(ln, cfg)
		}
		hs := &http.Server{Handler: certs.WrapHTTP(s), ReadHeaderTimeout: 30 * time.Second, WriteTimeout: time.Minute,
			ErrorLog: nil}
		srvs = append(srvs, hs)
		go func() {
			if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs <- fmt.Errorf("msn: passport: %w", err)
			}
		}()
		return nil
	}
	if s.S.HTTPAddress != "" {
		if err := start(s.S.HTTPAddress, nil); err != nil {
			return err
		}
	}
	if s.S.HTTPSAddress != "" {
		src, err := certs.New(s.S.CertFile, s.S.KeyFile)
		if err != nil {
			return fmt.Errorf("msn: tls_cert, tls_key: %w", err)
		}
		if err := start(s.S.HTTPSAddress, certs.Legacy(src.GetCertificate)); err != nil {
			return err
		}
	}
	var err error
	select {
	case <-ctx.Done():
	case err = <-errs:
	}
	for _, hs := range srvs {
		hs.Close()
	}
	return err
}

// webAddr is an address as a net.Addr.
type webAddr string

func (a webAddr) Network() string { return "tcp" }
func (a webAddr) String() string  { return string(a) }

// ServeHTTP answers the Passport server's requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.B.Refuse("msn", webAddr(r.RemoteAddr)) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	w.Header().Set("Cache-Control", "no-cache")
	switch strings.ToLower(r.URL.Path) {
	case "/rdr/pprdr.asp":
		s.nexus(w, r)
	case "/login2.srf":
		s.login2(w, r, host)
	case "/rst.srf":
		s.rst(w, r, host)
	default:
		http.NotFound(w, r)
	}
}

// passportHost is where clients log in (DALogin), host[:port]: the
// setting's, or the host the nexus was asked at.
func (s *Server) passportHost(r *http.Request) string {
	if s.S.PassportHost != "" {
		return s.S.PassportHost
	}
	return r.Host
}

// nexus is pprdr.asp: where to log in.
func (s *Server) nexus(w http.ResponseWriter, r *http.Request) {
	h := s.passportHost(r)
	site := s.site()
	w.Header()["PassportURLs"] = []string{"DARealm=Passport.Net,DALogin=" + h + "/login2.srf,DAReg=" + site +
		",Properties=" + site + ",Privacy=" + site + ",GeneralRedir=" + site + ",Help=" + site + ",ConfigVersion=15"}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

// site is the web address given for the clients' links.
func (s *Server) site() string {
	if s.S.Site != "" {
		return s.S.Site
	}
	return "http://" + s.S.Domain + "/"
}

// passportArgs are a Passport1.4 Authorization header's arguments
// (key=value, comma-separated, values URL-encoded).
func passportArgs(h string) (map[string]string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(h), "Passport1.4")
	if !ok {
		return nil, false
	}
	out := map[string]string{}
	for _, kv := range strings.Split(strings.TrimSpace(rest), ",") {
		k, v, _ := strings.Cut(kv, "=")
		if d, err := url.PathUnescape(v); err == nil {
			v = d
		}
		out[strings.ToLower(strings.TrimSpace(k))] = v
	}
	return out, true
}

// login checks a handle and password given to the Passport server; the
// user, or nil (the failure is counted and logged as any login's).
func (s *Server) login(handle, pw, host string) *store.User {
	name := handle
	if u := s.resolve(handle); u != nil {
		name = u.Name
	} else if local, _, ok := strings.Cut(handle, "@"); ok {
		name = local
	}
	u, err := s.B.Authenticate("msn", "ip:"+host, name, pw)
	if err != nil {
		return nil
	}
	if s.may(u) != nil {
		return nil
	}
	return u
}

// login2 is login2.srf: Passport 1.4, the password in the Authorization
// header; a ticket for the notification server.
func (s *Server) login2(w http.ResponseWriter, r *http.Request, host string) {
	args, ok := passportArgs(r.Header.Get("Authorization"))
	var u *store.User
	if ok && args["sign-in"] != "" {
		u = s.login(args["sign-in"], args["pwd"], host)
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("cachecontrol", "no-store")
	if u == nil {
		w.Header().Set("WWW-Authenticate", "Passport1.4 da-status=failed,srealm=Passport.NET,ts=-1,prompt")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "<HTML><HEAD></HEAD><BODY>The handle or password is not right.</BODY></HTML>")
		return
	}
	w.Header().Set("Authentication-Info", "Passport1.4 da-status=success,tname=MSPAuth,tname=MSPProf,tname=MSPSec,"+
		"from-PP='"+s.newTicket(u)+"',ru=http://messenger.msn.com")
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

// rstRequest is what is read of an RST.srf request: the user, password,
// and the domains asked for, in order.
type rstRequest struct {
	user, pw string
	domains  []string
}

// parseRST reads an RST.srf request.
func parseRST(r io.Reader) (*rstRequest, error) {
	d := xml.NewDecoder(io.LimitReader(r, 64<<10))
	q := &rstRequest{}
	var path []string
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			path = append(path, t.Name.Local)
		case xml.EndElement:
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		case xml.CharData:
			if len(path) == 0 {
				continue
			}
			v := strings.TrimSpace(string(t))
			switch path[len(path)-1] {
			case "Username":
				q.user += v
			case "Password":
				q.pw += string(t)
			case "Address":
				if len(path) >= 3 && path[len(path)-2] == "EndpointReference" {
					q.domains = append(q.domains, v)
				}
			}
		}
	}
	return q, nil
}

const rstTime = "2006-01-02T15:04:05Z"

// rst is RST.srf: Passport SOAP (MSN 7.5), the password in the request; a
// token for each domain asked, the ticket for messenger.msn.com.
func (s *Server) rst(w http.ResponseWriter, r *http.Request, host string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	if r.Method != http.MethodPost {
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	q, err := parseRST(r.Body)
	var u *store.User
	if err == nil && q.user != "" {
		u = s.login(q.user, q.pw, host)
	}
	now := time.Now().UTC()
	head := `<?xml version="1.0" encoding="utf-8"?><S:Envelope xmlns:S="http://schemas.xmlsoap.org/soap/envelope/">` +
		`<S:Header><psf:pp xmlns:psf="http://schemas.microsoft.com/Passport/SoapServices/SOAPFault">` +
		`<psf:serverVersion>1</psf:serverVersion>`
	info := `<psf:serverInfo Path="Live1" RollingUpgradeState="ExclusiveNew" LocVersion="0" ServerTime="` +
		now.Format(rstTime) + `">D-Modem BBS</psf:serverInfo><psf:cookies/><psf:response/></psf:pp></S:Header>`
	var b strings.Builder
	b.WriteString(head)
	if u == nil {
		b.WriteString(`<psf:authstate>0x80048800</psf:authstate><psf:reqstatus>0x80048821</psf:reqstatus>` + info +
			`<S:Fault><faultcode>wsse:FailedAuthentication</faultcode>` +
			`<faultstring>Authentication Failure</faultstring></S:Fault></S:Envelope>`)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, b.String())
		return
	}
	fmt.Fprintf(&b, `<psf:PUID>%016X</psf:PUID><psf:configVersion>3.0.869.0</psf:configVersion>`+
		`<psf:uiVersion>3.0.869.0</psf:uiVersion><psf:authstate>0x48803</psf:authstate>`+
		`<psf:reqstatus>0x0</psf:reqstatus>`, u.ID)
	b.WriteString(info)
	b.WriteString(`<S:Body><wst:RequestSecurityTokenResponseCollection xmlns:wst="http://schemas.xmlsoap.org/ws/2004/04/trust"` +
		` xmlns:wsse="http://schemas.xmlsoap.org/ws/2003/06/secext"` +
		` xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"` +
		` xmlns:saml="urn:oasis:names:tc:SAML:1.0:assertion" xmlns:wsp="http://schemas.xmlsoap.org/ws/2002/12/policy"` +
		` xmlns:psf="http://schemas.microsoft.com/Passport/SoapServices/SOAPFault">`)
	life := `<wst:LifeTime><wsu:Created>` + now.Format(rstTime) + `</wsu:Created><wsu:Expires>` +
		now.Add(24*time.Hour).Format(rstTime) + `</wsu:Expires></wst:LifeTime>`
	secret := func() string {
		k := make([]byte, 24)
		copy(k, randomHex(12))
		return base64.StdEncoding.EncodeToString(k)
	}
	domains := q.domains
	if len(domains) == 0 {
		domains = []string{"messenger.msn.com"}
	}
	for i, d := range domains {
		applies := `<wsp:AppliesTo xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/03/addressing"><wsa:EndpointReference>` +
			`<wsa:Address>` + xmlText(d) + `</wsa:Address></wsa:EndpointReference></wsp:AppliesTo>`
		b.WriteString(`<wst:RequestSecurityTokenResponse>`)
		if d == "http://Passport.NET/tb" {
			id := fmt.Sprintf("BinaryDAToken%d", i)
			b.WriteString(`<wst:TokenType>urn:passport:legacy</wst:TokenType>` + applies + life +
				`<wst:RequestedSecurityToken><EncryptedData xmlns="http://www.w3.org/2001/04/xmlenc#" Id="` + id +
				`" Type="http://www.w3.org/2001/04/xmlenc#Element"><EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#tripledes-cbc"/>` +
				`<ds:KeyInfo xmlns:ds="http://www.w3.org/2000/09/xmldsig#"><ds:KeyName>http://Passport.NET/STS</ds:KeyName></ds:KeyInfo>` +
				`<CipherData><CipherValue>` + base64.StdEncoding.EncodeToString([]byte(randomHex(32))) + `</CipherValue></CipherData>` +
				`</EncryptedData></wst:RequestedSecurityToken><wst:RequestedTokenReference><wsse:KeyIdentifier ValueType="urn:passport"/>` +
				`<wsse:Reference URI="#` + id + `"/></wst:RequestedTokenReference><wst:RequestedProofToken><wst:BinarySecret>` +
				secret() + `</wst:BinarySecret></wst:RequestedProofToken>`)
		} else {
			id := fmt.Sprintf("Compact%d", i)
			tok := "t=" + randomHex(24) + "&p=" + randomHex(16)
			if d == "messenger.msn.com" {
				tok = s.newTicket(u)
			}
			b.WriteString(`<wst:TokenType>urn:passport:compact</wst:TokenType>` + applies + life +
				`<wst:RequestedSecurityToken><wsse:BinarySecurityToken Id="` + id + `">` + xmlText(tok) +
				`</wsse:BinarySecurityToken></wst:RequestedSecurityToken><wst:RequestedTokenReference>` +
				`<wsse:KeyIdentifier ValueType="urn:passport:compact"/><wsse:Reference URI="#` + id + `"/>` +
				`</wst:RequestedTokenReference>`)
		}
		b.WriteString(`</wst:RequestSecurityTokenResponse>`)
	}
	b.WriteString(`</wst:RequestSecurityTokenResponseCollection></S:Body></S:Envelope>`)
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, b.String())
}

// xmlText is s escaped for XML.
func xmlText(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
