package imageboard

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// Files besides images, as 4chan takes them: WebM and MP4 videos, PDFs.
// A board takes those its file types name.  Each is read here, by its
// bytes: its container walked for its size, length, codecs and sound;
// thumbnailed by ffmpeg or pdftoppm if [chan] names them, else on a card
// saying what it is.

// mediaTypes are the files besides images: their extensions, MIME types,
// and names in a board's file types.
var mediaTypes = map[string]struct{ mime, kind string }{
	".webm": {"video/webm", "webm"},
	".mp4":  {"video/mp4", "mp4"},
	".pdf":  {"application/pdf", "pdf"},
	".swf":  {"application/x-shockwave-flash", "swf"},
	".flv":  {"video/x-flv", "flv"},
}

// FileKinds are the file types a board may take besides images.
var FileKinds = []string{"webm", "mp4", "pdf", "swf", "flv"}

// kindNames are the kinds as the post form says them.
var kindNames = map[string]string{"webm": "WebM", "mp4": "MP4", "pdf": "PDF", "swf": "SWF", "flv": "FLV"}

// fileType is any file's MIME type by its extension ("": none kept).
func fileType(ext string) string {
	if t, ok := imageTypes[ext]; ok {
		return t
	}
	return mediaTypes[ext].mime
}

// sniffMedia is a video's or PDF's extension by its first bytes ("": not
// one).
func sniffMedia(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return ".webm"
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return ".mp4"
	case bytes.HasPrefix(b, []byte("%PDF-")):
		return ".pdf"
	case len(b) >= 8 && (string(b[:3]) == "FWS" || string(b[:3]) == "CWS" || string(b[:3]) == "ZWS"):
		return ".swf"
	case bytes.HasPrefix(b, []byte("FLV\x01")):
		return ".flv"
	}
	return ""
}

// media is what reading a video or PDF found.
type media struct {
	W, H       int   // a video's pixels; a PDF's first page, in points
	DurationMS int64 // a video's length (0: not said)
	Sound      bool  // a video has an audio track
	Pages      int   // a PDF's (0: not found)
}

var errVideo = errors.New("That video could not be read.")

// ---- WebM (Matroska's EBML) ----

// vint reads an EBML variable-length integer at b[i:]: its value (with
// its length marker if id), its length; ok false if it is broken.
func vint(b []byte, i int, id bool) (v uint64, n int, ok bool) {
	if i >= len(b) || b[i] == 0 {
		return 0, 0, false
	}
	n = 1
	for mask := byte(0x80); b[i]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 8 || i+n > len(b) || id && n > 4 {
		return 0, 0, false
	}
	v = uint64(b[i])
	if !id {
		v &= uint64(0xff >> n)
	}
	for k := 1; k < n; k++ {
		v = v<<8 | uint64(b[i+k])
	}
	return v, n, true
}

// unknownSize is an EBML element whose size is not said (all ones).
func unknownSize(v uint64, n int) bool { return v == 1<<(7*n)-1 }

// ebmlChildren walks the elements in b[start:end], calling f with each's
// ID, data and where its data starts in b; an element of unknown size
// runs to end.
func ebmlChildren(b []byte, start, end int, f func(id uint64, data []byte, at int, unknown bool) bool) error {
	in := b[:end] // an element's head stays inside its parent
	for i := start; i < end; {
		id, n, ok := vint(in, i, true)
		if !ok {
			return errVideo
		}
		i += n
		size, n, ok := vint(in, i, false)
		if !ok {
			return errVideo
		}
		i += n
		stop := end
		unknown := unknownSize(size, n)
		if !unknown {
			if size > uint64(end-i) {
				return errVideo
			}
			stop = i + int(size)
		}
		if !f(id, b[i:stop], i, unknown) {
			return nil
		}
		i = stop
	}
	return nil
}

func ebmlUint(d []byte) uint64 {
	var v uint64
	for _, c := range d {
		v = v<<8 | uint64(c)
	}
	return v
}

func ebmlFloat(d []byte) float64 {
	switch len(d) {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(d)))
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(d))
	}
	return 0
}

// webm codecs a browser plays
var webmCodecs = map[string]bool{"V_VP8": true, "V_VP9": true, "V_AV1": true, "A_VORBIS": true, "A_OPUS": true}

// readWebM reads a WebM's header, info and tracks.
func readWebM(b []byte) (*media, error) {
	var m media
	var docType string
	var segStart, segEnd int
	err := ebmlChildren(b, 0, len(b), func(id uint64, d []byte, at int, unknown bool) bool {
		switch id {
		case 0x1a45dfa3: // EBML header
			ebmlChildren(d, 0, len(d), func(id uint64, d []byte, _ int, _ bool) bool {
				if id == 0x4282 {
					docType = string(bytes.TrimRight(d, "\x00"))
				}
				return true
			})
		case 0x18538067: // Segment
			segStart, segEnd = at, at+len(d)
			return false
		}
		return true
	})
	if err != nil || segEnd == 0 {
		return nil, errVideo
	}
	if docType != "webm" {
		return nil, errors.New("That is a Matroska file, not a WebM: browsers do not play it.")
	}
	scale := uint64(1000000) // ns a tick
	var duration float64
	var info, tracks bool
	var bad string
	var videos int
	err = ebmlChildren(b, segStart, segEnd, func(id uint64, d []byte, _ int, unknown bool) bool {
		switch id {
		case 0x1549a966: // Info
			info = true
			ebmlChildren(d, 0, len(d), func(id uint64, d []byte, _ int, _ bool) bool {
				switch id {
				case 0x2ad7b1:
					scale = ebmlUint(d)
				case 0x4489:
					duration = ebmlFloat(d)
				}
				return true
			})
		case 0x1654ae6b: // Tracks
			tracks = true
			ebmlChildren(d, 0, len(d), func(id uint64, d []byte, _ int, _ bool) bool {
				if id != 0xae { // TrackEntry
					return true
				}
				var kind uint64
				var codec string
				var w, h uint64
				ebmlChildren(d, 0, len(d), func(id uint64, d []byte, _ int, _ bool) bool {
					switch id {
					case 0x83:
						kind = ebmlUint(d)
					case 0x86:
						codec = string(bytes.TrimRight(d, "\x00"))
					case 0xe0: // Video
						ebmlChildren(d, 0, len(d), func(id uint64, d []byte, _ int, _ bool) bool {
							switch id {
							case 0xb0:
								w = ebmlUint(d)
							case 0xba:
								h = ebmlUint(d)
							}
							return true
						})
					}
					return true
				})
				switch {
				case !webmCodecs[codec]:
					bad = codec
				case kind == 1:
					videos++
					if m.W == 0 {
						m.W, m.H = int(min(w, 1<<20)), int(min(h, 1<<20))
					}
				case kind == 2:
					m.Sound = true
				default:
					bad = codec
				}
				return true
			})
		case 0x1f43b675: // a Cluster: the pictures; the header is before
			return !(info && tracks)
		}
		return !unknown
	})
	switch {
	case err != nil || !tracks:
		return nil, errVideo
	case bad != "":
		return nil, fmt.Errorf("That video has a track browsers do not play (%s): WebM takes VP8, VP9 or AV1 with Vorbis or Opus.", bad)
	case videos != 1:
		return nil, errors.New("A video has one video track.")
	}
	if duration > 0 && scale > 0 {
		m.DurationMS = int64(duration * float64(scale) / 1e6)
	}
	return &m, nil
}

// ---- MP4 (ISO base media) ----

// boxes walks the boxes in b, calling f with each's type and body.
func boxes(b []byte, f func(typ string, body []byte) bool) error {
	for i := 0; i < len(b); {
		if len(b)-i < 8 {
			return errVideo
		}
		size := uint64(binary.BigEndian.Uint32(b[i:]))
		typ := string(b[i+4 : i+8])
		head := 8
		switch size {
		case 0:
			size = uint64(len(b) - i)
		case 1:
			if len(b)-i < 16 {
				return errVideo
			}
			size, head = binary.BigEndian.Uint64(b[i+8:]), 16
		}
		if size < uint64(head) || size > uint64(len(b)-i) {
			return errVideo
		}
		if !f(typ, b[i+head:i+int(size)]) {
			return nil
		}
		i += int(size)
	}
	return nil
}

// mp4 codecs a browser plays
var mp4Video = map[string]bool{"avc1": true, "avc3": true, "av01": true, "vp09": true}
var mp4Audio = map[string]bool{"mp4a": true, "Opus": true}

// readMP4 reads an MP4's movie header and tracks.
func readMP4(b []byte) (*media, error) {
	var m media
	var moov []byte
	first := true
	err := boxes(b, func(typ string, body []byte) bool {
		if first && typ != "ftyp" {
			return false
		}
		first = false
		if typ == "moov" {
			moov = body
		}
		return true
	})
	if err != nil || moov == nil {
		return nil, errors.New("That video could not be read (no movie header: an MP4 still being written?).")
	}
	var bad string
	var videos int
	err = boxes(moov, func(typ string, body []byte) bool {
		switch typ {
		case "mvhd":
			if len(body) >= 20 && body[0] == 0 {
				if ts := binary.BigEndian.Uint32(body[12:]); ts > 0 {
					m.DurationMS = int64(uint64(binary.BigEndian.Uint32(body[16:])) * 1000 / uint64(ts))
				}
			} else if len(body) >= 32 && body[0] == 1 {
				if ts := binary.BigEndian.Uint32(body[20:]); ts > 0 {
					m.DurationMS = int64(binary.BigEndian.Uint64(body[24:]) / uint64(ts) * 1000)
				}
			}
		case "trak":
			var w, h int
			var handler, codec string
			boxes(body, func(typ string, body []byte) bool {
				switch typ {
				case "tkhd":
					if len(body) >= 8 {
						w = int(binary.BigEndian.Uint32(body[len(body)-8:]) >> 16)
						h = int(binary.BigEndian.Uint32(body[len(body)-4:]) >> 16)
					}
				case "mdia":
					boxes(body, func(typ string, body []byte) bool {
						switch typ {
						case "hdlr":
							if len(body) >= 12 {
								handler = string(body[8:12])
							}
						case "minf":
							boxes(body, func(typ string, body []byte) bool {
								if typ == "stbl" {
									boxes(body, func(typ string, body []byte) bool {
										if typ == "stsd" && len(body) >= 16 {
											codec = string(body[12:16])
										}
										return true
									})
								}
								return true
							})
						}
						return true
					})
				}
				return true
			})
			switch handler {
			case "vide":
				if !mp4Video[codec] {
					bad = codec
				}
				videos++
				m.W, m.H = w, h
			case "soun":
				if !mp4Audio[codec] {
					bad = codec
				}
				m.Sound = true
			}
		}
		return true
	})
	switch {
	case err != nil:
		return nil, errVideo
	case bad != "":
		return nil, fmt.Errorf("That video has a track browsers do not play (%q): MP4 takes H.264, AV1 or VP9 with AAC or Opus.", bad)
	case videos != 1:
		return nil, errors.New("A video has one video track.")
	}
	return &m, nil
}

// ---- PDF ----

var (
	pdfPage     = regexp.MustCompile(`/Type\s*/Page\b`)
	pdfCount    = regexp.MustCompile(`/Type\s*/Pages\b[^>]*?/Count\s+(\d+)|/Count\s+(\d+)[^>]*?/Type\s*/Pages\b`)
	pdfMediaBox = regexp.MustCompile(`/MediaBox\s*\[\s*(-?[\d.]+)\s+(-?[\d.]+)\s+(-?[\d.]+)\s+(-?[\d.]+)\s*\]`)
)

// readPDF checks a PDF's header and end, and finds its pages and the
// first one's size where its objects are not compressed.
func readPDF(b []byte) (*media, error) {
	if len(b) < 16 || !bytes.HasPrefix(b, []byte("%PDF-")) || !bytes.Contains(b[max(0, len(b)-2048):], []byte("%%EOF")) {
		return nil, errors.New("That PDF could not be read (cut short?).")
	}
	m := &media{W: 612, H: 792} // a letter, if it does not say
	// the page tree's count, else the pages counted
	for _, c := range pdfCount.FindAllSubmatch(b, -1) {
		s := c[1]
		if len(s) == 0 {
			s = c[2]
		}
		if n, err := strconv.Atoi(string(s)); err == nil && n > m.Pages && n < 1<<20 {
			m.Pages = n
		}
	}
	if m.Pages == 0 {
		m.Pages = len(pdfPage.FindAllIndex(b, -1))
	}
	if mb := pdfMediaBox.FindSubmatch(b); mb != nil {
		var v [4]float64
		for i := range v {
			v[i], _ = strconv.ParseFloat(string(mb[i+1]), 64)
		}
		if w, h := math.Abs(v[2]-v[0]), math.Abs(v[3]-v[1]); w >= 1 && h >= 1 && w < 20000 && h < 20000 {
			m.W, m.H = int(w+0.5), int(h+0.5)
		}
	}
	return m, nil
}

// maxVideoSide is a video's largest width or height.
const maxVideoSide = 4096

// takes says whether a board takes a kind of file besides images.
func takes(b *store.Board, kind string) bool {
	for _, k := range strings.Split(b.FileTypes, ",") {
		if strings.TrimSpace(k) == kind {
			return true
		}
	}
	return false
}

// taken says what a board takes, for when a file is not.
func taken(b *store.Board) error {
	kinds := "JPEG, PNG, GIF, WebP"
	for _, k := range FileKinds {
		if takes(b, k) {
			kinds += ", " + kindNames[k]
		}
	}
	if i := strings.LastIndex(kinds, ", "); i >= 0 {
		kinds = kinds[:i] + " or " + kinds[i+2:]
	}
	return fmt.Errorf("That file is not one this board takes (%s).", kinds)
}

// prepareFile checks an upload, an image or (where the board takes them)
// a video or PDF, and makes its thumbnail.
func (s *Server) prepareFile(b *store.Board, data []byte, side int, bg color.Color) (*picture, error) {
	if sniff(data) != "" {
		return prepare(data, side, bg, s.S.Strip)
	}
	ext := sniffMedia(data)
	if ext == "" || !takes(b, mediaTypes[ext].kind) {
		return nil, taken(b)
	}
	var m *media
	var err error
	switch ext {
	case ".webm":
		m, err = readWebM(data)
	case ".mp4":
		m, err = readMP4(data)
	case ".pdf":
		m, err = readPDF(data)
	case ".swf":
		m, err = readSWF(data)
	case ".flv":
		m, err = readFLV(data)
	}
	if err != nil {
		return nil, err
	}
	if ext == ".swf" && m.Sound && !b.Sound {
		return nil, errors.New("This board takes Flash without sound.")
	}
	video := fileKind(ext) == "video" || ext == ".flv"
	if video {
		switch {
		case m.W < 1 || m.H < 1:
			return nil, errVideo
		case m.W > maxVideoSide || m.H > maxVideoSide:
			return nil, fmt.Errorf("That video is too large: at most %d by %d pixels.", maxVideoSide, maxVideoSide)
		case m.Sound && !b.Sound:
			return nil, errors.New("This board takes videos without sound: take its audio track out.")
		case s.S.MaxVideo > 0 && m.DurationMS == 0:
			return nil, errors.New("That video does not say how long it is.")
		case s.S.MaxVideo > 0 && time.Duration(m.DurationMS)*time.Millisecond > s.S.MaxVideo:
			return nil, fmt.Errorf("That video is too long: at most %s.", clock(s.S.MaxVideo.Milliseconds()))
		}
	}
	img, err := s.frame(data, ext)
	if err != nil {
		// the helper could not make it out: no more can a browser
		s.B.Log.Printf("chan: /%s/: a %s's thumbnail: %v", b.Dir, ext, err)
		if video {
			return nil, errVideo
		}
		return nil, errors.New("That PDF could not be read.")
	}
	if img == nil {
		w, h := fit(m.W, m.H, side)
		switch {
		case ext == ".swf":
			img = card(w, h, "FLASH", fmt.Sprintf("%dx%d", m.W, m.H))
		case video:
			img = card(w, h, strings.ToUpper(strings.TrimPrefix(ext, ".")), clock(m.DurationMS))
		default:
			pages := "PDF"
			if m.Pages > 0 {
				pages = strconv.Itoa(m.Pages) + " " + plural(m.Pages, "page", "pages")
			}
			img = card(w, h, "PDF", pages)
		}
	}
	sum := md5.Sum(data)
	p := &picture{Data: data, Ext: ext, W: m.W, H: m.H, MD5: base64.StdEncoding.EncodeToString(sum[:]),
		DurationMS: m.DurationMS, Pages: m.Pages}
	if p.Thumb, p.TnW, p.TnH, err = thumbnail(img, side, bg); err != nil {
		return nil, err
	}
	return p, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// ---- thumbnails ----

// helperTimeout is how long ffmpeg or pdftoppm may take over a file.
const helperTimeout = 30 * time.Second

// frame is the picture a helper makes of a file: ffmpeg's of a video's
// first frame, pdftoppm's of a PDF's first page (nil, nil: no helper).
func (s *Server) frame(data []byte, ext string) (image.Image, error) {
	var tool string
	switch ext {
	case ".webm", ".mp4", ".flv":
		tool = s.S.FFmpeg
	case ".pdf":
		tool = s.S.PDFToPPM
	}
	if tool == "" {
		return nil, nil
	}
	decodeSlots <- struct{}{}
	defer func() { <-decodeSlots }()
	dir, err := os.MkdirTemp("", "chan")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "in"+ext)
	if err := os.WriteFile(in, data, 0600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), helperTimeout)
	defer cancel()
	var cmd *exec.Cmd
	out := filepath.Join(dir, "out")
	if ext == ".pdf" {
		cmd = exec.CommandContext(ctx, tool, "-f", "1", "-l", "1", "-png", "-scale-to", "500", "-singlefile", in, out)
	} else {
		// the container named, not guessed: nothing but the file is read
		format := map[string]string{".webm": "matroska", ".mp4": "mov", ".flv": "flv"}[ext]
		cmd = exec.CommandContext(ctx, tool, "-nostdin", "-v", "error", "-protocol_whitelist", "file",
			"-f", format, "-i", in, "-frames:v", "1", "-an", "-vf", "scale='min(500,iw)':-2", "-f", "image2", out+".png")
	}
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %v: %s", filepath.Base(tool), err, strings.TrimSpace(stderr.String()))
	}
	f, err := os.ReadFile(out + ".png")
	if err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(f))
}

// card is a thumbnail for a file there is no picture of: dark, saying
// what it is (WEBM 0:12, PDF 3 pages), w by h.
func card(w, h int, lines ...string) image.Image {
	w, h = max(w, 60), max(h, 40)
	small := image.NewRGBA(image.Rect(0, 0, w/2, h/2))
	draw.Draw(small, small.Bounds(), image.NewUniform(color.RGBA{0x22, 0x22, 0x2a, 0xff}), image.Point{}, draw.Src)
	d := &font.Drawer{Dst: small, Src: image.NewUniform(color.RGBA{0xee, 0xee, 0xee, 0xff}), Face: basicfont.Face7x13}
	top := (h/2 - len(lines)*14) / 2
	for i, l := range lines {
		d.Dot = fixed.P((w/2-d.MeasureString(l).Round())/2, top+11+i*14)
		d.DrawString(l)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.NearestNeighbor.Scale(img, img.Bounds(), small, small.Bounds(), draw.Src, nil)
	return img
}

// clock is a length as 4chan shows it: 1:05, 1:02:03.
func clock(ms int64) string {
	s := ms / 1000
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// fileKind is what a post's file is, for its page: "video", "pdf" or
// "image".
func fileKind(ext string) string {
	switch ext {
	case ".webm", ".mp4":
		return "video"
	case ".pdf":
		return "pdf"
	case ".swf", ".flv":
		return "flash"
	}
	return "image"
}
