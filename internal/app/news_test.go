package app

import (
	"path/filepath"
	"strings"
	"testing"

	iex "github.com/D-Modem/bbs/internal/quotes"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/view"
	"github.com/D-Modem/bbs/internal/weather"
)

// TestNewsMenu: the headlines (each news area with its newest story), and
// the quotes: the default list, a symbol added and kept, one looked at.
func TestNewsMenu(t *testing.T) {
	b, alice, _ := setup(t)
	a, err := b.Store.CreateArea("World News", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Store.Post(&store.Message{AreaID: a.ID, From: "Example", To: "All", Subject: "Big story", Body: "It happened."}); err != nil {
		t.Fatal(err)
	}
	b.FeedAreas = []string{"World News", "Not Made Yet"}
	path := filepath.Join(t.TempDir(), "quotes.json.gz")
	snap := &iex.Snapshot{Date: "20261106", Source: "IEX HIST TOPS 1.6", Session: "closed", Quotes: map[string]*iex.Quote{
		"SPY":  {Symbol: "SPY", Close: 735.40, PrevClose: 730, High: 742.11, Low: 734.61, Volume: 1941930},
		"AAPL": {Symbol: "AAPL", Close: 342.41, Last: 342.41, High: 345.76, Low: 338.59, Volume: 1313258}}}
	if err := snap.Save(path); err != nil {
		t.Fatal(err)
	}
	b.Quotes = &iex.Board{Path: path}
	b.QuoteDefault = []string{"SPY"}
	f := run(t, b, alice, "N",
		"H", view.Choice{Action: view.Back},
		"Q", view.Choice{Action: "add"}, map[string]string{"sym": "aapl"},
		view.Choice{Action: view.Pick, Row: 1}, "",
		view.Choice{Action: view.Back}, "B")
	has(t, "screens", f.shown, "menu News, quotes")
	has(t, "screens", f.shown, "table Headlines: World News|1|Big story")
	has(t, "screens", f.shown, "table Stock quotes: SPY|735.40|+5.40|+0.74|742.11|734.61|1941930")
	has(t, "screens", f.shown, "table Stock quotes: SPY|735.40|+5.40|+0.74|742.11|734.61|1941930; AAPL|342.41||")
	has(t, "screens", f.shown, "panel AAPL: ")
	if got := alice.Prefs()["quotes"]; got != "SPY,AAPL" {
		t.Errorf("kept symbols %q", got)
	}
	for _, s := range f.shown {
		if strings.Contains(s, "Not Made Yet") {
			t.Error("an area not made is listed")
		}
	}
}

// TestWeatherLines: warnings first, the forecast, the earthquakes, the
// credits.
func TestWeatherLines(t *testing.T) {
	r := &weather.Report{
		Alerts:   []weather.Alert{{Event: "Tornado Warning", Headline: "Tornado Warning until 5:30 PM", Severity: "Extreme", Instruction: "Take shelter now."}},
		Forecast: &weather.Forecast{Source: "National Weather Service", Periods: []weather.Period{{Name: "Tonight", Temp: "46°F", Short: "Clear", Details: "A low around 46."}}},
		Quakes:   []weather.Quake{{Mag: 4.1, Place: "near here", Distance: 12}},
	}
	var text []string
	for _, l := range WeatherLines(r, false) {
		text = append(text, l.Label+l.Text)
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{"* Tornado Warning until 5:30 PM", "  Take shelter now.", "Tonight: Clear, 46°F", "  A low around 46.",
		"M4.1", "near here (12 km away)", "Forecast: National Weather Service.  Alerts: National Weather Service.", "Earthquakes: USGS."} {
		if !strings.Contains(all, want) {
			t.Errorf("no %q in\n%s", want, all)
		}
	}
	if strings.Index(all, "Tornado") > strings.Index(all, "Tonight") {
		t.Error("the warning is not first")
	}
}
