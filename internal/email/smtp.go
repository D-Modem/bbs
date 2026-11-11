package email

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/D-Modem/bbs/internal/store"
)

// The kinds of SMTP server.
const (
	Submission = "smtp" // users send mail (587, 465): AUTH needed
	MX         = "mx"   // mail from the internet for users here (25)
	LMTP       = "lmtp" // mail for users here from a mail server in front
)

// SMTPServer is an SMTP (or LMTP) server.
type SMTPServer struct {
	svc    *Service
	kind   string
	set    ServerSettings
	checks bool // MX: SPF, DKIM and DMARC are checked
	srv    *smtp.Server
	ln     *limited
}

// NewSMTP makes an SMTP server of a kind; checks: SPF, DKIM and DMARC
// checked on mail from the internet.
func (s *Service) NewSMTP(kind string, set ServerSettings, checks bool) (*SMTPServer, error) {
	tc, err := tlsConfig(set)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", kind, err)
	}
	x := &SMTPServer{svc: s, kind: kind, set: set, checks: checks}
	srv := smtp.NewServer(x)
	srv.Domain = s.Set.Hostname
	srv.MaxMessageBytes = s.Set.MaxMessage
	srv.MaxRecipients = s.Set.MaxRecipients
	srv.ReadTimeout = set.IdleTimeout
	srv.WriteTimeout = set.IdleTimeout
	srv.ErrorLog = s.B.Log
	srv.LMTP = kind == LMTP
	srv.AllowInsecureAuth = set.TLS == TLSNo || set.TLS == TLSOptional
	if set.TLS == TLSOptional || set.TLS == TLSRequired {
		srv.TLSConfig = tc
	}
	x.srv = srv
	x.ln, err = s.listen(kind, set, tc, func(why string) string { return "421 4.7.0 " + why + "\r\n" })
	if err != nil {
		return nil, err
	}
	return x, nil
}

// Run serves until ctx is done.
func (x *SMTPServer) Run(ctx context.Context) error {
	x.svc.B.Log.Printf("%s: listening on %s (tls %s)", x.kind, x.set.Address, x.set.TLS)
	go func() {
		<-ctx.Done()
		x.srv.Close()
	}()
	err := x.srv.Serve(x.ln)
	if ctx.Err() != nil || errors.Is(err, smtp.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return fmt.Errorf("%s: %w", x.kind, err)
}

// NewSession is a client's session (smtp.Backend).
func (x *SMTPServer) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &smtpSession{x: x, c: c, who: x.kind + " " + c.Conn().RemoteAddr().String()}, nil
}

// smtpSession is an SMTP client's connection.
type smtpSession struct {
	x     *SMTPServer
	c     *smtp.Conn
	who   string
	user  *store.User // submission: who logged in
	from  string
	rcpts []string
	local []*store.User
	bl    bool // the blocklists asked
}

func smtpErr(code int, enh smtp.EnhancedCode, msg string) *smtp.SMTPError {
	return &smtp.SMTPError{Code: code, EnhancedCode: enh, Message: msg}
}

var (
	errTLSFirst  = smtpErr(530, smtp.EnhancedCode{5, 7, 0}, "Use STARTTLS first")
	errAuthFirst = smtpErr(530, smtp.EnhancedCode{5, 7, 0}, "Authentication required")
	errNoRelay   = smtpErr(550, smtp.EnhancedCode{5, 7, 1}, "Mail for elsewhere is not taken here")
	errNoUser    = smtpErr(550, smtp.EnhancedCode{5, 1, 1}, "No such user here")
)

// tls reports whether the session is in TLS.
func (ss *smtpSession) tls() bool {
	_, ok := ss.c.TLSConnectionState()
	return ok
}

// AuthMechanisms are those offered: PLAIN and LOGIN, and NTLM while
// it's offered, for submission.
func (ss *smtpSession) AuthMechanisms() []string {
	if ss.x.kind != Submission {
		return nil
	}
	m := []string{sasl.Plain, sasl.Login}
	if ss.x.svc.ntlmOffered() {
		m = append(m, ntlmMechs...)
	}
	return m
}

// Auth is a mechanism's server.
func (ss *smtpSession) Auth(mech string) (sasl.Server, error) {
	if ss.x.kind != Submission {
		return nil, smtp.ErrAuthUnsupported
	}
	login := func(identity, name, password string) error {
		if identity != "" && identity != name {
			return smtp.ErrAuthFailed
		}
		u, err := ss.x.svc.Login(ss.who, ss.c.Conn().RemoteAddr(), "smtp", name, password)
		if err != nil {
			return smtp.ErrAuthFailed
		}
		ss.user = u
		ss.x.svc.B.Log.Printf("%s: %s logged in", ss.who, u.Name)
		return nil
	}
	if isNTLMMech(mech) && ss.x.svc.ntlmOffered() {
		return ss.x.svc.ntlmServer(ss.who, ss.c.Conn().RemoteAddr(), "smtp", func(u *store.User) {
			ss.user = u
			ss.x.svc.B.Log.Printf("%s: %s logged in by NTLM", ss.who, u.Name)
		}), nil
	}
	switch mech {
	case sasl.Plain:
		return sasl.NewPlainServer(login), nil
	case sasl.Login:
		return &loginServer{auth: login}, nil
	}
	return nil, smtp.ErrAuthUnsupported
}

// Mail is MAIL FROM.
func (ss *smtpSession) Mail(from string, opts *smtp.MailOptions) error {
	if ss.x.kind == Submission {
		if ss.user == nil {
			if !ss.tls() && ss.x.set.TLS == TLSRequired {
				return errTLSFirst
			}
			return errAuthFirst
		}
		if from != "" && !strings.EqualFold(from, ss.x.svc.Address(ss.user)) {
			return smtpErr(553, smtp.EnhancedCode{5, 7, 1}, "Send from your own address, "+ss.x.svc.Address(ss.user))
		}
	}
	if ss.x.kind == MX && len(ss.x.set.DNSBL) > 0 && !ss.bl {
		ss.bl = true
		ip := net.ParseIP(hostOf(ss.c.Conn().RemoteAddr()))
		if list, why := ss.x.svc.listedBy(ip, ss.x.set.DNSBL); list != "" {
			ss.x.svc.B.Log.Printf("%s: refused: %s is listed by %s %s", ss.who, ip, list, why)
			msg := fmt.Sprintf("%s is listed by %s", ip, list)
			if why != "" {
				msg += ": " + printable(why)
			}
			return smtpErr(554, smtp.EnhancedCode{5, 7, 1}, msg)
		}
	}
	ss.from = from
	return nil
}

// Rcpt is RCPT TO.
func (ss *smtpSession) Rcpt(to string, opts *smtp.RcptOptions) error {
	svc := ss.x.svc
	r, err := svc.resolve(to)
	if err != nil {
		if errors.Is(err, ErrNoUser) {
			return errNoUser
		}
		return smtpErr(553, smtp.EnhancedCode{5, 1, 3}, err.Error())
	}
	if ss.x.kind == Submission {
		if err := svc.checkRecipient(ss.user, r); err != nil {
			return smtpErr(550, smtp.EnhancedCode{5, 7, 1}, err.Error())
		}
		ss.rcpts = append(ss.rcpts, to)
		return nil
	}
	if r.kind != toLocal {
		return errNoRelay
	}
	ss.rcpts = append(ss.rcpts, to)
	ss.local = append(ss.local, r.user)
	return nil
}

// Data is DATA: the mail sent, or kept.
func (ss *smtpSession) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	svc := ss.x.svc
	if ss.x.kind == Submission {
		if err := svc.Submit(ss.user, ss.rcpts, raw); err != nil {
			if errors.Is(err, ErrNotYours) {
				return smtpErr(553, smtp.EnhancedCode{5, 7, 1}, err.Error())
			}
			return smtpErr(554, smtp.EnhancedCode{5, 6, 0}, err.Error())
		}
		return nil
	}
	trace := ss.received()
	if ss.x.checks {
		raw = withoutFields(raw, "Authentication-Results")
		ip := net.ParseIP(hostOf(ss.c.Conn().RemoteAddr()))
		v := svc.check(ip, ss.c.Hostname(), ss.from, raw)
		if v.refuse != "" {
			svc.B.Log.Printf("%s: refused mail from %s: %s", ss.who, ss.from, v.refuse)
			return smtpErr(550, smtp.EnhancedCode{5, 7, 1}, v.refuse)
		}
		trace += v.field
	}
	if err := svc.Receive(ss.local, raw, trace); err != nil {
		return smtpErr(451, smtp.EnhancedCode{4, 3, 0}, "Not kept: "+err.Error())
	}
	return nil
}

// received is the Received field for a mail taken from the session.
func (ss *smtpSession) received() string {
	with := "ESMTP"
	if ss.tls() {
		with = "ESMTPS"
	}
	if ss.x.kind == LMTP {
		with = "LMTP"
	}
	addr := ss.c.Conn().RemoteAddr().String()
	if h := hostOf(ss.c.Conn().RemoteAddr()); h != "" {
		addr = "[" + h + "]"
	}
	return fmt.Sprintf("Received: from %s (%s)\r\n\tby %s with %s; %s\r\n", sanitize(ss.c.Hostname()), addr,
		ss.x.svc.Set.Hostname, with, time.Now().Format(time.RFC1123Z))
}

// sanitize is a client's word fit for a header: printable ASCII, no
// spaces or parentheses.
func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if r <= ' ' || r > '~' || r == '(' || r == ')' {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return "unknown"
	}
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

// Reset forgets the mail begun.
func (ss *smtpSession) Reset() {
	ss.from, ss.rcpts, ss.local = "", nil, nil
}

// Logout ends the session.
func (ss *smtpSession) Logout() error { return nil }

// loginServer is SASL's LOGIN, which some mail programs use (Outlook).
type loginServer struct {
	auth func(identity, name, password string) error
	step int
	name string
}

func (l *loginServer) Next(response []byte) ([]byte, bool, error) {
	switch l.step {
	case 0:
		l.step = 1
		if len(response) == 0 {
			return []byte("Username:"), false, nil
		}
		fallthrough
	case 1:
		l.name = string(response)
		l.step = 2
		return []byte("Password:"), false, nil
	case 2:
		l.step = 3
		return nil, true, l.auth("", l.name, string(response))
	}
	return nil, true, sasl.ErrUnexpectedClientResponse
}
