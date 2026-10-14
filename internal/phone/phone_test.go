package phone

import (
	"errors"
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/geoip"
)

func TestParse(t *testing.T) {
	for _, c := range []struct {
		in, home                     string
		e164, country, region, where string
		str                          string
	}{
		{"+1 (415) 555-0123", "", "+14155550123", "US", "California", "California, United States", "+1 415-555-0123"},
		{"4155550123", "US", "+14155550123", "US", "California", "California, United States", ""},
		{"14165550123", "US", "+14165550123", "CA", "Ontario", "Ontario, Canada", ""},
		{"\"Ann\" <8765550123>", "CA", "+18765550123", "JM", "", "Jamaica", ""},
		{"011 44 20 7123 4567", "US", "+442071234567", "GB", "", "United Kingdom", "+44 2071234567"},
		{"02071234567", "GB", "+442071234567", "GB", "", "United Kingdom", ""},
		{"01534 123456", "GB", "+441534123456", "JE", "", "Jersey", ""},
		{"0044 1624 123456", "", "+441624123456", "IM", "", "Isle of Man", ""},
		{"0612345678", "IT", "+390612345678", "IT", "", "Italy", ""},
		{"+390669812345", "", "+390669812345", "VA", "", "Vatican City", ""},
		{"84951234567", "RU", "+74951234567", "RU", "", "Russia", ""},
		{"+77011234567", "", "+77011234567", "KZ", "", "Kazakhstan", ""},
		{"0011 61 2 9876 5432", "AU", "+61298765432", "AU", "", "Australia", ""},
		{"+18005550123", "", "+18005550123", "", "", "toll-free", ""},
		{"+80012345678", "", "+80012345678", "", "", "international freephone", ""},
		{"+49 30 1234567", "", "+49301234567", "DE", "", "Germany", ""},
		{"5550123", "US", "", "US", "", "United States", "555-0123"},
	} {
		n, err := Parse(c.in, c.home)
		if err != nil {
			t.Errorf("%q (%s): %v", c.in, c.home, err)
			continue
		}
		if n.E164 != c.e164 || n.Country != c.country || n.Region != c.region {
			t.Errorf("%q (%s) = %+v", c.in, c.home, n)
		}
		if w := n.Where(); w != c.where && !(c.country != "" && w == c.country) {
			t.Errorf("%q: where %q, want %q", c.in, w, c.where)
		}
		if c.str != "" && n.String() != c.str {
			t.Errorf("%q: %q, want %q", c.in, n.String(), c.str)
		}
	}
}

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

// Every area code is one place's, every country one there are names for.
func TestTables(t *testing.T) {
	seen := map[string]string{}
	for _, a := range areas {
		for _, c := range strings.Fields(a.codes) {
			if len(c) != 3 || c[0] < '2' {
				t.Errorf("%s: area code %q", a.country, c)
			}
			if prev, ok := seen[c]; ok {
				t.Errorf("area code %s: %s and %s", c, prev, a.country+" "+a.region)
			}
			seen[c] = a.country + " " + a.region
			if _, ok := nanpServices[c]; ok {
				t.Errorf("area code %s is a place and a service", c)
			}
		}
	}
	for code, cs := range countryCodes {
		for _, c := range cs {
			if _, ok := geoip.Names[c]; !ok {
				t.Errorf("+%s: no name for %s", code, c)
			}
		}
		if _, ok := nonGeographic[code]; ok {
			t.Errorf("+%s is a country's and none's", code)
		}
		for l := 1; l < len(code); l++ {
			if _, ok := countryCodes[code[:l]]; ok {
				t.Errorf("+%s begins with +%s", code, code[:l])
			}
		}
	}
	for c := range dialingOf {
		if !Known(c) {
			t.Errorf("dialing for %s, which has no calling code", c)
		}
	}
}
