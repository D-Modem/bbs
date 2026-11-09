package email

import (
	"net"
	"net/smtp"
	"strings"
	"testing"
)

func TestReversed(t *testing.T) {
	if r := reversed(net.ParseIP("192.0.2.1"), "zen.spamhaus.org"); r != "1.2.0.192.zen.spamhaus.org" {
		t.Error(r)
	}
	r := reversed(net.ParseIP("2001:db8::1"), "bl.test")
	if r != "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.bl.test" {
		t.Error(r)
	}
}

// A sender on a blocklist is refused at MAIL FROM, with the list's reason;
// one the list cannot answer for, or that is not on it, is not.
func TestDNSBL(t *testing.T) {
	dns := &fakeDNS{bl: map[string]string{
		"9.2.0.192.bl.test": "127.0.0.2", "8.2.0.192.bl.test": "127.255.255.254",
		"9.2.0.192.other.test": "127.0.0.4",
	}, txt: map[string][]string{"9.2.0.192.bl.test": {"Listed: see https://bl.test/lookup"}}}
	s := newSite(t, "a.test", dns, []string{"sysop", "bob"}, MX)
	lists := []string{"other.bl.test", "bl.test"}
	if l, why := s.svc.listedBy(net.ParseIP("192.0.2.9"), lists); l != "bl.test" || !strings.Contains(why, "https://bl.test/lookup") {
		t.Errorf("listed: %q %q", l, why)
	}
	for _, ip := range []string{"192.0.2.8", "192.0.2.7", "127.0.0.1", "10.1.2.3"} {
		if l, _ := s.svc.listedBy(net.ParseIP(ip), lists); l != "" {
			t.Errorf("%s listed by %s", ip, l)
		}
	}
	if !strings.Contains(s.log.String(), "refused the query") {
		t.Error("no note of the refused query")
	}

	// over SMTP: this test's 127.0.0.1, put on the list
	old := unlisted
	unlisted = func(net.IP) bool { return false }
	t.Cleanup(func() { unlisted = old })
	dns.mu.Lock()
	dns.bl["1.0.0.127.bl.test"] = "127.0.0.2"
	dns.mu.Unlock()
	c, err := smtp.Dial(s.addrs[MX])
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	err = c.Mail("z@example.com")
	if err == nil || !strings.Contains(err.Error(), "554") || !strings.Contains(err.Error(), "listed by bl.test") {
		t.Fatalf("mail from a listed address: %v", err)
	}
}
