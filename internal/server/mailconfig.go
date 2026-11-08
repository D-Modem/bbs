package server

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/email"
	"github.com/D-Modem/bbs/internal/news"
)

// MailServer is a mail server's section: [pop3], [imap], [smtp], [mx] or
// [lmtp].
type MailServer struct {
	Kind   string
	Set    email.ServerSettings
	Checks bool // [mx]: SPF, DKIM and DMARC checked
}

// mailServers are the mail servers' sections, their default addresses
// (unprivileged, as [nntp]'s; [mx] has to be on 25 to be found) and TLS.
var mailServers = []struct{ kind, addr, tls string }{
	{"pop3", ":1110", email.TLSRequired},
	{"imap", ":1143", email.TLSRequired},
	{email.Submission, ":1587", email.TLSRequired},
	{email.MX, ":25", email.TLSOptional},
	{email.LMTP, "", email.TLSNo},
}

// mailSections reads [mail], [smarthost] and the mail servers' sections.
// [mail] is needed for any of them; without it the BBS has no e-mail.
func (c *Config) mailSections(cf *config.File) error {
	m := cf.Get("mail", "")
	if m == nil {
		for _, ms := range mailServers {
			if cf.Get(ms.kind, "") != nil {
				return fmt.Errorf("[%s] needs a [mail] section (the domain, how mail goes out)", ms.kind)
			}
		}
		if cf.Get("smarthost", "") != nil {
			return errors.New("[smarthost] needs a [mail] section with outbound = smarthost")
		}
		return nil
	}
	set := email.Defaults(m.String("domain", c.News.Domain), m.String("hostname", c.News.PathHost))
	if !news.ValidMessageID("<x@"+set.Domain+">") || !strings.Contains(set.Domain, ".") {
		return errors.New("[mail] domain: a domain, like example.org")
	}
	if !news.ValidMessageID("<x@" + set.Hostname + ">") {
		return errors.New("[mail] hostname: a host name, like mail.example.org")
	}
	var err error
	if set.MaxMessage, err = parseSize(m, "max_message", set.MaxMessage); err != nil {
		return fmt.Errorf("[mail] %w", err)
	}
	if set.MaxRecipients, err = m.Int("max_recipients", set.MaxRecipients, 1, 1000); err != nil {
		return fmt.Errorf("[mail] %w", err)
	}
	if set.TrashDays, err = m.Int("trash_days", set.TrashDays, 0, 3650); err != nil {
		return fmt.Errorf("[mail] %w", err)
	}
	days, err := m.Int("queue_days", 5, 1, 30)
	if err != nil {
		return fmt.Errorf("[mail] %w", err)
	}
	set.QueueLife = time.Duration(days) * 24 * time.Hour
	set.Postmaster = m.String("postmaster", "")
	sh := cf.Get("smarthost", "")
	def := email.OutboundDirect
	if sh != nil {
		def = email.OutboundSmarthost
	}
	set.Outbound = strings.ToLower(m.String("outbound", def))
	switch set.Outbound {
	case email.OutboundDirect:
		c.Warnings = append(c.Warnings, "[mail] outbound = direct: mail goes straight to its servers on port 25; "+
			"many networks block that, and receivers want SPF, DKIM and a reverse DNS name for this host (or set a [smarthost])")
	case email.OutboundSmarthost:
		if sh == nil {
			return errors.New("[mail] outbound = smarthost: no [smarthost] section")
		}
		set.Smarthost = email.Smarthost{Address: sh.String("address", ""), TLS: strings.ToLower(sh.String("tls", "starttls")),
			Username: sh.String("username", ""), Password: sh.String("password", "")}
		if set.Smarthost.Insecure, err = sh.Bool("insecure", false); err != nil {
			return fmt.Errorf("[smarthost] %w", err)
		}
		if _, _, err := net.SplitHostPort(set.Smarthost.Address); err != nil {
			return fmt.Errorf("[smarthost] address: %v (host:port, like smtp.example.net:587)", err)
		}
		switch set.Smarthost.TLS {
		case "starttls", "implicit":
		case "no":
			if set.Smarthost.Password != "" {
				c.Warnings = append(c.Warnings, "[smarthost] tls = no: its password goes in the clear")
			}
		default:
			return fmt.Errorf("[smarthost] tls: %q is not starttls, implicit or no", set.Smarthost.TLS)
		}
	case email.OutboundNone:
	default:
		return fmt.Errorf("[mail] outbound: %q is not direct, smarthost or none", set.Outbound)
	}
	if key := m.String("dkim_key_file", ""); key != "" {
		signer, err := email.LoadDKIMKey(key)
		if err != nil {
			return fmt.Errorf("[mail] dkim_key_file: %w", err)
		}
		set.DKIM = &email.DKIM{Domain: set.Domain, Selector: m.String("dkim_selector", "bbs"), Signer: signer}
	} else if set.Outbound == email.OutboundDirect {
		c.Warnings = append(c.Warnings, "[mail] no dkim_key_file: mail sent is not signed, and many receivers will refuse it (bbs mail dkim makes a key)")
	}
	certFile, keyFile := m.String("cert_file", ""), m.String("key_file", "")
	c.Mail = &set
	for _, ms := range mailServers {
		s := cf.Get(ms.kind, "")
		if s == nil {
			continue
		}
		srv, err := mailServer(s, ms.kind, ms.addr, ms.tls, certFile, keyFile)
		if err != nil {
			return fmt.Errorf("[%s] %w", ms.kind, err)
		}
		switch {
		case srv.Set.TLS == email.TLSNo && ms.kind != email.MX && ms.kind != email.LMTP:
			c.Warnings = append(c.Warnings, fmt.Sprintf("[%s] tls = no: passwords go in the clear", ms.kind))
		case srv.Set.TLS == email.TLSOptional && ms.kind != email.MX && ms.kind != email.LMTP:
			c.Warnings = append(c.Warnings, fmt.Sprintf("[%s] tls = optional: mail programs that do not STARTTLS send passwords in the clear", ms.kind))
		}
		c.MailServers = append(c.MailServers, srv)
	}
	return nil
}

// mailServer reads a mail server's section.
func mailServer(s *config.Section, kind, addr, tls, certFile, keyFile string) (*MailServer, error) {
	ms := &MailServer{Kind: kind, Set: email.ServerSettings{Address: s.String("address", addr),
		TLS: strings.ToLower(s.String("tls", tls)), CertFile: s.String("cert_file", certFile), KeyFile: s.String("key_file", keyFile)}}
	set := &ms.Set
	if set.Address == "" {
		return nil, errors.New("address: none given (a unix socket's path, or host:port)")
	}
	if !strings.HasPrefix(set.Address, "/") {
		if _, _, err := net.SplitHostPort(set.Address); err != nil {
			return nil, fmt.Errorf("address: %v", err)
		}
	}
	switch set.TLS {
	case email.TLSNo, email.TLSOptional, email.TLSRequired, email.TLSImplicit:
	default:
		return nil, fmt.Errorf("tls: %q is not no, optional, required or implicit", set.TLS)
	}
	if set.TLS != email.TLSNo && (set.CertFile == "" || set.KeyFile == "") {
		return nil, errors.New("cert_file and key_file are needed for TLS (here or in [mail]), or tls = no")
	}
	var err error
	if set.MaxClients, err = s.Int("max_clients", 64, 1, 10000); err != nil {
		return nil, err
	}
	if set.MaxPerIP, err = s.Int("max_per_ip", 10, 0, 1000); err != nil {
		return nil, err
	}
	idle := 1800 // IMAP's IDLE wants 30 minutes
	if kind != "imap" {
		idle = 600
	}
	if set.IdleTimeout, err = s.Seconds("idle_timeout", idle, 30, 86400); err != nil {
		return nil, err
	}
	if kind == email.MX {
		if ms.Checks, err = s.Bool("checks", true); err != nil {
			return nil, err
		}
	}
	return ms, nil
}
