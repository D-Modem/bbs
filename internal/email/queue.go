package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"sort"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// Kick has the queue looked at now.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// RunQueue sends the queue's mail until ctx is done: when mail is put in
// it, and each minute for what is due again.
func (s *Service) RunQueue(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.RunQueueOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.kick:
		}
	}
}

// permanent is a refusal that trying again will not change (a 5xx reply).
type permanent struct{ error }

func (p permanent) Unwrap() error { return p.error }

// isPermanent reports whether err is a permanent failure.
func isPermanent(err error) bool {
	var p permanent
	if errors.As(err, &p) {
		return true
	}
	var te *textproto.Error
	return errors.As(err, &te) && te.Code >= 500
}

// retryAfter is how long to wait after the nth failure.
func retryAfter(n int) time.Duration {
	steps := []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour}
	if n < len(steps) {
		return steps[n]
	}
	return 4 * time.Hour
}

// RunQueueOnce tries what is due in the queue, once.
func (s *Service) RunQueueOnce(ctx context.Context) {
	due, err := s.B.Store.MailQueue(time.Now(), 100)
	if err != nil {
		s.B.Log.Printf("mail: queue: %v", err)
		return
	}
	for _, o := range due {
		if ctx.Err() != nil {
			return
		}
		raw, err := s.B.Store.MailRaw(o.MailID)
		if err != nil {
			s.B.Store.QueueDone(o.ID)
			continue
		}
		err = s.send(ctx, o, raw)
		switch {
		case err == nil:
			s.B.Store.QueueDone(o.ID)
			s.B.Log.Printf("mail: %d sent to %s", o.MailID, o.Rcpt)
		case isPermanent(err) || time.Since(o.Created) > s.Set.QueueLife:
			s.B.Store.QueueDone(o.ID)
			s.bounce(o, err.Error())
		default:
			s.B.Store.QueueRetry(o.ID, time.Now().Add(retryAfter(o.Attempts)), err.Error())
			s.B.Log.Printf("mail: %d for %s: %v (tried again later)", o.MailID, o.Rcpt, err)
		}
	}
}

// send sends one mail to one recipient, as the settings say.
func (s *Service) send(ctx context.Context, o *store.Outbound, raw []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	switch s.Set.Outbound {
	case OutboundSmarthost:
		return s.sendSmarthost(ctx, o, raw)
	case OutboundDirect:
		return s.sendDirect(ctx, o, raw)
	}
	return permanent{ErrNoInternet}
}

// hosts are the servers that take mail for a domain, in the order they
// are tried: its MX records (RFC 5321 5.1), or the domain itself.
func (s *Service) hosts(ctx context.Context, domain string) ([]string, error) {
	mxs, err := s.Set.Resolver.LookupMX(ctx, domain)
	if err != nil {
		var de *net.DNSError
		if errors.As(err, &de) && de.IsNotFound {
			return []string{domain}, nil
		}
		return nil, fmt.Errorf("looking up %s: %w", domain, err)
	}
	if len(mxs) == 1 && (mxs[0].Host == "." || mxs[0].Host == "") {
		return nil, permanent{fmt.Errorf("%s takes no mail (null MX)", domain)}
	}
	sort.SliceStable(mxs, func(i, j int) bool { return mxs[i].Pref < mxs[j].Pref })
	var out []string
	for _, mx := range mxs {
		out = append(out, strings.TrimSuffix(mx.Host, "."))
	}
	if len(out) == 0 {
		return []string{domain}, nil
	}
	return out, nil
}

// sendDirect sends a mail to its recipient's servers, each in turn until
// one takes it or refuses it for good.  TLS is used where a server offers
// it, its certificate unchecked: as most servers do (RFC 7435), since mail
// would otherwise go in the clear.
func (s *Service) sendDirect(ctx context.Context, o *store.Outbound, raw []byte) error {
	_, domain, ok := strings.Cut(o.Rcpt, "@")
	if !ok {
		return permanent{fmt.Errorf("%q is not an address", o.Rcpt)}
	}
	hosts, err := s.hosts(ctx, domain)
	if err != nil {
		return err
	}
	var last error
	for _, h := range hosts {
		err := s.sendTo(ctx, net.JoinHostPort(h, s.Set.MXPort), h, "starttls-opportunistic", nil, o, raw)
		if err == nil || isPermanent(err) {
			return err
		}
		last = err
	}
	return last
}

// sendSmarthost gives a mail to the smarthost.
func (s *Service) sendSmarthost(ctx context.Context, o *store.Outbound, raw []byte) error {
	sh := s.Set.Smarthost
	host, _, _ := net.SplitHostPort(sh.Address)
	var auth smtp.Auth
	if sh.Username != "" {
		auth = smtp.PlainAuth("", sh.Username, sh.Password, host)
	}
	return s.sendTo(ctx, sh.Address, host, sh.TLS, auth, o, raw)
}

// sendTo sends a mail on a connection to addr: TLS "implicit", "starttls"
// (required), "starttls-opportunistic", or "no".
func (s *Service) sendTo(ctx context.Context, addr, host, mode string, auth smtp.Auth, o *store.Outbound, raw []byte) error {
	d := net.Dialer{Timeout: 30 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	insecure := mode == "starttls-opportunistic" || s.Set.Smarthost.Insecure
	tc := &tls.Config{ServerName: host, InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12} //nolint:gosec // opportunistic TLS (RFC 7435), or as the sysop says
	if mode == "implicit" {
		conn = tls.Client(conn, tc)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Hello(s.Set.Hostname); err != nil {
		return err
	}
	if mode == "starttls" || mode == "starttls-opportunistic" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tc); err != nil {
				return err
			}
		} else if mode == "starttls" {
			return fmt.Errorf("%s offers no STARTTLS", addr)
		}
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(o.Sender); err != nil {
		return err
	}
	if err := c.Rcpt(o.Rcpt); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	c.Quit()
	return nil
}
