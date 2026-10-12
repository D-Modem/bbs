package events

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSplit(t *testing.T) {
	for in, want := range map[string]string{
		`/usr/bin/backup --to "/var/backups/my bbs" -v`: `/usr/bin/backup|--to|/var/backups/my bbs|-v`,
		`echo 'a "b" c'  d`:                             `echo|a "b" c|d`,
		`  x  `:                                         `x`,
		`x ""`:                                          `x|`,
	} {
		got, err := Split(in)
		if err != nil || strings.Join(got, "|") != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if _, err := Split(`echo "open`); err == nil {
		t.Error("an open quote")
	}
	if ParseAt("03:30") != nil || ParseAt("3:30") == nil || ParseAt("25:00") == nil {
		t.Error("ParseAt")
	}
}

func TestNext(t *testing.T) {
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.Local)
	s := &Scheduler{started: now}
	if n := s.next(&Event{At: "03:30"}, now); !n.Equal(time.Date(2026, 10, 13, 3, 30, 0, 0, time.Local)) {
		t.Errorf("tomorrow %v", n)
	}
	if n := s.next(&Event{At: "12:00"}, now); !n.Equal(time.Date(2026, 10, 12, 12, 0, 0, 0, time.Local)) {
		t.Errorf("today %v", n)
	}
	if n := s.next(&Event{Every: time.Hour}, now.Add(time.Minute)); !n.Equal(now.Add(time.Hour)) {
		t.Errorf("every %v", n)
	}
}

func TestRun(t *testing.T) {
	s := &Scheduler{Events: []*Event{
		{Name: "hello", Command: []string{"/bin/sh", "-c", `echo "hi from $BBS_EVENT"`}, Every: time.Hour},
		{Name: "fails", Command: []string{"/bin/sh", "-c", "echo oops >&2; exit 3"}, At: "04:00"},
		{Name: "slow", Command: []string{"/bin/sleep", "5"}, Every: time.Hour, Timeout: 200 * time.Millisecond},
	}}
	ctx := context.Background()
	r, err := s.RunNow(ctx, "hello", "sysop")
	if err != nil || r.Err != "" || r.Output != "hi from hello\n" || r.By != "sysop" {
		t.Errorf("hello: %+v %v", r, err)
	}
	r, _ = s.RunNow(ctx, "fails", "schedule")
	if r.Err != "exit status 3" || r.Output != "oops\n" {
		t.Errorf("fails: %+v", r)
	}
	done := make(chan *Result)
	go func() { r, _ := s.RunNow(ctx, "slow", "x"); done <- r }()
	time.Sleep(50 * time.Millisecond)
	if _, err := s.RunNow(ctx, "slow", "y"); err != ErrRunning {
		t.Errorf("twice: %v", err)
	}
	if r := <-done; !strings.Contains(r.Err, "stopped after 200ms") {
		t.Errorf("slow: %+v", r)
	}
	if _, err := s.RunNow(ctx, "nope", "x"); err == nil {
		t.Error("no such event")
	}
	st := s.Status()
	if len(st) != 3 || st[0].Name != "fails" || st[0].Last == nil || st[0].Schedule() != "daily at 04:00" {
		t.Errorf("status %+v", st)
	}
}
