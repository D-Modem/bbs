package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	iex "github.com/D-Modem/bbs/internal/quotes"
	"github.com/D-Modem/bbs/internal/store"
	"github.com/D-Modem/bbs/internal/view"
	"github.com/D-Modem/bbs/internal/weather"
)

// The callers' settings for these screens.
func init() {
	bbs.RegisterPref("weather-at", weather.CheckPref) // where they are: "lat,lon;Name;CC"
	bbs.RegisterPref("weather-units", func(v string) bool { return v == "us" || v == "metric" })
	bbs.RegisterPref("quotes", func(v string) bool { // their symbols, "AAPL,MSFT"
		for _, s := range strings.Split(v, ",") {
			if iex.Normalize(s) != s {
				return false
			}
		}
		return true
	})
}

// hasNews: there are feeds, weather or quotes.
func (s *Session) hasNews() bool {
	return len(s.B.FeedAreas) > 0 || s.B.Weather != nil || s.B.Quotes != nil
}

// newsLabel is the news menu's name: what it has.
func (s *Session) newsLabel() string {
	var has []string
	if len(s.B.FeedAreas) > 0 {
		has = append(has, "News")
	}
	if s.B.Weather != nil {
		has = append(has, "weather")
	}
	if s.B.Quotes != nil {
		has = append(has, "quotes")
	}
	if len(has) == 0 {
		return "News"
	}
	l := strings.Join(has, ", ")
	return strings.ToUpper(l[:1]) + l[1:]
}

// newsMenu is the news, the weather and the quotes, as one menu (the
// way the online services had them).
func (s *Session) newsMenu() error {
	for {
		s.N.Doing("News")
		var items []view.Item
		if len(s.B.FeedAreas) > 0 {
			items = append(items, view.Item{Key: "H", Label: "Headlines"})
		}
		if s.B.Weather != nil {
			items = append(items, view.Item{Key: "W", Label: "Your weather"})
		}
		if s.B.Quotes != nil {
			items = append(items, view.Item{Key: "Q", Label: "Stock quotes"})
		}
		items = append(items, view.Item{Key: "B", Label: "Back to the main menu"})
		k, err := s.UI.Menu(&view.Menu{Title: s.newsLabel(), Items: items, Brief: s.expert()})
		if err != nil {
			return err
		}
		switch strings.ToUpper(k) {
		case "H":
			err = s.headlines()
		case "W":
			err = s.weather()
		case "Q":
			err = s.stockQuotes()
		case "B", view.Back:
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// headlines are the news areas, each with its newest story; one chosen is
// read as any area is.
func (s *Session) headlines() error {
	if len(s.B.FeedAreas) == 0 {
		s.UI.Say(view.Plain, "There are no news feeds here.")
		return nil
	}
	for {
		s.N.Doing("Headlines")
		var areas []*store.Area
		for _, name := range s.B.FeedAreas {
			if a, err := s.B.Store.AreaByName(name); err == nil && s.B.CanRead(s.User, a) {
				areas = append(areas, a)
			}
		}
		t := &view.Table{Title: "Headlines", Empty: "(no news yet)", Pick: "Number to read",
			Columns: []view.Column{{Title: "News", Width: 22}, {Title: "Stories", Width: 7, Right: true, Wide: true}, {Title: "The newest"}}}
		for i, a := range areas {
			newest := ""
			if a.LastID > 0 {
				if m, err := s.B.Store.Message(a.LastID); err == nil {
					newest = m.Subject
				}
			}
			t.Rows = append(t.Rows, view.Row{Num: fmt.Sprint(i + 1), Cells: []string{a.Name, fmt.Sprint(a.Messages), newest}})
		}
		c, err := s.UI.Table(t)
		if err != nil || c.Action != view.Pick {
			return err
		}
		if c.Row < 0 || c.Row >= len(areas) {
			s.UI.Say(view.Plain, "No such news.")
			continue
		}
		if err := s.area(areas[c.Row]); err != nil {
			return err
		}
	}
}

// place is where the caller said they are (ok false: they have not).
func (s *Session) place() (weather.Location, bool) {
	return weather.FromPref(s.N.Prefs()["weather-at"])
}

// metric: the caller's units, or those of where they are.
func (s *Session) metric(l weather.Location) bool {
	switch s.N.Prefs()["weather-units"] {
	case "metric":
		return true
	case "us":
		return false
	}
	return l.Country != "" && l.Country != "US" && l.Country != "LR" && l.Country != "MM"
}

// setPlace asks where the caller is and keeps it; false if they gave up.
func (s *Session) setPlace() (bool, error) {
	for {
		v, ok, err := s.UI.Form(&view.Form{Title: "Your weather", Submit: "Find",
			Intro: []view.Line{view.L("Where are you?  A US ZIP code, a town (\"Springfield, IL\", \"Leeds, UK\"),"),
				view.L("or a latitude and longitude (\"51.48,-0.01\").")},
			Fields: []view.Field{{Name: "where", Label: "Place", Prompt: "Place: ", Width: 40}}})
		if err != nil || !ok || strings.TrimSpace(v["where"]) == "" {
			return false, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		ls, err := s.B.Weather.Find(ctx, v["where"])
		cancel()
		if errors.Is(err, weather.ErrNoPlace) {
			s.UI.Say(view.Bad, "No place of that name was found.")
			continue
		}
		if err != nil {
			s.UI.Say(view.Bad, "The places could not be looked up just now.")
			return false, nil
		}
		l := ls[0]
		if len(ls) > 1 {
			t := &view.Table{Title: "Which place?", Pick: "Number",
				Columns: []view.Column{{Title: "Place"}, {Title: "Where", Width: 20, Wide: true}}}
			for i, l := range ls {
				t.Rows = append(t.Rows, view.Row{Num: fmt.Sprint(i + 1), Cells: []string{l.Name, l.Point()}})
			}
			c, err := s.UI.Table(t)
			if err != nil || c.Action != view.Pick {
				return false, err
			}
			if c.Row < 0 || c.Row >= len(ls) {
				continue
			}
			l = ls[c.Row]
		}
		if err := s.N.SetPref("weather-at", l.Pref()); err != nil {
			s.UI.Say(view.Bad, "That place could not be kept: "+err.Error())
			return false, nil
		}
		s.UI.Say(view.Good, "Your weather is for "+l.Label()+".")
		return true, nil
	}
}

// weather is the caller's weather: the warnings, the forecast and the
// earthquakes near.
func (s *Session) weather() error {
	if s.B.Weather == nil {
		s.UI.Say(view.Plain, "There is no weather here.")
		return nil
	}
	for {
		s.N.Doing("Weather")
		l, ok := s.place()
		if !ok {
			if ok, err := s.setPlace(); err != nil || !ok {
				return err
			}
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		r := s.B.Weather.Report(ctx, l, s.metric(l))
		cancel()
		if err := s.UI.Show("Weather for "+l.Label(), WeatherLines(r, s.UI.Narrow())); err != nil {
			return err
		}
		units := "US (°F, mph)"
		if s.metric(l) {
			units = "metric (°C, km/h)"
		}
		act, err := s.UI.Panel(&view.Panel{Title: "Your weather",
			Lines: []view.Line{view.V("Place:", l.Label()), view.V("Units:", units)},
			Actions: []view.Action{{ID: "again", Label: "Again", Key: 'A', PF: 5},
				{ID: "place", Label: "Change place", Key: 'P', PF: 4, Short: "Place"},
				{ID: "units", Label: "Units", Key: 'U', PF: 6}}})
		if err != nil {
			return err
		}
		switch act {
		case "again":
		case "place":
			if _, err := s.setPlace(); err != nil {
				return err
			}
		case "units":
			u := "metric"
			if s.metric(l) {
				u = "us"
			}
			if err := s.N.SetPref("weather-units", u); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

// WeatherLines are a report as lines to read: warnings first, then the
// forecast, the earthquakes near, and the services' credits.
func WeatherLines(r *weather.Report, narrow bool) []view.Line {
	width := 76
	if narrow {
		width = 38
	}
	var out []view.Line
	wrap := func(tone view.Tone, indent, s string) {
		for _, l := range wrapText(s, width-len(indent)) {
			out = append(out, view.T(tone, indent+l))
		}
	}
	for _, a := range r.Alerts {
		tone := view.Warn
		if a.Serious() {
			tone = view.Bad
		}
		head := a.Headline
		if head == "" {
			head = a.Event
		}
		wrap(tone, "", "* "+head)
		if a.Description != "" {
			wrap(view.Plain, "  ", firstParagraph(a.Description, 600))
		}
		if a.Instruction != "" {
			wrap(view.Dim, "  ", firstParagraph(a.Instruction, 400))
		}
		out = append(out, view.L(""))
	}
	if f := r.Forecast; f != nil {
		if f.Now != "" {
			out = append(out, view.V("Now:", f.Now), view.L(""))
		}
		for i, p := range f.Periods {
			if i >= 10 {
				break
			}
			head := p.Name + ": " + p.Short
			if p.Temp != "" {
				head += ", " + p.Temp
			}
			wrap(view.Bright, "", head)
			if p.Details != "" && p.Details != p.Short {
				wrap(view.Plain, "  ", p.Details)
			}
		}
		out = append(out, view.L(""))
	}
	if len(r.Quakes) > 0 {
		out = append(out, view.T(view.Bright, "Earthquakes near you, the past week:"))
		for i, q := range r.Quakes {
			if i == 10 {
				out = append(out, view.T(view.Dim, fmt.Sprintf("  ... and %d more.", len(r.Quakes)-10)))
				break
			}
			line := fmt.Sprintf("M%.1f  %s  %s (%.0f km away)", q.Mag, q.Time.UTC().Format("Mon 2 Jan 15:04Z"), q.Place, q.Distance)
			tone := view.Plain
			if q.Alert != "" && q.Alert != "green" || q.Tsunami {
				tone = view.Warn
				if q.Tsunami {
					line += ", tsunami: see tsunami.gov"
				}
			}
			wrap(tone, "  ", line)
		}
		out = append(out, view.L(""))
	}
	for _, p := range r.Problems {
		wrap(view.Dim, "", p)
	}
	var credits []string
	if r.Forecast != nil {
		credits = append(credits, "Forecast: "+r.Forecast.Source)
	}
	if len(r.Alerts) > 0 {
		credits = append(credits, "Alerts: National Weather Service")
	}
	credits = append(credits, "Earthquakes: USGS")
	wrap(view.Dim, "", strings.Join(credits, ".  ")+".")
	return out
}

// firstParagraph is the first paragraph of s, at most max runes.
func firstParagraph(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r", ""))
	if i := strings.Index(s, "\n\n"); i > 0 {
		s = s[:i]
	}
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max]) + " ..."
	}
	return s
}

// wrapText is s in lines of width.
func wrapText(s string, width int) []string {
	if width < 10 {
		width = 10
	}
	return bbs.Wrap(s, width)
}

// loginWeather is a line for the welcome when a serious warning is in
// force where the caller is ("": none, or not asked for).
func (s *Session) loginWeather() []view.Line {
	if s.B.Weather == nil || !s.B.WeatherAtLogin {
		return nil
	}
	l, ok := s.place()
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	as, err := s.B.Weather.Alerts(ctx, l)
	if err != nil {
		return nil
	}
	var out []view.Line
	for _, a := range as {
		if a.Serious() {
			head := a.Headline
			if head == "" {
				head = a.Event
			}
			out = append(out, view.T(view.Bad, "Weather warning: "+bbs.Clean(head, 200)))
		}
	}
	if len(out) > 3 {
		out = append(out[:3], view.T(view.Bad, fmt.Sprintf("... and %d more.", len(out)-3)))
	}
	if len(out) > 0 {
		out = append(out, view.T(view.Dim, "The news menu's weather has the details."))
	}
	return out
}

// symbols are the caller's quotes' symbols.
func (s *Session) symbols() []string {
	if v := s.N.Prefs()["quotes"]; v != "" {
		return strings.Split(v, ",")
	}
	return append([]string(nil), s.B.QuoteDefault...)
}

func (s *Session) setSymbols(syms []string) error {
	if len(syms) == 0 {
		syms = []string{"SPY"}
	}
	return s.N.SetPref("quotes", strings.Join(syms, ","))
}

// money is a price as quotes show them.
func money(p float64) string {
	switch {
	case p == 0:
		return "-"
	case p < 1:
		return fmt.Sprintf("%.4f", p)
	}
	return fmt.Sprintf("%.2f", p)
}

// stockQuotes is the caller's list of quotes, to look at, add to and take
// from.
func (s *Session) stockQuotes() error {
	if s.B.Quotes == nil {
		s.UI.Say(view.Plain, "There are no stock quotes here.")
		return nil
	}
	for {
		s.N.Doing("Stock quotes")
		syms := s.symbols()
		s.B.Quotes.Want(syms...)
		snap, err := s.B.Quotes.Current()
		if err != nil {
			s.UI.Say(view.Plain, "There are no quotes yet: "+err.Error()+".")
			return nil
		}
		t := &view.Table{Title: "Stock quotes", Pick: "Number for more", Empty: "(no symbols: A adds one)",
			Note: QuoteNote(snap, s.B.Quotes.LiveDelay),
			Columns: []view.Column{{Title: "Symbol", Width: 8}, {Title: "Price", Width: 10, Right: true},
				{Title: "Change", Width: 9, Right: true}, {Title: "%", Width: 7, Right: true},
				{Title: "High", Width: 10, Right: true, Wide: true}, {Title: "Low", Width: 10, Right: true, Wide: true},
				{Title: volumeLabel(snap), Width: 11, Right: true, Wide: true}},
			Actions: []view.Action{{ID: "add", Label: "Add", Key: 'A', PF: 4}, {ID: "remove", Label: "Remove", Key: 'R', PF: 5},
				{ID: "look", Label: "Look up", Key: 'L', PF: 6}}}
		for i, sym := range syms {
			t.Rows = append(t.Rows, view.Row{Num: fmt.Sprint(i + 1), Cells: quoteCells(sym, snap.Quotes[sym])})
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Pick:
			if c.Row >= 0 && c.Row < len(syms) {
				if err := s.quote(snap, syms[c.Row]); err != nil {
					return err
				}
			}
		case "add", "look":
			sym, err := s.askSymbol(c.Action == "add")
			if err != nil || sym == "" {
				if err != nil {
					return err
				}
				continue
			}
			s.B.Quotes.Want(sym)
			if now, err := s.B.Quotes.Current(); err == nil {
				snap = now
			}
			if _, ok := snap.Quotes[sym]; !ok {
				s.UI.Say(view.Bad, "There are no trades or quotes of "+sym+" in these prices.")
				if c.Action == "look" {
					continue
				}
			}
			if c.Action == "look" {
				if err := s.quote(snap, sym); err != nil {
					return err
				}
				continue
			}
			for _, have := range syms {
				if have == sym {
					sym = ""
				}
			}
			if sym != "" && len(syms) < 40 {
				if err := s.setSymbols(append(syms, sym)); err != nil {
					s.UI.Say(view.Bad, err.Error())
				}
			}
		case "remove":
			sym, err := s.askSymbol(false)
			if err != nil {
				return err
			}
			var keep []string
			for _, have := range syms {
				if have != sym {
					keep = append(keep, have)
				}
			}
			if len(keep) < len(syms) {
				if err := s.setSymbols(keep); err != nil {
					s.UI.Say(view.Bad, err.Error())
				}
			}
		default:
			return nil
		}
	}
}

func (s *Session) askSymbol(adding bool) (string, error) {
	title := "Look up a symbol"
	if adding {
		title = "Add a symbol"
	}
	v, ok, err := s.UI.Form(&view.Form{Title: title, Submit: "OK",
		Fields: []view.Field{{Name: "sym", Label: "Symbol", Prompt: "Symbol (AAPL, BRK.B): ", Width: 8}}})
	if err != nil || !ok {
		return "", err
	}
	sym := iex.Normalize(v["sym"])
	if sym == "" && strings.TrimSpace(v["sym"]) != "" {
		s.UI.Say(view.Bad, "That is not a symbol.")
	}
	return sym, nil
}

// quoteCells are a quote's row.
func quoteCells(sym string, q *iex.Quote) []string {
	if q == nil {
		return []string{sym, "-", "", "", "", "", ""}
	}
	ch, pct := "", ""
	if d, p, ok := q.Change(); ok {
		ch, pct = fmt.Sprintf("%+.2f", d), fmt.Sprintf("%+.2f", p)
	}
	price := money(q.Price())
	if q.Halted {
		price = "halted"
	}
	return []string{sym, price, ch, pct, money(q.High), money(q.Low), fmt.Sprint(q.Volume)}
}

// QuoteNote is what a table of quotes says of them: when they are of, and
// whose.
func QuoteNote(snap *iex.Snapshot, delay string) []view.Line {
	var when string
	if strings.Contains(snap.Source, "live") {
		when = "The prices now"
		if delay != "" {
			when += ", " + delay + " behind"
		}
		if snap.Session != "" {
			when += " (" + snap.Session + ")"
		}
	} else {
		when = "The session of " + snap.SessionDate().Format("Monday 2 January 2006")
		if snap.Session != "closed" && snap.Session != "" {
			when += " (as far as it went: " + snap.Session + ")"
		}
	}
	scope, credit := "on IEX only", iex.Credit
	if snap.Scope != "" && snap.Scope != "IEX" {
		scope = "on " + snap.Scope
	}
	if snap.Credit != "" {
		credit = snap.Credit
	}
	return []view.Line{view.T(view.Dim, when+". Trades and quotes "+scope+"."), view.T(view.Dim, credit)}
}

// volumeLabel is what the volume is of: IEX's alone, or all the market's.
func volumeLabel(snap *iex.Snapshot) string {
	if snap.Scope == "" || snap.Scope == "IEX" {
		return "IEX volume"
	}
	return "Volume"
}

// quote is one symbol's quote in full.
func (s *Session) quote(snap *iex.Snapshot, sym string) error {
	q := snap.Quotes[sym]
	if q == nil {
		s.UI.Say(view.Plain, "There is nothing of "+sym+" in these prices.")
		return nil
	}
	lines := QuoteNote(snap, s.B.Quotes.LiveDelay)
	lines = append(lines, view.L(""), view.V("Price:", money(q.Price())))
	if d, p, ok := q.Change(); ok {
		lines = append(lines, view.V("Change:", fmt.Sprintf("%+.2f (%+.2f%%) from %s", d, p, money(q.PrevClose))))
	}
	if q.Last != 0 && !q.LastTime.IsZero() {
		lines = append(lines, view.V("Last trade:", fmt.Sprintf("%s, %d shares, %s ET", money(q.Last), q.LastSize,
			q.LastTime.In(easternTime()).Format("Mon 2 Jan 15:04:05"))))
	}
	lines = append(lines, view.V("Open:", money(q.Open)), view.V("High:", money(q.High)), view.V("Low:", money(q.Low)))
	if q.Close != 0 {
		lines = append(lines, view.V("Close:", money(q.Close)))
	}
	if q.Extended != 0 {
		lines = append(lines, view.V("Extended hours:", money(q.Extended)))
	}
	if q.Bid != 0 || q.Ask != 0 {
		lines = append(lines, view.V("Bid/ask:", fmt.Sprintf("%s x %d / %s x %d", money(q.Bid), q.BidSize, money(q.Ask), q.AskSize)))
	}
	lines = append(lines, view.V(volumeLabel(snap)+":", fmt.Sprintf("%d shares", q.Volume)))
	if q.Halted {
		lines = append(lines, view.T(view.Warn, "Trading is halted."))
	}
	_, err := s.UI.Panel(&view.Panel{Title: sym, Lines: lines})
	return err
}

func easternTime() *time.Location {
	if l, err := time.LoadLocation("America/New_York"); err == nil {
		return l
	}
	return time.FixedZone("ET", -5*3600)
}
