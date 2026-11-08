package server

import (
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/ui3270"
)

// TestCompuServeCB: two callers on the CB Simulator: handles, a channel,
// /UST, a monitored channel, scrambling and /EXI.
func TestCompuServeCB(t *testing.T) {
	e := startFull(t, ui3270.Graphics{}, nil, nil, cisListener())

	a := cisLogin(e, "72000,1", "alicepass1")
	a.send("GO CB\r")
	a.expect("CompuServe CB Simulator")
	a.expect("What's your handle? ")
	a.send("sysop\r")
	a.expect("That handle is another member's name.")
	a.expect("What's your handle? ")
	a.send("Red Leather\r")
	a.expect("What channel (1-36)? ")
	a.send("17\r")
	a.expect("(Tuned to channel 17, 0 others here)\r\n")

	b := cisLogin(e, "72000,2", "sysoppass1")
	b.send("GO CB\r")
	b.expect("What's your handle? ")
	b.send("Old Man\r")
	b.expect("What channel (1-36)? ")
	b.send("\r")
	b.expect("(Tuned to channel 1, 0 others here)\r\n")
	b.send("/MON 17\r")
	b.expect("Monitoring channels 17\r\n")

	a.send("Does anyone know what time it is?\r")
	b.expect("(17,Red Leather) Does anyone know what time it is?\r\n")
	b.send("/TUN 17\r")
	b.expect("(Tuned to channel 17, 1 other here)\r\n")
	a.expect("** Old Man tuned in to channel 17 **\r\n")
	b.send("Time for CB.\r")
	a.expect("(Old Man) Time for CB.\r\n")
	a.send("/WHO\r")
	a.expect("72000,2\r\n")
	a.send("/UST\r")
	a.expect("   Job   User Id   Ch.  Node  Handle\r\n   ---  ---------- ---  ----  ------\r\n")
	a.expect(" 72000,1     17  ")
	a.expect("Red Leather\r\n")
	a.expect(" 72000,2     17  ")
	a.expect("Old Man\r\n")

	// scrambled: clear to the same key, garbage to the rest
	a.send("/SCR secret\r")
	a.expect("Scrambled\r\n")
	a.send("meet at noon\r")
	b.expect("(Red Leather) ")
	b.expect("\r\n")
	if got := string(b.before); strings.Contains(got, "meet") {
		t.Errorf("unscrambled without the key: %q", got)
	}
	b.send("/SCR secret\r")
	b.expect("Scrambled\r\n")
	a.send("meet at noon\r")
	b.expect("(Red Leather) meet at noon\r\n")
	a.send("/UST 17\r")
	a.expect(" 17s  ")

	b.send("/EXI\r")
	b.expect("Thank you for using CB.")
	b.expect("Enter choice !")
	a.expect("** Old Man left channel 17 **")
	a.send("/OFF\r")
	a.expect("Thank you for using CompuServe!")
	b.send("OFF\r")
	b.expect("Thank you for using CompuServe!")
}
