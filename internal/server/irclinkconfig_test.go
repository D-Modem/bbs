package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIRCLinkConfig: [irclink] and its peers with their defaults, none
// without it, and bad names, IDs, tags, protocols and passwords refused.
func TestIRCLinkConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	const base = "[irclink]\nserver_name = irc.bbs.example\nsid = 1bb\n"
	c, err := load(base + "tag = DM\n[ircpeer hub.example.net]\naddress = hub.example.net:7000\n" +
		"send_password = a\naccept_password = b\nprotocol = hybrid\n[ircpeer leaf.example.net]\nsend_password = c\naccept_password = d\n")
	if err != nil {
		t.Fatal(err)
	}
	l := c.IRCLink
	if l.SID != "1BB" || l.Tag != "DM" || l.Prefix != "irc-" || l.Address != ":7000" || l.NickLen != 30 || len(l.Peers) != 2 {
		t.Fatalf("irclink %+v", l)
	}
	if p := l.Peers[0]; !p.Hybrid || !p.TLSVerify || p.Tag != "hub" || p.SendPassword != "a" {
		t.Errorf("peer %+v", p)
	}
	if p := l.Peers[1]; p.Hybrid || p.Address != "" || p.Tag != "leaf" {
		t.Errorf("peer %+v", p)
	}
	if l.Numeric != -1 {
		t.Errorf("numeric %d", l.Numeric)
	}
	c, err = load(base + "numeric = 42\n[ircpeer under.example.net]\nsend_password = a\naccept_password = b\nprotocol = p10\n")
	if err != nil {
		t.Fatal(err)
	}
	if l := c.IRCLink; l.Numeric != 42 || l.NickLen != 12 || !l.Peers[0].P10 {
		t.Errorf("p10 %+v", l)
	}
	if c, err := load(""); err != nil || c.IRCLink != nil {
		t.Errorf("no [irclink]: %+v %v", c.IRCLink, err)
	}
	for body, want := range map[string]string{
		"[irclink]\nsid = 1BB\n":                          "server_name",
		"[irclink]\nserver_name = irc.bbs.example\n":      "sid",
		base + "tag = no-dashes\n":                        "tag",
		base + "prefix = Bad_\n":                          "prefix",
		base + "nick_length = 3\n":                        "nick_length",
		base + "[ircpeer x.example]\nsend_password = a\n": "accept_password",
		base + "[ircpeer x.example]\nsend_password = a\naccept_password = b\nprotocol = p10\n": "numeric",
		base + "[ircpeer x.example]\nsend_password = a\naccept_password = b\nprotocol = p9\n":  "protocol",
		base + "numeric = 4096\n": "numeric",
		"[ircpeer x.example]\n":   "without [irclink]",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", body, err)
		}
	}
}
