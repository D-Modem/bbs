package xfer

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// TestBChecks: the spec's packet DLE B '7' "TDAS.C" ETX has checksum 0x2A
// and CRC-16 (preset 0xFFFF) 0x57FF.
func TestBChecks(t *testing.T) {
	p := []byte{0x37, 0x54, 0x44, 0x41, 0x53, 0x2E, 0x43, 0x03}
	if c := BChecksum(p); c != 0x2A {
		t.Errorf("checksum %#x, want 0x2a", c)
	}
	if c := bCheck(CheckCRC16, p); !bytes.Equal(c, []byte{0x57, 0xFF}) {
		t.Errorf("CRC-16 % x, want 57 ff", c)
	}
	// the receiver's residues
	c := bCheck(CheckCCITT, p)
	if r := crcX25(append(append([]byte{}, p...), c...), 0xFFFF); r != 0xF0B8 {
		t.Errorf("CCITT residue %#x", r)
	}
}

// TestBParams: CompuServe's host quote masks are its set, and a '+' body
// reads back as it was.
func TestBParams(t *testing.T) {
	k := &BLink{}
	k.Params = HostParams
	k.applyParams()
	var set []byte
	for i, q := range k.quote {
		if q {
			set = append(set, byte(i))
		}
	}
	if want := []byte{3, 5, 0x10, 0x11, 0x13, 0x15, 0x1E, 0x91, 0x93}; !bytes.Equal(set, want) {
		t.Errorf("quote set % x, want % x", set, want)
	}
	if p := parseBParams(HostParams.bytes()); p != HostParams {
		t.Errorf("parsed %+v", p)
	}
	// short: OzCIS's
	oz := parseBParams([]byte{1, 2, 4, 1, 1, 0, 0x14, 0, 0xD4, 0, 0, 0, 0, 0, 0, 0, 1})
	n := negotiate(HostParams, oz)
	if n.WS != 1 || n.WR != 1 || n.BS != 4 || n.CM != 1 || n.FI != 0 {
		t.Errorf("negotiated %+v", n)
	}
}

// tcpPair is two ends of a loopback TCP connection (buffered, unlike
// net.Pipe, so both sides may write at once).
func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ch := make(chan net.Conn)
	go func() {
		c, _ := ln.Accept()
		ch <- c
	}()
	a, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b := <-ch
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// bNoisy corrupts every nth byte written.
type bNoisy struct {
	net.Conn
	mu sync.Mutex
	n  int
	i  int
}

func (c *bNoisy) Write(p []byte) (int, error) {
	c.mu.Lock()
	q := append([]byte{}, p...)
	for j := range q {
		if c.i++; c.n > 0 && c.i%c.n == 0 {
			q[j] ^= 0x5A
		}
	}
	c.mu.Unlock()
	_, err := c.Conn.Write(q)
	return len(p), err
}

func TestBTransfers(t *testing.T) {
	data := make([]byte, 10000)
	rand.Read(data)
	cases := []struct {
		name   string
		params *BParams
		noise  int
	}{
		{"classic", nil, 0},
		{"plus", &BParams{WS: 2, WR: 2, BS: 8, CM: CheckCRC16, DQ: 1}, 0},
		{"checksum", &BParams{WS: 0, WR: 0, BS: 4, CM: CheckSum}, 0},
		{"ccitt", &BParams{WS: 1, WR: 1, BS: 16, CM: CheckCCITT}, 0},
		{"crc32", &BParams{WS: 3, WR: 3, BS: 16, CM: CheckCRC32}, 0},
		{"classic bNoisy", nil, 997},
		{"plus bNoisy", &BParams{WS: 2, WR: 2, BS: 8, CM: CheckCRC16}, 1499},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := tcpPair(t)
			var hp, tp Port = a, b
			if c.noise > 0 {
				hp = &bNoisy{Conn: a, n: c.noise}
				tp = &bNoisy{Conn: b, n: c.noise + 2}
			}
			host, term := NewBLink(hp), NewBLink(tp)
			host.Timeout, term.Timeout = 300*time.Millisecond, 300*time.Millisecond
			host.Errors, term.Errors = 30, 30
			var got bytes.Buffer
			var gotName string
			var gotBin bool
			done := make(chan error, 1)
			go func() {
				if err := term.AnswerENQ(c.params); err != nil {
					done <- err
					return
				}
				err := term.Terminal(func(name string, bin bool) (io.Writer, error) {
					gotName, gotBin = name, bin
					return &got, nil
				}, nil)
				if err == nil {
					// then an upload
					err = term.Terminal(nil, func(name string) (io.Reader, error) {
						return bytes.NewReader(data[:3333]), nil
					})
				}
				done <- err
			}()
			if err := host.Start(HostParams); err != nil {
				t.Fatal("start:", err)
			}
			if host.PlusPlus != (c.params != nil) {
				t.Errorf("B Plus seen %v", host.PlusPlus)
			}
			n, err := host.SendFile("TEST.BIN", true, bytes.NewReader(data))
			if err != nil || n != int64(len(data)) {
				t.Fatalf("send: %d, %v", n, err)
			}
			var up bytes.Buffer
			un, err := host.ReceiveFile("UP.TXT", false, &up, 1<<20)
			if err != nil {
				t.Fatalf("receive: %d, %v", un, err)
			}
			if err := <-done; err != nil {
				t.Fatal("terminal:", err)
			}
			if gotName != "TEST.BIN" || !gotBin || !bytes.Equal(got.Bytes(), data) {
				t.Errorf("download: %q %v %d bytes, equal %v", gotName, gotBin, got.Len(), bytes.Equal(got.Bytes(), data))
			}
			if !bytes.Equal(up.Bytes(), data[:3333]) {
				t.Errorf("upload: %d bytes", up.Len())
			}
		})
	}
}

// TestBRefused: a terminal's F packet in answer to TD ends the download.
func TestBRefused(t *testing.T) {
	a, b := tcpPair(t)
	host, term := NewBLink(a), NewBLink(b)
	host.Timeout, term.Timeout = 300*time.Millisecond, 300*time.Millisecond
	go func() {
		term.AnswerENQ(nil)
		term.Terminal(func(string, bool) (io.Writer, error) { return nil, io.ErrClosedPipe }, nil)
	}()
	if err := host.Start(HostParams); err != nil {
		t.Fatal(err)
	}
	_, err := host.SendFile("X", true, bytes.NewReader([]byte("hello")))
	if f, ok := err.(*BFailure); !ok || f.Code != 'E' {
		t.Errorf("got %v", err)
	}
}

// TestBNoTerminal: a terminal that never answers ENQ times out.
func TestBNoTerminal(t *testing.T) {
	a, _ := tcpPair(t)
	host := NewBLink(a)
	host.Timeout = 50 * time.Millisecond
	if err := host.Start(HostParams); err != ErrTimeout {
		t.Errorf("got %v", err)
	}
}
