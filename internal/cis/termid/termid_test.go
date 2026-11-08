package termid

import (
	"bufio"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	oz := "#OZ3,OzCIS1,AC,CA,SSxx,GF,PB,DT,+"
	oz += strconv.Itoa(Sum(oz))
	cases := []struct {
		in      string
		product string
		ap, b   bool
		err     error
	}{
		{"#IB1,PB,DT", "IB1", false, true, nil},
		{oz, "OZ3", false, true, nil},
		{"#WINCIM:3.0.1:WN6,PB,DT,CA,GF,AP,+1", "WINCIM", true, true, ErrBadChecksum},
		{"#PC3:3.2a:D,CC,GH,GM,GF,PB,+1680", "PC3", false, true, nil},
	}
	for _, c := range cases {
		caps, err := Parse(c.in)
		if err != c.err {
			t.Errorf("%q: %v", c.in, err)
		}
		if caps == nil || caps.Product != c.product || caps.AP() != c.ap || caps.B() != c.b {
			t.Errorf("%q: %+v", c.in, caps)
		}
	}
	c, _ := Parse("#PC3:3.2a:D,CC,GH,GM,GF,PB,+1680")
	if c.Version != "3.2a" || c.OS != "D" || !c.Has("GH") || c.Checksum != 1680 {
		t.Errorf("%+v", c)
	}
	if c, err := Parse("\x1e4"); err != nil || !c.Vidtex {
		t.Errorf("vidtex: %+v %v", c, err)
	}
}

func TestProbe(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	go func() {
		buf := make([]byte, 2)
		b.Read(buf)
		// a bad checksum, twice: taken
		b.Write([]byte("x#WINCIM:3.0.1:WN6,PB,AP,+1\r"))
		b.Read(buf)
		b.Write([]byte("#WINCIM:3.0.1:WN6,PB,AP,+1\r"))
	}()
	c, junk, err := Probe(a, bufio.NewReader(a), 10, time.Second)
	if err != nil || c == nil || !c.AP() || string(junk) != "x" {
		t.Fatalf("%+v %q %v", c, junk, err)
	}
	// nobody there
	x, y := net.Pipe()
	defer x.Close()
	go func() {
		buf := make([]byte, 8)
		for {
			if _, err := y.Read(buf); err != nil {
				return
			}
		}
	}()
	c, _, err = Probe(x, bufio.NewReader(x), 3, 50*time.Millisecond)
	if c != nil || err != nil {
		t.Errorf("silent: %+v %v", c, err)
	}
	y.Close()
}
