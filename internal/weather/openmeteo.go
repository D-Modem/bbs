package weather

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"time"
)

// Open-Meteo (open-meteo.com): forecasts for anywhere, free with no key for
// non-commercial use; its data is CC BY 4.0, so it is credited.

func (s *Service) openMeteo(ctx context.Context, l Location, metric bool) (*Forecast, error) {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(l.Lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(l.Lon, 'f', 4, 64))
	q.Set("current", "temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m,wind_direction_10m")
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max,wind_speed_10m_max,wind_direction_10m_dominant")
	q.Set("timezone", "auto")
	q.Set("forecast_days", "7")
	tu, wu := "C", "km/h"
	if !metric {
		q.Set("temperature_unit", "fahrenheit")
		q.Set("wind_speed_unit", "mph")
		tu, wu = "F", "mph"
	}
	u := s.openMeteoURL() + "?" + q.Encode()
	v, err := s.remember("openmeteo "+u, 30*time.Minute, func() (any, error) {
		var r struct {
			Timezone string `json:"timezone"`
			Current  struct {
				Time     string   `json:"time"`
				Temp     *float64 `json:"temperature_2m"`
				Humidity *float64 `json:"relative_humidity_2m"`
				Code     *int     `json:"weather_code"`
				Wind     *float64 `json:"wind_speed_10m"`
				WindDir  *float64 `json:"wind_direction_10m"`
			} `json:"current"`
			Daily struct {
				Time    []string   `json:"time"`
				Code    []*int     `json:"weather_code"`
				Max     []*float64 `json:"temperature_2m_max"`
				Min     []*float64 `json:"temperature_2m_min"`
				Rain    []*float64 `json:"precipitation_probability_max"`
				Wind    []*float64 `json:"wind_speed_10m_max"`
				WindDir []*float64 `json:"wind_direction_10m_dominant"`
			} `json:"daily"`
		}
		if err := s.getJSON(ctx, u, &r); err != nil {
			return nil, err
		}
		f := &Forecast{Place: l.Label(), Source: "Open-Meteo.com (CC BY 4.0)", Updated: time.Now()}
		c := r.Current
		if c.Temp != nil {
			now := fmt.Sprintf("%.0f°%s", *c.Temp, tu)
			if c.Code != nil {
				now = WMO(*c.Code) + ", " + now
			}
			if c.Humidity != nil {
				now += fmt.Sprintf(", humidity %.0f%%", *c.Humidity)
			}
			if c.Wind != nil {
				now += ", wind " + compass(c.WindDir) + fmt.Sprintf("%.0f %s", *c.Wind, wu)
			}
			f.Now = now
		}
		d := r.Daily
		for i, day := range d.Time {
			t, err := time.Parse("2006-01-02", day)
			if err != nil {
				continue
			}
			p := Period{Name: t.Format("Monday 2 Jan")}
			if i == 0 {
				p.Name = "Today"
			}
			if i < len(d.Code) && d.Code[i] != nil {
				p.Short = WMO(*d.Code[i])
			}
			if i < len(d.Max) && i < len(d.Min) && d.Max[i] != nil && d.Min[i] != nil {
				p.Temp = fmt.Sprintf("%.0f°/%.0f°%s", *d.Max[i], *d.Min[i], tu)
			}
			det := ""
			if i < len(d.Rain) && d.Rain[i] != nil {
				det = fmt.Sprintf("Chance of rain %.0f%%.", *d.Rain[i])
			}
			if i < len(d.Wind) && d.Wind[i] != nil {
				var dir *float64
				if i < len(d.WindDir) {
					dir = d.WindDir[i]
				}
				if det != "" {
					det += " "
				}
				det += fmt.Sprintf("Wind %sup to %.0f %s.", compass(dir), *d.Wind[i], wu)
			}
			p.Details = det
			f.Periods = append(f.Periods, p)
		}
		return f, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Forecast), nil
}

// compass is a wind's direction in words ("NW "), "" if not known.
func compass(deg *float64) string {
	if deg == nil {
		return ""
	}
	names := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE", "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	i := int(math.Round(math.Mod(*deg+360, 360)/22.5)) % 16
	return names[i] + " "
}

// WMO is a WMO weather code (as Open-Meteo gives them) in words.
func WMO(code int) string {
	switch code {
	case 0:
		return "Clear"
	case 1:
		return "Mostly clear"
	case 2:
		return "Partly cloudy"
	case 3:
		return "Overcast"
	case 45, 48:
		return "Fog"
	case 51, 53, 55:
		return "Drizzle"
	case 56, 57:
		return "Freezing drizzle"
	case 61:
		return "Light rain"
	case 63:
		return "Rain"
	case 65:
		return "Heavy rain"
	case 66, 67:
		return "Freezing rain"
	case 71:
		return "Light snow"
	case 73:
		return "Snow"
	case 75:
		return "Heavy snow"
	case 77:
		return "Snow grains"
	case 80, 81:
		return "Rain showers"
	case 82:
		return "Violent rain showers"
	case 85, 86:
		return "Snow showers"
	case 95:
		return "Thunderstorms"
	case 96, 99:
		return "Thunderstorms with hail"
	}
	return "Weather code " + strconv.Itoa(code)
}
