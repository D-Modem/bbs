package imageboard

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image/color"
	"net/http"
	"os"
	"strings"
	"testing"
)

// testReplay is a w by h drawing of a pen stroke, an eraser and a fill.
func testReplay(w, h int) *replay {
	return &replay{V: 1, W: w, H: h, BG: "#ffffff", S: []stroke{
		{T: "p", C: "#ff0000", Z: 3, P: []int{0, 1, 1, 100, 10, 10, 250, 20, 5}},
		{T: "e", C: "#000000", Z: 8, P: []int{1500, 5, 5}},
		{T: "f", C: "#00a2e8", Z: 1, P: []int{2000, 30, 30}},
	}}
}

// TestPNGReplay: a drawing's strokes kept in its PNG come back with their
// time; one of the wrong size, a bad stroke, two chunks, not zlib or JSON,
// or a zlib bomb is taken out, the picture left.
func TestPNGReplay(t *testing.T) {
	plain := pngOf(40, 40, color.RGBA{255, 0, 0, 255})
	good, err := withReplay(plain, testReplay(40, 40))
	if err != nil {
		t.Fatal(err)
	}
	if out, ms, ok := pngReplay(good, 40, 40); !ok || ms != 3850 || !bytes.Equal(out, good) {
		t.Errorf("good: %v %d", ok, ms)
	}
	// not this picture's size: the chunk goes, the picture stays
	if out, _, ok := pngReplay(good, 41, 40); ok || !bytes.Equal(out, plain) {
		t.Errorf("wrong size kept: %v", ok)
	}
	bad := map[string]*replay{
		"version":    {V: 2, W: 40, H: 40, BG: "#ffffff", S: testReplay(40, 40).S},
		"no strokes": {V: 1, W: 40, H: 40, BG: "#ffffff"},
		"colour":     {V: 1, W: 40, H: 40, BG: "#ffffff", S: []stroke{{T: "p", C: "red", Z: 3, P: []int{0, 1, 1}}}},
		"tool":       {V: 1, W: 40, H: 40, BG: "#ffffff", S: []stroke{{T: "x", C: "#000000", Z: 3, P: []int{0, 1, 1}}}},
		"off canvas": {V: 1, W: 40, H: 40, BG: "#ffffff", S: []stroke{{T: "p", C: "#000000", Z: 3, P: []int{0, 99, 1}}}},
		"fill off":   {V: 1, W: 40, H: 40, BG: "#ffffff", S: []stroke{{T: "f", C: "#000000", Z: 1, P: []int{0, 40, 1}}}},
		"points":     {V: 1, W: 40, H: 40, BG: "#ffffff", S: []stroke{{T: "p", C: "#000000", Z: 3, P: []int{0, 1}}}},
		"time":       {V: 1, W: 40, H: 40, BG: "#ffffff", S: []stroke{{T: "p", C: "#000000", Z: 3, P: []int{-5, 1, 1}}}},
		"brush":      {V: 1, W: 40, H: 40, BG: "#ffffff", S: []stroke{{T: "p", C: "#000000", Z: 500, P: []int{0, 1, 1}}}},
	}
	for name, rp := range bad {
		b, _ := withReplay(plain, rp)
		if out, _, ok := pngReplay(b, 40, 40); ok || !bytes.Equal(out, plain) {
			t.Errorf("%s: kept", name)
		}
	}
	// not zlib, not JSON, two of them: gone
	twice, _ := withReplay(good, testReplay(40, 40))
	for name, b := range map[string][]byte{"twice": twice, "garbage": rawChunk(plain, []byte("not zlib at all")),
		"not json": rawChunk(plain, zlibOf([]byte("{{{")))} {
		if out, _, ok := pngReplay(b, 40, 40); ok || !bytes.Equal(out, plain) {
			t.Errorf("%s: kept", name)
		}
	}
	// a bomb: a little zlib that is a great deal of JSON
	bomb := rawChunk(plain, zlibOf(bytes.Repeat([]byte(" "), maxReplayJSON+10)))
	if _, _, ok := pngReplay(bomb, 40, 40); ok {
		t.Error("a bomb kept")
	}
}

// zlibOf is b compressed with zlib.
func zlibOf(b []byte) []byte {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write(b)
	w.Close()
	return z.Bytes()
}

// rawChunk is a PNG with an okST chunk of these bytes.
func rawChunk(png, data []byte) []byte {
	// withReplay's work, without its JSON
	rp, _ := withReplay(png, &replay{})
	chunks, _ := pngChunks(rp)
	for _, c := range chunks {
		if c.typ == replayChunk {
			out := append([]byte{}, rp[:c.start]...)
			ch := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
			ch = append(append(ch, replayChunk...), data...)
			ch = binary.BigEndian.AppendUint32(ch, crc32.ChecksumIEEE(ch[4:]))
			return append(append(out, ch...), rp[c.end:]...)
		}
	}
	return nil
}

// TestOekakiPost: a drawing posted keeps its strokes, and is shown and
// given as one; a bad chunk is dropped, the picture kept.
func TestOekakiPost(t *testing.T) {
	s := setup(t)
	s.bd.Oekaki = true
	s.b.Store.UpdateBoard(s.bd)
	br := s.browser()
	if _, _, body := br.get("/chan/b/"); !strings.Contains(body, `id="okCanvas"`) || !strings.Contains(body, "oekaki.js") {
		t.Fatal("no drawing board")
	}
	drawing, _ := withReplay(pngOf(120, 80, color.RGBA{0, 0, 255, 255}), testReplay(120, 80))
	if code, _, body := br.post("b", map[string]string{"com": "my drawing"}, drawing, "oekaki.png"); code != 303 {
		t.Fatalf("post: %d\n%s", code, body)
	}
	p, _ := s.b.Store.ChanPost(s.bd.ID, 1)
	if !p.Replay || p.DrawMS != 3850 {
		t.Fatalf("replay %v %d", p.Replay, p.DrawMS)
	}
	kept, _ := os.ReadFile(s.srv.srcPath("b", p.Tim, ".png"))
	if !bytes.Equal(kept, drawing) {
		t.Error("the PNG was not kept as it came")
	}
	_, _, body := br.get("/chan/b/thread/1")
	for _, want := range []string{"drawn in 3s", `class="replayLink"`, `class="drawOver"`, "?draw=1#postForm"} {
		if !strings.Contains(body, want) {
			t.Errorf("no %s", want)
		}
	}
	var thread struct {
		Posts []apiPost `json:"posts"`
	}
	getJSON(t, s.ts.URL+"/chan/b/thread/1.json", &thread)
	if thread.Posts[0].Replay != 1 || thread.Posts[0].DrawMS != 3850 {
		t.Errorf("json: %+v", thread.Posts[0])
	}
	var boards struct {
		Boards []map[string]any `json:"boards"`
	}
	getJSON(t, s.ts.URL+"/chan/boards.json", &boards)
	if boards.Boards[0]["oekaki"] != float64(1) {
		t.Errorf("boards.json: %v", boards.Boards[0])
	}

	// a replay that is not of this picture: dropped, the picture kept
	liar, _ := withReplay(pngOf(60, 60, color.RGBA{0, 255, 0, 255}), testReplay(10, 10))
	if code, _, _ := br.post("b", map[string]string{"resto": "1"}, liar, "x.png"); code != 303 {
		t.Fatalf("reply: %d", code)
	}
	p2, _ := s.b.Store.ChanPost(s.bd.ID, 2)
	kept, _ = os.ReadFile(s.srv.srcPath("b", p2.Tim, ".png"))
	if p2.Replay || bytes.Contains(kept, []byte(replayChunk)) || len(kept) == 0 {
		t.Errorf("a bad replay kept: %v", p2.Replay)
	}
	// on a board that is not oekaki, a drawing still replays
	s.bd.Oekaki = false
	s.b.Store.UpdateBoard(s.bd)
	if _, _, body := br.get("/chan/b/thread/1"); !strings.Contains(body, "replayLink") || strings.Contains(body, "drawOver") ||
		strings.Contains(body, "okCanvas") {
		t.Error("not oekaki: still replayed, not drawn over")
	}
	resp, _ := http.Get(s.ts.URL + "/chan/static/oekaki.js")
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("oekaki.js: %d", resp.StatusCode)
	}
}
