package imageboard

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// Flash, played by Ruffle (ruffle.rs) where [chan] ruffle names its files:
// SWF movies, and FLV videos (which Ruffle plays in a movie of ours that
// streams them, as Flash players did).  Each runs in a sandboxed frame of
// its own, without the page's origin; a movie may not reach the network
// or the page's scripts.

// ---- SWF ----

// maxSWF is the most a compressed movie may unpack to.
const maxSWF = 128 << 20

// readSWF reads a movie's header: its stage, frame rate and frames; and
// whether it has sounds.  Its tags are walked as it unpacks, nothing
// kept, so a small file that unpacks to much costs no memory.
func readSWF(b []byte) (*media, error) {
	if len(b) < 8 {
		return nil, errFlash
	}
	length := int64(binary.LittleEndian.Uint32(b[4:8]))
	if length < 21 || length > maxSWF {
		return nil, errFlash
	}
	var in io.Reader
	switch string(b[:3]) {
	case "FWS":
		in = bytes.NewReader(b[8:])
	case "CWS":
		z, err := zlib.NewReader(bytes.NewReader(b[8:]))
		if err != nil {
			return nil, errFlash
		}
		in = z
	case "ZWS":
		// LZMA: the stage is packed with the rest; Ruffle unpacks it
		return &media{W: 550, H: 400, Sound: true}, nil
	default:
		return nil, errFlash
	}
	body := bufio.NewReaderSize(io.LimitReader(in, length-8), 64)
	head, _ := body.Peek(21) // a RECT is 17 bytes at most; then rate and frames
	r := bitReader{b: head}
	n := int(r.bits(5))
	var v [4]int64
	for i := range v {
		v[i] = r.signed(n)
	}
	at := r.byteOffset()
	if r.err || at+4 > len(head) {
		return nil, errFlash
	}
	m := &media{W: int((v[1] - v[0]) / 20), H: int((v[3] - v[2]) / 20)}
	rate := float64(binary.LittleEndian.Uint16(head[at:])) / 256
	frames := int(binary.LittleEndian.Uint16(head[at+2:]))
	if rate > 0 && frames > 1 {
		m.DurationMS = int64(float64(frames) / rate * 1000)
	}
	read := int64(8 + at + 4)
	body.Discard(at + 4)
	// the tags: sounds, and that they run to their end
	var hb [6]byte
	for {
		if _, err := io.ReadFull(body, hb[:2]); err != nil {
			break
		}
		h := binary.LittleEndian.Uint16(hb[:])
		code, size := int(h>>6), int64(h&0x3f)
		read += 2
		if size == 0x3f {
			if _, err := io.ReadFull(body, hb[2:6]); err != nil {
				break
			}
			size = int64(binary.LittleEndian.Uint32(hb[2:6]))
			read += 4
		}
		if code == 0 {
			break
		}
		switch code {
		case 14, 18, 45: // DefineSound, SoundStreamHead, SoundStreamHead2
			m.Sound = true
		}
		got, _ := io.CopyN(io.Discard, body, size)
		read += got
		if got < size {
			break
		}
	}
	if read < length-64 { // cut short (some tools pad; none fall short by much)
		return nil, errors.New("That Flash movie is cut short.")
	}
	if m.W < 1 || m.H < 1 || m.W > 8192 || m.H > 8192 {
		return nil, errFlash
	}
	return m, nil
}

var errFlash = errors.New("That Flash movie could not be read.")

// bitReader reads a SWF's bit fields, most significant first.
type bitReader struct {
	b   []byte
	pos int // in bits
	err bool
}

func (r *bitReader) bits(n int) uint64 {
	var v uint64
	for ; n > 0; n-- {
		if r.pos/8 >= len(r.b) {
			r.err = true
			return 0
		}
		v = v<<1 | uint64(r.b[r.pos/8]>>(7-r.pos%8)&1)
		r.pos++
	}
	return v
}

func (r *bitReader) signed(n int) int64 {
	if n == 0 {
		return 0
	}
	v := int64(r.bits(n))
	if v&(1<<(n-1)) != 0 {
		v -= 1 << n
	}
	return v
}

func (r *bitReader) byteOffset() int { return (r.pos + 7) / 8 }

// ---- FLV ----

// readFLV reads a video's header, its metadata (onMetaData: size,
// length) and its tags (its codecs; its length, if the metadata does not
// say).
func readFLV(b []byte) (*media, error) {
	if len(b) < 13 || string(b[:3]) != "FLV" || b[3] != 1 {
		return nil, errFLV
	}
	off := int(binary.BigEndian.Uint32(b[5:9]))
	if off < 9 || off+4 > len(b) {
		return nil, errFLV
	}
	m := &media{}
	var meta map[string]any
	var last uint32
	video, tags := -1, 0
	for i := off + 4; i+11 <= len(b); {
		typ := b[i] & 0x1f
		size := int(b[i+1])<<16 | int(b[i+2])<<8 | int(b[i+3])
		ts := uint32(b[i+4])<<16 | uint32(b[i+5])<<8 | uint32(b[i+6]) | uint32(b[i+7])<<24
		data := i + 11
		if size > len(b)-data {
			break // cut short at the end: what is there plays
		}
		d := b[data : data+size]
		switch typ {
		case 8:
			m.Sound = true
		case 9:
			if video < 0 && len(d) > 0 {
				video = int(d[0] & 0x0f)
			}
		case 18:
			if meta == nil {
				meta = amfMeta(d)
			}
		}
		last = max(last, ts)
		tags++
		i = data + size + 4
	}
	switch {
	case tags == 0:
		return nil, errFLV
	case video < 0:
		return nil, errors.New("That FLV has no video in it.")
	case video < 2 || video > 7:
		return nil, fmt.Errorf("That FLV's video is in a codec Flash did not have (%d).", video)
	}
	num := func(k string) float64 {
		f, _ := meta[k].(float64)
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 1e9 {
			return 0
		}
		return f
	}
	m.W, m.H = int(num("width")), int(num("height"))
	if d := num("duration"); d > 0 {
		m.DurationMS = int64(d * 1000)
	} else {
		m.DurationMS = int64(last)
	}
	if m.W < 1 || m.H < 1 {
		m.W, m.H = 320, 240 // not said: Flash's own default
	}
	return m, nil
}

var errFLV = errors.New("That FLV could not be read.")

// amfMeta reads an onMetaData script tag (AMF0) for its top level's
// numbers and strings.
func amfMeta(d []byte) map[string]any {
	r := amf{b: d}
	if name, ok := r.value(0).(string); !ok || name != "onMetaData" {
		return nil
	}
	out, _ := r.value(0).(map[string]any)
	return out
}

type amf struct {
	b   []byte
	i   int
	bad bool
}

func (r *amf) take(n int) []byte {
	if r.bad || n < 0 || r.i+n > len(r.b) {
		r.bad = true
		return nil
	}
	s := r.b[r.i : r.i+n]
	r.i += n
	return s
}

func (r *amf) str(long bool) string {
	n := 2
	if long {
		n = 4
	}
	h := r.take(n)
	if h == nil {
		return ""
	}
	if long {
		return string(r.take(int(binary.BigEndian.Uint32(h))))
	}
	return string(r.take(int(binary.BigEndian.Uint16(h))))
}

// value reads one AMF0 value: numbers, booleans and strings as they are,
// objects and arrays as maps (nested ones read and passed over).
func (r *amf) value(depth int) any {
	t := r.take(1)
	if t == nil || depth > 16 {
		r.bad = true
		return nil
	}
	switch t[0] {
	case 0: // number
		if b := r.take(8); b != nil {
			return math.Float64frombits(binary.BigEndian.Uint64(b))
		}
	case 1: // boolean
		if b := r.take(1); b != nil {
			return b[0] != 0
		}
	case 2:
		return r.str(false)
	case 12:
		return r.str(true)
	case 3, 8: // object, ECMA array
		if t[0] == 8 {
			r.take(4)
		}
		out := map[string]any{}
		for !r.bad && len(out) < 4096 {
			k := r.str(false)
			if k == "" {
				if e := r.take(1); e == nil || e[0] != 9 {
					r.bad = true
				}
				break
			}
			out[k] = r.value(depth + 1)
		}
		return out
	case 10: // strict array
		h := r.take(4)
		if h == nil {
			return nil
		}
		for n := binary.BigEndian.Uint32(h); n > 0 && !r.bad; n-- {
			r.value(depth + 1)
		}
	case 11: // date
		r.take(10)
	case 5, 6: // null, undefined
	default:
		r.bad = true
	}
	return nil
}

// ---- the FLV player: a movie of ours ----

// flvPlayer is a movie w by h pixels that plays the FLV its src
// parameter names, stretched to its stage:
//
//	nc = new NetConnection(); nc.connect(null);
//	ns = new NetStream(nc); vid.attachVideo(ns); ns.play(src);
func flvPlayer(w, h int) []byte {
	w, h = min(max(w, 16), 4096), min(max(h, 16), 4096)
	var tags bytes.Buffer
	tag := func(code int, body []byte) {
		if len(body) < 0x3f {
			binary.Write(&tags, binary.LittleEndian, uint16(code<<6|len(body)))
		} else {
			binary.Write(&tags, binary.LittleEndian, uint16(code<<6|0x3f))
			binary.Write(&tags, binary.LittleEndian, uint32(len(body)))
		}
		tags.Write(body)
	}
	le16 := func(v int) []byte { return []byte{byte(v), byte(v >> 8)} }
	tag(69, []byte{0, 0, 0, 0}) // FileAttributes: AVM1
	tag(9, []byte{0, 0, 0})     // SetBackgroundColor: black
	// DefineVideoStream 1: no frames of its own, w by h, smoothed
	tag(60, append(append(append(le16(1), le16(0)...), append(le16(w), le16(h)...)...), 0x01, 0))
	// PlaceObject2: it, at depth 1, named vid
	tag(26, append(append([]byte{0x26}, append(le16(1), le16(1)...)...), append([]byte{0x00}, "vid\x00"...)...))
	var as bytes.Buffer
	push := func(items ...any) {
		var p bytes.Buffer
		for _, it := range items {
			switch v := it.(type) {
			case string:
				p.WriteByte(0)
				p.WriteString(v)
				p.WriteByte(0)
			case int:
				p.WriteByte(7)
				binary.Write(&p, binary.LittleEndian, uint32(v))
			case nil:
				p.WriteByte(2)
			}
		}
		as.WriteByte(0x96)
		as.Write(le16(p.Len()))
		as.Write(p.Bytes())
	}
	const (
		getVar     = 0x1c
		setVar     = 0x1d
		newObject  = 0x40
		callMethod = 0x52
		pop        = 0x17
	)
	push("nc", 0, "NetConnection")
	as.Write([]byte{newObject, setVar})
	push(nil, 1, "nc")
	as.WriteByte(getVar)
	push("connect")
	as.Write([]byte{callMethod, pop})
	push("ns", "nc")
	as.WriteByte(getVar)
	push(1, "NetStream")
	as.Write([]byte{newObject, setVar})
	push("ns")
	as.WriteByte(getVar)
	push(1, "vid")
	as.WriteByte(getVar)
	push("attachVideo")
	as.Write([]byte{callMethod, pop})
	push("src")
	as.WriteByte(getVar)
	push(1, "ns")
	as.WriteByte(getVar)
	push("play")
	as.Write([]byte{callMethod, pop})
	as.WriteByte(0) // end
	tag(12, as.Bytes())
	tag(1, nil) // ShowFrame
	tag(0, nil) // End

	// the stage: a RECT of four 16-bit fields, in twips
	var rect bytes.Buffer
	bw := bitWriter{w: &rect}
	bw.put(16, 5)
	for _, v := range []int{0, w * 20, 0, h * 20} {
		bw.put(uint64(v), 16)
	}
	bw.flush()
	var out bytes.Buffer
	out.WriteString("FWS")
	out.WriteByte(8)
	binary.Write(&out, binary.LittleEndian, uint32(8+rect.Len()+4+tags.Len()))
	out.Write(rect.Bytes())
	out.Write([]byte{0, 30}) // 30 frames a second
	out.Write(le16(1))       // one frame
	out.Write(tags.Bytes())
	return out.Bytes()
}

type bitWriter struct {
	w   *bytes.Buffer
	cur byte
	n   int
}

func (b *bitWriter) put(v uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		b.cur = b.cur<<1 | byte(v>>i&1)
		if b.n++; b.n == 8 {
			b.w.WriteByte(b.cur)
			b.cur, b.n = 0, 0
		}
	}
}

func (b *bitWriter) flush() {
	if b.n > 0 {
		b.w.WriteByte(b.cur << (8 - b.n))
		b.cur, b.n = 0, 0
	}
}

// ---- serving ----

// ruffleName is a file of Ruffle's self-hosted package: no directories.
var ruffleName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.(js|wasm|map)$`)

// serveRuffle serves Ruffle's files, from [chan] ruffle.  The player's
// frame is sandboxed, of no origin, so they are fetched from elsewhere:
// anyone may (they are Ruffle's, the same for every site).
func (s *Server) serveRuffle(q *req, name string) {
	if s.conf().Ruffle == "" || !ruffleName.MatchString(name) {
		q.notFound()
		return
	}
	f, err := os.Open(filepath.Join(s.conf().Ruffle, name))
	if err != nil {
		q.notFound()
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		q.notFound()
		return
	}
	h := q.w.Header()
	h.Set("Content-Type", map[string]string{".js": "text/javascript", ".wasm": "application/wasm",
		".map": "application/json"}[filepath.Ext(name)])
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'")
	h.Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(q.w, q.r, "", fi.ModTime(), f)
}

// flashPage is the player's frame: Ruffle, and a script that takes the
// movie from the page that framed it.  It is sandboxed (allow-scripts
// alone): no origin of ours, no cookies, nothing of the page's.
const flashPage = `<!doctype html>
<html><head><meta charset="utf-8"><title>Flash</title>
<link rel="stylesheet" href="/chan/static/flash.css">
<script src="/chan/static/flash.js"></script>
<script src="/chan/ruffle/ruffle.js"></script>
</head><body><div id="player"></div></body></html>
`

func (s *Server) serveFlashPage(q *req) {
	if s.conf().Ruffle == "" {
		q.notFound()
		return
	}
	h := q.w.Header()
	h.Del("X-Frame-Options")
	h.Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; "+
		"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' data: blob:; "+
		"connect-src 'self' data: blob:; worker-src 'self' blob:; font-src 'self' data:; "+
		"frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "public, max-age=3600")
	io.WriteString(q.w, flashPage)
}

// serveFLVPlayer is the movie that plays an FLV, for a stage w by h.
func (s *Server) serveFLVPlayer(q *req) {
	w, _ := strconv.Atoi(q.r.URL.Query().Get("w"))
	hh, _ := strconv.Atoi(q.r.URL.Query().Get("h"))
	h := q.w.Header()
	h.Set("Content-Type", "application/x-shockwave-flash")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'")
	h.Set("Cache-Control", "public, max-age=86400")
	q.w.Write(flvPlayer(w, hh))
}
