package imageboard

import (
	"strings"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// TestIDsAndFlags: a poster's ID, the same through a thread and another
// in the next; their country's flag from the database.
func TestIDsAndFlags(t *testing.T) {
	s := setup(t)
	s.srv.S.GeoIP = "../geoip/testdata/test-24.mmdb"
	s.bd.PosterIDs, s.bd.Flags, s.bd.TextOnly = true, true, true
	s.b.Store.UpdateBoard(s.bd)
	post := func(ip string, thread int64, com string) *store.BoardPost {
		t.Helper()
		p, err := s.srv.Submit(&Submission{Board: s.bd, Thread: thread, IP: ip, Comment: com})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := s.b.Store.ChanPost(s.bd.ID, p.No)
		return got
	}
	op := post("203.0.113.7", 0, "first")
	same := post("203.0.113.7", 1, "me again")
	other := post("198.51.100.9", 1, "someone else")
	unknown := post("8.8.8.8", 1, "from nowhere known")
	member := post("bbs:alice", 1, "a terminal's")
	next := post("203.0.113.7", 0, "a new thread")
	if op.PosterID == "" || op.PosterID != s.srv.posterID("203.0.113.7", "b", 1) || same.PosterID != op.PosterID ||
		other.PosterID == op.PosterID || next.PosterID == op.PosterID || len(op.PosterID) != 8 || member.PosterID == "" {
		t.Errorf("IDs: %q %q %q %q %q", op.PosterID, same.PosterID, other.PosterID, next.PosterID, member.PosterID)
	}
	if op.Country != "NZ" || op.CountryName != "New Zealand" || other.Country != "DE" || unknown.Country != "" ||
		member.Country != "" {
		t.Errorf("countries: %q %q %q %q", op.Country, other.Country, unknown.Country, member.Country)
	}

	br := s.browser()
	_, _, body := br.get("/chan/b/thread/1")
	for _, want := range []string{`data-id="` + op.PosterID + `"`,
		`(ID: <span class="hand ` + idClass(op.PosterID) + `" data-id="` + op.PosterID + `" title="2 posts by this ID; click to highlight them">`,
		`<span class="flag" title="New Zealand">` + "\U0001F1F3\U0001F1FF" + `</span>`,
		`<span class="flag" title="Germany">`} {
		if !strings.Contains(body, want) {
			t.Errorf("no %s", want)
		}
	}
	var thread struct {
		Posts []apiPost `json:"posts"`
	}
	getJSON(t, s.ts.URL+"/chan/b/thread/1.json", &thread)
	if thread.Posts[0].ID != op.PosterID || thread.Posts[0].Country != "NZ" || thread.Posts[0].CountryName != "New Zealand" ||
		thread.Posts[3].Country != "" {
		t.Errorf("json: %+v", thread.Posts[0])
	}
	var boards struct {
		Boards []map[string]any `json:"boards"`
	}
	getJSON(t, s.ts.URL+"/chan/boards.json", &boards)
	if boards.Boards[0]["user_ids"] != float64(1) || boards.Boards[0]["country_flags"] != float64(1) {
		t.Errorf("boards.json: %v", boards.Boards[0])
	}

	// off again: no more of either
	s.bd.PosterIDs, s.bd.Flags = false, false
	s.b.Store.UpdateBoard(s.bd)
	if p := post("203.0.113.7", 1, "plain"); p.PosterID != "" || p.Country != "" {
		t.Errorf("off: %+v", p)
	}
	// without a database: IDs, no flags
	s.srv.S.GeoIP = ""
	s.bd.PosterIDs, s.bd.Flags = true, true
	s.b.Store.UpdateBoard(s.bd)
	if p := post("198.51.100.9", 1, "x"); p.PosterID == "" || p.Country != "" {
		t.Errorf("no database: %+v", p)
	}
}
