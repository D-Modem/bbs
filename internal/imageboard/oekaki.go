package imageboard

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"regexp"
)

// Oekaki (お絵かき, doodling): a board's posters can draw their picture in
// the page instead of uploading one, as the Futaba boards' Java painters
// let them.  The drawing is posted as a PNG like any other image, and
// carries how it was drawn: its strokes, in a chunk of its own that other
// programs pass over, so that the picture can be replayed stroke by
// stroke, and drawn over, wherever the file goes.
//
// The chunk is "okST" (ancillary, private, not safe to copy: the strokes
// are of this picture), its data zlib-compressed JSON:
//
//	{"v":1, "w":400, "h":300, "bg":"#ffffff",
//	 "s":[{"t":"p", "c":"#000000", "z":3, "p":[dt,x,y, dt,x,y, ...]}, ...]}
//
// A stroke is the pen ("p"), the eraser ("e": the pen in bg) or a fill
// ("f": one point).  dt is milliseconds since the stroke's last point, or
// for its first since the stroke before ended: their sum is the time the
// picture took.

// replayChunk is the chunk's type.
const replayChunk = "okST"

const (
	maxReplayChunk  = 8 << 20  // compressed
	maxReplayJSON   = 32 << 20 // uncompressed
	maxReplayPoints = 1 << 20  // numbers, three a point
	maxStrokes      = 100000
	maxDT           = 10 * 60 * 1000
	maxBrush        = 100
	minCanvas       = 50
	maxCanvas       = 1200
)

type stroke struct {
	T string `json:"t"`
	C string `json:"c"`
	Z int    `json:"z"`
	P []int  `json:"p"`
}

type replay struct {
	V  int      `json:"v"`
	W  int      `json:"w"`
	H  int      `json:"h"`
	BG string   `json:"bg"`
	S  []stroke `json:"s"`
}

var colorPat = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// check says what is wrong with a replay of a w by h picture, and how long
// the picture took to draw.
func (rp *replay) check(w, h int) (int64, error) {
	switch {
	case rp.V != 1:
		return 0, errors.New("not version 1")
	case rp.W != w || rp.H != h:
		return 0, errors.New("not this picture's size")
	case !colorPat.MatchString(rp.BG):
		return 0, errors.New("no background")
	case len(rp.S) == 0 || len(rp.S) > maxStrokes:
		return 0, errors.New("no strokes, or too many")
	}
	var ms int64
	points := 0
	for _, s := range rp.S {
		if !colorPat.MatchString(s.C) || s.Z < 1 || s.Z > maxBrush || len(s.P) < 3 || len(s.P)%3 != 0 {
			return 0, errors.New("a stroke is not one")
		}
		switch s.T {
		case "p", "e":
		case "f":
			if len(s.P) != 3 || s.P[1] < 0 || s.P[1] >= w || s.P[2] < 0 || s.P[2] >= h {
				return 0, errors.New("a fill is not on the picture")
			}
		default:
			return 0, errors.New("a stroke of no tool")
		}
		if points += len(s.P); points > maxReplayPoints {
			return 0, errors.New("too many points")
		}
		for i := 0; i < len(s.P); i += 3 {
			dt, x, y := s.P[i], s.P[i+1], s.P[i+2]
			if dt < 0 || dt > maxDT || x < -s.Z || x > w+s.Z || y < -s.Z || y > h+s.Z {
				return 0, errors.New("a point is not on the picture")
			}
			ms += int64(dt)
		}
	}
	return ms, nil
}

// pngReplay finds a PNG's replay and checks it against the picture (w by
// h): the PNG (the chunk taken out if it is not good: the picture stays),
// how long it took to draw, and whether it has a good one.
func pngReplay(data []byte, w, h int) ([]byte, int64, bool) {
	chunks, err := pngChunks(data)
	if err != nil {
		return data, 0, false
	}
	var found []pngChunk
	for _, c := range chunks {
		if c.typ == replayChunk {
			found = append(found, c)
		}
	}
	if len(found) == 0 {
		return data, 0, false
	}
	if len(found) == 1 {
		if rp, err := readReplay(found[0].data); err == nil {
			if ms, err := rp.check(w, h); err == nil {
				return data, ms, true
			}
		}
	}
	// a bad one, or more than one: the picture without them
	out := append([]byte{}, data[:8]...)
	for _, c := range chunks {
		if c.typ != replayChunk {
			out = append(out, data[c.start:c.end]...)
		}
	}
	return out, 0, false
}

// readReplay reads a replay chunk's data.
func readReplay(data []byte) (*replay, error) {
	if len(data) > maxReplayChunk {
		return nil, errors.New("too big")
	}
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(zr, maxReplayJSON+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxReplayJSON {
		return nil, errors.New("too big")
	}
	var rp replay
	if err := json.Unmarshal(raw, &rp); err != nil {
		return nil, err
	}
	return &rp, nil
}

type pngChunk struct {
	typ        string
	data       []byte
	start, end int // the whole chunk's, in the file
}

// pngChunks splits a PNG into its chunks, their CRCs checked.
func pngChunks(b []byte) ([]pngChunk, error) {
	if !bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) {
		return nil, errors.New("not a PNG")
	}
	var out []pngChunk
	for i := 8; i < len(b); {
		if i+12 > len(b) {
			return nil, errors.New("cut short")
		}
		n := int(binary.BigEndian.Uint32(b[i:]))
		if n < 0 || n > len(b)-i-12 {
			return nil, errors.New("cut short")
		}
		typ := string(b[i+4 : i+8])
		body := b[i+8 : i+8+n]
		if crc32.ChecksumIEEE(b[i+4:i+8+n]) != binary.BigEndian.Uint32(b[i+8+n:]) {
			return nil, errors.New("a chunk's CRC is wrong")
		}
		out = append(out, pngChunk{typ: typ, data: body, start: i, end: i + 12 + n})
		i += 12 + n
		if typ == "IEND" {
			break
		}
	}
	return out, nil
}

// withReplay is a PNG with a replay chunk put in before its end (for
// tests, and for what other programs may want to make).
func withReplay(png []byte, rp *replay) ([]byte, error) {
	j, err := json.Marshal(rp)
	if err != nil {
		return nil, err
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(j)
	zw.Close()
	chunk := make([]byte, 8, 12+z.Len())
	binary.BigEndian.PutUint32(chunk, uint32(z.Len()))
	copy(chunk[4:], replayChunk)
	chunk = append(chunk, z.Bytes()...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	end := len(png) - 12 // IEND
	if end < 8 || string(png[end+4:end+8]) != "IEND" {
		return nil, errors.New("no IEND at the end")
	}
	return append(append(append([]byte{}, png[:end]...), chunk...), png[end:]...), nil
}
