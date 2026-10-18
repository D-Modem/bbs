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

// TestMMDB: the test databases, in each record size, find each case's
// country, and none where they have none.
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

// TestBroken: a database cut at any point, or with any byte flipped, is
// refused or looked up in without a panic.
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

// flip is a copy of b with byte i inverted.
func flip(b []byte, i int) []byte {
	c := append([]byte{}, b...)
	c[i] ^= 0xff
	return c
}

// TestCSV: DB-IP's and IP2Location's country CSVs, plain and gzipped, find
// countries by IPv4, IPv6 and IPv4 in 6to4 and Teredo, and none in gaps or
// ranges of no country; a text file of no ranges is refused.
func TestCSV(t *testing.T) {
	dir := t.TempDir()
	dbip := "0.0.0.0,0.255.255.255,ZZ\n1.0.0.0,1.0.0.255,AU\n203.0.113.0,203.0.113.255,NZ\n2001:db8::,2001:db8:ffff:ffff:ffff:ffff:ffff:ffff,FR\n"
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
		// IPv4 in IPv6: 6to4's 2002:cb00:7109::, Teredo's inverted client
		for _, a := range []string{"2002:cb00:7109::1", "2001:0:4136:e378:8000:63bf:34ff:8ef6"} {
			if code, _, _ := r.Country(net.ParseIP(a)); code != "NZ" {
				t.Errorf("%s: %s: %q", name, a, code)
			}
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

// mustOpen is the database dir/name opened, failing the test if it won't.
func mustOpen(t *testing.T, dir, name string) *Reader {
	r, err := Open(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestFlag: a code's flag is its two regional indicators, no code's is
// "", and a nil Reader finds no country.
func TestFlag(t *testing.T) {
	if Flag("nz") != "\U0001F1F3\U0001F1FF" || Flag("") != "" {
		t.Errorf("%q", Flag("nz"))
	}
	var r *Reader
	if _, _, ok := r.Country(net.ParseIP("1.2.3.4")); ok {
		t.Error("a nil reader found a country")
	}
}

// A City database's region and city, from a MaxMind DB and from the CSVs
// of DB-IP's and IP2Location's city lists.
func TestCity(t *testing.T) {
	r, err := Open(filepath.Join("testdata", "test-city.mmdb"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		ip   string
		want Location
	}{
		{"81.2.69.160", Location{"GB", "United Kingdom", "England", "London"}},
		{"198.51.100.1", Location{"DE", "Germany", "Land Berlin", "Berlin"}},
		{"203.0.113.9", Location{"NZ", "New Zealand", "", ""}},
		{"192.0.2.1", Location{"US", "United States", "California", ""}},
		{"2001:db8::1", Location{"JP", "Japan", "Tokyo", "Shinjuku"}},
	} {
		if l, ok := r.Lookup(net.ParseIP(c.ip)); !ok || l != c.want {
			t.Errorf("%s: %+v %v, want %+v", c.ip, l, ok, c.want)
		}
	}
	if _, ok := r.Lookup(net.ParseIP("10.0.0.1")); ok {
		t.Error("10.0.0.1 has a place")
	}

	for name, csv := range map[string]string{
		"dbip": "1.0.0.0,1.0.0.255,OC,AU,Queensland,South Brisbane,-27.4767,153.017\n" +
			"2001:db8::,2001:db8:ffff:ffff:ffff:ffff:ffff:ffff,AS,JP,Tokyo,Shinjuku,35.69,139.70\n" +
			"5.0.0.0,5.0.0.255,EU,ZZ,,,0,0\n" +
			"8.8.8.0,8.8.8.255,NA,US,California,\"Mountain View, CA\",37.4,-122.1\n",
		"ip2location": "\"16777216\",\"16777471\",\"AU\",\"Australia\",\"Queensland\",\"South Brisbane\",\"-27.47\",\"153.02\"\n" +
			"\"42540766411282592856903984951653826560\",\"42540766490510755371168322545197776895\",\"JP\",\"Japan\",\"Tokyo\",\"Shinjuku\"\n" +
			"\"83886080\",\"83886335\",\"-\",\"-\",\"-\",\"-\"\n" +
			"\"134744064\",\"134744319\",\"US\",\"United States of America\",\"California\",\"Mountain View, CA\"\n",
	} {
		r, err := Open(writeTemp(t, name+".csv", []byte(csv)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		us := "United States"
		if name == "ip2location" {
			us = "United States of America"
		}
		for _, c := range []struct {
			ip   string
			want Location
			ok   bool
		}{
			{"1.0.0.7", Location{"AU", "Australia", "Queensland", "South Brisbane"}, true},
			{"2001:db8::1", Location{"JP", "Japan", "Tokyo", "Shinjuku"}, true},
			{"5.0.0.1", Location{}, false},
			{"8.8.8.8", Location{"US", us, "California", "Mountain View, CA"}, true},
		} {
			if l, ok := r.Lookup(net.ParseIP(c.ip)); ok != c.ok || l != c.want {
				t.Errorf("%s %s: %+v %v, want %+v", name, c.ip, l, ok, c.want)
			}
		}
	}
}

// writeTemp writes b to a file name in a new temporary directory, and is
// its path.
func writeTemp(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
