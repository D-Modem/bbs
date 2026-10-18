package menuauth

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// serve answers as modemmenu's [authd] would: alice/secret, then a
// refusal for "eve", and silence for "slow".
func serve(t *testing.T) string {
	sock := filepath.Join(t.TempDir(), "auth.sock")
	ln, err := net.Listen("unix", sock)
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
				line, _ := bufio.NewReader(c).ReadBytes('\n')
				var r request
				json.Unmarshal(line, &r)
				switch {
				case r.User == "slow":
					time.Sleep(time.Second)
				case r.User == "eve":
					c.Write([]byte(`{"ok":false,"error":"refused"}` + "\n"))
				case r.User == "alice" && r.Password == "secret":
					c.Write([]byte(`{"ok":true,"auth":"file"}` + "\n"))
				default:
					c.Write([]byte(`{"ok":false,"error":"denied"}` + "\n"))
				}
			}()
		}
	}()
	return sock
}

// TestCheck: the helper's yes (with how), its no, its refusal (an error),
// a helper too slow, and no helper at all.
func TestCheck(t *testing.T) {
	c := &Client{Socket: serve(t), Timeout: 300 * time.Millisecond}
	if ok, auth, err := c.Check("alice", "secret"); !ok || auth != "file" || err != nil {
		t.Errorf("alice: %v %q %v", ok, auth, err)
	}
	if ok, _, err := c.Check("alice", "wrong"); ok || err != nil {
		t.Errorf("wrong: %v %v", ok, err)
	}
	if _, _, err := c.Check("eve", "x"); err == nil || err.Error() != "refused" {
		t.Errorf("eve: %v", err)
	}
	if _, _, err := c.Check("slow", "x"); err == nil {
		t.Error("slow: no time out")
	}
	if _, _, err := (&Client{Socket: "/nonexistent/auth.sock"}).Check("alice", "secret"); err == nil {
		t.Error("no socket")
	}
}
