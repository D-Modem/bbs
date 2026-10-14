// Package sockets lets the BBS bind its ports as root and then run as a
// user of its own: the root process binds every listening socket, gives
// up root (groups, group, user) and executes itself again, the same
// process, the sockets handed down; there each server's Listen takes the
// socket for its address instead of binding one.
package sockets

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// env is how sockets are handed down: "3 tcp :23\n4 unix /run/bbs/x.sock".
const env = "BBS_SOCKETS"

var (
	mu        sync.Mutex
	inherited = map[string]net.Listener{} // by key: "tcp :23"
)

func key(network, address string) string { return network + " " + address }

// Inherit takes the sockets handed down, if any (once, at the start).
func Inherit() error {
	v := os.Getenv(env)
	if v == "" {
		return nil
	}
	os.Unsetenv(env)
	mu.Lock()
	defer mu.Unlock()
	for _, line := range strings.Split(v, "\n") {
		fdText, k, ok := strings.Cut(line, " ")
		fd, err := strconv.Atoi(fdText)
		if !ok || err != nil || fd < 3 {
			return fmt.Errorf("%s: %q", env, line)
		}
		syscall.CloseOnExec(fd)
		f := os.NewFile(uintptr(fd), k)
		ln, err := net.FileListener(f)
		f.Close() // FileListener has its own
		if err != nil {
			return fmt.Errorf("the socket for %s: %w", k, err)
		}
		inherited[k] = ln
	}
	return nil
}

// Take is the socket handed down for an address (nil: none).
func Take(network, address string) net.Listener {
	mu.Lock()
	defer mu.Unlock()
	k := key(network, address)
	ln := inherited[k]
	delete(inherited, k)
	return ln
}

// Listen is net.Listen, but for an address whose socket was handed down.
func Listen(network, address string) (net.Listener, error) {
	if ln := Take(network, address); ln != nil {
		return ln, nil
	}
	return net.Listen(network, address)
}

// Left are the sockets handed down that nothing took (the configuration
// changed between the two: they are closed).
func Left() []string {
	mu.Lock()
	defer mu.Unlock()
	var out []string
	for k, ln := range inherited {
		out = append(out, k)
		ln.Close()
		delete(inherited, k)
	}
	return out
}

// Account is who the BBS runs as.
type Account struct {
	User   string // as configured
	UID    int
	GID    int
	Groups []int // supplementary: the user's groups
}

// Lookup finds a user (a name or a number), and its group: group, or the
// user's own ("": its own).
func Lookup(name, group string) (*Account, error) {
	u, err := user.Lookup(name)
	if err != nil {
		if u, err = user.LookupId(name); err != nil {
			return nil, fmt.Errorf("user %q: no such user", name)
		}
	}
	a := &Account{User: name}
	a.UID, _ = strconv.Atoi(u.Uid)
	a.GID, _ = strconv.Atoi(u.Gid)
	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			if g, err = user.LookupGroupId(group); err != nil {
				return nil, fmt.Errorf("group %q: no such group", group)
			}
		}
		a.GID, _ = strconv.Atoi(g.Gid)
	}
	ids, _ := u.GroupIds()
	for _, id := range ids {
		if n, err := strconv.Atoi(id); err == nil {
			a.Groups = append(a.Groups, n)
		}
	}
	if a.UID == 0 {
		return nil, errors.New("user: root is who it runs as already: name another")
	}
	return a, nil
}

// Drop gives up root for the account, in this process: its groups, its
// group, its user.
func Drop(a *Account) error {
	if err := syscall.Setgroups(a.Groups); err != nil {
		return fmt.Errorf("setgroups: %w", err)
	}
	if err := syscall.Setgid(a.GID); err != nil {
		return fmt.Errorf("setgid %d: %w", a.GID, err)
	}
	if err := syscall.Setuid(a.UID); err != nil {
		return fmt.Errorf("setuid %d: %w", a.UID, err)
	}
	if os.Geteuid() == 0 || os.Getuid() == 0 {
		return errors.New("still root after setuid")
	}
	return nil
}

// Socket is a socket bound as root, to hand down.
type Socket struct {
	Network, Address string
	File             *os.File
}

// Become gives up root for the account and executes this program again
// with the same arguments, the sockets handed down; it returns only if it
// could not.
func Become(a *Account, socks []Socket) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	envv := os.Environ()
	if h, err := user.LookupId(strconv.Itoa(a.UID)); err == nil {
		envv = setEnv(setEnv(setEnv(envv, "HOME", h.HomeDir), "USER", h.Username), "LOGNAME", h.Username)
	}
	// each socket left at its own descriptor, open across the exec (not
	// moved to 3, 4 ...: the runtime's own are among those); the new
	// program's are made after them, in what is free
	var lines []string
	for _, s := range socks {
		fd := s.File.Fd()
		if _, _, e := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFD, 0); e != 0 {
			return fmt.Errorf("the socket for %s: %w", s.Address, e)
		}
		lines = append(lines, fmt.Sprintf("%d %s", fd, key(s.Network, s.Address)))
	}
	if len(lines) > 0 {
		envv = setEnv(envv, env, strings.Join(lines, "\n"))
	}
	if err := Drop(a); err != nil {
		return err
	}
	err = syscall.Exec(exe, os.Args, envv)
	if errors.Is(err, syscall.EACCES) {
		return fmt.Errorf("%s cannot run %s: %w", a.User, exe, err)
	}
	return err
}

// setEnv is env with k's value v (in its place, or added).
func setEnv(env []string, k, v string) []string {
	for i, e := range env {
		if strings.HasPrefix(e, k+"=") {
			env[i] = k + "=" + v
			return env
		}
	}
	return append(env, k+"="+v)
}
