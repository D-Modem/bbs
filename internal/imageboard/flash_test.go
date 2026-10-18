package imageboard

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// cws is a movie compressed, as Flash's own were.
func cws(fws []byte) []byte {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write(fws[8:])
	w.Close()
	return append(append([]byte("CWS"), fws[3:8]...), z.Bytes()...)
}

// noMeta is an FLV without its onMetaData tag.
func noMeta(b []byte) []byte {
	off := int(binary.BigEndian.Uint32(b[5:9]))
	out := append([]byte{}, b[:off+4]...)
	for i := off + 4; i+11 <= len(b); {
		size := int(b[i+1])<<16 | int(b[i+2])<<8 | int(b[i+3])
		end := i + 11 + size + 4
		if b[i] != 18 {
			out = append(out, b[i:end]...)
		}
		i = end
	}
	return out
}

// TestReadFlash: a SWF's size, length and sound, plain and compressed (an
// LZMA one taken unread), and cut or broken ones refused; an FLV's, with
// and without its metadata; and flvPlayer a movie of the size asked whose
// script plays src.
func TestReadFlash(t *testing.T) {
	swf := testFile(t, "tiny.swf")
	for name, b := range map[string][]byte{"FWS": swf, "CWS": cws(swf)} {
		m, err := readSWF(b)
		if err != nil || m.W != 32 || m.H != 24 || m.DurationMS != 1000 || m.Sound {
			t.Errorf("%s: %+v %v", name, m, err)
		}
	}
	if _, err := readSWF(swf[:len(swf)/2]); err == nil {
		t.Error("a movie cut in half read")
	}
	if m, err := readSWF([]byte("ZWS\x0d\x00\x10\x00\x00junkjunkjunk")); err != nil || !m.Sound {
		t.Errorf("an LZMA movie: %+v %v", m, err)
	}
	for _, bad := range []string{"FWS", "FWS\x08\xff\xff\xff\xff", "CWS\x08\x40\x00\x00\x00not zlib at all"} {
		if _, err := readSWF([]byte(bad)); err == nil {
			t.Errorf("%q read", bad)
		}
	}

	flv := testFile(t, "tiny.flv")
	if m, err := readFLV(flv); err != nil || m.W != 32 || m.H != 24 || !m.Sound || m.DurationMS < 900 || m.DurationMS > 1100 {
		t.Errorf("tiny.flv: %+v %v", m, err)
	}
	// without its metadata: the length from the tags, the size Flash's default
	if m, err := readFLV(noMeta(flv)); err != nil || m.W != 320 || m.DurationMS < 700 {
		t.Errorf("without metadata: %+v %v", m, err)
	}
	if _, err := readFLV(flv[:13]); err == nil {
		t.Error("an FLV of no tags read")
	}

	// the player is a movie of the size asked, that plays src
	p := flvPlayer(640, 360)
	m, err := readSWF(p)
	if err != nil || m.W != 640 || m.H != 360 {
		t.Fatalf("the player: %+v %v", m, err)
	}
	for _, s := range []string{"NetConnection", "NetStream", "attachVideo", "play", "src", "vid"} {
		if !bytes.Contains(p, []byte(s+"\x00")) {
			t.Errorf("the player has no %s", s)
		}
	}
	if int(binary.LittleEndian.Uint32(p[4:8])) != len(p) {
		t.Errorf("the player's length: %d, is %d", binary.LittleEndian.Uint32(p[4:8]), len(p))
	}
}

// Flash on a board: taken as the board says, played by Ruffle where it
// is, in a sandboxed frame; its files served to that frame.
func TestFlashPosts(t *testing.T) {
	s := setup(t)
	anon := s.browser()
	s.bd.FileTypes = "swf,flv"
	s.b.Store.UpdateBoard(s.bd)
	post := func(name string, b []byte) (int, string) {
		code, _, body := anon.post("b", map[string]string{"com": name}, b, name)
		return code, body
	}
	if code, body := post("tiny.flv", testFile(t, "tiny.flv")); code != 400 || !strings.Contains(body, "without sound") {
		t.Fatalf("an FLV with sound: %d %s", code, body)
	}
	if code, body := post("tiny.swf", testFile(t, "tiny.swf")); code != 303 && code != 200 {
		t.Fatalf("a movie: %d %s", code, body)
	}
	s.bd.Sound = true
	s.b.Store.UpdateBoard(s.bd)
	if code, body := post("tiny.flv", testFile(t, "tiny.flv")); code != 303 && code != 200 {
		t.Fatalf("an FLV: %d %s", code, body)
	}
	get := func(u string) (*http.Response, string) {
		resp, err := http.Get(s.ts.URL + u)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, string(b)
	}
	// without Ruffle: links to the files, no player
	_, _, page := anon.get("/chan/b/")
	if strings.Contains(page, "flashThumb") || !strings.Contains(page, "32x24, Flash)") || !strings.Contains(page, "32x24, 0:01)") {
		t.Errorf("without Ruffle: %s", page)
	}
	if r, _ := get("/chan/flash"); r.StatusCode != 404 {
		t.Errorf("/chan/flash without Ruffle: %d", r.StatusCode)
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ruffle.js"), []byte("// ruffle"), 0644)
	os.WriteFile(filepath.Join(dir, "core.wasm"), []byte("\x00asm"), 0644)
	os.WriteFile(filepath.Join(t.TempDir(), "secret.js"), []byte("no"), 0644)
	s.srv.S.Ruffle = dir
	_, _, page = anon.get("/chan/b/")
	for _, want := range []string{`class="fileThumb flashThumb" href="/chan/b/src/`, `data-flash=".swf" data-w="32" data-h="24"`, `data-flash=".flv"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the index has no %q", want)
		}
	}
	r, body := get("/chan/flash")
	csp := r.Header.Get("Content-Security-Policy")
	if r.StatusCode != 200 || !strings.HasPrefix(csp, "sandbox allow-scripts;") || !strings.Contains(csp, "frame-ancestors 'self'") ||
		r.Header.Get("X-Frame-Options") != "" || !strings.Contains(body, "/chan/ruffle/ruffle.js") {
		t.Errorf("the player's page: %d %q %q", r.StatusCode, csp, r.Header.Get("X-Frame-Options"))
	}
	for u, typ := range map[string]string{"/chan/ruffle/ruffle.js": "text/javascript", "/chan/ruffle/core.wasm": "application/wasm"} {
		if r, _ := get(u); r.StatusCode != 200 || r.Header.Get("Content-Type") != typ || r.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("%s: %d %s", u, r.StatusCode, r.Header.Get("Content-Type"))
		}
	}
	for _, u := range []string{"/chan/ruffle/..%2fsecret.js", "/chan/ruffle/README.md", "/chan/ruffle/", "/chan/ruffle/sub/ruffle.js"} {
		if r, _ := get(u); r.StatusCode != 404 {
			t.Errorf("%s: %d", u, r.StatusCode)
		}
	}
	r, body = get("/chan/flvplayer.swf?w=32&h=24")
	if m, err := readSWF([]byte(body)); r.StatusCode != 200 || err != nil || m.W != 32 {
		t.Errorf("the FLV player: %d %v %v", r.StatusCode, m, err)
	}
}

// A small movie that unpacks to a great deal is read as it unpacks:
// refused (it is no movie), and nothing much kept meanwhile.
func TestSWFBomb(t *testing.T) {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	head := []byte{0x50, 0x00, 0x05, 0xdc, 0x00, 0x05, 0xdc, 0x00, 0x00, 0x18, 0x01, 0x00} // a stage, 24 fps, 1 frame
	w.Write(head)
	tag := []byte{0x3f, 0x03, 0xff, 0xff, 0xff, 0x07} // a tag of 128 MB: zeros
	w.Write(tag)
	zeros := make([]byte, 1<<20)
	for i := 0; i < 100; i++ {
		w.Write(zeros)
	}
	w.Close()
	b := append([]byte{'C', 'W', 'S', 10}, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(b[4:], 120<<20)
	b = append(b, z.Bytes()...)
	if len(b) > 1<<20 {
		t.Fatalf("the bomb is %d bytes", len(b))
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := readSWF(b)
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Error("the bomb read as a movie")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 8<<20 {
		t.Errorf("reading it took %d MB", grew>>20)
	}
}
