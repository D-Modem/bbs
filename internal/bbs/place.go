package bbs

import (
	"net"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/geoip"
	"github.com/Neo-Desktop/bbs-sec/internal/phone"
)

// Place is where a caller is, as far as is known: from the address, with
// a country database (geoip), else from a modem caller's number.
type Place struct {
	Country string // ISO 3166-1 ("": not known)
	Name    string // the country's name
	Region  string // the place in the country, if known (a caller ID's): "Jersey City, NJ"
	Number  string // the caller's number as it is written abroad, if read
	Service string // what a number that is no place's is ("toll-free")
	Kind    string // what the number is ("mobile", "fixed line"), if read
}

// String is the place in a few words: "Jersey City, NJ, United States",
// "Germany", "United States (toll-free)"; "" if nothing is known.
func (p Place) String() string {
	switch {
	case p.Region != "" && p.Name != "":
		return p.Region + ", " + p.Name
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
		if n, err := phone.Parse(c.CallerID, b.PhoneCountry); err == nil {
			return Place{Country: n.Country, Name: n.CountryName(), Region: n.Region, Number: n.String(),
				Service: n.Service, Kind: string(n.Kind)}
		}
	}
	if ip, ok := strings.CutPrefix(c.Source, "ip:"); ok {
		code, name := b.Country(ip)
		return Place{Country: code, Name: name}
	}
	return Place{}
}

// Country is an address's country (code and name; "" if not known).
func (b *BBS) Country(addr string) (string, string) {
	if b.GeoIP == "" {
		return "", ""
	}
	b.geoOnce.Do(func() {
		b.geo = &geoip.File{Path: b.GeoIP, Log: func(err error) { b.Log.Printf("the country database: %v", err) }}
	})
	code, name, _ := b.geo.Country(net.ParseIP(addr))
	return code, name
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
