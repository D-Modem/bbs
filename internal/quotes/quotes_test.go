package quotes

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seg makes an IEX-TP TOPS segment of messages.
func seg(msgs ...[]byte) []byte {
	b := make([]byte, tpHeader)
	b[0] = 1
	binary.LittleEndian.PutUint16(b[2:], protoTOPS)
	binary.LittleEndian.PutUint16(b[14:], uint16(len(msgs)))
	for _, m := range msgs {
		b = binary.LittleEndian.AppendUint16(b, uint16(len(m)))
		b = append(b, m...)
	}
	binary.LittleEndian.PutUint16(b[12:], uint16(len(b)-tpHeader))
	return b
}

func sym8(s string) []byte { return []byte((s + "        ")[:8]) }

// ts is a time on Friday 6 November 2026, New York time.
func ts(h, m int) int64 { return time.Date(2026, 11, 6, h, m, 0, 0, eastern).UnixNano() }

func trade(s string, t int64, size uint32, price float64, flags byte) []byte {
	m := []byte{msgTradeReport, flags}
	m = binary.LittleEndian.AppendUint64(m, uint64(t))
	m = append(m, sym8(s)...)
	m = binary.LittleEndian.AppendUint32(m, size)
	m = binary.LittleEndian.AppendUint64(m, uint64(int64(price*priceScale+0.5)))
	return binary.LittleEndian.AppendUint64(m, 1)
}

func quote(s string, t int64, bid, ask float64) []byte {
	m := []byte{msgQuoteUpdate, 0}
	m = binary.LittleEndian.AppendUint64(m, uint64(t))
	m = append(m, sym8(s)...)
	m = binary.LittleEndian.AppendUint32(m, 100)
	m = binary.LittleEndian.AppendUint64(m, uint64(int64(bid*priceScale)))
	m = binary.LittleEndian.AppendUint64(m, uint64(int64(ask*priceScale)))
	return binary.LittleEndian.AppendUint32(m, 200)
}

func system(t int64, ev byte) []byte {
	m := []byte{msgSystemEvent, ev}
	return binary.LittleEndian.AppendUint64(m, uint64(t))
}

func official(s string, t int64, kind byte, price float64) []byte {
	m := []byte{msgOfficialPrice, kind}
	m = binary.LittleEndian.AppendUint64(m, uint64(t))
	m = append(m, sym8(s)...)
	return binary.LittleEndian.AppendUint64(m, uint64(int64(price*priceScale)))
}

// udpFrame wraps a payload in Ethernet, IPv4 and UDP.
func udpFrame(payload []byte, vlan bool) []byte {
	var f []byte
	f = append(f, make([]byte, 12)...)
	if vlan {
		f = append(f, 0x81, 0x00, 0, 5)
	}
	f = append(f, 0x08, 0x00)
	ip := make([]byte, 20)
	ip[0], ip[9] = 0x45, 17
	binary.BigEndian.PutUint16(ip[2:], uint16(20+8+len(payload)))
	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp[4:], uint16(8+len(payload)))
	f = append(f, ip...)
	f = append(f, udp...)
	return append(f, payload...)
}

func pad4(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// pcapngFile is a pcapng capture of the payloads.
func pcapngFile(payloads ...[]byte) []byte {
	le := binary.LittleEndian
	var out []byte
	block := func(typ uint32, body []byte) {
		body = pad4(body)
		n := uint32(12 + len(body))
		out = le.AppendUint32(out, typ)
		out = le.AppendUint32(out, n)
		out = append(out, body...)
		out = le.AppendUint32(out, n)
	}
	shb := le.AppendUint32(nil, 0x1a2b3c4d)
	shb = append(shb, 1, 0, 0, 0)
	shb = le.AppendUint64(shb, ^uint64(0))
	block(0x0a0d0d0a, shb)
	block(1, []byte{1, 0, 0, 0, 0xff, 0xff, 0, 0})
	for i, p := range payloads {
		fr := udpFrame(p, i%2 == 1)
		epb := le.AppendUint32(nil, 0)
		epb = le.AppendUint64(epb, 0)
		epb = le.AppendUint32(epb, uint32(len(fr)))
		epb = le.AppendUint32(epb, uint32(len(fr)))
		block(6, append(epb, fr...))
	}
	return out
}

// pcapFile is a classic capture.
func pcapFile(payloads ...[]byte) []byte {
	le := binary.LittleEndian
	out := le.AppendUint32(nil, 0xa1b2c3d4)
	out = append(out, 2, 0, 4, 0)
	out = append(out, make([]byte, 8)...)
	out = le.AppendUint32(out, 65535)
	out = le.AppendUint32(out, 1)
	for _, p := range payloads {
		fr := udpFrame(p, false)
		out = append(out, make([]byte, 8)...)
		out = le.AppendUint32(out, uint32(len(fr)))
		out = le.AppendUint32(out, uint32(len(fr)))
		out = append(out, fr...)
	}
	return out
}

func session() [][]byte {
	return [][]byte{
		seg(system(ts(7, 0), 'O'), quote("AAPL", ts(7, 1), 340.10, 340.20)),
		seg(trade("AAPL", ts(8, 0), 100, 338.08, flagExtended), system(ts(9, 30), 'R')),
		seg(trade("AAPL", ts(9, 31), 100, 338.59, 0), trade("AAPL", ts(10, 0), 40, 350, flagOddLot),
			trade("AAPL", ts(11, 0), 200, 345.76, flagISO), trade("AAPL", ts(15, 59), 50, 342.41, 0),
			trade("ZVZZT", ts(10, 0), 100, 10, 0), official("IEXL", ts(9, 30), 'Q', 20), official("IEXL", ts(16, 0), 'M', 21.5)),
		seg(system(ts(16, 0), 'M'), trade("AAPL", ts(16, 30), 10, 342.90, flagExtended), system(ts(20, 0), 'E')),
	}
}

func check(t *testing.T, s *Snapshot) {
	t.Helper()
	if s.Date != "20261106" || s.Session != "closed" {
		t.Errorf("date %q session %q", s.Date, s.Session)
	}
	a := s.Quotes["AAPL"]
	if a == nil {
		t.Fatalf("no AAPL: %v", s.Quotes)
	}
	if a.Open != 338.59 || a.High != 345.76 || a.Low != 338.59 || a.Close != 342.41 || a.Last != 342.41 ||
		a.Volume != 500 || a.Extended != 342.90 || a.Bid != 340.10 || a.Ask != 340.20 {
		t.Errorf("AAPL %+v", *a)
	}
	if q := s.Quotes["IEXL"]; q == nil || q.Open != 20 || q.Close != 21.5 {
		t.Errorf("IEXL %+v", q)
	}
	if _, ok := s.Quotes["ZVZZT"]; ok {
		t.Error("the test security is kept")
	}
}

func TestCaptures(t *testing.T) {
	ng := pcapngFile(session()...)
	b := NewBook(nil)
	if err := ReadCapture(bytes.NewReader(ng), b, nil); err != nil {
		t.Fatal(err)
	}
	check(t, b.Snapshot("test"))

	// gzipped classic pcap, and the session before's closes for the change
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(pcapFile(session()...))
	zw.Close()
	prev := &Snapshot{Quotes: map[string]*Quote{"AAPL": {Symbol: "AAPL", Close: 340}}}
	b = NewBook(prev)
	if err := ReadCapture(&gz, b, nil); err != nil {
		t.Fatal(err)
	}
	s := b.Snapshot("test")
	check(t, s)
	d, p, ok := s.Quotes["AAPL"].Change()
	if !ok || d < 2.40 || d > 2.42 || p < 0.70 || p > 0.72 {
		t.Errorf("change %v %v %v", d, p, ok)
	}

	// kept and read again; the board reads it once and again when it changes
	path := filepath.Join(t.TempDir(), "quotes.json.gz")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	bd := &Board{Path: path}
	got, err := bd.Current()
	if err != nil || got.Quotes["AAPL"].Close != 342.41 || got.SessionDate().Day() != 6 {
		t.Fatalf("board: %v %v", got, err)
	}
	s.Quotes["AAPL"].Close = 1
	s.Save(path)
	os.Chtimes(path, time.Now().Add(time.Minute), time.Now().Add(time.Minute))
	if got, _ := bd.Current(); got.Quotes["AAPL"].Close != 1 {
		t.Error("the board did not read the new file")
	}
	if _, err := (&Board{Path: filepath.Join(t.TempDir(), "none")}).Current(); err == nil {
		t.Error("no file, no error")
	}
}

func TestSegmentErrors(t *testing.T) {
	if err := Segment([]byte("short"), NewBook(nil)); err != errSegment {
		t.Errorf("short: %v", err)
	}
	s := seg(trade("A", 1, 1, 1, 0))
	if err := Segment(s[:len(s)-3], NewBook(nil)); err == nil {
		t.Error("a message cut short taken")
	}
	if err := Packets(bytes.NewReader([]byte("not a capture")), func([]byte) error { return nil }); err == nil {
		t.Error("not a capture taken")
	}
}

func TestListen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	addr := pc.LocalAddr().String()
	pc.Close()
	b := NewBook(nil)
	done := make(chan error, 1)
	go func() { done <- Listen(ctx, addr, "", b) }()
	c, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.Write(seg(trade("MSFT", ts(10, 0), 100, 530.58, 0)))
		if q := b.Snapshot("live").Quotes["MSFT"]; q != nil && q.Last == 530.58 {
			cancel()
			if err := <-done; err != nil {
				t.Error(err)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the live feed's trade did not come")
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"aapl": "AAPL", " brk.b ": "BRK.B", "bad sym": "", "": "", "TOOLONGSYM": ""} {
		if got := Normalize(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
