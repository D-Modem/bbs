// Package irclink links the BBS's chat into an IRC network as a hub
// server, by TS6: the server-to-server protocol of Solanum (Libera.Chat),
// charybdis, ircd-ratbox and ircd-hybrid (EFnet); or by P10, ircu's
// (Undernet).  It takes any number of server links, in and out, of either,
// and routes between them as a hub does.
//
// Every BBS user online, on a terminal, the web, IRC, AIM or any other way,
// is a user on the network, whom anyone there can /msg and /whois.  The
// BBS's rooms beginning with the prefix (irc- by default) are the network's
// channels: irc-retro is #retro, and talk in one is talk in the other.
//
// A nick taken on the network when a BBS user comes on, or found taken
// when a link comes up, is not fought over: the BBS user is renamed
// Nick|Tag, Tag being the BBS's short tag.  When two users of other
// servers collide, the TS6 rules apply (the newer nick is killed).
package irclink

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/ircd"
	"github.com/D-Modem/bbs/internal/sockets"
)

// Settings are the [irclink] section's.
type Settings struct {
	ServerName  string // the BBS's server name on the network (irc.bbs.example)
	SID         string // its TS6 server ID: a digit, then two letters or digits
	Description string
	Network     string // the network's name, as WHOIS shows it
	Tag         string // the short tag of a renamed nick (Nick|Tag)
	Host        string // the BBS users' host (default: the server name)
	Prefix      string // the rooms that are channels ("": every room but sysop-)
	NickLen     int    // the network's longest nick
	Address     string // where servers link in ("": nowhere)
	TLSAddress  string // the same, by TLS
	CertFile    string
	KeyFile     string
	Numeric     int // its P10 server numeric, 0 to 4095 (-1: none; needed for a P10 link)
	Peers       []Peer
	Retry       time.Duration // between tries of a link out (default 30s)
}

// Peer is a server the BBS links with ([ircpeer NAME]).
type Peer struct {
	Name           string // its server name
	Address        string // host:port to link out to ("": it links in)
	TLS            bool   // link out by TLS
	TLSVerify      bool   // check its certificate
	SendPassword   string // the password we send it
	AcceptPassword string // the password it must send us
	Tag            string // its users' tag, beside a BBS user of the same nick
	Hybrid         bool   // it is ircd-hybrid 8, whose TS6 differs (protocol = hybrid)
	P10            bool   // it is ircu, of P10 (protocol = p10)
}

var (
	sidRE  = regexp.MustCompile(`^[0-9][0-9A-Z]{2}$`)
	tagRE  = regexp.MustCompile(`^[A-Za-z0-9]{1,8}$`)
	nameRE = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
)

// Check checks the settings, filling in the defaults.
func (s *Settings) Check(bbsName string) error {
	if !nameRE.MatchString(s.ServerName) {
		return errors.New("server_name: a host-like name with a dot (irc.bbs.example)")
	}
	s.SID = strings.ToUpper(s.SID)
	if !sidRE.MatchString(s.SID) {
		return errors.New("sid: a digit and two letters or digits (1BB)")
	}
	if s.Description == "" {
		s.Description = bbsName
	}
	if s.Tag == "" {
		s.Tag = defaultTag(bbsName)
	}
	if !tagRE.MatchString(s.Tag) {
		return errors.New("tag: 1 to 8 letters or digits")
	}
	if s.Host == "" {
		s.Host = s.ServerName
	}
	if s.NickLen == 0 {
		s.NickLen = 30
	}
	if s.Numeric > 4095 {
		return errors.New("numeric: the P10 server numeric, 0 to 4095")
	}
	if s.Retry == 0 {
		s.Retry = 30 * time.Second
	}
	seen := map[string]bool{}
	for i := range s.Peers {
		p := &s.Peers[i]
		if !nameRE.MatchString(p.Name) {
			return fmt.Errorf("[ircpeer %s]: the section's name is the server's name", p.Name)
		}
		if seen[fold(p.Name)] || fold(p.Name) == fold(s.ServerName) {
			return fmt.Errorf("[ircpeer %s] given twice, or our own name", p.Name)
		}
		seen[fold(p.Name)] = true
		if p.AcceptPassword == "" || p.SendPassword == "" {
			return fmt.Errorf("[ircpeer %s]: send_password and accept_password", p.Name)
		}
		if strings.ContainsAny(p.SendPassword+p.AcceptPassword, " :") {
			return fmt.Errorf("[ircpeer %s]: a password has no spaces or colons", p.Name)
		}
		if p.Tag == "" {
			p.Tag = defaultTag(p.Name)
		}
		if !tagRE.MatchString(p.Tag) {
			return fmt.Errorf("[ircpeer %s] tag: 1 to 8 letters or digits", p.Name)
		}
		if p.P10 && s.Numeric < 0 {
			return fmt.Errorf("[ircpeer %s] protocol = p10: [irclink] needs its numeric", p.Name)
		}
	}
	return nil
}

// defaultTag is a name's first letters and digits, up to 4, as a tag.
func defaultTag(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r == '.' && b.Len() > 0 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
		if b.Len() == 4 {
			break
		}
	}
	if b.Len() == 0 {
		return "BBS"
	}
	return b.String()
}

// Hub is the BBS as a server on the network.
type Hub struct {
	B *bbs.BBS
	S Settings

	mu      sync.Mutex
	me      *server
	servers map[string]*server // by SID
	names   map[string]*server // by folded name
	users   map[string]*user   // by UID
	nicks   map[string]*user   // by folded nick
	chans   map[string]*channel
	local   map[string]*user // ours, by folded BBS name
	killed  map[string]bool  // BBS users killed off the network, until they come on again
	links   map[*link]bool
	nums    *numerics // P10's numerics of the network's servers and users
	nextUID int
	started time.Time
}

// New makes the hub (Check the settings first).
func New(b *bbs.BBS, s Settings) (*Hub, error) {
	if err := s.Check(b.Conf().Name); err != nil {
		return nil, err
	}
	h := &Hub{B: b, S: s, servers: map[string]*server{}, names: map[string]*server{},
		users: map[string]*user{}, nicks: map[string]*user{}, chans: map[string]*channel{},
		local: map[string]*user{}, killed: map[string]bool{}, links: map[*link]bool{}, nums: newNumerics(),
		started: time.Now()}
	h.me = &server{sid: s.SID, name: s.ServerName, desc: s.Description, tag: s.Tag}
	h.servers[h.me.sid] = h.me
	h.names[fold(h.me.name)] = h.me
	if s.Numeric >= 0 {
		h.nums.set(h.me.sid, toB64(s.Numeric, 2))
	}
	return h, nil
}

// Run links until ctx is done: it listens for servers linking in, links
// out to those with addresses (again whenever a link drops), and puts the
// BBS's users on the network as they come and go.
func (h *Hub) Run(ctx context.Context) error {
	stop := h.B.WatchPresence(h.presence)
	defer stop()
	seen := map[string]bool{}
	for _, n := range h.B.Nodes() {
		if n.User != "" && !seen[fold(n.User)] {
			seen[fold(n.User)] = true
			h.presence(n.User, true)
		}
	}
	var lns []net.Listener
	if h.S.Address != "" {
		ln, err := sockets.Listen("tcp", h.S.Address)
		if err != nil {
			return fmt.Errorf("irclink: %w", err)
		}
		lns = append(lns, ln)
	}
	if h.S.TLSAddress != "" {
		cfg, err := certs.Config(h.S.CertFile, h.S.KeyFile)
		if err != nil {
			return fmt.Errorf("irclink: certificate: %w", err)
		}
		ln, err := sockets.Listen("tcp", h.S.TLSAddress)
		if err != nil {
			return fmt.Errorf("irclink: %w", err)
		}
		lns = append(lns, tls.NewListener(ln, cfg))
	}
	var wg sync.WaitGroup
	for _, ln := range lns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				if h.B.Refuse("irclink", c.RemoteAddr()) {
					c.Close()
					continue
				}
				go h.serve(ctx, c, nil)
			}
		}()
	}
	for i := range h.S.Peers {
		if p := &h.S.Peers[i]; p.Address != "" {
			wg.Add(1)
			go func() { defer wg.Done(); h.linkOut(ctx, p) }()
		}
	}
	<-ctx.Done()
	for _, ln := range lns {
		ln.Close()
	}
	h.mu.Lock()
	for l := range h.links {
		l.sendRaw("ERROR :Shutting down")
		go l.finish()
	}
	h.mu.Unlock()
	wg.Wait()
	return nil
}

// linkOut links to a peer, and again after its link drops.
func (h *Hub) linkOut(ctx context.Context, p *Peer) {
	for ctx.Err() == nil {
		if !h.linked(p.Name) {
			d := net.Dialer{Timeout: 20 * time.Second}
			c, err := d.DialContext(ctx, "tcp", p.Address)
			if err == nil && p.TLS {
				host, _, _ := net.SplitHostPort(p.Address)
				tc := tls.Client(c, &tls.Config{ServerName: host, InsecureSkipVerify: !p.TLSVerify, MinVersion: tls.VersionTLS12})
				if err = tc.HandshakeContext(ctx); err != nil {
					c.Close()
				}
				c = tc
			}
			if err != nil {
				h.B.Log.Printf("irclink: %s: %v", p.Name, err)
			} else {
				h.serve(ctx, c, p)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(h.S.Retry):
		}
	}
}

// linked: a server of that name is on the network.
func (h *Hub) linked(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.names[fold(name)] != nil
}

// peer is the configured peer of a name (nil: none).
func (h *Hub) peer(name string) *Peer {
	for i := range h.S.Peers {
		if fold(h.S.Peers[i].Name) == fold(name) {
			return &h.S.Peers[i]
		}
	}
	return nil
}

// Links is how many servers are linked to the BBS directly, and how many
// are on the network besides it.
func (h *Hub) Links() (direct, network int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for l := range h.links {
		if l.srv != nil {
			direct++
		}
	}
	return direct, len(h.servers) - 1
}

// fold is a nick or channel as compared (rfc1459, as TS6 servers fold).
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r + 32
		case r == '[':
			return '{'
		case r == ']':
			return '}'
		case r == '\\':
			return '|'
		case r == '~':
			return '^'
		}
		return r
	}, s)
}

// nickOf is a BBS name as a nick on the network.
func nickOf(name string) string {
	n := ircd.Nick(name)
	if n == "" || n[0] >= '0' && n[0] <= '9' || n[0] == '-' {
		n = "_" + n
	}
	return n
}
