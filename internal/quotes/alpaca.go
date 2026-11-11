package quotes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Alpaca's market data: a stream of trades, quotes and bars over a
// WebSocket, and snapshots (the day so far, the day before) by REST.  Its
// free plan has IEX's trades and quotes, live, for 30 symbols at a time
// on the stream; its paid one every exchange's (feed "sip").
const (
	AlpacaStream = "wss://stream.data.alpaca.markets/v2/"
	AlpacaData   = "https://data.alpaca.markets/v2/"
)

// Alpaca keeps a Book of the symbols callers look at, from Alpaca's market
// data: each is fetched as a snapshot when wanted, and the most recently
// wanted (up to Max) are followed on the stream.
type Alpaca struct {
	Key, Secret string
	Feed        string // "iex" (the free plan), "sip", "delayed_sip"
	Max         int    // the most symbols on the stream at once (the free plan's 30)
	Pinned      []string
	Book        *Book
	Client      *http.Client
	UserAgent   string
	StreamURL   string // AlpacaStream if "" (the feed is added)
	DataURL     string // AlpacaData if ""
	Log         func(format string, args ...any)
	Now         func() time.Time // the clock (time.Now if nil): what session it is, how fresh a snapshot is

	mu      sync.Mutex
	wanted  map[string]time.Time // when each symbol was last wanted
	fetched map[string]time.Time // when each was last fetched whole
	subbed  map[string]bool      // those on the stream now
	kick    chan struct{}
	err     error // the stream's last failure (nil: connected)
}

func (a *Alpaca) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Alpaca) init() {
	if a.wanted == nil {
		a.wanted, a.fetched, a.subbed = map[string]time.Time{}, map[string]time.Time{}, map[string]bool{}
		a.kick = make(chan struct{}, 1)
	}
	if a.Max <= 0 {
		a.Max = 30
	}
	if a.Feed == "" {
		a.Feed = "iex"
	}
	if a.Client == nil {
		a.Client = &http.Client{Timeout: 20 * time.Second}
	}
}

func (a *Alpaca) logf(format string, args ...any) {
	if a.Log != nil {
		a.Log(format, args...)
	}
}

// Scope is whose trades the feed has, for the screens.
func (a *Alpaca) Scope() string {
	if a.Feed == "iex" || a.Feed == "" {
		return "IEX"
	}
	return "all US exchanges"
}

// Credit is what the screens say of the data.
func (a *Alpaca) Credit() string {
	if a.Scope() == "IEX" {
		return "Market data from IEX, by way of Alpaca (alpaca.markets)."
	}
	return "Market data from the US exchanges' SIP, by way of Alpaca (alpaca.markets)."
}

// Err is why the stream is not connected (nil: it is, or has not tried).
func (a *Alpaca) Err() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

// Want: the symbols are about to be seen.  Those not fetched in the last
// minute, or not on the stream, are fetched now; all go on the stream, in
// place of those wanted longest ago.
func (a *Alpaca) Want(ctx context.Context, syms []string) {
	a.mu.Lock()
	a.init()
	now := a.now()
	var fetch []string
	for _, s := range syms {
		if s = Normalize(s); s == "" {
			continue
		}
		a.wanted[s] = now
		if now.Sub(a.fetched[s]) > time.Minute || !a.subbed[s] && now.Sub(a.fetched[s]) > 15*time.Second {
			fetch = append(fetch, s)
		}
	}
	a.mu.Unlock()
	a.poke()
	if len(fetch) > 0 {
		if err := a.Fetch(ctx, fetch); err != nil {
			a.logf("quotes: Alpaca snapshots: %v", err)
		}
	}
}

func (a *Alpaca) poke() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

// following are the symbols the stream should have: the pinned, then the
// most recently wanted, up to Max.
func (a *Alpaca) following() map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]bool{}
	for _, s := range a.Pinned {
		if len(out) < a.Max {
			out[s] = true
		}
	}
	var rest []string
	for s := range a.wanted {
		if !out[s] {
			rest = append(rest, s)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return a.wanted[rest[i]].After(a.wanted[rest[j]]) })
	for _, s := range rest {
		if len(out) >= a.Max {
			break
		}
		out[s] = true
	}
	return out
}

// alpacaBar, alpacaTrade, alpacaQuote are as the REST snapshots have them.
type alpacaBar struct {
	T string  `json:"t"`
	O float64 `json:"o"`
	H float64 `json:"h"`
	L float64 `json:"l"`
	C float64 `json:"c"`
	V uint64  `json:"v"`
}

type alpacaTrade struct {
	T string   `json:"t"`
	P float64  `json:"p"`
	S uint32   `json:"s"`
	C []string `json:"c"`
}

type alpacaQuote struct {
	T  string  `json:"t"`
	BP float64 `json:"bp"`
	BS uint32  `json:"bs"`
	AP float64 `json:"ap"`
	AS uint32  `json:"as"`
}

type alpacaSnapshot struct {
	LatestTrade  *alpacaTrade `json:"latestTrade"`
	LatestQuote  *alpacaQuote `json:"latestQuote"`
	DailyBar     *alpacaBar   `json:"dailyBar"`
	PrevDailyBar *alpacaBar   `json:"prevDailyBar"`
}

func alpacaTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// regular: t is in regular hours (9:30 to 4, New York time).
func regular(t time.Time) bool {
	et := t.In(eastern)
	m := et.Hour()*60 + et.Minute()
	return m >= 9*60+30 && m < 16*60
}

// Fetch reads the symbols' snapshots into the book (at most 100 a request).
func (a *Alpaca) Fetch(ctx context.Context, syms []string) error {
	a.mu.Lock()
	a.init()
	a.mu.Unlock()
	for len(syms) > 0 {
		n := min(len(syms), 100)
		if err := a.fetch(ctx, syms[:n]); err != nil {
			return err
		}
		syms = syms[n:]
	}
	return nil
}

func (a *Alpaca) fetch(ctx context.Context, syms []string) error {
	base := a.DataURL
	if base == "" {
		base = AlpacaData
	}
	u := base + "stocks/snapshots?" + url.Values{"symbols": {strings.Join(syms, ",")}, "feed": {a.Feed}}.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("APCA-API-KEY-ID", a.Key)
	req.Header.Set("APCA-API-SECRET-KEY", a.Secret)
	req.Header.Set("Accept", "application/json")
	if a.UserAgent != "" {
		req.Header.Set("User-Agent", a.UserAgent)
	}
	resp, err := a.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		var e struct {
			Message string `json:"message"`
		}
		json.Unmarshal(body, &e)
		if e.Message == "" {
			e.Message = resp.Status
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			e.Message += " (are the key and secret right, and the feed one the plan has?)"
		}
		return fmt.Errorf("Alpaca: %s", e.Message)
	}
	var snaps map[string]*alpacaSnapshot
	if err := json.Unmarshal(body, &snaps); err != nil {
		return fmt.Errorf("Alpaca: %w", err)
	}
	now := a.now()
	a.mu.Lock()
	for _, s := range syms {
		a.fetched[s] = now
	}
	a.mu.Unlock()
	for sym, sn := range snaps {
		if sn == nil {
			continue
		}
		q, date, ts := sn.quote(sym, now)
		if date != "" {
			a.Book.Put(q, date, ts)
		}
	}
	return nil
}

// quote is a snapshot as a Quote, of the session of date.
func (sn *alpacaSnapshot) quote(sym string, now time.Time) (Quote, string, int64) {
	q := Quote{Symbol: sym}
	day := sn.DailyBar
	if day == nil || alpacaTime(day.T).IsZero() {
		return q, "", 0
	}
	dt := alpacaTime(day.T)
	date := dt.In(eastern).Format("20060102")
	q.Open, q.High, q.Low, q.Volume = day.O, day.H, day.L, day.V
	if sn.PrevDailyBar != nil {
		q.PrevClose = sn.PrevDailyBar.C
	}
	ts := dt.UnixNano()
	if t := sn.LatestTrade; t != nil {
		tt := alpacaTime(t.T)
		if tt.In(eastern).Format("20060102") == date {
			ts = tt.UnixNano()
			if regular(tt) {
				q.Last, q.LastSize, q.LastTime = t.P, t.S, tt
			} else {
				q.Extended = t.P
			}
		}
	}
	if q.Last == 0 {
		q.Last = day.C // the day's last regular trade, as the bar has it
	}
	// the session over: the bar's close is the close
	sessionEnd := time.Date(dt.In(eastern).Year(), dt.In(eastern).Month(), dt.In(eastern).Day(), 16, 0, 0, 0, eastern)
	if !now.Before(sessionEnd) {
		q.Close = day.C
	}
	if lq := sn.LatestQuote; lq != nil {
		q.Bid, q.BidSize, q.Ask, q.AskSize = lq.BP, lq.BS, lq.AP, lq.AS
	}
	return q, date, ts
}

// Run follows the stream until ctx is done, connecting again when it is
// cut off (later each time, and much later when the keys are refused).
func (a *Alpaca) Run(ctx context.Context) {
	a.mu.Lock()
	a.init()
	a.mu.Unlock()
	if len(a.Pinned) > 0 {
		a.Want(ctx, a.Pinned)
	}
	go a.clock(ctx)
	wait := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := a.stream(ctx)
		if ctx.Err() != nil {
			return
		}
		a.mu.Lock()
		a.err = err
		a.subbed = map[string]bool{}
		a.mu.Unlock()
		a.logf("quotes: Alpaca stream: %v", err)
		if time.Since(start) > 5*time.Minute {
			wait = time.Second
		}
		var ae *alpacaError
		if errors.As(err, &ae) && (ae.Code == 401 || ae.Code == 402 || ae.Code == 409) {
			wait = 10 * time.Minute // not authenticated, or not allowed that feed: no use asking often
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 10*time.Minute)
	}
}

// clock keeps the session where the clock has it, and the snapshots of
// what is followed fresh (each five minutes: the bars' highs and lows,
// corrections).
func (a *Alpaca) clock(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	n := 0
	for {
		a.Book.Clock(a.now())
		if n++; n%5 == 0 {
			var syms []string
			for s := range a.following() {
				syms = append(syms, s)
			}
			if err := a.Fetch(ctx, syms); err != nil && ctx.Err() == nil {
				a.logf("quotes: Alpaca snapshots: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// alpacaError is an error message of the stream's.
type alpacaError struct {
	Code int
	Msg  string
}

func (e *alpacaError) Error() string { return fmt.Sprintf("%s (%d)", e.Msg, e.Code) }

// alpacaMsg is any of the stream's messages (the fields are by type: "c"
// is a trade's conditions but a bar's close, so it is kept raw).
type alpacaMsg struct {
	T    string          `json:"T"`
	Sym  string          `json:"S"`
	Msg  string          `json:"msg"`
	Code int             `json:"code"`
	Time string          `json:"t"`
	P    float64         `json:"p"`
	Size uint32          `json:"s"`
	C    json.RawMessage `json:"c"`
	BP   float64         `json:"bp"`
	BS   uint32          `json:"bs"`
	AP   float64         `json:"ap"`
	AS   uint32          `json:"as"`
	O    float64         `json:"o"`
	H    float64         `json:"h"`
	L    float64         `json:"l"`
	V    uint64          `json:"v"`
	SC   string          `json:"sc"` // a status's code
	OS   uint32          `json:"os"` // a correction's original size
	CS   uint32          `json:"cs"` // and corrected size
}

func scaled(p float64) int64 { return int64(math.Round(p * priceScale)) }

// stream is one connection to the stream, until it fails.
func (a *Alpaca) stream(ctx context.Context) error {
	u := a.StreamURL
	if u == "" {
		u = AlpacaStream
	}
	u += a.Feed
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	h := http.Header{}
	if a.UserAgent != "" {
		h.Set("User-Agent", a.UserAgent)
	}
	c, _, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPClient: a.Client, HTTPHeader: h})
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(4 << 20)
	read := func(ctx context.Context) ([]alpacaMsg, error) {
		_, data, err := c.Read(ctx)
		if err != nil {
			return nil, err
		}
		var ms []alpacaMsg
		if err := json.Unmarshal(data, &ms); err != nil {
			return nil, fmt.Errorf("Alpaca: %w", err)
		}
		for _, m := range ms {
			if m.T == "error" {
				return nil, &alpacaError{Code: m.Code, Msg: m.Msg}
			}
		}
		return ms, nil
	}
	send := func(v any) error {
		b, _ := json.Marshal(v)
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return c.Write(wctx, websocket.MessageText, b)
	}
	// connected, then authenticated
	if _, err := read(dctx); err != nil {
		return err
	}
	if err := send(map[string]string{"action": "auth", "key": a.Key, "secret": a.Secret}); err != nil {
		return err
	}
	for authed := false; !authed; {
		ms, err := read(dctx)
		if err != nil {
			return err
		}
		for _, m := range ms {
			authed = authed || m.T == "success" && m.Msg == "authenticated"
		}
	}
	a.mu.Lock()
	a.err = nil
	a.mu.Unlock()
	a.logf("quotes: Alpaca stream (%s) connected", a.Feed)

	msgs := make(chan []alpacaMsg, 64)
	failed := make(chan error, 1)
	go func() {
		for {
			ms, err := read(ctx)
			if err != nil {
				failed <- err
				return
			}
			msgs <- ms
		}
	}()
	a.poke()
	for {
		select {
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return ctx.Err()
		case err := <-failed:
			return err
		case <-a.kick:
			if err := a.resubscribe(send); err != nil {
				return err
			}
		case ms := <-msgs:
			for _, m := range ms {
				a.take(m)
			}
		}
	}
}

// resubscribe brings the stream's symbols to those followed.
func (a *Alpaca) resubscribe(send func(any) error) error {
	want := a.following()
	a.mu.Lock()
	var add, drop []string
	for s := range want {
		if !a.subbed[s] {
			add = append(add, s)
		}
	}
	for s := range a.subbed {
		if !want[s] {
			drop = append(drop, s)
		}
	}
	a.mu.Unlock()
	sort.Strings(add)
	sort.Strings(drop)
	msg := func(action string, syms []string) map[string]any {
		return map[string]any{"action": action, "trades": syms, "quotes": syms, "dailyBars": syms, "statuses": syms}
	}
	if len(drop) > 0 {
		if err := send(msg("unsubscribe", drop)); err != nil {
			return err
		}
	}
	if len(add) > 0 {
		if err := send(msg("subscribe", add)); err != nil {
			return err
		}
	}
	a.mu.Lock()
	for _, s := range drop {
		delete(a.subbed, s)
	}
	for _, s := range add {
		a.subbed[s] = true
	}
	a.mu.Unlock()
	return nil
}

// take is one of the stream's messages, into the book.
func (a *Alpaca) take(m alpacaMsg) {
	t := alpacaTime(m.Time)
	if t.IsZero() {
		return
	}
	ts := t.UnixNano()
	b := a.Book
	switch m.T {
	case "t":
		var flags byte
		var cond []string
		json.Unmarshal(m.C, &cond)
		for _, c := range cond {
			switch c {
			case "I":
				flags |= flagOddLot
			case "T", "U":
				flags |= flagExtended
			}
		}
		if !regular(t) {
			flags |= flagExtended
		}
		b.Trade(m.Sym, ts, m.Size, scaled(m.P), flags)
	case "q":
		b.Quote(m.Sym, ts, m.BS, scaled(m.BP), scaled(m.AP), m.AS)
	case "d":
		b.Day(m.Sym, ts, m.O, m.H, m.L, m.V)
	case "x": // a trade cancelled
		b.Break(m.Sym, ts, m.Size, scaled(m.P))
	case "c": // a trade corrected: its size, in the volume
		b.Break(m.Sym, ts, m.OS, 0)
		b.mu.Lock()
		if q := b.q[m.Sym]; q != nil {
			q.Volume += uint64(m.CS)
		}
		b.mu.Unlock()
	case "s":
		switch m.SC {
		case "H", "P", "M":
			b.Status(m.Sym, ts, 'H')
		case "T", "Q":
			b.Status(m.Sym, ts, 'T')
		}
	}
}
