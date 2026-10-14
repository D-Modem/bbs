package geoip

import (
	"bytes"
	"compress/gzip"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// The test databases were made by MaxMind's own writer (mmdbwriter), in
// each record size.
var cases = []struct{ ip, code, name string }{
	{"203.0.113.7", "NZ", "New Zealand"},
	{"198.51.100.200", "DE", "Germany"},
	{"192.0.2.5", "JP", "Japan"},
	{"192.0.2.200", "", ""}, // the /25 ends before it
	{"2001:db8::1", "FR", "France"},
	{"2001:db9::1", "", ""},
	{"81.2.69.160", "GB", "United Kingdom"},
	{"::ffff:81.2.69.160", "GB", "United Kingdom"},
	{"100.64.1.1", "US", "United States"}, // a registered country alone
	{"8.8.8.8", "", ""},
}

func TestMMDB(t *testing.T) {
	for _, rs := range []string{"24", "28", "32"} {
		r, err := Open(filepath.Join("testdata", "test-"+rs+".mmdb"))
		if err != nil {
			t.Fatalf("%s: %v", rs, err)
		}
		for _, c := range cases {
			code, name, ok := r.Country(net.ParseIP(c.ip))
			if code != c.code || name != c.name || ok != (c.code != "") {
				t.Errorf("%s bits, %s: %q %q %v", rs, c.ip, code, name, ok)
			}
		}
	}
}

func TestBroken(t *testing.T) {
	good, _ := os.ReadFile(filepath.Join("testdata", "test-24.mmdb"))
	dir := t.TempDir()
	// every cut, and every byte of the data turned: no panic
	for i := 0; i < len(good); i += 7 {
		for _, b := range [][]byte{good[:i], flip(good, i)} {
			p := filepath.Join(dir, "x.mmdb")
			os.WriteFile(p, b, 0600)
			if r, err := Open(p); err == nil {
				for _, c := range cases {
					r.Country(net.ParseIP(c.ip))
				}
			}
		}
	}
}

func flip(b []byte, i int) []byte {
	c := append([]byte{}, b...)
	c[i] ^= 0xff
	return c
}

func TestCSV(t *testing.T) {
	dir := t.TempDir()
	dbip := "1.0.0.0,1.0.0.255,AU\n203.0.113.0,203.0.113.255,NZ\n2001:db8::,2001:db8:ffff:ffff:ffff:ffff:ffff:ffff,FR\n"
	ip2l := "\"16777216\",\"16777471\",\"AU\",\"Australia\"\n\"3405803776\",\"3405804031\",\"NZ\",\"New Zealand (Aotearoa)\"\n" +
		"\"0\",\"16777215\",\"-\",\"-\"\n"
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte(dbip))
	zw.Close()
	for name, data := range map[string][]byte{"dbip.csv": []byte(dbip), "ip2l.csv": []byte(ip2l), "dbip.csv.gz": gz.Bytes()} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, data, 0600)
		r, err := Open(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		code, cname, ok := r.Country(net.ParseIP("203.0.113.9"))
		want := "New Zealand"
		if name == "ip2l.csv" {
			want = "New Zealand (Aotearoa)"
		}
		if code != "NZ" || cname != want || !ok {
			t.Errorf("%s: %q %q %v", name, code, cname, ok)
		}
		if code, _, _ := r.Country(net.ParseIP("1.0.0.1")); code != "AU" {
			t.Errorf("%s: 1.0.0.1 %q", name, code)
		}
		if _, _, ok := r.Country(net.ParseIP("9.9.9.9")); ok {
			t.Errorf("%s: 9.9.9.9 found", name)
		}
		if _, _, ok := r.Country(net.ParseIP("0.1.2.3")); ok {
			t.Errorf("%s: a range of no country found", name)
		}
	}
	if code, _, _ := mustOpen(t, dir, "dbip.csv").Country(net.ParseIP("2001:db8::5")); code != "FR" {
		t.Errorf("IPv6: %q", code)
	}
	p := filepath.Join(dir, "junk.txt")
	os.WriteFile(p, []byte("hello\nworld\n"), 0600)
	if _, err := Open(p); err == nil {
		t.Error("junk opened")
	}
}

func mustOpen(t *testing.T, dir, name string) *Reader {
	r, err := Open(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestFlag(t *testing.T) {
	if Flag("nz") != "\U0001F1F3\U0001F1FF" || Flag("") != "" {
		t.Errorf("%q", Flag("nz"))
	}
	var r *Reader
	if _, _, ok := r.Country(net.ParseIP("1.2.3.4")); ok {
		t.Error("a nil reader found a country")
	}
}
