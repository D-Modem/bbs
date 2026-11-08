package oscar

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// FLAP: each frame is '*', its channel, a sequence number and the length
// of its data, then the data.
const (
	flapStart = '*'

	chanSignon    = 1 // the hello, and a login's or a service's cookie
	chanData      = 2 // SNACs
	chanError     = 3
	chanSignoff   = 4 // the close, and a login's answer
	chanKeepAlive = 5

	maxFrame = 0xffff
)

// errFrame: the client sent what is not FLAP.
var errFrame = errors.New("not a FLAP frame")

// frame is a FLAP frame.
type frame struct {
	ch   byte
	data []byte
}

// flapConn is a connection's FLAP: frames read, and written one at a
// time, numbered.
type flapConn struct {
	c   net.Conn
	r   *bufio.Reader
	wmu sync.Mutex
	seq uint16
}

func newFlapConn(c net.Conn, r *bufio.Reader) *flapConn {
	return &flapConn{c: c, r: r, seq: uint16(time.Now().UnixNano() & 0x7fff)}
}

// read reads a frame.
func (f *flapConn) read() (frame, error) {
	var h [6]byte
	if _, err := io.ReadFull(f.r, h[:]); err != nil {
		return frame{}, err
	}
	if h[0] != flapStart {
		return frame{}, errFrame
	}
	n := int(binary.BigEndian.Uint16(h[4:]))
	data := make([]byte, n)
	if _, err := io.ReadFull(f.r, data); err != nil {
		return frame{}, err
	}
	return frame{ch: h[1], data: data}, nil
}

// write writes a frame.
func (f *flapConn) write(ch byte, data []byte) error {
	if len(data) > maxFrame {
		return fmt.Errorf("a frame of %d bytes", len(data))
	}
	f.wmu.Lock()
	defer f.wmu.Unlock()
	b := make([]byte, 6, 6+len(data))
	b[0], b[1] = flapStart, ch
	binary.BigEndian.PutUint16(b[2:], f.seq)
	f.seq++
	binary.BigEndian.PutUint16(b[4:], uint16(len(data)))
	b = append(b, data...)
	f.c.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err := f.c.Write(b)
	return err
}

// hello is the server's signon frame: FLAP version 1.
func (f *flapConn) hello() error { return f.write(chanSignon, []byte{0, 0, 0, 1}) }

// snac sends a SNAC.
func (f *flapConn) snac(fam, sub uint16, flags uint16, req uint32, body []byte) error {
	b := make([]byte, 10, 10+len(body))
	binary.BigEndian.PutUint16(b[0:], fam)
	binary.BigEndian.PutUint16(b[2:], sub)
	binary.BigEndian.PutUint16(b[4:], flags)
	binary.BigEndian.PutUint32(b[6:], req)
	return f.write(chanData, append(b, body...))
}

// signoff closes the connection with a signoff frame (its TLVs: a
// login's answer, or why).
func (f *flapConn) signoff(t tlvs) error { return f.write(chanSignoff, t.bytes()) }

// snacHeader is a SNAC's: its family, subtype, flags and request id.
type snacHeader struct {
	fam, sub uint16
	flags    uint16
	req      uint32
}

// parseSNAC splits a data frame into its SNAC header and body (its
// version TLVs, if flagged, skipped).
func parseSNAC(data []byte) (snacHeader, []byte, error) {
	if len(data) < 10 {
		return snacHeader{}, nil, errors.New("a SNAC too short")
	}
	h := snacHeader{binary.BigEndian.Uint16(data), binary.BigEndian.Uint16(data[2:]),
		binary.BigEndian.Uint16(data[4:]), binary.BigEndian.Uint32(data[6:])}
	body := data[10:]
	if h.flags&0x8000 != 0 && len(body) >= 2 {
		// opt-in TLVs, before the body: skipped
		n := int(binary.BigEndian.Uint16(body))
		if 2+n <= len(body) {
			body = body[2+n:]
		}
	}
	return h, body, nil
}

// buf builds a big-endian body.
type buf []byte

func (b *buf) u8(v byte)    { *b = append(*b, v) }
func (b *buf) u16(v uint16) { *b = binary.BigEndian.AppendUint16(*b, v) }
func (b *buf) u32(v uint32) { *b = binary.BigEndian.AppendUint32(*b, v) }
func (b *buf) raw(p []byte) { *b = append(*b, p...) }

// str8 is a string after its length in a byte (a screen name).
func (b *buf) str8(s string) {
	if len(s) > 255 {
		s = s[:255]
	}
	b.u8(byte(len(s)))
	*b = append(*b, s...)
}

// str16 is a string after its length in two bytes.
func (b *buf) str16(s string) {
	if len(s) > 0xffff {
		s = s[:0xffff]
	}
	b.u16(uint16(len(s)))
	*b = append(*b, s...)
}

// rd reads a big-endian body; reading past its end sets bad and gives
// zeros, so a short SNAC is checked once, at the end.
type rd struct {
	p   []byte
	bad bool
}

func (r *rd) take(n int) []byte {
	if n < 0 || len(r.p) < n {
		r.bad = true
		r.p = nil
		return make([]byte, max(n, 0))
	}
	v := r.p[:n]
	r.p = r.p[n:]
	return v
}

func (r *rd) u8() byte      { return r.take(1)[0] }
func (r *rd) u16() uint16   { return binary.BigEndian.Uint16(r.take(2)) }
func (r *rd) u32() uint32   { return binary.BigEndian.Uint32(r.take(4)) }
func (r *rd) str8() string  { return string(r.take(int(r.u8()))) }
func (r *rd) str16() string { return string(r.take(int(r.u16()))) }
func (r *rd) rest() []byte  { v := r.p; r.p = nil; return v }
func (r *rd) left() int     { return len(r.p) }

// tlv is a type, length, value.
type tlv struct {
	t uint16
	v []byte
}

// tlvs is a list of them.
type tlvs []tlv

// parseTLVs reads TLVs to the end of p (a short last one dropped).
func parseTLVs(p []byte) tlvs {
	var out tlvs
	for len(p) >= 4 {
		t, n := binary.BigEndian.Uint16(p), int(binary.BigEndian.Uint16(p[2:]))
		if 4+n > len(p) {
			break
		}
		out = append(out, tlv{t, p[4 : 4+n]})
		p = p[4+n:]
	}
	return out
}

// readTLVs reads a count of TLVs (a TLV block).
func (r *rd) tlvBlock() tlvs {
	n := int(r.u16())
	var out tlvs
	for i := 0; i < n && !r.bad; i++ {
		t := r.u16()
		v := r.take(int(r.u16()))
		out = append(out, tlv{t, v})
	}
	return out
}

// get is the first TLV of type t.
func (l tlvs) get(t uint16) ([]byte, bool) {
	for _, x := range l {
		if x.t == t {
			return x.v, true
		}
	}
	return nil, false
}

func (l tlvs) has(t uint16) bool { _, ok := l.get(t); return ok }

func (l tlvs) str(t uint16) string { v, _ := l.get(t); return string(v) }

func (l tlvs) u16(t uint16) (uint16, bool) {
	v, ok := l.get(t)
	if !ok || len(v) < 2 {
		return 0, false
	}
	return binary.BigEndian.Uint16(v), true
}

func (l tlvs) u32(t uint16) (uint32, bool) {
	v, ok := l.get(t)
	if !ok || len(v) < 4 {
		return 0, false
	}
	return binary.BigEndian.Uint32(v), true
}

// add appends a TLV.
func (l *tlvs) add(t uint16, v []byte) { *l = append(*l, tlv{t, v}) }

func (l *tlvs) addStr(t uint16, s string) { l.add(t, []byte(s)) }

func (l *tlvs) addU8(t uint16, v byte) { l.add(t, []byte{v}) }

func (l *tlvs) addU16(t uint16, v uint16) { l.add(t, binary.BigEndian.AppendUint16(nil, v)) }

func (l *tlvs) addU32(t uint16, v uint32) { l.add(t, binary.BigEndian.AppendUint32(nil, v)) }

// set replaces the TLVs of type t with one.
func (l *tlvs) set(t uint16, v []byte) {
	out := (*l)[:0]
	for _, x := range *l {
		if x.t != t {
			out = append(out, x)
		}
	}
	*l = append(out, tlv{t, v})
}

// bytes is the TLVs written one after another (a "rest block").
func (l tlvs) bytes() []byte {
	var b buf
	for _, x := range l {
		b.u16(x.t)
		b.u16(uint16(len(x.v)))
		b.raw(x.v)
	}
	return b
}

// block is the TLVs after their count.
func (l tlvs) block() []byte {
	var b buf
	b.u16(uint16(len(l)))
	b.raw(l.bytes())
	return b
}

// lblock is the TLVs after their length in bytes.
func (l tlvs) lblock() []byte {
	p := l.bytes()
	var b buf
	b.u16(uint16(len(p)))
	b.raw(p)
	return b
}
