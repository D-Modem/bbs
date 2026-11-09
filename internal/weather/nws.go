package weather

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The National Weather Service's API (api.weather.gov): public domain,
// free, no key; it asks for a User-Agent that says who is asking.

// nwsPoint is what the NWS says of a point: its forecast's address, the
// nearest town.
type nwsPoint struct {
	Forecast    string
	City, State string
}

func (s *Service) points(ctx context.Context, l Location) (*nwsPoint, error) {
	v, err := s.remember("points "+l.Point(), 24*time.Hour, func() (any, error) {
		var r struct {
			Properties struct {
				Forecast         string `json:"forecast"`
				RelativeLocation struct {
					Properties struct {
						City  string `json:"city"`
						State string `json:"state"`
					} `json:"properties"`
				} `json:"relativeLocation"`
			} `json:"properties"`
		}
		err := s.getJSON(ctx, s.nwsURL()+"/points/"+l.Point(), &r)
		var es errStatus
		if errors.As(err, &es) && es.code == 404 {
			return nil, ErrNotCovered // outside the US
		}
		if err != nil {
			return nil, err
		}
		p := r.Properties
		if p.Forecast == "" {
			return nil, ErrNotCovered
		}
		return &nwsPoint{Forecast: p.Forecast, City: p.RelativeLocation.Properties.City, State: p.RelativeLocation.Properties.State}, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*nwsPoint), nil
}

func (s *Service) nwsForecast(ctx context.Context, l Location, metric bool) (*Forecast, error) {
	p, err := s.points(ctx, l)
	if err != nil {
		return nil, err
	}
	u := p.Forecast
	if metric {
		u += "?units=si"
	}
	v, err := s.remember("forecast "+u, 30*time.Minute, func() (any, error) {
		var r struct {
			Properties struct {
				Updated string `json:"updateTime"`
				Periods []struct {
					Name     string `json:"name"`
					Temp     *int   `json:"temperature"`
					Unit     string `json:"temperatureUnit"`
					Wind     string `json:"windSpeed"`
					WindDir  string `json:"windDirection"`
					Short    string `json:"shortForecast"`
					Detailed string `json:"detailedForecast"`
				} `json:"periods"`
			} `json:"properties"`
		}
		if err := s.getJSON(ctx, u, &r); err != nil {
			return nil, err
		}
		f := &Forecast{Place: l.Label(), Source: "National Weather Service (weather.gov)"}
		if p.City != "" {
			f.Place = p.City + ", " + p.State
		}
		f.Updated, _ = time.Parse(time.RFC3339, r.Properties.Updated)
		for _, pd := range r.Properties.Periods {
			per := Period{Name: pd.Name, Short: pd.Short, Details: pd.Detailed}
			if pd.Temp != nil {
				per.Temp = fmt.Sprintf("%d°%s", *pd.Temp, pd.Unit)
			}
			if per.Details == "" && pd.Wind != "" {
				per.Details = strings.TrimSpace("Wind " + pd.WindDir + " " + pd.Wind + ".")
			}
			f.Periods = append(f.Periods, per)
		}
		return f, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Forecast), nil
}

// Alerts are the NWS's alerts in force at l (none outside the US), most
// serious first.
func (s *Service) Alerts(ctx context.Context, l Location) ([]Alert, error) {
	if l.Country != "" && l.Country != "US" && !usTerritory(l.Country) {
		return nil, nil
	}
	v, err := s.remember("alerts "+l.Point(), 5*time.Minute, func() (any, error) {
		var r struct {
			Features []struct {
				Properties struct {
					Event       string `json:"event"`
					Headline    string `json:"headline"`
					Severity    string `json:"severity"`
					Urgency     string `json:"urgency"`
					AreaDesc    string `json:"areaDesc"`
					Description string `json:"description"`
					Instruction string `json:"instruction"`
					Sender      string `json:"senderName"`
					Onset       string `json:"onset"`
					Effective   string `json:"effective"`
					Ends        string `json:"ends"`
					Expires     string `json:"expires"`
					Status      string `json:"status"`
				} `json:"properties"`
			} `json:"features"`
		}
		err := s.getJSON(ctx, s.nwsURL()+"/alerts/active?status=actual&point="+l.Point(), &r)
		var es errStatus
		if errors.As(err, &es) && (es.code == 400 || es.code == 404) {
			return []Alert(nil), nil // a point the NWS does not cover
		}
		if err != nil {
			return nil, err
		}
		var out []Alert
		for _, f := range r.Features {
			p := f.Properties
			if p.Status != "" && p.Status != "Actual" {
				continue
			}
			a := Alert{Event: p.Event, Headline: p.Headline, Severity: p.Severity, Urgency: p.Urgency,
				Areas: p.AreaDesc, Description: strings.TrimSpace(p.Description), Instruction: strings.TrimSpace(p.Instruction), Sender: p.Sender}
			a.Onset = parseTime(p.Onset, p.Effective)
			a.Ends = parseTime(p.Ends, p.Expires)
			out = append(out, a)
		}
		sortBy(out, func(a Alert) float64 { return float64(severityRank(a.Severity)) })
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]Alert), nil
}

func severityRank(s string) int {
	switch s {
	case "Extreme":
		return 0
	case "Severe":
		return 1
	case "Moderate":
		return 2
	case "Minor":
		return 3
	}
	return 4
}

func parseTime(vs ...string) time.Time {
	for _, v := range vs {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	return time.Time{}
}
