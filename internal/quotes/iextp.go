// Package quotes is stock quotes for the callers, from IEX (the Investors
// Exchange): its TOPS feed, read from the free historical files IEX
// publishes after each trading day (HIST: the previous session's prices),
// or live from a TOPS feed the sysop receives (real-time or IEX's free
// delayed one, which needs a connection to IEX).  Prices are IEX's own
// trades and quotes, not the whole market's.
package quotes

import (
	"encoding/binary"
	"errors"
	"strings"
)

// IEX-TP, the IEX Transport Protocol (version 1): a segment is a 40 byte
// header and messages, each with its length before it; all little-endian.
//
//	0  version (1)       1  reserved           2  message protocol (0x8003 TOPS, 0x8004 DEEP)
//	4  channel           8  session           12  payload length
//	14 message count    16  stream offset     24  first message's sequence number
//	32 send time (ns since 1970)
const (
	tpHeader   = 40
	protoTOPS  = 0x8003
	protoDEEP  = 0x8004
	priceScale = 10000 // prices are fixed point, four decimals
)

// Message types (TOPS 1.6 and DEEP 1.0 have these alike).
const (
	msgSystemEvent   = 'S'
	msgSecurityDir   = 'D'
	msgTradingStatus = 'H'
	msgQuoteUpdate   = 'Q'
	msgTradeReport   = 'T'
	msgOfficialPrice = 'X'
	msgTradeBreak    = 'B'
)

// Trade flags (a trade report's sale condition).
const (
	flagISO        = 0x80 // intermarket sweep
	flagExtended   = 0x40 // outside regular hours (form T)
	flagOddLot     = 0x20
	flagTradeThru  = 0x10
	flagSinglePrc  = 0x08 // single-price cross (an auction)
	secFlagTest    = 0x80 // a security directory's: a test security
	secFlagWhenIss = 0x40
	secFlagETP     = 0x20
)

// errSegment: not an IEX-TP segment.
var errSegment = errors.New("not an IEX-TP segment")

// Handler is told of a feed's messages.
type Handler interface {
	Trade(sym string, ts int64, size uint32, price int64, flags byte)
	Quote(sym string, ts int64, bidSize uint32, bid, ask int64, askSize uint32)
	Official(sym string, ts int64, kind byte, price int64) // kind 'Q' opening, 'M' closing
	Status(sym string, ts int64, status byte)              // 'H' halted, 'P' paused, 'O' released, 'T' trading
	Directory(sym string, flags byte, roundLot uint32)
	System(ts int64, event byte) // 'O' 'S' 'R' 'M' 'E' 'C'
	Break(sym string, ts int64, size uint32, price int64)
}

// Segment reads an IEX-TP segment's TOPS (or DEEP) messages into h.
func Segment(b []byte, h Handler) error {
	if len(b) < tpHeader || b[0] != 1 {
		return errSegment
	}
	proto := binary.LittleEndian.Uint16(b[2:])
	if proto != protoTOPS && proto != protoDEEP {
		return errSegment
	}
	count := int(binary.LittleEndian.Uint16(b[14:]))
	p := b[tpHeader:]
	for i := 0; i < count; i++ {
		if len(p) < 2 {
			return errors.New("IEX-TP: a segment cut short")
		}
		n := int(binary.LittleEndian.Uint16(p))
		if len(p) < 2+n {
			return errors.New("IEX-TP: a message cut short")
		}
		message(p[2:2+n], h)
		p = p[2+n:]
	}
	return nil
}

// message reads one message (those it does not know, or too short, are
// passed over).
func message(m []byte, h Handler) {
	if len(m) < 10 {
		return
	}
	le := binary.LittleEndian
	ts := int64(le.Uint64(m[2:]))
	switch m[0] {
	case msgQuoteUpdate: // flags, ts, symbol, bid size, bid, ask, ask size
		if len(m) >= 42 {
			h.Quote(symbol(m[10:18]), ts, le.Uint32(m[18:]), int64(le.Uint64(m[22:])), int64(le.Uint64(m[30:])), le.Uint32(m[38:]))
		}
	case msgTradeReport, msgTradeBreak: // flags, ts, symbol, size, price, trade id
		if len(m) >= 38 {
			sym, size, price := symbol(m[10:18]), le.Uint32(m[18:]), int64(le.Uint64(m[22:]))
			if m[0] == msgTradeReport {
				h.Trade(sym, ts, size, price, m[1])
			} else {
				h.Break(sym, ts, size, price)
			}
		}
	case msgOfficialPrice: // price type, ts, symbol, price
		if len(m) >= 26 {
			h.Official(symbol(m[10:18]), ts, m[1], int64(le.Uint64(m[18:])))
		}
	case msgTradingStatus:
		if len(m) >= 18 {
			h.Status(symbol(m[10:18]), ts, m[1])
		}
	case msgSecurityDir: // flags, ts, symbol, round lot, adjusted POC price, LULD tier
		if len(m) >= 22 {
			h.Directory(symbol(m[10:18]), m[1], le.Uint32(m[18:]))
		}
	case msgSystemEvent:
		h.System(ts, m[1])
	}
}

// symbol is a symbol without the spaces after it.
func symbol(b []byte) string {
	return strings.TrimRight(string(b), " \x00")
}
