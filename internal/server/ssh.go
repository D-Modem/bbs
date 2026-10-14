package server

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
)

// sshNewUser is the SSH user name that needs no password: the caller gets
// the BBS's own login prompt, to sign up (or log in there).
const sshNewUser = "new"

// sshHandshakeTime: how long a client has from connecting to asking for
// its shell.
const sshHandshakeTime = 30 * time.Second

// DefaultHostKeys are the host keys an SSH listener has unless it is told
// others: in dir (the database's), made the first time if missing.
func DefaultHostKeys(dir string) []string {
	return []string{filepath.Join(dir, "ssh_host_ed25519_key"), filepath.Join(dir, "ssh_host_rsa_key")}
}

// LoadHostKey reads a host key, making it first if there is none: RSA
// (3072 bits) when the file's name says rsa, ECDSA P-256 when it says
// ecdsa, else ed25519.  made reports a new key.
func LoadHostKey(path string) (signer ssh.Signer, made bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if b, err = makeHostKey(path); err != nil {
			return nil, false, err
		}
		made = true
	} else if err != nil {
		return nil, false, err
	} else if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0077 != 0 {
		return nil, false, fmt.Errorf("%s: others may read it (chmod 600 it)", path)
	}
	signer, err = ssh.ParsePrivateKey(b)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	return signer, made, nil
}

func makeHostKey(path string) ([]byte, error) {
	var key any
	var err error
	switch base := strings.ToLower(filepath.Base(path)); {
	case strings.Contains(base, "rsa"):
		key, err = rsa.GenerateKey(rand.Reader, 3072)
	case strings.Contains(base, "ecdsa"):
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	default:
		_, key, err = ed25519.GenerateKey(rand.Reader)
	}
	if err != nil {
		return nil, err
	}
	blk, err := ssh.MarshalPrivateKey(key, "bbs host key")
	if err != nil {
		return nil, err
	}
	b := pem.EncodeToMemory(blk)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return b, f.Close()
}

// HostKeyInfo describes a host key file for bbs check: its type and
// fingerprint, or that it will be made.
func HostKeyInfo(path string) string {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "none yet (made at the first start)"
	}
	if err != nil {
		return err.Error()
	}
	k, err := ssh.ParsePrivateKey(b)
	if err != nil {
		return err.Error()
	}
	return k.PublicKey().Type() + " " + ssh.FingerprintSHA256(k.PublicKey())
}

// algorithms old clients (SyncTERM's cryptlib, old PuTTYs) may need, with
// what is offered anyway
var (
	sshKexes = []string{"curve25519-sha256", "curve25519-sha256@libssh.org", "ecdh-sha2-nistp256",
		"ecdh-sha2-nistp384", "ecdh-sha2-nistp521", "diffie-hellman-group14-sha256",
		"diffie-hellman-group16-sha512", "diffie-hellman-group14-sha1"}
	sshCiphers = []string{"aes128-gcm@openssh.com", "aes256-gcm@openssh.com",
		"chacha20-poly1305@openssh.com", "aes128-ctr", "aes192-ctr", "aes256-ctr"}
	sshLegacyKexes   = []string{"diffie-hellman-group1-sha1"}
	sshLegacyCiphers = []string{"aes128-cbc", "3des-cbc"}
)

// sshSetup loads an SSH listener's host keys.
func (s *Server) sshSetup(l *Listener) error {
	cfg := &ssh.ServerConfig{ServerVersion: "SSH-2.0-bbs"}
	if l.Legacy {
		cfg.KeyExchanges = append(append([]string{}, sshKexes...), sshLegacyKexes...)
		cfg.Ciphers = append(append([]string{}, sshCiphers...), sshLegacyCiphers...)
	}
	if len(l.HostKeys) == 0 {
		return errors.New("no host keys")
	}
	for _, p := range l.HostKeys {
		k, made, err := LoadHostKey(p)
		if err != nil {
			return err
		}
		if made {
			s.B.Log.Printf("[listen %s] made host key %s: %s %s", l.Name, p, k.PublicKey().Type(),
				ssh.FingerprintSHA256(k.PublicKey()))
		}
		cfg.AddHostKey(k)
	}
	l.ssh = cfg
	return nil
}

// sshUser is the account an SSH user name means: as given, or with _ or
// . for the spaces a BBS name may have ("Alice_Smith").
func (s *Server) sshUser(name string) string {
	if _, err := s.B.Store.UserByName(name); err == nil || !strings.ContainsAny(name, "_.") {
		return name
	}
	alt := strings.NewReplacer("_", " ", ".", " ").Replace(name)
	if _, err := s.B.Store.UserByName(alt); err == nil {
		return alt
	}
	return name
}

// sshConfig is the listener's configuration with this caller's checks.
func (s *Server) sshConfig(l *Listener, caller *bbs.Caller) *ssh.ServerConfig {
	cfg := *l.ssh
	password := func(name, pw string) (*ssh.Permissions, error) {
		if strings.EqualFold(name, sshNewUser) {
			return nil, bbs.ErrBadLogin
		}
		u, err := s.B.Authenticate("ssh", caller.Source, s.sshUser(name), pw)
		if err != nil {
			return nil, err
		}
		return &ssh.Permissions{Extensions: map[string]string{"user": u.Name, "how": "ssh password"}}, nil
	}
	cfg.PasswordCallback = func(cm ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		return password(cm.User(), string(pw))
	}
	cfg.KeyboardInteractiveCallback = func(cm ssh.ConnMetadata, ask ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		a, err := ask("", "", []string{"Password: "}, []bool{false})
		if err != nil {
			return nil, err
		}
		if len(a) != 1 {
			return nil, bbs.ErrBadLogin
		}
		return password(cm.User(), a[0])
	}
	cfg.PublicKeyCallback = func(cm ssh.ConnMetadata, pk ssh.PublicKey) (*ssh.Permissions, error) {
		// may be only a question (would this key do?): what is decided
		// is in the Permissions, which count only once the key is proved
		u, k, err := s.B.SSHKeyLogin(s.sshUser(cm.User()), pk)
		if err != nil {
			return nil, err
		}
		return &ssh.Permissions{Extensions: map[string]string{"user": u.Name,
			"how": "ssh key " + k.Fingerprint, "key": strconv.FormatInt(k.ID, 10)}}, nil
	}
	cfg.NoClientAuth = true
	cfg.NoClientAuthCallback = func(cm ssh.ConnMetadata) (*ssh.Permissions, error) {
		if strings.EqualFold(cm.User(), sshNewUser) && s.B.Conf().NewUsers {
			return &ssh.Permissions{}, nil
		}
		return nil, bbs.ErrBadLogin
	}
	return &cfg
}

// sshCall is an SSH caller: the handshake and login, a session with a
// shell (and a pty, if the client asks), then the text BBS.
func (s *Server) sshCall(c net.Conn, l *Listener, caller *bbs.Caller, remote string) {
	c.SetReadDeadline(time.Now().Add(sshHandshakeTime))
	sc, chans, reqs, err := ssh.NewServerConn(c, s.sshConfig(l, caller))
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.B.Log.Printf("[listen %s] %s: ssh: %v", l.Name, remote, err)
		}
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs) // port forwarding and the like: no

	var ch ssh.Channel
	var creqs <-chan *ssh.Request
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.Prohibited, "only a terminal session here")
			continue
		}
		if ch, creqs, err = nc.Accept(); err != nil {
			return
		}
		break
	}
	if ch == nil {
		return
	}
	defer ch.Close()
	go func() { // one session a connection
		for nc := range chans {
			nc.Reject(ssh.Prohibited, "one session at a time")
		}
	}()

	pty := sshPty{}
	for started := false; !started; {
		req, ok := <-creqs
		if !ok {
			return
		}
		switch req.Type {
		case "pty-req":
			ok := ssh.Unmarshal(req.Payload, &pty) == nil
			req.Reply(ok, nil)
		case "shell":
			req.Reply(true, nil)
			started = true
		default: // exec, subsystem (sftp), env, x11, agent forwarding
			req.Reply(false, nil)
		}
	}
	c.SetReadDeadline(time.Time{})
	go func() {
		for req := range creqs {
			// window-change: the size is the one the session began with
			req.Reply(req.Type == "window-change", nil)
		}
	}()

	if u := sc.Permissions.Extensions["user"]; u != "" {
		caller.TrustedUser, caller.TrustedHow = u, sc.Permissions.Extensions["how"]
	}
	caller.Terminal = bbs.Clean(pty.Term, 40)
	t := newSSHTerm(ch, c, pty)
	defer t.close()
	node, err := s.B.Enter(caller, func() { c.Close() })
	if err != nil {
		s.B.Log.Printf("[listen %s] %s: %v", l.Name, remote, err)
		io.WriteString(t, "\r\nAll lines are busy; please call again later.\r\n")
		ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{1}))
		return
	}
	defer node.Leave()
	s.B.Log.Printf("node %d: %s on [listen %s] as %s", node.Num, remote, l.Name, sc.User())
	if id, err := strconv.ParseInt(sc.Permissions.Extensions["key"], 10, 64); err == nil {
		s.B.Store.TouchSSHKey(id)
	}
	err = s.text(t, t, l, node, caller.Terminal)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		s.B.Log.Printf("node %d: %v", node.Num, err)
	}
	ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
	s.B.Log.Printf("node %d: gone", node.Num)
}

// sshPty is a pty-req's payload (RFC 4254 6.2).
type sshPty struct {
	Term             string
	Cols, Rows, W, H uint32
	Modes            string
}

// sshTerm is a stream (an SSH session channel, a web terminal's
// WebSocket) as the text BBS wants a line: reads with deadlines that only
// end the read, and writes that hang up a client that stops reading.
type sshTerm struct {
	ch         io.ReadWriter
	conn       io.Closer
	cols, rows int

	in   chan []byte
	done chan struct{}
	rerr error // after in is closed
	pend []byte

	mu      sync.Mutex
	dl      time.Time
	changed chan struct{} // closed when dl changes
}

func newSSHTerm(ch io.ReadWriter, conn io.Closer, p sshPty) *sshTerm {
	t := &sshTerm{ch: ch, conn: conn, in: make(chan []byte), done: make(chan struct{}),
		changed: make(chan struct{})}
	if p.Cols >= 20 && p.Cols <= 255 && p.Rows >= 5 && p.Rows <= 255 {
		t.cols, t.rows = int(p.Cols), int(p.Rows)
	}
	go t.pump()
	return t
}

func (t *sshTerm) pump() {
	for {
		b := make([]byte, 4096)
		n, err := t.ch.Read(b)
		if n > 0 {
			select {
			case t.in <- b[:n]:
			case <-t.done:
				return
			}
		}
		if err != nil {
			t.rerr = err
			close(t.in)
			return
		}
	}
}

func (t *sshTerm) close() { close(t.done) }

// Size is the pty's (0, 0 if the client gave none).
func (t *sshTerm) Size() (cols, rows int) { return t.cols, t.rows }

func (t *sshTerm) SetReadDeadline(d time.Time) error {
	t.mu.Lock()
	t.dl = d
	close(t.changed)
	t.changed = make(chan struct{})
	t.mu.Unlock()
	return nil
}

func (t *sshTerm) Read(p []byte) (int, error) {
	if len(t.pend) > 0 {
		n := copy(p, t.pend)
		t.pend = t.pend[n:]
		return n, nil
	}
	for {
		t.mu.Lock()
		dl, changed := t.dl, t.changed
		t.mu.Unlock()
		var timeout <-chan time.Time
		var tm *time.Timer
		if !dl.IsZero() {
			d := time.Until(dl)
			if d <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			tm = time.NewTimer(d)
			timeout = tm.C
		}
		var b []byte
		ok, expired := true, false
		select {
		case b, ok = <-t.in:
		case <-timeout:
			expired = true
		case <-changed:
		}
		if tm != nil {
			tm.Stop()
		}
		switch {
		case expired:
			return 0, os.ErrDeadlineExceeded
		case !ok:
			return 0, t.rerr
		case b != nil:
			n := copy(p, b)
			t.pend = b[n:]
			return n, nil
		}
	}
}

func (t *sshTerm) Write(p []byte) (int, error) {
	hang := time.AfterFunc(writeTimeout, func() { t.conn.Close() })
	defer hang.Stop()
	return t.ch.Write(p)
}
