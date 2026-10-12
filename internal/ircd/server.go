// Package ircd is an IRC server for the BBS's chat: its channels are the
// chat rooms, so IRC clients talk with callers on terminals, the web and
// the API.  Callers log in with their BBS names and passwords (SASL PLAIN
// or PASS); their nicks are their names.
//
// The protocol's parts are ergo's (github.com/ergochat/ergo): messages
// are read and written by its irc-go library, and the numerics and
// ISUPPORT lists are taken from its source (see LICENSE.ergo).
package ircd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/ircd/isupport"
	"github.com/Neo-Desktop/bbs-sec/internal/tcpserv"
)

// Settings are the [irc] section's.
type Settings struct {
	Address    string // plain IRC ("": none)
	TLSAddress string // IRC over TLS ("": none)
	CertFile   string
	KeyFile    string
	ServerName string // the server's name to clients (default: the host's name)
	Network    string // the network's name (default: the BBS's)
	MaxClients int
	MaxPerIP   int
	Service    string // the services bot's nick (default BBSServ)
}

// Server serves IRC.
type Server struct {
	B *bbs.BBS
	S Settings

	created time.Time
	support *isupport.List
	svc     *service // BBSServ
	mu      sync.Mutex
	clients map[*client]bool
}

// New checks the settings and makes the server.
func New(b *bbs.BBS, s Settings) (*Server, error) {
	if s.Address == "" && s.TLSAddress == "" {
		return nil, errors.New("irc: no address to listen on")
	}
	if s.TLSAddress != "" {
		if _, err := tls.LoadX509KeyPair(s.CertFile, s.KeyFile); err != nil {
			return nil, fmt.Errorf("irc: certificate: %w", err)
		}
	}
	if s.ServerName == "" {
		s.ServerName, _ = os.Hostname()
		if s.ServerName == "" || !strings.Contains(s.ServerName, ".") {
			s.ServerName = "irc.bbs"
		}
	}
	if s.Network == "" {
		s.Network = strings.Map(func(r rune) rune {
			if r == ' ' {
				return '-'
			}
			return r
		}, b.Name)
	}
	if s.Service == "" {
		s.Service = "BBSServ"
	}
	if !ValidNick(s.Service) {
		return nil, fmt.Errorf("irc: service %q: a nick is letters, digits, - and _", s.Service)
	}
	srv := &Server{B: b, S: s, created: time.Now(), clients: map[*client]bool{}, svc: newBBSServ(s.Service)}
	srv.support = isupport.NewList()
	for k, v := range map[string]string{
		"CASEMAPPING": "ascii",
		"CHANTYPES":   "#",
		"CHANNELLEN":  "21",
		"CHANLIMIT":   "#:20",
		"NICKLEN":     strconv.Itoa(bbs.MaxNameLen),
		"NETWORK":     s.Network,
		"PREFIX":      "(o)@",
		"CHANMODES":   ",,,nt",
		"MODES":       "1",
		"TOPICLEN":    "0",
		"TARGMAX":     "PRIVMSG:1,NOTICE:1,JOIN:,PART:,NAMES:1,WHOIS:1",
		"LINELEN":     "512",
		"UTF8ONLY":    "",
		"SAFELIST":    "",
	} {
		srv.support.Add(k, v)
	}
	if err := srv.support.RegenerateCachedReply(); err != nil {
		return nil, err
	}
	return srv, nil
}

// Run listens until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	ts := &tcpserv.Server{Name: "irc", MaxClients: s.S.MaxClients, MaxPerIP: s.S.MaxPerIP, Log: s.B.Log,
		Handle: s.handle,
		Busy: func(c net.Conn) {
			fmt.Fprintf(c, "ERROR :Too many connections; try again later\r\n")
		},
		Refuse: func(c net.Conn) bool { return s.B.Refuse("irc", c.RemoteAddr()) }}
	var lns []net.Listener
	if s.S.Address != "" {
		ln, err := net.Listen("tcp", s.S.Address)
		if err != nil {
			return fmt.Errorf("irc: %w", err)
		}
		lns = append(lns, ln)
	}
	if s.S.TLSAddress != "" {
		cert, err := tls.LoadX509KeyPair(s.S.CertFile, s.S.KeyFile)
		if err != nil {
			return fmt.Errorf("irc: certificate: %w", err)
		}
		ln, err := tls.Listen("tcp", s.S.TLSAddress, &tls.Config{Certificates: []tls.Certificate{cert},
			MinVersion: tls.VersionTLS12})
		if err != nil {
			for _, l := range lns {
				l.Close()
			}
			return fmt.Errorf("irc: %w", err)
		}
		lns = append(lns, ln)
	}
	errs := make(chan error, len(lns))
	for _, ln := range lns {
		go func() { errs <- ts.Serve(ctx, ln) }()
	}
	var err error
	for range lns {
		if e := <-errs; e != nil && err == nil {
			err = e
		}
	}
	return err
}

// Clients is how many are connected.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

// Nick is a BBS name as an IRC nick: what nicks may not have (spaces,
// dots) as underscores.
func Nick(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, name)
}

// numerics ergo's list lacks
const (
	RPL_WHOISSPECIAL = "320"
	RPL_LISTSTART    = "321"
)

// ValidNick: a nick as Nick makes them (a letter first).
func ValidNick(nick string) bool {
	if nick == "" || len(nick) > 30 || !(nick[0] >= 'a' && nick[0] <= 'z' || nick[0] >= 'A' && nick[0] <= 'Z') {
		return false
	}
	return Nick(nick) == nick
}

// fold is a nick or channel as compared (CASEMAPPING=ascii).
func fold(s string) string { return strings.ToLower(s) }
