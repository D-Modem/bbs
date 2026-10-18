// Package xfer moves files over a caller's line with the protocols terminal
// programs speak: XMODEM (checksum, CRC, 1K), YMODEM (batch), ZMODEM and
// Kermit, both ways.
//
// The line is whatever the caller is on: a modem (raw), telnet (in binary
// mode; telnet.Conn escapes and unescapes IAC) or rlogin.  Every protocol
// here has timeouts and retry limits, takes no more than its Incoming will
// hold, and never takes a path from the other side: names are only passed
// to the Sink, which decides.
package xfer

import (
	"errors"
	"io"
	"os"
	"time"
)

// Port is the caller's line.
type Port interface {
	io.Reader
	io.Writer
	SetReadDeadline(time.Time) error
}

// Outgoing is a file to send.
type Outgoing struct {
	Name    string
	Size    int64
	ModTime time.Time
	Data    io.Reader
}

// Incoming is a file being received: written, then committed or aborted.
type Incoming interface {
	io.Writer
	Commit() error
	Abort()
}

// Sink is asked for each file the other side sends: its name (as sent:
// check it) and size (-1 if not told).  An error refuses the file.
type Sink func(name string, size int64) (Incoming, error)

// Errors of transfers.
var (
	ErrCancelled = errors.New("transfer cancelled by the other side")
	ErrTimeout   = errors.New("transfer timed out")
	ErrRetries   = errors.New("too many errors")
	ErrProtocol  = errors.New("protocol error")
)

// line reads a port byte by byte with timeouts, buffered.
type line struct {
	p   Port
	buf []byte
	r   int
	w   int
}

// newLine is a line on p, with an 8 KB buffer.
func newLine(p Port) *line { return &line{p: p, buf: make([]byte, 8192)} }

// getc reads a byte, waiting up to d.
func (l *line) getc(d time.Duration) (byte, error) {
	if l.r == l.w {
		l.p.SetReadDeadline(time.Now().Add(d))
		n, err := l.p.Read(l.buf)
		l.p.SetReadDeadline(time.Time{})
		if n == 0 {
			if err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
				return 0, ErrTimeout
			}
			return 0, err
		}
		l.r, l.w = 0, n
	}
	b := l.buf[l.r]
	l.r++
	return b, nil
}

// read fills p, each byte within d.
func (l *line) read(p []byte, d time.Duration) error {
	for i := range p {
		b, err := l.getc(d)
		if err != nil {
			return err
		}
		p[i] = b
	}
	return nil
}

// purge drops what comes until the line has been quiet for d.
func (l *line) purge(d time.Duration) {
	l.r, l.w = 0, 0
	for i := 0; i < 1000; i++ {
		if _, err := l.getc(d); err != nil {
			return
		}
		l.r = l.w
	}
}

// write sends p, whole or an error.
func (l *line) write(p []byte) error {
	_, err := l.p.Write(p)
	return err
}

// cancel sends CAN CAN CAN CAN CAN and backspaces: the X/Y/ZMODEM abort.
func (l *line) cancel() {
	l.write([]byte{can, can, can, can, can, 8, 8, 8, 8, 8})
}

// crc16 is CRC-16/XMODEM (CCITT, polynomial 0x1021, initial 0).
func crc16(data []byte, crc uint16) uint16 {
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// Protocol is a transfer protocol, for the menus.
type Protocol struct {
	Key     string // its menu key
	Name    string
	Batch   bool // sends names (and many files); false: XMODEM's one, unnamed
	Send    func(p Port, files []Outgoing) error
	Receive func(p Port, sink Sink) error
}

// Protocols are the protocols, in menu order.
var Protocols = []Protocol{
	{"X", "XMODEM (CRC)", false, func(p Port, f []Outgoing) error { return SendXMODEM(p, f[0], false) },
		func(p Port, s Sink) error { return ReceiveXMODEM(p, s) }},
	{"1", "XMODEM-1K", false, func(p Port, f []Outgoing) error { return SendXMODEM(p, f[0], true) },
		func(p Port, s Sink) error { return ReceiveXMODEM(p, s) }},
	{"Y", "YMODEM (batch)", true, SendYMODEM, ReceiveYMODEM},
	{"Z", "ZMODEM", true, SendZMODEM, ReceiveZMODEM},
	{"K", "Kermit", true, SendKermit, ReceiveKermit},
}

// Lookup finds a protocol by its key.
func Lookup(key string) *Protocol {
	for i := range Protocols {
		if Protocols[i].Key == key {
			return &Protocols[i]
		}
	}
	return nil
}
