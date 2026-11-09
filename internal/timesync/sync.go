package timesync

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server is a time server the clock follows: "ntp HOST[:PORT]", "nts
// HOST[:PORT]" (the NTS-KE port), "ptp HOST".
type Server struct {
	Kind string // ntp, nts, ptp
	Host string
}

func (s Server) String() string { return s.Kind + " " + s.Host }

// ParseServers reads a list: "nts time.cloudflare.com, ntp pool.ntp.org,
// ptp 192.0.2.1" (a bare host is NTP's).
func ParseServers(v string) ([]Server, error) {
	var out []Server
	for _, f := range strings.Split(v, ",") {
		w := strings.Fields(f)
		switch len(w) {
		case 0:
			continue
		case 1:
			w = []string{"ntp", w[0]}
		case 2:
		default:
			return nil, fmt.Errorf("%q: KIND HOST", strings.TrimSpace(f))
		}
		k := strings.ToLower(w[0])
		switch k {
		case "ntp", "nts", "ptp":
		default:
			return nil, fmt.Errorf("%q: ntp, nts or ptp", w[0])
		}
		h := w[1]
		if k == "ptp" {
			if _, _, err := net.SplitHostPort(h); err == nil {
				return nil, fmt.Errorf("%q: PTP's ports are 319 and 320: a host alone", h)
			}
		} else if host, port, err := net.SplitHostPort(h); err == nil {
			if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || host == "" {
				return nil, fmt.Errorf("%q: not HOST:PORT", h)
			}
		}
		out = append(out, Server{Kind: k, Host: h})
	}
	return out, nil
}

// Settings are the [time] section's.
type Settings struct {
	Servers  []Server
	Poll     time.Duration // how often the servers are asked
	SetClock bool
	Local    Local
	RootCAs  string // a PEM file NTS-KE servers' certificates are checked against ("": the system's)
}

// Follower asks the servers the time, every Poll, and has the clock
// follow them.
type Follower struct {
	S     Settings
	Clock *Clock
	PTP   *PTP // for ptp servers: the port, opened (nil: none)
	Log   func(string, ...any)

	tlsc     *tls.Config
	mu       sync.Mutex
	sessions map[string]*ntsSession
	ptps     map[string]*ptpUpstream
	last     []Result
}

// Result is one server's answer to a poll.
type Result struct {
	Server Server
	Sample Sample
	Err    error
	Chosen bool
}

func (f *Follower) logf(format string, a ...any) {
	if f.Log != nil {
		f.Log(format, a...)
	}
}

// Prepare makes the PTP clients, before the port runs.
func (f *Follower) Prepare() error {
	f.sessions = map[string]*ntsSession{}
	f.ptps = map[string]*ptpUpstream{}
	if f.S.RootCAs != "" {
		c, err := RootCAs(f.S.RootCAs)
		if err != nil {
			return err
		}
		f.tlsc = c
	}
	for _, s := range f.S.Servers {
		if s.Kind != "ptp" {
			continue
		}
		if f.PTP == nil {
			return errors.New("ptp servers need the PTP port")
		}
		u, err := f.PTP.follow(s.Host)
		if err != nil {
			return err
		}
		f.ptps[s.Host] = u
	}
	return nil
}

// Run polls until ctx is done.
func (f *Follower) Run(ctx context.Context) {
	if f.sessions == nil {
		if err := f.Prepare(); err != nil {
			f.logf("time: %v", err)
			return
		}
	}
	poll := max(f.S.Poll, 16*time.Second)
	wait := 2 * time.Second // the first soon: PTP's first syncs come in a second or two
	failing := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = poll
		res := f.Poll(ctx)
		ok := false
		for _, r := range res {
			ok = ok || r.Chosen
		}
		if !ok && !failing {
			var why []string
			for _, r := range res {
				why = append(why, fmt.Sprintf("%s: %v", r.Server, r.Err))
			}
			f.logf("time: no server answered (%s)", strings.Join(why, "; "))
		}
		failing = !ok
	}
}

// Poll asks every server once and has the clock follow the best: the
// median of their offsets, falsetickers outvoted.
func (f *Follower) Poll(ctx context.Context) []Result {
	res := make([]Result, len(f.S.Servers))
	var wg sync.WaitGroup
	for i, s := range f.S.Servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm, err := f.ask(ctx, s)
			sm.Source = s.String()
			res[i] = Result{Server: s, Sample: sm, Err: err}
		}()
	}
	wg.Wait()
	var good []int
	for i, r := range res {
		if r.Err == nil {
			good = append(good, i)
		}
	}
	if len(good) > 0 {
		sort.Slice(good, func(a, b int) bool { return res[good[a]].Sample.Offset < res[good[b]].Sample.Offset })
		m := good[(len(good)-1)/2]
		res[m].Chosen = true
		f.Clock.Follow(res[m].Sample)
	}
	f.mu.Lock()
	f.last = res
	f.mu.Unlock()
	return res
}

// Last is the last poll's results.
func (f *Follower) Last() []Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Result(nil), f.last...)
}

// burst is how many times an NTP server is asked a poll: the answer with
// the shortest round trip is taken.
const burst = 4

// ask is one server's sample.
func (f *Follower) ask(ctx context.Context, s Server) (Sample, error) {
	switch s.Kind {
	case "ptp":
		u := f.ptps[s.Host]
		if u == nil {
			return Sample{}, errors.New("not prepared")
		}
		return u.best(max(f.S.Poll, 16*time.Second))
	case "nts":
		return f.askNTS(ctx, s.Host)
	}
	addr := s.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "123")
	}
	return bestOf(ctx, func() (Sample, error) { return query(ctx, addr, nil) })
}

func bestOf(ctx context.Context, q func() (Sample, error)) (Sample, error) {
	var best Sample
	var lastErr error
	got := false
	for i := range burst {
		if i > 0 {
			select {
			case <-ctx.Done():
				return Sample{}, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		sm, err := q()
		if err != nil {
			lastErr = err
			if errors.Is(err, errNoCookies) || strings.Contains(err.Error(), "kissed") || strings.Contains(err.Error(), "NAK") {
				break
			}
			continue
		}
		if !got || sm.Delay < best.Delay {
			best, got = sm, true
		}
	}
	if !got {
		return Sample{}, lastErr
	}
	return best, nil
}

func (f *Follower) askNTS(ctx context.Context, host string) (Sample, error) {
	f.mu.Lock()
	sess := f.sessions[host]
	f.mu.Unlock()
	if sess == nil {
		var err error
		if sess, err = keyExchange(ctx, host, f.tlsc); err != nil {
			return Sample{}, err
		}
		f.mu.Lock()
		f.sessions[host] = sess
		f.mu.Unlock()
	}
	sm, err := bestOf(ctx, func() (Sample, error) {
		sm, err := query(ctx, sess.addr, sess)
		if err != nil && sess.spent() {
			// begun again next poll
			f.mu.Lock()
			if f.sessions[host] == sess {
				delete(f.sessions, host)
			}
			f.mu.Unlock()
		}
		return sm, err
	})
	return sm, err
}

// Query asks a server the time once, as a poll would (for bbs time): PTP
// servers through the port given.
func Query(ctx context.Context, s Server, rootCAs string, port *PTP) (Sample, error) {
	f := &Follower{S: Settings{Servers: []Server{s}, RootCAs: rootCAs}, Clock: &Clock{}, PTP: port}
	if err := f.Prepare(); err != nil {
		return Sample{}, err
	}
	if s.Kind == "ptp" {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		go port.Run(ctx)
		select {
		case <-ctx.Done():
			return Sample{}, ctx.Err()
		case <-time.After(5 * time.Second):
		}
		return f.ptps[s.Host].best(time.Minute)
	}
	sm, err := f.ask(ctx, s)
	sm.Source = s.String()
	return sm, err
}
