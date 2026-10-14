package server

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A unix socket is made with its mode from the start and without
// touching the umask: files made meanwhile keep theirs (a door's script,
// made as the server started, lost its x bits and would not run).
func TestSocketLeavesUmask(t *testing.T) {
	dir := t.TempDir()
	s := &Server{}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			l := &Listener{Name: "s", Address: filepath.Join(dir, fmt.Sprintf("s%d.sock", i%4)), SocketMode: 0660}
			ln, err := s.listen(l)
			if err != nil {
				t.Error(err)
				return
			}
			if fi, err := os.Stat(l.path()); err != nil || fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0660 {
				t.Errorf("socket: %v %v", fi.Mode(), err)
			}
			if c, err := net.Dial("unix", l.path()); err != nil {
				t.Errorf("dial: %v", err)
			} else {
				c.Close()
			}
			ln.Close()
			if _, err := os.Lstat(l.path()); !os.IsNotExist(err) {
				t.Errorf("socket left behind: %v", err)
			}
		}
	}()
	want := os.FileMode(0755)
	probe := filepath.Join(t.TempDir(), "probe")
	os.WriteFile(probe, nil, 0755)
	if fi, err := os.Stat(probe); err == nil {
		want = fi.Mode().Perm() // as the umask leaves it
	}
	for i := 0; i < 3000; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%d", i))
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
		if fi, _ := os.Stat(p); fi.Mode().Perm() != want {
			t.Fatalf("file %d made %v, want %v", i, fi.Mode().Perm(), want)
		}
		os.Remove(p)
	}
	close(stop)
	wg.Wait()
	if left, _ := filepath.Glob(filepath.Join(dir, ".s*")); len(left) > 0 {
		t.Errorf("left behind: %v", left)
	}
}
