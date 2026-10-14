package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/Neo-Desktop/bbs-sec/internal/api"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/door"
	"github.com/Neo-Desktop/bbs-sec/internal/events"
	"github.com/Neo-Desktop/bbs-sec/internal/fido"
	"github.com/Neo-Desktop/bbs-sec/internal/finger"
	"github.com/Neo-Desktop/bbs-sec/internal/ftp"
	"github.com/Neo-Desktop/bbs-sec/internal/gopher"
	"github.com/Neo-Desktop/bbs-sec/internal/guard"
	"github.com/Neo-Desktop/bbs-sec/internal/imageboard"
	"github.com/Neo-Desktop/bbs-sec/internal/ircd"
	"github.com/Neo-Desktop/bbs-sec/internal/logo"
	"github.com/Neo-Desktop/bbs-sec/internal/mrc"
	"github.com/Neo-Desktop/bbs-sec/internal/nntp"
	"github.com/Neo-Desktop/bbs-sec/internal/phone"
	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
	"github.com/Neo-Desktop/bbs-sec/internal/qwknet"
	"github.com/Neo-Desktop/bbs-sec/internal/screens"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/ui3270"
)

// Config is the whole configuration.
type Config struct {
	Settings bbs.Settings
	Graphics ui3270.Graphics
	LogoFile string // [general] logo: a PNG, for sixel terminals and 3270s

	FilesDir     string // file areas' files
	MaxFileSize  int64
	UploadQuota  int64         // a user's uploads a day (0: no limit)
	FTP          *ftp.Settings // nil: no FTP server
	FTPAdvertise string        // the FTP server's address, as users are told it

	News       bbs.NewsSettings
	MaxArticle int64
	NNTP       *nntp.ServerSettings // nil: no NNTP server
	Peers      []*nntp.Peer
	Fido       *fido.Settings // nil: not on FidoNet
	QWKNets    []*qwknet.Net  // [qwknet NAME]: QWK networks this is a node of
	Binkp      *BinkpSettings // nil: no binkp answering
	Doors      []*door.Door
	Finger     *finger.Settings // nil: no finger server
	IRC        *ircd.Settings   // nil: no IRC server
	MRC        *mrc.Settings    // nil: not on Multi Relay Chat
	Screens    *screens.Set     // nil: no screens of the sysop's
	ScriptsDir string           // "": no scripts
	Menus      map[string]*bbs.Menu
	Text       map[string]string // [strings]
	HTTP       *api.Settings     // nil: no HTTP API
	Chan       imageboard.Settings
	Gopher     *gopher.Settings // nil: no gopher server
	Database   string
	Log        string // syslog, stderr, none, or a file
	Listeners  []*Listener
	Warnings   []string
	Guard      *guard.Guard // [security]
	Events     []*events.Event
}

// DefaultListeners are used when the file has no [listen] section: telnet
// (and TN3270) for everyone, and app_dmodem's [bbs] default, a raw port on
// 127.0.0.1:2323, for modem callers.
func DefaultListeners() []*Listener {
	return []*Listener{
		{Name: "telnet", Kind: "telnet", Address: ":2300", Via: "telnet", TN3270: true,
			ANSI: "auto", UTF8: "auto", Sixel: "auto", RIP: "auto", PETSCII: "no", VT: "auto", MaxPerIP: 4},
		{Name: "modem", Kind: "raw", Address: "127.0.0.1:2323", Via: "modem",
			ANSI: "auto", UTF8: "no", Sixel: "no", RIP: "auto", PETSCII: "no", VT: "auto", HeaderTimeout: 10 * time.Second},
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
	c := &Config{Settings: bbs.Defaults(), Database: "bbs.db", Log: "stderr", FilesDir: "files"}
	if err := c.general(cf.Get("general", "")); err != nil {
		return nil, fmt.Errorf("[general] %w", err)
	}
	if err := c.files(cf.Get("general", "")); err != nil {
		return nil, fmt.Errorf("[general] %w", err)
	}
	if err := c.newsSections(cf); err != nil {
		return nil, err
	}
	if err := c.fidoSections(cf); err != nil {
		return nil, err
	}
	if err := c.qwkNets(cf); err != nil {
		return nil, err
	}
	if err := c.ircSection(cf); err != nil {
		return nil, err
	}
	if err := c.mrcSection(cf); err != nil {
		return nil, err
	}
	if err := c.infoSections(cf); err != nil {
		return nil, err
	}
	if err := c.levelSections(cf); err != nil {
		return nil, err
	}
	if err := c.doorSections(cf); err != nil {
		return nil, err
	}
	if err := c.chanSection(cf.Get("chan", "")); err != nil {
		return nil, fmt.Errorf("[chan] %w", err)
	}
	if err := c.httpSection(cf.Get("http", "")); err != nil {
		return nil, fmt.Errorf("[http] %w", err)
	}
	if err := c.customSections(cf); err != nil {
		return nil, err
	}
	if err := c.ftpSection(cf.Get("ftp", "")); err != nil {
		return nil, fmt.Errorf("[ftp] %w", err)
	}
	if err := c.securitySection(cf.Get("security", "")); err != nil {
		return nil, fmt.Errorf("[security] %w", err)
	}
	for _, sec := range cf.All("event") {
		e, err := c.event(sec)
		if err != nil {
			return nil, fmt.Errorf("[event %s] %w", sec.Name, err)
		}
		for _, o := range c.Events {
			if o.Name == e.Name {
				return nil, fmt.Errorf("[event %s] given twice", e.Name)
			}
		}
		c.Events = append(c.Events, e)
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
		if l.Kind == "ssh" && len(l.HostKeys) == 0 {
			l.HostKeys = DefaultHostKeys(c.DataDir())
		}
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
		switch s.Kind {
		case "general", "security", "event", "listen", "ftp", "news", "nntp", "peer", "qwknet", "fido", "binkp", "link", "door", "finger", "gopher", "irc", "mrc", "menu", "strings", "http", "level", "chan":
		default:
			return nil, fmt.Errorf("line %d: unknown section [%s]", s.Line, s.Kind)
		}
	}
	for _, u := range cf.Unused() {
		c.Warnings = append(c.Warnings, "unknown option "+u)
	}
	return c, nil
}

// DataDir is where the BBS keeps what it makes (host keys): beside an
// SQLite database, or here for a server's.
func (c *Config) DataDir() string {
	if store.IsFile(c.Database) {
		return filepath.Dir(strings.TrimPrefix(c.Database, "sqlite:"))
	}
	return "."
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
	if s.GeoIP = g.String("geoip", ""); s.GeoIP != "" {
		if _, err := os.Stat(s.GeoIP); err != nil {
			c.Warnings = append(c.Warnings, fmt.Sprintf("geoip: %v: callers' countries are not known until it is there", err))
		}
	}
	s.GeoCredit, s.GeoCreditURL = g.String("geoip_credit", ""), g.String("geoip_credit_url", "")
	if u := s.GeoCreditURL; u != "" && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return fmt.Errorf("geoip_credit_url: an http(s) address, not %q", u)
	}
	if s.PhoneCountry = strings.ToUpper(g.String("phone_country", "")); s.PhoneCountry != "" && !phone.Known(s.PhoneCountry) {
		return fmt.Errorf("phone_country: %q is not a country with a calling code (an ISO 3166 code: US, GB, DE)", s.PhoneCountry)
	}
	s.QWKID = strings.ToUpper(g.String("qwk_id", s.QWKID))
	if s.QWKID != "" && !qwk.ValidID(s.QWKID) {
		return errors.New("qwk_id: 1 to 8 letters and digits")
	}
	if s.PageCommand, err = door.Split(g.String("page_command", "")); err != nil {
		return fmt.Errorf("page_command: %w", err)
	}
	if s.NewUsers, err = g.Bool("new_users", s.NewUsers); err != nil {
		return err
	}
	if c.Graphics.Symbols, err = g.Bool("tn3270_symbols", true); err != nil {
		return err
	}
	if c.Graphics.Vector, err = g.Bool("tn3270_vector", false); err != nil {
		return err
	}
	c.LogoFile = g.String("logo", "")
	// 3270s may have a logo of their own (one-color, cut into cells)
	c.Graphics.LogoFile = g.String("tn3270_logo", c.LogoFile)
	for _, f := range []string{c.LogoFile, c.Graphics.LogoFile} {
		if f != "" {
			if _, err := logo.Load(f); err != nil {
				return fmt.Errorf("logo %s: %w", f, err)
			}
		}
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

// event reads an [event NAME]: a program run daily at a time, or every so
// many minutes.
func (c *Config) event(sec *config.Section) (*events.Event, error) {
	if sec.Name == "" {
		return nil, errors.New("an event needs a name: [event backup]")
	}
	e := &events.Event{Name: strings.ToLower(sec.Name), At: sec.String("at", "")}
	cmd, err := events.Split(sec.String("command", ""))
	if err != nil {
		return nil, fmt.Errorf("command: %v", err)
	}
	if len(cmd) == 0 {
		return nil, errors.New("command: none given (a program and its arguments; no shell)")
	}
	if _, err := exec.LookPath(cmd[0]); err != nil {
		c.Warnings = append(c.Warnings, fmt.Sprintf("[event %s] command: %v", e.Name, err))
	}
	e.Command = cmd
	mins, err := sec.Int("every", 0, 0, 7*24*60)
	if err != nil {
		return nil, err
	}
	e.Every = events.Minutes(mins)
	switch {
	case e.At != "" && e.Every > 0:
		return nil, errors.New("at or every, not both")
	case e.At != "":
		if err := events.ParseAt(e.At); err != nil {
			return nil, fmt.Errorf("at: %v", err)
		}
	case e.Every <= 0:
		return nil, errors.New("at = HH:MM (daily) or every = MINUTES")
	}
	if e.Timeout, err = sec.Seconds("timeout", 600, 1, 24*3600); err != nil {
		return nil, err
	}
	return e, nil
}

// securitySection reads [security]: the deny files, the hack log, bans.
func (c *Config) securitySection(sec *config.Section) error {
	g := &guard.Guard{}
	c.Guard = g
	if sec == nil {
		return nil
	}
	g.DenyFile = sec.String("deny_file", "")
	g.NameDenyFile = sec.String("name_deny_file", "")
	g.HackLog = sec.String("hack_log", "")
	mins, err := sec.Int("ban_minutes", 0, 0, 7*24*60)
	if err != nil {
		return err
	}
	g.BanTime = time.Duration(mins) * time.Minute
	for _, w := range g.Check() {
		c.Warnings = append(c.Warnings, "[security] "+w)
	}
	return nil
}

func listener(sec *config.Section) (*Listener, error) {
	if sec.Name == "" {
		return nil, errors.New("a listener needs a name: [listen NAME]")
	}
	l := &Listener{Name: sec.Name, Kind: strings.ToLower(sec.String("type", "telnet"))}
	switch l.Kind {
	case "telnet", "raw", "rlogin", "ssh":
	default:
		return nil, fmt.Errorf("type: %q is not telnet, raw, rlogin or ssh", l.Kind)
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
	if l.Kind == "ssh" && l.Unix() {
		return nil, errors.New("address: ssh is for a TCP port")
	}
	via := map[string]string{"telnet": "telnet", "raw": "modem", "rlogin": "rlogin", "ssh": "ssh"}[l.Kind]
	l.Via = sec.String("via", via)
	var err error
	if l.TN3270, err = sec.Bool("tn3270", l.Kind == "telnet"); err != nil {
		return nil, err
	}
	if sec.Has("tn3270") && l.Kind != "telnet" {
		return nil, errors.New("tn3270: only for type = telnet")
	}
	for _, o := range []struct {
		key string
		p   *string
		def string
	}{{"ansi", &l.ANSI, "auto"}, {"utf8", &l.UTF8, map[bool]string{true: "auto", false: "no"}[l.Kind == "telnet" || l.Kind == "ssh"]},
		{"sixel", &l.Sixel, map[bool]string{true: "auto", false: "no"}[l.Kind == "telnet" || l.Kind == "ssh"]},
		{"petscii", &l.PETSCII, "no"}, {"rip", &l.RIP, "auto"}, {"vt", &l.VT, "auto"}} {
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
	if l.Kind == "ssh" && (l.Header || l.TrustUser) {
		return nil, errors.New("header, trust_user: not for ssh (SSH logs its users in itself)")
	}
	if l.HostKeys = strings.Fields(sec.String("host_keys", "")); len(l.HostKeys) > 0 && l.Kind != "ssh" {
		return nil, errors.New("host_keys: only for type = ssh")
	}
	if l.Legacy, err = sec.Bool("legacy", false); err != nil {
		return nil, err
	}
	l.CertFile, l.KeyFile = sec.String("tls_cert", ""), sec.String("tls_key", "")
	if (l.CertFile == "") != (l.KeyFile == "") {
		return nil, errors.New("tls_cert and tls_key: both, or neither")
	}
	if l.CertFile != "" && (l.Kind != "telnet" && l.Kind != "raw" || l.Unix()) {
		return nil, errors.New("tls_cert: only for telnet and raw listeners on a TCP port")
	}
	if l.CertFile != "" {
		if _, err := tls.LoadX509KeyPair(l.CertFile, l.KeyFile); err != nil {
			return nil, fmt.Errorf("tls_cert, tls_key: %v", err)
		}
	}
	if sec.Has("legacy") && l.Kind != "ssh" {
		return nil, errors.New("legacy: only for type = ssh")
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
	if l.Kind == "ssh" && l.Legacy {
		w("legacy offers ciphers and a key exchange that are weak (CBC, 1024-bit DH): only for clients that need them")
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
