// Package bbs is the BBS itself, whatever the terminal: the settings, the
// nodes (callers online), logins and new users.  The text and 3270 front
// ends (textui, ui3270) call into it.
package bbs

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
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
}

// Defaults are the settings without a configuration.
func Defaults() Settings {
	return Settings{
		Name:          "Go BBS",
		Sysop:         "Sysop",
		Banner:        "",
		Goodbye:       "Thanks for calling.",
		NewUsers:      true,
		MaxNodes:      32,
		Attempts:      3,
		FailDelay:     3 * time.Second,
		LoginTimeout:  2 * time.Minute,
		IdleTimeout:   10 * time.Minute,
		ThrottleFails: 10,
		ThrottleTime:  10 * time.Minute,
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
	Rate     string // modem: tx/rx bit/s
	CallerID string // modem: the caller's number
	Dialed   string // modem: the number dialed
	Channel  string // modem: the Asterisk channel

	// an upstream menu has logged the caller in already (a trusted
	// header's MODEM_USER, a trusted rlogin's server user)
	TrustedUser string
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

	mu       sync.Mutex
	nodes    map[int]*Node
	failures map[string][]time.Time
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
		out = append(out, NodeInfo{n.Num, name, n.Caller.Describe(), n.Since, n.activity})
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
	ErrBadLogin  = errors.New("wrong name or password")
	ErrLocked    = errors.New("this account is locked")
	ErrThrottled = errors.New("too many failed logins; try again later")
	ErrNoNew     = errors.New("new accounts are not taken")
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
		time.Sleep(b.FailDelay)
		return nil, ErrThrottled
	}
	u, err := b.Store.CheckPassword(name, pw)
	if errors.Is(err, store.ErrNotFound) {
		b.failed(source)
		b.Log.Printf("%s: %s: failed login as %q", who, source, Clean(name, MaxNameLen))
		time.Sleep(b.FailDelay)
		return nil, ErrBadLogin
	}
	if err != nil {
		b.Log.Printf("%s: login: %v", who, err)
		return nil, errors.New("the system is in trouble; try again later")
	}
	if u.Locked {
		b.Log.Printf("%s: %s: %s is locked", who, source, u.Name)
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
	return n.loggedIn(u, "upstream")
}

func (n *Node) loggedIn(u *store.User, how string) (*store.User, error) {
	if err := n.b.Store.LoggedIn(u.ID); err != nil {
		return nil, err
	}
	n.SetUser(u)
	n.Doing("Main menu")
	n.b.Log.Printf("node %d: %s logged in (%s, %s from %s)", n.Num, u.Name, how,
		n.Caller.Describe(), n.Caller.Remote)
	return u, nil
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

// CanPost reports whether u may post in area a.
func CanPost(u *store.User, a *store.Area) bool {
	return !a.SysopOnly || u.Sysop
}

// Post cleans and posts a message.
func (n *Node) Post(a *store.Area, to, subject string, body []string, replyTo int64) (int64, error) {
	u := n.User()
	if u == nil {
		return 0, errors.New("not logged in")
	}
	if !CanPost(u, a) {
		return 0, errors.New("only the sysop posts here")
	}
	m := &store.Message{AreaID: a.ID, FromID: u.ID, From: u.Name,
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
	id, err := n.b.Store.Post(m)
	if err == nil {
		n.b.Log.Printf("node %d: %s posted #%d in %s", n.Num, u.Name, id, a.Name)
	}
	return id, err
}

// SendMail cleans and sends a private message.
func (n *Node) SendMail(to, subject string, body []string, replyTo int64) error {
	u := n.User()
	if u == nil {
		return errors.New("not logged in")
	}
	rcpt, err := n.b.Store.UserByName(strings.TrimSpace(to))
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("there is no user %q", Clean(to, MaxNameLen))
	}
	if err != nil {
		return err
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
	out := make([]AreaNew, len(areas))
	for i, a := range areas {
		n, err := b.Store.NewCount(u.ID, a.ID)
		if err != nil {
			return nil, err
		}
		out[i] = AreaNew{a, n}
	}
	return out, nil
}
