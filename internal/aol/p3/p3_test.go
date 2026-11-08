package p3

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func unhex(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic(err)
	}
	return b
}

// Packets a host sends live clients (as Dialtone, which serves them, has
// them): the ACK of the client's INIT and the SD that follows it; and a
// sign-off (XS) with AOL 4.0's "**" for a CRC.
var (
	initACK = unhex("5ab71100037f7f240d")
	sdData  = unhex("5a88e9000b107f20534454d1f68b00000d")
	xsBye   = unhex("5a2a2a000d7f7f20585347 6f6f64627965210d")
)

func TestPackets(t *testing.T) {
	for _, c := range []struct {
		raw []byte
		p   Packet
	}{
		{initACK, Packet{TX: 0x7F, RX: 0x7F, Type: ACK}},
		{sdData, Packet{TX: 0x10, RX: 0x7F, Type: DATA, Data: unhex("5344 54d1f68b0000")}},
	} {
		if got := Append(nil, c.p); !bytes.Equal(got, c.raw) {
			t.Errorf("Append(%v) = %x, want %x", c.p, got, c.raw)
		}
		p, err := NewReader(bytes.NewReader(c.raw)).Read()
		if err != nil || p.TX != c.p.TX || p.RX != c.p.RX || p.Type != c.p.Type || !bytes.Equal(p.Data, c.p.Data) || p.NoCRC {
			t.Errorf("Read(%x) = %v, %v", c.raw, p, err)
		}
	}
	p, err := NewReader(bytes.NewReader(xsBye)).Read()
	if tok, rest := Token(p.Data); err != nil || !p.NoCRC || tok != "XS" || string(rest) != "Goodbye!" {
		t.Errorf("Read(**) = %v, %v", p, err)
	}
	if CRC([]byte("123456789")) != 0xBB3D { // CRC-16/ARC's check value
		t.Error("CRC")
	}
}

func TestReader(t *testing.T) {
	bad := append([]byte(nil), sdData...)
	bad[len(bad)-2] ^= 1
	noEnd := initACK[:len(initACK)-1]
	var in []byte
	in = append(in, "ATDT\r"...) // what isn't P3 is passed over
	in = append(in, initACK...)
	in = append(in, bad...)
	in = append(in, 0x5A, 0x00, 0x00, 0xFF, 0xFF) // a length too long
	in = append(in, noEnd...)
	in = append(in, sdData...)
	in = append(in, Append(nil, Packet{TX: 0x11, RX: 0x7F, Type: DATA, Data: bytes.Repeat([]byte{End}, 30)})...)
	r := NewReader(bytes.NewReader(in))
	var got []string
	for {
		p, err := r.Read()
		if err == io.EOF {
			break
		}
		switch err {
		case nil:
			got = append(got, fmt.Sprintf("%02x/%d", p.Type, len(p.Data)))
		case ErrCRC, ErrLength:
			got = append(got, err.Error())
		default:
			t.Fatal(err)
		}
	}
	want := "24/0 p3: bad CRC p3: bad length 24/0 20/8 20/30"
	if g := strings.Join(got, " "); g != want {
		t.Errorf("got  %s\nwant %s", g, want)
	}
	if r.Skipped != 5+4 {
		t.Errorf("skipped %d", r.Skipped)
	}
}

func TestSeq(t *testing.T) {
	if next(0x7F) != 0x10 || next(0x10) != 0x11 || next(0) != 0x10 {
		t.Error("next")
	}
	if behind(0x7F, 0x10) != 1 || behind(0x10, 0x7F) != seqRing-1 || behind(0x20, 0x20) != 0 {
		t.Error("behind")
	}
}

// initData is an INIT as the 32-bit Windows client lays it out.
func initData() []byte {
	d := make([]byte, 0x34)
	d[0], d[1], d[2] = PlatformWindows, 3, 0
	d[0x1F], d[0x20], d[0x21], d[0x22] = 0x03, 0x20, 0x02, 0x58 // 800x600
	d[0x30] = 5
	return d
}

func TestInit(t *testing.T) {
	in := ParseInit(initData())
	if in.Platform != PlatformWindows || in.Width != 800 || in.Height != 600 || in.ConnectSpeed != 5 {
		t.Errorf("%+v", in)
	}
	if s := in.String(); s != "Windows client, version 3/0, 800x600" {
		t.Error(s)
	}
	if in := ParseInit([]byte{PlatformMac, 2}); in.Platform != PlatformMac || in.Version != 2 {
		t.Errorf("short: %+v", in)
	}
	if !Sniff(Append(nil, Packet{TX: 0x7F, RX: 0x7F, Type: INIT | FromClient, Data: initData()})) || Sniff(sdData) || Sniff([]byte("*\x01")) {
		t.Error("Sniff")
	}
}

// client is the client's end of a link, scripted by a test.
type client struct {
	t    *testing.T
	c    net.Conn
	pkts chan Packet
	errs chan error
}

func dial(t *testing.T, opt Options) (*client, *Link) {
	t.Helper()
	a, b := net.Pipe()
	c := &client{t: t, c: a, pkts: make(chan Packet, 1024), errs: make(chan error, 64)}
	go func() {
		r := NewReader(bufio.NewReader(a))
		for {
			p, err := r.Read()
			if err == ErrCRC || err == ErrLength {
				c.errs <- err
				continue
			}
			if err != nil {
				close(c.pkts)
				return
			}
			c.pkts <- p
		}
	}()
	var l *Link
	done := make(chan error)
	go func() {
		var err error
		l, err = Serve(b, opt)
		done <- err
	}()
	c.send(Packet{TX: SeqInit, RX: SeqInit, Type: INIT, Data: initData()})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p := c.expect(ACK); p.TX != 0x7F || p.RX != 0x7F {
		t.Fatalf("INIT answered with %v", p)
	}
	t.Cleanup(func() { l.Close(); a.Close() })
	return c, l
}

func (c *client) send(p Packet) {
	p.Type |= FromClient
	if _, err := c.c.Write(Append(nil, p)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) next() (Packet, bool) {
	select {
	case p, ok := <-c.pkts:
		return p, ok
	case <-time.After(2 * time.Second):
		c.t.Fatal("nothing from the host")
		return Packet{}, false
	}
}

func (c *client) expect(kind uint8) Packet {
	c.t.Helper()
	p, ok := c.next()
	if !ok {
		c.t.Fatalf("link closed, wanted %02x", kind)
	}
	if p.Type != kind {
		c.t.Fatalf("got %v, wanted %02x", p, kind)
	}
	return p
}

func (c *client) quiet(d time.Duration) {
	c.t.Helper()
	select {
	case p := <-c.pkts:
		c.t.Fatalf("unexpected %v", p)
	case <-time.After(d):
	}
}

func read(t *testing.T, l *Link) []byte {
	t.Helper()
	got := make(chan []byte, 1)
	go func() {
		d, err := l.Read()
		if err != nil {
			t.Error(err)
		}
		got <- d
	}()
	select {
	case d := <-got:
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("nothing read")
		return nil
	}
}

func TestHandshake(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	go a.Write(Append(nil, Packet{TX: SeqInit, RX: SeqInit, Type: INIT | FromClient, Data: initData()}))
	got := make(chan []byte)
	go func() {
		buf := make([]byte, 64)
		n, _ := a.Read(buf)
		got <- buf[:n]
	}()
	l, err := Serve(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if g := <-got; !bytes.Equal(g, initACK) {
		t.Errorf("INIT answered with %x, want %x", g, initACK)
	}
	if in := l.Init(); in.Width != 800 {
		t.Errorf("Init: %+v", in)
	}

	// no INIT: Serve gives up
	a2, b2 := net.Pipe()
	go func() { a2.Write([]byte("hello\r")); a2.Close() }()
	if _, err := Serve(b2, Options{InitWait: time.Second}); !errors.Is(err, ErrNoInit) {
		t.Errorf("no INIT: %v", err)
	}
}

func TestData(t *testing.T) {
	c, l := dial(t, Options{AckDelay: 20 * time.Millisecond})

	// the client's DATA is read, and acknowledged soon after
	c.send(Packet{TX: 0x10, RX: 0x7F, Type: DATA, Data: []byte("Dd\x00\x15")})
	if d := read(t, l); string(d) != "Dd\x00\x15" {
		t.Errorf("read %q", d)
	}
	if p := c.expect(ACK); p.RX != 0x10 || p.TX != 0x7F {
		t.Errorf("ACK %v", p)
	}

	// the host's DATA carries the acknowledgement: no ACK as well
	c.send(Packet{TX: 0x11, RX: 0x7F, Type: DATA, Data: []byte("ya")})
	read(t, l)
	if err := l.WriteToken("SD", nil); err != nil {
		t.Fatal(err)
	}
	if p := c.expect(DATA); p.TX != 0x10 || p.RX != 0x11 || string(p.Data) != "SD" {
		t.Errorf("DATA %v", p)
	}
	c.quiet(60 * time.Millisecond)
	if l.Unacked() != 1 {
		t.Errorf("unacked %d", l.Unacked())
	}
	c.send(Packet{TX: 0x11, RX: 0x10, Type: ACK})
	time.Sleep(20 * time.Millisecond)
	if l.Unacked() != 0 {
		t.Errorf("unacked %d after ACK", l.Unacked())
	}

	// a DATA again (our ACK lost): acknowledged, not read twice
	c.send(Packet{TX: 0x11, RX: 0x10, Type: DATA, Data: []byte("ya")})
	if p := c.expect(ACK); p.RX != 0x11 {
		t.Errorf("ACK %v", p)
	}

	// one missing: NAKed once, then taken when it comes
	c.send(Packet{TX: 0x13, RX: 0x10, Type: DATA, Data: []byte("x3")})
	c.send(Packet{TX: 0x14, RX: 0x10, Type: DATA, Data: []byte("x4")})
	if p := c.expect(NAK); p.RX != 0x11 || !bytes.Equal(p.Data, []byte{NakSequence}) {
		t.Errorf("NAK %v", p)
	}
	c.quiet(60 * time.Millisecond)
	for i := uint8(0x12); i <= 0x14; i++ {
		c.send(Packet{TX: i, RX: 0x10, Type: DATA, Data: []byte{'x', '0' + i - 0x10}})
	}
	for _, w := range []string{"x2", "x3", "x4"} {
		if d := read(t, l); string(d) != w {
			t.Errorf("read %q, want %q", d, w)
		}
	}
	if p := c.expect(ACK); p.RX != 0x14 {
		t.Errorf("ACK %v", p)
	}

	// a bad CRC: NAKed
	b := Append(nil, Packet{TX: 0x15, RX: 0x10, Type: DATA | FromClient, Data: []byte("zz")})
	b[1] ^= 0xFF
	c.c.Write(b)
	if p := c.expect(NAK); p.RX != 0x14 || !bytes.Equal(p.Data, []byte{NakCRC}) {
		t.Errorf("NAK %v", p)
	}

	// a HEARTBEAT: ACKed at once
	c.send(Packet{TX: 0x14, RX: 0x10, Type: HEARTBEAT})
	if p := c.expect(ACK); p.RX != 0x14 {
		t.Errorf("ACK %v", p)
	}

	// AOL 4.0's "**" CRC
	b = Append(nil, Packet{TX: 0x15, RX: 0x10, Type: DATA | FromClient, Data: []byte("pE")})
	b[1], b[2] = '*', '*'
	c.c.Write(b)
	if d := read(t, l); string(d) != "pE" {
		t.Errorf("read %q", d)
	}
}

func TestAckEvery(t *testing.T) {
	c, l := dial(t, Options{AckDelay: time.Hour})
	seq := uint8(SeqInit)
	for i := range 8 {
		seq = next(seq)
		c.send(Packet{TX: seq, RX: SeqInit, Type: DATA, Data: []byte{'a', byte(i)}})
		read(t, l)
	}
	if p := c.expect(ACK); p.RX != seq {
		t.Errorf("ACK %v", p)
	}
}

func TestWindow(t *testing.T) {
	c, l := dial(t, Options{})
	wrote := make(chan int, 32)
	go func() {
		for i := range 20 {
			if err := l.Write([]byte{'w', byte(i)}); err != nil {
				t.Error(err)
				return
			}
			wrote <- i
		}
	}()
	var last Packet
	for range 16 {
		last = c.expect(DATA)
	}
	c.quiet(50 * time.Millisecond)
	if len(wrote) != 16 || last.TX != 0x1F {
		t.Fatalf("%d written, last %v", len(wrote), last)
	}
	// acknowledging 3 lets 3 more go
	c.send(Packet{TX: SeqInit, RX: 0x12, Type: ACK})
	for i := range 3 {
		if p := c.expect(DATA); p.TX != uint8(0x20+i) {
			t.Errorf("DATA %v", p)
		}
	}
	c.quiet(50 * time.Millisecond)

	// a NAK has all the unacknowledged sent again, from after its rx
	c.send(Packet{TX: SeqInit, RX: 0x15, Type: NAK, Data: []byte{NakSequence}})
	for i := 0x16; i <= 0x22; i++ {
		if p := c.expect(DATA); p.TX != uint8(i) || p.Data[1] != byte(i-0x10) {
			t.Fatalf("resent %v, want tx %02x", p, i)
		}
	}
	c.send(Packet{TX: SeqInit, RX: 0x22, Type: ACK})
	if p := c.expect(DATA); p.TX != 0x23 {
		t.Errorf("DATA %v", p)
	}
}

func TestWrap(t *testing.T) {
	c, l := dial(t, Options{AckDelay: time.Millisecond})
	const n = 300
	go func() {
		for i := range n {
			if err := l.Write([]byte(fmt.Sprint(i))); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	seq, rx := uint8(SeqInit), uint8(SeqInit)
	for i := 0; i < n; {
		p, _ := c.next()
		if p.Type != DATA {
			continue // its ACKs of ours
		}
		if p.TX != next(rx) || string(p.Data) != fmt.Sprint(i) {
			t.Fatalf("%d: %v %q", i, p, p.Data)
		}
		rx = p.TX
		i++
		// and the client talks back, acknowledging as it goes
		seq = next(seq)
		c.send(Packet{TX: seq, RX: rx, Type: DATA, Data: []byte(fmt.Sprint(i))})
		if d := read(t, l); string(d) != fmt.Sprint(i) {
			t.Fatalf("read %q", d)
		}
	}
}

func TestHeartbeat(t *testing.T) {
	c, l := dial(t, Options{Timeout: 30 * time.Millisecond, Retries: 3})
	l.Write([]byte("aa"))
	l.Write([]byte("ab"))
	c.expect(DATA)
	c.expect(DATA)
	// no acknowledgement: HEARTBEATs, and the client's ACK of only the
	// first has the second sent again
	if p := c.expect(HEARTBEAT); p.TX != 0x11 {
		t.Errorf("HEARTBEAT %v", p)
	}
	c.send(Packet{TX: SeqInit, RX: 0x10, Type: ACK})
	if p := c.expect(DATA); p.TX != 0x11 || string(p.Data) != "ab" {
		t.Errorf("resent %v", p)
	}
	// then silence: three HEARTBEATs and the link is down
	for range 3 {
		c.expect(HEARTBEAT)
	}
	select {
	case <-l.Done():
	case <-time.After(time.Second):
		t.Fatal("link still up")
	}
	if l.Err() != ErrTimeout {
		t.Errorf("Err %v", l.Err())
	}
	if err := l.Write([]byte("x")); err != ErrTimeout {
		t.Errorf("Write after: %v", err)
	}
	if _, err := l.Read(); err != ErrTimeout {
		t.Errorf("Read after: %v", err)
	}
}

func TestReinit(t *testing.T) {
	c, l := dial(t, Options{})
	l.Write([]byte("aa"))
	c.expect(DATA)
	// the client starts over: so does the host
	c.send(Packet{TX: SeqInit, RX: SeqInit, Type: INIT, Data: initData()})
	if p := c.expect(ACK); p.TX != SeqInit || p.RX != SeqInit {
		t.Errorf("ACK %v", p)
	}
	l.Write([]byte("ab"))
	if p := c.expect(DATA); p.TX != 0x10 {
		t.Errorf("DATA %v", p)
	}
}

func TestClose(t *testing.T) {
	c, l := dial(t, Options{})
	c.c.Close()
	select {
	case <-l.Done():
	case <-time.After(time.Second):
		t.Fatal("link still up")
	}
	if _, err := l.Read(); err == nil {
		t.Error("Read after close")
	}
	if err := l.Write(make([]byte, MaxData+1)); err != ErrTooBig {
		t.Error(err)
	}
}

func TestCloseSends(t *testing.T) {
	c, l := dial(t, Options{})
	l.WriteToken("XS", []byte("Goodbye!"))
	l.Close()
	if p := c.expect(DATA); string(p.Data) != "XSGoodbye!" {
		t.Errorf("DATA %v", p)
	}
	if _, ok := c.next(); ok {
		t.Error("not closed")
	}
}
