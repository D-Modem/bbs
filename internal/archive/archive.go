// Package archive reads the archives FidoNet moves mail and files in, in
// Go and without other programs: ZIP, ARC (stored, packed, squeezed,
// crunched and squashed), ARJ (methods 0 to 4) and LHA (-lh0-, -lh4- to
// -lh7-, header levels 0 to 2).  It writes ZIP, and ARC, ARJ and LHA
// stored (each format's own "no compression"), which every unpacker
// takes.
//
// Archives come from other systems: every size is checked before memory
// is given to it, every entry against its CRC, and nothing is written to
// disk here.
package archive

import (
	"bytes"
	"errors"
	"hash/crc32"
	"io"
	"strings"
	"time"
)

// Kind is an archive's format.
type Kind string

// The formats.
const (
	ZIP Kind = "zip"
	ARC Kind = "arc"
	ARJ Kind = "arj"
	LHA Kind = "lha"
)

// Kinds are the formats read, in the order Detect tries them.
var Kinds = []Kind{ZIP, ARJ, LHA, ARC}

// Errors.
var (
	ErrFormat   = errors.New("not an archive this BBS reads (ZIP, ARC, ARJ, LHA)")
	ErrCorrupt  = errors.New("the archive is damaged")
	ErrChecksum = errors.New("an entry does not match its CRC")
	ErrMethod   = errors.New("an entry is packed by a method this BBS does not unpack")
	ErrTooBig   = errors.New("an entry unpacks too big")
)

// Entry is a file in an archive.
type Entry struct {
	Name     string // as the archive has it (it may have a path: check it)
	Size     int64  // unpacked
	Packed   int64
	Method   string // "stored", "crunched", "-lh5-", "method 4"...
	Modified time.Time
	Dir      bool
}

// Detect is an archive's format by its first bytes ("": none of them).
func Detect(head []byte) Kind {
	switch {
	case len(head) >= 4 && string(head[:4]) == "PK\x03\x04", len(head) >= 4 && string(head[:4]) == "PK\x05\x06":
		return ZIP
	case len(head) >= 4 && head[0] == 0x60 && head[1] == 0xEA:
		return ARJ
	case len(head) >= 7 && head[2] == '-' && head[6] == '-' && (head[3] == 'l') && (head[4] == 'h' || head[4] == 'z'):
		return LHA
	case len(head) >= 2 && head[0] == 0x1A && head[1] <= 11:
		return ARC
	}
	return ""
}

// KindByName is the format a file's name says ("": none of them).
func KindByName(name string) Kind {
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".zip"):
		return ZIP
	case strings.HasSuffix(n, ".arc"):
		return ARC
	case strings.HasSuffix(n, ".arj"):
		return ARJ
	case strings.HasSuffix(n, ".lzh"), strings.HasSuffix(n, ".lha"):
		return LHA
	}
	return ""
}

// Opener unpacks an entry: its contents, checked against its CRC, or an
// error.  max is the most it may unpack to (ErrTooBig past it).
type Opener func(max int64) ([]byte, error)

// Walk calls fn for each entry of the archive in r (size bytes long), in
// order, until fn returns false or the archive ends.
func Walk(r io.ReaderAt, size int64, fn func(e Entry, open Opener) bool) error {
	head := make([]byte, 8)
	n, _ := r.ReadAt(head, 0)
	switch Detect(head[:n]) {
	case ZIP:
		return walkZIP(r, size, fn)
	case ARC:
		return walkARC(r, size, fn)
	case ARJ:
		return walkARJ(r, size, fn)
	case LHA:
		return walkLHA(r, size, fn)
	}
	return ErrFormat
}

// WalkBytes is Walk over an archive in memory.
func WalkBytes(b []byte, fn func(e Entry, open Opener) bool) error {
	return Walk(bytes.NewReader(b), int64(len(b)), fn)
}

// readAt reads exactly n bytes at off, or ErrCorrupt.
func readAt(r io.ReaderAt, size, off, n int64) ([]byte, error) {
	if off < 0 || n < 0 || off+n > size {
		return nil, ErrCorrupt
	}
	b := make([]byte, n)
	if _, err := r.ReadAt(b, off); err != nil && !(err == io.EOF && off+n == size) {
		return nil, ErrCorrupt
	}
	return b, nil
}

// dosTime is an MS-DOS date and time (as ARC, ARJ and LHA level 0 and 1
// keep them), in local time as DOS knew no other.
func dosTime(date, tm uint16) time.Time {
	if date == 0 {
		return time.Time{}
	}
	return time.Date(int(date>>9)+1980, time.Month(date>>5&15), int(date&31),
		int(tm>>11), int(tm>>5&63), int(tm&31)*2, 0, time.Local)
}

// dosStamp is a time as DOS's date and time.
func dosStamp(t time.Time) (date, tm uint16) {
	if t.Year() < 1980 {
		return 1<<5 | 1, 0 // 1980-01-01
	}
	return uint16((t.Year()-1980)<<9 | int(t.Month())<<5 | t.Day()),
		uint16(t.Hour()<<11 | t.Minute()<<5 | t.Second()/2)
}

// crc16 is the CRC ARC and LHA keep (CRC-16/ARC: 0xA001 reflected, from 0).
func crc16(b []byte) uint16 {
	var c uint16
	for _, x := range b {
		c ^= uint16(x)
		for i := 0; i < 8; i++ {
			if c&1 != 0 {
				c = c>>1 ^ 0xA001
			} else {
				c >>= 1
			}
		}
	}
	return c
}

// crc32 is the CRC ARJ keeps (the usual one).
func crc32sum(b []byte) uint32 { return crc32.ChecksumIEEE(b) }

func le16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }
func le32(b []byte) uint32 { return uint32(le16(b)) | uint32(le16(b[2:]))<<16 }

func put16(b []byte, v uint16) { b[0], b[1] = byte(v), byte(v>>8) }
func put32(b []byte, v uint32) { put16(b, uint16(v)); put16(b[2:], uint16(v>>16)) }

// cString is a NUL-ended string's text.
func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// sized is an Opener's start: refuse what is too big before taking any
// memory for it.
func sized(size, max int64) error {
	if size < 0 || size > max {
		return ErrTooBig
	}
	return nil
}
