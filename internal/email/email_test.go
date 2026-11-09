package email

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

func init() { store.BcryptCost = 4 }

// fakeDNS is DNS for the tests.
type fakeDNS struct {
	mu  sync.Mutex
	txt map[string][]string
	mx  map[string][]*net.MX
	bl  map[string]string // DNSBL query names listed: their address
}

func (d *fakeDNS) LookupTXT(ctx context.Context, name string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if v, ok := d.txt[strings.TrimSuffix(name, ".")]; ok {
		return v, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (d *fakeDNS) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if v, ok := d.mx[strings.TrimSuffix(name, ".")]; ok {
		return v, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (d *fakeDNS) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if a, ok := d.bl[host]; ok {
		return []net.IPAddr{{IP: net.ParseIP(a)}}, nil
	}
	if strings.HasSuffix(host, ".bl.test") {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
}

func (d *fakeDNS) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	return nil, &net.DNSError{Err: "no such host", Name: addr, IsNotFound: true}
}

// site is a BBS with e-mail, its servers on local ports.
type site struct {
	b     *bbs.BBS
	svc   *Service
	st    *store.Store
	log   *syncWriter
	addrs map[string]string
	users map[string]*store.User
}

// newSite makes a BBS for domain with users, and the servers of kinds
// asked for (pop3, imap, smtp, mx), without TLS.
func newSite(t *testing.T, domain string, dns *fakeDNS, users []string, kinds ...string) *site {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := &site{st: st, log: &syncWriter{w: &bytes.Buffer{}}, addrs: map[string]string{}, users: map[string]*store.User{}}
	for _, name := range users {
		u, err := st.CreateUser(name, "password1", "", name == "sysop")
		if err != nil {
			t.Fatal(err)
		}
		s.users[name] = u
	}
	set := bbs.Defaults()
	set.FailDelay = 0
	s.b = bbs.New(set, st, log.New(s.log, domain+": ", 0))
	ms := Defaults(domain, "mail."+domain)
	ms.Resolver = dns
	s.svc = New(s.b, ms)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, k := range kinds {
		set := ServerSettings{Address: "127.0.0.1:0", TLS: TLSNo, MaxClients: 10, MaxPerIP: 10, IdleTimeout: time.Minute}
		if k == MX {
			set.DNSBL = []string{"bl.test"}
		}
		var run func(context.Context) error
		var addr net.Addr
		switch k {
		case "pop3":
			x, err := s.svc.NewPOP3(set)
			if err != nil {
				t.Fatal(err)
			}
			run, addr = x.Run, x.Addr()
		case "imap":
			x, err := s.svc.NewIMAP(set)
			if err != nil {
				t.Fatal(err)
			}
			run, addr = x.Run, x.Addr()
		default:
			x, err := s.svc.NewSMTP(k, set, k == MX)
			if err != nil {
				t.Fatal(err)
			}
			run, addr = x.Run, x.Addr()
		}
		s.addrs[k] = addr.String()
		wg.Add(1)
		go func() { defer wg.Done(); run(ctx) }()
	}
	t.Cleanup(func() {
		cancel()
		wg.Wait()
		if t.Failed() {
			t.Logf("%s log:\n%s", domain, s.log.String())
		}
	})
	return s
}

type syncWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

// String is what has been written, read under the same lock.
func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.String()
}

// submit sends a mail by SMTP submission, as name.
func submit(t *testing.T, addr, name, from string, to []string, msg string) error {
	t.Helper()
	host, _, _ := net.SplitHostPort(addr)
	return smtp.SendMail(addr, smtp.PlainAuth("", name, "password1", host), from, to, []byte(msg))
}

// inbox is a user's inbox.
func inbox(t *testing.T, s *site, name string, folder int) []*store.Message {
	t.Helper()
	id := s.users[name].ID
	l, err := s.st.Folder(id, folder)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range l {
		full, _, err := s.st.BoxMail(id, m.Box)
		if err != nil {
			t.Fatal(err)
		}
		l[i].Raw = full.Raw
	}
	return l
}

// TestAddresses: users' addresses, those here and FidoNet's read.
func TestAddresses(t *testing.T) {
	dns := &fakeDNS{}
	s := newSite(t, "bbs.test", dns, []string{"Alice Smith", "sysop", "Bob"})
	if a := s.svc.Address(s.users["Alice Smith"]); a != "alice.smith@bbs.test" {
		t.Errorf("address %q", a)
	}
	for addr, want := range map[string]string{"alice.smith@bbs.test": "Alice Smith", "ALICE.SMITH+lists@BBS.TEST": "Alice Smith",
		"postmaster@bbs.test": "sysop", "abuse@mail.bbs.test": "sysop", "bob@bbs.test": "Bob"} {
		r, err := s.svc.resolve(addr)
		if err != nil || r.kind != toLocal || r.user.Name != want {
			t.Errorf("%s: %+v %v", addr, r, err)
		}
	}
	if _, err := s.svc.resolve("carol@bbs.test"); !errors.Is(err, ErrNoUser) {
		t.Errorf("no such user: %v", err)
	}
	r, err := s.svc.resolve("john.doe@p4.f100.n1.z21.fidonet.org")
	if err != nil || r.kind != toFido || r.name != "John Doe" || r.ftn.String() != "21:1/100.4" {
		t.Errorf("fidonet: %+v %v", r, err)
	}
	if r, _ := s.svc.resolve("x@example.com"); r.kind != toInternet {
		t.Errorf("internet: %+v", r)
	}
	if !bbs.IsEmailAddress("x@example.com") || !bbs.IsEmailAddress(`"Jo, Smith" <jo@example.com>`) ||
		bbs.IsEmailAddress("Bob@1:2/3") || bbs.IsEmailAddress("Bob") || bbs.IsEmailAddress("x@localhost") {
		t.Error("IsEmailAddress")
	}
	if got := shownAddress("Jo, Smith", "jo@example.com"); !bbs.IsEmailAddress(got) {
		t.Errorf("shown %q is not an address again", got)
	}
}

// TestParse: a mail's text for the terminals, from plain text, HTML, and
// with attachments listed.
func TestParse(t *testing.T) {
	raw := "From: \"Jo\" <jo@example.com>\r\nTo: bob@bbs.test\r\nSubject: =?utf-8?q?Caf=C3=A9?=\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Hello <b>there</b></p><p>bye &amp; all</p>\r\n" +
		"--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=a.pdf\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\nJVBERi0=\r\n--b--\r\n"
	p, err := parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if p.subject != "Café" || p.fromAddr != "jo@example.com" || p.fromName != "Jo" {
		t.Errorf("header: %+v", p)
	}
	if !strings.Contains(p.text, "Hello there\nbye & all") || !strings.Contains(p.text, "a.pdf (5 bytes)") {
		t.Errorf("text: %q", p.text)
	}
}

// TestRender: mail written here, as a mail program gets it.
func TestRender(t *testing.T) {
	s := newSite(t, "bbs.test", &fakeDNS{}, []string{"alice", "sysop"})
	m := &store.Message{ID: 7, FromID: s.users["alice"].ID, From: "alice", ToID: s.users["sysop"].ID, To: "sysop",
		Subject: "Grüße", Body: "line one\n.dot line", Created: time.Unix(1e9, 0), ReplyTo: 3}
	raw := string(s.svc.Render(m))
	for _, want := range []string{"From: \"alice\" <alice@bbs.test>\r\n", "To: \"sysop\" <sysop@bbs.test>\r\n",
		"Subject: =?utf-8?q?Gr=C3=BC=C3=9Fe?=\r\n", "Message-ID: <mail-7@mail.bbs.test>\r\n",
		"In-Reply-To: <mail-3@mail.bbs.test>\r\n", "\r\n\r\nline one\r\n.dot line\r\n"} {
		if !strings.Contains(raw, want) {
			t.Errorf("no %q in\n%s", want, raw)
		}
	}
	if string(s.svc.Render(m)) != raw {
		t.Error("not the same twice")
	}
	fido := &store.Message{From: "Hub Sysop@21:1/100", Subject: "s", Body: "b"}
	if a := s.svc.fromAddress(fido); a.Address != "hub.sysop@f100.n1.z21.fidonet" {
		t.Errorf("fido from %v", a)
	}
}

// TestMail: mail sent by SMTP submission to a user here and to another
// BBS, which takes it from the queue by SMTP with its SPF, DKIM and DMARC
// holding; mail refused for elsewhere, forged, or for no one; a bounce;
// then the other BBS's user reads it by POP3 and IMAP.
func TestMail(t *testing.T) {
	dns := &fakeDNS{txt: map[string][]string{}, mx: map[string][]*net.MX{}}
	a := newSite(t, "a.test", dns, []string{"alice", "sysop"}, Submission)
	b := newSite(t, "b.test", dns, []string{"bob", "sysop"}, MX, "pop3", "imap")

	// a.test signs; its records say so, and that it sends from here
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	a.svc.Set.DKIM = &DKIM{Domain: "a.test", Selector: "bbs", Signer: key}
	rec, _ := DKIMRecord(key)
	dns.txt["bbs._domainkey.a.test"] = []string{rec}
	dns.txt["a.test"] = []string{"v=spf1 ip4:127.0.0.1 -all"}
	dns.txt["_dmarc.a.test"] = []string{"v=DMARC1; p=reject"}
	_, port, _ := net.SplitHostPort(b.addrs[MX])
	dns.mx["b.test"] = []*net.MX{{Host: "127.0.0.1", Pref: 10}}
	a.svc.Set.MXPort = port

	sub := a.addrs[Submission]
	msg := "From: alice <alice@a.test>\r\nTo: bob@b.test, sysop@a.test\r\nBcc: secret@b.test\r\nSubject: Hello\r\n\r\nHi Bob.\r\n"
	if err := submit(t, sub, "alice", "alice@a.test", []string{"bob@b.test", "sysop@a.test"}, msg); err != nil {
		t.Fatal(err)
	}
	if in := inbox(t, a, "sysop", store.FolderInbox); len(in) != 1 || in[0].Subject != "Hello" || in[0].FromID != a.users["alice"].ID {
		t.Fatalf("sysop's inbox: %+v", in)
	}
	sent := inbox(t, a, "alice", store.FolderSent)
	if len(sent) != 1 || strings.Contains(string(sent[0].Raw), "secret") || !strings.Contains(string(sent[0].Raw), "DKIM-Signature:") {
		t.Fatalf("alice's sent: %+v", sent)
	}

	// refused: another's address, a user not there, mail before a login
	if err := submit(t, sub, "alice", "sysop@a.test", []string{"bob@b.test"}, msg); err == nil {
		t.Error("sent as another")
	}
	if err := submit(t, sub, "alice", "alice@a.test", []string{"nobody@a.test"}, msg); err == nil {
		t.Error("sent to no one")
	}
	if c, err := smtp.Dial(sub); err == nil {
		if err := c.Mail("alice@a.test"); err == nil || !strings.Contains(err.Error(), "530") {
			t.Errorf("mail without a login: %v", err)
		}
		c.Close()
	}

	// the queue: to b.test, where it is checked
	if q, _ := a.st.MailQueue(time.Time{}, 0); len(q) != 1 || q[0].Rcpt != "bob@b.test" {
		t.Fatalf("queue: %+v", q)
	}
	a.svc.RunQueueOnce(context.Background())
	if q, _ := a.st.MailQueue(time.Time{}, 0); len(q) != 0 {
		t.Fatalf("queue after: %+v", q)
	}
	got := inbox(t, b, "bob", store.FolderInbox)
	if len(got) != 1 {
		t.Fatalf("bob's inbox: %+v", got)
	}
	raw := string(got[0].Raw)
	for _, want := range []string{"spf=pass", "dkim=pass", "dmarc=pass", "Received: from mail.a.test", "Hi Bob."} {
		if !strings.Contains(raw, want) {
			t.Errorf("no %q in\n%s", want, raw)
		}
	}
	if got[0].From != "alice <alice@a.test>" || got[0].FromID != 0 || !bbs.IsEmailAddress(got[0].From) {
		t.Errorf("from %q", got[0].From)
	}

	// b.test's MX: no relaying, no one there, and a forgery refused
	mx := b.addrs[MX]
	err = smtp.SendMail(mx, nil, "x@c.test", []string{"y@elsewhere.test"}, []byte("Subject: x\r\n\r\nx\r\n"))
	if err == nil || !strings.Contains(err.Error(), "550") {
		t.Errorf("relayed: %v", err)
	}
	forged := "From: alice@a.test\r\nTo: bob@b.test\r\nSubject: Forged\r\n\r\nGive me money\r\n"
	err = smtp.SendMail(mx, nil, "x@c.test", []string{"bob@b.test"}, []byte(forged))
	if err == nil || !strings.Contains(err.Error(), "DMARC") {
		t.Errorf("forged: %v", err)
	}
	if n := len(inbox(t, b, "bob", store.FolderInbox)); n != 1 {
		t.Errorf("bob has %d", n)
	}

	// a bounce, for no one at b.test
	if err := submit(t, sub, "alice", "alice@a.test", []string{"nobody@b.test"}, "Subject: Lost\r\n\r\nx\r\n"); err != nil {
		t.Fatal(err)
	}
	a.svc.RunQueueOnce(context.Background())
	if in := inbox(t, a, "alice", store.FolderInbox); len(in) != 1 || in[0].Subject != "Undelivered: Lost" ||
		!strings.Contains(in[0].Body, "nobody@b.test") {
		t.Errorf("bounce: %+v", in)
	}

	// from a terminal
	n, _ := a.b.Enter(&bbs.Caller{Via: "test"}, nil)
	n.SetUser(a.users["alice"])
	if err := n.SendMail("bob@b.test", "From the BBS", []string{"typed on a terminal"}, 0); err != nil {
		t.Fatal(err)
	}
	a.svc.RunQueueOnce(context.Background())
	if in := inbox(t, b, "bob", store.FolderInbox); len(in) != 2 || in[1].Subject != "From the BBS" {
		t.Fatalf("bob's inbox: %+v", in)
	}

	testPOP3(t, b)
	testIMAP(t, b)
}

// pop is a POP3 command's reply.
func pop(t *testing.T, r *bufio.Reader, c net.Conn, cmd string, multi bool) string {
	t.Helper()
	if cmd != "" {
		fmt.Fprintf(c, "%s\r\n", cmd)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !multi || !strings.HasPrefix(line, "+OK") {
		return line
	}
	var b strings.Builder
	b.WriteString(line)
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if l == ".\r\n" {
			return b.String()
		}
		b.WriteString(l)
	}
}

// testPOP3: bob's inbox by POP3: listed, read, one deleted (to the trash).
func testPOP3(t *testing.T, b *site) {
	c, err := net.Dial("tcp", b.addrs["pop3"])
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := bufio.NewReader(c)
	pop(t, r, c, "", false)
	if got := pop(t, r, c, "PASS x", false); !strings.HasPrefix(got, "-ERR") {
		t.Errorf("PASS first: %q", got)
	}
	pop(t, r, c, "USER bob", false)
	if got := pop(t, r, c, "PASS wrong", false); !strings.HasPrefix(got, "-ERR") {
		t.Errorf("a wrong password: %q", got)
	}
	pop(t, r, c, "USER bob@b.test", false)
	if got := pop(t, r, c, "PASS password1", false); !strings.HasPrefix(got, "+OK") {
		t.Fatalf("login: %q", got)
	}
	if got := pop(t, r, c, "STAT", false); !strings.HasPrefix(got, "+OK 2 ") {
		t.Errorf("STAT %q", got)
	}
	if got := pop(t, r, c, "UIDL", true); strings.Count(got, "\r\n") != 3 {
		t.Errorf("UIDL %q", got)
	}
	if got := pop(t, r, c, "RETR 1", true); !strings.Contains(got, "Subject: Hello") || !strings.Contains(got, "Hi Bob.") {
		t.Errorf("RETR %q", got)
	}
	if got := pop(t, r, c, "TOP 2 0", true); !strings.Contains(got, "Subject: From the BBS") || strings.Contains(got, "typed") {
		t.Errorf("TOP %q", got)
	}
	pop(t, r, c, "DELE 2", false)
	if got := pop(t, r, c, "RETR 2", false); !strings.HasPrefix(got, "-ERR") {
		t.Errorf("deleted RETR %q", got)
	}
	pop(t, r, c, "QUIT", false)
	in := inbox(t, b, "bob", store.FolderInbox)
	if len(in) != 1 || !in[0].Read {
		t.Errorf("after POP3: %+v", in)
	}
	if tr := inbox(t, b, "bob", store.FolderTrash); len(tr) != 1 || tr[0].Subject != "From the BBS" {
		t.Errorf("trash: %+v", tr)
	}
}

// testIMAP: bob's folders by IMAP: listed with their special uses, mail
// restored from the trash, starred, read, put in Sent once, and new mail
// told of.
func testIMAP(t *testing.T, b *site) {
	var mu sync.Mutex
	var exists []uint32
	c, err := imapclient.DialInsecure(b.addrs["imap"], &imapclient.Options{UnilateralDataHandler: &imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages != nil {
				mu.Lock()
				exists = append(exists, *d.NumMessages)
				mu.Unlock()
			}
		}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Login("bob", "wrong").Wait(); err == nil {
		t.Error("a wrong password")
	}
	if err := c.Login("bob", "password1").Wait(); err != nil {
		t.Fatal(err)
	}
	list, err := c.List("", "*", &imap.ListOptions{ReturnSpecialUse: true}).Collect()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range list {
		n := l.Mailbox
		for _, a := range l.Attrs {
			if a != imap.MailboxAttrHasNoChildren {
				n += string(a)
			}
		}
		names = append(names, n)
	}
	if strings.Join(names, " ") != `INBOX Sent\Sent Drafts\Drafts Starred\Flagged Unread All Mail\All Trash\Trash` {
		t.Errorf("list: %v", names)
	}
	if err := c.Create("Work", nil).Wait(); err == nil {
		t.Error("a folder made")
	}

	// out of the trash, into the inbox
	if d, err := c.Select("Trash", nil).Wait(); err != nil || d.NumMessages != 1 {
		t.Fatalf("trash: %+v %v", d, err)
	}
	cd, err := c.Copy(imap.SeqSetNum(1), "INBOX").Wait()
	if err != nil || len(cd.DestUIDs) != 1 {
		t.Fatalf("copy: %+v %v", cd, err)
	}
	if err := c.Noop().Wait(); err != nil {
		t.Fatal(err)
	}
	if mb := c.Mailbox(); mb == nil || mb.NumMessages != 0 {
		t.Errorf("trash after: %+v", mb)
	}
	d, err := c.Select("INBOX", nil).Wait()
	if err != nil || d.NumMessages != 2 {
		t.Fatalf("inbox: %+v %v", d, err)
	}

	// starred; read by fetching
	if _, err := c.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagFlagged}}, nil).Collect(); err != nil {
		t.Fatal(err)
	}
	if st, _ := c.Status("Starred", &imap.StatusOptions{NumMessages: true}).Wait(); st == nil || *st.NumMessages != 1 {
		t.Errorf("starred: %+v", st)
	}
	msgs, err := c.Fetch(imap.SeqSetNum(2), &imap.FetchOptions{Envelope: true, Flags: true, RFC822Size: true,
		BodySection: []*imap.FetchItemBodySection{{}}}).Collect()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("fetch: %v %v", msgs, err)
	}
	body := string(msgs[0].FindBodySection(&imap.FetchItemBodySection{}))
	if msgs[0].Envelope.Subject != "From the BBS" || !strings.Contains(body, "typed on a terminal") || int64(len(body)) != msgs[0].RFC822Size {
		t.Errorf("fetched: %+v %q", msgs[0].Envelope, body)
	}
	if st, _ := c.Status("Unread", &imap.StatusOptions{NumMessages: true}).Wait(); st == nil || *st.NumMessages != 0 {
		t.Errorf("unread: %+v", st)
	}
	res, err := c.Search(&imap.SearchCriteria{Body: []string{"terminal"}}, nil).Wait()
	if err != nil || len(res.AllSeqNums()) != 1 || res.AllSeqNums()[0] != 2 {
		t.Errorf("search: %+v %v", res, err)
	}

	// put in Sent: once, by its Message-ID
	mail := "From: bob@b.test\r\nTo: x@example.com\r\nSubject: Mine\r\nMessage-ID: <m1@b.test>\r\n\r\nkept\r\n"
	for i := 0; i < 2; i++ {
		ap := c.Append("Sent", int64(len(mail)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}})
		ap.Write([]byte(mail))
		ap.Close()
		if _, err := ap.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if st, _ := c.Status("Sent", &imap.StatusOptions{NumMessages: true}).Wait(); st == nil || *st.NumMessages != 1 {
		t.Errorf("sent: %+v", st)
	}

	// deleted: to the trash
	if _, err := c.Store(imap.SeqSetNum(2), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Collect(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Expunge().Collect(); err != nil {
		t.Fatal(err)
	}
	if st, _ := c.Status("Trash", &imap.StatusOptions{NumMessages: true}).Wait(); st == nil || *st.NumMessages != 1 {
		t.Errorf("trash: %+v", st)
	}

	// new mail, told by IDLE
	idle, err := c.Idle()
	if err != nil {
		t.Fatal(err)
	}
	b.svc.Receive([]*store.User{b.users["bob"]}, []byte("From: z@example.com\r\nSubject: New\r\n\r\nnew\r\n"), "")
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(exists)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	idle.Close()
	idle.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(exists) == 0 || exists[len(exists)-1] != 2 {
		t.Errorf("IDLE: %v", exists)
	}
}
