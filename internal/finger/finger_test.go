package finger

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func setup(t *testing.T, set Settings, addr string) (*bbs.BBS, string) {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b := bbs.New(bbs.Defaults(), st, log.New(&bytes.Buffer{}, "", 0))
	b.Name = "Test BBS"
	u, _ := st.CreateUser("alice", "password1", "Zürich", false)
	st.CreateUser("locked", "password1", "", false)
	l, _ := st.UserByName("locked")
	st.SetLocked(l.ID, true)
	n, _ := b.Enter(&bbs.Caller{Via: "telnet"}, nil)
	n.SetUser(u)
	n.Doing("Reading mail")
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go (&Server{B: b, S: set}).Serve(ctx, ln)
	return b, ln.Addr().String()
}

func ask(t *testing.T, addr, q string) string {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte(q + "\r\n"))
	b, _ := io.ReadAll(c)
	return string(b)
}

func TestFinger(t *testing.T) {
	_, addr := setup(t, Settings{Online: true, Users: true}, "127.0.0.1:0")
	got := ask(t, addr, "")
	if !strings.Contains(got, "Test BBS: 1 caller on") || !strings.Contains(got, "alice") ||
		!strings.Contains(got, "Reading mail") || !strings.Contains(got, "\r\n") {
		t.Errorf("online:\n%s", got)
	}
	got = ask(t, addr, "/W ALICE")
	for _, want := range []string{"User:         alice", "From:         Zürich", "on now, node 1: Reading mail"} {
		if !strings.Contains(got, want) {
			t.Errorf("user: no %q in\n%s", want, got)
		}
	}
	for q, want := range map[string]string{
		"nobody":                 "No such user here.",
		"locked":                 "No such user here.",
		"alice@elsewhere":        "Finger forwarding is not done here.",
		"@elsewhere":             "Finger forwarding is not done here.",
		strings.Repeat("x", 300): "",
	} {
		if got := ask(t, addr, q); !strings.Contains(got, want) || want == "" && got != "" {
			t.Errorf("%.20q: %q", q, got)
		}
	}
	_, addr = setup(t, Settings{}, "127.0.0.1:0")
	if got := ask(t, addr, ""); !strings.Contains(got, "is not told by finger") || strings.Contains(got, "alice") {
		t.Errorf("online off: %q", got)
	}
	if got := ask(t, addr, "alice"); strings.Contains(got, "Zürich") {
		t.Errorf("users off: %q", got)
	}
}

// TestFingerClient: the finger program (which asks port 79 only).
func TestFingerClient(t *testing.T) {
	if _, err := exec.LookPath("finger"); err != nil {
		t.Skip("no finger")
	}
	setup(t, Settings{Online: true, Users: true}, "127.0.0.1:79")
	out, err := exec.Command("finger", "alice@127.0.0.1").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "User:         alice") {
		t.Errorf("finger alice@: %v\n%s", err, out)
	}
	out, _ = exec.Command("finger", "@127.0.0.1").CombinedOutput()
	if !strings.Contains(string(out), "1 caller on") {
		t.Errorf("finger @:\n%s", out)
	}
}
