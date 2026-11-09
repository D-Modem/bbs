package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTimeConfig: [time], [ntp], [nts] and [ptp] read, and what they
// need of each other checked.
func TestTimeConfig(t *testing.T) {
	dir := t.TempDir()
	load := func(conf string) (*Config, error) {
		p := filepath.Join(dir, "bbs.conf")
		os.WriteFile(p, []byte("[general]\ndatabase = "+filepath.Join(dir, "bbs.db")+"\n[listen telnet]\naddress = :2323\n"+conf), 0600)
		return Load(p)
	}
	c, err := load("[time]\nservers = nts time.cloudflare.com, ptp 192.0.2.1\npoll = 32\n[ntp]\naddress = 127.0.0.1:1123\n")
	if err != nil {
		t.Fatal(err)
	}
	ts := c.Time
	if len(ts.Sync.Servers) != 2 || ts.Sync.Poll.Seconds() != 32 || ts.NTP.Address != "127.0.0.1:1123" ||
		ts.NTP.Rate != 2 || ts.PTP == nil || ts.PTP.Master || ts.UTCOffset != 37 {
		t.Errorf("%+v %+v", ts, ts.PTP)
	}
	if c, err = load("[ptp]\ndomain = 4\nlog_min_interval = -2\n"); err != nil || !c.Time.PTP.Master ||
		c.Time.PTP.Domain != 4 || c.Time.PTP.MinLog != -2 || c.Time.PTP.Multicast || !c.Time.PTP.Unicast {
		t.Errorf("%+v %v", c.Time.PTP, err)
	}
	for conf, want := range map[string]string{
		"[nts]\ntls_cert = x\ntls_key = y\n": "needs [ntp]",
		"[ntp]\n[nts]\n":                     "tls_cert",
		"[time]\nservers = sntp x\n":         "servers",
		"[ptp]\nmulticast = yes\n":           "interface",
		"[ptp]\ntimestamping = hardware\n":   "interface",
		"[ptp]\naddress = eth0\n":            "address",
		"[time]\nrefid = TOOLONG\n":          "refid",
		"[ntp]\naddress = 123\n":             "address",
	} {
		if _, err := load(conf); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", conf, err)
		}
	}
	if c, _ := load(""); c.Time != nil {
		t.Error("time without its sections")
	}
}
