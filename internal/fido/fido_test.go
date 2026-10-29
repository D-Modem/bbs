package fido

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/archive"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/files"
	"github.com/D-Modem/bbs/internal/ftn"
	"github.com/D-Modem/bbs/internal/store"
)

// init makes bcrypt cheap, for the tests' users.
func init() { store.BcryptCost = 4 }

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

// Write adds p to the buffer.
func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

// String is what has been written.
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// system is a BBS on FidoNet, answering binkp, with users alice and
// sysop and an area General as the echo TEST.
type system struct {
	f    *Fido
	area *store.Area
	host string
	log  *syncBuf
}

// newSystem is a system at addr with links, answering binkp on a local port;
// its log is shown if the test fails.
func newSystem(t *testing.T, addr string, links ...*Link) *system {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.CreateUser("alice", "alicepass1", "", false)
	st.CreateUser("sysop", "sysoppass1", "", true)
	a, _ := st.CreateArea("General", "Talk", false)
	st.SetEchoTag(a.ID, "TEST")
	area, _ := st.Area(a.ID)
	lb := &syncBuf{}
	b := bbs.New(bbs.Defaults(), st, log.New(lb, "", 0))
	if b.Files, err = files.Open(st, filepath.Join(dir, "files"), 1<<20, 0); err != nil {
		t.Fatal(err)
	}
	ad, _ := ftn.ParseAddr(addr)
	f, err := New(b, Settings{Addrs: []ftn.Addr{ad}, System: "Test " + addr, Sysop: "Sysop",
		Origin: "Test system", Dir: filepath.Join(dir, "fido"), Links: links})
	if err != nil {
		t.Fatal(err)
	}
	b.Netmail = f.Netmail
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go (&Server{F: f, MaxClients: 4}).Serve(ctx, ln)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("%s log:\n%s", addr, lb)
		}
	})
	return &system{f: f, area: area, host: ln.Addr().String(), log: lb}
}

// node is a node on the system with user logged in, left at the test's end.
func (s *system) node(t *testing.T, user string) *bbs.Node {
	n, err := s.f.B.Enter(&bbs.Caller{Via: "test"}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.f.B.Store.UserByName(user)
	if err != nil {
		t.Fatal(err)
	}
	n.SetUser(u)
	t.Cleanup(n.Leave)
	return n
}

// call has s call a link, and waits for the answering side to be done
// with the session (it tosses after the caller hangs up).
func (s *system) call(t *testing.T, l *Link, other *system) {
	t.Helper()
	before := strings.Count(other.log.String(), "sent ")
	if err := s.f.Call(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500 && strings.Count(other.log.String(), "sent ") == before; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	other.f.lock(other.f.S.Links[0]).Lock()
	other.f.lock(other.f.S.Links[0]).Unlock()
	for _, ol := range other.f.S.Links {
		k := other.f.lock(ol)
		k.Lock()
		k.Unlock()
	}
}

// messages is the messages in the system's area General.
func (s *system) messages(t *testing.T) []*store.Message {
	ms, err := s.f.B.Store.Messages(s.area.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// TestEchomailAndNetmail: echomail goes from node to hub to node with its
// SEEN-BY and PATH, a reply comes back threaded and nothing goes twice; netmail
// reaches a user at the next system and is answered, netmail for elsewhere is
// dropped, and netmail with no route is refused.
func TestEchomailAndNetmail(t *testing.T) {
	// hub 21:1/100 with nodes 21:1/200 and 21:1/300; 200 calls the hub,
	// the hub calls 300
	hubTo200 := &Link{Name: "n200", Addr: mustAddr("21:1/200"), Password: "pw200", Echoes: "*", Packer: archive.ZIP}
	hubTo300 := &Link{Name: "n300", Addr: mustAddr("21:1/300"), Password: "pw300", Echoes: "*"}
	hub := newSystem(t, "21:1/100", hubTo200, hubTo300)
	n200 := newSystem(t, "21:1/200", &Link{Name: "hub", Addr: mustAddr("21:1/100"), Host: hub.host,
		Password: "pw200", Echoes: "*", Packer: archive.ZIP, Route: true})
	n300 := newSystem(t, "21:1/300", &Link{Name: "hub", Addr: mustAddr("21:1/100"), Password: "pw300", Echoes: "TEST"})
	hubTo300.Host = n300.host

	// first scans only mark where to start
	for _, s := range []*system{hub, n200, n300} {
		if _, err := s.f.ScanAll(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := n200.node(t, "alice").Post(n200.area, "All", "Hello FidoNet", []string{"First post — café"}, 0); err != nil {
		t.Fatal(err)
	}
	n200.call(t, n200.f.S.Links[0], hub)
	hm := hub.messages(t)
	if len(hm) != 1 || hm[0].Subject != "Hello FidoNet" || hm[0].From != "alice" || hm[0].Origin != "ftn:n200" {
		t.Fatalf("hub has %+v", hm)
	}
	if !strings.Contains(hm[0].Body, "First post - café") || !strings.Contains(hm[0].Body, "* Origin: Test system (21:1/200)") {
		t.Fatalf("body %q", hm[0].Body)
	}
	if !strings.HasPrefix(hm[0].FTNMsgID, "21:1/200 ") {
		t.Fatalf("msgid %q", hm[0].FTNMsgID)
	}
	// on to 300, with SEEN-BY of all three and the PATH 200 100
	hub.call(t, hubTo300, n300)
	m3 := n300.messages(t)
	if len(m3) != 1 || m3[0].FTNMsgID != hm[0].FTNMsgID {
		t.Fatalf("300 has %+v", m3)
	}
	raw, _ := n300.f.B.Store.FTN(m3[0].ID)
	em, err := ftn.ParseMessage(raw, len(raw)+1)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ftn.FormatNet2D("", em.SeenBy, 70), ""); got != "1/100 200 300" {
		t.Errorf("SEEN-BY %q", got)
	}
	if got := strings.Join(ftn.FormatNet2D("", em.Path, 70), ""); got != "1/200 100" {
		t.Errorf("PATH %q", got)
	}

	// a reply from 300 goes back by the hub to 200, threaded
	if _, err := n300.node(t, "sysop").Post(n300.area, "alice", "Re: Hello FidoNet", []string{"Welcome!"}, m3[0].ID); err != nil {
		t.Fatal(err)
	}
	hub.call(t, hubTo300, n300)
	n200.call(t, n200.f.S.Links[0], hub)
	m2 := n200.messages(t)
	if len(m2) != 2 || m2[1].Subject != "Re: Hello FidoNet" || m2[1].ReplyTo != m2[0].ID || m2[1].To != "alice" {
		t.Fatalf("200 has %+v", m2)
	}
	// and not again: 300 has it, the hub has it
	if n, _ := hub.f.Scan(hubTo300); n != 0 {
		t.Errorf("the hub packed %d for 300 again", n)
	}

	// netmail: alice at 200 to the sysop at 300, routed by the hub?  No:
	// we route nothing, so 200 sends to 21:1/100's sysop
	if err := n200.node(t, "alice").SendMail("sysop@21:1/100", "Private", []string{"Just for you"}, 0); err != nil {
		t.Fatal(err)
	}
	if err := n200.node(t, "alice").SendMail("Bob@99:1/1", "Far", []string{"x"}, 0); err != nil {
		t.Fatal(err) // by the route
	}
	n200.call(t, n200.f.S.Links[0], hub)
	sysop, _ := hub.f.B.Store.UserByName("sysop")
	in, _ := hub.f.B.Store.Inbox(sysop.ID)
	if len(in) != 1 || in[0].From != "alice@21:1/200" || in[0].Subject != "Private" || !strings.Contains(in[0].Body, "Just for you") {
		t.Fatalf("hub sysop's inbox %+v", in)
	}
	if !strings.Contains(hub.log.String(), "netmail for 99:1/1, not us: dropped") {
		t.Error("netmail for elsewhere not dropped")
	}
	// a reply, as the inbox does it
	if err := hub.node(t, "sysop").SendMail(in[0].From, "Re: Private", []string{"Got it"}, in[0].ID); err != nil {
		t.Fatal(err)
	}
	n200.call(t, n200.f.S.Links[0], hub)
	alice, _ := n200.f.B.Store.UserByName("alice")
	in, _ = n200.f.B.Store.Inbox(alice.ID)
	if len(in) != 1 || in[0].From != "sysop@21:1/100" || in[0].Subject != "Re: Private" {
		t.Fatalf("alice's inbox %+v", in)
	}
	if err := n300.node(t, "alice").SendMail("x@1:2/3", "Nowhere", []string{"x"}, 0); err == nil || !strings.Contains(err.Error(), "no route") {
		t.Errorf("no route: %v", err)
	}
}

// mustAddr is an address known to be good (a panic if not).
func mustAddr(s string) ftn.Addr {
	a, err := ftn.ParseAddr(s)
	if err != nil {
		panic(err)
	}
	return a
}

// TestRefusals: a call with a wrong password, from an address not a link, or to
// a system that is not the link called is refused; the right one is not.
func TestRefusals(t *testing.T) {
	hub := newSystem(t, "21:1/100", &Link{Name: "n200", Addr: mustAddr("21:1/200"), Password: "right", Echoes: "*"})
	ctx := context.Background()
	call := func(addr, pw string, plain bool) error {
		s := newSystem(t, addr, &Link{Name: "hub", Addr: mustAddr("21:1/100"), Host: hub.host,
			Password: pw, PlainOK: plain, Echoes: "*"})
		return s.f.Call(ctx, s.f.S.Links[0])
	}
	if err := call("21:1/200", "wrong", false); err == nil || !strings.Contains(err.Error(), "Bad password") {
		t.Errorf("wrong password: %v", err)
	}
	if err := call("21:1/999", "right", false); err == nil || !strings.Contains(err.Error(), "Unknown address") {
		t.Errorf("unknown address: %v", err)
	}
	if err := call("21:1/200", "right", false); err != nil {
		t.Errorf("right password: %v", err)
	}
	// it says it is someone else
	s := newSystem(t, "21:1/200", &Link{Name: "hub", Addr: mustAddr("21:1/101"), Host: hub.host, Password: "right"})
	if err := s.f.Call(ctx, s.f.S.Links[0]); err == nil || !strings.Contains(err.Error(), "not 21:1/101") {
		t.Errorf("impostor: %v", err)
	}
}

// TestTossChecks: packets from the wrong address, to the wrong one or with the
// wrong password, echoes not exchanged, dupes and messages already through us,
// a bundle that is not one and a bad TIC are not tossed (the bad files kept in
// bad); escapes are cleaned; a file waits for its TIC.
func TestTossChecks(t *testing.T) {
	l := &Link{Name: "hub", Addr: mustAddr("21:1/100"), Password: "x", Echoes: "TEST,OTHER", PktPassword: "PKT"}
	s := newSystem(t, "21:1/200", l)
	pkt := func(from, to, pw string, msgs ...*ftn.Message) {
		var b bytes.Buffer
		ftn.WriteHeader(&b, &ftn.Header{Orig: mustAddr(from), Dest: mustAddr(to), Password: pw})
		for _, m := range msgs {
			ftn.WriteMessage(&b, m)
		}
		ftn.WriteEnd(&b)
		os.WriteFile(filepath.Join(s.f.inbound(l), ftn.NewMSGID(mustAddr("1:1/1"))[6:]+".pkt"), b.Bytes(), 0600)
	}
	echo := func(id string, path ...ftn.Net2D) *ftn.Message {
		return &ftn.Message{OrigNode: 100, OrigNet: 1, DestNode: 200, DestNet: 1, DateTime: ftn.FormatDate(time.Now()),
			From: "Bob", To: "All", Subject: "S " + id, Area: "TEST",
			Kludges: []ftn.Kludge{{Name: "MSGID", Value: id}}, Lines: []string{"Text", "\x1b[31mred"},
			SeenBy: []ftn.Net2D{{Net: 1, Node: 100}}, Path: path}
	}
	pkt("21:1/100", "21:1/200", "PKT", echo("21:1/100 00000001"), echo("21:1/100 00000001"),
		echo("21:1/100 00000002", ftn.Net2D{Net: 1, Node: 200}),
		&ftn.Message{Area: "UNKNOWN", Kludges: []ftn.Kludge{{Name: "MSGID", Value: "21:1/100 3"}}})
	pkt("21:1/100", "21:1/200", "WRONG", echo("21:1/100 00000004"))
	pkt("21:1/101", "21:1/200", "PKT", echo("21:1/100 00000005"))
	pkt("21:1/100", "21:1/201", "PKT", echo("21:1/100 00000006"))
	os.WriteFile(filepath.Join(s.f.inbound(l), "abcd0001.mo0"), []byte("not a zip"), 0600)
	os.WriteFile(filepath.Join(s.f.inbound(l), "file.tic"), []byte("tic"), 0600)
	// a file echo's file, and file echoes are not exchanged with the link
	os.WriteFile(filepath.Join(s.f.inbound(l), "good.tic"), (&ftn.TIC{Area: "X", File: "X.ZIP"}).Bytes(), 0600)
	os.WriteFile(filepath.Join(s.f.inbound(l), "X.ZIP"), []byte("x"), 0600)
	// a file with no TIC, not yet a day old: left to wait for one
	os.WriteFile(filepath.Join(s.f.inbound(l), "LONELY.ZIP"), []byte("x"), 0600)
	st, err := s.f.Toss(l)
	if err != nil {
		t.Fatal(err)
	}
	if st.Echomail != 1 || st.Dupes != 2 || st.Bad != 6 {
		t.Errorf("stats %+v", st)
	}
	ms := s.messages(t)
	if len(ms) != 1 || strings.Contains(ms[0].Body, "\x1b") {
		t.Fatalf("%+v", ms)
	}
	bad, _ := os.ReadDir(filepath.Join(s.f.S.Dir, "bad"))
	if len(bad) != 5 || !exists(filepath.Join(s.f.S.Dir, "bad", "hub-file.tic")) {
		t.Errorf("%d in bad", len(bad))
	}
	for _, p := range []string{"files/good.tic", "files/X.ZIP", "LONELY.ZIP"} {
		if !exists(filepath.Join(s.f.inbound(l), p)) {
			t.Errorf("no %s", p)
		}
	}
}

// TestGateway: an area that is an echo and a newsgroup both.  Echomail
// becomes articles; articles (from a news peer) go out as echomail.
func TestGateway(t *testing.T) {
	l := &Link{Name: "hub", Addr: mustAddr("21:1/100"), Password: "x", Echoes: "*"}
	s := newSystem(t, "21:1/200", l)
	st := s.f.B.Store
	st.SetNewsgroup(s.area.ID, "fido.test")
	s.f.B.News = bbs.NewsSettings{PathHost: "news.example", Domain: "news.example"}
	s.f.ScanAll()

	var b bytes.Buffer
	ftn.WriteHeader(&b, &ftn.Header{Orig: l.Addr, Dest: mustAddr("21:1/200")})
	ftn.WriteMessage(&b, &ftn.Message{OrigNode: 100, OrigNet: 1, DestNode: 200, DestNet: 1,
		DateTime: ftn.FormatDate(time.Now()), From: "Bob Jones", To: "All", Subject: "Gated",
		Area: "TEST", Kludges: []ftn.Kludge{{Name: "MSGID", Value: "21:1/100 0000abcd"}},
		Lines: []string{"Through the gate.", "--- x", " * Origin: Bob's place (21:1/100.7)"}})
	ftn.WriteEnd(&b)
	os.WriteFile(filepath.Join(s.f.inbound(l), "a.pkt"), b.Bytes(), 0600)
	if _, err := s.f.Toss(l); err != nil {
		t.Fatal(err)
	}
	ms := s.messages(t)
	if len(ms) != 1 || ms[0].MessageID != "<MSGID_21=3A1=2F100_0000abcd@fidonet.org>" {
		t.Fatalf("%+v", ms)
	}
	raw, _ := st.Raw(ms[0].ID)
	for _, want := range []string{"From: \"Bob Jones\" <Bob_Jones@p7.f100.n1.z21.fidonet.org>",
		"Newsgroups: fido.test", "Message-ID: <MSGID_21=3A1=2F100_0000abcd@fidonet.org>",
		"X-FTN-MSGID: 21:1/100 0000abcd", "Through the gate."} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("the article lacks %q:\n%s", want, raw)
		}
	}

	// an article from a news peer, answering it: out as echomail, its
	// MSGID of its Message-ID, its REPLY the MSGID it answers
	if _, err := st.Post(&store.Message{AreaID: s.area.ID, From: "Carol", To: "Bob Jones", Subject: "Re: Gated",
		Body: "From Usenet.", MessageID: "<abc@news.peer>", Origin: "peer", ReplyTo: ms[0].ID}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.f.Scan(l); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	ents, _ := os.ReadDir(s.f.outbound(l))
	var pkt []byte
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".pkt") {
			pkt, _ = os.ReadFile(filepath.Join(s.f.outbound(l), e.Name()))
		}
	}
	r, _, err := ftn.NewReader(bytes.NewReader(pkt), 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	em, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if em.From != "Carol" || em.Kludge("REPLY") != "21:1/100 0000abcd" ||
		em.Kludge("MSGID") != ftn.MSGIDOf("<abc@news.peer>", mustAddr("21:1/200")) {
		t.Fatalf("%+v", em)
	}
}

// TestBundles: echomail in ARC, ARJ and LHA bundles (stored, as written
// here) is tossed like ZIP's; the bundle is named as ARCmail is,
// whatever is in it.
func TestBundles(t *testing.T) {
	for _, k := range []archive.Kind{archive.ARC, archive.ARJ, archive.LHA} {
		t.Run(string(k), func(t *testing.T) {
			hub := newSystem(t, "21:1/100", &Link{Name: "n200", Addr: mustAddr("21:1/200"), Password: "pw", Echoes: "*"})
			n200 := newSystem(t, "21:1/200", &Link{Name: "hub", Addr: mustAddr("21:1/100"), Host: hub.host,
				Password: "pw", Echoes: "*", Packer: k})
			n200.f.ScanAll()
			if _, err := n200.node(t, "alice").Post(n200.area, "All", "In "+string(k), []string{"Packed"}, 0); err != nil {
				t.Fatal(err)
			}
			if n, err := n200.f.Scan(n200.f.S.Links[0]); err != nil || n != 1 {
				t.Fatalf("scan: %d, %v", n, err)
			}
			var bundles []string
			ents, _ := os.ReadDir(n200.f.outbound(n200.f.S.Links[0]))
			for _, e := range ents {
				if !strings.HasPrefix(e.Name(), ".") {
					bundles = append(bundles, e.Name())
				}
			}
			if len(bundles) != 1 || !bundleName.MatchString(bundles[0]) {
				t.Fatalf("outbound: %v", bundles)
			}
			b, _ := os.ReadFile(filepath.Join(n200.f.outbound(n200.f.S.Links[0]), bundles[0]))
			if archive.Detect(b) != k {
				t.Fatalf("the bundle is %q", archive.Detect(b))
			}
			n200.call(t, n200.f.S.Links[0], hub)
			if hm := hub.messages(t); len(hm) != 1 || hm[0].Subject != "In "+string(k) {
				t.Fatalf("hub has %+v", hm)
			}
		})
	}
}

// TestRealBundles: bundles packed by the real programs (arc crunching,
// ARJ's methods 1 and 4, LHA's -lh5-), where they are installed.
func TestRealBundles(t *testing.T) {
	for _, c := range []struct {
		prog string
		args []string // then the bundle and the packet
	}{
		{"arc", []string{"a"}},
		{"arj", []string{"a", "-m1", "-y"}},
		{"arj", []string{"a", "-m4", "-y"}},
		{"jlha", []string{"ao5q"}},
	} {
		t.Run(c.prog+strings.Join(c.args, ""), func(t *testing.T) {
			if _, err := exec.LookPath(c.prog); err != nil {
				t.Skipf("no %s", c.prog)
			}
			hub := newSystem(t, "21:1/100", &Link{Name: "n200", Addr: mustAddr("21:1/200"), Password: "pw", Echoes: "*"})
			n200 := newSystem(t, "21:1/200", &Link{Name: "hub", Addr: mustAddr("21:1/100"), Password: "pw", Echoes: "*"})
			n200.f.ScanAll()
			for i := 0; i < 20; i++ { // enough text to be worth packing
				n200.node(t, "alice").Post(n200.area, "All", fmt.Sprintf("Message %d", i), []string{strings.Repeat("Hello, hub. ", 40)}, 0)
			}
			if n, err := n200.f.Scan(n200.f.S.Links[0]); err != nil || n != 20 {
				t.Fatalf("scan: %d, %v", n, err)
			}
			out := n200.f.outbound(n200.f.S.Links[0])
			pkts, _ := filepath.Glob(filepath.Join(out, "*.pkt"))
			if len(pkts) != 1 {
				t.Fatalf("packets: %v", pkts)
			}
			bundle := filepath.Join(hub.f.inbound(hub.f.S.Links[0]), "0000abcd.mo1")
			tmp := filepath.Join(t.TempDir(), "b."+c.prog)
			cmd := exec.Command(c.prog, append(c.args, tmp, filepath.Base(pkts[0]))...)
			cmd.Dir = out
			cmd.Env = append(os.Environ(), "JAVA_TOOL_OPTIONS=")
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v\n%s", c.prog, err, b)
			}
			if _, err := os.Stat(tmp); err != nil { // arj adds .arj, jlha may add .lzh
				m, _ := filepath.Glob(tmp + ".*")
				if len(m) != 1 {
					t.Fatalf("no bundle made")
				}
				tmp = m[0]
			}
			b, _ := os.ReadFile(tmp)
			os.WriteFile(bundle, b, 0600)
			st, err := hub.f.Toss(hub.f.S.Links[0])
			if err != nil || st.Echomail != 20 {
				t.Fatalf("tossed %v, %v (%s bundle)", st, err, archive.Detect(b))
			}
		})
	}
}
