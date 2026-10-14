package imageboard

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The containers read: size, length, sound; what browsers do not play
// refused.  (The files were made by ffmpeg: VP9, VP9 with Opus, H.264,
// HEVC, a Matroska file, a WebM written to a pipe, so its length unsaid.)
func TestReadMedia(t *testing.T) {
	for _, c := range []struct {
		file    string
		w, h    int
		ms      int64
		sound   bool
		refused string
	}{
		{"quiet.webm", 64, 48, 2000, false, ""},
		{"sound.webm", 64, 48, 2000, true, ""},
		{"live.webm", 64, 48, 0, false, ""},
		{"quiet.mp4", 64, 48, 2000, false, ""},
		{"plain.mkv", 0, 0, 0, false, "not a WebM"},
		{"hevc.mp4", 0, 0, 0, false, "browsers do not play"},
	} {
		data := testFile(t, c.file)
		var m *media
		var err error
		switch sniffMedia(data) {
		case ".webm":
			m, err = readWebM(data)
		case ".mp4":
			m, err = readMP4(data)
		default:
			t.Fatalf("%s: not sniffed", c.file)
		}
		if c.refused != "" {
			if err == nil || !strings.Contains(err.Error(), c.refused) {
				t.Errorf("%s: %v, want %q", c.file, err, c.refused)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		if m.W != c.w || m.H != c.h || m.Sound != c.sound || (m.DurationMS+50)/100 != (c.ms+50)/100 {
			t.Errorf("%s: %+v", c.file, m)
		}
	}
	// cut short, or nonsense after the magic: refused, never a panic
	for _, name := range []string{"quiet.webm", "quiet.mp4", "sound.webm"} {
		data := testFile(t, name)
		for _, n := range []int{4, 12, 40, 100, 300, len(data) / 2} {
			readWebM(data[:n])
			readMP4(data[:n])
		}
	}
	readWebM([]byte{0x1a, 0x45, 0xdf, 0xa3, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	readMP4([]byte("\x00\x00\x00\x01ftyp\xff\xff\xff\xff\xff\xff\xff\xff"))

	p, err := readPDF(testFile(t, "two.pdf"))
	if err != nil || p.Pages != 2 || p.W != 300 || p.H != 200 {
		t.Errorf("two.pdf: %+v %v", p, err)
	}
	if _, err := readPDF([]byte("%PDF-1.4\n1 0 obj")); err == nil {
		t.Error("a PDF cut short read")
	}
}

// A board that takes videos and PDFs: posted, thumbnailed (a card, or a
// helper's picture), served as what they are, shown with their length or
// pages; a board that does not refuses them, as one without sound a
// video with it.
func TestMediaPosts(t *testing.T) {
	for _, helpers := range []bool{false, true} {
		s := setup(t)
		if helpers {
			ff, err1 := exec.LookPath("ffmpeg")
			pp, err2 := exec.LookPath("pdftoppm")
			if err1 != nil || err2 != nil {
				t.Log("no ffmpeg or pdftoppm: their thumbnails not tried")
				continue
			}
			s.srv.S.FFmpeg, s.srv.S.PDFToPPM = ff, pp
		}
		anon := s.browser()
		post := func(file string, fields ...string) (int, string) {
			f := map[string]string{"com": "file " + file}
			for i := 0; i+1 < len(fields); i += 2 {
				f[fields[i]] = fields[i+1]
			}
			code, _, body := anon.post("b", f, testFile(t, file), file)
			return code, body
		}
		if code, body := post("quiet.webm"); code != 400 || !strings.Contains(body, "not one this board takes (JPEG, PNG, GIF or WebP)") {
			t.Fatalf("a board without videos: %d %s", code, body)
		}
		s.bd.FileTypes = "webm,mp4,pdf"
		if err := s.b.Store.UpdateBoard(s.bd); err != nil {
			t.Fatal(err)
		}
		if code, body := post("sound.webm"); code != 400 || !strings.Contains(body, "without sound") {
			t.Fatalf("sound on a board without: %d %s", code, body)
		}
		if code, body := post("live.webm"); code != 400 || !strings.Contains(body, "does not say how long") {
			t.Fatalf("no length: %d %s", code, body)
		}
		for _, f := range []string{"quiet.webm", "quiet.mp4", "two.pdf"} {
			if code, body := post(f); code != 303 && code != 200 {
				t.Fatalf("%s: %d %s", f, code, body)
			}
		}
		s.srv.S.MaxVideo = time.Second
		if code, body := post("quiet.mp4", "com", "again"); code != 400 || !strings.Contains(body, "too long: at most 0:01") {
			t.Fatalf("too long: %d %s", code, body)
		}
		s.bd.Sound = true
		s.b.Store.UpdateBoard(s.bd)
		s.srv.S.MaxVideo = 0
		if code, body := post("sound.webm"); code != 303 && code != 200 {
			t.Fatalf("sound on a board with: %d %s", code, body)
		}

		ops, _ := s.b.Store.ChanThreads(s.bd.ID, 0, 10)
		byExt := map[string]int64{}
		for _, op := range ops {
			byExt[op.Ext] = op.No
			switch op.Ext {
			case ".webm", ".mp4":
				if op.W != 64 || op.H != 48 || op.DurationMS < 1900 || op.TnW != 64 {
					t.Errorf("%s: %+v", op.Ext, op)
				}
			case ".pdf":
				if op.PageCount != 2 || op.W != 300 || op.H != 200 || op.TnW == 0 {
					t.Errorf("pdf: %+v", op)
				}
			}
			for _, u := range []string{"/chan/b/src/" + itoa(op.Tim) + op.Ext, "/chan/b/thumb/" + itoa(op.Tim) + "s.jpg"} {
				resp, err := http.Get(s.ts.URL + u)
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				want := fileType(op.Ext)
				if strings.Contains(u, "thumb") {
					want = "image/jpeg"
				}
				if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != want || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
					t.Errorf("%s: %d %s", u, resp.StatusCode, resp.Header.Get("Content-Type"))
				}
				if op.Ext == ".pdf" && !strings.Contains(u, "thumb") && resp.Header.Get("Content-Security-Policy") != "" {
					t.Errorf("a PDF under a CSP its viewer will not show under")
				}
			}
		}
		if len(byExt) != 3 {
			t.Fatalf("posted: %v", byExt)
		}
		_, _, page := anon.get("/chan/b/")
		for _, want := range []string{"64x48, 0:02)", "PDF, 2 pages)", `class="fileThumb videoThumb"`, `class="fileThumb pdfThumb"`,
			"Supported file types are JPG, PNG, GIF, WebP, WebM, MP4 and PDF", `accept="image/jpeg,image/png,image/gif,image/webp,video/webm,video/mp4,application/pdf"`} {
			if !strings.Contains(page, want) {
				t.Errorf("the index has no %q", want)
			}
		}
	}
}

// Whatever is uploaded, reading it ends: an answer or a refusal, never a
// panic or a hang.
func FuzzReadMedia(f *testing.F) {
	for _, name := range []string{"quiet.webm", "sound.webm", "live.webm", "quiet.mp4", "hevc.mp4", "plain.mkv", "two.pdf"} {
		b, _ := os.ReadFile(filepath.Join("testdata", name))
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		readWebM(b)
		readMP4(b)
		readPDF(b)
	})
}
