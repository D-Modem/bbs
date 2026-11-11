package server

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestDNSConfig: [dns]'s defaults (the front ends set up, the host's DNS
// servers), the sysop's primary and backup, and bad settings refused.
func TestDNSConfig(t *testing.T) {
	dir := t.TempDir()
	load := func(body string) (*Config, error) {
		p := filepath.Join(dir, "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+
			"[listen cis]\ntype = telnet\naddress = :2324\nfrontend = compuserve\n"+body), 0600)
		return Load(p)
	}
	c, err := load("[msn]\n[oscar]\n[dns]\n")
	if err != nil {
		t.Fatal(err)
	}
	d := c.DNS
	if d.Address != ":53" || d.Upstreams != nil || d.Answer != nil || d.CacheSize != 10000 ||
		d.MaxTTL != 24*time.Hour || d.TTL != 60 || len(d.Allow) == 0 {
		t.Errorf("defaults %+v", d)
	}
	if want := []string{"msn", "oscar", "compuserve"}; !slices.Equal(d.Services, want) {
		t.Errorf("services %v, want %v", d.Services, want)
	}
	if !slices.ContainsFunc(c.Warnings, func(w string) bool { return strings.Contains(w, "https_address") }) {
		t.Errorf("no warning of MSN's Passport port: %v", c.Warnings)
	}
	c, err = load("[ymsg]\n[dns]\naddress = 192.168.77.1:53\nprimary = 1.1.1.1\nbackup = [2606:4700::1111]:5353\n" +
		"answer = 192.168.77.1\nservices = msn, ymsg\nnames = bbs.example.org *.retro.example\nallow = 192.168.77.0/24\n" +
		"cache_size = 0\n")
	if err != nil {
		t.Fatal(err)
	}
	d = c.DNS
	if !slices.Equal(d.Upstreams, []string{"1.1.1.1:53", "[2606:4700::1111]:5353"}) ||
		!d.Answer.Equal(net.IPv4(192, 168, 77, 1)) || !slices.Equal(d.Services, []string{"msn", "ymsg"}) ||
		len(d.Names) != 2 || len(d.Allow) != 1 || d.CacheSize != 0 {
		t.Errorf("set %+v", d)
	}
	if !slices.ContainsFunc(c.Warnings, func(w string) bool { return strings.Contains(w, "msn is not set up") }) {
		t.Errorf("no warning of msn not set up: %v", c.Warnings)
	}
	if c, err := load(""); err != nil || c.DNS != nil {
		t.Errorf("no [dns]: %+v %v", c.DNS, err)
	}
	for body, want := range map[string]string{
		"[dns]\naddress = 53\n":            "address",
		"[dns]\nbackup = 8.8.8.8\n":        "backup",
		"[dns]\nprimary = dns.google\n":    "primary",
		"[dns]\nanswer = ::1\n":            "answer",
		"[dns]\nservices = myspace\n":      "services",
		"[dns]\nnames = example\n":         "names",
		"[dns]\nallow = everyone\n":        "allow",
		"[dns]\nnames = a.*.example.org\n": "names",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
}

// TestSharedPorts: the front ends' web servers on one port share it; one
// both TLS and plain is refused.
func TestSharedPorts(t *testing.T) {
	dir := t.TempDir()
	cert, key := selfSigned(t, dir)
	load := func(body string) (*Config, error) {
		p := filepath.Join(dir, "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	tlsKeys := "tls_cert = " + cert + "\ntls_key = " + key + "\n"
	c, err := load("[msn]\nhttps_address = :443\n" + tlsKeys + "[ymsg]\nhttps_address = 0.0.0.0:443\nhttp_address = :80\n" +
		tlsKeys + "[oscar]\ntls_address = :443\n" + tlsKeys)
	if err != nil {
		t.Fatal(err)
	}
	ports, err := c.SharedPorts()
	if err != nil || len(ports) != 1 {
		t.Fatalf("%+v %v", ports, err)
	}
	if p := ports[0]; p.Address != ":443" || !p.TLS || !slices.Equal(p.Members, []string{"msn", "ymsg", "oscar"}) {
		t.Errorf("%+v", p)
	}
	if _, err := load("[msn]\nhttp_address = :443\n[ymsg]\nhttps_address = :443\n" + tlsKeys); err == nil ||
		!strings.Contains(err.Error(), "both a TLS port and a plain one") {
		t.Errorf("TLS and plain on one port: %v", err)
	}
}

// TestDNSServiceAnswers: a front end's HTTPS server on an IP of its own is
// what [dns] gives for its names; <service>_answer says otherwise.
func TestDNSServiceAnswers(t *testing.T) {
	dir := t.TempDir()
	cert, key := selfSigned(t, dir)
	tlsKeys := "tls_cert = " + cert + "\ntls_key = " + key + "\n"
	load := func(body string) (*Config, error) {
		p := filepath.Join(dir, "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load("[msn]\nhttps_address = 192.168.77.11:443\n" + tlsKeys + "[ymsg]\nhttps_address = 192.168.77.12:443\n" +
		tlsKeys + "[oscar]\ntls_address = 192.168.77.13:443\n" + tlsKeys + "[dns]\nprimary = 1.1.1.1\nymsg_answer = 10.0.0.12 fd00::12\n")
	if err != nil {
		t.Fatal(err)
	}
	if ports, err := c.SharedPorts(); err != nil || len(ports) != 0 {
		t.Errorf("an IP each: shared %v %v", ports, err)
	}
	a := c.DNS.ServiceAnswers
	if a["msn"][0].String() != "192.168.77.11" || a["oscar"][0].String() != "192.168.77.13" ||
		len(a["ymsg"]) != 2 || a["ymsg"][0].String() != "10.0.0.12" {
		t.Errorf("answers %v", a)
	}
	if _, err := load("[msn]\nhttps_address = 192.168.77.11:443\n" + tlsKeys + "[ymsg]\nhttps_address = :443\n" +
		tlsKeys); err == nil || !strings.Contains(err.Error(), "an IP of its own") {
		t.Errorf("every address and one IP on one port: %v", err)
	}
	if _, err := load("[msn]\n[dns]\nmsn_answer = 1.2.3.4 5.6.7.8\n"); err == nil || !strings.Contains(err.Error(), "msn_answer") {
		t.Errorf("two IPv4 answers: %v", err)
	}
}

// TestDNSRecords: the [dnsrecord NAME]s, by NAME or their name, a bad one
// refused, and one without [dns] warned of.
func TestDNSRecords(t *testing.T) {
	dir := t.TempDir()
	load := func(body string) (*Config, error) {
		p := filepath.Join(dir, "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load("[dns]\nttl = 90\n[dnsrecord games.bbs.example]\nvalue = 192.168.77.20\n" +
		"[dnsrecord mail]\nname = bbs.example\ntype = mx\nvalue = 10 mail.bbs.example\nttl = 3600\n")
	if err != nil {
		t.Fatal(err)
	}
	if r := c.DNS.Records; len(r) != 2 || r[0].Header().Name != "games.bbs.example." || r[0].Header().Ttl != 90 ||
		r[1].Header().Name != "bbs.example." || r[1].Header().Ttl != 3600 {
		t.Errorf("records %v", r)
	}
	if _, err := load("[dns]\n[dnsrecord x.example]\ntype = MX\nvalue = nope\n"); err == nil ||
		!strings.Contains(err.Error(), "[dnsrecord x.example]") {
		t.Errorf("bad MX: %v", err)
	}
	c, err = load("[dnsrecord x.example]\nvalue = 10.0.0.1\n")
	if err != nil || !slices.ContainsFunc(c.Warnings, func(w string) bool { return strings.Contains(w, "without a [dns]") }) {
		t.Errorf("no [dns]: %v %v", err, c.Warnings)
	}
}
