package server

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
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
func TestCompuServeCIM(t *testing.T) {
	e := start(t, nil, cisListener())
	d := e.dial("cis.sock")
	d.expect("Host Name: ")
	d.send("CPS\r")
	d.expect("User ID: ")
	d.send("alice\r")
	d.expect("Password: ")
	d.send("alicepass1\r")
	d.expect("\x1bI")
	reply := "#WINCIM:3.0.1:WN6,PB,DT,CA,GF,AP,+"
	sum := 0
	for i := 0; i < len(reply); i++ {
		sum += int(reply[i])
	}
	d.send(reply + strconv.Itoa(sum) + "\r")
	d.expect("?LOG CAI")
	d.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64)
	for {
		if _, err := d.c.Read(buf); err != nil {
			break // hung up
		}
	}
}
