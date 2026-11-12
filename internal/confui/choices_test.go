package confui

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	bbssec "github.com/D-Modem/bbs"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/confedit"
	"github.com/D-Modem/bbs/internal/view"
)

// editorOf is an Editor of the file text, not run: its fields to look at.
func editorOf(t *testing.T, text string) *Editor {
	t.Helper()
	doc, err := confedit.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := confedit.Parse([]byte(text))
	return &Editor{UI: &script{t: t}, cat: confedit.NewCatalog(bbssec.Sample), doc: doc, orig: orig}
}

// fakeAddrs makes this machine's addresses eth0's 192.0.2.10 and loopback's.
func fakeAddrs(t *testing.T) {
	was := localAddrs
	localAddrs = func() []ifAddr {
		return []ifAddr{{"lo", net.ParseIP("127.0.0.1")}, {"eth0", net.ParseIP("192.0.2.10")}}
	}
	t.Cleanup(func() { localAddrs = was })
}

// noteOf is the note beside choice v of it ("" for none; fails if v is no
// choice).
func noteOf(t *testing.T, it item, v string) string {
	t.Helper()
	i := slices.Index(it.Choices, v)
	if i < 0 {
		t.Fatalf("%s: no %q in %q", it.Label, v, it.Choices)
	}
	return it.ChoiceNote(i)
}

// TestCertificateChoices: a certificate is picked from [acme]'s and the
// files other servers name; its key from the one that goes with it.
func TestCertificateChoices(t *testing.T) {
	e := editorOf(t, "[acme]\ndomains = bbs.example.org\n\n[acme mail]\ndomains = mail.example.org\n\n"+
		"[listen telnets]\ntype = telnet\naddress = :992\ntls_cert = /etc/bbs/tn.crt\ntls_key = /etc/bbs/tn.key\n\n"+
		"[irc]\naddress = :6667\ntls_cert = /etc/bbs/tn.crt\n\n[msn]\ntls_cert = acme:mail\n")
	cert := e.field(sec("irc"), "tls_cert")
	if cert.Kind != view.EntryChoice || !cert.Open {
		t.Fatalf("irc tls_cert: kind %v open %v", cert.Kind, cert.Open)
	}
	if n := noteOf(t, cert, "acme:mail"); n != "mail.example.org" {
		t.Errorf("acme:mail's note %q", n)
	}
	if n := noteOf(t, cert, "/etc/bbs/tn.crt"); !strings.Contains(n, "[listen telnets]") {
		t.Errorf("tn.crt's note %q", n)
	}
	noteOf(t, cert, "acme")
	if slices.Contains(cert.Choices, "ca") {
		t.Error("irc offered the BBS's CA")
	}
	noteOf(t, e.field(sec("msn"), "tls_cert"), "ca")
	key := e.field(sec("irc"), "tls_key")
	if len(key.Choices) < 2 || key.Choices[1] != "/etc/bbs/tn.key" || !strings.Contains(key.ChoiceNote(1), "goes with tn.crt") {
		t.Errorf("irc tls_key: %q %q", key.Choices, key.ChoiceNotes)
	}
	if k := e.field(sec("msn"), "tls_key"); k.Default != "not needed with acme:mail" {
		t.Errorf("msn tls_key's default %q", k.Default)
	}
	// a file of one's own may still be typed
	if err := cert.set("/etc/bbs/irc.crt"); err != nil {
		t.Error(err)
	}
}

// TestAddressChoices: an address is offered on every address, this
// machine's, and each of its IPs, those other services have marked.
func TestAddressChoices(t *testing.T) {
	fakeAddrs(t)
	e := editorOf(t, "[ymsg]\nhttps_address = 192.0.2.10:443\n\n[msn]\nhttps_address = :443\n\n[oscar]\naddress = :5190\n")
	it := e.field(sec("oscar"), "tls_address")
	for _, v := range []string{":443", "127.0.0.1:443", "192.0.2.10:443"} {
		noteOf(t, it, v)
	}
	if n := noteOf(t, it, "192.0.2.10:443"); !strings.Contains(n, "[ymsg] https_address") {
		t.Errorf("192.0.2.10:443's note %q", n)
	}
	if n := noteOf(t, it, ":443"); !strings.Contains(n, "[msn] https_address") {
		t.Errorf(":443's note %q", n)
	}
	// the port it has is kept
	it = e.field(sec("oscar"), "address")
	noteOf(t, it, "192.0.2.10:5190")
	// [dns]'s answers: the services' own IPs, and this machine's
	a := e.field(sec("dns"), "msn_answer")
	if n := noteOf(t, a, "192.0.2.10"); !strings.Contains(n, "ymsg") {
		t.Errorf("answer 192.0.2.10's note %q", n)
	}
}

// TestClosedAndMultiChoices: a closed list takes its values only; a list
// of some takes several, by commas.
func TestClosedAndMultiChoices(t *testing.T) {
	e := editorOf(t, "[mail]\ndomain = example.org\n\n[msn]\naddress = :1863\n\n[dns]\naddress = :53\n")
	out := e.field(sec("mail"), "outbound")
	if out.Kind != view.EntryChoice || out.Open {
		t.Fatalf("outbound: kind %v open %v", out.Kind, out.Open)
	}
	if err := out.set("carrier pigeon"); err == nil {
		t.Error("outbound took carrier pigeon")
	}
	if err := out.set("smarthost"); err != nil {
		t.Error(err)
	}
	svc := e.field(sec("dns"), "services")
	if !svc.Multi || noteOf(t, svc, "msn") != "set up here" || noteOf(t, svc, "ymsg") != "not set up here" {
		t.Errorf("services: %q %q", svc.Choices, svc.ChoiceNotes)
	}
	if err := svc.set("msn, oscar"); err != nil {
		t.Error(err)
	}
	if v, _ := e.doc.Get(sec("dns"), "services"); v != "msn, oscar" {
		t.Errorf("services = %q", v)
	}
}

// TestLevelChoices: levels are picked from the [level]s, by name.
func TestLevelChoices(t *testing.T) {
	e := editorOf(t, "[level 20]\nname = Member\n\n[level 10]\nname = New user\n")
	it := e.field(sec("general"), "new_user_level")
	if want := []string{"", "10", "20"}; !slices.Equal(it.Choices, want) {
		t.Errorf("choices %q, want %q", it.Choices, want)
	}
	if n := noteOf(t, it, "20"); n != "Member" {
		t.Errorf("20's note %q", n)
	}
	if err := it.set("30"); err != nil { // a level of no [level] of its own
		t.Error(err)
	}
}

// TestMenuItems: a menu's item is added by its key, its action picked
// from the BBS's own, the doors and the other menus.
func TestMenuItems(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bbs.conf")
	os.WriteFile(p, []byte("[general]\nname = Test\n\n[door lord]\ndescription = Legend of the Red Dragon\n\n"+
		"[menu main]\nm = areas Message areas\n\n[menu extras]\ntitle = {cyan}Extras{reset}\n"), 0o600)
	ui := &script{t: t, forms: []map[string]string{{"key": "L"}}, steps: []step{
		open("Board configuration"), open("Menus"), open("main"), open("Add a menu item"),
		func(t *testing.T, sh *view.Sheet) view.SheetResult {
			e := sh.Entries[entry(t, sh, "l")]
			if e.Value != "door:lord Legend of the Red Dragon" {
				t.Errorf("l = %q", e.Value)
			}
			if !slices.Contains(e.Choices, "menu:extras Extras") {
				t.Errorf("no menu:extras in %q", e.Choices)
			}
			return save(t, sh)
		},
	}}
	ui.tables = []func(*view.Table) view.Choice{func(tb *view.Table) view.Choice {
		for i, r := range tb.Rows {
			if strings.HasPrefix(r.Cells[1], "door:lord") {
				return view.Choice{Action: view.Pick, Row: i}
			}
		}
		t.Fatalf("no door:lord in the table")
		return view.Choice{}
	}}
	if err := (&Editor{UI: ui, Tools: bbs.ConfigTools{Path: p}}).Run(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), "l = door:lord Legend of the Red Dragon\n") {
		t.Errorf("written:\n%s", data)
	}
}

// TestPickChoice: a list of some is turned on and off by number, Done
// taking it.
func TestPickChoice(t *testing.T) {
	ui := &script{t: t}
	e := view.Entry{Label: "Servers", Kind: view.EntryChoice, Multi: true, Value: "b, own",
		Choices: []string{"a", "b", "c"}}
	ui.tables = []func(*view.Table) view.Choice{
		func(*view.Table) view.Choice { return view.Choice{Action: view.Pick, Row: 2} }, // c on
		func(*view.Table) view.Choice { return view.Choice{Action: view.Pick, Row: 1} }, // b off
		func(*view.Table) view.Choice { return view.Choice{Action: "done"} },
	}
	v, ok, err := view.PickChoice(ui, e)
	if err != nil || !ok || v != "c, own" {
		t.Errorf("%q %v %v", v, ok, err)
	}
}

// TestDNSRecordsPage: a record is added under DNS for callers, its type
// picked from a list, its address from this machine's.
func TestDNSRecordsPage(t *testing.T) {
	fakeAddrs(t)
	p := filepath.Join(t.TempDir(), "bbs.conf")
	os.WriteFile(p, []byte("[general]\nname = Test\n\n[dns]\naddress = :53\n"), 0o600)
	ui := &script{t: t, forms: []map[string]string{{"name": "games.bbs.example"}}, steps: []step{
		open("Internet services"), open("DNS for callers"), open("DNS records"), key("add"),
		func(t *testing.T, sh *view.Sheet) view.SheetResult {
			ty := sh.Entries[entry(t, sh, "type")]
			if ty.Kind != view.EntryChoice || !slices.Contains(ty.Choices, "MX") {
				t.Errorf("type: %+v", ty)
			}
			v := sh.Entries[entry(t, sh, "value")]
			if !slices.Contains(v.Choices, "192.0.2.10") {
				t.Errorf("value's choices %q", v.Choices)
			}
			return set("value", "192.0.2.10")(t, sh)
		},
		back,
		func(t *testing.T, sh *view.Sheet) view.SheetResult {
			if it := sh.Entries[entry(t, sh, "games.bbs.example")]; it.Value != "games.bbs.example A 192.0.2.10" {
				t.Errorf("summary %q", it.Value)
			}
			return save(t, sh)
		},
	}}
	if err := (&Editor{UI: ui, Tools: bbs.ConfigTools{Path: p}}).Run(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), "[dnsrecord games.bbs.example]\ntype = A\nvalue = 192.0.2.10\n") {
		t.Errorf("written:\n%s", data)
	}
}
