// Package phone reads the numbers modem callers call from (the caller ID
// a modem or Asterisk gives): their country, what kind of number it is
// and the place it is in, by Google's libphonenumber's tables of every
// country's numbering plan (metadata.go and places.gz, made by
// gen: Apache License 2.0, LICENSE.libphonenumber).  Numbers as a
// telephone exchange gives them are often national ("4155550123",
// "02071234567"): those are read as the BBS's home country dials.
package phone

import (
	"errors"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/geoip"
)

// Number is a caller's number, read.
type Number struct {
	E164     string // "+14155550123" ("": a local number, without its area code)
	Code     string // the country calling code: "1", "44"
	National string // the number after it
	Country  string // ISO 3166-1 ("": no country's, or not known)
	Region   string // the place in the country, if known: "Jersey City, NJ", "Berlin"
	Kind     Kind   // what it is, as the plan says ("": not a number the plan has)
	Service  string // what a number that is no place's is: "toll-free", "Inmarsat"

	t *territory
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

// Today is the day area codes are in service on (a variable for tests).
var Today = time.Now

// Known says whether a country is one Parse reads national numbers for.
func Known(home string) bool {
	compile()
	_, ok := byID[strings.ToUpper(home)]
	return ok
}

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
	compile()
	t := byID[strings.ToUpper(home)]
	switch {
	case plus:
		return international(digits)
	case t != nil:
		if m := t.prepare().intlRE.FindStringIndex(digits); m != nil && m[1] < len(digits) {
			return international(digits[m[1]:])
		}
		national := t.stripNational(digits)
		if t.code == "1" && len(national) == 7 {
			// a local number, without its area code: home's, and no more
			return Number{Code: "1", National: national, Country: t.id, t: t}, nil
		}
		return international(t.code + national)
	case strings.HasPrefix(digits, "00"):
		return international(digits[2:])
	case strings.HasPrefix(digits, "011"):
		return international(digits[3:])
	}
	return Number{}, ErrNational
}

// international reads a number in full (its calling code and on).
func international(d string) (Number, error) {
	if len(d) > 15 {
		return Number{}, ErrNotNumber
	}
	var n Number
	var ts []*territory
	for l := 1; l <= 3 && l < len(d); l++ {
		if ts = byCode[d[:l]]; ts != nil {
			n.Code = d[:l]
			break
		}
	}
	if n.Code == "" {
		return Number{}, ErrNotNumber
	}
	n.National = d[len(n.Code):]
	// whose: a shared code's country by how its numbers begin, else the
	// first whose plan has the number
	for _, t := range ts {
		if t.prepare().leadingRE != nil && t.leadingRE.MatchString(n.National) {
			n.t = t
			break
		}
	}
	if n.t != nil {
		n.Kind = n.t.kindOf(n.National)
	} else {
		for _, t := range ts {
			if k := t.kindOf(n.National); k != "" {
				n.t, n.Kind = t, k
				break
			}
		}
	}
	if n.t == nil {
		// not a number any of them has: one of its length, still
		for _, t := range ts {
			if t.prepare().generalRE.MatchString(n.National) {
				n.t = t
				break
			}
		}
		if n.t == nil {
			return Number{}, ErrNotNumber
		}
	}
	n.E164 = "+" + d
	// NANPA's area codes: those in service before libphonenumber has them
	// are numbers still, and its states and provinces are the area codes'
	var area nanpArea
	if n.Code == "1" && len(n.National) == 10 {
		area, _ = nanpAreaOf(n.National[:3], Today())
		if n.Kind == "" && area.country != "" && area.service == "" {
			if t := byID[area.country]; t != nil {
				n.t, n.Kind = t, FixedLineOrMobile
			}
		}
	}
	if n.t.id != "001" {
		n.Country = n.t.id
	}
	switch {
	case nonGeographic[n.Code] != "":
		n.Service = nonGeographic[n.Code]
	case n.Kind != "" && !n.Kind.placed():
		n.Service = string(n.Kind)
	case n.Kind == "" && area.service != "":
		n.Service = area.service
	}
	if n.Country != "" && n.Service == "" {
		r, l := placeOf(d)
		if n.Code == "1" && l <= 4 && area.country == n.Country {
			r = area.region // the area's, as NANPA has it now
		}
		if r != n.CountryName() {
			n.Region = r
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
// States", "Berlin, Germany", "United States (toll-free)", "Inmarsat".
func (n Number) Where() string {
	switch {
	case n.Country != "" && n.Region != "":
		return n.Region + ", " + n.CountryName()
	case n.Country != "" && n.Service != "":
		return n.CountryName() + " (" + n.Service + ")"
	case n.Country != "":
		return n.CountryName()
	case n.Service != "":
		return n.Service
	}
	return ""
}

// String is the number as it is written abroad: "+1 415-555-0123", "+44
// 20 7123 4567"; a local number as it is.
func (n Number) String() string {
	switch {
	case n.E164 == "":
		if n.Code == "1" && len(n.National) == 7 {
			return n.National[:3] + "-" + n.National[3:]
		}
		return n.National
	case n.t != nil:
		f := n.t.formatIntl(n.National)
		if f == "" && len(n.t.formats) == 0 {
			// a country without formats of its own writes them as the
			// code's own country does
			if ts := byCode[n.Code]; len(ts) > 0 {
				f = ts[0].formatIntl(n.National)
			}
		}
		if f != "" {
			return "+" + n.Code + " " + f
		}
	}
	return "+" + n.Code + " " + n.National
}
