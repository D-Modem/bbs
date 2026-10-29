package bbs

import (
	"net"
	"strings"

	"github.com/D-Modem/bbs/internal/geoip"
	"github.com/D-Modem/bbs/internal/phone"
)

// Place is where a caller is, as far as is known: from the address, with
// a country database (geoip; a city database says the region and city
// too), else from a modem caller's number.
type Place struct {
	Country string // ISO 3166-1 ("": not known)
	Name    string // the country's name
	Region  string // the place in the country, if known: "California", or a caller ID's "Jersey City, NJ"
	City    string // the city, if a city database says it
	Number  string // the caller's number as it is written abroad, if read
	Service string // what a number that is no place's is ("toll-free")
	Kind    string // what the number is ("mobile", "fixed line"), if read
}

// String is the place in a few words: "Jersey City, NJ, United States",
// "Mountain View, California, United States", "Germany", "United States
// (toll-free)"; "" if nothing is known.
func (p Place) String() string {
	switch {
	case p.Name != "" && (p.Region != "" || p.City != ""):
		var parts []string
		for _, s := range []string{p.City, p.Region, p.Name} {
			// Tokyo, Tokyo: once
			if s != "" && (len(parts) == 0 || parts[len(parts)-1] != s) {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case p.Name != "" && p.Service != "":
		return p.Name + " (" + p.Service + ")"
	case p.Name != "":
		return p.Name
	}
	return p.Service
}

// Flag is the country's flag as an emoji ("": none).
func (p Place) Flag() string { return geoip.Flag(p.Country) }

// Locate is where a caller is: by their number if a modem's caller ID
// says it (the address is then the modem server's), else by their
// address in the country database.
func (b *BBS) Locate(c *Caller) Place {
	if c == nil {
		return Place{}
	}
	if c.CallerID != "" {
		if n, err := phone.Parse(c.CallerID, b.Conf().PhoneCountry); err == nil {
			return Place{Country: n.Country, Name: n.CountryName(), Region: n.Region, Number: n.String(),
				Service: n.Service, Kind: string(n.Kind)}
		}
	}
	if ip, ok := strings.CutPrefix(c.Source, "ip:"); ok {
		if l, ok := b.Geo().Lookup(net.ParseIP(ip)); ok {
			return Place{Country: l.Code, Name: l.Name, Region: l.Region, City: l.City}
		}
	}
	return Place{}
}

// Geo is the country (or city) database, kept up to date (nil: none).
// Others with the same file (the image boards) share it.
func (b *BBS) Geo() *geoip.File {
	path := b.Conf().GeoIP
	if path == "" {
		return nil
	}
	b.geoMu.Lock()
	defer b.geoMu.Unlock()
	if b.geo == nil || b.geo.Path != path {
		// the first time, or the setting changed while the BBS ran
		b.geo = &geoip.File{Path: path, Log: func(err error) { b.Log.Printf("the country database: %v", err) }}
	}
	return b.geo
}

// Place is where the node's caller is (found once, when first asked).
func (n *Node) Place() Place {
	if p := n.place.Load(); p != nil {
		return *p
	}
	p := n.b.Locate(n.Caller)
	n.place.Store(&p)
	return p
}
