package nntp

import (
	"os"
	"strings"
	"testing"
	"time"
)

// with INN (BBS_TEST_INN=host:port, bbs.general there, this host a peer
// of it): our article goes to INN by IHAVE, one posted at INN comes back
// by pull, and INN serves ours to readers
func TestINN(t *testing.T) {
	addr := os.Getenv("BBS_TEST_INN")
	if addr == "" {
		t.Skip("BBS_TEST_INN not set")
	}
	a := newSite(t, "news.bbs.test", ServerSettings{Post: true}, nil)
	inn := &Peer{Name: "inn", Address: addr, TLS: "no", Groups: "bbs.general", Push: true, Pull: true,
		MaxPerRun: 100, Initial: 0}
	if _, _, err := a.svc.Sync(inn); err != nil { // starting points
		t.Fatal(err)
	}
	ours := a.post(t, "alice", "To INN", "Sent from the BBS by IHAVE.\n.dot", 0)
	pushed, _, err := a.svc.Sync(inn)
	if err != nil || pushed != 1 {
		t.Fatalf("push: %d, %v\n%s", pushed, err, a.log)
	}

	// a newsreader at INN reads ours and posts its own
	cv := dialConv(t, addr)
	cv.send("MODE READER")
	cv.expect("20")
	cv.send("ARTICLE " + ours.MessageID)
	cv.expect("220")
	text := cv.block()
	if !strings.Contains(text, "Sent from the BBS by IHAVE.") || !strings.Contains(text, "news.bbs.test!not-for-mail") {
		t.Fatalf("at INN:\n%s", text)
	}
	cv.send("POST")
	cv.expect("340")
	subject := "From INN " + time.Now().Format("150405.000")
	cv.send("From: Inn User <user@inn.example.test>\r\nNewsgroups: bbs.general\r\nSubject: " + subject +
		"\r\n\r\nPosted at INN.\r\n.")
	cv.expect("240")
	cv.send("QUIT")

	_, pulled, err := a.svc.Sync(inn)
	if err != nil || pulled < 1 {
		t.Fatalf("pull: %d, %v\n%s", pulled, err, a.log)
	}
	ms, _ := a.b.Store.Messages(a.area.ID, 0, 100)
	found := false
	for _, m := range ms {
		if m.Subject == subject && m.From == "Inn User" && m.Origin == "inn" {
			found = true
		}
		if m.Subject == "To INN" && m.Origin != "" {
			t.Errorf("our own came back: %+v", m)
		}
	}
	if !found {
		t.Errorf("INN's article not pulled; have %d", len(ms))
	}
}
