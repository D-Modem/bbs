package server

import (
	"strings"
	"testing"
	"time"
)

func TestFeedSections(t *testing.T) {
	c, err := Parse([]byte(`[listen telnet]
type = telnet
address = :2300
[feeds]
contact = sysop@example.org
catalog = government nasa
every = 45
[feed nasa]
area = Space
[feed house-floor]
enabled = no
[feed lobsters]
url = https://lobste.rs/rss
title = Lobsters
group = Technology
every = 60
[weather]
quake_radius = 500
quake_min = 3.5
international = no
[quotes]
default = spy, brk.b
listen = 233.215.21.4:10378
delay = 15 minutes
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Feeds == nil || !strings.Contains(c.Feeds.UserAgent, "sysop@example.org") {
		t.Fatalf("feeds %+v", c.Feeds)
	}
	by := map[string]int{}
	for i, s := range c.Feeds.Sources {
		by[s.Name] = i
	}
	if _, ok := by["house-floor"]; ok {
		t.Error("a feed turned off is kept")
	}
	nasa := c.Feeds.Sources[by["nasa"]]
	if nasa.Area != "Space" || nasa.Every != 45*time.Minute {
		t.Errorf("nasa %+v", nasa)
	}
	if fr := c.Feeds.Sources[by["fedreg"]]; fr.Every != 45*time.Minute || fr.Group != "Government" {
		t.Errorf("fedreg %+v", fr)
	}
	if cb := c.Feeds.Sources[by["congress-bills"]]; cb.Every != 6*time.Hour {
		t.Errorf("a catalogue feed's own interval lost: %+v", cb)
	}
	lb := c.Feeds.Sources[by["lobsters"]]
	if lb.URL != "https://lobste.rs/rss" || lb.Area != "lobsters" || lb.Group != "Technology" || lb.Every != time.Hour || lb.KeepDays != 30 {
		t.Errorf("lobsters %+v", lb)
	}
	if w := c.Weather; w == nil || w.QuakeRadius != 500 || w.QuakeMin != 3.5 || w.International || !c.WeatherAtLogin {
		t.Errorf("weather %+v", c.Weather)
	}
	if q := c.Quotes; q == nil || strings.Join(q.Default, ",") != "SPY,BRK.B" || q.Listen != "233.215.21.4:10378" ||
		q.Delay != "15 minutes" || !strings.HasSuffix(q.File, "quotes.json.gz") {
		t.Errorf("quotes %+v", c.Quotes)
	}
	for _, w := range c.Warnings {
		if strings.Contains(w, "unknown option") || strings.Contains(w, "contact") {
			t.Errorf("warning: %s", w)
		}
	}

	for _, bad := range []string{
		"[feeds]\ncatalog = nosuch\n",
		"[feed x]\nurl = ftp://example.org/\n",
		"[weather]\nquake_min = big\n",
		"[quotes]\ndefault = not a symbol!\n",
		"[quotes]\nalpaca_key = PK1\n",
		"[quotes]\nalpaca_key = PK1\nalpaca_secret = s\nlisten = 233.215.21.4:10378\n",
		"[quotes]\nalpaca_key = PK1\nalpaca_secret = s\nalpaca_feed = nasdaq\n",
	} {
		if _, err := Parse([]byte("[listen telnet]\ntype = telnet\naddress = :2300\n" + bad)); err == nil {
			t.Errorf("taken: %q", bad)
		}
	}

	c, err = Parse([]byte("[listen telnet]\ntype = telnet\naddress = :2300\n[quotes]\nalpaca_key = PK1\nalpaca_secret = s\nalpaca_feed = delayed_sip\n"))
	if err != nil {
		t.Fatal(err)
	}
	if q := c.Quotes; q.AlpacaKey != "PK1" || q.AlpacaSecret != "s" || q.AlpacaSymbols != 30 || q.Delay != "15 minutes" {
		t.Errorf("Alpaca %+v", q)
	}
}
