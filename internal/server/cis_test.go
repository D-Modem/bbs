package server

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/cis"
	"github.com/D-Modem/bbs/internal/xfer"
)

func cisListener() *Listener {
	return &Listener{Name: "cis", Kind: "raw", Address: "cis.sock", Frontend: "compuserve", Connect: "9600"}
}

// TestCompuServe: a terminal caller logs on with a User ID, finds their
// way through the pages and signs off; then a bad password, and a name
// for the User ID.
func TestCompuServe(t *testing.T) {
	e := start(t, func(s *bbs.Settings) { s.FailDelay = 0 }, cisListener())
	d := e.dial("cis.sock")
	d.expect("CONNECT 9600\r\n")
	d.expect("Host Name: ")
	d.send("CIS\r")
	d.expect("User ID: ")
	d.send("72000,1\r")
	d.expect("Password: ")
	d.send("wrong\r")
	d.expect("?? LOGINE - Invalid entry - try again")
	d.expect("User ID: ")
	d.send("72000,1;TTY\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("\x1bI") // the terminal's identification, unanswered
	d.expect("CompuServe Information Service\r\n")
	d.expect("CompuServe" + strings.Repeat(" ", 80-len("CompuServe")-len("TOP")) + "TOP\r\n")
	if strings.Contains(string(d.before), "Your User ID") {
		t.Error("the User ID shown to a caller who gave it")
	}
	d.expect(" 1 Member Assistance (FREE)\r\n")
	d.expect("12 Business/Other Interests\r\n")
	d.expect("Enter choice !")
	d.send("1\r")
	d.expect("MEMBER\r\n")
	d.expect("Enter choice !")
	d.send("3\r")
	d.expect("RATES\r\n")
	d.expect("Connect time on this service is free")
	d.expect("Last page !")
	d.send("\r") // back to the menu
	d.expect(" 3 Rates and Billing")
	d.expect("Enter choice !")
	d.send("GO BILLS\r")
	d.expect("BILLS is unrecognized !")
	d.send("SET BRI ON;GO SOURCE\r")
	d.expect("OK\r\n")
	d.expect("https://github.com/D-Modem/bbs")
	d.expect("\r\n!")
	d.send("FIND mail\r")
	d.expect("Index of MAIL")
	d.expect("[GO MAIL]")
	d.send("T\r")
	d.expect(" 2 Find a Topic")
	d.send("off\r")
	d.expect("Thank you for using CompuServe!\r\nOff at ")
	d.expect("Connect time = 0:01\r\n")

	d = e.dial("cis.sock")
	d.expect("Host Name: ")
	d.send("cps\r")
	d.expect("User ID: ")
	d.send("sysop/INT:60\r")
	d.expect("Password: ")
	d.send("sysoppass1\r")
	d.expect("Your User ID is 72000,2")
	d.send("LOG\r")
	d.expect("Thank you for using CompuServe!")
	d.expect("User ID: ")
}

// TestCompuServeCIM: a CIM client (AP in its identification) is turned
// away with ?LOG CAI until the HMI service is here.
// cimReply is a CIM's ESC I answer, its sum after the '+'.
func cimReply() string {
	reply := "#WINCIM:3.0.1:WN6,PB,DT,CA,GF,GJ,GP,+,MS,AP,PB,LA1,CH1,CO0,+"
	sum := 0
	for i := 0; i < len(reply); i++ {
		sum += int(reply[i])
	}
	return reply + strconv.Itoa(sum) + "\r"
}

// cimStart is a CIM's side after its login: the ESC I answer, the terminal
// mode's trade (once with a bad sum), the Transport Layer.
func cimStart(t *testing.T, d *dialog) *xfer.BTransport {
	t.Helper()
	d.expect("\x1bI")
	d.send(cimReply())
	d.expect("CompuServe Information Service")
	d.expect("\x1b[>0q")
	d.send("#1,2,3,6,8,10\r")
	d.expect("\x1b[>3;3p")
	d.send("#3,2,1,4,2,0;613\r")
	d.expect("\x15")
	d.expect("\x1b[>3;3p")
	d.send("#3,2,1,4,2,0;614\r")
	d.expect("\x06")
	tl, err := xfer.AnswerTL(d.port(), []byte{4, 4, 8, 0, 0, 1}, 2*time.Second)
	if err != nil {
		t.Fatalf("Transport Layer: %v", err)
	}
	return tl
}

// TestCompuServeCIM: WinCIM's way in, as recovered from it: a password
// login (which keeps the Secure Login key), the mode's trade, the B Plus
// Transport Layer, and HMI to the main menu, an article and the rest;
// then a Secure Login.
func TestCompuServeCIM(t *testing.T) {
	e := start(t, func(s *bbs.Settings) { s.FailDelay = 0 }, cisListener())
	d := e.dial("cis.sock")
	d.expect("Host Name: ")
	d.send("CPS\r")
	d.expect("User ID: ")
	d.send("alice\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	tl := cimStart(t, d)

	ask := func(m []byte, want ...byte) []byte {
		t.Helper()
		if err := tl.Send(m); err != nil {
			t.Fatalf("send % x: %v", m, err)
		}
		got, err := tl.Recv(5 * time.Second)
		if err != nil {
			t.Fatalf("after % x: %v", m, err)
		}
		if !bytes.HasPrefix(got, want) {
			t.Fatalf("after % x: got % x, want % x...", m, got, want)
		}
		return got
	}
	// the first answer comes twice
	ask([]byte{3, 0x0F}, 1, 1, 0x0F)
	if got, err := tl.Recv(5 * time.Second); err != nil || !bytes.Equal(got, []byte{1, 1, 0x0F}) {
		t.Fatalf("the first ack again: % x %v", got, err)
	}
	ask([]byte{1, 0x0B}, 1, 1, 0x0B)
	got := ask(append([]byte{1, 0x20, 0, 0}, "CIS:TOP\x00"...), 1, 0x21, 0, 0)
	if !bytes.Contains(got, []byte("\x00CIS\x00")) {
		t.Errorf("open: % x", got)
	}
	got = ask(append([]byte{1, 0x24}, "CIS:TOP\x00"...), append([]byte{1, 0x27, 0}, "CIS:TOP\x00"...)...)
	if got[3+8+3] != 12 || !bytes.Contains(got, []byte("\x01\x00Member Assistance (FREE)\x00")) {
		t.Errorf("TOP: %q", got)
	}
	ask(append([]byte{1, 0x34}, "CIS:TOP\x00\x00\x0D"...), 1, 0x35, 0)
	got = ask(append([]byte{1, 0x25}, "CIS:TOP\x00\x01"...), append([]byte{1, 0x27, 0}, "CIS:MEMBER\x00"...)...)
	ask(append([]byte{1, 0x24}, "HOM:MAILHMI\x00"...), append([]byte{1, 0x28}, "CIS:MAIL\x00"...)...)
	got = ask(append([]byte{1, 0x29}, "CIS:MAIL\x00\x00\x00\xDC\x05"...), 1, 0x04, 0x29, ' ')
	if !bytes.Contains(got, []byte("CompuServe Mail")) {
		t.Errorf("mail's text: %q", got)
	}
	ask([]byte{1, 0x2A, 0xDC, 0x05}, 1, 0x2B, 0)
	ask(append([]byte{1, 0x24}, "CIS:NEWPAGE\x00"...), append([]byte{1, 0x28}, "CIS:NEW\x00"...)...)
	ask(append([]byte{1, 0x24}, "HOM:XYZZY\x00"...), append([]byte{1, 0x28}, "CIS:XYZZY\x00"...)...)
	got = ask(append([]byte{1, 0x22, 0x14}, "rates\x00"...), 1, 0x23)
	if got[2] == 0 || !bytes.Contains(got, []byte("CIS:RATES\x00")) {
		t.Errorf("find: %q", got)
	}
	ask(append([]byte{1, 0x0D, 0xBF, 4, 0, 2}, "CSRV\x00HRB\x00"...), 1, 0x0E, 0, 0)
	if err := tl.Send([]byte{1, 0x07}); err != nil {
		t.Fatal(err)
	}
	d.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64)
	for {
		if _, err := d.c.Read(buf); err != nil {
			break // hung up
		}
	}

	// the Secure Login, with the key the password login kept
	secure := func(pw string) *dialog {
		d := e.dial("cis.sock")
		d.expect("Host Name: ")
		d.send("CPS\r")
		d.expect("User ID: ")
		d.send("+72000,1/INT:60\r")
		d.expect("RA:\r\n")
		d.expect(".\r\n")
		rb, ur, hostOK, err := cis.SecureAnswer(pw, string(d.before))
		if err != nil {
			t.Fatal(err)
		}
		d.send("RB:\r\n" + rb + ".\r\nUR:\r\n" + ur + ".\r\n")
		if pw != "alicepass1" {
			d.expect("No.\r\n")
			return d
		}
		d.expect("HR:\r\n")
		d.expect(".\r\n")
		if !hostOK(string(d.before)) {
			t.Errorf("the host's HR: %q", d.before)
		}
		return d
	}
	secure("wrong")
	d = secure("alicepass1")
	tl = cimStart(t, d)
	ask([]byte{3, 0x0F}, 1, 1, 0x0F)
}
