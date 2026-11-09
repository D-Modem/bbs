package bbs

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/store"
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
		{Caller{Source: "ip:198.51.100.200"}, Place{Country: "DE", Name: "Germany", By: "address"}},
		{Caller{Source: "ip:127.0.0.1"}, Place{}},
		{Caller{Source: "ip:127.0.0.1", CallerID: "4155550123"},
			Place{Country: "US", Name: "United States", Region: "San Francisco, CA", Number: "+1 415-555-0123",
				Kind: "fixed line or mobile", By: "caller ID"}},
		{Caller{CallerID: "8005550123"}, Place{Country: "US", Name: "United States", Number: "+1 800-555-0123",
			Service: "toll-free", Kind: "toll-free", By: "caller ID"}},
		{Caller{Source: "ip:81.2.69.160", CallerID: "anonymous"}, Place{Country: "GB", Name: "United Kingdom", By: "address"}},
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
		p.By = ""
		if p != c.want || p.String() != c.str {
			t.Errorf("%s: %+v %q", c.ip, p, p.String())
		}
	}
	if s := (Place{Country: "JP", Name: "Japan", Region: "Tokyo", City: "Tokyo"}).String(); s != "Tokyo, Japan" {
		t.Errorf("Tokyo: %q", s)
	}
}

// A modem call by VoIP without a caller ID is placed by the address its
// call came from (DMODEM_PEER), in the shapes modem servers write it.
func TestPlacePeer(t *testing.T) {
	b, _ := chatBBS(t)
	b.Settings.GeoIP = filepath.Join("..", "geoip", "testdata", "test-city.mmdb")
	for _, peer := range []string{"81.2.69.160", "81.2.69.160:5060", "sip:81.2.69.160:5060", "sip:bob@81.2.69.160", "[::ffff:81.2.69.160]:5060"} {
		p := b.Locate(&Caller{Source: "ip:127.0.0.1", Peer: peer})
		if p.String() != "London, England, United Kingdom" || p.By != "VoIP address" {
			t.Errorf("%s: %+v", peer, p)
		}
	}
	if p := b.Locate(&Caller{Peer: "nonsense"}); p != (Place{}) {
		t.Errorf("nonsense: %+v", p)
	}
	// the caller ID comes first
	b.Settings.PhoneCountry = "US"
	if p := b.Locate(&Caller{Peer: "81.2.69.160", CallerID: "4155550123"}); p.By != "caller ID" {
		t.Errorf("with a caller ID: %+v", p)
	}
}

// A new user's location is guessed (not a toll-free number's); a user who
// shares where they are has it kept at each login, and forgotten when
// they stop.
func TestGeoShare(t *testing.T) {
	b, _ := chatBBS(t)
	b.Settings.GeoIP = filepath.Join("..", "geoip", "testdata", "test-city.mmdb")
	b.Settings.PhoneCountry = "US"
	enter := func(c Caller) *Node {
		n, err := b.Enter(&c, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(n.Leave)
		return n
	}
	if g := enter(Caller{Source: "ip:81.2.69.160"}).LocationGuess(); g != "London, England, United Kingdom" {
		t.Errorf("guess: %q", g)
	}
	if g := enter(Caller{CallerID: "8005550123"}).LocationGuess(); g != "" {
		t.Errorf("toll-free guess: %q", g)
	}
	b.Settings.GeoPrefill = false
	if g := enter(Caller{Source: "ip:81.2.69.160"}).LocationGuess(); g != "" {
		t.Errorf("guess, prefill off: %q", g)
	}
	if !b.GeoOffered() {
		t.Error("not offered")
	}

	n := enter(Caller{Source: "ip:81.2.69.160"})
	u, err := n.Register("Zed", "zedpassword", "")
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := b.Store.UserByID(u.ID); u.GeoShare || u.Geo.Place != "" || GeoWords(u) != "not shared" {
		t.Errorf("not shared yet: %+v", u)
	}
	if err := n.SetGeoShare(true); err != nil {
		t.Fatal(err)
	}
	u, _ = b.Store.UserByID(u.ID)
	if !u.GeoShare || u.Geo.Country != "GB" || u.Geo.By != "address" ||
		!strings.HasPrefix(GeoWords(u), "London, England, United Kingdom (by address, 20") {
		t.Errorf("shared: %+v %q", u.Geo, GeoWords(u))
	}

	// the next login, from elsewhere
	n2 := enter(Caller{Source: "ip:192.0.2.1"})
	if _, err := n2.Login("Zed", "zedpassword"); err != nil {
		t.Fatal(err)
	}
	if u, _ = b.Store.UserByID(u.ID); u.Geo.Place != "California, United States" {
		t.Errorf("the next login: %+v", u.Geo)
	}
	if err := n2.SetGeoShare(false); err != nil {
		t.Fatal(err)
	}
	if u, _ = b.Store.UserByID(u.ID); u.GeoShare || u.Geo != (store.Geo{}) {
		t.Errorf("no longer shared: %+v", u.Geo)
	}
	// not kept while not shared
	if err := b.Store.SetGeo(u.ID, store.Geo{Country: "NZ", Place: "New Zealand"}); err != nil {
		t.Fatal(err)
	}
	if u, _ = b.Store.UserByID(u.ID); u.Geo != (store.Geo{}) {
		t.Errorf("kept unshared: %+v", u.Geo)
	}
}
