package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The country database and the home country of caller IDs; the image
// boards take [general]'s database unless they have their own.
func TestPlaceConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load("[general]\ngeoip = /nowhere/countries.mmdb\ngeoip_credit = DB-IP\ngeoip_credit_url = https://db-ip.com\nphone_country = gb\n[http]\ntls = no\n")
	if err != nil {
		t.Fatal(err)
	}
	if s := c.Settings; s.GeoIP != "/nowhere/countries.mmdb" || s.PhoneCountry != "GB" || s.GeoCredit != "DB-IP" {
		t.Errorf("settings %+v", s)
	}
	if c.Chan.GeoIP != c.Settings.GeoIP || c.Chan.GeoCredit != "DB-IP" || c.Chan.GeoCreditURL != "https://db-ip.com" {
		t.Errorf("[chan] %q %q %q", c.Chan.GeoIP, c.Chan.GeoCredit, c.Chan.GeoCreditURL)
	}
	if !c.HTTP.ForumFlags {
		t.Error("forum_flags is not on")
	}
	if w := strings.Join(c.Warnings, "\n"); !strings.Contains(w, "geoip:") || strings.Contains(w, "[chan] geoip") {
		t.Errorf("warnings: %v", c.Warnings)
	}
	c, err = load("[general]\ngeoip = /nowhere/a.mmdb\n[chan]\ngeoip = /nowhere/b.csv\n[http]\ntls = no\nforum_flags = no\n")
	if err != nil || c.Chan.GeoIP != "/nowhere/b.csv" || c.HTTP.ForumFlags {
		t.Errorf("own: %q %v", c.Chan.GeoIP, err)
	}
	for _, bad := range []string{"phone_country = XY", "phone_country = USA", "geoip_credit_url = ftp://x"} {
		if _, err := load("[general]\n" + bad + "\n"); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}
