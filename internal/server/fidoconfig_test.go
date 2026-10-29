package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/archive"
)

// TestFidoConfig: [fido], [binkp] and [link NAME] sections with their
// defaults and warnings, and bad addresses, packers, passwords and links
// refused.
func TestFidoConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load(`
[fido]
address = 21:1/200 2:5020/9999.1
[binkp]
[link hub]
address = 21:1/100
host = hub.example
password = secret
packet_password = PKT
echoes = FSX_*,!FSX_BOT
route = yes
poll = 3600
[link other]
address = 2:5020/1
packer = arj
host = nodelist
fileechoes = FSX*
areafix = yes
areafix_password = FIXME
tic_password = TIC
create = yes
[link third]
address = 2:5020/3
packer = none
`)
	if err != nil {
		t.Fatal(err)
	}
	f := c.Fido
	if len(f.Addrs) != 2 || f.Addrs[1].Point != 1 || f.Dir != "fido" || c.Binkp.Address != ":24554" {
		t.Fatalf("%+v %+v", f, c.Binkp)
	}
	hub := f.Links[0]
	if hub.Host != "hub.example:24554" || hub.Packer != archive.ZIP || !hub.Route || hub.Poll != time.Hour || hub.PktPassword != "PKT" {
		t.Fatalf("%+v", hub)
	}
	o := f.Links[1]
	if o.Packer != archive.ARJ || o.Host != "nodelist" || o.FileEchoes != "FSX*" || !o.AreaFix || o.AreafixPassword != "FIXME" ||
		o.TicPassword != "TIC" || !o.Create || hub.AreaFix || hub.Create || hub.FileEchoes != "" {
		t.Fatalf("%+v", o)
	}
	if f.Links[2].Packer != "" || !strings.Contains(strings.Join(c.Warnings, "\n"), "[link other] has no password") {
		t.Fatalf("%+v %v", f.Links[2], c.Warnings)
	}
	for body, want := range map[string]string{
		"[fido]\n": "[fido] address",
		"[fido]\naddress = 1:1/1\n[link x]\naddress = 1:1/2\nareafix = yes\n": "needs an areafix_password",
		"[fido]\naddress = 1:1/1\n[link x]\naddress = 1:1/2\npacker = rar\n":  "packer",
		"[fido]\naddress = 21:1\n":                             "not an FTN address",
		"[link x]\naddress = 1:1/1\n":                          "need a [fido] section",
		"[fido]\naddress = 1:1/1\n[link x]\n":                  "address",
		"[fido]\naddress = 1:1/1\n[link x]\naddress = 1:1/1\n": "our own address",
		"[fido]\naddress = 1:1/1\n[link a]\naddress = 1:1/2\nroute = yes\n[link b]\naddress = 1:1/3\nroute = yes\n": "more than one",
		"[fido]\naddress = 1:1/1\n[link a]\naddress = 1:1/2\n[link b]\naddress = 1:1/2\n":                           "has the address of",
		"[fido]\naddress = 1:1/1\n[link a]\naddress = 1:1/2\npacket_password = 123456789\n":                         "8 characters",
		"[fido]\naddress = 1:1/1\ncharset = koi8\n":                                                                 "charset",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}
