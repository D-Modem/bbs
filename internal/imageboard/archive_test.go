package imageboard

import (
	"image/color"
	"os"
	"strings"
	"testing"
	"time"
)

// A thread pruned off the last page is archived: off the board, read-only
// (no replies, no deleting but by staff), its file kept, listed in the
// archive and archive.json; when its time is up, deleted with its file.
func TestArchive(t *testing.T) {
	s := setup(t)
	s.bd.Pages, s.bd.PerPage = 1, 2
	s.b.Store.UpdateBoard(s.bd)
	br := s.browser()
	var tims []int64
	for i := 0; i < 3; i++ {
		code, _, body := br.post("b", map[string]string{"com": "thread " + itoa(int64(i+1)), "sub": "Subject " + itoa(int64(i+1)),
			"pwd": "secret12"}, pngOf(20+i, 20, color.RGBA{uint8(i * 50), 0, 0, 255}), "x.png")
		if code != 303 {
			t.Fatalf("thread %d: %d\n%s", i, code, body)
		}
		p, _ := s.b.Store.ChanPost(s.bd.ID, int64(i+1))
		tims = append(tims, p.Tim)
	}
	op, err := s.b.Store.ChanPost(s.bd.ID, 1)
	if err != nil || op.Archived.IsZero() {
		t.Fatalf("the oldest thread: %+v %v", op, err)
	}
	if _, err := os.Stat(s.srv.srcPath("b", tims[0], ".png")); err != nil {
		t.Error("its image went with it")
	}
	if n, _ := s.b.Store.ChanThreadCount(s.bd.ID); n != 2 {
		t.Errorf("%d live threads", n)
	}
	_, _, index := br.get("/chan/b/")
	if strings.Contains(index, `id="t1"`) || !strings.Contains(index, `href="/chan/b/archive">Archive</a>`) {
		t.Error("the index shows the archived thread, or no [Archive]")
	}
	code, _, page := br.get("/chan/b/archive")
	if code != 200 || !strings.Contains(page, `<b>Subject 1</b>: thread 1`) || !strings.Contains(page, `href="/chan/b/thread/1"`) ||
		strings.Contains(page, `thread/2"`) || !strings.Contains(page, "kept 3 days") {
		t.Errorf("the archive: %d %s", code, page)
	}
	code, _, page = br.get("/chan/b/thread/1")
	if code != 200 || !strings.Contains(page, "This thread is archived.") || strings.Contains(page, `name="resto"`) ||
		!strings.Contains(page, `class="badge archived"`) || strings.Contains(page, "Posting mode: Reply") {
		t.Errorf("the archived thread's page: %d", code)
	}

	// read-only
	if code, _, body := br.post("b", map[string]string{"resto": "1", "com": "late"}, nil, ""); code != 403 ||
		!strings.Contains(body, "it is archived") {
		t.Errorf("a reply to it: %d %s", code, body)
	}
	if code, _, body := br.postForm("/chan/b/delete", map[string][]string{"1": {"delete"}, "pwd": {"secret12"}, "tok": {br.tok}}); code != 403 ||
		!strings.Contains(body, "archived") {
		t.Errorf("deleting it: %d %s", code, body)
	}

	// the JSON
	var nos []int64
	if getJSON(t, s.ts.URL+"/chan/b/archive.json", &nos); len(nos) != 1 || nos[0] != 1 {
		t.Errorf("archive.json: %v", nos)
	}
	var th struct {
		Posts []apiPost `json:"posts"`
	}
	if getJSON(t, s.ts.URL+"/chan/b/thread/1.json", &th); len(th.Posts) == 0 || th.Posts[0].Archived != 1 ||
		th.Posts[0].ArchivedOn != op.Archived.Unix() {
		t.Errorf("the thread's JSON: %+v", th.Posts)
	}
	var boards struct {
		Boards []map[string]any `json:"boards"`
	}
	if getJSON(t, s.ts.URL+"/chan/boards.json", &boards); boards.Boards[0]["is_archived"] != float64(1) {
		t.Errorf("boards.json: %v", boards)
	}

	// its time up: gone, its file too
	s.srv.S.ArchiveFor = time.Nanosecond
	if code, _, _ := br.get("/chan/b/thread/1"); code != 404 {
		t.Errorf("an expired thread: %d", code)
	}
	if _, err := s.b.Store.ChanPost(s.bd.ID, 1); err == nil {
		t.Error("the expired thread is still there")
	}
	if _, err := os.Stat(s.srv.srcPath("b", tims[0], ".png")); err == nil {
		t.Error("its image is still there")
	}

	// a board without an archive
	s.bd.Archive = false
	s.b.Store.UpdateBoard(s.bd)
	if code, _, _ := br.get("/chan/b/archive"); code != 404 {
		t.Errorf("no archive: %d", code)
	}
}
