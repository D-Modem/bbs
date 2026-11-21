package xfer

// ZMODEM with zstd (docs/zmodem-zstd.md of the D-Modem repository): the
// receiver tells it can decompress by CANZSTD in its ZRINIT's ZF1, and the
// sender asks for it file by file with ZTZSTD in the ZFILE's ZF2.  The file
// goes as independent zstd frames, one for each chunk of up to 64 KB of the
// file, and every position on the wire is still an offset in the file: a
// ZDATA starts a frame, and the receiver writes (and acknowledges) only
// whole frames, so ZRPOS and a resume work as they do without zstd.

import (
	"encoding/binary"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	canZstd      = 0x40      // ZRINIT ZF1: the receiver decompresses zstd
	ztZstd       = 8         // ZFILE ZF2 (the transport option): the file is in zstd frames
	zstdChunk    = 64 * 1024 // the file's bytes in each frame we send
	zstdMinChunk = 1024      // and the fewest, on a line with many errors
	zstdMaxSize  = 256 * 1024
	zstdMagic    = 0xfd2fb528
)

// Zstd lets this side compress (as a sender) and decompress (as a
// receiver) ZMODEM transfers with zstd, where the other side can too.
var Zstd = true

// precompressed are the extensions of files that zstd does not make
// smaller: sent as they are.
var precompressed = map[string]bool{
	".zip": true, ".gz": true, ".tgz": true, ".bz2": true, ".xz": true, ".zst": true, ".lz": true,
	".lzma": true, ".7z": true, ".rar": true, ".arj": true, ".arc": true, ".lzh": true, ".lha": true,
	".zoo": true, ".pak": true, ".sqz": true, ".cab": true, ".z": true, ".jpg": true, ".jpeg": true,
	".png": true, ".gif": true, ".webp": true, ".mp3": true, ".ogg": true, ".opus": true, ".flac": true,
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".webm": true, ".jar": true, ".apk": true,
	".docx": true, ".xlsx": true, ".pptx": true, ".odt": true, ".epub": true, ".pdf": true,
}

// worthCompressing tells whether a file of this name is likely to get
// smaller with zstd.
func worthCompressing(name string) bool {
	return !precompressed[strings.ToLower(path.Ext(strings.ReplaceAll(name, `\`, "/")))]
}

var (
	zenc, _ = zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderCRC(true),
		zstd.WithSingleSegment(true), zstd.WithZeroFrames(true), zstd.WithEncoderLevel(zstd.SpeedDefault))
	zdec, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(zstdMaxSize), zstd.WithDecoderMaxMemory(zstdMaxSize))
)

// sampleCompresses tells whether the start of a file gets at least 5%
// smaller with zstd (and goes back to its start); true if it cannot tell.
func sampleCompresses(r io.Reader) bool {
	s, ok := r.(io.ReadSeeker)
	if !ok {
		return true
	}
	buf := make([]byte, zstdChunk)
	n, _ := io.ReadFull(s, buf)
	if _, err := s.Seek(0, io.SeekStart); err != nil {
		return true
	}
	return n > 0 && len(zstdFrame(buf[:n]))*100 <= n*95
}

// zstdFrame is chunk as one zstd frame: its content size and checksum in
// it, no dictionary.
func zstdFrame(chunk []byte) []byte { return zenc.EncodeAll(chunk, nil) }

// zstdFrameLen is the length of the zstd frame at the start of b, once all
// of it is there: 0 while it is not, an error if it is not a frame we take.
func zstdFrameLen(b []byte) (int, error) {
	if len(b) < 5 {
		return 0, nil
	}
	if binary.LittleEndian.Uint32(b) != zstdMagic {
		return 0, fmt.Errorf("%w: not a zstd frame", ErrProtocol)
	}
	fhd := b[4]
	if fhd&0x08 != 0 {
		return 0, fmt.Errorf("%w: zstd reserved bit", ErrProtocol)
	}
	if fhd&0x03 != 0 {
		return 0, fmt.Errorf("%w: a zstd dictionary", ErrProtocol)
	}
	n := 5
	single := fhd&0x20 != 0
	if !single {
		n++ // the window descriptor
	}
	switch fhd >> 6 {
	case 0:
		if single {
			n++
		}
	case 1:
		n += 2
	case 2:
		n += 4
	case 3:
		n += 8
	}
	for {
		if len(b) < n+3 {
			return 0, nil
		}
		bh := uint32(b[n]) | uint32(b[n+1])<<8 | uint32(b[n+2])<<16
		last, typ, size := bh&1 != 0, (bh>>1)&3, int(bh>>3)
		n += 3
		switch typ {
		case 0, 2: // raw, compressed
			if typ == 2 && size > 128*1024 {
				return 0, fmt.Errorf("%w: zstd block too large", ErrProtocol)
			}
			n += size
		case 1: // RLE
			n++
		default:
			return 0, fmt.Errorf("%w: zstd reserved block", ErrProtocol)
		}
		if last {
			break
		}
		if n > zstdMaxSize+zstdMaxSize/8 {
			return 0, fmt.Errorf("%w: zstd frame too large", ErrProtocol)
		}
	}
	if fhd&0x04 != 0 {
		n += 4 // the content checksum
	}
	if len(b) < n {
		return 0, nil
	}
	return n, nil
}

// zstdDecode is a whole frame's content, checked: its size given and at
// most zstdMaxSize, its checksum right.
func zstdDecode(frame []byte) ([]byte, error) {
	h := zstd.Header{}
	if err := h.Decode(frame); err != nil || !h.HasFCS || h.FrameContentSize > zstdMaxSize || !h.HasCheckSum {
		return nil, fmt.Errorf("%w: a zstd frame we do not take", ErrProtocol)
	}
	out, err := zdec.DecodeAll(frame, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: zstd: %v", ErrProtocol, err)
	}
	if uint64(len(out)) != h.FrameContentSize {
		return nil, fmt.Errorf("%w: zstd frame of the wrong size", ErrProtocol)
	}
	return out, nil
}
