package bbs

import (
	"path/filepath"
	"testing"
)

// Callers are placed by their caller ID, else their address; a post
// keeps the poster's country.
func TestPlace(t *testing.T) {
	b, nodes := chatBBS(t)
	b.Settings.GeoIP = filepath.Join("..", "geoip", "testdata", "test-24.mmdb")
	b.Settings.PhoneCountry = "US"
	for _, c := range []struct {
		caller Caller
		want   Place
	}{
		{Caller{Source: "ip:198.51.100.200"}, Place{Country: "DE", Name: "Germany"}},
		{Caller{Source: "ip:127.0.0.1"}, Place{}},
		{Caller{Source: "ip:127.0.0.1", CallerID: "4155550123"},
			Place{Country: "US", Name: "United States", Region: "San Francisco, CA", Number: "+1 415-555-0123",
				Kind: "fixed line or mobile"}},
		{Caller{CallerID: "8005550123"}, Place{Country: "US", Name: "United States", Number: "+1 800-555-0123",
			Service: "toll-free", Kind: "toll-free"}},
		{Caller{Source: "ip:81.2.69.160", CallerID: "anonymous"}, Place{Country: "GB", Name: "United Kingdom"}},
	} {
		if got := b.Locate(&c.caller); got != c.want {
			t.Errorf("%+v: %+v, want %+v", c.caller, got, c.want)
		}
	}
	if s := (Place{Country: "US", Name: "United States", Service: "toll-free"}).String(); s != "United States (toll-free)" {
		t.Errorf("place: %q", s)
	}

	a, _ := b.Store.CreateArea("General", "", false)
	n := nodes["alice"]
	n.Caller.Source = "ip:198.51.100.200"
	id, err := n.Post(a, "All", "Hallo", []string{"From Berlin."}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := b.Store.Message(id); m.Country != "DE" {
		t.Errorf("the post's country: %q", m.Country)
	}
	for _, ni := range b.Nodes() {
		if ni.User == "alice" && ni.Place.Country != "DE" {
			t.Errorf("who's online: %+v", ni.Place)
		}
	}
}

// A city database: the region and city of callers by address.
func TestPlaceCity(t *testing.T) {
	b, _ := chatBBS(t)
	b.Settings.GeoIP = filepath.Join("..", "geoip", "testdata", "test-city.mmdb")
	for _, c := range []struct {
		ip   string
		want Place
		str  string
	}{
		{"81.2.69.160", Place{Country: "GB", Name: "United Kingdom", Region: "England", City: "London"}, "London, England, United Kingdom"},
		{"192.0.2.1", Place{Country: "US", Name: "United States", Region: "California"}, "California, United States"},
		{"203.0.113.9", Place{Country: "NZ", Name: "New Zealand"}, "New Zealand"},
	} {
		p := b.Locate(&Caller{Source: "ip:" + c.ip})
		if p != c.want || p.String() != c.str {
			t.Errorf("%s: %+v %q", c.ip, p, p.String())
		}
	}
	if s := (Place{Country: "JP", Name: "Japan", Region: "Tokyo", City: "Tokyo"}).String(); s != "Tokyo, Japan" {
		t.Errorf("Tokyo: %q", s)
	}
}
