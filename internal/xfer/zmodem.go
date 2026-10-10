package xfer

// ZMODEM, after Chuck Forsberg's "The ZMODEM Inter Application File
// Transfer Protocol" (1988) and lrzsz.  Headers are hex or binary (CRC-16
// or CRC-32), data subpackets are ZDLE-escaped.  We send in segments: a
// run of ZCRCG subpackets ended by ZCRCW, whose ZACK (or ZRPOS) we wait
// for, so no reading is needed while streaming and an error costs at most
// a segment.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	zpad   = '*'
	zdle   = 0x18
	zbin   = 'A'
	zhex   = 'B'
	zbin32 = 'C'

	zrqinit = 0
	zrinit  = 1
	zsinit  = 2
	zack    = 3
	zfile   = 4
	zskip   = 5
	znak    = 6
	zabort  = 7
	zfin    = 8
	zrpos   = 9
	zdata   = 10
	zeof    = 11
	zferr   = 12
	zcrc    = 13
	zcompl  = 15
	zcan    = 16
	zfreecn = 17
	zcomm   = 18

	zcrce = 'h' // end of frame; a header follows
	zcrcg = 'i' // frame goes on
	zcrcq = 'j' // frame goes on; ZACK wanted
	zcrcw = 'k' // end of frame; ZACK wanted
	zrub0 = 'l'
	zrub1 = 'm'

	// ZRINIT flags (ZF0)
	canfdx  = 0x01
	canovio = 0x02
	canfc32 = 0x20

	// results of zgetc beyond bytes
	gotCRCE = 0x100 | zcrce
	gotCRCG = 0x100 | zcrcg
	gotCRCQ = 0x100 | zcrcq
	gotCRCW = 0x100 | zcrcw
	gotCAN  = 0x200
)

const (
	zSubpacket = 1024      // what we send in a subpacket
	zMaxSub    = 8192      // the most we take in one
	zSegment   = 32 * 1024 // bytes between ZCRCW acknowledgements
	zWait      = 10 * time.Second
	zRetries   = 10
	// bytes we skip hunting for a header: after an error the sender may
	// finish its segment before it hears of it
	zGarbage = 4 * zSegment
)

type zline struct {
	*line
	crc32 bool // send binary headers and data with CRC-32
}

// zgetc reads a byte undoing ZDLE escapes: a byte, a frame end (gotCRC*),
// or gotCAN for five CANs in a row.  Unescaped XON and XOFF are dropped.
func (z *zline) zgetc(d time.Duration) (int, error) {
	for {
		b, err := z.getc(d)
		if err != nil {
			return 0, err
		}
		switch b {
		case 0x11, 0x13, 0x91, 0x93:
			continue
		case zdle:
		default:
			return int(b), nil
		}
		cans := 1
		for {
			b, err = z.getc(d)
			if err != nil {
				return 0, err
			}
			switch b {
			case zdle:
				if cans++; cans >= 5 {
					return gotCAN, nil
				}
				continue
			case 0x11, 0x13, 0x91, 0x93:
				continue
			case zcrce, zcrcg, zcrcq, zcrcw:
				return 0x100 | int(b), nil
			case zrub0:
				return 0x7f, nil
			case zrub1:
				return 0xff, nil
			}
			if b&0x60 == 0x40 {
				return int(b ^ 0x40), nil
			}
			return 0, fmt.Errorf("%w: bad escape %#x", ErrProtocol, b)
		}
	}
}

// escape appends b to out, ZDLE-escaped where it must be: ZDLE, DLE, XON,
// XOFF (and their high-bit forms) and CR, so that nothing on the way (a
// modem's flow control, telnet, an rlogin) takes them.
func escape(out []byte, b ...byte) []byte {
	for _, c := range b {
		switch c {
		case zdle, 0x10, 0x90, 0x11, 0x91, 0x13, 0x93, 0x0d, 0x8d:
			out = append(out, zdle, c^0x40)
		default:
			out = append(out, c)
		}
	}
	return out
}

// header is a frame's type and its four bytes (position, or flags).
type header struct {
	typ byte
	p   [4]byte
}

func posHeader(typ byte, pos int64) header {
	var h header
	h.typ = typ
	binary.LittleEndian.PutUint32(h.p[:], uint32(pos))
	return h
}

func (h header) pos() int64 { return int64(binary.LittleEndian.Uint32(h.p[:])) }

const hexdigits = "0123456789abcdef"

// sendHex sends a hex header.
func (z *zline) sendHex(h header) error {
	raw := append([]byte{h.typ}, h.p[:]...)
	c := crc16(raw, 0)
	raw = append(raw, byte(c>>8), byte(c))
	out := []byte{zpad, zpad, zdle, zhex}
	for _, b := range raw {
		out = append(out, hexdigits[b>>4], hexdigits[b&15])
	}
	out = append(out, 0x0d, 0x8a)
	if h.typ != zfin && h.typ != zack {
		out = append(out, 0x11)
	}
	return z.write(out)
}

// sendBin sends a binary header, CRC-32 if agreed.
func (z *zline) sendBin(h header) error {
	raw := append([]byte{h.typ}, h.p[:]...)
	out := []byte{zpad, zdle}
	if z.crc32 {
		out = append(out, zbin32)
		c := crc32.ChecksumIEEE(raw)
		out = escape(out, raw...)
		out = escape(out, byte(c), byte(c>>8), byte(c>>16), byte(c>>24))
	} else {
		out = append(out, zbin)
		c := crc16(raw, 0)
		out = escape(out, raw...)
		out = escape(out, byte(c>>8), byte(c))
	}
	return z.write(out)
}

// subpacket builds a data subpacket ending in end.
func (z *zline) subpacket(data []byte, end byte) []byte {
	out := escape(make([]byte, 0, len(data)+len(data)/8+16), data...)
	out = append(out, zdle, end)
	if z.crc32 {
		c := crc32.Update(crc32.ChecksumIEEE(data), crc32.IEEETable, []byte{end})
		out = escape(out, byte(c), byte(c>>8), byte(c>>16), byte(c>>24))
	} else {
		c := crc16([]byte{end}, crc16(data, 0))
		out = escape(out, byte(c>>8), byte(c))
	}
	return out
}

// recvHeader hunts for a header and reads it; crc32 tells whether the
// data after it (if any) has CRC-32s.
func (z *zline) recvHeader(d time.Duration) (header, bool, error) {
	var h header
	skipped := 0
	for {
		b, err := z.getc(d)
		if err != nil {
			return h, false, err
		}
		if b == can {
			// five CANs (ZDLEs) cancel
			n := 1
			for n < 5 {
				if b, err = z.getc(time.Second); err != nil || b != can {
					break
				}
				n++
			}
			if n >= 5 {
				return h, false, ErrCancelled
			}
		}
		if b != zpad {
			if skipped++; skipped > zGarbage {
				return h, false, fmt.Errorf("%w: no header", ErrProtocol)
			}
			continue
		}
		for b == zpad {
			if b, err = z.getc(d); err != nil {
				return h, false, err
			}
		}
		if b != zdle {
			continue
		}
		kind, err := z.getc(d)
		if err != nil {
			return h, false, err
		}
		switch kind {
		case zhex:
			var hex [14]byte
			if err := z.read(hex[:], d); err != nil {
				return h, false, err
			}
			var raw [7]byte
			ok := true
			for i := range raw {
				hi := strings.IndexByte(hexdigits, hex[2*i])
				lo := strings.IndexByte(hexdigits, hex[2*i+1])
				if hi < 0 || lo < 0 {
					ok = false
					break
				}
				raw[i] = byte(hi<<4 | lo)
			}
			if !ok || crc16(raw[:5], 0) != uint16(raw[5])<<8|uint16(raw[6]) {
				continue // a damaged header: the next will come
			}
			h.typ = raw[0]
			copy(h.p[:], raw[1:5])
			return h, false, nil
		case zbin, zbin32:
			n := 7
			if kind == zbin32 {
				n = 9
			}
			raw := make([]byte, n)
			bad := false
			for i := range raw {
				c, err := z.zgetc(d)
				if err != nil {
					return h, false, err
				}
				if c > 0xff {
					bad = true
					break
				}
				raw[i] = byte(c)
			}
			if bad {
				continue
			}
			if kind == zbin32 {
				if crc32.ChecksumIEEE(raw[:5]) != binary.LittleEndian.Uint32(raw[5:9]) {
					continue
				}
			} else if crc16(raw[:5], 0) != uint16(raw[5])<<8|uint16(raw[6]) {
				continue
			}
			h.typ = raw[0]
			copy(h.p[:], raw[1:5])
			return h, kind == zbin32, nil
		}
	}
}

// recvData reads a data subpacket: its data and how it ended (gotCRC*).
func (z *zline) recvData(crc32ed bool, d time.Duration) ([]byte, int, error) {
	var data []byte
	for {
		c, err := z.zgetc(d)
		if err != nil {
			return nil, 0, err
		}
		if c == gotCAN {
			return nil, 0, ErrCancelled
		}
		if c <= 0xff {
			if len(data) >= zMaxSub {
				return nil, 0, fmt.Errorf("%w: subpacket too long", ErrProtocol)
			}
			data = append(data, byte(c))
			continue
		}
		end := byte(c & 0xff)
		n := 2
		if crc32ed {
			n = 4
		}
		var crc [4]byte
		for i := 0; i < n; i++ {
			v, err := z.zgetc(d)
			if err != nil {
				return nil, 0, err
			}
			if v > 0xff {
				return nil, 0, fmt.Errorf("%w: bad CRC", ErrProtocol)
			}
			crc[i] = byte(v)
		}
		if crc32ed {
			want := crc32.Update(crc32.ChecksumIEEE(data), crc32.IEEETable, []byte{end})
			if binary.LittleEndian.Uint32(crc[:]) != want {
				return nil, 0, fmt.Errorf("%w: bad CRC", ErrProtocol)
			}
		} else if crc16([]byte{end}, crc16(data, 0)) != uint16(crc[0])<<8|uint16(crc[1]) {
			return nil, 0, fmt.Errorf("%w: bad CRC", ErrProtocol)
		}
		return data, c, nil
	}
}

// zcancel sends the ZMODEM abort: eight CANs and backspaces.
func (z *zline) zcancel() {
	z.write([]byte{zdle, zdle, zdle, zdle, zdle, zdle, zdle, zdle, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8})
}

// SendZMODEM sends files by ZMODEM.
func SendZMODEM(p Port, files []Outgoing) error {
	z := &zline{line: newLine(p)}
	// "rz\r" starts a terminal program's receiver by itself
	z.write([]byte("rz\r"))
	if err := z.sendHex(header{typ: zrqinit}); err != nil {
		return err
	}
	// the receiver's ZRINIT
	for try := 0; ; try++ {
		h, _, err := z.recvHeader(zWait)
		if errors.Is(err, ErrTimeout) && try < zRetries {
			z.sendHex(header{typ: zrqinit})
			continue
		}
		if err != nil {
			return err
		}
		if h.typ == zrinit {
			z.crc32 = h.p[3]&canfc32 != 0
			break
		}
		if h.typ == zabort || h.typ == zcan {
			return ErrCancelled
		}
		if try >= zRetries {
			z.zcancel()
			return ErrRetries
		}
		z.sendHex(header{typ: zrqinit})
	}
	for i, f := range files {
		if err := z.sendFile(f, len(files)-i); err != nil {
			z.zcancel()
			return err
		}
	}
	// ZFIN, the receiver's ZFIN, "OO"
	for try := 0; try < zRetries; try++ {
		z.sendHex(header{typ: zfin})
		h, _, err := z.recvHeader(zWait)
		if err == nil && h.typ == zfin {
			z.write([]byte("OO"))
			return nil
		}
		if err != nil && !errors.Is(err, ErrTimeout) {
			return err
		}
	}
	return nil // the files are there; only the goodbye was lost
}

// seeker is what a ZRPOS needs of a file being sent.
type seeker interface {
	io.Reader
	io.Seeker
}

func (z *zline) sendFile(f Outgoing, left int) error {
	info := fmt.Sprintf("%d %o 0 0 %d", f.Size, f.ModTime.Unix(), left)
	meta := append(append([]byte(f.Name), 0), info...)
	meta = append(meta, 0)
	var pos int64
	for try := 0; ; try++ {
		if try >= zRetries {
			return ErrRetries
		}
		if err := z.sendBin(header{typ: zfile}); err != nil {
			return err
		}
		if err := z.write(z.subpacket(meta, zcrcw)); err != nil {
			return err
		}
		h, _, err := z.recvHeader(zWait)
		if errors.Is(err, ErrTimeout) {
			continue
		}
		if err != nil {
			return err
		}
		switch h.typ {
		case zrpos:
			pos = h.pos()
		case zskip:
			return nil
		case zrinit:
			continue // it missed the ZFILE
		case zabort, zferr, zcan:
			return ErrCancelled
		default:
			continue
		}
		break
	}
	r := f.Data
	buf := make([]byte, zSubpacket)
	errs := 0
	lastRpos := int64(-1)
	for {
		// a segment from pos
		if pos != 0 || errs > 0 {
			s, ok := r.(seeker)
			if !ok {
				return fmt.Errorf("%w: cannot go back in the file", ErrProtocol)
			}
			if _, err := s.Seek(pos, io.SeekStart); err != nil {
				return err
			}
		}
		if err := z.sendBin(posHeader(zdata, pos)); err != nil {
			return err
		}
		sent := 0
		eof := false
		var out bytes.Buffer
		for {
			n, err := io.ReadFull(r, buf)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				eof = true
			} else if err != nil {
				return err
			}
			end := byte(zcrcg)
			if eof {
				end = zcrce
			} else if sent+n >= zSegment {
				end = zcrcw
			}
			out.Write(z.subpacket(buf[:n], end))
			pos += int64(n)
			sent += n
			if out.Len() >= 4096 || end != zcrcg {
				if err := z.write(out.Bytes()); err != nil {
					return err
				}
				out.Reset()
			}
			if end != zcrcg {
				break
			}
		}
		if eof {
			if err := z.sendBin(posHeader(zeof, pos)); err != nil {
				return err
			}
		}
		// the answer: ZACK (segment taken), ZRPOS (go back), ZRINIT (file
		// taken), ZSKIP
	answer:
		for {
			h, _, err := z.recvHeader(zWait)
			if errors.Is(err, ErrTimeout) {
				if errs++; errs > zRetries {
					return ErrRetries
				}
				if eof {
					z.sendBin(posHeader(zeof, pos))
					continue
				}
				// lost: go back to the start of the segment
				pos -= int64(sent)
				break answer
			}
			if err != nil {
				return err
			}
			switch h.typ {
			case zack:
				if !eof && h.pos() == pos {
					errs = 0
					break answer
				}
			case zrpos:
				// errors in a row without getting further are too many
				if h.pos() > lastRpos {
					errs = 0
				}
				lastRpos = h.pos()
				if errs++; errs > zRetries {
					return ErrRetries
				}
				pos = h.pos()
				z.purge(200 * time.Millisecond)
				break answer
			case zrinit:
				if eof {
					return nil
				}
			case zskip:
				return nil
			case zabort, zferr, zcan:
				return ErrCancelled
			}
		}
		if !eof {
			continue
		}
		eof = false
	}
}

// zrinitHeader is our ZRINIT: full duplex, overlapped I/O, CRC-32, no
// buffer limit.
var zrinitHeader = header{typ: zrinit, p: [4]byte{0, 0, 0, canfdx | canovio | canfc32}}

// ReceiveZMODEM receives a ZMODEM batch.
func ReceiveZMODEM(p Port, sink Sink) error {
	z := &zline{line: newLine(p)}
	if err := z.sendHex(zrinitHeader); err != nil {
		return err
	}
	var in Incoming
	var name string
	var pos, size int64
	errs := 0
	fail := func(err error) error {
		if in != nil {
			in.Abort()
		}
		z.zcancel()
		return err
	}
	retry := func() error {
		if errs++; errs > zRetries {
			return fail(ErrRetries)
		}
		z.purge(200 * time.Millisecond)
		if in != nil {
			return z.sendHex(posHeader(zrpos, pos))
		}
		return z.sendHex(zrinitHeader)
	}
	for {
		h, c32, err := z.recvHeader(zWait)
		if errors.Is(err, ErrTimeout) {
			if err := retry(); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return fail(err)
		}
		switch h.typ {
		case zrqinit:
			z.sendHex(zrinitHeader)
		case zsinit:
			// the sender's attention string: taken and ignored
			if _, _, err := z.recvData(c32, zWait); err != nil {
				if err := retry(); err != nil {
					return err
				}
				continue
			}
			z.sendHex(header{typ: zack})
		case zfile:
			data, _, err := z.recvData(c32, zWait)
			if err != nil {
				if err := retry(); err != nil {
					return err
				}
				continue
			}
			n, info, _ := bytes.Cut(data, []byte{0})
			if in != nil && pos == 0 && string(n) == name {
				// the same ZFILE again: our ZRPOS was lost
				z.sendHex(posHeader(zrpos, 0))
				continue
			}
			if in != nil {
				in.Abort()
				in = nil
			}
			name, size = string(n), -1
			if fields := strings.Fields(string(bytes.TrimRight(info, "\x00"))); len(fields) > 0 {
				if v, err := strconv.ParseInt(fields[0], 10, 64); err == nil && v >= 0 {
					size = v
				}
			}
			in, err = sink(name, size)
			if err != nil {
				// refused: on to the next
				in = nil
				z.sendHex(header{typ: zskip})
				continue
			}
			pos = 0
			z.sendHex(posHeader(zrpos, 0))
		case zdata:
			if in == nil {
				z.sendHex(zrinitHeader)
				continue
			}
			if h.pos() != pos {
				if err := retry(); err != nil {
					return err
				}
				continue
			}
			// subpackets until the frame ends
			for {
				data, end, err := z.recvData(c32, zWait)
				if errors.Is(err, ErrCancelled) {
					return fail(err)
				}
				if err != nil {
					if err := retry(); err != nil {
						return err
					}
					break
				}
				if size >= 0 && pos+int64(len(data)) > size {
					return fail(fmt.Errorf("%w: more data than the file's size", ErrProtocol))
				}
				if _, err := in.Write(data); err != nil {
					return fail(err)
				}
				pos += int64(len(data))
				errs = 0
				if end == gotCRCQ || end == gotCRCW {
					z.sendHex(posHeader(zack, pos))
				}
				if end == gotCRCE || end == gotCRCW {
					break
				}
			}
		case zeof:
			if in == nil || h.pos() != pos {
				continue // stale: the sender is still coming back
			}
			if err := in.Commit(); err != nil {
				in = nil
				return fail(err)
			}
			in = nil
			z.sendHex(zrinitHeader)
		case zfin:
			z.sendHex(header{typ: zfin})
			// the sender's "OO", if it comes
			z.getc(time.Second)
			z.getc(100 * time.Millisecond)
			return nil
		case zcan, zabort:
			if in != nil {
				in.Abort()
			}
			return ErrCancelled
		case zcomm, zfreecn, zcrc:
			// commands, free space, file CRCs: not here
			z.sendHex(header{typ: znak})
		}
	}
}
