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
