package xfer

import (
	"bytes"
	"errors"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// pipePort is our end of a program's stdin and stdout.
type pipePort struct{ r, w *os.File }

// Read reads from the program's stdout.
func (p *pipePort) Read(b []byte) (int, error) { return p.r.Read(b) }

// Write writes to the program's stdin.
func (p *pipePort) Write(b []byte) (int, error) { return p.w.Write(b) }

// SetReadDeadline sets the stdout pipe's.
func (p *pipePort) SetReadDeadline(t time.Time) error { return p.r.SetReadDeadline(t) }

// run starts a program on a port of ours, in dir.
func run(t *testing.T, dir string, name string, args ...string) (*pipePort, func() error) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("no %s", name)
	}
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout = inR, outW
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	inR.Close()
	outW.Close()
	wait := func() error {
		inW.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Logf("%s: %v\n%s", name, err, stderr.String())
			}
			return err
		case <-time.After(30 * time.Second):
			cmd.Process.Kill()
			return errors.New("timed out")
		}
	}
	t.Cleanup(func() { outR.Close() })
	return &pipePort{outR, inW}, wait
}

// memFile is an Incoming in memory.
type memFile struct {
	name      string
	size      int64
	buf       bytes.Buffer
	committed bool
	aborted   bool
}

// Write keeps p.
func (m *memFile) Write(p []byte) (int, error) { return m.buf.Write(p) }

// Commit notes the file committed.
func (m *memFile) Commit() error { m.committed = true; return nil }

// Abort notes the file aborted.
func (m *memFile) Abort() { m.aborted = true }

type memSink struct{ files []*memFile }

// sink takes every file, into memory, and keeps it in files.
func (s *memSink) sink(name string, size int64) (Incoming, error) {
	f := &memFile{name: name, size: size}
	s.files = append(s.files, f)
	return f, nil
}

// data is n random bytes from seed, not ending in a SUB.
func data(n int, seed int64) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	if n > 0 && b[n-1] == sub {
		b[n-1] = 'x' // XMODEM cannot tell a trailing SUB from padding
	}
	return b
}

var sizes = []int{1, 127, 128, 129, 1000, 1024, 1025, 5000, 70000}

// TestReceiveXMODEM: files of sizes about the block's sx sends, by 128 and
// by 1K blocks, come whole, the padding taken off.
func TestReceiveXMODEM(t *testing.T) {
	for _, n := range sizes {
		for _, big := range []bool{false, true} {
			dir := t.TempDir()
			want := data(n, int64(n))
			os.WriteFile(filepath.Join(dir, "f"), want, 0600)
			args := []string{"-q", "f"}
			if big {
				args = []string{"-q", "-k", "f"}
			}
			p, wait := run(t, dir, "sx", args...)
			var s memSink
			err := ReceiveXMODEM(p, s.sink)
			werr := wait()
			if err != nil || werr != nil {
				t.Fatalf("%d bytes, 1K %v: %v, sx %v", n, big, err, werr)
			}
			if got := s.files[0].buf.Bytes(); !bytes.Equal(got, want) || !s.files[0].committed {
				t.Errorf("%d bytes, 1K %v: got %d bytes", n, big, len(got))
			}
		}
	}
}

// TestSendXMODEM: files of sizes about the block's we send rx, by 128 and
// by 1K blocks, come whole, but for the padding of SUBs.
func TestSendXMODEM(t *testing.T) {
	for _, n := range sizes {
		for _, big := range []bool{false, true} {
			dir := t.TempDir()
			want := data(n, int64(n))
			p, wait := run(t, dir, "rx", "-q", "-c", "f")
			err := SendXMODEM(p, Outgoing{Name: "f", Size: int64(n), Data: bytes.NewReader(want)}, big)
			werr := wait()
			if err != nil || werr != nil {
				t.Fatalf("%d bytes, 1K %v: %v, rx %v", n, big, err, werr)
			}
			got, _ := os.ReadFile(filepath.Join(dir, "f"))
			// rx keeps the padding: what came must start with the file
			// and be only SUBs after
			if !bytes.HasPrefix(got, want) || len(bytes.Trim(got[len(want):], "\x1a")) != 0 {
				t.Errorf("%d bytes, 1K %v: got %d bytes", n, big, len(got))
			}
		}
	}
}

// TestReceiveYMODEM: a batch sb sends, an empty file in it, comes whole, its
// names and sizes as sent.
func TestReceiveYMODEM(t *testing.T) {
	dir := t.TempDir()
	var names []string
	var want [][]byte
	for i, n := range []int{0, 1, 1024, 5000, 70001} {
		name := filepath.Join(dir, "file"+string(rune('a'+i)))
		d := data(n, int64(i))
		os.WriteFile(name, d, 0600)
		names = append(names, filepath.Base(name))
		want = append(want, d)
	}
	p, wait := run(t, dir, "sb", append([]string{"-q"}, names...)...)
	var s memSink
	err := ReceiveYMODEM(p, s.sink)
	werr := wait()
	if err != nil || werr != nil {
		t.Fatalf("%v, sb %v", err, werr)
	}
	if len(s.files) != len(want) {
		t.Fatalf("%d files", len(s.files))
	}
	for i, f := range s.files {
		if f.name != names[i] || f.size != int64(len(want[i])) || !bytes.Equal(f.buf.Bytes(), want[i]) {
			t.Errorf("%s: %q %d, %d bytes", names[i], f.name, f.size, f.buf.Len())
		}
	}
}

// TestSendYMODEM: a batch we send rb, an empty file in it, comes whole.
func TestSendYMODEM(t *testing.T) {
	dir := t.TempDir()
	var out []Outgoing
	var want [][]byte
	for i, n := range []int{0, 1, 1024, 5000, 70001} {
		d := data(n, int64(i))
		want = append(want, d)
		out = append(out, Outgoing{Name: "file" + string(rune('a'+i)), Size: int64(n),
			ModTime: time.Unix(1700000000, 0), Data: bytes.NewReader(d)})
	}
	p, wait := run(t, dir, "rb", "-q")
	err := SendYMODEM(p, out)
	werr := wait()
	if err != nil || werr != nil {
		t.Fatalf("%v, rb %v", err, werr)
	}
	for i, o := range out {
		got, err := os.ReadFile(filepath.Join(dir, o.Name))
		if err != nil || !bytes.Equal(got, want[i]) {
			t.Errorf("%s: %d bytes, %v", o.Name, len(got), err)
		}
	}
}
