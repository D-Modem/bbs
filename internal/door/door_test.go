package door

import (
	"bufio"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func info() *Info {
	return &Info{Node: 3, UserID: 7, UserName: "Alice Smith", Location: "Zürich", Logins: 5, Posts: 2,
		LastLogin: time.Date(2026, 10, 9, 18, 30, 0, 0, time.UTC), ANSI: true, Rows: 24, Cols: 80,
		Baud: 38400, BBSName: "Test BBS", SysopName: "Sid Sysop", Now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
}

func TestDropFiles(t *testing.T) {
	dir := t.TempDir()
	in := info()
	in.TimeLeft = 30 * time.Minute
	in.ComPort, in.CommType = 1, 1
	if err := WriteDropFiles(dir, in, DropFiles); err != nil {
		t.Fatal(err)
	}
	read := func(name string) []string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(string(b), "\r\n") {
			t.Errorf("%s: lines not CR LF", name)
		}
		return strings.Split(strings.TrimSuffix(string(b), "\r\n"), "\r\n")
	}
	ds := read("DOOR.SYS")
	if len(ds) != 52 || ds[0] != "COM1:" || ds[3] != "3" || ds[9] != "Alice Smith" || ds[10] != "Z\x81rich" ||
		ds[17] != "1800" || ds[18] != "30" || ds[19] != "GR" || ds[25] != "7" || ds[34] != "Sid Sysop" {
		t.Errorf("DOOR.SYS %q", ds)
	}
	di := read("DORINFO1.DEF")
	if len(di) != 13 || di[0] != "Test BBS" || di[1] != "Sid" || di[2] != "Sysop" || di[3] != "COM1" ||
		di[6] != "Alice" || di[7] != "Smith" || di[9] != "1" || di[11] != "30" {
		t.Errorf("DORINFO1.DEF %q", di)
	}
	d32 := read("DOOR32.SYS")
	if len(d32) != 11 || d32[0] != "1" || d32[1] != "1" || d32[5] != "Alice Smith" || d32[8] != "30" || d32[10] != "3" {
		t.Errorf("DOOR32.SYS %q", d32)
	}
	ch := read("CHAIN.TXT")
	if len(ch) != 31 || ch[0] != "7" || ch[1] != "Alice Smith" || ch[15] != "1800.00" {
		t.Errorf("CHAIN.TXT %q", ch)
	}
}

func TestSplit(t *testing.T) {
	got, err := Split(`docker run -v {dir}:/node bbs 'CD \LORD' "LORD.EXE /P\"x\"" C:\DOORS`)
	want := []string{"docker", "run", "-v", "{dir}:/node", "bbs", `CD \LORD`, `LORD.EXE /P"x"`, `C:\DOORS`}
	if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := Split(`a 'b`); err == nil {
		t.Fatal("unclosed quote")
	}
}

// script is a door: a shell script.
func script(t *testing.T, body string) *Door {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "door.sh")
	os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0755)
	return &Door{Name: "test", Command: []string{p}, Dir: filepath.Join(dir, "node{node}"),
		DropFiles: []string{DoorSys}, Charset: CP437, TimeLimit: time.Minute, CommType: 1}
}

// caller runs the door on a pipe, and gives the caller's end.
func caller(t *testing.T, d *Door, utf8 bool, idle time.Duration) (net.Conn, chan Result) {
	ours, theirs := net.Pipe()
	done := make(chan Result, 1)
	go func() {
		res, err := d.Run(ours, info(), utf8, idle, t.Logf)
		if err != nil {
			t.Error(err)
		}
		ours.Close()
		done <- res
	}()
	return theirs, done
}

func TestRun(t *testing.T) {
	// it reads its drop file, prints CP437, echoes a line
	d := script(t, `sed -n 10p DOOR.SYS | tr -d '\r'
printf 'caf\202\r\n'
read -r line
printf 'got:%s\r\n' "$line" | od -An -c | tr -s ' '
exit 3
`)
	c, done := caller(t, d, true, 0)
	r := bufio.NewReader(c)
	if l, _ := r.ReadString('\n'); l != "Alice Smith\n" {
		t.Errorf("%q", l)
	}
	if l, _ := r.ReadString('\n'); l != "café\r\n" {
		t.Errorf("CP437 to UTF-8: %q", l)
	}
	c.Write([]byte("é\r\n")) // CR LF: the door gets CR only? sh reads to LF
	c.Write([]byte("\n"))
	rest, _ := io.ReadAll(r)
	if !strings.Contains(string(rest), "g o t : 202 \\r") {
		t.Errorf("UTF-8 to CP437: %q", rest)
	}
	res := <-done
	if res.ExitErr == nil || res.TimeUp || res.HungUp {
		t.Errorf("%+v", res)
	}
}

func TestTimeLimitAndHangup(t *testing.T) {
	d := script(t, "trap '' HUP\nsleep 30\n")
	d.TimeLimit = time.Second
	_, done := caller(t, d, false, 0)
	select {
	case res := <-done:
		if !res.TimeUp {
			t.Errorf("%+v", res)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the time limit did not end it")
	}

	d = script(t, "sleep 30\n")
	c, done := caller(t, d, false, 0)
	time.Sleep(200 * time.Millisecond)
	c.Close()
	select {
	case res := <-done:
		if !res.HungUp {
			t.Errorf("%+v", res)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("hanging up did not end it")
	}

	d = script(t, "sleep 30\n")
	_, done = caller(t, d, false, 300*time.Millisecond)
	if res := <-done; !res.Idle {
		t.Errorf("idle: %+v", res)
	}
}

func TestSingleAndCleanup(t *testing.T) {
	d := script(t, "sleep 1\n")
	d.Single = true
	mark := filepath.Join(t.TempDir(), "cleaned")
	d.Cleanup = []string{"touch", mark + "{node}"}
	c, done := caller(t, d, false, 0)
	defer c.Close()
	time.Sleep(200 * time.Millisecond)
	ours, _ := net.Pipe()
	if _, err := d.Run(ours, info(), false, 0, t.Logf); err != ErrBusy {
		t.Errorf("a second caller: %v", err)
	}
	<-done
	if _, err := os.Stat(mark + "3"); err != nil {
		t.Error("cleanup did not run")
	}
}

func TestPTY(t *testing.T) {
	d := script(t, `tty; stty size; printf 'x\n'`)
	d.PTY = true
	c, done := caller(t, d, false, 0)
	out, _ := io.ReadAll(c)
	<-done
	if !strings.Contains(string(out), "/dev/pts/") || !strings.Contains(string(out), "24 80") {
		t.Errorf("%q", out)
	}
	if strings.Contains(string(out), "x\r\n") {
		t.Errorf("output post-processed: %q", out)
	}
}
