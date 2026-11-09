package xfer

import (
	"bytes"
	"crypto/rand"
	"testing"
	"time"
)

// TestBTransport: the host's start as WinCIM sees it, then messages both
// ways, one of them several packets long, over a clean and a noisy line.
func TestBTransport(t *testing.T) {
	// a 1 KB packet quoted is up to 2 KB on the wire, so the noise is one
	// byte in 3001: one packet in two or so is hit
	for _, noise := range []int{0, 3001} {
		a, b := tcpPair(t)
		var hp, tp Port = a, b
		if noise > 0 {
			hp, tp = &bNoisy{Conn: a, n: noise}, &bNoisy{Conn: b, n: noise + 2}
		}
		big := make([]byte, 5000)
		rand.Read(big)
		done := make(chan error, 1)
		go func() {
			term, err := AnswerTL(tp, []byte{4, 4, 0, 2, 3, 1}, 300*time.Millisecond)
			if err != nil {
				done <- err
				return
			}
			if err := term.Send([]byte{3, 0x0F}); err != nil {
				done <- err
				return
			}
			m, err := term.Recv(5 * time.Second)
			if err == nil && !bytes.Equal(m, big) {
				t.Errorf("noise %d: the big message came as %d bytes", noise, len(m))
			}
			if err == nil {
				err = term.Send([]byte{1, 0x07})
			}
			done <- err
		}()
		host, err := NewBTransport(hp, 300*time.Millisecond)
		if err != nil {
			t.Fatalf("noise %d: start: %v", noise, err)
		}
		if !bytes.Equal(host.Their, []byte{4, 4, 0, 2, 3, 1}) {
			t.Errorf("their '+': % x", host.Their)
		}
		m, err := host.Recv(5 * time.Second)
		if err != nil || !bytes.Equal(m, []byte{3, 0x0F}) {
			t.Fatalf("noise %d: got % x, %v", noise, m, err)
		}
		if err := host.Send(big); err != nil {
			t.Fatal(err)
		}
		m, err = host.Recv(5 * time.Second)
		if err != nil || !bytes.Equal(m, []byte{1, 0x07}) {
			t.Fatalf("noise %d: got % x, %v", noise, m, err)
		}
		if err := <-done; err != nil {
			t.Fatalf("noise %d: terminal: %v", noise, err)
		}
	}
}

// TestBTransportWire: the host's first bytes are WinCIM's trace: ENQ, then
// DLE B '1' '+' 04 04 08 00 00 01 ETX and the checksum.
func TestBTransportWire(t *testing.T) {
	a, b := tcpPair(t)
	go NewBTransport(a, time.Second)
	buf := make([]byte, 1)
	b.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := b.Read(buf); err != nil || buf[0] != bENQ {
		t.Fatalf("% x %v", buf, err)
	}
	b.Write([]byte{bDLE, '+', '+', bDLE, '0'})
	raw := []byte{'1', '+', 4, 4, 8, 0, 0, 1, bETX}
	var want []byte
	want = append(want, bDLE, 'B', '1', '+', 4, 4, 8, 0, 0, 1)
	// every control byte quoted: 04 as DLE 'D', 08 as DLE 'H', 00 as DLE '@', 01 as DLE 'A'
	want = []byte{bDLE, 'B', '1', '+', bDLE, 0x44, bDLE, 0x44, bDLE, 0x48, bDLE, 0x40, bDLE, 0x40, bDLE, 0x41, bETX}
	sum := BChecksum(raw)
	if sum < 0x20 {
		want = append(want, bDLE, sum+0x40)
	} else if sum >= 0x80 && sum < 0xA0 {
		want = append(want, bDLE, sum&0x1F+0x60)
	} else {
		want = append(want, sum)
	}
	got := make([]byte, 0, len(want))
	for len(got) < len(want) {
		n, err := b.Read(buf)
		if err != nil {
			t.Fatalf("after % x: %v", got, err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got  % x\nwant % x", got, want)
	}
}
