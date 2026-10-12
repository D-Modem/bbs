package store

import (
	"testing"
	"time"
)

func TestTopics(t *testing.T) {
	s := open(t)
	u, _ := s.CreateUser("alice", "password1", "", false)
	a, _ := s.CreateArea("General", "", false)
	b, _ := s.CreateArea("Other", "", false)
	post := func(area *Area, reply int64, subject string) int64 {
		t.Helper()
		id, err := s.Post(&Message{AreaID: area.ID, FromID: u.ID, From: "alice", To: "All", Subject: subject,
			Body: "x", ReplyTo: reply})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	one := post(a, 0, "One")
	two := post(a, 0, "Two")
	r1 := post(a, one, "Re: One")
	r2 := post(a, r1, "Re: Re: One")   // an answer to an answer: still One's
	cross := post(b, one, "Elsewhere") // in another area: a topic there
	if m, _ := s.Message(r2); m.Thread != one {
		t.Errorf("thread %d", m.Thread)
	}
	if m, _ := s.Message(cross); m.Thread != 0 {
		t.Errorf("cross-area thread %d", m.Thread)
	}
	if n, _ := s.TopicCount(a.ID); n != 2 {
		t.Errorf("topics %d", n)
	}
	ts, err := s.Topics(a.ID, 0, 10)
	if err != nil || len(ts) != 2 || ts[0].Root.ID != one || ts[0].Replies != 2 || ts[0].Last.ID != r2 ||
		ts[1].Root.ID != two || ts[1].Last.ID != two {
		t.Fatalf("topics %+v %v", ts, err)
	}
	if ms, _ := s.TopicMessages(one, 1, 10); len(ms) != 2 || ms[0].ID != r1 {
		t.Errorf("page %v", ms)
	}
	if n, _ := s.TopicLen(one); n != 3 {
		t.Errorf("len %d", n)
	}
	if n, _ := s.TopicPosition(one, r2); n != 2 {
		t.Errorf("position %d", n)
	}

	// read marks only go forward
	s.SetTopicRead(u.ID, one, r2)
	s.SetTopicRead(u.ID, one, r1)
	if m, _ := s.TopicsRead(u.ID, []int64{one, two}); m[one] != r2 || len(m) != 1 {
		t.Errorf("read %v", m)
	}

	// the first gone, the next begins the topic
	if err := s.DeleteMessage(one); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.Message(r1); m.Thread != 0 {
		t.Errorf("new first's thread %d", m.Thread)
	}
	if m, _ := s.Message(r2); m.Thread != r1 {
		t.Errorf("rest's thread %d", m.Thread)
	}

	// purged: what is left of a topic begins again
	old := post(a, 0, "Old")
	newer := post(a, old, "Re: Old")
	s.db.Exec(`UPDATE messages SET created = ? WHERE id = ?`, time.Now().AddDate(0, 0, -10).Unix(), old)
	a.MaxDays = 5
	if n, err := s.Purge(a); err != nil || n != 1 {
		t.Fatalf("purge %d %v", n, err)
	}
	if m, _ := s.Message(newer); m.Thread != 0 {
		t.Errorf("after the purge %d", m.Thread)
	}

	// an older database: threads found
	s.db.Exec(`UPDATE messages SET thread = 0`)
	if err := findThreads(s.db); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.Message(r2); m.Thread != r1 {
		t.Errorf("found %d", m.Thread)
	}
}
