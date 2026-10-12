// Package bbs is the BBS itself, whatever the terminal: the settings, the
// nodes (callers online), logins and new users.  The text and 3270 front
// ends (textui, ui3270) call into it.
package bbs

import (
	"errors"
	"fmt"
	"github.com/Neo-Desktop/bbs-sec/internal/door"
	"github.com/Neo-Desktop/bbs-sec/internal/events"
	"github.com/Neo-Desktop/bbs-sec/internal/guard"
	"github.com/Neo-Desktop/bbs-sec/internal/screens"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Settings are the [general] options.
type Settings struct {
	Name          string        // the system's name
	Sysop         string        // the sysop's name, for the system info
	Location      string        // where the system is
	Banner        string        // shown on connect, before the login
	MOTD          string        // shown after the login (text or file)
	MOTDFile      string        // read at each login
	Goodbye       string        // shown on logoff
	NewUsers      bool          // callers may make accounts
	MaxNodes      int           // callers at once
	Attempts      int           // logins before hanging up
	FailDelay     time.Duration // after a failed login
	LoginTimeout  time.Duration // to log in
	IdleTimeout   time.Duration // for a key once logged in
	ThrottleFails int           // failures from one source ...
	ThrottleTime  time.Duration // ... within this lock it out for this
	PageCommand   []string      // run when a page finds no sysop on
	QWKID         string        // QWK packets' BBSID ("": made of Name)

	Levels         []Level // [level N], by number
	NewUserLevel   int     // a new user's level
	ValidatedLevel int     // a validated user's
}

// Defaults are the settings without a configuration.
func Defaults() Settings {
	return Settings{
		Name:           "Go BBS",
		Sysop:          "Sysop",
		Banner:         "",
		Goodbye:        "Thanks for calling.",
		NewUsers:       true,
		NewUserLevel:   20,
		ValidatedLevel: 20,
		MaxNodes:       32,
		Attempts:       3,
		FailDelay:      3 * time.Second,
		LoginTimeout:   2 * time.Minute,
		IdleTimeout:    10 * time.Minute,
		ThrottleFails:  10,
		ThrottleTime:   10 * time.Minute,
	}
}

// Caller is what is known of a connection before the login: how it came,
// and from a modem, the call's details.
type Caller struct {
	Via      string // telnet, tn3270, rlogin, modem, socket
	Listener string // the [listen NAME]
	Remote   string // the peer's address, or the caller ID
	Source   string // what throttling counts failures against
	Terminal string // terminal type, if told
	Ident    string // what the terminal said it is (a VT's identification)
	Rate     string // modem: tx/rx bit/s
	CallerID string // modem: the caller's number
	Dialed   string // modem: the number dialed
	Channel  string // modem: the Asterisk channel

	// an upstream menu has logged the caller in already (a trusted
	// header's MODEM_USER, a trusted rlogin's server user)
	TrustedUser string
	// how TrustedUser was vouched for, for the log ("upstream" if not
	// said): "ssh password", "ssh key SHA256:..."
	TrustedHow string
	// credentials to try before asking: rlogin's server user, and its
	// client user as the password (the BBS convention)
	LoginUser     string
	LoginPassword string
}

// Baud is the connect rate towards the caller, from Rate ("tx/rx").
func (c *Caller) Baud() string {
	tx, _, _ := strings.Cut(c.Rate, "/")
	return strings.TrimSpace(tx)
}

// Describe is the connection in a few words, for the log and who's online.
func (c *Caller) Describe() string {
	s := c.Via
	if b := c.Baud(); b != "" {
		s += " " + b
	}
	return s
}

// BBS is the running system.
type BBS struct {
	Settings
	Store *store.Store
	Files *files.Store // nil: no file areas
	Log   *log.Logger
	// FTPAddress is where users are told to fetch files ("": no FTP)
	FTPAddress string
	// News is how articles made here are signed (newsgroup areas)
	News NewsSettings
	// Netmail sends FidoNet netmail (nil: the BBS is not on FidoNet)
	Netmail NetmailFunc
	// Doors are the door programs (for text callers)
	Doors []*door.Door
	// the sysop's screens, menus and strings
	Screens *screens.Set
	Menus   map[string]*Menu
	Text    map[string]string
	// ScriptsDir has the sysop's JavaScript ("": no scripts)
	ScriptsDir string
	// Guard keeps addresses and names out, and the hack log (nil: none)
	Guard *guard.Guard
	// Events are the sysop's scheduled events (nil: none)
	Events *events.Scheduler
	// Stats count logins, posts and the like
	Stats Stats
	// sysopAway: the sysop is not to be paged (pages leave a message)
	sysopAway atomic.Bool

	mu       sync.Mutex
	nodes    map[int]*Node
	failures map[string][]time.Time
	hubOnce  sync.Once
	chat     *hub
}

// New makes the BBS of the settings and store.
func New(s Settings, st *store.Store, logger *log.Logger) *BBS {
	if logger == nil {
		logger = log.New(os.Stderr, "bbs: ", log.LstdFlags)
	}
	return &BBS{Settings: s, Store: st, Log: logger,
		nodes: map[int]*Node{}, failures: map[string][]time.Time{}}
}

// Node is a caller online.
type Node struct {
	Num    int
	Caller *Caller
	Since  time.Time

	b        *BBS
	user     *store.User
	activity string
	kick     func()
	chatOnce sync.Once
	cs       *chatState
	clock    *clock // the day's time limit, for a caller logged in
	sysopTap tap    // sysops watching the node
}

// ErrBusy: every node is taken.
var ErrBusy = errors.New("all lines are busy")

// Enter takes a node for a caller; kick is called to throw the caller off
// (on shutdown).  The node must be left with Leave.
func (b *BBS) Enter(c *Caller, kick func()) (*Node, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.nodes) >= b.MaxNodes {
		return nil, ErrBusy
	}
	n := 1
	for b.nodes[n] != nil {
		n++
	}
	node := &Node{Num: n, Caller: c, Since: time.Now(), b: b,
		activity: "Logging in", kick: kick}
	b.nodes[n] = node
	return node, nil
}

// Leave gives the node back.
func (n *Node) Leave() {
	n.stopClock()
	n.leaveChat()
	n.b.mu.Lock()
	delete(n.b.nodes, n.Num)
	n.b.mu.Unlock()
}

// User is the node's user, nil before the login.
func (n *Node) User() *store.User {
	n.b.mu.Lock()
	defer n.b.mu.Unlock()
	return n.user
}

// SetUser records the login.
func (n *Node) SetUser(u *store.User) {
	n.b.mu.Lock()
	n.user = u
	n.b.mu.Unlock()
}

// SetTerminal records how the caller came after all (a telnet caller that
// is a 3270), as who's online shows it.
func (n *Node) SetTerminal(via, terminal string) {
	n.b.mu.Lock()
	n.Caller.Via, n.Caller.Terminal = via, terminal
	n.b.mu.Unlock()
}

// SetRate records the caller's line rate ("tx/rx") once known.
func (n *Node) SetRate(rate string) {
	n.b.mu.Lock()
	n.Caller.Rate = rate
	n.b.mu.Unlock()
}

// SetIdent records what the caller's terminal said it is.
func (n *Node) SetIdent(ident string) {
	n.b.mu.Lock()
	n.Caller.Ident = ident
	n.b.mu.Unlock()
}

// Doing sets what the caller is doing, for who's online.
func (n *Node) Doing(what string) {
	n.b.mu.Lock()
	n.activity = what
	n.b.mu.Unlock()
}

// NodeInfo is a snapshot of a node.
type NodeInfo struct {
	Num      int
	User     string
	Via      string
	Since    time.Time
	Activity string
	Terminal string // its identification, else its terminal type
}

// Nodes lists the callers online.
func (b *BBS) Nodes() []NodeInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []NodeInfo
	for _, n := range b.nodes {
		name := ""
		if n.user != nil {
			name = n.user.Name
		}
		term := n.Caller.Ident
		if term == "" {
			term = n.Caller.Terminal
		}
		out = append(out, NodeInfo{n.Num, name, n.Caller.Describe(), n.Since, n.activity, term})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Num < out[j].Num })
	return out
}

// KickAll throws every caller off.
func (b *BBS) KickAll() {
	b.mu.Lock()
	var kicks []func()
	for _, n := range b.nodes {
		if n.kick != nil {
			kicks = append(kicks, n.kick)
		}
	}
	b.mu.Unlock()
	for _, k := range kicks {
		k()
	}
}

// Errors of logins.
var (
	ErrBadLogin   = errors.New("wrong name or password")
	ErrLocked     = errors.New("this account is locked")
	ErrThrottled  = errors.New("too many failed logins; try again later")
	ErrNoNew      = errors.New("new accounts are not taken")
	ErrNameDenied = errors.New("that name is not allowed here")
)

// throttled reports whether source has failed too often lately.
func (b *BBS) throttled(source string) bool {
	if source == "" || b.ThrottleFails <= 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	cut := time.Now().Add(-b.ThrottleTime)
	f := b.failures[source]
	for len(f) > 0 && f[0].Before(cut) {
		f = f[1:]
	}
	if len(f) == 0 {
		delete(b.failures, source)
	} else {
		b.failures[source] = f
	}
	return len(f) >= b.ThrottleFails
}

// Refuse says whether a connection to service (finger, nntp, [listen
// telnet] ...) from addr is to be closed: an address in the deny file, or
// banned.  It is logged, and to the hack log.
func (b *BBS) Refuse(service string, addr net.Addr) bool {
	a, ok := addr.(*net.TCPAddr)
	if !ok {
		return false // a unix socket's: the modem server, a menu
	}
	ip := a.IP.String()
	if no, why := b.Guard.Refused(ip); no {
		b.Log.Printf("%s: %s refused: %s", service, ip, why)
		b.Guard.Hack("%s %s: refused: %s", ip, service, why)
		return true
	}
	return false
}

// ban bans a source that is an address (not a caller ID), for the guard's
// ban time.
func (b *BBS) ban(source, why string) {
	if ip, ok := strings.CutPrefix(source, "ip:"); ok {
		b.Guard.Ban(ip, why)
	}
}

func (b *BBS) failed(source string) {
	if source == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures[source] = append(b.failures[source], time.Now())
	// keep the map small: forget the oldest sources
	if len(b.failures) > 10000 {
		for k := range b.failures {
			delete(b.failures, k)
			if len(b.failures) <= 5000 {
				break
			}
		}
	}
}

// Login checks a name and password for the node.  A failure is delayed by
// FailDelay; a source that fails too often is turned away for a while.
func (n *Node) Login(name, pw string) (*store.User, error) {
	u, err := n.b.Authenticate(fmt.Sprintf("node %d", n.Num), n.Caller.Source, name, pw)
	if err != nil {
		return nil, err
	}
	return n.loggedIn(u, "password")
}

// Authenticate checks a name and password from source (an address or
// caller ID; "" for none), for a node or another service (who, for the
// log): a failure is delayed by FailDelay and counted against source, and
// a source that fails too often is turned away for a while.
func (b *BBS) Authenticate(who, source, name, pw string) (*store.User, error) {
	if b.throttled(source) {
		b.Log.Printf("%s: %s: login refused, throttled", who, source)
		b.Guard.Hack("%s %s: login refused, throttled", source, who)
		b.ban(source, "too many failed logins")
		time.Sleep(b.FailDelay)
		return nil, ErrThrottled
	}
	u, err := b.Store.CheckPassword(name, pw)
	if errors.Is(err, store.ErrNotFound) {
		b.failed(source)
		b.Stats.LoginFailures.Add(1)
		b.Log.Printf("%s: %s: failed login as %q", who, source, Clean(name, MaxNameLen))
		b.Guard.Hack("%s %s: failed login as %q", source, who, Clean(name, MaxNameLen))
		if b.throttled(source) {
			b.ban(source, "too many failed logins")
		}
		time.Sleep(b.FailDelay)
		return nil, ErrBadLogin
	}
	if err != nil {
		b.Log.Printf("%s: login: %v", who, err)
		return nil, errors.New("the system is in trouble; try again later")
	}
	if u.Locked {
		b.Log.Printf("%s: %s: %s is locked", who, source, u.Name)
		b.Guard.Hack("%s %s: login as %s, a locked account", source, who, u.Name)
		time.Sleep(b.FailDelay)
		return nil, ErrLocked
	}
	return u, nil
}

// LoginTrusted logs in a user an upstream menu vouched for (Caller's
// TrustedUser).  With create, a user unknown here gets an external account.
func (n *Node) LoginTrusted(name string, create bool) (*store.User, error) {
	b := n.b
	u, err := b.Store.UserByName(name)
	if errors.Is(err, store.ErrNotFound) && create {
		if msg := ValidName(name); msg != "" {
			return nil, fmt.Errorf("%s: %s", name, msg)
		}
		if b.Guard.NameDenied(name) {
			b.Guard.Hack("%s node %d: new external user %q refused by name_deny_file", n.Caller.Source, n.Num, name)
			return nil, ErrNameDenied
		}
		u, err = b.Store.CreateUser(name, "", "", false)
		if err == nil {
			b.Log.Printf("node %d: new external user %s", n.Num, u.Name)
		}
	}
	if err != nil {
		return nil, err
	}
	if u.Locked {
		return nil, ErrLocked
	}
	how := "upstream"
	if n.Caller.TrustedHow != "" {
		how = n.Caller.TrustedHow
	}
	return n.loggedIn(u, how)
}

func (n *Node) loggedIn(u *store.User, how string) (*store.User, error) {
	if err := n.b.Store.LoggedIn(u.ID); err != nil {
		return nil, err
	}
	n.SetUser(u)
	n.startClock(u)
	if err := n.b.Store.AddCall(&store.Call{UserID: u.ID, Name: u.Name, Location: u.Location,
		Via: n.Caller.Via, Node: n.Num}); err != nil {
		n.b.Log.Printf("node %d: the last callers: %v", n.Num, err)
	}
	n.b.Stats.Logins.Add(1)
	n.Doing("Main menu")
	n.b.Log.Printf("node %d: %s logged in (%s, %s from %s)", n.Num, u.Name, how,
		n.Caller.Describe(), n.Caller.Remote)
	return u, nil
}

// CheckNewName says what is wrong with a new account's name for the
// node's caller ("": nothing but perhaps that it is taken).
func (n *Node) CheckNewName(name string) string {
	if msg := ValidName(name); msg != "" {
		return msg
	}
	if n.b.Guard.NameDenied(name) {
		n.b.Guard.Hack("%s node %d: new user %q refused by name_deny_file", n.Caller.Source, n.Num, Clean(name, MaxNameLen))
		return "That name is not allowed here."
	}
	return ""
}

// Register makes a new account for the node's caller and logs it in.
func (n *Node) Register(name, pw, location string) (*store.User, error) {
	b := n.b
	if !b.NewUsers {
		return nil, ErrNoNew
	}
	if msg := ValidName(name); msg != "" {
		return nil, errors.New(msg)
	}
	if msg := ValidPassword(pw, name); msg != "" {
		return nil, errors.New(msg)
	}
	if b.Guard.NameDenied(name) {
		b.Log.Printf("node %d: new user %q refused by name_deny_file", n.Num, Clean(name, MaxNameLen))
		b.Guard.Hack("%s node %d: new user %q refused by name_deny_file", n.Caller.Source, n.Num, Clean(name, MaxNameLen))
		return nil, ErrNameDenied
	}
	if b.throttled(n.Caller.Source) {
		return nil, ErrThrottled
	}
	u, err := b.Store.CreateUser(name, pw, Clean(location, MaxLocationLen), false)
	if errors.Is(err, store.ErrExists) {
		return nil, errors.New("that name is taken")
	}
	if err != nil {
		return nil, err
	}
	if err := b.Store.SetLevel(u.ID, b.NewUserLevel); err != nil {
		return nil, err
	}
	u.Level = b.NewUserLevel
	// a new account counts against the source like a failure, so that
	// one caller cannot make thousands
	b.failed(n.Caller.Source)
	b.Log.Printf("node %d: new user %s", n.Num, u.Name)
	return n.loggedIn(u, "new user")
}

// MOTDText is the message of the day: MOTD, then MOTDFile.
func (b *BBS) MOTDText() string {
	text := b.MOTD
	if b.MOTDFile != "" {
		if data, err := os.ReadFile(b.MOTDFile); err == nil {
			if len(data) > 8192 {
				data = data[:8192]
			}
			if text != "" {
				text += "\n"
			}
			text += string(data)
		}
	}
	return text
}

// Post cleans and posts a message.
func (n *Node) Post(a *store.Area, to, subject string, body []string, replyTo int64) (int64, error) {
	return n.PostWith(a, to, subject, body, replyTo, PostOpts{})
}

// PostOpts are a post's choices: under the poster's alias (where the area
// allows), a poll's answers.
type PostOpts struct {
	Alias bool
	Poll  []string
}

// MaxPollAnswers is the most answers a poll has.
const MaxPollAnswers = 10

// PostWith is Post with choices.
func (n *Node) PostWith(a *store.Area, to, subject string, body []string, replyTo int64, o PostOpts) (int64, error) {
	u := n.User()
	if u == nil {
		return 0, errors.New("not logged in")
	}
	var answers []string
	for _, p := range o.Poll {
		if p = Clean(p, 60); p != "" {
			answers = append(answers, p)
		}
	}
	if o.Poll != nil && (len(answers) < 2 || len(answers) > MaxPollAnswers) {
		return 0, fmt.Errorf("a poll has 2 to %d answers", MaxPollAnswers)
	}
	from := u.Name
	if o.Alias {
		if !a.Aliases {
			return 0, errors.New("this area takes no aliases")
		}
		if u.Alias == "" {
			return 0, errors.New("you have no alias (set one in your profile)")
		}
		from = u.Alias
	}
	if !n.b.CanPost(u, a) {
		if a.SysopOnly {
			return 0, errors.New("only the sysop posts here")
		}
		return 0, errors.New("your access does not allow posting here")
	}
	m := &store.Message{AreaID: a.ID, FromID: u.ID, From: from,
		To: Clean(to, MaxNameLen), Subject: Clean(subject, MaxSubjectLen),
		Body: CleanBody(body), ReplyTo: replyTo}
	if m.To == "" {
		m.To = "All"
	}
	if m.Subject == "" {
		return 0, errors.New("a message needs a subject")
	}
	if m.Body == "" {
		return 0, errors.New("the message is empty")
	}
	n.b.MakeArticle(u, a, m)
	id, err := n.b.Store.Post(m)
	if err == nil && answers != nil {
		err = n.b.Store.SetPoll(id, answers)
	}
	if err == nil {
		n.b.Stats.Posts.Add(1)
		as := ""
		if o.Alias {
			as = " as " + from
		}
		n.b.Log.Printf("node %d: %s posted #%d in %s%s", n.Num, u.Name, id, a.Name, as)
	}
	return id, err
}

// SendMail cleans and sends a private message.
func (n *Node) SendMail(to, subject string, body []string, replyTo int64) error {
	u := n.User()
	if u == nil {
		return errors.New("not logged in")
	}
	mail := n.b.LevelOf(u).Mail
	if name, addr, ok := SplitNetmail(to); ok {
		if !mail {
			return errors.New("your access allows mail only to the sysop")
		}
		return n.sendNetmail(u, name, addr, subject, body, replyTo)
	}
	rcpt, err := n.b.Store.UserByName(strings.TrimSpace(to))
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("there is no user %q", Clean(to, MaxNameLen))
	}
	if err != nil {
		return err
	}
	// a user who may not mail may still write to the sysop (to be validated)
	if !mail && !rcpt.Sysop {
		return errors.New("your access allows mail only to the sysop")
	}
	m := &store.Message{FromID: u.ID, From: u.Name, ToID: rcpt.ID, To: rcpt.Name,
		Subject: Clean(subject, MaxSubjectLen), Body: CleanBody(body), ReplyTo: replyTo}
	if m.Subject == "" {
		return errors.New("a message needs a subject")
	}
	if m.Body == "" {
		return errors.New("the message is empty")
	}
	if _, err := n.b.Store.SendMail(m); err != nil {
		return err
	}
	n.b.Stats.Mail.Add(1)
	n.b.tellMail(rcpt.ID, u.Name, m.Subject)
	n.b.Log.Printf("node %d: %s sent mail to %s", n.Num, u.Name, rcpt.Name)
	return nil
}

// ChangePassword checks the old password and sets the new.
func (n *Node) ChangePassword(old, pw string) error {
	u := n.User()
	if u == nil {
		return errors.New("not logged in")
	}
	if u.External {
		return errors.New("your password is kept by the system you came from")
	}
	if _, err := n.b.Store.CheckPassword(u.Name, old); err != nil {
		time.Sleep(n.b.FailDelay)
		return errors.New("that is not your password")
	}
	if msg := ValidPassword(pw, u.Name); msg != "" {
		return errors.New(msg)
	}
	if err := n.b.Store.SetPassword(u.ID, pw); err != nil {
		return err
	}
	n.b.Log.Printf("node %d: %s changed their password", n.Num, u.Name)
	return nil
}

// Refresh rereads the node's user (post counts, sysop rights).
func (n *Node) Refresh() *store.User {
	u := n.User()
	if u == nil {
		return nil
	}
	if fresh, err := n.b.Store.UserByID(u.ID); err == nil {
		n.SetUser(fresh)
		return fresh
	}
	return u
}

// DeleteMessage removes a message: its author's, or any for a sysop.
func (n *Node) DeleteMessage(m *store.Message) error {
	u := n.User()
	if u == nil || !(u.Sysop || u.ID == m.FromID) {
		return errors.New("only its author or the sysop deletes a message")
	}
	if err := n.b.Store.DeleteMessage(m.ID); err != nil {
		return err
	}
	n.b.Log.Printf("node %d: %s deleted message #%d", n.Num, u.Name, m.ID)
	return nil
}

// AreaNew is an area with the user's count of new messages.
type AreaNew struct {
	*store.Area
	New int
}

// AreasFor lists the areas with u's new counts.
func (b *BBS) AreasFor(u *store.User) ([]AreaNew, error) {
	areas, err := b.Store.Areas()
	if err != nil {
		return nil, err
	}
	out := make([]AreaNew, 0, len(areas))
	for _, a := range areas {
		if !b.CanRead(u, a) {
			continue
		}
		n, err := b.Store.NewCount(u.ID, a.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, AreaNew{a, n})
	}
	return out, nil
}
