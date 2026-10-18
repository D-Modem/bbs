package xfer

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// ckermit runs C-Kermit in dir, connected by a raw TCP socket to our end,
// with the commands given; it returns our end and a wait.
func ckermit(t *testing.T, dir string, commands string) (net.Conn, func() error) {
	t.Helper()
	if _, err := exec.LookPath("kermit"); err != nil {
		t.Skip("no kermit")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	script := fmt.Sprintf("set quiet on\nset host 127.0.0.1 %d /raw-socket\nif fail exit 2\n"+
		"set file type binary\nset file names literal\nset transfer display none\n%s\nexit \\v(status)\n", port, commands)
	os.WriteFile(filepath.Join(dir, "k.ksc"), []byte(script), 0600)
	cmd := exec.Command("kermit", "-Y", "-B", "-C", "take k.ksc")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ln.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second))
	c, err := ln.Accept()
	if err != nil {
		cmd.Process.Kill()
		t.Fatalf("kermit did not connect: %v\n%s", err, out.String())
	}
	t.Cleanup(func() { c.Close() })
	wait := func() error {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Logf("kermit: %v\n%s", err, out.String())
			}
			return err
		case <-time.After(60 * time.Second):
			cmd.Process.Kill()
			return errors.New("kermit timed out")
		}
	}
	return c, wait
}

// TestReceiveKermit: files C-Kermit sends, empty, short and long, and one of
// runs of prefix and 8-bit characters, come whole and in order.
func TestReceiveKermit(t *testing.T) {
	dir := t.TempDir()
	var names []string
	var want [][]byte
	for i, n := range []int{0, 1, 94, 5000, 70001} {
		d := data(n, int64(i))
		if i == 3 {
			// runs, to exercise repeat counts
			d = bytes.Repeat([]byte{0, 0xff, '#', '~', '&'}, 1000)
		}
		name := "kf" + string(rune('a'+i))
		os.WriteFile(filepath.Join(dir, name), d, 0600)
		names = append(names, name)
		want = append(want, d)
	}
	c, wait := ckermit(t, dir, "send kf*")
	var s memSink
	err := ReceiveKermit(c, s.sink)
	werr := wait()
	if err != nil || werr != nil {
		t.Fatalf("%v, kermit %v", err, werr)
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

// TestSendKermit: the files we send C-Kermit, empty, short and long, and one
// of runs of prefix and 8-bit characters, come whole.
func TestSendKermit(t *testing.T) {
	dir := t.TempDir()
	var out []Outgoing
	var want [][]byte
	for i, n := range []int{0, 1, 94, 5000, 70001} {
		d := data(n, int64(i))
		if i == 3 {
			d = bytes.Repeat([]byte{0, 0xff, '#', '~', '&'}, 1000)
		}
		want = append(want, d)
		out = append(out, Outgoing{Name: "kf" + string(rune('a'+i)), Size: int64(len(d)),
			ModTime: time.Unix(1700000000, 0), Data: bytes.NewReader(d)})
	}
	c, wait := ckermit(t, dir, "receive")
	err := SendKermit(c, out)
	werr := wait()
	if err != nil || werr != nil {
		t.Fatalf("%v, kermit %v", err, werr)
	}
	for i, o := range out {
		got, err := os.ReadFile(filepath.Join(dir, o.Name))
		if err != nil || !bytes.Equal(got, want[i]) {
			t.Errorf("%s: %d bytes, %v", o.Name, len(got), err)
		}
	}
}

// between ourselves, with 8th-bit prefixing and block check 1 forced on
func TestKermitSevenBit(t *testing.T) {
	a, b := pair(t)
	saved := ourParams
	defer func() { ourParams = saved }()
	want := data(10000, 3)
	var s memSink
	done := make(chan error, 1)
	go func() { done <- ReceiveKermit(b, s.sink) }()
	ourParams.qbin = '&' // both ask: the sender's wins
	ourParams.chkt = '1'
	err := SendKermit(a, []Outgoing{{Name: "seven", Size: int64(len(want)), Data: bytes.NewReader(want)}})
	if err != nil || <-done != nil {
		t.Fatal(err)
	}
	if len(s.files) != 1 || !bytes.Equal(s.files[0].buf.Bytes(), want) {
		t.Errorf("got %d bytes", s.files[0].buf.Len())
	}
}
