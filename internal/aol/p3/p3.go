// Package p3 is P3, the link layer under the classic AOL client (3.0, 4.0
// and on): the packets that carry its tokens and atom streams to the host,
// over a modem or inside TCP on port 5190, numbered, checked and
// acknowledged.
//
// A packet is
//
//	5A  crc(2)  length(2)  tx  rx  type  data...  0D
//
// the CRC a CRC-16/ARC of everything from the length to the last data byte,
// the length that of tx, rx, type and data.  DATA packets are numbered by
// tx from 0x10 to 0x7F and round again, and each side's rx acknowledges
// every packet up to the one it names; the rest (ACK, NAK, HEARTBEAT,
// INIT, SS, SSR) repeat the last DATA number.  A sender has at most 16
// packets unacknowledged; a receiver acknowledges at least every 8th; 12
// seconds without one and the sender asks with a HEARTBEAT, up to 10 times.
// The client sets the top bit of the type (A0, A3, A4...), so a packet
// echoed back by a line is told from the host's; the host sends types bare.
//
// The client opens with INIT (its version, its machine) at 7F/7F, and the
// link is up when the host acknowledges it.  AOL 4.0 over TCP stops
// computing CRCs after that and sends "**" in their place.
//
// Reader and Append read and write packets; Link is the host's end of a
// link, which Serve starts on a connection.
//
// Sources: Deceptio's P3 pages (deceptio.org/re/p3), written with the
// help of P3's author; the FDO91 manual's chapter 27; and what Dialtone
// and others have learned serving live clients.
package p3

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// Sync and End begin and end every packet.
const (
	Sync = 0x5A
	End  = 0x0D
)

// The packet types.
const (
	DATA      = 0x20
	SS        = 0x21 // asks for an SSR
	SSR       = 0x22 // answers an SS: resend what follows my rx
	INIT      = 0x23 // the client's first packet
	ACK       = 0x24
	NAK       = 0x25 // data: why (NakCRC, NakSequence, NakLength)
	HEARTBEAT = 0x26 // asks for an ACK

	// FromClient is set in the type of every packet the client sends.
	FromClient = 0x80
)

// A NAK's reason, its one byte of data.
const (
	NakCRC      = 1
	NakSequence = 2
	NakLength   = 3
)

// Sequence numbers.
const (
	SeqMin  = 0x10
	SeqMax  = 0x7F
	SeqInit = 0x7F // both sides' tx and rx before any DATA
	seqRing = SeqMax - SeqMin + 1
)

// Sizes.
const (
	// MaxLength is the largest length a packet may have (1,024 bytes in
	// all); MaxData is the most data in one packet.
	MaxLength = 0x3FA
	MaxData   = MaxLength - 3

	// MaxClientData is the most the client puts in one packet.
	MaxClientData = 0x77

	headerSize = 8
)

// noCRC is what AOL 4.0 over TCP sends for a CRC.
const noCRC = 0x2A2A

// Packet is a P3 packet.
type Packet struct {
	TX, RX uint8
	Type   uint8 // as sent: the client's have FromClient set
	Data   []byte
	NoCRC  bool // read: its CRC was "**", not checked
}

// Kind is p's type without FromClient.
func (p Packet) Kind() uint8 { return p.Type &^ FromClient }

// FromClient is whether the client sent p.
func (p Packet) FromClient() bool { return p.Type&FromClient != 0 }

func (p Packet) String() string {
	name, ok := typeNames[p.Kind()]
	if !ok {
		name = fmt.Sprintf("type %02x", p.Type)
	}
	s := fmt.Sprintf("%s tx=%02x rx=%02x", name, p.TX, p.RX)
	if p.Kind() == DATA && len(p.Data) >= 2 {
		s += fmt.Sprintf(" %q", p.Data[:2])
	}
	if len(p.Data) > 0 {
		s += fmt.Sprintf(" (%d bytes)", len(p.Data))
	}
	return s
}

var typeNames = map[uint8]string{
	DATA: "DATA", SS: "SS", SSR: "SSR", INIT: "INIT", ACK: "ACK", NAK: "NAK", HEARTBEAT: "HEARTBEAT",
}

// CRC is P3's CRC: CRC-16/ARC (reflected 0x8005, from zero).
func CRC(b []byte) uint16 {
	var c uint16
	for _, x := range b {
		c = c>>8 ^ crcTable[byte(c)^x]
	}
	return c
}

var crcTable = func() (t [256]uint16) {
	for i := range t {
		c := uint16(i)
		for range 8 {
			if c&1 != 0 {
				c = c>>1 ^ 0xA001
			} else {
				c >>= 1
			}
		}
		t[i] = c
	}
	return
}()

// Append appends p as bytes to b, with its length and CRC.  It panics on
// more than MaxData of data.
func Append(b []byte, p Packet) []byte {
	if len(p.Data) > MaxData {
		panic(fmt.Sprintf("p3: %d bytes of data in one packet", len(p.Data)))
	}
	n := len(p.Data) + 3
	start := len(b)
	b = append(b, Sync, 0, 0, byte(n>>8), byte(n), p.TX, p.RX, p.Type)
	b = append(b, p.Data...)
	c := CRC(b[start+3:])
	b[start+1], b[start+2] = byte(c>>8), byte(c)
	return append(b, End)
}

// Errors reading packets.  A packet with ErrCRC or ErrLength is to be
// NAKed; the Reader has passed over it and goes on with the next.
var (
	ErrCRC    = errors.New("p3: bad CRC")
	ErrLength = errors.New("p3: bad length")
)

// Reader reads packets.
type Reader struct {
	r       *bufio.Reader
	Skipped int // bytes passed over looking for a packet
}

// NewReader reads packets from r.
func NewReader(r io.Reader) *Reader {
	br, ok := r.(*bufio.Reader)
	if !ok || br.Size() < MaxLength+6 {
		br = bufio.NewReaderSize(r, 4096)
	}
	return &Reader{r: br}
}

// Read reads the next packet.  Bytes before a Sync are passed over; a
// packet whose CRC is wrong is ErrCRC, one too long or short ErrLength.
// A packet without its End is taken as it is.
func (r *Reader) Read() (Packet, error) {
	for {
		c, err := r.r.ReadByte()
		if err != nil {
			return Packet{}, err
		}
		if c == Sync {
			break
		}
		r.Skipped++
	}
	h, err := r.r.Peek(4)
	if err != nil {
		return Packet{}, unexpected(err)
	}
	n := int(h[2])<<8 | int(h[3])
	if n < 3 || n > MaxLength {
		// line noise, most likely: look for the next Sync from here
		return Packet{}, ErrLength
	}
	b, err := r.r.Peek(4 + n)
	if err != nil {
		return Packet{}, unexpected(err)
	}
	crc := uint16(b[0])<<8 | uint16(b[1])
	p := Packet{TX: b[4], RX: b[5], Type: b[6], NoCRC: crc == noCRC}
	ok := p.NoCRC || CRC(b[2:]) == crc
	p.Data = append([]byte(nil), b[7:]...)
	r.r.Discard(4 + n)
	if e, err := r.r.Peek(1); err == nil && e[0] == End {
		r.r.Discard(1)
	}
	if !ok {
		return p, ErrCRC
	}
	return p, nil
}

func unexpected(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// Sniff is whether b begins as a P3 client's first packet, an INIT.
func Sniff(b []byte) bool {
	return len(b) >= headerSize && b[0] == Sync && b[7]&^FromClient == INIT
}

// Token splits a DATA packet's data into its token (two letters, "Dd",
// "Aa"...) and what follows it.
func Token(data []byte) (string, []byte) {
	if len(data) < 2 {
		return "", data
	}
	return string(data[:2]), data[2:]
}

// next is the sequence number after s.
func next(s uint8) uint8 {
	if s >= SeqMax || s < SeqMin {
		return SeqMin
	}
	return s + 1
}

// behind is how far s is behind t, round the ring: 0 if s is t.
func behind(s, t uint8) int {
	return ((int(t)-int(s))%seqRing + seqRing) % seqRing
}
