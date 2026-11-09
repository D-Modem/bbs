package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Settings are [weather]'s.
type Settings struct {
	// UserAgent names the BBS to the services; the NWS asks for a way to
	// reach the sysop in it
	UserAgent string
	// QuakeRadius is how near an earthquake is shown (km), QuakeMin the
	// least magnitude
	QuakeRadius float64
	QuakeMin    float64
	// International: Open-Meteo for places outside the US (its terms:
	// non-commercial use, with credit)
	International bool
	// the services' addresses (tests point them elsewhere; "": the real ones)
	NWSURL, OpenMeteoURL, GeocodeURL, QuakesURL string
}

// Service answers for the callers, keeping what it fetched a while (the
// services ask that the same thing is not fetched again and again).
type Service struct {
	S      Settings
	Client *http.Client // nil: one with a 15 second timeout

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	v    any
	err  error
	when time.Time
}

// Forecast is the weather for a place.
type Forecast struct {
	Place   string
	Source  string // its credit: "National Weather Service", "Open-Meteo.com"
	Updated time.Time
	Now     string // the weather now ("": not told)
	Periods []Period
}

// Period is a part of the forecast: a day, a night.
type Period struct {
	Name    string // "Tonight", "Tuesday", "Tue 10 Nov"
	Temp    string // "46°F", "High 12°C, low 4°C"
	Short   string // "Mostly Clear"
	Details string // a sentence or two ("": none)
}

// Alert is a weather warning, watch or advisory in force.
type Alert struct {
	Event       string // "Tornado Warning"
	Headline    string
	Severity    string // Extreme, Severe, Moderate, Minor, Unknown
	Urgency     string
	Areas       string
	Description string
	Instruction string
	Sender      string
	Onset, Ends time.Time
}

// Serious: a warning to tell the caller at once (Severe or Extreme).
func (a Alert) Serious() bool { return a.Severity == "Severe" || a.Severity == "Extreme" }

// Report is everything for a place: the forecast, the alerts, the
// earthquakes near.  What could not be had is left out, with why in
// Problems.
type Report struct {
	Where    Location
	Forecast *Forecast
	Alerts   []Alert
	Quakes   []Quake
	Problems []string
}

// Report is the weather for l, fetched (or as kept) now.
func (s *Service) Report(ctx context.Context, l Location, metric bool) *Report {
	r := &Report{Where: l}
	var wg sync.WaitGroup
	var fErr, aErr, qErr error
	wg.Add(3)
	go func() { defer wg.Done(); r.Forecast, fErr = s.Forecast(ctx, l, metric) }()
	go func() { defer wg.Done(); r.Alerts, aErr = s.Alerts(ctx, l) }()
	go func() { defer wg.Done(); r.Quakes, qErr = s.Quakes(ctx, l) }()
	wg.Wait()
	for i, e := range []error{fErr, aErr, qErr} {
		if e != nil && !errors.Is(e, ErrNotCovered) {
			what := []string{"The forecast", "The weather alerts", "The earthquake list"}[i]
			r.Problems = append(r.Problems, what+" could not be had just now: "+short(e)+".")
		}
	}
	if errors.Is(fErr, ErrNotCovered) {
		r.Problems = append(r.Problems, "Forecasts here are for the United States only.")
	}
	return r
}

// ErrNotCovered: the place is outside what the services cover.
var ErrNotCovered = errors.New("not covered")

// Forecast is the forecast for l, in metric units (°C, km/h) or US ones:
// the NWS's in the US, Open-Meteo's elsewhere (where the sysop allows it).
func (s *Service) Forecast(ctx context.Context, l Location, metric bool) (*Forecast, error) {
	if l.Country == "" || l.Country == "US" || usTerritory(l.Country) {
		f, err := s.nwsForecast(ctx, l, metric)
		if err == nil || !s.S.International || l.Country == "US" {
			return f, err
		}
	}
	if !s.S.International {
		return nil, ErrNotCovered
	}
	return s.openMeteo(ctx, l, metric)
}

func usTerritory(c string) bool {
	switch c {
	case "PR", "GU", "AS", "VI", "MP":
		return true
	}
	return false
}

// remember is what get makes for key, kept for ttl (failures for a
// minute).
func (s *Service) remember(key string, ttl time.Duration, get func() (any, error)) (any, error) {
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[string]cached{}
	}
	c, ok := s.cache[key]
	s.mu.Unlock()
	keep := ttl
	if c.err != nil {
		keep = time.Minute
	}
	if ok && time.Since(c.when) < keep {
		return c.v, c.err
	}
	v, err := get()
	s.mu.Lock()
	s.cache[key] = cached{v, err, time.Now()}
	if len(s.cache) > 5000 {
		for k, c := range s.cache {
			if time.Since(c.when) > 6*time.Hour {
				delete(s.cache, k)
			}
		}
	}
	s.mu.Unlock()
	return v, err
}

// short is an error in a few words, for a caller.
func short(err error) string {
	var es errStatus
	switch {
	case errors.As(err, &es):
		return es.Error()
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "it took too long"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if u, perr := url.Parse(ue.URL); perr == nil {
			return u.Host + " did not answer"
		}
	}
	return "the answer could not be read"
}

// errStatus is a service's refusal.
type errStatus struct {
	code int
	url  string
}

func (e errStatus) Error() string {
	host := e.url
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	host, _, _ = strings.Cut(host, "/")
	return fmt.Sprintf("%s answered %d %s", host, e.code, http.StatusText(e.code))
}

// getJSON fetches u's JSON into v.
func (s *Service) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", s.S.UserAgent)
	req.Header.Set("Accept", "application/geo+json, application/json")
	cl := s.Client
	if cl == nil {
		cl = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return errStatus{resp.StatusCode, u}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(v)
}

func (s *Service) nwsURL() string {
	if s.S.NWSURL != "" {
		return strings.TrimSuffix(s.S.NWSURL, "/")
	}
	return "https://api.weather.gov"
}

func (s *Service) openMeteoURL() string {
	if s.S.OpenMeteoURL != "" {
		return s.S.OpenMeteoURL
	}
	return "https://api.open-meteo.com/v1/forecast"
}

func (s *Service) geocodeURL() string {
	if s.S.GeocodeURL != "" {
		return s.S.GeocodeURL
	}
	return "https://geocoding-api.open-meteo.com/v1/search"
}

func (s *Service) quakesURL() string {
	if s.S.QuakesURL != "" {
		return s.S.QuakesURL
	}
	return "https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/2.5_week.geojson"
}
