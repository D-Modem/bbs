package xfer

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestReceiveZMODEM(t *testing.T) {
	dir := t.TempDir()
	var names []string
	var want [][]byte
	for i, n := range []int{0, 1, 1024, 5000, 300001} {
		d := data(n, int64(i))
		name := "zf" + string(rune('a'+i))
		os.WriteFile(filepath.Join(dir, name), d, 0600)
		names = append(names, name)
		want = append(want, d)
	}
	p, wait := run(t, dir, "sz", append([]string{"-q"}, names...)...)
	var s memSink
	err := ReceiveZMODEM(p, s.sink)
	werr := wait()
	if err != nil || werr != nil {
		t.Fatalf("%v, sz %v", err, werr)
	}
	if len(s.files) != len(want) {
		t.Fatalf("%d files", len(s.files))
	}
	for i, f := range s.files {
		if f.name != names[i] || !f.committed || !bytes.Equal(f.buf.Bytes(), want[i]) {
			t.Errorf("%s: %q, %d bytes, committed %v", names[i], f.name, f.buf.Len(), f.committed)
		}
	}
}

func TestSendZMODEM(t *testing.T) {
	dir := t.TempDir()
	var out []Outgoing
	var want [][]byte
	for i, n := range []int{0, 1, 1024, 5000, 300001} {
		d := data(n, int64(i))
		want = append(want, d)
		out = append(out, Outgoing{Name: "zf" + string(rune('a'+i)), Size: int64(n),
			ModTime: time.Unix(1700000000, 0), Data: bytes.NewReader(d)})
	}
	p, wait := run(t, dir, "rz", "-q")
	err := SendZMODEM(p, out)
	werr := wait()
	if err != nil || werr != nil {
		t.Fatalf("%v, rz %v", err, werr)
	}
	for i, o := range out {
		got, err := os.ReadFile(filepath.Join(dir, o.Name))
		if err != nil || !bytes.Equal(got, want[i]) {
			t.Errorf("%s: %d bytes, %v", o.Name, len(got), err)
		}
	}
}

// noisy is a line that damages a byte now and then, one way.
type noisy struct {
	net.Conn
	mu    sync.Mutex
	n     int
	every int
}

func (c *noisy) Write(p []byte) (int, error) {
	c.mu.Lock()
	q := append([]byte(nil), p...)
	for i := range q {
		if c.n++; c.n%c.every == 0 {
			q[i] ^= 0x55
		}
	}
	c.mu.Unlock()
	return c.Conn.Write(q)
}

// pair is two ends of a line, through a TCP socket (deadlines and all).
func pair(t *testing.T) (net.Conn, net.Conn) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ch := make(chan net.Conn)
	go func() { c, _ := ln.Accept(); ch <- c }()
	a, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b := <-ch
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// both ways between ourselves, over a line that damages every 20000th
// byte from the sender: ZRPOS brings the data back
func TestZMODEMNoisy(t *testing.T) {
	a, b := pair(t)
	sender := &noisy{Conn: a, every: 20000}
	want := data(200000, 7)
	var s memSink
	done := make(chan error, 1)
	go func() { done <- ReceiveZMODEM(b, s.sink) }()
	if err := SendZMODEM(sender, []Outgoing{{Name: "noisy.bin", Size: int64(len(want)), Data: bytes.NewReader(want)}}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(s.files) != 1 || !bytes.Equal(s.files[0].buf.Bytes(), want) {
		for _, f := range s.files {
			t.Logf("file %q %d bytes committed %v aborted %v", f.name, f.buf.Len(), f.committed, f.aborted)
		}
		t.Errorf("got %d files", len(s.files))
	}
}

// a refused file is skipped, the batch goes on
func TestZMODEMSkip(t *testing.T) {
	a, b := pair(t)
	var got []string
	sink := func(name string, size int64) (Incoming, error) {
		if name == "no" {
			return nil, io.ErrShortWrite
		}
		got = append(got, name)
		return &memFile{name: name}, nil
	}
	done := make(chan error, 1)
	go func() { done <- ReceiveZMODEM(b, sink) }()
	err := SendZMODEM(a, []Outgoing{
		{Name: "no", Size: 3, Data: bytes.NewReader([]byte("abc"))},
		{Name: "yes", Size: 3, Data: bytes.NewReader([]byte("def"))},
	})
	if err != nil || <-done != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "yes" {
		t.Errorf("got %v", got)
	}
}
