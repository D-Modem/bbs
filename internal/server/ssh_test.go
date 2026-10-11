package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// startSSH: a BBS with an SSH listener; its address and host key file.
func startSSH(t *testing.T, tweak func(*bbs.Settings)) (*env, string, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	hk := filepath.Join(t.TempDir(), "ssh_host_ed25519_key")
	e := start(t, tweak, &Listener{Name: "ssh", Kind: "ssh", Address: addr, Via: "ssh",
		ANSI: "auto", UTF8: "auto", Sixel: "no", HostKeys: []string{hk}})
	return e, addr, hk
}

func sshDial(t *testing.T, addr, user string, auth ...ssh.AuthMethod) (*ssh.Client, error) {
	t.Helper()
	return ssh.Dial("tcp", addr, &ssh.ClientConfig{User: user, Auth: auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
}

// shell opens a session with a pty and a shell: a dialog on it.
func shell(t *testing.T, c *ssh.Client, term string, cols, rows int) *dialog {
	t.Helper()
	s, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if term != "" {
		if err := s.RequestPty(term, rows, cols, ssh.TerminalModes{}); err != nil {
			t.Fatal(err)
		}
	}
	in, _ := s.StdinPipe()
	out, _ := s.StdoutPipe()
	if err := s.Shell(); err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	go func() { io.Copy(a, out); a.Close() }()
	go io.Copy(in, a)
	t.Cleanup(func() { s.Close(); b.Close() })
	return &dialog{t: t, c: b}
}

// TestSSH: logging in by password, keyboard-interactive and key, as "new";
// the terminal the pty said; what is refused.
func TestSSH(t *testing.T) {
	e, addr, hk := startSSH(t, nil)

	// a password; the pty's terminal type and size
	c, err := sshDial(t, addr, "alice", ssh.Password("alicepass1"))
	if err != nil {
		t.Fatal(err)
	}
	d := shell(t, c, "xterm-256color", 100, 40)
	d.expect("Welcome, ")
	d.expect("alice")
	d.expect("Command: ")
	d.send("P")
	d.expect("Terminal:  100x40, ANSI on, UTF-8 on")
	d.send("Q")
	d.expect("Command: ")

	// the host key was made, private, and is the one the server shows
	fi, err := os.Stat(hk)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("host key: %v %v", fi, err)
	}
	if !strings.HasPrefix(HostKeyInfo(hk), "ssh-ed25519 SHA256:") {
		t.Fatalf("host key info: %s", HostKeyInfo(hk))
	}

	// no commands, sftp, port forwarding, or a second session
	if s, err := c.NewSession(); err == nil {
		if err := s.Run("id"); err == nil {
			t.Fatal("exec ran")
		}
	}
	if s, err := c.NewSession(); err == nil {
		if err := s.RequestSubsystem("sftp"); err == nil {
			t.Fatal("sftp started")
		}
	}
	if _, err := c.Dial("tcp", "127.0.0.1:22"); err == nil {
		t.Fatal("port forwarding")
	}
	c.Close()

	// wrong passwords, unknown users
	for _, u := range []struct{ name, pw string }{{"alice", "wrong"}, {"nobody", "alicepass1"}} {
		if _, err := sshDial(t, addr, u.name, ssh.Password(u.pw)); err == nil {
			t.Fatalf("%s/%s logged in", u.name, u.pw)
		}
	}

	// keyboard-interactive, with a name in another case
	ki := ssh.KeyboardInteractive(func(user, instr string, qs []string, echo []bool) ([]string, error) {
		if len(qs) != 1 || echo[0] {
			t.Errorf("questions: %q %v", qs, echo)
		}
		return []string{"sysoppass1"}, nil
	})
	c, err = sshDial(t, addr, "SYSOP", ki)
	if err != nil {
		t.Fatal(err)
	}
	d = shell(t, c, "vt100", 80, 24)
	d.expect("Welcome, ")
	d.expect("sysop")
	c.Close()

	// "new": no password, the BBS's own prompt
	c, err = sshDial(t, addr, "new")
	if err != nil {
		t.Fatal(err)
	}
	d = shell(t, c, "ansi", 80, 25)
	d.expect("(or NEW)")
	d.send("NEW\r")
	d.expect("name")
	c.Close()

	// a key: alice's opens alice's account only
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	sp, _ := ssh.NewPublicKey(pub)
	u, _ := e.b.Store.UserByName("alice")
	k, err := e.b.AddSSHKey(u, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp)))+" alice@laptop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.b.AddSSHKey(u, string(ssh.MarshalAuthorizedKey(sp))); err != store.ErrKeyTaken {
		t.Fatalf("the same key twice: %v", err)
	}
	if _, err := sshDial(t, addr, "sysop", ssh.PublicKeys(signer)); err == nil {
		t.Fatal("alice's key logged in as sysop")
	}
	c, err = sshDial(t, addr, "alice", ssh.PublicKeys(signer))
	if err != nil {
		t.Fatal(err)
	}
	d = shell(t, c, "xterm", 80, 24)
	d.expect("Welcome, ")
	d.expect("alice")
	d.expect("Command: ")
	c.Close()
	if ks, _ := e.b.Store.SSHKeys(u.ID); len(ks) != 1 || ks[0].LastUsed.IsZero() || ks[0].Fingerprint != k.Fingerprint {
		t.Fatalf("keys: %+v", ks)
	}
	// someone else's key is no key; a locked user gets in by neither
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	os2, _ := ssh.NewSignerFromKey(other)
	if _, err := sshDial(t, addr, "alice", ssh.PublicKeys(os2)); err == nil {
		t.Fatal("an unknown key logged in")
	}
	e.b.Store.SetLocked(u.ID, true)
	if _, err := sshDial(t, addr, "alice", ssh.PublicKeys(signer)); err == nil {
		t.Fatal("a locked user logged in by key")
	}
	if _, err := sshDial(t, addr, "alice", ssh.Password("alicepass1")); err == nil {
		t.Fatal("a locked user logged in by password")
	}
}

// TestSSHKeysInProfile: a user adds a key on the text BBS (after giving
// their password), logs in with it, and deletes it.
func TestSSHKeysInProfile(t *testing.T) {
	_, addr, _ := startSSH(t, nil)
	c, err := sshDial(t, addr, "alice", ssh.Password("alicepass1"))
	if err != nil {
		t.Fatal(err)
	}
	d := shell(t, c, "vt100", 80, 24)
	d.expect("Command: ")
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sp, _ := ssh.NewPublicKey(pub)
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " alice@home"
	d.send("PK")
	d.expect("none: you log in by SSH with your password")
	d.send("A")
	d.expect("Your password: ")
	d.send("wrongpass1\r")
	d.expect("[Q]uit: ")
	d.send("A")
	d.expect("Your password: ")
	d.send("alicepass1\r")
	d.expect("Paste the public key")
	d.send("ssh-dss AAAA nope\r")
	d.expect("not an SSH public key")
	d.send("A")
	d.expect("Your password: ")
	d.send("alicepass1\r")
	d.expect("Paste the public key")
	d.send(line + "\r")
	d.expect("Added " + ssh.FingerprintSHA256(sp))
	d.expect("1. ssh-ed25519 " + ssh.FingerprintSHA256(sp) + " alice@home")

	signer, _ := ssh.NewSignerFromKey(priv)
	c2, err := sshDial(t, addr, "alice", ssh.PublicKeys(signer))
	if err != nil {
		t.Fatal(err)
	}
	c2.Close()

	d.send("D")
	d.expect("Which (its number)? ")
	d.send("1\r")
	d.expect("Deleted.")
	if _, err := sshDial(t, addr, "alice", ssh.PublicKeys(signer)); err == nil {
		t.Fatal("a deleted key logged in")
	}
}

// TestOpenSSH: the OpenSSH client, by key, with a pty (-tt).
func TestOpenSSH(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh")
	}
	e, addr, _ := startSSH(t, nil)
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	blk, _ := ssh.MarshalPrivateKey(priv, "")
	keyFile := filepath.Join(dir, "id_ed25519")
	os.WriteFile(keyFile, pem.EncodeToMemory(blk), 0600)
	sp, _ := ssh.NewPublicKey(pub)
	u, _ := e.b.Store.UserByName("alice")
	if _, err := e.b.AddSSHKey(u, string(ssh.MarshalAuthorizedKey(sp))); err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(addr)
	cmd := exec.Command("ssh", "-tt", "-F", "/dev/null", "-i", keyFile, "-p", port,
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "alice@"+host)
	cmd.Env = append(os.Environ(), "TERM=xterm")
	a, b := net.Pipe()
	cmd.Stdout, cmd.Stderr = a, io.Discard
	in, _ := cmd.StdinPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait(); a.Close() })
	d := &dialog{t: t, c: b}
	d.expect("Welcome, ")
	d.expect("alice")
	d.expect("Command: ")
	in.Write([]byte("GY"))
	d.expect("Thanks for calling.")
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	go io.Copy(io.Discard, b)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ssh did not end when the BBS hung up")
	}
}

func TestSSHConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		dir := t.TempDir()
		p := filepath.Join(dir, "bbs.conf")
		os.WriteFile(p, []byte("[general]\ndatabase = "+filepath.Join(dir, "db", "bbs.db")+"\n"+body), 0600)
		return Load(p)
	}
	c, err := load("[listen ssh]\ntype = ssh\naddress = :2222\n")
	if err != nil {
		t.Fatal(err)
	}
	l := c.Listeners[0]
	if l.Via != "ssh" || l.TN3270 || l.UTF8 != "auto" || len(l.HostKeys) != 2 ||
		filepath.Base(filepath.Dir(l.HostKeys[0])) != "db" || filepath.Base(l.HostKeys[1]) != "ssh_host_rsa_key" {
		t.Errorf("listener %+v", l)
	}
	c, err = load("[listen ssh]\ntype = ssh\naddress = :2222\nhost_keys = /etc/bbs/k1 /etc/bbs/k2\nlegacy = yes\n")
	if err != nil || strings.Join(c.Listeners[0].HostKeys, " ") != "/etc/bbs/k1 /etc/bbs/k2" || !c.Listeners[0].Legacy {
		t.Fatalf("%+v %v", c, err)
	}
	if !strings.Contains(strings.Join(c.Warnings, "\n"), "legacy offers") {
		t.Errorf("no warning: %v", c.Warnings)
	}
	for body, want := range map[string]string{
		"[listen s]\ntype = ssh\naddress = /run/bbs.sock\n":          "ssh is for a TCP port",
		"[listen s]\ntype = ssh\naddress = :22\nheader = yes\n":      "not for ssh",
		"[listen s]\ntype = ssh\naddress = :22\ntrust_user = yes\n":  "trust_user",
		"[listen s]\ntype = ssh\naddress = :22\ntn3270 = yes\n":      "tn3270",
		"[listen s]\ntype = telnet\naddress = :23\nhost_keys = /x\n": "host_keys",
		"[listen s]\ntype = telnet\naddress = :23\nlegacy = yes\n":   "legacy",
		"[listen s]\ntype = ssh\naddress = :22\nlegacy = maybe\n":    "legacy",
		"[listen s]\ntype = sshh\naddress = :22\n":                   "type",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
}

// TestHostKeys: made by their names' kinds, kept, and refused when others
// may read them.
func TestHostKeys(t *testing.T) {
	dir := t.TempDir()
	for name, typ := range map[string]string{"ssh_host_ed25519_key": "ssh-ed25519",
		"ssh_host_rsa_key": "ssh-rsa", "ssh_host_ecdsa_key": "ecdsa-sha2-nistp256"} {
		p := filepath.Join(dir, name)
		k, made, err := LoadHostKey(p)
		if err != nil || !made || k.PublicKey().Type() != typ {
			t.Fatalf("%s: %v %v %v", name, k, made, err)
		}
		k2, made, err := LoadHostKey(p)
		if err != nil || made || ssh.FingerprintSHA256(k2.PublicKey()) != ssh.FingerprintSHA256(k.PublicKey()) {
			t.Fatalf("%s again: %v %v", name, made, err)
		}
	}
	p := filepath.Join(dir, "ssh_host_ed25519_key")
	os.Chmod(p, 0644)
	if _, _, err := LoadHostKey(p); err == nil || !strings.Contains(err.Error(), "others may read it") {
		t.Fatalf("a readable key: %v", err)
	}
	if !strings.Contains(HostKeyInfo(filepath.Join(dir, "none")), "made at the first start") {
		t.Error(HostKeyInfo(filepath.Join(dir, "none")))
	}
}

// TestSSHLegacy: CBC ciphers only when legacy = yes.
func TestSSHLegacy(t *testing.T) {
	cbc := func(addr string) error {
		c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{User: "alice", Auth: []ssh.AuthMethod{ssh.Password("alicepass1")},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
			Config: ssh.Config{Ciphers: []string{"aes128-cbc"}, KeyExchanges: []string{"diffie-hellman-group1-sha1"}}})
		if err == nil {
			c.Close()
		}
		return err
	}
	_, addr, _ := startSSH(t, nil)
	if cbc(addr) == nil {
		t.Fatal("CBC without legacy")
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr = ln.Addr().String()
	ln.Close()
	start(t, nil, &Listener{Name: "ssh", Kind: "ssh", Address: addr, Via: "ssh", ANSI: "auto",
		HostKeys: []string{filepath.Join(t.TempDir(), "k")}, Legacy: true})
	if err := cbc(addr); err != nil {
		t.Fatalf("CBC with legacy: %v", err)
	}
}
