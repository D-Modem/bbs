// Package phone reads the numbers modem callers call from (the caller ID
// a modem or Asterisk gives): their country, by the ITU's calling codes,
// and in North America the state or province, by the area code.  Numbers
// as a telephone exchange gives them are often national ("4155550123",
// "02071234567"): those are read as the BBS's home country dials.
package phone

import (
	"errors"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/geoip"
)

// Number is a caller's number, read.
type Number struct {
	E164     string // "+14155550123" ("": a local number, without its area code)
	Code     string // the country calling code: "1", "44"
	National string // the number after it
	Country  string // ISO 3166-1 ("": no country's, or not known)
	Region   string // the state or province (the NANP's), if known
	Service  string // what a number that is no place's is: "toll-free", "Inmarsat"
}

var (
	// ErrWithheld is a caller ID that is no number: withheld, or not
	// given.
	ErrWithheld = errors.New("the number is withheld")
	// ErrNotNumber is a caller ID that is not a telephone number.
	ErrNotNumber = errors.New("not a telephone number")
	// ErrNational is a national number when there is no home country to
	// read it in.
	ErrNational = errors.New("a national number, and no home country to read it in")
)

// withheld are what caller IDs say for no number.
var withheld = map[string]bool{"": true, "anonymous": true, "unknown": true, "private": true,
	"restricted": true, "unavailable": true, "withheld": true, "p": true, "o": true, "out of area": true}

// codeOf is the calling code of each country.
var codeOf = func() map[string]string {
	m := map[string]string{}
	for code, cs := range countryCodes {
		for _, c := range cs {
			if _, ok := m[c]; !ok || len(code) < len(m[c]) {
				m[c] = code
			}
		}
	}
	return m
}()

// Known says whether a country is one Parse reads national numbers for.
func Known(home string) bool { _, ok := codeOf[strings.ToUpper(home)]; return ok }

// Parse reads a caller ID: an international number ("+44 20 7123 4567"),
// one dialed from home ("011 44 20...", "00 44 20..."), or a national one
// ("(415) 555-0123", "020 7123 4567"), the last two as home (an ISO 3166
// code, "": none) dials.  A name with it ("Ann <4155550123>") is passed
// over.
func Parse(s, home string) (Number, error) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j > 0 {
			s = strings.TrimSpace(s[i+1 : i+j])
		}
	}
	if withheld[strings.ToLower(s)] {
		return Number{}, ErrWithheld
	}
	plus := strings.HasPrefix(s, "+")
	var b strings.Builder
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0, r == ' ', r == '-', r == '.', r == '(', r == ')', r == '/':
		default:
			return Number{}, ErrNotNumber
		}
	}
	digits := b.String()
	if len(digits) < 3 || len(digits) > 18 {
		return Number{}, ErrNotNumber
	}
	home = strings.ToUpper(home)
	var intl string
	switch {
	case plus:
		intl = digits
	case home != "" && codeOf[home] != "":
		d, ok := dialingOf[home]
		if !ok {
			d = dialing{"00", "0"}
		}
		if p, ok := strings.CutPrefix(digits, d.intl); ok {
			intl = p
			break
		}
		cc := codeOf[home]
		national := digits
		if d.trunk != "" {
			national = strings.TrimPrefix(digits, d.trunk)
		}
		if cc == "1" && len(national) == 7 {
			// a local number, without its area code: home's, and no more
			return Number{Code: "1", National: national, Country: home}, nil
		}
		intl = cc + national
	case strings.HasPrefix(digits, "00"):
		intl = digits[2:]
	case strings.HasPrefix(digits, "011"):
		intl = digits[3:]
	default:
		return Number{}, ErrNational
	}
	return international(intl)
}

// international reads a number in full (its calling code and on).
func international(d string) (Number, error) {
	if len(d) > 15 {
		return Number{}, ErrNotNumber
	}
	var n Number
	for l := 1; l <= 3 && l < len(d); l++ {
		if _, ok := countryCodes[d[:l]]; ok {
			n.Code = d[:l]
			break
		}
		if svc, ok := nonGeographic[d[:l]]; ok {
			n.Code, n.Service = d[:l], svc
			break
		}
	}
	if n.Code == "" {
		return Number{}, ErrNotNumber
	}
	n.National = d[len(n.Code):]
	if len(n.National) < 4 {
		return Number{}, ErrNotNumber
	}
	n.E164 = "+" + d
	if n.Service != "" {
		return n, nil
	}
	if n.Code == "1" {
		if len(n.National) != 10 {
			return Number{}, ErrNotNumber
		}
		area := n.National[:3]
		if p, ok := areaPlace[area]; ok {
			n.Country, n.Region = p.country, p.region
		} else {
			n.Service = nanpServices[area]
		}
		return n, nil
	}
	n.Country = countryCodes[n.Code][0]
	best := 0
	for prefix, c := range shared[n.Code] {
		if len(prefix) > best && strings.HasPrefix(n.National, prefix) {
			n.Country, best = c, len(prefix)
		}
	}
	return n, nil
}

// CountryName is the number's country's name ("": none).
func (n Number) CountryName() string {
	if n.Country == "" {
		return ""
	}
	if name, ok := geoip.Names[n.Country]; ok {
		return name
	}
	return n.Country
}

// Where says where the number is, in a few words: "California, United
// States", "United Kingdom", "toll-free", "North America".
func (n Number) Where() string {
	switch {
	case n.Country != "" && n.Region != "":
		return n.Region + ", " + n.CountryName()
	case n.Country != "":
		return n.CountryName()
	case n.Service != "":
		return n.Service
	case n.Code == "1":
		return "North America"
	}
	return ""
}

// String is the number as it is written abroad: "+1 415-555-0123", "+44
// 2071234567"; a local number as it is.
func (n Number) String() string {
	switch {
	case n.E164 == "":
		if n.Code == "1" && len(n.National) == 7 {
			return n.National[:3] + "-" + n.National[3:]
		}
		return n.National
	case n.Code == "1":
		return "+1 " + n.National[:3] + "-" + n.National[3:6] + "-" + n.National[6:]
	}
	return "+" + n.Code + " " + n.National
}
