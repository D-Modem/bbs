package xfer

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// text is n bytes of text that compresses (as a BBS's text files do).
func text(n int, seed int64) []byte {
	words := []string{"modem", "carrier", "the", "BBS", "file", "area", "sysop", "message", "ZMODEM",
		"of", "and", "a", "upload", "download", "baud", "line", "\r\n"}
	var b bytes.Buffer
	x := uint64(seed)*2654435761 + 1
	for b.Len() < n {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		b.WriteString(words[x%uint64(len(words))])
		b.WriteByte(' ')
	}
	return b.Bytes()[:n]
}

// counted is a line that counts what goes out on it.
type counted struct {
	net.Conn
	mu sync.Mutex
	n  int
}

func (c *counted) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.n += len(p)
	c.mu.Unlock()
	return c.Conn.Write(p)
}

// zsend sends files from a (through w) to a receiver on b, and gives what
// the receiver took.
func zsend(t *testing.T, w Port, b net.Conn, sink Sink, files []Outgoing) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- ReceiveZMODEM(b, sink) }()
	if err := SendZMODEM(w, files); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// text of many sizes about the frame's goes compressed, whole, in a fraction
// of the bytes
func TestZstdZMODEM(t *testing.T) {
	sizes := []int{0, 1, 1000, zstdChunk - 1, zstdChunk, zstdChunk + 1, 300001}
	var out []Outgoing
	var want [][]byte
	total := 0
	for i, n := range sizes {
		d := text(n, int64(i))
		want = append(want, d)
		total += n
		out = append(out, Outgoing{Name: fmt.Sprintf("f%d.txt", i), Size: int64(n), Data: bytes.NewReader(d)})
	}
	a, b := pair(t)
	c := &counted{Conn: a}
	var s memSink
	zsend(t, c, b, s.sink, out)
	if len(s.files) != len(want) {
		t.Fatalf("%d files", len(s.files))
	}
	for i, f := range s.files {
		if !f.committed || !bytes.Equal(f.buf.Bytes(), want[i]) {
			t.Errorf("%s: %d bytes, committed %v", f.name, f.buf.Len(), f.committed)
		}
	}
	if c.n > total/2 {
		t.Errorf("%d bytes on the line for %d of text", c.n, total)
	}
	t.Logf("%d bytes on the line for %d of text", c.n, total)
}

// a receiver that does not take zstd (Zstd off, as a legacy one) gets the
// file as it is; so does a file of a compressed type
func TestZstdNotUsed(t *testing.T) {
	d := text(100000, 3)
	for _, tc := range []struct {
		name string
		off  bool
	}{{"plain.txt", true}, {"archive.zip", false}} {
		a, b := pair(t)
		c := &counted{Conn: a}
		var s memSink
		Zstd = !tc.off
		zsend(t, c, b, s.sink, []Outgoing{{Name: tc.name, Size: int64(len(d)), Data: bytes.NewReader(d)}})
		Zstd = true
		if len(s.files) != 1 || !bytes.Equal(s.files[0].buf.Bytes(), d) {
			t.Fatalf("%s: wrong file", tc.name)
		}
		if c.n < len(d) {
			t.Errorf("%s: %d bytes on the line for %d: compressed", tc.name, c.n, len(d))
		}
	}
}

// errors on the line cost a frame: ZRPOS goes back to the last whole one
func TestZstdNoisy(t *testing.T) {
	want := text(150000, 9)
	a, b := pair(t)
	var s memSink
	zsend(t, &noisy{Conn: a, every: 10000}, b, s.sink,
		[]Outgoing{{Name: "noisy.txt", Size: int64(len(want)), Data: bytes.NewReader(want)}})
	if len(s.files) != 1 || !bytes.Equal(s.files[0].buf.Bytes(), want) {
		t.Fatal("wrong file")
	}
}

// resumable is a memFile that has the start of the file from before.
type resumable struct {
	memFile
	had []byte
}

func (r *resumable) Resume() int64 { r.buf.Write(r.had); return int64(len(r.had)) }

// a file cut short goes on from where it stopped, with zstd and without
func TestZMODEMResume(t *testing.T) {
	for _, name := range []string{"resume.txt", "resume.zip"} {
		want := text(200000, 5)
		f := &resumable{had: want[:123457]}
		a, b := pair(t)
		c := &counted{Conn: a}
		sink := func(string, int64) (Incoming, error) { return f, nil }
		zsend(t, c, b, sink, []Outgoing{{Name: name, Size: int64(len(want)), Data: bytes.NewReader(want)}})
		if !f.committed || !bytes.Equal(f.buf.Bytes(), want) {
			t.Fatalf("%s: %d bytes, committed %v", name, f.buf.Len(), f.committed)
		}
		if name == "resume.zip" && c.n > len(want)-123457+2000 {
			t.Errorf("%s: %d bytes on the line: not resumed", name, c.n)
		}
	}
}

// frames that the reference zstd makes are taken as ours are
func TestZstdReferenceFrames(t *testing.T) {
	if _, err := exec.LookPath("zstd"); err != nil {
		t.Skip("no zstd")
	}
	dir := t.TempDir()
	in := text(zstdChunk, 11)
	os.WriteFile(filepath.Join(dir, "in"), in, 0600)
	cmd := exec.Command("zstd", "-q", "-19", "--content-size", "--check", "-o", "out", "in")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	frame, _ := os.ReadFile(filepath.Join(dir, "out"))
	// byte by byte, as from subpackets: complete only at the end
	for i := 1; i < len(frame); i++ {
		if n, err := zstdFrameLen(frame[:i]); n != 0 || err != nil {
			t.Fatalf("at %d of %d: %d %v", i, len(frame), n, err)
		}
	}
	if n, err := zstdFrameLen(frame); n != len(frame) || err != nil {
		t.Fatalf("%d of %d, %v", n, len(frame), err)
	}
	got, err := zstdDecode(frame)
	if err != nil || !bytes.Equal(got, in) {
		t.Fatalf("decode: %v", err)
	}
	// and ours, by the reference
	ours := zstdFrame(in)
	os.WriteFile(filepath.Join(dir, "ours.zst"), ours, 0600)
	cmd = exec.Command("zstd", "-q", "-d", "-c", "ours.zst")
	cmd.Dir = dir
	ref, err := cmd.Output()
	if err != nil || !bytes.Equal(ref, in) {
		t.Fatalf("reference decode: %v", err)
	}
	// a frame without the content size or the checksum is refused
	cmd = exec.Command("zstd", "-q", "--no-check", "-c", "in")
	cmd.Dir = dir
	bare, _ := cmd.Output()
	if _, err := zstdDecode(bare); err == nil {
		t.Error("a frame without a checksum taken")
	}
}
