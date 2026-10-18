package door

import (
	"bytes"
	"crypto/ed25519"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// listen serves each connection with handle, on a local port.
func listen(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				handle(c)
			}()
		}
	}()
	return ln.Addr().String()
}

// readUntil reads r until want has come; what came.
func readUntil(t *testing.T, r io.Reader, want string) string {
	t.Helper()
	if c, ok := r.(net.Conn); ok {
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer c.SetReadDeadline(time.Time{})
	}
	var got []byte
	buf := make([]byte, 256)
	for !bytes.Contains(got, []byte(want)) {
		n, err := r.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			t.Fatalf("waiting for %q: %v; got %q", want, err, got)
		}
	}
	return string(got)
}

// play is a door's side: it greets, then waits for q and says bye.
func play(t *testing.T, c net.Conn, greeting string) {
	io.WriteString(c, greeting)
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64)
	for {
		n, err := c.Read(buf)
		if bytes.Contains(buf[:n], []byte("q")) {
			io.WriteString(c, "bye\r\n")
			return
		}
		if err != nil {
			return
		}
	}
}

// visit runs a door for a caller who reads want, types q and reads bye.
func visit(t *testing.T, d *Door, want string) Result {
	t.Helper()
	c, done := caller(t, d, false, 0)
	readUntil(t, c, want)
	io.WriteString(c, "q")
	readUntil(t, c, "bye")
	select {
	case res := <-done:
		return res
	case <-time.After(5 * time.Second):
		t.Fatal("the door never ended")
	}
	return Result{}
}

// TestTelnetDoor: a telnet door's negotiation is answered (terminal type,
// window size, echo, an unknown option refused), and IACs in the text come
// through single.
func TestTelnetDoor(t *testing.T) {
	answers := make(chan string, 1)
	addr := listen(t, func(c net.Conn) {
		// what a telnet server asks of a terminal
		c.Write([]byte{tIAC, tDO, optTType, tIAC, tDO, optNAWS, tIAC, tWILL, optEcho, tIAC, tDO, 99,
			tIAC, tSB, optTType, 1, tIAC, tSE})
		want := [][]byte{{tIAC, tWILL, optTType}, {tIAC, tWILL, optNAWS}, {tIAC, tSB, optNAWS, 0, 80, 0, 24, tIAC, tSE},
			{tIAC, tDO, optEcho}, {tIAC, tWONT, 99}, append(append([]byte{tIAC, tSB, optTType, 0}, "ANSI"...), tIAC, tSE)}
		var got []byte
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 64)
		for {
			all := true
			for _, w := range want {
				all = all && bytes.Contains(got, w)
			}
			if all {
				break
			}
			n, err := c.Read(buf)
			got = append(got, buf[:n]...)
			if err != nil {
				answers <- fmt.Sprintf("only % x", got)
				return
			}
		}
		answers <- "ok"
		// a 0xFF in the text is doubled; the caller's 0xFF comes doubled
		play(t, c, "Welcome \xff\xffhere\r\n")
	})
	d := &Door{Name: "far", Charset: Raw, Remote: &Remote{Kind: KindTelnet, Address: addr}}
	res := visit(t, d, "Welcome \xffhere")
	if a := <-answers; a != "ok" {
		t.Errorf("negotiation: %s", a)
	}
	if res.HungUp || res.TimeUp {
		t.Errorf("result %+v", res)
	}
}

// rloginServer checks the start it is sent, and plays.
func rloginServer(t *testing.T, want string) (string, chan string) {
	got := make(chan string, 1)
	addr := listen(t, func(c net.Conn) {
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		var b []byte
		buf := make([]byte, 1)
		for strings.Count(string(b), "\x00") < 4 {
			if _, err := c.Read(buf); err != nil {
				got <- "short: " + string(b)
				return
			}
			b = append(b, buf[0])
		}
		got <- string(b)
		if string(b) != want {
			io.WriteString(c, "\x01bad")
			return
		}
		c.Write([]byte{0})
		play(t, c, "rlogin door\r\n")
	})
	return addr, got
}

// TestRLoginDoor: an RLogin door is sent the caller's name, the server user
// filled in and the terminal and speed; a server that does not answer with a
// zero byte refuses the caller.
func TestRLoginDoor(t *testing.T) {
	addr, got := rloginServer(t, "\x00Alice Smith\x00guest-7\x00ansi/38400\x00")
	d := &Door{Name: "rl", Charset: Raw, Remote: &Remote{Kind: KindRLogin, Address: addr, ServerUser: "guest-{userid}"}}
	visit(t, d, "rlogin door")
	if g := <-got; g != "\x00Alice Smith\x00guest-7\x00ansi/38400\x00" {
		t.Errorf("start %q", g)
	}
	// refused
	addr, _ = rloginServer(t, "nothing")
	d = &Door{Name: "rl", Remote: &Remote{Kind: KindRLogin, Address: addr}}
	ours, _ := net.Pipe()
	if _, err := d.Run(ours, info(), false, 0, t.Logf); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("refused: %v", err)
	}
}

// TestBBSLinkDoor: BBSLink's token and auth are asked with the hashed codes
// it wants before its telnet server is joined; a wrong code is refused.
func TestBBSLinkDoor(t *testing.T) {
	md5hex := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
	var key string
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token.php":
			key = r.URL.Query().Get("key")
			io.WriteString(w, "tok123\n")
		case "/auth.php":
			h := r.Header
			ok := r.URL.Query().Get("key") == key && len(key) == 6 && h.Get("X-Key") == key &&
				h.Get("X-Token") == "tok123" && h.Get("X-System") == "SYS" && h.Get("X-User") == "7" &&
				h.Get("X-Door") == "lord" && h.Get("X-Rows") == "24" &&
				h.Get("X-Auth") == md5hex("AUTH"+"tok123") && h.Get("X-Code") == md5hex("SCHEME"+"tok123")
			if ok {
				io.WriteString(w, "complete")
			} else {
				io.WriteString(w, "denied")
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer web.Close()
	addr := listen(t, func(c net.Conn) { play(t, c, "BBSLink: LORD\r\n") })
	r := &Remote{Kind: KindBBSLink, URL: web.URL, Address: addr, SysCode: "SYS", AuthCode: "AUTH",
		SchemeCode: "SCHEME", DoorCode: "lord"}
	visit(t, &Door{Name: "lord", Charset: CP437, Remote: r}, "BBSLink: LORD")

	bad := *r
	bad.AuthCode = "WRONG"
	ours, _ := net.Pipe()
	d := &Door{Name: "lord", Remote: &bad}
	if _, err := d.Run(ours, info(), false, 0, t.Logf); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("wrong code: %v", err)
	}
}

// sshTunnel is an SSH server that lets user/pass forward to anywhere:
// DoorParty's.
func sshTunnel(t *testing.T) (addr, fingerprint string) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(key)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == "dpuser" && string(pw) == "dppass" {
			return nil, nil
		}
		return nil, fmt.Errorf("no")
	}}
	cfg.AddHostKey(signer)
	addr = listen(t, func(c net.Conn) {
		_, chans, reqs, err := ssh.NewServerConn(c, cfg)
		if err != nil {
			return
		}
		go ssh.DiscardRequests(reqs)
		for nc := range chans {
			if nc.ChannelType() != "direct-tcpip" {
				nc.Reject(ssh.UnknownChannelType, "no")
				continue
			}
			var p struct {
				Host       string
				Port       uint32
				OriginHost string
				OriginPort uint32
			}
			ssh.Unmarshal(nc.ExtraData(), &p)
			far, err := net.Dial("tcp", net.JoinHostPort(p.Host, fmt.Sprint(p.Port)))
			if err != nil {
				nc.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			ch, rq, _ := nc.Accept()
			go ssh.DiscardRequests(rq)
			go func() { io.Copy(ch, far); ch.Close() }()
			go func() { io.Copy(far, ch); far.Close() }()
		}
	})
	return addr, ssh.FingerprintSHA256(signer.PublicKey())
}

// TestDoorPartyDoor: a DoorParty door is reached by RLogin through an SSH
// tunnel, the server user tagged with the BBS's tag; a wrong password or host
// key keeps it closed.
func TestDoorPartyDoor(t *testing.T) {
	sshAddr, fp := sshTunnel(t)
	rl, got := rloginServer(t, "\x00Alice Smith\x00[XB]Alice Smith\x00lord/38400\x00")
	r := &Remote{Kind: KindDoorParty, Address: sshAddr, SSHUser: "dpuser", SSHPassword: "dppass",
		HostKey: fp, Tag: "[XB]", DoorCode: "lord", Target: rl}
	visit(t, &Door{Name: "dp", Charset: CP437, Remote: r}, "rlogin door")
	if g := <-got; g != "\x00Alice Smith\x00[XB]Alice Smith\x00lord/38400\x00" {
		t.Errorf("start %q", g)
	}
	for _, bad := range []Remote{
		{Kind: KindDoorParty, Address: sshAddr, SSHUser: "dpuser", SSHPassword: "wrong", Target: rl},
		{Kind: KindDoorParty, Address: sshAddr, SSHUser: "dpuser", SSHPassword: "dppass", HostKey: "SHA256:other", Target: rl},
	} {
		ours, _ := net.Pipe()
		d := &Door{Name: "dp", Remote: &bad}
		if _, err := d.Run(ours, info(), false, 0, t.Logf); err == nil {
			t.Errorf("%+v let in", bad)
		}
	}
}

// optionConn is a connection that reads telnet options forever and
// fails every write.
type optionConn struct{ net.Conn }

func (optionConn) Read(p []byte) (int, error) {
	return copy(p, []byte{tIAC, tDO, 1}), nil // DO ECHO, and nothing else
}
func (optionConn) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// TestTelnetClientStops: a Read with no room returns at once, and one
// whose answers to options cannot be written ends with that error, not
// reading on forever.
func TestTelnetClientStops(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		c := newTelnetClient(optionConn{}, &Info{})
		if n, err := c.Read(nil); n != 0 || err != nil {
			done <- fmt.Errorf("empty Read: %d, %v", n, err)
			return
		}
		_, err := c.Read(make([]byte, 16))
		if err != io.ErrClosedPipe {
			done <- fmt.Errorf("Read: %v, want the write's error", err)
			return
		}
		if _, err := c.Write([]byte("x")); err != io.ErrClosedPipe {
			done <- fmt.Errorf("Write: %v", err)
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read never returned")
	}
}
