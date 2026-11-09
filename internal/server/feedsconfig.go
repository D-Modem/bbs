package server

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/feeds"
	"github.com/D-Modem/bbs/internal/quotes"
	"github.com/D-Modem/bbs/internal/weather"
)

// QuoteSettings are [quotes]': where the quotes come from.
type QuoteSettings struct {
	File      string // the snapshot bbs quotes fetch makes, and the BBS shows
	Listen    string // a live TOPS feed's UDP address ("": none)
	Interface string // the interface its multicast group is joined on
	Delay     string // how far behind the live feed is ("": real time)
	Default   []string

	// Alpaca's market data, live, instead of a TOPS feed (AlpacaKey "":
	// not)
	AlpacaKey, AlpacaSecret string
	AlpacaFeed              string // iex, sip or delayed_sip
	AlpacaSymbols           int    // the most on its stream at once
	UserAgent               string
}

// userAgent is how the BBS names itself to the sites it reads, with the
// sysop's contact as some ask.
func userAgent(contact string) string {
	if contact != "" {
		// a contact alone: the SEC refuses agents that carry a web address
		return "D-Modem-BBS/1.0 (" + contact + ")"
	}
	return "D-Modem-BBS/1.0 (+https://github.com/D-Modem/bbs)"
}

// feedSections reads [feeds], [feed NAME], [weather] and [quotes].
func (c *Config) feedSections(cf *config.File) error {
	fs := cf.Get("feeds", "")
	contact := fs.String("contact", "")
	if fs != nil || len(cf.All("feed")) > 0 {
		set := &feeds.Settings{UserAgent: userAgent(contact)}
		err := func() error {
			var err error
			if set.MaxItems, err = fs.Int("max_items", 50, 1, 1000); err != nil {
				return err
			}
			every, err := fs.Int("every", int(feeds.DefaultEvery/time.Minute), 5, 7*24*60)
			if err != nil {
				return err
			}
			keep, err := fs.Int("keep_days", feeds.DefaultKeep, 0, 3650)
			if err != nil {
				return err
			}
			if names := fs.String("catalog", ""); names != "" {
				cat, err := feeds.FromCatalog(names)
				if err != nil {
					return fmt.Errorf("catalog: %v", err)
				}
				// [feeds] every and keep_days are for the catalogue's feeds
				// that say none of their own
				for _, src := range cat {
					if src.Every == feeds.DefaultEvery {
						src.Every = time.Duration(every) * time.Minute
					}
					if src.KeepDays == feeds.DefaultKeep {
						src.KeepDays = keep
					}
				}
				set.Sources = cat
			}
			if contact == "" {
				c.Warnings = append(c.Warnings, "[feeds] contact: some sites (the SEC's) refuse readers that give no way to reach the sysop")
			}
			return nil
		}()
		if err != nil {
			return fmt.Errorf("[feeds] %w", err)
		}
		for _, s := range cf.All("feed") {
			if err := feedSection(s, set); err != nil {
				return fmt.Errorf("[feed %s] %w", s.Name, err)
			}
		}
		areas := map[string]string{}
		for _, src := range set.Sources {
			if other, dup := areas[strings.ToLower(src.Area)]; dup {
				c.Warnings = append(c.Warnings, fmt.Sprintf("[feed %s] and [feed %s] both post to the area %s", other, src.Name, src.Area))
			}
			areas[strings.ToLower(src.Area)] = src.Name
		}
		c.Feeds = set
	}
	if s := cf.Get("weather", ""); s != nil {
		w := &weather.Settings{UserAgent: userAgent(s.String("contact", contact))}
		err := func() error {
			var err error
			if w.International, err = s.Bool("international", true); err != nil {
				return err
			}
			r, err := s.Int("quake_radius", 300, 10, 5000)
			if err != nil {
				return err
			}
			w.QuakeRadius = float64(r)
			m := s.String("quake_min", "2.5")
			if _, err := fmt.Sscanf(m, "%g", &w.QuakeMin); err != nil || w.QuakeMin < 0 || w.QuakeMin > 10 {
				return fmt.Errorf("quake_min: %q is not a magnitude", m)
			}
			if c.WeatherAtLogin, err = s.Bool("alerts_at_login", true); err != nil {
				return err
			}
			return nil
		}()
		if err != nil {
			return fmt.Errorf("[weather] %w", err)
		}
		if s.String("contact", contact) == "" {
			c.Warnings = append(c.Warnings, "[weather] contact: the National Weather Service asks for a way to reach the sysop")
		}
		c.Weather = w
	}
	if s := cf.Get("quotes", ""); s != nil {
		q := &QuoteSettings{File: s.String("file", filepath.Join(c.DataDir(), "quotes.json.gz")),
			Listen: s.String("listen", ""), Interface: s.String("interface", ""), Delay: s.String("delay", "")}
		if q.Listen != "" {
			if _, _, err := net.SplitHostPort(q.Listen); err != nil {
				return fmt.Errorf("[quotes] listen: %v", err)
			}
		}
		q.AlpacaKey = s.String("alpaca_key", "")
		q.AlpacaSecret = s.String("alpaca_secret", "")
		q.AlpacaFeed = s.String("alpaca_feed", "iex")
		q.UserAgent = userAgent(fs.String("contact", ""))
		var err error
		if q.AlpacaSymbols, err = s.Int("alpaca_symbols", 30, 1, 10000); err != nil {
			return fmt.Errorf("[quotes] %v", err)
		}
		switch {
		case q.AlpacaKey == "" && q.AlpacaSecret == "":
		case q.AlpacaKey == "" || q.AlpacaSecret == "":
			return fmt.Errorf("[quotes] alpaca_key and alpaca_secret go together")
		case q.Listen != "":
			return fmt.Errorf("[quotes] listen (a TOPS feed) or alpaca_key (Alpaca's), not both")
		case q.AlpacaFeed != "iex" && q.AlpacaFeed != "sip" && q.AlpacaFeed != "delayed_sip":
			return fmt.Errorf("[quotes] alpaca_feed: %q is not iex, sip or delayed_sip", q.AlpacaFeed)
		}
		if q.AlpacaKey != "" && q.AlpacaFeed == "delayed_sip" && q.Delay == "" {
			q.Delay = "15 minutes"
		}
		for _, sym := range strings.FieldsFunc(s.String("default", "SPY DIA QQQ AAPL MSFT"), func(r rune) bool { return r == ',' || r == ' ' }) {
			n := quotes.Normalize(sym)
			if n == "" {
				return fmt.Errorf("[quotes] default: %q is not a symbol", sym)
			}
			q.Default = append(q.Default, n)
		}
		c.Quotes = q
	}
	return nil
}

// feedSection reads a [feed NAME]: a feed of the sysop's, or changes to
// one of the catalogue's of that name.
func feedSection(s *config.Section, set *feeds.Settings) error {
	var src *feeds.Source
	at := -1
	for i, c := range set.Sources {
		if c.Name == s.Name {
			src, at = c, i
		}
	}
	if src == nil {
		for _, c := range feeds.Catalog {
			if c.Name == s.Name {
				cc := c
				src = &cc
			}
		}
	}
	if src == nil {
		src = &feeds.Source{Name: s.Name, Group: "News"}
	}
	on, err := s.Bool("enabled", true)
	if err != nil {
		return err
	}
	if !on {
		if at >= 0 {
			set.Sources = append(set.Sources[:at], set.Sources[at+1:]...)
		}
		return nil
	}
	if s.Name == "" {
		return errors.New("needs a name: [feed NAME]")
	}
	src.URL = s.String("url", src.URL)
	if !strings.HasPrefix(src.URL, "https://") && !strings.HasPrefix(src.URL, "http://") {
		return fmt.Errorf("url: %q is not an http or https address", src.URL)
	}
	src.Title = s.String("title", src.Title)
	src.Area = s.String("area", src.Area)
	if src.Area == "" {
		src.Area = s.Name
	}
	if len(src.Area) > 30 {
		return fmt.Errorf("area: %q is longer than 30 letters", src.Area)
	}
	src.Group = s.String("group", src.Group)
	src.Description = s.String("description", src.Description)
	def := int(src.Every / time.Minute)
	if def == 0 {
		def = int(feeds.DefaultEvery / time.Minute)
	}
	every, err := s.Int("every", def, 5, 7*24*60)
	if err != nil {
		return err
	}
	src.Every = time.Duration(every) * time.Minute
	keep := src.KeepDays
	if keep == 0 && at < 0 {
		keep = feeds.DefaultKeep
	}
	if src.KeepDays, err = s.Int("keep_days", keep, 0, 3650); err != nil {
		return err
	}
	if at < 0 {
		set.Sources = append(set.Sources, src)
	}
	return nil
}
