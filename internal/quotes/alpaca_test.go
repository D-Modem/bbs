package quotes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeAlpaca answers as Alpaca's market data does: snapshots by REST, and
// a stream that asks for the keys, then sends what is subscribed.
func fakeAlpaca(t *testing.T) (*httptest.Server, chan map[string]any) {
	subs := make(chan map[string]any, 8)
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/stocks/snapshots":
			if r.Header.Get("APCA-API-KEY-ID") != "KEY" || r.Header.Get("APCA-API-SECRET-KEY") != "SECRET" {
				w.WriteHeader(401)
				w.Write([]byte(`{"message":"forbidden."}`))
				return
			}
			if r.URL.Query().Get("feed") != "iex" {
				t.Errorf("feed %q", r.URL.Query().Get("feed"))
			}
			out := map[string]any{}
			for _, s := range strings.Split(r.URL.Query().Get("symbols"), ",") {
				if s == "NOSUCH" {
					continue
				}
				out[s] = map[string]any{
					"latestTrade":  map[string]any{"t": "2026-11-06T20:59:59.5Z", "p": 342.41, "s": 50, "c": []string{"@"}},
					"latestQuote":  map[string]any{"t": "2026-11-06T21:00:00Z", "bp": 342.3, "bs": 1, "ap": 342.5, "as": 2},
					"dailyBar":     map[string]any{"t": "2026-11-06T05:00:00Z", "o": 338.59, "h": 345.76, "l": 338.59, "c": 342.41, "v": 500},
					"prevDailyBar": map[string]any{"t": "2026-11-05T05:00:00Z", "o": 335, "h": 341, "l": 334, "c": 340, "v": 900},
				}
			}
			json.NewEncoder(w).Encode(out)
		case "/v2/iex":
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer c.CloseNow()
			ctx := r.Context()
			c.Write(ctx, websocket.MessageText, []byte(`[{"T":"success","msg":"connected"}]`))
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var auth map[string]string
			json.Unmarshal(data, &auth)
			if auth["action"] != "auth" || auth["key"] != "KEY" || auth["secret"] != "SECRET" {
				c.Write(ctx, websocket.MessageText, []byte(`[{"T":"error","code":402,"msg":"auth failed"}]`))
				return
			}
			c.Write(ctx, websocket.MessageText, []byte(`[{"T":"success","msg":"authenticated"}]`))
			for {
				_, data, err := c.Read(ctx)
				if err != nil {
					return
				}
				var m map[string]any
				json.Unmarshal(data, &m)
				subs <- m
				mu.Lock()
				// a new session's first trades: Monday's, then an odd lot
				// and one after hours
				c.Write(ctx, websocket.MessageText, []byte(`[{"T":"subscription","trades":["AAPL"]},
					{"T":"t","S":"AAPL","p":345,"s":100,"c":["@"],"t":"2026-11-09T14:31:00Z"},
					{"T":"t","S":"AAPL","p":399,"s":10,"c":["@","I"],"t":"2026-11-09T14:32:00Z"},
					{"T":"q","S":"AAPL","bp":344.9,"bs":3,"ap":345.1,"as":4,"t":"2026-11-09T14:32:01Z"},
					{"T":"d","S":"AAPL","o":344,"h":346,"l":343.5,"c":345,"v":1200,"t":"2026-11-09T05:00:00Z"},
					{"T":"t","S":"AAPL","p":346.5,"s":5,"c":["@","T"],"t":"2026-11-09T21:30:00Z"},
					{"T":"s","S":"AAPL","sc":"H","t":"2026-11-09T21:31:00Z"}]`))
				mu.Unlock()
			}
		default:
			t.Errorf("asked for %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, subs
}

func TestAlpacaSnapshots(t *testing.T) {
	srv, _ := fakeAlpaca(t)
	a := &Alpaca{Key: "KEY", Secret: "SECRET", Book: NewBook(nil), DataURL: srv.URL + "/v2/"}
	if err := a.Fetch(context.Background(), []string{"AAPL", "NOSUCH"}); err != nil {
		t.Fatal(err)
	}
	s := a.Book.Snapshot("Alpaca iex")
	q := s.Quotes["AAPL"]
	if q == nil || q.Open != 338.59 || q.High != 345.76 || q.Close != 342.41 || q.Last != 342.41 || q.PrevClose != 340 ||
		q.Volume != 500 || q.Bid != 342.3 || q.AskSize != 2 || q.LastTime.IsZero() {
		t.Fatalf("AAPL %+v", q)
	}
	if d, _, ok := q.Change(); !ok || d < 2.40 || d > 2.42 {
		t.Errorf("change %v", d)
	}
	if _, ok := s.Quotes["NOSUCH"]; ok {
		t.Error("a symbol Alpaca has not")
	}
	bad := &Alpaca{Key: "KEY", Secret: "WRONG", Book: NewBook(nil), DataURL: srv.URL + "/v2/"}
	if err := bad.Fetch(context.Background(), []string{"AAPL"}); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("wrong secret: %v", err)
	}
}

// TestAlpacaStream: Friday's quote from the snapshot, then Monday's trades
// on the stream begin the quote again, with Friday's close the change is
// from.
func TestAlpacaStream(t *testing.T) {
	srv, subs := fakeAlpaca(t)
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v2/"
	// Monday before the open, whenever the test is run: the session not
	// over, so no close yet
	monday := time.Date(2026, 11, 9, 9, 0, 0, 0, eastern)
	a := &Alpaca{Key: "KEY", Secret: "SECRET", Book: NewBook(nil), Pinned: []string{"AAPL"},
		DataURL: srv.URL + "/v2/", StreamURL: ws, Now: func() time.Time { return monday }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	select {
	case m := <-subs:
		if m["action"] != "subscribe" || len(m["trades"].([]any)) != 1 || m["dailyBars"].([]any)[0] != "AAPL" {
			t.Errorf("subscribed %v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("not subscribed (%v)", a.Err())
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if q := a.Book.Snapshot("x").Quotes["AAPL"]; q != nil && q.Halted {
			if q.PrevClose != 342.41 || q.Last != 345 || q.Open != 345 || q.High != 346 || q.Low != 343.5 ||
				q.Volume != 1205 || q.Extended != 346.5 || q.Bid != 344.9 || q.Close != 0 {
				t.Errorf("Monday's AAPL %+v", q)
			}
			// a symbol wanted goes on the stream too
			a.Want(ctx, []string{"MSFT"})
			select {
			case m := <-subs:
				if m["action"] != "subscribe" || m["quotes"].([]any)[0] != "MSFT" {
					t.Errorf("subscribed %v", m)
				}
			case <-time.After(5 * time.Second):
				t.Error("MSFT not subscribed")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the stream's trades did not come: %+v (%v)", a.Book.Snapshot("x").Quotes["AAPL"], a.Err())
}

func TestAlpacaRefused(t *testing.T) {
	srv, _ := fakeAlpaca(t)
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v2/"
	a := &Alpaca{Key: "KEY", Secret: "WRONG", Book: NewBook(nil), StreamURL: ws}
	a.init()
	err := a.stream(context.Background())
	if ae, ok := err.(*alpacaError); !ok || ae.Code != 402 {
		t.Errorf("refused: %v", err)
	}
}

func TestFollowing(t *testing.T) {
	a := &Alpaca{Max: 3, Pinned: []string{"SPY"}}
	a.init()
	now := time.Now()
	a.wanted["A"], a.wanted["B"], a.wanted["C"] = now.Add(-3*time.Minute), now.Add(-time.Minute), now
	f := a.following()
	if len(f) != 3 || !f["SPY"] || !f["C"] || !f["B"] || f["A"] {
		t.Errorf("following %v", f)
	}
}

func TestClock(t *testing.T) {
	b := NewBook(nil)
	at := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, eastern) }
	for _, c := range []struct {
		when time.Time
		want string
	}{{at(7, 12, 0), "closed"}, {at(9, 3, 0), "closed"}, {at(9, 8, 0), "before the open"},
		{at(9, 9, 40), "closed"}} { // no trades by 9:35: a holiday
		b.Clock(c.when)
		if b.session != c.want {
			t.Errorf("%v: %q", c.when, b.session)
		}
	}
	b.Trade("AAPL", at(9, 9, 31).UnixNano(), 100, 3450000, 0)
	b.Clock(at(9, 10, 0))
	if b.session != "regular hours" {
		t.Errorf("trading: %q", b.session)
	}
	b.Clock(at(9, 17, 0))
	if b.session != "after hours" {
		t.Errorf("after: %q", b.session)
	}
}
