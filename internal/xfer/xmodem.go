package xfer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	soh = 0x01
	stx = 0x02
	eot = 0x04
	ack = 0x06
	nak = 0x15
	can = 0x18
	sub = 0x1a
)

const (
	xRetries    = 10
	xStartWait  = 60 * time.Second // for the receiver to start
	xBlockWait  = 10 * time.Second // for an answer, or a block to start
	xByteWait   = 5 * time.Second  // within a block
	xStartTries = 4                // 'C's before falling back to checksums
)

// waitStart waits for the receiver's start: 'C' (CRC) or NAK (checksum).
func waitStart(l *line) (crc bool, err error) {
	end := time.Now().Add(xStartWait)
	cans := 0
	for time.Now().Before(end) {
		b, err := l.getc(time.Until(end))
		if err != nil {
			return false, err
		}
		switch b {
		case 'C':
			return true, nil
		case nak:
			return false, nil
		case can:
			if cans++; cans >= 2 {
				return false, ErrCancelled
			}
		default:
			cans = 0
		}
	}
	return false, ErrTimeout
}

// sendBlock sends block seq (128 or 1024 bytes of data) until ACKed.
func sendBlock(l *line, seq byte, data []byte, crc bool) error {
	hdr := byte(soh)
	if len(data) == 1024 {
		hdr = stx
	}
	pkt := append([]byte{hdr, seq, ^seq}, data...)
	if crc {
		c := crc16(data, 0)
		pkt = append(pkt, byte(c>>8), byte(c))
	} else {
		var sum byte
		for _, b := range data {
			sum += b
		}
		pkt = append(pkt, sum)
	}
	for try := 0; try < xRetries; try++ {
		if err := l.write(pkt); err != nil {
			return err
		}
		cans := 0
	wait:
		for {
			b, err := l.getc(xBlockWait)
			if errors.Is(err, ErrTimeout) {
				break wait
			}
			if err != nil {
				return err
			}
			switch b {
			case ack:
				return nil
			case nak, 'C':
				break wait
			case can:
				if cans++; cans >= 2 {
					return ErrCancelled
				}
			}
		}
	}
	l.cancel()
	return ErrRetries
}

// sendData sends r in blocks from seq 1; with big, 1K blocks (but a short
// last one in 128s).
func sendData(l *line, r io.Reader, crc, big bool) error {
	seq := byte(1)
	buf := make([]byte, 1024)
	for {
		size := 128
		if big {
			size = 1024
		}
		n, err := io.ReadFull(r, buf[:size])
		if n == 0 {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return err
		}
		if big && n <= 128*7 {
			// a short last block: as 128s, not padded to 1K
			for off := 0; off < n; off += 128 {
				blk := pad(buf[off:min(off+128, n)], 128)
				if err := sendBlock(l, seq, blk, crc); err != nil {
					return err
				}
				seq++
			}
		} else {
			if err := sendBlock(l, seq, pad(buf[:n], size), crc); err != nil {
				return err
			}
			seq++
		}
		if n < size {
			break
		}
	}
	return sendEOT(l)
}

func pad(b []byte, size int) []byte {
	out := make([]byte, size)
	copy(out, b)
	for i := len(b); i < size; i++ {
		out[i] = sub
	}
	return out
}

func sendEOT(l *line) error {
	for try := 0; try < xRetries; try++ {
		if err := l.write([]byte{eot}); err != nil {
			return err
		}
		b, err := l.getc(xBlockWait)
		if err == nil && b == ack {
			return nil
		}
		if err != nil && !errors.Is(err, ErrTimeout) {
			return err
		}
	}
	return ErrRetries
}

// SendXMODEM sends one file by XMODEM: CRC or checksum as the receiver
// asks; with big, 1K blocks (XMODEM-1K, CRC only).
func SendXMODEM(p Port, f Outgoing, big bool) error {
	l := newLine(p)
	crc, err := waitStart(l)
	if err != nil {
		return err
	}
	return sendData(l, f.Data, crc, big && crc)
}

// recvBlocks receives data blocks from seq 1 into w until EOT.  first is a
// block header byte already read (0: none).  With size >= 0 only size bytes
// are kept (YMODEM); otherwise the last block's trailing SUBs are dropped
// (XMODEM's padding).  With ymodem, the first EOT is NAKed, as YMODEM does.
func recvBlocks(l *line, w io.Writer, crc bool, first byte, size int64, ymodem bool) error {
	expect := byte(1)
	var pending []byte // the last block, held back to strip its padding
	written := int64(0)
	flush := func(b []byte, last bool) error {
		if size >= 0 {
			if left := size - written; int64(len(b)) > left {
				b = b[:max(left, 0)]
			}
		} else if last {
			b = bytes.TrimRight(b, "\x1a")
		}
		written += int64(len(b))
		_, err := w.Write(b)
		return err
	}
	errs := 0
	eots := 0
	hdr := first
	for {
		if hdr == 0 {
			b, err := l.getc(xBlockWait)
			if errors.Is(err, ErrTimeout) {
				if errs++; errs > xRetries {
					l.cancel()
					return ErrRetries
				}
				l.write([]byte{nak})
				continue
			}
			if err != nil {
				return err
			}
			hdr = b
		}
		switch hdr {
		case eot:
			if ymodem && eots == 0 {
				eots++
				l.write([]byte{nak})
				hdr = 0
				continue
			}
			l.write([]byte{ack})
			if pending != nil {
				return flush(pending, true)
			}
			return nil
		case can:
			b, err := l.getc(time.Second)
			if err == nil && b == can {
				return ErrCancelled
			}
			hdr = 0
			continue
		case soh, stx:
		default:
			hdr = 0
			continue
		}
		n := 128
		if hdr == stx {
			n = 1024
		}
		hdr = 0
		blk := make([]byte, 2+n+1+btoi(crc))
		if err := l.read(blk, xByteWait); err != nil {
			if !errors.Is(err, ErrTimeout) {
				return err
			}
			if errs++; errs > xRetries {
				l.cancel()
				return ErrRetries
			}
			l.purge(time.Second)
			l.write([]byte{nak})
			continue
		}
		seq, cseq, data := blk[0], blk[1], blk[2:2+n]
		good := seq == ^cseq
		if crc {
			good = good && crc16(data, 0) == uint16(blk[2+n])<<8|uint16(blk[3+n])
		} else {
			var sum byte
			for _, b := range data {
				sum += b
			}
			good = good && sum == blk[2+n]
		}
		if !good {
			if errs++; errs > xRetries {
				l.cancel()
				return ErrRetries
			}
			l.purge(time.Second)
			l.write([]byte{nak})
			continue
		}
		switch seq {
		case expect:
			if pending != nil {
				if err := flush(pending, false); err != nil {
					l.cancel()
					return err
				}
			}
			pending = append([]byte(nil), data...)
			expect++
			errs = 0
		case expect - 1:
			// our ACK was lost: it came again
		default:
			l.cancel()
			return fmt.Errorf("%w: block %d, expected %d", ErrProtocol, seq, expect)
		}
		l.write([]byte{ack})
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// startReceive asks for a transfer: 'C' a few times, then NAK (checksum);
// the first header byte, and whether CRC was taken.
func startReceive(l *line, crcOnly bool) (hdr byte, crc bool, err error) {
	for try := 0; try < 2*xStartTries; try++ {
		crc = try < xStartTries || crcOnly
		start := byte(nak)
		if crc {
			start = 'C'
		}
		if err := l.write([]byte{start}); err != nil {
			return 0, false, err
		}
		b, err := l.getc(3 * time.Second)
		if errors.Is(err, ErrTimeout) {
			continue
		}
		if err != nil {
			return 0, false, err
		}
		switch b {
		case soh, stx, eot:
			return b, crc, nil
		case can:
			if b, err := l.getc(time.Second); err == nil && b == can {
				return 0, false, ErrCancelled
			}
		}
	}
	return 0, false, ErrTimeout
}

// ReceiveXMODEM receives one file by XMODEM (CRC, falling back to
// checksum; 128 or 1K blocks).  XMODEM sends no name: the sink is asked
// for "" and size -1.
func ReceiveXMODEM(p Port, sink Sink) error {
	in, err := sink("", -1)
	if err != nil {
		return err
	}
	l := newLine(p)
	hdr, crc, err := startReceive(l, false)
	if err == nil {
		err = recvBlocks(l, in, crc, hdr, -1, false)
	}
	if err != nil {
		in.Abort()
		return err
	}
	return in.Commit()
}

// SendYMODEM sends files by YMODEM batch: block 0 with each one's name,
// size and time, 1K blocks, and an empty block 0 to end.
func SendYMODEM(p Port, files []Outgoing) error {
	l := newLine(p)
	for _, f := range files {
		crc, err := waitStart(l)
		if err != nil {
			return err
		}
		if !crc {
			l.cancel()
			return fmt.Errorf("%w: the receiver wants checksums, YMODEM needs CRC", ErrProtocol)
		}
		hdr := []byte(f.Name)
		hdr = append(hdr, 0)
		hdr = append(hdr, fmt.Sprintf("%d %o 0", f.Size, f.ModTime.Unix())...)
		if len(hdr) > 128 {
			return errors.New("ymodem: name too long")
		}
		if err := sendBlock(l, 0, pad0(hdr, 128), true); err != nil {
			return err
		}
		if _, err := waitStart(l); err != nil {
			return err
		}
		if err := sendData(l, f.Data, true, true); err != nil {
			return err
		}
	}
	if _, err := waitStart(l); err != nil {
		return err
	}
	return sendBlock(l, 0, make([]byte, 128), true)
}

func pad0(b []byte, size int) []byte {
	out := make([]byte, size)
	copy(out, b)
	return out
}

// ReceiveYMODEM receives a YMODEM batch.
func ReceiveYMODEM(p Port, sink Sink) error {
	l := newLine(p)
	for {
		hdr, _, err := startReceive(l, true)
		if err != nil {
			return err
		}
		if hdr == eot {
			// a sender still finishing the last file
			l.write([]byte{ack})
			continue
		}
		name, size, err := recvHeader(l, hdr)
		if err != nil {
			return err
		}
		if name == "" {
			l.write([]byte{ack})
			return nil
		}
		in, err := sink(name, size)
		if err != nil {
			l.cancel()
			return err
		}
		l.write([]byte{ack, 'C'})
		if err := recvBlocks(l, in, true, 0, size, true); err != nil {
			in.Abort()
			return err
		}
		if err := in.Commit(); err != nil {
			l.cancel()
			return err
		}
	}
}

// recvHeader reads YMODEM's block 0: the name, and the size (-1 if none).
func recvHeader(l *line, hdr byte) (string, int64, error) {
	for try := 0; try < xRetries; try++ {
		if hdr != soh && hdr != stx {
			b, err := l.getc(xBlockWait)
			if err != nil && !errors.Is(err, ErrTimeout) {
				return "", 0, err
			}
			if err != nil {
				l.write([]byte{'C'})
				continue
			}
			hdr = b
			if hdr == can {
				return "", 0, ErrCancelled
			}
			continue
		}
		n := 128
		if hdr == stx {
			n = 1024
		}
		hdr = 0
		blk := make([]byte, 2+n+2)
		if err := l.read(blk, xByteWait); err != nil {
			l.purge(time.Second)
			l.write([]byte{nak})
			continue
		}
		data := blk[2 : 2+n]
		if blk[0] != 0 || blk[1] != 0xff || crc16(data, 0) != uint16(blk[2+n])<<8|uint16(blk[3+n]) {
			l.purge(time.Second)
			l.write([]byte{nak})
			continue
		}
		name, rest, _ := bytes.Cut(data, []byte{0})
		size := int64(-1)
		if fields := strings.Fields(string(bytes.TrimRight(rest, "\x00"))); len(fields) > 0 {
			if v, err := strconv.ParseInt(fields[0], 10, 64); err == nil && v >= 0 {
				size = v
			}
		}
		return string(name), size, nil
	}
	l.cancel()
	return "", 0, ErrRetries
}
