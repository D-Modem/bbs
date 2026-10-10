package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/news"
	"github.com/Neo-Desktop/bbs-sec/internal/nntp"
)

// newsSections reads [news], [nntp] and the [peer NAME]s.
func (c *Config) newsSections(cf *config.File) error {
	n := cf.Get("news", "")
	host := n.String("hostname", "")
	if host == "" {
		host, _ = os.Hostname()
		if !strings.Contains(host, ".") {
			host += ".invalid"
		}
	}
	c.News = bbs.NewsSettings{PathHost: host, Domain: n.String("domain", host),
		Organization: n.String("organization", c.Settings.Name)}
	if strings.ContainsAny(c.News.PathHost, "! \t") || !news.ValidMessageID("<x@"+c.News.Domain+">") {
		return errors.New("[news] hostname, domain: a host name, like news.example.org")
	}
	var err error
	if c.MaxArticle, err = parseSize(n, "max_article", 64<<10); err != nil {
		return fmt.Errorf("[news] %w", err)
	}

	if s := cf.Get("nntp", ""); s != nil {
		set := &nntp.ServerSettings{Address: s.String("address", ":1119")}
		if _, _, err := net.SplitHostPort(set.Address); err != nil {
			return fmt.Errorf("[nntp] address: %v", err)
		}
		set.TLS = strings.ToLower(s.String("tls", nntp.TLSNo))
		switch set.TLS {
		case nntp.TLSNo:
			c.Warnings = append(c.Warnings, "[nntp] tls = no: AUTHINFO passwords go in the clear")
		case nntp.TLSOptional:
			c.Warnings = append(c.Warnings, "[nntp] tls = optional: readers that do not STARTTLS send passwords in the clear")
		case nntp.TLSRequired, nntp.TLSImplicit:
		default:
			return fmt.Errorf("[nntp] tls: %q is not no, optional, required or implicit", set.TLS)
		}
		set.CertFile = s.String("cert_file", "")
		set.KeyFile = s.String("key_file", "")
		if set.TLS != nntp.TLSNo && (set.CertFile == "" || set.KeyFile == "") {
			return errors.New("[nntp] cert_file and key_file are needed for TLS")
		}
		switch r := strings.ToLower(s.String("read", "anyone")); r {
		case "anyone":
		case "users":
			set.ReadUsers = true
		default:
			return fmt.Errorf("[nntp] read: %q is not anyone or users", r)
		}
		if set.Post, err = s.Bool("post", true); err != nil {
			return fmt.Errorf("[nntp] %w", err)
		}
		if set.MaxClients, err = s.Int("max_clients", 32, 1, 10000); err != nil {
			return fmt.Errorf("[nntp] %w", err)
		}
		if set.MaxPerIP, err = s.Int("max_per_ip", 8, 0, 1000); err != nil {
			return fmt.Errorf("[nntp] %w", err)
		}
		if set.IdleTimeout, err = s.Seconds("idle_timeout", 600, 30, 86400); err != nil {
			return fmt.Errorf("[nntp] %w", err)
		}
		c.NNTP = set
	}

	names := map[string]bool{}
	for _, s := range cf.All("peer") {
		p, err := peer(s)
		if err != nil {
			return fmt.Errorf("[peer %s] %w", s.Name, err)
		}
		if names[p.Name] {
			return fmt.Errorf("[peer %s] given twice", p.Name)
		}
		names[p.Name] = true
		if p.TLS == "no" && p.Password != "" {
			c.Warnings = append(c.Warnings, fmt.Sprintf("[peer %s] its password goes in the clear (tls = no)", p.Name))
		}
		c.Peers = append(c.Peers, p)
	}
	return nil
}

func peer(s *config.Section) (*nntp.Peer, error) {
	if s.Name == "" {
		return nil, errors.New("a peer needs a name: [peer NAME]")
	}
	p := &nntp.Peer{Name: s.Name, Address: s.String("address", "")}
	var err error
	if p.Pull, err = s.Bool("pull", true); err != nil {
		return nil, err
	}
	if p.Push, err = s.Bool("push", true); err != nil {
		return nil, err
	}
	if (p.Pull || p.Push) && p.Address == "" {
		return nil, errors.New("address: none given (or pull = no and push = no)")
	}
	if p.Address != "" {
		if _, _, err := net.SplitHostPort(p.Address); err != nil {
			return nil, fmt.Errorf("address: %v", err)
		}
	}
	p.TLS = strings.ToLower(s.String("tls", "no"))
	switch p.TLS {
	case "no", "starttls", "implicit":
	default:
		return nil, fmt.Errorf("tls: %q is not no, starttls or implicit", p.TLS)
	}
	p.CAFile = s.String("ca_file", "")
	p.Username = s.String("username", "")
	p.Password = s.String("password", "")
	p.Groups = s.String("groups", "*")
	if p.Interval, err = s.Seconds("interval", 600, 60, 86400); err != nil {
		return nil, err
	}
	if p.MaxPerRun, err = s.Int("max_per_run", 1000, 1, 100000); err != nil {
		return nil, err
	}
	if p.Initial, err = s.Int("initial", 100, 0, 100000); err != nil {
		return nil, err
	}
	for _, h := range strings.Fields(strings.ReplaceAll(s.String("accept_from", ""), ",", " ")) {
		if ip := net.ParseIP(h); ip != nil {
			p.AcceptFrom = append(p.AcceptFrom, ip)
			continue
		}
		ips, err := net.LookupIP(h)
		if err != nil {
			return nil, fmt.Errorf("accept_from %s: %v", h, err)
		}
		p.AcceptFrom = append(p.AcceptFrom, ips...)
	}
	return p, nil
}
