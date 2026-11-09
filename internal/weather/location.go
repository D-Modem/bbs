// Package weather is each caller's own weather: the forecast for where
// they are (the US National Weather Service's in the US, Open-Meteo's
// elsewhere), the weather warnings in force there (the NWS's), and the
// earthquakes near them (the US Geological Survey's, worldwide).
package weather

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"errors"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Location is a place on the earth.
type Location struct {
	Lat, Lon float64
	Name     string // "Washington, DC", "Paris, France" ("": none known)
	Country  string // ISO 3166 code, "" if not known
}

// Point is the location as "lat,lon", four places each (a few metres: as
// fine as the weather services take).
func (l Location) Point() string {
	return strconv.FormatFloat(l.Lat, 'f', 4, 64) + "," + strconv.FormatFloat(l.Lon, 'f', 4, 64)
}

// Label is the name, or the point.
func (l Location) Label() string {
	if l.Name != "" {
		return l.Name
	}
	return l.Point()
}

// ParsePoint reads "lat,lon".
func ParsePoint(s string) (Location, bool) {
	a, b, ok := strings.Cut(strings.TrimSpace(s), ",")
	if !ok {
		return Location{}, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(a), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if err1 != nil || err2 != nil || math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return Location{}, false
	}
	return Location{Lat: lat, Lon: lon}, true
}

// The US Census Bureau's ZIP Code Tabulation Areas (2025 Gazetteer, public
// domain): each ZIP code and the point inside it, "zip lat lon" a line.
//
//go:embed zcta.txt.gz
var zctaGz []byte

var (
	zipOnce sync.Once
	zips    map[string][2]float32
)

// ZIP is a US ZIP code's location (its tabulation area's inside point).
func ZIP(zip string) (Location, bool) {
	zipOnce.Do(func() {
		zips = map[string][2]float32{}
		zr, err := gzip.NewReader(bytes.NewReader(zctaGz))
		if err != nil {
			return
		}
		sc := bufio.NewScanner(zr)
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) != 3 {
				continue
			}
			lat, _ := strconv.ParseFloat(f[1], 32)
			lon, _ := strconv.ParseFloat(f[2], 32)
			zips[f[0]] = [2]float32{float32(lat), float32(lon)}
		}
	})
	p, ok := zips[zip]
	if !ok {
		return Location{}, false
	}
	return Location{Lat: float64(p[0]), Lon: float64(p[1]), Name: "ZIP " + zip, Country: "US"}, true
}

// isZIP: five digits (or ZIP+4).
func isZIP(s string) bool {
	if len(s) == 10 && s[5] == '-' {
		s = s[:5]
	}
	if len(s) != 5 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ErrNoPlace: nowhere was found of that name.
var ErrNoPlace = errors.New("no place of that name was found")

// Find is the places a caller's words may mean: "lat,lon", a US ZIP code,
// or a place's name, with its state, region or country after a comma
// ("Springfield, IL", "Paris, France"), looked up in Open-Meteo's
// gazetteer (GeoNames).  The likeliest come first; at most five.
func (s *Service) Find(ctx context.Context, q string) ([]Location, error) {
	q = strings.TrimSpace(q)
	if l, ok := ParsePoint(q); ok {
		return []Location{l}, nil
	}
	if isZIP(q) {
		if l, ok := ZIP(q[:5]); ok {
			// the town's name, where the NWS says it
			if p, err := s.points(ctx, l); err == nil && p.City != "" {
				l.Name = p.City + ", " + p.State + " " + q[:5]
			}
			return []Location{l}, nil
		}
		// a ZIP code with no tabulation area (a post office's, a
		// building's): the gazetteer may know it
		q = q[:5] + ", US"
	}
	name, within, _ := strings.Cut(q, ",")
	name, within = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(within))
	if len(name) < 2 {
		return nil, ErrNoPlace
	}
	var res struct {
		Results []struct {
			Name        string  `json:"name"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
			CountryCode string  `json:"country_code"`
			Country     string  `json:"country"`
			Admin1      string  `json:"admin1"`
			Population  int     `json:"population"`
		} `json:"results"`
	}
	u := s.geocodeURL() + "?count=20&language=en&format=json&name=" + url.QueryEscape(name)
	if err := s.getJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	var out []Location
	for _, r := range res.Results {
		admin := r.Admin1
		if r.CountryCode == "US" {
			if ab := usStates[strings.ToLower(admin)]; ab != "" {
				admin = ab
			}
		}
		if within != "" && !matches(within, r.CountryCode, r.Country, r.Admin1, admin) {
			continue
		}
		label := r.Name
		switch {
		case r.CountryCode == "US" && admin != "":
			label += ", " + admin
		case admin != "" && admin != r.Name:
			label += ", " + admin + ", " + r.Country
		default:
			label += ", " + r.Country
		}
		out = append(out, Location{Lat: r.Latitude, Lon: r.Longitude, Name: label, Country: r.CountryCode})
		if len(out) == 5 {
			break
		}
	}
	if len(out) == 0 {
		return nil, ErrNoPlace
	}
	return out, nil
}

// matches: the words after the comma are the place's country or region
// (its name, or its code: "FR", "IL", "UK" for GB).
func matches(within, code, country, admin1, abbrev string) bool {
	within = strings.TrimSuffix(within, ".")
	switch within {
	case strings.ToLower(code), strings.ToLower(country), strings.ToLower(admin1), strings.ToLower(abbrev):
		return true
	case "uk", "united kingdom", "england", "scotland", "wales":
		return code == "GB" || strings.EqualFold(admin1, within)
	case "usa", "us", "united states", "america":
		return code == "US"
	}
	return strings.HasPrefix(strings.ToLower(admin1), within) || strings.HasPrefix(strings.ToLower(country), within)
}

// distance is the great-circle distance between two places, in km.
func distance(a, b Location) float64 {
	const r = 6371.0
	la1, la2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dla, dlo := la2-la1, (b.Lon-a.Lon)*math.Pi/180
	h := math.Sin(dla/2)*math.Sin(dla/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dlo/2)*math.Sin(dlo/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(h)))
}

// usStates are the states' names and their postal codes.
var usStates = map[string]string{
	"alabama": "AL", "alaska": "AK", "arizona": "AZ", "arkansas": "AR", "california": "CA", "colorado": "CO",
	"connecticut": "CT", "delaware": "DE", "district of columbia": "DC", "washington, d.c.": "DC", "florida": "FL",
	"georgia": "GA", "hawaii": "HI", "idaho": "ID", "illinois": "IL", "indiana": "IN", "iowa": "IA", "kansas": "KS",
	"kentucky": "KY", "louisiana": "LA", "maine": "ME", "maryland": "MD", "massachusetts": "MA", "michigan": "MI",
	"minnesota": "MN", "mississippi": "MS", "missouri": "MO", "montana": "MT", "nebraska": "NE", "nevada": "NV",
	"new hampshire": "NH", "new jersey": "NJ", "new mexico": "NM", "new york": "NY", "north carolina": "NC",
	"north dakota": "ND", "ohio": "OH", "oklahoma": "OK", "oregon": "OR", "pennsylvania": "PA", "rhode island": "RI",
	"south carolina": "SC", "south dakota": "SD", "tennessee": "TN", "texas": "TX", "utah": "UT", "vermont": "VT",
	"virginia": "VA", "washington": "WA", "west virginia": "WV", "wisconsin": "WI", "wyoming": "WY",
	"puerto rico": "PR", "guam": "GU", "american samoa": "AS", "u.s. virgin islands": "VI", "northern mariana islands": "MP",
}

// Pref is a location as a user's setting keeps it: "lat,lon" then its name
// with spaces as underscores ("38.8894,-77.0352;Washington,_DC;US").
func (l Location) Pref() string {
	name := strings.NewReplacer(" ", "_", "=", "", ";", "").Replace(l.Name)
	return l.Point() + ";" + name + ";" + l.Country
}

// FromPref reads what Pref made; false if it is not that.
func FromPref(v string) (Location, bool) {
	parts := strings.SplitN(v, ";", 3)
	l, ok := ParsePoint(parts[0])
	if !ok {
		return Location{}, false
	}
	if len(parts) > 1 {
		l.Name = strings.ReplaceAll(parts[1], "_", " ")
	}
	if len(parts) > 2 {
		l.Country = parts[2]
	}
	return l, true
}

// CheckPref says whether v is a location setting.
func CheckPref(v string) bool { _, ok := FromPref(v); return ok }

// sortBy sorts places nearest a first.
func sortBy[T any](xs []T, d func(T) float64) {
	sort.SliceStable(xs, func(i, j int) bool { return d(xs[i]) < d(xs[j]) })
}
