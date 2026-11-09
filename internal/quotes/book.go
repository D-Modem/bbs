package quotes

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Quote is what is known of a security from IEX's feed.
type Quote struct {
	Symbol    string
	Last      float64   // the last regular-hours trade of a round lot or more (0: none)
	LastTime  time.Time // and when
	LastSize  uint32
	Open      float64 // IEX's official opening price (IEX-listed securities), else the first regular trade
	Close     float64 // IEX's official closing price, else the last regular trade
	High, Low float64 // of the regular-hours trades
	Volume    uint64  // shares traded on IEX, all hours
	Extended  float64 // the last trade outside regular hours (0: none)
	Bid, Ask  float64 // IEX's best, as it last was (0: none)
	BidSize   uint32
	AskSize   uint32
	PrevClose float64 // the session before's close, where known
	Halted    bool
}

// Change is the price's change from the session before's close, and in
// percent; ok false if either is not known.
func (q *Quote) Change() (float64, float64, bool) {
	p := q.Price()
	if p == 0 || q.PrevClose == 0 {
		return 0, 0, false
	}
	d := p - q.PrevClose
	return d, d / q.PrevClose * 100, true
}

// Price is the quote's price: the close if there is one, else the last
// trade, else the last trade outside regular hours (before the open), else
// the middle of the bid and ask.
func (q *Quote) Price() float64 {
	switch {
	case q.Close != 0:
		return q.Close
	case q.Last != 0:
		return q.Last
	case q.Extended != 0:
		return q.Extended
	case q.Bid != 0 && q.Ask != 0:
		return (q.Bid + q.Ask) / 2
	}
	return 0
}

// Snapshot is the quotes of a session (or of the feed so far).
type Snapshot struct {
	Date    string // the session's, YYYYMMDD
	Source  string // "IEX HIST TOPS 1.6", "IEX TOPS (live)"
	Made    time.Time
	Quotes  map[string]*Quote
	Session string // where the session was: "closed", "regular hours" ...
	// Scope is whose trades these are ("": IEX's own), Credit whose data
	// it is ("": IEX's, as its HIST terms ask)
	Scope  string `json:",omitempty"`
	Credit string `json:",omitempty"`
}

// Book keeps the quotes as a feed's messages come: a Handler.
type Book struct {
	mu      sync.Mutex
	q       map[string]*Quote
	test    map[string]bool
	date    string
	session string
	prev    map[string]float64 // the session before's closes
	last    int64              // the newest timestamp seen (ns)
	day     map[string]string  // each quote's session (YYYYMMDD), when known
}

// NewBook is an empty book; prev is the session before's (nil: none).
func NewBook(prev *Snapshot) *Book {
	b := &Book{q: map[string]*Quote{}, test: map[string]bool{}, prev: map[string]float64{}, day: map[string]string{}}
	if prev != nil {
		for s, q := range prev.Quotes {
			if p := q.Price(); p != 0 {
				b.prev[s] = p
			}
		}
	}
	return b
}

func (b *Book) get(sym string) *Quote {
	q := b.q[sym]
	if q == nil {
		q = &Quote{Symbol: sym, PrevClose: b.prev[sym]}
		b.q[sym] = q
	}
	return q
}

func (b *Book) seen(ts int64) {
	if ts > b.last {
		b.last = ts
		if b.date == "" {
			b.date = time.Unix(0, ts).In(eastern).Format("20060102")
		}
	}
}

func price(p int64) float64 { return float64(p) / priceScale }

func dateOf(ts int64) string { return time.Unix(0, ts).In(eastern).Format("20060102") }

// touch: a trade of sym at ts.  The first of a new session (a live feed
// kept over days) begins the quote again, with the session before's
// price as the close the change is from.
func (b *Book) touch(sym string, ts int64) {
	d := dateOf(ts)
	was := b.day[sym]
	b.day[sym] = d
	q := b.q[sym]
	if q == nil || was == "" || d <= was {
		return
	}
	prev := q.Close
	if prev == 0 {
		prev = q.Last
	}
	if prev == 0 {
		prev = q.PrevClose
	}
	*q = Quote{Symbol: sym, PrevClose: prev}
}

// Trade: Handler.
func (b *Book) Trade(sym string, ts int64, size uint32, p int64, flags byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen(ts)
	b.touch(sym, ts)
	q := b.get(sym)
	q.Volume += uint64(size)
	pr := price(p)
	if flags&flagExtended != 0 {
		q.Extended = pr
		return
	}
	if flags&flagOddLot != 0 {
		return // odd lots do not set the last sale, the high or the low
	}
	q.Last, q.LastSize, q.LastTime = pr, size, time.Unix(0, ts)
	if q.High == 0 || pr > q.High {
		q.High = pr
	}
	if q.Low == 0 || pr < q.Low {
		q.Low = pr
	}
	if q.Open == 0 {
		q.Open = pr
	}
}

// Break: Handler (a trade undone: its shares come off the volume).
func (b *Book) Break(sym string, ts int64, size uint32, p int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.get(sym)
	if q.Volume >= uint64(size) {
		q.Volume -= uint64(size)
	}
}

// Quote: Handler.
func (b *Book) Quote(sym string, ts int64, bidSize uint32, bid, ask int64, askSize uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen(ts)
	q := b.get(sym)
	q.Bid, q.Ask, q.BidSize, q.AskSize = price(bid), price(ask), bidSize, askSize
}

// Official: Handler.
func (b *Book) Official(sym string, ts int64, kind byte, p int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.get(sym)
	switch kind {
	case 'Q':
		q.Open = price(p)
	case 'M':
		q.Close = price(p)
	}
}

// Status: Handler.
func (b *Book) Status(sym string, ts int64, status byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.get(sym).Halted = status == 'H' || status == 'P'
}

// Directory: Handler.
func (b *Book) Directory(sym string, flags byte, roundLot uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if flags&secFlagTest != 0 {
		b.test[sym] = true
	}
}

// System: Handler.
func (b *Book) System(ts int64, event byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen(ts)
	switch event {
	case 'O', 'S':
		b.session = "before the open"
	case 'R':
		b.session = "regular hours"
	case 'M':
		b.session = "after hours"
	case 'E', 'C':
		b.session = "closed"
	}
}

// Put is a quote whole, as a source of them has it (Alpaca's snapshots):
// of the session of date (YYYYMMDD), and as of ts.
func (b *Book) Put(q Quote, date string, ts int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen(ts)
	c := q
	b.q[q.Symbol] = &c
	b.day[q.Symbol] = date
}

// Day is a session's prices so far, as bars have them: the open, high and
// low are taken where the trades have not given them, and the volume where
// it is more.  Not of the quote's session, it is left.
func (b *Book) Day(sym string, ts int64, open, high, low float64, volume uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	q := b.q[sym]
	if q == nil || b.day[sym] != dateOf(ts) {
		return
	}
	if q.Open == 0 {
		q.Open = open
	}
	if q.High == 0 || high > q.High {
		q.High = high
	}
	if q.Low == 0 || low != 0 && low < q.Low {
		q.Low = low
	}
	if volume > q.Volume {
		q.Volume = volume
	}
}

// Clock: where the session is, by the clock, for a feed with no system
// events (Alpaca's).  A weekday with no trades by 9:35 is a holiday.
func (b *Book) Clock(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	et := now.In(eastern)
	min := et.Hour()*60 + et.Minute()
	traded := b.last != 0 && dateOf(b.last) == et.Format("20060102")
	switch {
	case et.Weekday() == time.Saturday || et.Weekday() == time.Sunday, min < 4*60, min >= 20*60:
		b.session = "closed"
	case min < 9*60+30:
		b.session = "before the open"
	case !traded && min >= 9*60+35:
		b.session = "closed"
	case min < 16*60:
		b.session = "regular hours"
	default:
		b.session = "after hours"
	}
}

// testSymbols are IEX's test securities, which trade like any other.
var testSymbols = map[string]bool{"ZEXIT": true, "ZIEXT": true, "ZXIET": true, "ZVZZT": true, "ZWZZT": true,
	"ZXZZT": true, "ZBZX": true, "ZJZZT": true, "ZTEST": true, "CBO": true, "CBX": true, "NTEST": true}

// Snapshot is the book now, without test securities and those with
// nothing to show.
func (b *Book) Snapshot(source string) *Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := &Snapshot{Date: b.date, Source: source, Made: time.Now(), Session: b.session, Quotes: map[string]*Quote{}}
	for sym, q := range b.q {
		if b.test[sym] || testSymbols[sym] || q.Price() == 0 {
			continue
		}
		c := *q
		if b.session == "closed" && c.Close == 0 {
			c.Close = c.Last // the last regular trade stands for the close
		}
		s.Quotes[sym] = &c
	}
	return s
}

var eastern = func() *time.Location {
	l, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("ET", -5*3600)
	}
	return l
}()

// Save writes s as gzipped JSON to path (by way of a temporary file, so
// that a reader never sees half of one).
func (s *Snapshot) Save(path string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".quotes")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	zw := gzip.NewWriter(tmp)
	if err := json.NewEncoder(zw).Encode(s); err != nil {
		tmp.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	os.Chmod(tmp.Name(), 0o644)
	return os.Rename(tmp.Name(), path)
}

// Load reads a snapshot Save wrote.
func Load(path string) (*Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var s Snapshot
	if err := json.NewDecoder(zr).Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Quotes == nil {
		s.Quotes = map[string]*Quote{}
	}
	return &s, nil
}

// Symbols are the snapshot's symbols, sorted, those beginning with prefix.
func (s *Snapshot) Symbols(prefix string) []string {
	var out []string
	for sym := range s.Quotes {
		if strings.HasPrefix(sym, prefix) {
			out = append(out, sym)
		}
	}
	sort.Strings(out)
	return out
}

// SessionDate is the snapshot's date as a time (zero if none).
func (s *Snapshot) SessionDate() time.Time {
	t, _ := time.ParseInLocation("20060102", s.Date, eastern)
	return t
}
