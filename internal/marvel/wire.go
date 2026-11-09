package marvel

import (
	"encoding/binary"
	"errors"
)

// The serial link (MOSCP.EXE's "Select" protocol): each packet is
//
//	SeqNo AckNo Payload CRC-32 0x0D
//
// with the payload byte-stuffed behind escape 0x1B, the two header bytes
// XORed out of the way of the control bytes, and the CRC's own bytes
// masked the same way.  A packet never holds a raw 0x0D before its end.

const (
	terminator = 0x0D
	escape     = 0x1B
	ackOnly    = 0x41 // SeqNo of a packet that only acknowledges
	nakOnly    = 0x42 // SeqNo of a negative acknowledgement
	crcPoly    = 0x248EF9BE
)

// stuffed is what each byte the payload may not carry raw is sent as,
// after the escape.
var stuffed = map[byte]byte{0x1B: 0x30, 0x0D: 0x31, 0x10: 0x32, 0x0B: 0x33, 0x8D: 0x34, 0x90: 0x35, 0x8B: 0x36}

var unstuffed = func() map[byte]byte {
	m := map[byte]byte{}
	for raw, code := range stuffed {
		m[code] = raw
	}
	return m
}()

var crcTable = func() (t [256]uint32) {
	for i := range t {
		c := uint32(i)
		for range 8 {
			if c&1 != 0 {
				c = ^((c ^ crcPoly) >> 1)
			} else {
				c >>= 1
			}
		}
		t[i] = c
	}
	return
}()

// crc32 is the link's CRC: the polynomial above, from 0, no final XOR.
func crc32(b []byte) uint32 {
	var c uint32
	for _, x := range b {
		c = c>>8 ^ crcTable[(x^byte(c))&0xFF]
	}
	return c
}

// headerByte encodes a SeqNo or AckNo (bit 7 already set): the three
// values that are link controls move by XOR 0xC0.
func headerByte(v byte) byte {
	switch v {
	case 0x8D, 0x90, 0x8B:
		return v ^ 0xC0
	}
	return v
}

// unheaderByte undoes headerByte.
func unheaderByte(v byte) byte {
	switch v {
	case 0x8D ^ 0xC0, 0x90 ^ 0xC0, 0x8B ^ 0xC0:
		return v ^ 0xC0
	}
	return v
}

// stuff escapes a payload.
func stuff(b []byte) []byte {
	out := make([]byte, 0, len(b)+len(b)/16)
	for _, x := range b {
		if code, ok := stuffed[x]; ok {
			out = append(out, escape, code)
			continue
		}
		out = append(out, x)
	}
	return out
}

// unstuff undoes stuff; an escape at the very end is dropped.
func unstuff(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == escape && i+1 < len(b) {
			i++
			if raw, ok := unstuffed[b[i]]; ok {
				out = append(out, raw)
			} else {
				out = append(out, b[i])
			}
			continue
		}
		if b[i] == escape {
			break
		}
		out = append(out, b[i])
	}
	return out
}

// maskCRC moves the CRC bytes that are link controls (OR 0x60).
func maskCRC(c uint32) [4]byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], c)
	for i, x := range b {
		if _, ok := stuffed[x]; ok {
			b[i] = x | 0x60
		}
	}
	return b
}

// sealPacket adds the CRC and the terminator to a packet's encoded header
// and payload.
func sealPacket(wire []byte) []byte {
	m := maskCRC(crc32(wire))
	return append(append(wire, m[:]...), terminator)
}

// dataPacket is a packet carrying payload (raw: it is stuffed here).
func dataPacket(seq, ack byte, payload []byte) []byte {
	wire := []byte{headerByte(seq&0x7F | 0x80), headerByte(ack&0x7F | 0x80)}
	return sealPacket(append(wire, stuff(payload)...))
}

// ackPacket acknowledges everything before ack.
func ackPacket(ack byte) []byte {
	return sealPacket([]byte{ackOnly, headerByte(ack&0x7F | 0x80)})
}

// packet is one received packet.
type packet struct {
	kind    byte // 'D' data, 'A' acknowledgement, 'N' negative
	seq     byte // data only
	ack     byte
	stuffed []byte // the payload as it came: still stuffed
}

var errShort = errors.New("short packet")
var errCRC = errors.New("bad CRC")

// parsePacket reads one packet, its terminator taken off.
func parsePacket(raw []byte) (packet, error) {
	if len(raw) < 6 {
		return packet{}, errShort
	}
	var p packet
	switch raw[0] {
	case ackOnly:
		p.kind = 'A'
	case nakOnly:
		p.kind = 'N'
	default:
		p.kind = 'D'
		p.seq = unheaderByte(raw[0]) & 0x7F
	}
	p.ack = unheaderByte(raw[1]) & 0x7F
	body := raw[:len(raw)-4]
	if m := maskCRC(crc32(body)); string(m[:]) != string(raw[len(raw)-4:]) {
		return p, errCRC
	}
	p.stuffed = raw[2 : len(raw)-4]
	return p, nil
}

// The TCP link (ENGCT.EXE's "Straight" protocol) carries each whole pipe
// message as a record: a uint16 length that counts itself, a command byte
// (a copy of what the message says, so ignored), then the message.  TCP
// keeps order, so there is nothing else: no sequence numbers or CRC.

const straightHeader = 3

// straightRecord frames one message.
func straightRecord(msg []byte) []byte {
	out := make([]byte, straightHeader, straightHeader+len(msg))
	binary.LittleEndian.PutUint16(out, uint16(straightHeader+len(msg)))
	return append(out, msg...)
}

// takeStraight takes the whole records off the front of buf: the messages,
// and what is left (a part record).  A length too small to be a record
// can't be resynchronised on, so it is an error.
func takeStraight(buf []byte) (msgs [][]byte, rest []byte, err error) {
	for len(buf) >= straightHeader {
		n := int(binary.LittleEndian.Uint16(buf))
		if n < straightHeader {
			return msgs, nil, errors.New("bad record length")
		}
		if len(buf) < n {
			break
		}
		msgs = append(msgs, append([]byte(nil), buf[straightHeader:n]...))
		buf = buf[n:]
	}
	return msgs, buf, nil
}
