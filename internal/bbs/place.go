package bbs

import (
	"net"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/geoip"
	"github.com/D-Modem/bbs/internal/phone"
	"github.com/D-Modem/bbs/internal/store"
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
	By      string // what said so: "caller ID", "address", "VoIP address" ("": nothing)
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
// address in the country database, else (a modem call by VoIP without
// a caller ID) by the address its call came from.
func (b *BBS) Locate(c *Caller) Place {
	if c == nil {
		return Place{}
	}
	if c.CallerID != "" {
		if n, err := phone.Parse(c.CallerID, b.Conf().PhoneCountry); err == nil {
			return Place{Country: n.Country, Name: n.CountryName(), Region: n.Region, Number: n.String(),
				Service: n.Service, Kind: string(n.Kind), By: "caller ID"}
		}
	}
	if ip, ok := strings.CutPrefix(c.Source, "ip:"); ok {
		if l, ok := b.Geo().Lookup(net.ParseIP(ip)); ok {
			return Place{Country: l.Code, Name: l.Name, Region: l.Region, City: l.City, By: "address"}
		}
	}
	if ip := peerIP(c.Peer); ip != nil {
		if l, ok := b.Geo().Lookup(ip); ok {
			return Place{Country: l.Code, Name: l.Name, Region: l.Region, City: l.City, By: "VoIP address"}
		}
	}
	return Place{}
}

// peerIP is the address in a modem server's word for where a call came
// from: "203.0.113.5", "203.0.113.5:5060", "[2001:db8::1]:5060",
// "sip:203.0.113.5:5060"; nil if there is none.
func peerIP(s string) net.IP {
	s = strings.TrimSpace(s)
	for _, p := range []string{"sips:", "sip:", "udp:", "tcp:", "tls:", "ws:", "wss:"} {
		if len(s) > len(p) && strings.EqualFold(s[:len(p)], p) {
			s = s[len(p):]
		}
	}
	if _, after, ok := strings.Cut(s, "@"); ok {
		s = after
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		return net.ParseIP(h)
	}
	return net.ParseIP(strings.Trim(s, "[]"))
}

// LocationGuess is where the node's caller seems to be, in words, to
// start a new user's "Where are you calling from?" with ("": not known,
// or [general] geoip_prefill is off).
func (n *Node) LocationGuess() string {
	if !n.b.Conf().GeoPrefill {
		return ""
	}
	p := n.Place()
	if p.Region == "" && p.City == "" && p.Service != "" {
		return "" // a toll-free number is no place
	}
	return Clean(p.String(), MaxLocationLen)
}

// GeoOffered says whether callers are asked to let the sysops see where
// they call from: [general] geoip_users, with a GeoIP database or a
// caller ID country to find it by.
func (b *BBS) GeoOffered() bool {
	c := b.Conf()
	return c.GeoUsers && (c.GeoIP != "" || c.PhoneCountry != "")
}

// SetGeoShare lets the sysops see where the node's user calls from (kept
// now, and at each login), or stops it and forgets what was kept.
func (n *Node) SetGeoShare(share bool) error {
	u := n.User()
	if u == nil {
		return nil
	}
	if err := n.b.Store.SetGeoShare(u.ID, share); err != nil {
		return err
	}
	n.b.Log.Printf("node %d: %s %s where they call from", n.Num, u.Name, map[bool]string{true: "shares", false: "no longer shares"}[share])
	if share {
		return n.keepGeo(u.ID)
	}
	return nil
}

// keepGeo keeps where the node's caller is as user id's place, if they
// share it and it is known.
func (n *Node) keepGeo(id int64) error {
	p := n.Place()
	if p.Country == "" {
		return nil
	}
	return n.b.Store.SetGeo(id, store.Geo{Country: p.Country, Place: p.String(), By: p.By, At: time.Now()})
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

// GeoWords is where a user was last found to call from, if they share it,
// in words: "London, England, United Kingdom (by address, 2026-11-09)",
// "not shared".
func GeoWords(u *store.User) string {
	switch {
	case !u.GeoShare:
		return "not shared"
	case u.Geo.Place == "":
		return "shared (not known yet)"
	}
	w := u.Geo.Place
	if u.Geo.By != "" || !u.Geo.At.IsZero() {
		var how []string
		if u.Geo.By != "" {
			how = append(how, "by "+u.Geo.By)
		}
		if !u.Geo.At.IsZero() {
			how = append(how, u.Geo.At.Format("2006-01-02"))
		}
		w += " (" + strings.Join(how, ", ") + ")"
	}
	return w
}
