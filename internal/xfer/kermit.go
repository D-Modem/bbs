package xfer

// Kermit, after Frank da Cruz's "Kermit Protocol Manual" (6th edition):
// short packets (up to 94), one at a time, control quoting, 8th-bit
// prefixing when the other side asks for it, repeat counts, and block
// checks 1, 2 or 3 as agreed.

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	kMark = 0x01 // SOH
	// the longest packet: 94 by the book, though C-Kermit sends 95 (LEN
	// DEL) to one that asked for 94, so we take that and ask for 90
	kMaxl     = 94
	kTakeMaxl = 95
	kAskMaxl  = 90
	kRetries  = 10
	kTimeout  = 10 * time.Second
	kInitWait = 60 * time.Second // for the other side to begin
)

// tochar makes a number 0 to 94 a printable character, as Kermit sends it.
func tochar(x int) byte { return byte(x + 32) }

// unchar is the number a tochar character stands for.
func unchar(b byte) int { return int(b) - 32 }

// ctl toggles a control character to its printable form and back.
func ctl(b byte) byte { return b ^ 64 }

// kparams are a side's Send-Init parameters, and what was agreed.
type kparams struct {
	maxl int  // the longest packet the other side takes
	time int  // its timeout
	npad int  // padding it wants
	padc byte // and of what
	eol  byte // what ends its packets
	qctl byte // control prefix
	qbin byte // 8th-bit prefix ('Y': will if asked, 'N': will not)
	chkt byte // block check '1', '2', '3'
	rept byte // repeat prefix, ' ' for none
}

var ourParams = kparams{maxl: kAskMaxl, time: 10, npad: 0, padc: 0, eol: 0x0d,
	qctl: '#', qbin: 'Y', chkt: '3', rept: '~'}

// encode is the parameters as a Send-Init's (or its ACK's) data.
func (p kparams) encode() []byte {
	return []byte{tochar(p.maxl), tochar(p.time), tochar(p.npad), ctl(p.padc), tochar(int(p.eol)),
		p.qctl, p.qbin, p.chkt, p.rept}
}

// decodeParams are the other side's parameters from its Send-Init (or its
// ACK), the book's defaults for those it leaves out; a maxl under 10 is
// not taken, and one over kMaxl is kMaxl.
func decodeParams(d []byte) kparams {
	p := kparams{maxl: 80, time: 5, eol: 0x0d, qctl: '#', qbin: 'N', chkt: '1', rept: ' '}
	get := func(i int) (byte, bool) {
		if i < len(d) && d[i] != ' ' {
			return d[i], true
		}
		return 0, false
	}
	if b, ok := get(0); ok && unchar(b) >= 10 {
		p.maxl = min(unchar(b), kMaxl)
	}
	if b, ok := get(1); ok {
		p.time = unchar(b)
	}
	if b, ok := get(2); ok {
		p.npad = unchar(b)
	}
	if b, ok := get(3); ok {
		p.padc = ctl(b)
	}
	if b, ok := get(4); ok {
		p.eol = byte(unchar(b))
	}
	if b, ok := get(5); ok {
		p.qctl = b
	}
	if b, ok := get(6); ok {
		p.qbin = b
	}
	if b, ok := get(7); ok {
		p.chkt = b
	}
	if b, ok := get(8); ok {
		p.rept = b
	}
	return p
}

// kermit is a transfer's state.
type kermit struct {
	l     *line
	peer  kparams // the other side's
	chkt  byte    // agreed block check
	qbin  byte    // agreed 8th-bit prefix (0: none)
	rept  byte    // agreed repeat prefix (0: none)
	seq   int
	qctl  byte // the control prefix we send with
	pqctl byte // the one the other side sends with
}

// agree works out what both sides take: from the S (asSender false: we
// receive), or from the ACK to ours.
func (k *kermit) agree(theirs kparams, asSender bool) {
	k.peer = theirs
	k.chkt = '1'
	switch {
	case asSender && theirs.chkt == ourParams.chkt:
		// we asked; the receiver agreed by giving the same
		k.chkt = theirs.chkt
	case !asSender && (theirs.chkt == '2' || theirs.chkt == '3'):
		// the sender asks for one we have
		k.chkt = theirs.chkt
	}
	k.qbin = 0
	switch {
	case isPrefix(theirs.qbin):
		k.qbin = theirs.qbin
	case theirs.qbin == 'Y' && isPrefix(ourParams.qbin):
		k.qbin = ourParams.qbin
	}
	k.rept = 0
	if theirs.rept == ourParams.rept && isPrefix(theirs.rept) {
		k.rept = theirs.rept
	}
	k.qctl = ourParams.qctl
	k.pqctl = theirs.qctl
}

// isPrefix reports whether b may be a prefix character: printable, 33 to
// 62 or 96 to 126.
func isPrefix(b byte) bool { return (b > 32 && b < 63) || (b > 95 && b < 127) }

// check computes a packet's block check over s.
func check(s []byte, chkt byte) []byte {
	switch chkt {
	case '2':
		sum := 0
		for _, b := range s {
			sum += int(b)
		}
		return []byte{tochar((sum >> 6) & 63), tochar(sum & 63)}
	case '3':
		var crc uint16
		for _, b := range s {
			c := crc ^ uint16(b)
			crc = (crc >> 8) ^ (crcTable(c & 0xff))
		}
		return []byte{tochar(int(crc>>12) & 0x0f), tochar(int(crc>>6) & 0x3f), tochar(int(crc) & 0x3f)}
	}
	sum := 0
	for _, b := range s {
		sum += int(b)
	}
	return []byte{tochar((sum + ((sum & 192) >> 6)) & 63)}
}

// crcTable is CRC-CCITT reflected (poly 0x8408), byte at a time.
func crcTable(b uint16) uint16 {
	crc := b
	for i := 0; i < 8; i++ {
		if crc&1 != 0 {
			crc = crc>>1 ^ 0x8408
		} else {
			crc >>= 1
		}
	}
	return crc
}

// checkLen is how many characters a block check of type chkt takes.
func checkLen(chkt byte) int {
	switch chkt {
	case '2':
		return 2
	case '3':
		return 3
	}
	return 1
}

// sendPacket sends a packet with the block check chkt.
func (k *kermit) sendPacket(typ byte, seq int, data []byte, chkt byte) error {
	body := []byte{tochar(len(data) + 2 + checkLen(chkt)), tochar(seq % 64), typ}
	body = append(body, data...)
	out := make([]byte, 0, len(body)+8)
	for i := 0; i < k.peer.npad && i < 16; i++ {
		out = append(out, k.peer.padc)
	}
	out = append(out, kMark)
	out = append(out, body...)
	out = append(out, check(body, chkt)...)
	eol := k.peer.eol
	if eol == 0 {
		eol = 0x0d
	}
	out = append(out, eol)
	return k.l.write(out)
}

// packet is a packet received.
type packet struct {
	typ  byte
	seq  int
	data []byte
}

var errBadPacket = errors.New("kermit: bad packet")

// recvPacket reads a packet, checked with chkt.
func (k *kermit) recvPacket(chkt byte, d time.Duration) (packet, error) {
	skipped := 0
	for {
		b, err := k.l.getc(d)
		if err != nil {
			return packet{}, err
		}
		if b != kMark {
			if skipped++; skipped > 4096 {
				return packet{}, errBadPacket
			}
			continue
		}
		lb, err := k.l.getc(d)
		if err != nil {
			return packet{}, err
		}
		if lb == kMark {
			k.l.r-- // a new packet starts: go round again
			continue
		}
		n := unchar(lb)
		if n < 3 || n > kTakeMaxl {
			return packet{}, errBadPacket
		}
		rest := make([]byte, n)
		if err := k.l.read(rest, d); err != nil {
			return packet{}, err
		}
		cl := checkLen(chkt)
		// an S, or its ACK, comes with block check 1 whatever is agreed
		if n >= 3 && (rest[1] == 'S' || rest[1] == 'I') {
			cl, chkt = 1, '1'
		}
		if n < 2+cl {
			return packet{}, errBadPacket
		}
		body := append([]byte{lb}, rest[:n-cl]...)
		if !bytes.Equal(check(body, chkt), rest[n-cl:]) {
			return packet{}, errBadPacket
		}
		return packet{typ: rest[1], seq: unchar(rest[0]), data: rest[2 : n-cl]}, nil
	}
}

// encode packs data bytes into packet data of at most room bytes; it
// returns the packet data and how many bytes of data it took.
func (k *kermit) encode(data []byte, room int) ([]byte, int) {
	var out []byte
	i := 0
	for i < len(data) {
		var enc []byte
		b := data[i]
		run := 1
		if k.rept != 0 {
			for i+run < len(data) && data[i+run] == b && run < 94 {
				run++
			}
			if run < 3 {
				run = 1
			}
		}
		if run > 1 {
			enc = append(enc, k.rept, tochar(run))
		}
		if k.qbin != 0 && b&0x80 != 0 {
			enc = append(enc, k.qbin)
			b &= 0x7f
		}
		c := b & 0x7f
		switch {
		case c < 32 || c == 127:
			enc = append(enc, k.qctl, ctl(b))
		case c == k.qctl || (k.qbin != 0 && c == k.qbin) || (k.rept != 0 && c == k.rept):
			enc = append(enc, k.qctl, b)
		default:
			enc = append(enc, b)
		}
		if len(out)+len(enc) > room {
			break
		}
		out = append(out, enc...)
		i += run
	}
	return out, i
}

// decode unpacks packet data.
func (k *kermit) decode(d []byte) ([]byte, error) {
	var out []byte
	for i := 0; i < len(d); {
		run := 1
		if k.rept != 0 && d[i] == k.rept {
			if i+1 >= len(d) {
				return nil, errBadPacket
			}
			run = unchar(d[i+1])
			i += 2
			if run < 0 || run > 94 || i >= len(d) {
				return nil, errBadPacket
			}
		}
		high := byte(0)
		if k.qbin != 0 && d[i] == k.qbin {
			high = 0x80
			if i++; i >= len(d) {
				return nil, errBadPacket
			}
		}
		b := d[i]
		if d[i] == k.pqctl {
			if i++; i >= len(d) {
				return nil, errBadPacket
			}
			b = d[i]
			if c := b & 0x7f; c >= 63 && c <= 95 {
				b = ctl(b)
			}
		}
		i++
		b |= high
		for j := 0; j < run; j++ {
			out = append(out, b)
		}
	}
	return out, nil
}

// exchange sends a packet and waits for its ACK (data of the ACK).
func (k *kermit) exchange(typ byte, data []byte, chkt byte) ([]byte, error) {
	for try := 0; try < kRetries; try++ {
		if err := k.sendPacket(typ, k.seq, data, chkt); err != nil {
			return nil, err
		}
		d := kTimeout
		if typ == 'S' {
			d = 5 * time.Second
		}
		p, err := k.recvPacket(k.chkt, d)
		if errors.Is(err, ErrTimeout) || errors.Is(err, errBadPacket) {
			continue
		}
		if err != nil {
			return nil, err
		}
		switch {
		case p.typ == 'E':
			return nil, fmt.Errorf("%w: %s", ErrCancelled, p.data)
		case p.typ == 'Y' && p.seq == k.seq%64:
			k.seq++
			return p.data, nil
		case p.typ == 'N' && p.seq == (k.seq+1)%64:
			// a NAK for the next is an ACK for this
			k.seq++
			return nil, nil
		}
	}
	return nil, ErrRetries
}

// fail tells the other side the transfer is over, with an Error packet
// saying why.
func (k *kermit) fail(msg string) {
	k.sendPacket('E', k.seq, []byte(msg), k.chkt)
}

// SendKermit sends files by Kermit.
func SendKermit(p Port, files []Outgoing) error {
	k := &kermit{l: newLine(p), peer: kparams{eol: 0x0d}, chkt: '1'}
	// Send-Init, until the other side starts (it may still be starting)
	var ack []byte
	var err error
	end := time.Now().Add(kInitWait)
	for time.Now().Before(end) {
		ack, err = k.exchange('S', ourParams.encode(), '1')
		if err == nil || !errors.Is(err, ErrRetries) {
			break
		}
	}
	if err != nil {
		return err
	}
	k.agree(decodeParams(ack), true)
	for _, f := range files {
		if err := k.sendFile(f); err != nil {
			k.fail(err.Error())
			return err
		}
	}
	_, err = k.exchange('B', nil, k.chkt)
	return err
}

// room is how much data a packet to the other side holds: its longest, less
// the sequence, type and block check.
func (k *kermit) room() int { return k.peer.maxl - 2 - checkLen(k.chkt) }

// sendFile sends a file: its name, its size, its data and its end.  A file
// the receiver refuses by its size, or stops by an X or Z in an ACK, is
// ended as discarded (Z with D), and that is not an error.
func (k *kermit) sendFile(f Outgoing) error {
	name, _ := k.encode([]byte(f.Name), k.room())
	if _, err := k.exchange('F', name, k.chkt); err != nil {
		return err
	}
	// attributes: the size, which lets the other side refuse in time
	size := strconv.FormatInt(f.Size, 10)
	attr := append([]byte{'1', tochar(len(size))}, size...)
	reply, err := k.exchange('A', attr, k.chkt)
	if err != nil {
		return err
	}
	if len(reply) > 0 && reply[0] == 'N' {
		_, err := k.exchange('Z', []byte("D"), k.chkt)
		return err
	}
	buf := make([]byte, 4096)
	var pending []byte
	for {
		n, rerr := f.Data.Read(buf)
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 && (len(pending) >= 200 || rerr != nil) {
			d, used := k.encode(pending, k.room())
			reply, err := k.exchange('D', d, k.chkt)
			if err != nil {
				return err
			}
			pending = pending[used:]
			if len(reply) > 0 && (reply[0] == 'X' || reply[0] == 'Z') {
				_, err := k.exchange('Z', []byte("D"), k.chkt)
				return err
			}
		}
		if rerr != nil {
			break
		}
	}
	_, err = k.exchange('Z', nil, k.chkt)
	return err
}

// ReceiveKermit receives files by Kermit.
func ReceiveKermit(p Port, sink Sink) error {
	k := &kermit{l: newLine(p), peer: kparams{eol: 0x0d}, chkt: '1'}
	var in Incoming
	expect := 0
	errs := 0
	fail := func(err error) error {
		if in != nil {
			in.Abort()
		}
		k.fail(err.Error())
		return err
	}
	ackd := func(seq int, data []byte) {
		k.sendPacket('Y', seq, data, k.chkt)
	}
	start := time.Now()
	for {
		d := kTimeout
		if expect == 0 {
			d = 5 * time.Second
		}
		pk, err := k.recvPacket(k.chkt, d)
		if errors.Is(err, ErrTimeout) || errors.Is(err, errBadPacket) {
			if expect == 0 && time.Since(start) < kInitWait {
				continue
			}
			if errs++; errs > kRetries {
				return fail(ErrRetries)
			}
			k.sendPacket('N', expect, nil, k.chkt)
			continue
		}
		if err != nil {
			return fail(err)
		}
		if pk.typ == 'E' {
			if in != nil {
				in.Abort()
			}
			return fmt.Errorf("%w: %s", ErrCancelled, pk.data)
		}
		if pk.seq == (expect+63)%64 && expect > 0 {
			// ours was lost: again (an S's ACK carries our parameters)
			if pk.typ == 'S' {
				k.sendPacket('Y', pk.seq, ourAck(k), '1')
			} else {
				ackd(pk.seq, nil)
			}
			continue
		}
		if pk.seq != expect%64 {
			k.sendPacket('N', expect, nil, k.chkt)
			continue
		}
		errs = 0
		switch pk.typ {
		case 'S':
			k.agree(decodeParams(pk.data), false)
			// the ACK goes with block check 1, the agreed one after it
			k.sendPacket('Y', pk.seq, ourAck(k), '1')
		case 'F':
			nm, err := k.decode(pk.data)
			if err != nil {
				return fail(err)
			}
			if in != nil {
				in.Abort()
			}
			in, err = sink(string(nm), -1)
			if err != nil {
				return fail(err)
			}
			ackd(pk.seq, nil)
		case 'A':
			// attributes: the size ('1') goes to the sink's Incoming via
			// its limit when it is written; nothing to refuse here
			ackd(pk.seq, []byte("Y"))
		case 'D':
			if in == nil {
				return fail(fmt.Errorf("%w: data before a file", ErrProtocol))
			}
			data, err := k.decode(pk.data)
			if err != nil {
				return fail(err)
			}
			if _, err := in.Write(data); err != nil {
				return fail(err)
			}
			ackd(pk.seq, nil)
		case 'Z':
			if in != nil {
				if len(pk.data) > 0 && pk.data[0] == 'D' {
					in.Abort()
				} else if err := in.Commit(); err != nil {
					in = nil
					return fail(err)
				}
				in = nil
			}
			ackd(pk.seq, nil)
		case 'B':
			ackd(pk.seq, nil)
			return nil
		default:
			return fail(fmt.Errorf("%w: packet %q", ErrProtocol, pk.typ))
		}
		expect++
	}
}

// ourAck is the ACK to an S: our parameters, with the block check and
// prefixes as agreed.
func ourAck(k *kermit) []byte {
	p := ourParams
	p.chkt = k.chkt
	switch {
	case k.qbin != 0 && k.peer.qbin == 'Y':
		p.qbin = k.qbin
	case k.qbin != 0:
		p.qbin = 'Y'
	default:
		p.qbin = 'N'
	}
	if k.rept == 0 {
		p.rept = ' '
	}
	return p.encode()
}
