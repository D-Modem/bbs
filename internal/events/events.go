// Package events runs the sysop's scheduled events: programs (no shell)
// run every day at a time, or every so many minutes, each with a time
// limit, their output kept with how they ended.
package events

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event is a scheduled program.
type Event struct {
	Name    string
	Command []string      // the program and its arguments
	At      string        // HH:MM, every day ("": not daily)
	Every   time.Duration // or this often
	Timeout time.Duration // how long it may take (0: ten minutes)
}

// Schedule is how the event's times are said.
func (e *Event) Schedule() string {
	if e.At != "" {
		return "daily at " + e.At
	}
	return "every " + e.Every.String()
}

// Result is how a run went.
type Result struct {
	Start  time.Time
	Took   time.Duration
	Err    string // "": it ended well
	Output string // its last lines
	By     string // "schedule", or who ran it
}

// Status is an event and what is known of it.
type Status struct {
	*Event
	Next    time.Time
	Last    *Result
	Running bool
}

// maxOutput is how much of a run's output is kept.
const maxOutput = 4096

// Scheduler runs the events.
type Scheduler struct {
	Events []*Event
	Log    *log.Logger

	mu      sync.Mutex
	last    map[string]*Result
	running map[string]bool
	started time.Time
	now     func() time.Time
}

// clock is the time now (a test's own clock, if it has set one).
func (s *Scheduler) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// ParseAt reads a time of day, HH:MM.
func ParseAt(at string) error {
	t, err := time.Parse("15:04", at)
	if err != nil || t.Format("15:04") != at {
		return fmt.Errorf("%q is not a time of day as 03:30", at)
	}
	return nil
}

// Split splits a command line into words: spaces part them, and "..." or
// '...' keep spaces in one.  No shell: nothing else is special.
func Split(line string) ([]string, error) {
	var out []string
	var cur strings.Builder
	in, quote, word := false, rune(0), false
	for _, r := range line {
		switch {
		case in && r == quote:
			in = false
		case in:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			in, quote, word = true, r, true
		case r == ' ' || r == '\t':
			if word {
				out = append(out, cur.String())
				cur.Reset()
				word = false
			}
		default:
			cur.WriteRune(r)
			word = true
		}
	}
	if in {
		return nil, errors.New("a quote is not closed")
	}
	if word {
		out = append(out, cur.String())
	}
	return out, nil
}

// next is when an event runs next after now.
func (s *Scheduler) next(e *Event, now time.Time) time.Time {
	if e.At != "" {
		t, _ := time.ParseInLocation("15:04", e.At, now.Location())
		d := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if !d.After(now) {
			d = d.AddDate(0, 0, 1)
		}
		return d
	}
	from := s.started
	if r := s.last[e.Name]; r != nil && r.Start.After(from) {
		from = r.Start
	}
	n := from.Add(e.Every)
	if n.Before(now) {
		return now
	}
	return n
}

// Status is every event's state, by name.
func (s *Scheduler) Status() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	if s.started.IsZero() {
		s.started = now
	}
	var out []Status
	for _, e := range s.Events {
		st := Status{Event: e, Next: s.next(e, now), Running: s.running[e.Name]}
		if r := s.last[e.Name]; r != nil {
			cp := *r
			st.Last = &cp
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Run runs the events as their times come, until ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	s.mu.Lock()
	if s.started.IsZero() {
		s.started = s.clock()
	}
	nexts := map[string]time.Time{}
	for _, e := range s.Events {
		nexts[e.Name] = s.next(e, s.started)
	}
	s.mu.Unlock()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := s.clock()
		for _, e := range s.Events {
			if now.Before(nexts[e.Name]) {
				continue
			}
			go s.RunNow(ctx, e.Name, "schedule")
			s.mu.Lock()
			nexts[e.Name] = s.next(e, now.Add(time.Second))
			if e.Every > 0 {
				nexts[e.Name] = now.Add(e.Every)
			}
			s.mu.Unlock()
		}
	}
}

// ErrRunning: the event is running already.
var ErrRunning = errors.New("it is running already")

// RunNow runs an event now (by: "schedule", or who asked), and waits for
// it to end.
func (s *Scheduler) RunNow(ctx context.Context, name, by string) (*Result, error) {
	var e *Event
	for _, x := range s.Events {
		if x.Name == name {
			e = x
		}
	}
	if e == nil {
		return nil, fmt.Errorf("there is no event %q", name)
	}
	s.mu.Lock()
	if s.running == nil {
		s.running, s.last = map[string]bool{}, map[string]*Result{}
	}
	if s.running[name] {
		s.mu.Unlock()
		return nil, ErrRunning
	}
	s.running[name] = true
	s.mu.Unlock()

	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r := &Result{Start: s.clock(), By: by}
	cmd := exec.CommandContext(cctx, e.Command[0], e.Command[1:]...)
	cmd.Env = append(os.Environ(), "BBS_EVENT="+e.Name)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	r.Took = s.clock().Sub(r.Start)
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("stopped after %v", timeout)
	}
	if err != nil {
		r.Err = err.Error()
	}
	o := out.Bytes()
	if len(o) > maxOutput {
		o = o[len(o)-maxOutput:]
	}
	r.Output = strings.ToValidUTF8(string(o), "?")
	s.mu.Lock()
	s.running[name] = false
	s.last[name] = r
	s.mu.Unlock()
	if s.Log != nil {
		how := "ok"
		if r.Err != "" {
			how = r.Err
		}
		s.Log.Printf("event %s (%s): %s in %v", name, by, how, r.Took.Round(time.Millisecond))
	}
	return r, nil
}

// Minutes is n minutes, for config.
func Minutes(n int) time.Duration { return time.Duration(n) * time.Minute }

// Describe is a result in a line.
func (r *Result) Describe() string {
	how := "ok"
	if r.Err != "" {
		how = r.Err
	}
	return r.Start.Format("01-02 15:04") + " " + how + " (" + strconv.FormatFloat(r.Took.Seconds(), 'f', 1, 64) + "s, " + r.By + ")"
}
