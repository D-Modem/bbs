package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/config"
)

// Config is the whole configuration.
type Config struct {
	Settings  bbs.Settings
	Database  string
	Log       string // syslog, stderr, none, or a file
	Listeners []*Listener
	Warnings  []string
}

// DefaultListeners are used when the file has no [listen] section: telnet
// (and TN3270) for everyone, and app_dmodem's [bbs] default, a raw port on
// 127.0.0.1:2323, for modem callers.
func DefaultListeners() []*Listener {
	return []*Listener{
		{Name: "telnet", Kind: "telnet", Address: ":2300", Via: "telnet", TN3270: true,
			ANSI: "auto", UTF8: "auto", MaxPerIP: 4},
		{Name: "modem", Kind: "raw", Address: "127.0.0.1:2323", Via: "modem",
			ANSI: "auto", UTF8: "no", HeaderTimeout: 10 * time.Second},
	}
}

// Load reads the configuration file (path "" for none: the defaults).
func Load(path string) (*Config, error) {
	cf := &config.File{}
	if path != "" {
		var err error
		if cf, err = config.Load(path); err != nil {
			return nil, err
		}
	}
	c := &Config{Settings: bbs.Defaults(), Database: "bbs.db", Log: "stderr"}
	if err := c.general(cf.Get("general", "")); err != nil {
		return nil, fmt.Errorf("[general] %w", err)
	}
	names := map[string]bool{}
	for _, sec := range cf.All("listen") {
		l, err := listener(sec)
		if err != nil {
			return nil, fmt.Errorf("[listen %s] %w", sec.Name, err)
		}
		if names[l.Name] {
			return nil, fmt.Errorf("[listen %s] given twice", l.Name)
		}
		names[l.Name] = true
		c.Listeners = append(c.Listeners, l)
		c.warn(l)
	}
	if len(cf.Sections) > 0 && len(c.Listeners) == 0 {
		return nil, errors.New("no [listen ...] section: nothing to answer")
	}
	if len(c.Listeners) == 0 {
		c.Listeners = DefaultListeners()
	}
	for _, s := range cf.Sections {
		if s.Kind != "general" && s.Kind != "listen" {
			return nil, fmt.Errorf("line %d: unknown section [%s]", s.Line, s.Kind)
		}
	}
	for _, u := range cf.Unused() {
		c.Warnings = append(c.Warnings, "unknown option "+u)
	}
	return c, nil
}

func (c *Config) general(g *config.Section) error {
	s := &c.Settings
	var err error
	c.Database = g.String("database", c.Database)
	c.Log = g.String("log", c.Log)
	s.Name = g.String("name", s.Name)
	s.Sysop = g.String("sysop", s.Sysop)
	s.Location = g.String("location", s.Location)
	s.Banner = g.String("banner", s.Banner)
	if f := g.String("banner_file", ""); f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		s.Banner = strings.TrimRight(string(data), "\n")
	}
	s.MOTD = g.String("motd", s.MOTD)
	s.MOTDFile = g.String("motd_file", s.MOTDFile)
	s.Goodbye = g.String("goodbye", s.Goodbye)
	if s.NewUsers, err = g.Bool("new_users", s.NewUsers); err != nil {
		return err
	}
	ints := []struct {
		key      string
		p        *int
		min, max int
	}{
		{"max_nodes", &s.MaxNodes, 1, 1000},
		{"attempts", &s.Attempts, 1, 10},
		{"throttle_failures", &s.ThrottleFails, 0, 1000},
	}
	for _, i := range ints {
		if *i.p, err = g.Int(i.key, *i.p, i.min, i.max); err != nil {
			return err
		}
	}
	durs := []struct {
		key      string
		p        *time.Duration
		min, max int
	}{
		{"fail_delay", &s.FailDelay, 0, 60},
		{"login_timeout", &s.LoginTimeout, 10, 3600},
		{"idle_timeout", &s.IdleTimeout, 30, 86400},
		{"throttle_time", &s.ThrottleTime, 1, 86400},
	}
	for _, d := range durs {
		if *d.p, err = g.Seconds(d.key, int(*d.p/time.Second), d.min, d.max); err != nil {
			return err
		}
	}
	return nil
}

func listener(sec *config.Section) (*Listener, error) {
	if sec.Name == "" {
		return nil, errors.New("a listener needs a name: [listen NAME]")
	}
	l := &Listener{Name: sec.Name, Kind: strings.ToLower(sec.String("type", "telnet"))}
	switch l.Kind {
	case "telnet", "raw", "rlogin":
	default:
		return nil, fmt.Errorf("type: %q is not telnet, raw or rlogin", l.Kind)
	}
	l.Address = sec.String("address", "")
	if l.Address == "" {
		return nil, errors.New("address: none given")
	}
	if !l.Unix() {
		if _, _, err := net.SplitHostPort(l.Address); err != nil {
			return nil, fmt.Errorf("address: %v", err)
		}
	}
	via := map[string]string{"telnet": "telnet", "raw": "modem", "rlogin": "rlogin"}[l.Kind]
	l.Via = sec.String("via", via)
	var err error
	if l.TN3270, err = sec.Bool("tn3270", true); err != nil {
		return nil, err
	}
	if sec.Has("tn3270") && l.Kind != "telnet" {
		return nil, errors.New("tn3270: only for type = telnet")
	}
	for _, o := range []struct {
		key string
		p   *string
		def string
	}{{"ansi", &l.ANSI, "auto"}, {"utf8", &l.UTF8, map[bool]string{true: "auto", false: "no"}[l.Kind == "telnet"]}} {
		*o.p = strings.ToLower(sec.String(o.key, o.def))
		switch *o.p {
		case "auto", "yes", "no":
		default:
			return nil, fmt.Errorf("%s: %q is not auto, yes or no", o.key, *o.p)
		}
	}
	if l.Header, err = sec.Bool("header", false); err != nil {
		return nil, err
	}
	if l.HeaderTimeout, err = sec.Seconds("header_timeout", 10, 1, 300); err != nil {
		return nil, err
	}
	if l.TrustUser, err = sec.Bool("trust_user", false); err != nil {
		return nil, err
	}
	if l.TrustUser && !l.Header && l.Kind != "rlogin" {
		return nil, errors.New("trust_user: needs header = yes (or type = rlogin)")
	}
	if l.CreateUsers, err = sec.Bool("create_users", false); err != nil {
		return nil, err
	}
	if l.CreateUsers && !l.TrustUser {
		return nil, errors.New("create_users: only with trust_user = yes")
	}
	if l.MaxPerIP, err = sec.Int("max_per_ip", 4, 0, 1000); err != nil {
		return nil, err
	}
	mode := sec.String("socket_mode", "0660")
	m, err := strconv.ParseUint(mode, 8, 32)
	if err != nil || m > 0777 {
		return nil, fmt.Errorf("socket_mode: %q is not an octal mode", mode)
	}
	l.SocketMode = os.FileMode(m)
	l.SocketGroup = sec.String("socket_group", "")
	if (sec.Has("socket_mode") || l.SocketGroup != "") && !l.Unix() {
		return nil, errors.New("socket_mode, socket_group: only for a unix socket")
	}
	return l, nil
}

// warn notes what is dangerous in a listener.
func (c *Config) warn(l *Listener) {
	w := func(s string) { c.Warnings = append(c.Warnings, fmt.Sprintf("[listen %s] %s", l.Name, s)) }
	if l.TrustUser {
		switch {
		case l.Unix() && l.SocketMode&0007 != 0:
			w("trust_user with a socket anyone can connect to: anyone logs in as anyone")
		case !l.Unix() && !loopback(l.Address):
			w("trust_user on a port others can reach: anyone logs in as anyone; keep it to 127.0.0.1 or a unix socket")
		case !l.Unix():
			w("trust_user on a TCP port: every local user can log in as anyone; a unix socket's permissions are safer")
		}
	}
	if l.Kind == "rlogin" && !l.TrustUser && !l.Unix() && !loopback(l.Address) {
		w("rlogin sends the password in the clear")
	}
}

func loopback(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
