// Package email is the BBS's e-mail: its users' private mail by POP3 and
// IMAP, mail sent by SMTP submission, mail from the internet taken by SMTP
// (or LMTP, from a mail server in front), and mail for elsewhere sent from
// a queue, straight to the recipients' servers or through a smarthost.
//
// Mail is kept in the BBS's store, whatever its database: once, with each
// user's entries (store.DeliverMail), shown in folders (store.FolderNames)
// that are views of those entries.
package email

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/ftn"
	"github.com/D-Modem/bbs/internal/store"
)

// How mail for elsewhere goes.
const (
	OutboundDirect    = "direct"    // to the recipients' servers (their MX)
	OutboundSmarthost = "smarthost" // through another mail server
	OutboundNone      = "none"      // not at all: mail only here
)

// Settings are [mail]'s.
type Settings struct {
	Domain        string // users' addresses: name@Domain
	Hostname      string // this server's name (HELO, Received, Message-IDs)
	MaxMessage    int64  // bytes
	MaxRecipients int
	TrashDays     int    // days mail stays in the trash (0: for ever)
	Postmaster    string // the user postmaster@ and abuse@ go to ("": the first sysop)
	Outbound      string
	Smarthost     Smarthost
	DKIM          *DKIM         // nil: mail sent is not signed
	QueueLife     time.Duration // how long a mail is tried before it is given up
	// Resolver looks up MX, SPF, DKIM and DMARC records (nil: the
	// system's)
	Resolver Resolver
	// MXPort is the port mail is sent to straight to its servers ("25";
	// another in tests)
	MXPort string
}

// Smarthost is a mail server mail for elsewhere is given to.
type Smarthost struct {
	Address  string // host:port
	TLS      string // "starttls" (the default), "implicit" or "no"
	Username string // "": no AUTH
	Password string
	Insecure bool // its certificate is not checked
}

// DKIM is the key mail sent is signed with.
type DKIM struct {
	Domain   string
	Selector string
	Signer   crypto.Signer
}

// Resolver is DNS: the net package's, or a test's.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

// Defaults are the settings not given.
func Defaults(domain, hostname string) Settings {
	return Settings{Domain: domain, Hostname: hostname, MaxMessage: 10 << 20, MaxRecipients: 50,
		TrashDays: 30, Outbound: OutboundDirect, QueueLife: 5 * 24 * time.Hour, MXPort: "25"}
}

// Service is the e-mail of a BBS: its addresses and its deliveries.
type Service struct {
	B   *bbs.BBS
	Set Settings

	kick chan struct{} // the queue has new mail

	mu      sync.Mutex
	watches map[int64][]chan struct{} // IDLE's, by user
}

// New makes the service, and hooks it to the BBS: e-mail sent from the
// terminals, and mail delivered there told to IDLE.
func New(b *bbs.BBS, set Settings) *Service {
	if set.Resolver == nil {
		set.Resolver = net.DefaultResolver
	}
	if set.MXPort == "" {
		set.MXPort = "25"
	}
	s := &Service{B: b, Set: set, kick: make(chan struct{}, 1), watches: map[int64][]chan struct{}{}}
	b.Email = s.fromTerminal
	b.MailWatch = s.notify
	return s
}

// Address is a user's e-mail address.
func (s *Service) Address(u *store.User) string {
	return bbs.LocalPart(u.Name) + "@" + s.Set.Domain
}

// AddressOf is the address of a user by name.
func (s *Service) AddressOf(name string) string {
	return bbs.LocalPart(name) + "@" + s.Set.Domain
}

// Mailbox is a user's address with their name: "Alice Smith" <alice.smith@...>.
func (s *Service) Mailbox(name string) string {
	return (&mail.Address{Name: name, Address: s.AddressOf(name)}).String()
}

// ours reports whether a domain is this BBS's.
func (s *Service) ours(domain string) bool {
	domain = strings.TrimSuffix(strings.ToLower(domain), ".")
	return domain == strings.ToLower(s.Set.Domain) || domain == strings.ToLower(s.Set.Hostname)
}

// role addresses go to the postmaster, whoever has the name
var roleAddresses = map[string]bool{"postmaster": true, "abuse": true, "mailer-daemon": true, "hostmaster": true, "webmaster": true}

// ErrNoUser: no user here has that address.
var ErrNoUser = errors.New("no such user here")

// LocalUser is the user an address here is for: by their name's local
// part (the oldest account, if two names make the same), "+tag" ignored;
// postmaster@ and the like are the postmaster's.
func (s *Service) LocalUser(local string) (*store.User, error) {
	local = strings.ToLower(local)
	if i := strings.IndexByte(local, '+'); i > 0 {
		local = local[:i]
	}
	if roleAddresses[local] {
		return s.postmaster()
	}
	users, err := s.B.Store.Users()
	if err != nil {
		return nil, err
	}
	var best *store.User
	for _, u := range users {
		if bbs.LocalPart(u.Name) == local && (best == nil || u.ID < best.ID) {
			best = u
		}
	}
	if best == nil || best.Locked {
		return nil, ErrNoUser
	}
	return best, nil
}

// postmaster is the user the role addresses go to.
func (s *Service) postmaster() (*store.User, error) {
	if s.Set.Postmaster != "" {
		return s.B.Store.UserByName(s.Set.Postmaster)
	}
	users, err := s.B.Store.Users()
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.Sysop {
			return u, nil
		}
	}
	return nil, ErrNoUser
}

// Login finds the user a mail program logs in as: by name, or by address
// here; the password is checked as the BBS's are, throttled.
func (s *Service) Login(who string, remote net.Addr, service, name, password string) (*store.User, error) {
	if strings.Contains(name, "@") {
		local, domain, _ := strings.Cut(name, "@")
		if !s.ours(domain) {
			return nil, ErrNoUser
		}
		if u, err := s.LocalUser(local); err == nil && !roleAddresses[strings.ToLower(local)] {
			name = u.Name
		}
	}
	// a local client's address is everyone's: its failures count
	// against the account named
	source := bbs.LocalSource(service, name)
	if a, ok := remote.(*net.TCPAddr); ok && !a.IP.IsLoopback() {
		source = "ip:" + a.IP.String()
	}
	return s.B.Authenticate(who, source, name, password)
}

// mayMail reports whether u's access lets them mail anyone (else only the
// sysop).
func (s *Service) mayMail(u *store.User) bool { return s.B.LevelOf(u).Mail }

// Kinds of recipient.
const (
	toLocal = iota
	toFido
	toInternet
)

// recipient is an address read.
type recipient struct {
	kind int
	addr string // as given, cleaned
	user *store.User
	name string // FidoNet's
	ftn  ftn.Addr
}

// fidoDomain is a FidoNet gateway's domain: [p4.]f100.n1.z21.fidonet[.org]
var fidoDomain = regexp.MustCompile(`^(?:p(\d+)\.)?f(\d+)\.n(\d+)\.z(\d+)\.fidonet(?:\.org|\.net)?$`)

// fidoAddress is a FidoNet name and address as an e-mail address.
func fidoAddress(name string, a ftn.Addr) string {
	d := fmt.Sprintf("f%d.n%d.z%d.fidonet", a.Node, a.Net, a.Zone)
	if a.Point > 0 {
		d = fmt.Sprintf("p%d.", a.Point) + d
	}
	return bbs.LocalPart(name) + "@" + d
}

// resolve reads a recipient's address: a user here, FidoNet netmail (by
// the gateway's form, John.Smith@f100.n1.z21.fidonet), or the internet.
func (s *Service) resolve(addr string) (*recipient, error) {
	a, err := mail.ParseAddress(addr)
	if err != nil {
		return nil, fmt.Errorf("%q is not an address", addr)
	}
	local, domain, ok := strings.Cut(a.Address, "@")
	if !ok || local == "" || domain == "" {
		return nil, fmt.Errorf("%q is not an address", addr)
	}
	if s.ours(domain) {
		u, err := s.LocalUser(local)
		if err != nil {
			return nil, err
		}
		return &recipient{kind: toLocal, addr: a.Address, user: u}, nil
	}
	if m := fidoDomain.FindStringSubmatch(strings.ToLower(domain)); m != nil {
		n := func(s string) int { v, _ := strconv.Atoi(s); return v }
		fa := ftn.Addr{Zone: n(m[4]), Net: n(m[3]), Node: n(m[2]), Point: n(m[1])}
		name := strings.Join(strings.Fields(strings.NewReplacer(".", " ", "_", " ").Replace(local)), " ")
		return &recipient{kind: toFido, addr: a.Address, name: titleCase(name), ftn: fa}, nil
	}
	return &recipient{kind: toInternet, addr: a.Address}, nil
}

// titleCase is "john smith" as "John Smith", as FidoNet names are.
func titleCase(s string) string {
	f := strings.Fields(s)
	for i, w := range f {
		f[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(f, " ")
}

// watch is a channel told when mail comes for the user, and what stops it.
func (s *Service) watch(user int64) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.watches[user] = append(s.watches[user], ch)
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		l := s.watches[user]
		for i, c := range l {
			if c == ch {
				s.watches[user] = append(l[:i], l[i+1:]...)
				break
			}
		}
		if len(s.watches[user]) == 0 {
			delete(s.watches, user)
		}
	}
}

// notify tells the user's watches.
func (s *Service) notify(user int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.watches[user] {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}
