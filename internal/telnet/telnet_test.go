package telnet

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// TestIs3270: 3270 terminal types in any case are told, IBM's ASCII ones and
// others not.
func TestIs3270(t *testing.T) {
	for tt, want := range map[string]bool{
		"IBM-3278-2-E": true, "IBM-3279-4-E": true, "IBM-DYNAMIC": true, "ibm-3277-2": true,
		"IBM-3151": false, "XTERM": false, "ANSI": false, "": false,
	} {
		if Is3270(tt) != want {
			t.Errorf("Is3270(%q) != %v", tt, want)
		}
	}
}

// client plays a telnet client on c: it answers the server's requests and
// gives ttype as its terminal type; what it gets as data goes to data.
func client(c net.Conn, ttype string, data chan<- []byte) {
	buf := make([]byte, 1024)
	var got []byte
	for {
		n, err := c.Read(buf)
		if err != nil {
			data <- got
			return
		}
		b := buf[:n]
		for i := 0; i < len(b); i++ {
			if b[i] != IAC || i+1 >= len(b) {
				got = append(got, b[i])
				continue
			}
			switch b[i+1] {
			case IAC:
				got = append(got, IAC)
				i++
			case DO:
				switch b[i+2] {
				case OptTType, OptNAWS, OptSGA, OptBinary:
					c.Write([]byte{IAC, WILL, b[i+2]})
					if b[i+2] == OptNAWS {
						c.Write([]byte{IAC, SB, OptNAWS, 0, 132, 0, 50, IAC, SE})
					}
				default:
					c.Write([]byte{IAC, WONT, b[i+2]})
				}
				i += 2
			case WILL:
				c.Write([]byte{IAC, DO, b[i+2]})
				i += 2
			case DONT:
				c.Write([]byte{IAC, WONT, b[i+2]})
				i += 2
			case WONT:
				i += 2
			case SB:
				// SB TTYPE SEND IAC SE
				if b[i+2] == OptTType && b[i+3] == ttypeSend {
					msg := append([]byte{IAC, SB, OptTType, ttypeIs}, ttype...)
					c.Write(append(msg, IAC, SE))
				}
				for i < len(b) && b[i] != SE {
					i++
				}
			}
		}
	}
}

// TestNegotiate: a telnet client's terminal type comes upper-cased, the
// window size and binary are agreed, and an IAC written goes doubled.
func TestNegotiate(t *testing.T) {
	srv, cli := net.Pipe()
	data := make(chan []byte, 1)
	go client(cli, "xterm-256color", data)
	c := New(srv)
	tt, err := c.ProbeTermType(2 * time.Second)
	if err != nil || tt != "XTERM-256COLOR" {
		t.Fatalf("ProbeTermType = %q, %v", tt, err)
	}
	if !c.Spoke() {
		t.Error("Spoke false")
	}
	if err := c.StartNVT(true, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if cols, rows := c.Size(); cols != 132 || rows != 50 {
		t.Errorf("Size = %d, %d", cols, rows)
	}
	if !c.Binary() {
		t.Error("not binary")
	}
	c.Write([]byte{'a', IAC, 'b'})
	srv.Close()
	got := <-data
	if !bytes.HasSuffix(got, []byte{'a', IAC, 'b'}) {
		t.Errorf("client got %q", got)
	}
}

// TestReadStripsCommands: Read takes out commands and subnegotiations, gives
// an escaped IAC as one, and a CR NUL as a bare CR.
func TestReadStripsCommands(t *testing.T) {
	srv, cli := net.Pipe()
	c := New(srv)
	go func() {
		// data, a NOP, an escaped IAC, CR NUL, a subnegotiation, more data
		cli.Write([]byte{'h', 'i', IAC, NOP, IAC, IAC, '\r', 0, IAC, SB, OptNAWS, 0, 80, 0, 24, IAC, SE, '!'})
		cli.Close()
	}()
	// answers to nothing are never sent, so nothing reads cli
	got, err := io.ReadAll(c)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if want := []byte{'h', 'i', IAC, '\r', '!'}; !bytes.Equal(got, want) {
		t.Errorf("Read = %q, want %q", got, want)
	}
}

// TestProbeRawClient: a client that never answers gives no terminal type,
// has not spoken telnet, and the probe gives up at its timeout.
func TestProbeRawClient(t *testing.T) {
	srv, cli := net.Pipe()
	go io.Copy(io.Discard, cli) // a client that never answers
	c := New(srv)
	start := time.Now()
	tt, err := c.ProbeTermType(200 * time.Millisecond)
	if err != nil || tt != "" || c.Spoke() {
		t.Errorf("raw client: %q, %v, spoke %v", tt, err, c.Spoke())
	}
	if time.Since(start) > 2*time.Second {
		t.Error("probe did not time out")
	}
	srv.Close()
}

// TestHandoff3270: a 3270 client is told, and handing it off turns the
// terminal type option off again.
func TestHandoff3270(t *testing.T) {
	srv, cli := net.Pipe()
	data := make(chan []byte, 1)
	go client(cli, "IBM-3278-2-E", data)
	c := New(srv)
	tt, _ := c.ProbeTermType(2 * time.Second)
	if !Is3270(tt) {
		t.Fatalf("ttype %q", tt)
	}
	raw, err := c.Handoff3270(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// the option is off again: go3270 can ask for it from the start
	if c.him[OptTType] || c.askHim[OptTType] {
		t.Error("TTYPE still on")
	}
	raw.Close()
}

// TestBinaryKeepsNUL: in binary, the NUL after a CR is data.
func TestBinaryKeepsNUL(t *testing.T) {
	srv, cli := net.Pipe()
	c := New(srv)
	c.him[OptBinary] = true
	go func() {
		cli.Write([]byte{'\r', 0, 'x'})
		cli.Close()
	}()
	got, _ := io.ReadAll(c)
	if !bytes.Equal(got, []byte{'\r', 0, 'x'}) {
		t.Errorf("binary: %q", got)
	}
}
