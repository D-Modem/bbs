package fido

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/archive"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
)

func nodelistText(hubHostPort string) string {
	return ";A test nodelist\r\n" +
		"Zone,21,Zone_21,Here,Zone_C,-Unpublished-,300,CM\r\n" +
		"Host,1,Net_1,Here,Host_C,-Unpublished-,300,CM\r\n" +
		",100,The_Hub,Wellington_NZ,Hub_Sysop,-Unpublished-,300,CM,IBN:" + hubHostPort + "\r\n" +
		",200,Node_200,Sydney_AU,Two_Hundred,-Unpublished-,300,CM\r\n" +
		",300,Node_300,Perth_AU,Three_Hundred,-Unpublished-,300,CM\r\n" +
		"Down,400,Gone_BBS,Nowhere,Gone_Sysop,-Unpublished-,300,CM\r\n"
}

// TestNodelistUse: the newest nodelist of a pattern read (in an archive
// too), read again when it changes; netmail checked against it; a link
// called where it says.
func TestNodelistUse(t *testing.T) {
	hub := newSystem(t, "21:1/100", &Link{Name: "n200", Addr: mustAddr("21:1/200"), Password: "pw", Echoes: "*"})
	_, port, _ := net.SplitHostPort(hub.host)
	dir := t.TempDir()
	// an old plain list, and a newer one in an ARC (as NODELIST.A83 comes)
	os.WriteFile(filepath.Join(dir, "NODELIST.276"), []byte(nodelistText("old.example")), 0600)
	var arc bytes.Buffer
	archive.Write(&arc, archive.ARC, "", []archive.File{{Name: "NODELIST.283", Data: []byte(nodelistText("127.0.0.1:" + port)), Modified: time.Now()}})
	newer := filepath.Join(dir, "NODELIST.A83")
	os.WriteFile(newer, arc.Bytes(), 0600)
	os.Chtimes(newer, time.Now(), time.Now().Add(time.Hour))

	link := &Link{Name: "hub", Addr: mustAddr("21:1/100"), Host: NodelistHost, Password: "pw", Echoes: "*", Route: true}
	n200 := newSystem(t, "21:1/200", link)
	n200.f.S.Nodelists = []string{filepath.Join(dir, "NODELIST.*")}
	n200.f.loadNodelists()
	nl := n200.f.Nodelist()
	if nl.Len() != 6 {
		t.Fatalf("nodelist: %d systems", nl.Len())
	}
	if h, err := n200.f.host(link); err != nil || h != "127.0.0.1:"+port {
		t.Fatalf("host %q, %v", h, err)
	}

	// netmail: to a listed node, by the route; not to one not listed, or down
	if desc, err := n200.f.NetmailTo(mustAddr("21:1/300")); err != nil || desc != "Node 300, Perth AU" {
		t.Errorf("1/300: %q, %v", desc, err)
	}
	if desc, err := n200.f.NetmailTo(mustAddr("21:1/300.5")); err != nil || desc != "Node 300, Perth AU" {
		t.Errorf("a point of 1/300: %q, %v", desc, err)
	}
	if _, err := n200.f.NetmailTo(mustAddr("21:1/999")); err == nil || !strings.Contains(err.Error(), "not in the nodelist") {
		t.Errorf("1/999: %v", err)
	}
	if _, err := n200.f.NetmailTo(mustAddr("21:1/400")); err == nil || !strings.Contains(err.Error(), "down") {
		t.Errorf("1/400: %v", err)
	}
	if _, err := n200.f.NetmailTo(mustAddr("1:2/3")); err != nil {
		t.Errorf("a zone the list lacks is not refused: %v", err)
	}
	if err := n200.node(t, "alice").SendMail("Gone Sysop@21:1/400", "Hi", []string{"x"}, 0); err == nil {
		t.Error("netmail to a down node sent")
	}

	// called where the nodelist says
	if err := n200.node(t, "alice").SendMail("Hub Sysop@21:1/100", "Hello hub", []string{"By the nodelist"}, 0); err != nil {
		t.Fatal(err)
	}
	n200.call(t, link, hub)
	sysop, _ := hub.f.B.Store.UserByName("sysop")
	if in, _ := hub.f.B.Store.Inbox(sysop.ID); len(in) != 1 || in[0].Subject != "Hello hub" {
		t.Fatalf("hub's inbox: %+v", in)
	}

	// a newer list is read again; an unchanged one is not
	before := strings.Count(n200.log.String(), "fido: nodelist:")
	n200.f.loadNodelists()
	if strings.Count(n200.log.String(), "fido: nodelist:") != before {
		t.Error("read again unchanged")
	}
	newest := filepath.Join(dir, "NODELIST.290")
	os.WriteFile(newest, []byte(nodelistText("127.0.0.1:"+port)+",500,New_Node,There,Five,-Unpublished-,300,CM\r\n"), 0600)
	os.Chtimes(newest, time.Now(), time.Now().Add(2*time.Hour))
	n200.f.loadNodelists()
	if _, ok := n200.f.Nodelist().Lookup(ftn.Addr{Zone: 21, Net: 1, Node: 500}); !ok {
		t.Error("the newer list not read")
	}
	if !strings.Contains(n200.log.String(), fmt.Sprintf("from %s", newest)) {
		t.Errorf("log: %s", n200.log)
	}
}

// TestNodelistArea: nodelist = area:NAME reads the newest file in a file
// area, as the NODELIST file echo brings it.
func TestNodelistArea(t *testing.T) {
	s := newSystem(t, "21:1/200")
	a, _ := s.f.B.Store.CreateFileArea("Nodelist", "Nodelists", true)
	s.f.S.Nodelists = []string{"area:Nodelist"}
	put := func(name, text string) {
		p := filepath.Join(t.TempDir(), name)
		os.WriteFile(p, []byte(text), 0600)
		if _, err := s.f.B.Files.Put(a, p, name, "", "", "21:1/100", "ftn:hub"); err != nil {
			t.Fatal(err)
		}
	}
	put("NODELIST.276", nodelistText("old.example"))
	s.f.loadNodelists()
	if n, ok := s.f.Nodelist().Lookup(mustAddr("21:1/100")); !ok || n.Name != "The Hub" {
		t.Fatalf("1/100: %+v", n)
	}
	put("NODELIST.283", strings.Replace(nodelistText("x"), "The_Hub", "New_Hub", 1))
	s.f.loadNodelists()
	if n, _ := s.f.Nodelist().Lookup(mustAddr("21:1/100")); n.Name != "New Hub" {
		t.Fatalf("not read again: %+v", n)
	}
}
