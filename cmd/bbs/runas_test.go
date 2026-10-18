package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestRunAs: started as root with [general] user, serve binds its ports
// (one under 1024) and unix socket as root, then runs as the user, the
// same process; what it makes is the user's.  Root only.
func TestRunAs(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("not root")
	}
	nobody, err := user.Lookup("nobody")
	if err != nil {
		t.Skip("no nobody")
	}
	port := 0
	for p := 1000; p > 600; p-- {
		if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p)); err == nil {
			ln.Close()
			port = p
			break
		}
	}
	if port == 0 {
		t.Skip("no free port under 1024")
	}
	// a directory nobody can reach (t.TempDir's parent may not be)
	dir, err := os.MkdirTemp("/tmp", "bbs-runas-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	os.Chmod(dir, 0o755)
	data := filepath.Join(dir, "data")
	os.Mkdir(data, 0o700)
	var uid, gid int
	fmt.Sscan(nobody.Uid, &uid)
	fmt.Sscan(nobody.Gid, &gid)
	os.Chown(data, uid, gid)
	exe := filepath.Join(dir, "bbs")
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	conf := filepath.Join(dir, "bbs.conf")
	sock := filepath.Join(data, "menu.sock")
	os.WriteFile(conf, []byte(fmt.Sprintf(`[general]
name = RunAs BBS
user = nobody
database = %s
files_dir = %s
log = stderr
attempts = 1
[listen low]
type = raw
address = 127.0.0.1:%d
ansi = no
[listen menu]
type = raw
address = %s
ansi = no
`, filepath.Join(data, "bbs.db"), filepath.Join(data, "files"), port, sock)), 0o644)

	// a command as root makes the database as the user
	if out, err := exec.Command(exe, "-config", conf, "init").CombinedOutput(); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if fi, err := os.Stat(filepath.Join(data, "bbs.db")); err != nil || fi.Sys().(*syscall.Stat_t).Uid != uint32(uid) {
		t.Fatalf("the database is not nobody's: %v", err)
	}

	cmd := exec.Command(exe, "-config", conf, "serve")
	log := &syncBuf{}
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cmd.Process.Signal(syscall.SIGTERM)
		cmd.Wait()
		if t.Failed() {
			t.Log(log.String())
		}
	}()
	var c net.Conn
	for i := 0; i < 100; i++ {
		if c, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("port %d: %v", port, err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	var got string
	for i := 0; i < 5 && !strings.Contains(got, "RunAs BBS"); i++ {
		l, _ := r.ReadString('\n')
		got += l
	}
	if !strings.Contains(got, "RunAs BBS") {
		t.Errorf("banner %q", got)
	}
	c.Close()
	u, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	u.Close()
	status, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", cmd.Process.Pid))
	if !strings.Contains(string(status), fmt.Sprintf("Uid:\t%d\t%d\t%d\t%d", uid, uid, uid, uid)) {
		t.Errorf("not nobody:\n%s", status)
	}
	if fi, err := os.Stat(sock); err != nil || fi.Sys().(*syscall.Stat_t).Uid != uint32(uid) {
		t.Errorf("the socket is not nobody's")
	}
	// a reload, the same process
	cmd.Process.Signal(syscall.SIGHUP)
	time.Sleep(300 * time.Millisecond)
	if !strings.Contains(log.String(), "reload: ") {
		t.Errorf("no reload")
	}
}

// syncBuf is a buffer written by one goroutine (the command's output) and
// read by another.
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

// Write adds p to the buffer.
func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

// String is what has been written so far.
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
