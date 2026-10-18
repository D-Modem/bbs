package phone

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/geoip"
)

// TestParse: numbers written as people write them, international or
// national to a home country, are their E.164, country, region, kind,
// place and international form.
func TestParse(t *testing.T) {
	for _, c := range []struct {
		in, home                     string
		e164, country, region, where string
		kind                         Kind
		str                          string
	}{
		{"+1 (415) 555-0123", "", "+14155550123", "US", "San Francisco, CA", "San Francisco, CA, United States", FixedLineOrMobile, "+1 415-555-0123"},
		{"4155550123", "US", "+14155550123", "US", "San Francisco, CA", "", FixedLineOrMobile, ""},
		{"14165550123", "US", "+14165550123", "CA", "Ontario", "Ontario, Canada", FixedLineOrMobile, ""},
		{"+1 273 555 0123", "", "+12735550123", "CA", "Quebec", "", FixedLineOrMobile, ""},
		{"+12012001234", "", "+12012001234", "US", "Jersey City, NJ", "", FixedLineOrMobile, "+1 201-200-1234"},
		{"\"Ann\" <8765550123>", "CA", "+18765550123", "JM", "", "Jamaica", Mobile, ""},
		{"011 44 20 7123 4567", "US", "+442071234567", "GB", "London", "London, United Kingdom", FixedLine, "+44 20 7123 4567"},
		{"02071234567", "GB", "+442071234567", "GB", "London", "", FixedLine, ""},
		{"01534 456789", "GB", "+441534456789", "JE", "", "Jersey", FixedLine, ""},
		{"0044 1624 756789", "", "+441624756789", "IM", "", "Isle of Man", FixedLine, ""},
		{"0612345678", "IT", "+390612345678", "IT", "Rome", "Rome, Italy", FixedLine, "+39 06 1234 5678"},
		{"+390669812345", "", "+390669812345", "VA", "", "Vatican City", FixedLine, ""},
		{"84951234567", "RU", "+74951234567", "RU", "Moscow", "", FixedLine, "+7 495 123-45-67"},
		{"+77011234567", "", "+77011234567", "KZ", "", "Kazakhstan", Mobile, ""},
		{"0011 61 2 9876 5432", "AU", "+61298765432", "AU", "Sydney", "", FixedLine, "+61 2 9876 5432"},
		{"+18005550123", "", "+18005550123", "US", "", "United States (toll-free)", TollFree, ""},
		{"+80012345678", "", "+80012345678", "", "", "international freephone", TollFree, "+800 1234 5678"},
		{"+49 30 1234567", "", "+49301234567", "DE", "Berlin", "Berlin, Germany", FixedLine, "+49 30 1234567"},
		{"+33612345678", "", "+33612345678", "FR", "", "France", Mobile, "+33 6 12 34 56 78"},
		{"+81312345678", "", "+81312345678", "JP", "Tokyo", "Tokyo, Japan", FixedLine, "+81 3-1234-5678"},
		{"0800 555 0123", "GB", "+448005550123", "GB", "", "United Kingdom (toll-free)", TollFree, "+44 800 555 0123"},
		{"8 800 555-01-23", "RU", "+78005550123", "RU", "", "Russia (toll-free)", TollFree, ""},
		{"5550123", "US", "", "US", "", "United States", "", "555-0123"},
	} {
		n, err := Parse(c.in, c.home)
		if err != nil {
			t.Errorf("%q (%s): %v", c.in, c.home, err)
			continue
		}
		if n.E164 != c.e164 || n.Country != c.country || n.Region != c.region || n.Kind != c.kind {
			t.Errorf("%q (%s) = %+v", c.in, c.home, n)
		}
		if c.where != "" && n.Where() != c.where {
			t.Errorf("%q: where %q, want %q", c.in, n.Where(), c.where)
		}
		if c.str != "" && n.String() != c.str {
			t.Errorf("%q: %q, want %q", c.in, n.String(), c.str)
		}
	}
}

// TestParseRefused: a number withheld, not a number, too short, too long,
// of no calling code, or national without a home country is refused, and
// says which.
func TestParseRefused(t *testing.T) {
	for _, c := range []struct {
		in, home string
		err      error
	}{
		{"", "US", ErrWithheld},
		{"Anonymous", "US", ErrWithheld},
		{"P", "", ErrWithheld},
		{"sip:alice@example.com", "US", ErrNotNumber},
		{"4155550123", "", ErrNational},
		{"+1415555", "", ErrNotNumber},
		{"+1234567890123456", "", ErrNotNumber},
		{"+999123456", "", ErrNotNumber},
	} {
		if _, err := Parse(c.in, c.home); !errors.Is(err, c.err) {
			t.Errorf("%q (%s): %v, want %v", c.in, c.home, err, c.err)
		}
	}
}

// The tables: every pattern compiles, every country has a name.
func TestTables(t *testing.T) {
	compile()
	if len(byID) < 240 || len(byCode["1"]) < 25 || len(byCode["44"]) != 4 {
		t.Fatalf("%d countries, %d on +1, %d on +44", len(byID), len(byCode["1"]), len(byCode["44"]))
	}
	for id, tr := range byID {
		if _, ok := geoip.Names[id]; !ok {
			t.Errorf("no name for %s (+%s)", id, tr.code)
		}
	}
	for code, ts := range byCode {
		if !ts[0].main && len(ts) > 1 {
			t.Errorf("+%s: no main country first", code)
		}
	}
	if p, _ := placeOf("1201"); p != "New Jersey" {
		t.Errorf("places: %q", p)
	}
	if !strings.Contains(Version(), "(20") {
		t.Errorf("version %q", Version())
	}
}

// NANPA's area codes: those libphonenumber has not yet, those to come on
// their day, the services; every one in service a number of its country.
func TestNANPA(t *testing.T) {
	for _, c := range []struct {
		in, country, region, service string
	}{
		{"+1 357 555 0123", "US", "California", ""},
		{"+1 621 555 0123", "US", "Texas", ""},
		{"+1 729 555 0123", "US", "Tennessee", ""},
		{"+1 210 555 0123", "US", "Texas", ""}, // libphonenumber: "San Antonio, TX", of 2013
		{"+1 787 555 0123", "PR", "", ""},
		{"+1 538 555 0123", "US", "", "personal"},
		{"+1 700 555 0123", "US", "", "carrier services"},
	} {
		n, err := Parse(c.in, "")
		if err != nil || n.Country != c.country || n.Region != c.region || n.Service != c.service {
			t.Errorf("%s: %+v %v", c.in, n, err)
		}
	}
	// a code to come is no number before its day
	defer func() { Today = time.Now }()
	day := func(s string) func() time.Time {
		return func() time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	}
	if a := nanpAreas["565"]; a.from == "" || a.region != "Georgia" {
		t.Fatalf("565: %+v", a)
	}
	Today = day("2027-01-01")
	if n, _ := Parse("+1 565 555 0123", ""); n.Kind != "" {
		t.Errorf("565 before its day: %+v", n)
	}
	Today = day(nanpAreas["565"].from)
	if n, _ := Parse("+1 565 555 0123", ""); n.Country != "US" || n.Region != "Georgia" || n.Kind != FixedLineOrMobile {
		t.Errorf("565 on its day: %+v", n)
	}
	Today = time.Now
	for code, a := range nanpAreas {
		if a.from != "" || a.service != "" {
			continue
		}
		n, err := Parse("+1"+code+"5550123", "")
		if err != nil || n.Country != a.country || n.Kind == "" {
			t.Errorf("%s (%s %s): %+v %v", code, a.country, a.region, n, err)
		}
	}
}
