package weather

import (
	"context"
	"time"
)

// The US Geological Survey's earthquake feeds: worldwide, public domain.

// Quake is an earthquake.
type Quake struct {
	Mag      float64
	Place    string // "48 km N of Petersville, Alaska"
	Time     time.Time
	URL      string
	Alert    string  // the USGS PAGER level: green, yellow, orange, red ("": none)
	Tsunami  bool    // the tsunami centre may have said something
	Distance float64 // from the caller, km
	Depth    float64 // km

	lat, lon float64
}

// quakes are the past week's earthquakes of 2.5 and more, fetched at most
// every ten minutes and shared by all.
func (s *Service) quakes(ctx context.Context) ([]Quake, error) {
	v, err := s.remember("quakes", 10*time.Minute, func() (any, error) {
		var r struct {
			Features []struct {
				Properties struct {
					Mag     *float64 `json:"mag"`
					Place   string   `json:"place"`
					Time    int64    `json:"time"`
					URL     string   `json:"url"`
					Alert   *string  `json:"alert"`
					Tsunami int      `json:"tsunami"`
					Type    string   `json:"type"`
				} `json:"properties"`
				Geometry struct {
					Coordinates []float64 `json:"coordinates"`
				} `json:"geometry"`
			} `json:"features"`
		}
		if err := s.getJSON(ctx, s.quakesURL(), &r); err != nil {
			return nil, err
		}
		var out []Quake
		for _, f := range r.Features {
			p, c := f.Properties, f.Geometry.Coordinates
			if p.Mag == nil || len(c) < 2 || (p.Type != "" && p.Type != "earthquake") {
				continue
			}
			q := Quake{Mag: *p.Mag, Place: p.Place, Time: time.UnixMilli(p.Time), URL: p.URL,
				Tsunami: p.Tsunami != 0, lat: c[1], lon: c[0]}
			if p.Alert != nil {
				q.Alert = *p.Alert
			}
			if len(c) > 2 {
				q.Depth = c[2]
			}
			out = append(out, q)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]Quake), nil
}

// Quakes are the earthquakes of the past week near l (within the radius
// the sysop set, of the least magnitude; great ones from further), newest
// first.
func (s *Service) Quakes(ctx context.Context, l Location) ([]Quake, error) {
	all, err := s.quakes(ctx)
	if err != nil {
		return nil, err
	}
	radius, least := s.S.QuakeRadius, s.S.QuakeMin
	if radius <= 0 {
		radius = 300
	}
	var out []Quake
	for _, q := range all {
		if q.Mag < least {
			continue
		}
		d := distance(l, Location{Lat: q.lat, Lon: q.lon})
		// a great one is news further away
		if d > radius && !(q.Mag >= 6 && d < radius*4) {
			continue
		}
		q.Distance = d
		out = append(out, q)
	}
	sortBy(out, func(q Quake) float64 { return -float64(q.Time.Unix()) })
	if len(out) > 20 {
		out = out[:20]
	}
	return out, nil
}
