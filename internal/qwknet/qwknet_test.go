package qwknet

import (
	"bytes"
	"context"
	"log"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/ftp"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func newBBS(t *testing.T, name string) *bbs.BBS {
	t.Helper()
	store.BcryptCost = 4
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := bbs.Defaults()
	s.Name, s.FailDelay = name, 0
	return bbs.New(s, st, log.New(&bytes.Buffer{}, "", 0))
}

func user(t *testing.T, b *bbs.BBS, name string) *store.User {
	t.Helper()
	u, err := b.Store.CreateUser(name, name+"pass1", "", false)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func post(t *testing.T, b *bbs.BBS, u *store.User, a *store.Area, subject, body string, reply int64) int64 {
	t.Helper()
	id, err := b.Agent(u, "test").Post(a, "All", subject, []string{body}, reply)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// a hub serving FTP, and two nodes of it
func TestNetwork(t *testing.T) {
	hub := newBBS(t, "Hub BBS")
	gen, _ := hub.Store.CreateArea("General", "", false)
	hubUser := user(t, hub, "harry")
	for _, n := range []string{"nodea", "nodeb"} {
		u := user(t, hub, n)
		hub.Store.SetQWKNode(u.ID, true)
	}
	var err error
	if hub.Files, err = files.Open(hub.Store, filepath.Join(t.TempDir(), "files"), 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	srv, err := ftp.New(hub, ftp.Settings{Address: addr, TLS: ftp.TLSNo, MaxClients: 10, MaxPerIP: 10, IdleTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	for i := 0; ; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			break
		}
		if i > 100 {
			t.Fatal("the hub never listened")
		}
		time.Sleep(20 * time.Millisecond)
	}

	nodes := map[string]*bbs.BBS{}
	nets := map[string]*Net{}
	areas := map[string]*store.Area{}
	for _, n := range []string{"nodea", "nodeb"} {
		b := newBBS(t, n)
		areas[n], _ = b.Store.CreateArea("Net General", "", false)
		nodes[n] = b
		nets[n] = &Net{Name: "testnet", Address: addr, TLS: "no", User: n, Password: n + "pass1", Dir: "/qwk",
			Confs: map[int]string{int(gen.ID): "Net General"}}
	}
	carol := user(t, nodes["nodea"], "carol")
	dave := user(t, nodes["nodeb"], "dave")
	// already there before the first call: not sent
	post(t, nodes["nodea"], carol, areas["nodea"], "Old", "before joining", 0)

	call := func(n string, want string) {
		t.Helper()
		res, err := nets[n].Call(nodes[n])
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if res.String() != want {
			t.Errorf("%s: %s, want %s", n, res, want)
		}
	}
	call("nodea", "0 sent, 0 received, 0 passed over")
	post(t, nodes["nodea"], carol, areas["nodea"], "From A", "carol here", 0)
	post(t, hub, hubUser, gen, "From the hub", "harry here", 0)
	call("nodea", "1 sent, 1 received, 0 passed over")

	ms, _ := hub.Store.Messages(gen.ID, 0, 10)
	if len(ms) != 2 || ms[1].From != "carol" || ms[1].Origin != "qwk:nodea" {
		t.Fatalf("the hub has %+v", ms)
	}
	carolAtHub := ms[1].ID

	call("nodeb", "0 sent, 2 received, 0 passed over")
	got, _ := nodes["nodeb"].Store.Messages(areas["nodeb"].ID, 0, 10)
	if len(got) != 2 || got[1].From != "carol" || got[1].Origin != "qwk:testnet" || got[1].FromID != 0 {
		t.Fatalf("node B has %+v", got)
	}
	// dave answers carol: the hub threads it under her message
	post(t, nodes["nodeb"], dave, areas["nodeb"], "Re: From A", "dave here", got[1].ID)
	call("nodeb", "1 sent, 0 received, 0 passed over")
	ms, _ = hub.Store.Messages(gen.ID, carolAtHub, 10)
	if len(ms) != 1 || ms[0].From != "dave" || ms[0].ReplyTo != carolAtHub {
		t.Fatalf("dave at the hub: %+v", ms)
	}

	// A gets dave's, not carol's own back; then nothing new
	call("nodea", "0 sent, 1 received, 0 passed over")
	call("nodea", "0 sent, 0 received, 0 passed over")
	got, _ = nodes["nodea"].Store.Messages(areas["nodea"].ID, 0, 20)
	var froms []string
	for _, m := range got {
		froms = append(froms, m.From)
	}
	if strings.Join(froms, ",") != "carol,carol,harry,dave" {
		t.Errorf("node A has %v", froms)
	}

	// an area not here
	bad := &Net{Name: "x", Address: addr, User: "nodea", Password: "nodeapass1", Dir: "/qwk",
		Confs: map[int]string{9: "Nowhere"}}
	if _, err := bad.Call(nodes["nodea"]); err == nil || !strings.Contains(err.Error(), "no area") {
		t.Errorf("a missing area: %v", err)
	}
	bad = &Net{Name: "x", Address: addr, User: "nodea", Password: "wrong", Dir: "/qwk"}
	if _, err := bad.Call(nodes["nodea"]); err == nil {
		t.Error("a wrong password worked")
	}
}
