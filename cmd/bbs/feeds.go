package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/feeds"
	"github.com/D-Modem/bbs/internal/quotes"
	"github.com/D-Modem/bbs/internal/server"
	"github.com/D-Modem/bbs/internal/weather"
)

// startNews starts what [feeds], [weather] and [quotes] ask for: the feeds
// read into their areas, the callers' weather, the quotes board (and a
// live feed into it).
func startNews(ctx context.Context, b *bbs.BBS, cfg *server.Config) {
	if f := cfg.Feeds; f != nil && len(f.Sources) > 0 {
		fs := &feeds.Service{B: b, S: *f}
		for _, src := range f.Sources {
			b.FeedAreas = append(b.FeedAreas, src.Area)
		}
		b.FeedStatus = func() []bbs.FeedStatus { return feedStatus(fs) }
		go fs.Run(ctx)
	}
	if cfg.Weather != nil {
		b.Weather = &weather.Service{S: *cfg.Weather}
		b.WeatherAtLogin = cfg.WeatherAtLogin
	}
	if q := cfg.Quotes; q != nil {
		b.Quotes = &quotes.Board{Path: q.File, LiveDelay: q.Delay}
		b.QuoteDefault = q.Default
		if q.Listen != "" {
			prev, _ := quotes.Load(q.File)
			b.Quotes.Live = quotes.NewBook(prev)
			go func() {
				if err := quotes.Listen(ctx, q.Listen, q.Interface, b.Quotes.Live); err != nil {
					b.Log.Printf("quotes: %s: %v", q.Listen, err)
				}
			}()
		}
	}
}

func feedStatus(fs *feeds.Service) []bbs.FeedStatus {
	var out []bbs.FeedStatus
	for _, st := range fs.Statuses() {
		s := bbs.FeedStatus{Name: st.Source.Name, Area: st.Source.Area, Last: st.Last, Next: st.Next, Posted: st.Posted}
		if st.Err != nil {
			s.Err = st.Err.Error()
		}
		out = append(out, s)
	}
	return out
}

// feedsCmd: bbs feeds list | catalog | read [NAME...]
func feedsCmd(cfg *server.Config, args []string) {
	if len(args) == 0 {
		usage()
	}
	switch args[0] {
	case "catalog":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tGROUP\tAREA\tURL")
		for _, c := range feeds.Catalog {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Name, c.Group, c.Area, c.URL)
		}
		w.Flush()
	case "list":
		if cfg.Feeds == nil {
			fmt.Println("no feeds: [feeds] catalog = all, or [feed NAME] sections")
			return
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tAREA\tEVERY\tKEEP\tURL")
		for _, s := range cfg.Feeds.Sources {
			fmt.Fprintf(w, "%s\t%s\t%s\t%dd\t%s\n", s.Name, s.Area, s.Every, s.KeepDays, s.URL)
		}
		w.Flush()
	case "read":
		if cfg.Feeds == nil {
			fatal(fmt.Errorf("no feeds are configured"))
		}
		st := open(cfg)
		defer st.Close()
		b := bbs.New(cfg.Settings, st, nil)
		b.News = cfg.News
		fs := &feeds.Service{B: b, S: *cfg.Feeds}
		if err := fs.Prepare(); err != nil {
			fatal(err)
		}
		want := map[string]bool{}
		for _, n := range args[1:] {
			want[n] = true
		}
		failed := false
		for _, src := range cfg.Feeds.Sources {
			if len(want) > 0 && !want[src.Name] {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			n, err := fs.Read(ctx, src)
			cancel()
			if err != nil {
				fmt.Printf("%-20s %v\n", src.Name, err)
				failed = true
				continue
			}
			fmt.Printf("%-20s %d new in %s\n", src.Name, n, src.Area)
		}
		if failed {
			os.Exit(1)
		}
	default:
		usage()
	}
}

// quotesCmd: bbs quotes fetch [DATE] | load FILE | show SYMBOL...
func quotesCmd(cfg *server.Config, args []string) {
	if len(args) == 0 {
		usage()
	}
	if cfg.Quotes == nil {
		fatal(fmt.Errorf("no [quotes] section"))
	}
	path := cfg.Quotes.File
	prev, _ := quotes.Load(path)
	save := func(b *quotes.Book, source string) {
		s := b.Snapshot(source)
		if len(s.Quotes) == 0 {
			fatal(fmt.Errorf("no quotes were found"))
		}
		if prev != nil && prev.Date == s.Date && len(prev.Quotes) > len(s.Quotes)*2 {
			fatal(fmt.Errorf("%s has %d quotes of %s, these only %d: not replaced", path, len(prev.Quotes), s.Date, len(s.Quotes)))
		}
		if err := s.Save(path); err != nil {
			fatal(err)
		}
		fmt.Printf("%d quotes of %s (%s) in %s\n", len(s.Quotes), s.Date, s.Session, path)
	}
	switch args[0] {
	case "fetch":
		date := ""
		if len(args) > 1 {
			date = strings.ReplaceAll(args[1], "-", "")
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		cl := &http.Client{}
		ua := "D-Modem-BBS/1.0 (+https://github.com/D-Modem/bbs)"
		f, err := quotes.LatestTOPS(ctx, cl, ua, date)
		if err != nil {
			fatal(err)
		}
		if prev != nil && prev.Date == f.Date && date == "" {
			fmt.Printf("%s already has the quotes of %s\n", path, f.Date)
			return
		}
		// the session before's closes, for the change: those kept, unless
		// they are of this session (fetched again)
		base := prev
		if base != nil && base.Date >= f.Date {
			base = nil
		}
		fmt.Printf("reading IEX's TOPS file of %s (%.1f GB) as it comes\n", f.Date, float64(f.Size)/1e9)
		b := quotes.NewBook(base)
		start := time.Now()
		err = quotes.FetchHIST(ctx, cl, ua, f, b, func(n int64) {
			if f.Size > 0 {
				fmt.Printf("  %.0f%% (%.1f MB/s)\n", float64(n)*100/float64(f.Size), float64(n)/1e6/time.Since(start).Seconds())
			}
		})
		if err != nil {
			fatal(err)
		}
		save(b, "IEX HIST TOPS "+f.Version)
	case "load":
		if len(args) != 2 {
			usage()
		}
		fh, err := os.Open(args[1])
		if err != nil {
			fatal(err)
		}
		defer fh.Close()
		b := quotes.NewBook(prev)
		if err := quotes.ReadCapture(fh, b, nil); err != nil {
			fatal(err)
		}
		save(b, "IEX TOPS capture")
	case "show":
		s, err := quotes.Load(path)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s, %s (%s): %d symbols\n", s.Date, s.Source, s.Session, len(s.Quotes))
		for _, a := range args[1:] {
			sym := quotes.Normalize(a)
			q := s.Quotes[sym]
			if q == nil {
				fmt.Printf("%-8s -\n", a)
				continue
			}
			ch := ""
			if d, p, ok := q.Change(); ok {
				ch = fmt.Sprintf("%+.2f (%+.2f%%)", d, p)
			}
			fmt.Printf("%-8s %10.2f %-18s high %.2f low %.2f, %d shares on IEX\n", sym, q.Price(), ch, q.High, q.Low, q.Volume)
		}
	default:
		usage()
	}
}
