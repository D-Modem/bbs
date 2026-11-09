package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeServices answers as the NWS, Open-Meteo, its gazetteer and the USGS
// do, for Washington (in the US) and Paris (not).
func fakeServices(t *testing.T) (*Service, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.Contains(r.UserAgent(), "sysop@example.org") {
			t.Errorf("User-Agent %q", r.UserAgent())
		}
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/points/38.8951,-77.0364":
			w.Write([]byte(`{"properties":{"forecast":"` + "http://" + r.Host + `/gridpoints/LWX/97,71/forecast",
				"relativeLocation":{"properties":{"city":"Washington","state":"DC"}}}}`))
		case strings.HasPrefix(p, "/points/"):
			http.Error(w, `{"title":"Data Unavailable"}`, 404)
		case p == "/gridpoints/LWX/97,71/forecast":
			w.Write([]byte(`{"properties":{"updateTime":"2026-11-09T06:00:00Z","periods":[
				{"name":"Tonight","temperature":46,"temperatureUnit":"F","windSpeed":"8 mph","windDirection":"W",
				 "shortForecast":"Mostly Clear","detailedForecast":"Mostly clear, with a low around 46."},
				{"name":"Monday","temperature":63,"temperatureUnit":"F","shortForecast":"Sunny","detailedForecast":""}]}}`))
		case p == "/alerts/active":
			if r.URL.Query().Get("point") != "38.8951,-77.0364" {
				t.Errorf("alerts for %q", r.URL.RawQuery)
			}
			w.Write([]byte(`{"features":[
				{"properties":{"event":"Wind Advisory","headline":"Wind Advisory until 6 PM","severity":"Moderate","status":"Actual"}},
				{"properties":{"event":"Tornado Warning","headline":"Tornado Warning until 5:30 PM","severity":"Extreme","status":"Actual",
				 "description":"A tornado.\n\nMore.","instruction":"Take shelter now.","ends":"2026-11-09T22:30:00Z"}},
				{"properties":{"event":"Test Message","severity":"Severe","status":"Test"}}]}`))
		case p == "/forecast":
			if r.URL.Query().Get("temperature_unit") != "" {
				t.Errorf("metric asked, got %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"current":{"temperature_2m":5.7,"relative_humidity_2m":90,"weather_code":61,"wind_speed_10m":12,"wind_direction_10m":270},
				"daily":{"time":["2026-11-09","2026-11-10"],"weather_code":[61,0],"temperature_2m_max":[9.1,11],"temperature_2m_min":[3.2,4],
				"precipitation_probability_max":[80,5],"wind_speed_10m_max":[20,10],"wind_direction_10m_dominant":[250,90]}}`))
		case p == "/search":
			switch strings.ToLower(r.URL.Query().Get("name")) {
			case "paris":
				w.Write([]byte(`{"results":[
					{"name":"Paris","latitude":48.85341,"longitude":2.3488,"country_code":"FR","country":"France","admin1":"Île-de-France"},
					{"name":"Paris","latitude":33.66094,"longitude":-95.55551,"country_code":"US","country":"United States","admin1":"Texas"}]}`))
			case "20500":
				w.Write([]byte(`{"results":[{"name":"Washington","latitude":38.8951,"longitude":-77.0364,"country_code":"US","country":"United States","admin1":"District of Columbia"}]}`))
			default:
				w.Write([]byte(`{}`))
			}
		case p == "/quakes":
			w.Write([]byte(`{"features":[
				{"properties":{"mag":4.1,"place":"near Paris","time":1794200000000,"url":"https://e/1","type":"earthquake"},"geometry":{"coordinates":[2.5,48.9,10]}},
				{"properties":{"mag":3.0,"place":"far away","time":1794100000000,"type":"earthquake"},"geometry":{"coordinates":[140,35,10]}},
				{"properties":{"mag":7.1,"place":"great, further","time":1794300000000,"alert":"orange","tsunami":1,"type":"earthquake"},"geometry":{"coordinates":[12,45,10]}},
				{"properties":{"mag":3.5,"place":"a quarry","time":1794200000000,"type":"quarry blast"},"geometry":{"coordinates":[2.4,48.8,0]}}]}`))
		default:
			t.Errorf("asked for %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	s := &Service{S: Settings{UserAgent: "D-Modem-BBS/1.0 (sysop@example.org)", International: true, QuakeRadius: 300,
		NWSURL: srv.URL, OpenMeteoURL: srv.URL + "/forecast", GeocodeURL: srv.URL + "/search", QuakesURL: srv.URL + "/quakes"}}
	return s, &calls
}

func TestFind(t *testing.T) {
	s, _ := fakeServices(t)
	ctx := context.Background()
	ls, err := s.Find(ctx, "Paris, France")
	if err != nil || len(ls) != 1 || ls[0].Country != "FR" || ls[0].Name != "Paris, Île-de-France, France" {
		t.Errorf("Paris, France: %+v %v", ls, err)
	}
	ls, err = s.Find(ctx, "paris, tx")
	if err != nil || len(ls) != 1 || ls[0].Name != "Paris, TX" {
		t.Errorf("Paris, TX: %+v %v", ls, err)
	}
	if ls, err = s.Find(ctx, "Paris"); err != nil || len(ls) != 2 {
		t.Errorf("Paris: %+v %v", ls, err)
	}
	if _, err := s.Find(ctx, "Nowhere At All"); err != ErrNoPlace {
		t.Errorf("nowhere: %v", err)
	}
	if ls, err = s.Find(ctx, "51.4769, -0.0005"); err != nil || len(ls) != 1 || ls[0].Point() != "51.4769,-0.0005" {
		t.Errorf("a point: %+v %v", ls, err)
	}
	// a ZIP code in the Census table (named by the NWS where it can), and
	// one not in it, which the gazetteer is asked
	if ls, err = s.Find(ctx, "90210"); err != nil || len(ls) != 1 || ls[0].Country != "US" || ls[0].Lat < 34 || ls[0].Lat > 34.2 {
		t.Errorf("90210: %+v %v", ls, err)
	}
	if ls, err = s.Find(ctx, "20500"); err != nil || len(ls) != 1 || !strings.HasPrefix(ls[0].Name, "Washington") {
		t.Errorf("20500: %+v %v", ls, err)
	}
}

func TestPref(t *testing.T) {
	l := Location{Lat: 48.85341, Lon: 2.3488, Name: "Paris, Île-de-France, France", Country: "FR"}
	v := l.Pref()
	if strings.ContainsAny(v, " =") {
		t.Errorf("pref %q", v)
	}
	got, ok := FromPref(v)
	if !ok || got.Name != l.Name || got.Country != "FR" || got.Point() != "48.8534,2.3488" {
		t.Errorf("back: %+v %v", got, ok)
	}
	if CheckPref("nonsense") || !CheckPref("1,2") {
		t.Error("CheckPref")
	}
}

func TestReportUS(t *testing.T) {
	s, calls := fakeServices(t)
	ctx := context.Background()
	dc := Location{Lat: 38.8951, Lon: -77.0364, Country: "US"}
	r := s.Report(ctx, dc, false)
	if r.Forecast == nil || r.Forecast.Place != "Washington, DC" || len(r.Forecast.Periods) != 2 ||
		r.Forecast.Periods[0].Temp != "46°F" || r.Forecast.Periods[1].Details != "" {
		t.Fatalf("forecast %+v (problems %v)", r.Forecast, r.Problems)
	}
	if len(r.Alerts) != 2 || r.Alerts[0].Event != "Tornado Warning" || !r.Alerts[0].Serious() || r.Alerts[1].Serious() ||
		r.Alerts[0].Ends.IsZero() {
		t.Errorf("alerts %+v", r.Alerts)
	}
	if len(r.Problems) != 0 {
		t.Errorf("problems %v", r.Problems)
	}
	// asked again: as kept
	n := calls.Load()
	s.Report(ctx, dc, false)
	if calls.Load() != n {
		t.Errorf("%d more calls for the same report", calls.Load()-n)
	}
}

func TestReportElsewhere(t *testing.T) {
	s, _ := fakeServices(t)
	paris := Location{Lat: 48.8534, Lon: 2.3488, Name: "Paris, France", Country: "FR"}
	r := s.Report(context.Background(), paris, true)
	f := r.Forecast
	if f == nil || !strings.Contains(f.Source, "Open-Meteo") || f.Now != "Light rain, 6°C, humidity 90%, wind W 12 km/h" {
		t.Fatalf("forecast %+v (problems %v)", f, r.Problems)
	}
	if len(f.Periods) != 2 || f.Periods[0].Name != "Today" || f.Periods[0].Temp != "9°/3°C" || f.Periods[1].Short != "Clear" ||
		f.Periods[0].Details != "Chance of rain 80%. Wind WSW up to 20 km/h." {
		t.Errorf("periods %+v", f.Periods)
	}
	if len(r.Alerts) != 0 {
		t.Errorf("NWS alerts in France: %+v", r.Alerts)
	}
	// the one near, the great one further off; not the far one or the blast
	if len(r.Quakes) != 2 || r.Quakes[0].Place != "great, further" || !r.Quakes[0].Tsunami || r.Quakes[1].Distance > 20 {
		t.Errorf("quakes %+v", r.Quakes)
	}

	// without international forecasts: the US only
	s.S.International = false
	s.cache = nil
	r = s.Report(context.Background(), paris, true)
	if r.Forecast != nil || len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "United States only") {
		t.Errorf("not international: %+v %v", r.Forecast, r.Problems)
	}
}
