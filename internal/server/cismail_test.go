package server

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/ui3270"
	"github.com/D-Modem/bbs/internal/xfer"
)

// TestCompuServeMail: TAPCIS's PARAMS.SCR setup of Mail, a message sent
// to a User ID and read by its recipient in command mode, a download of
// it by B Plus, and a message uploaded TAPCIS's way.
func TestCompuServeMail(t *testing.T) {
	e := startFull(t, ui3270.Graphics{}, nil, nil, cisListener())

	d := cisLogin(e, "72000,1", "alicepass1")
	// PARAMS.SCR, as TAPCIS 5.4 sends it: every reply waits for "!"
	d.send("go mail\r")
	d.expect("CompuServe Mail  Main Menu")
	d.expect("Enter choice !")
	d.send("no\r")
	d.expect("% Invalid command\r\n")
	d.expect("!")
	for _, c := range []string{"set edit noline", "set page off", "set retain YES"} {
		d.send(c + "\r")
		d.expect("Enter choice !")
	}
	d.send("set\r")
	d.expect(" 2 MODE of operation is [MENU]")
	d.expect("!")
	d.send("2\r")
	d.expect("Mail  Mode Menu")
	d.expect("!")
	d.send("3\r")
	d.expect(" 2 MODE of operation is [COMMAND]")
	d.expect("!")
	d.send("\r")
	d.expect("future Mail sessions ? (Y or N) !")
	d.send("Y\r")
	d.expect("Mail!")
	d.send("ADD\r")
	d.expect("!")
	d.send("NAME\r")
	d.expect("  Old: alice\r\n  New: ")
	d.send("Alice A. [TAPCIS]\r")
	d.expect("return address? (Y or N) !")
	d.send("\x03")
	d.expect("!")
	d.send("\r") // the Address Book menu's exit
	d.expect("Mail!")

	// a message to the sysop by User ID, with a bad address first
	d.send("COMPOSE\r")
	d.expect("Enter /EX to exit EDIT\r\n")
	d.send("Hello from Mail.\r/ex\r")
	d.expect("Send to (Name or User ID): ")
	d.send("Bob >FAX:301-555-1212;\r")
	d.expect("% FAX mail is not offered on this service: Bob >FAX:301-555-1212\r\n")
	d.expect("Send to (Name or User ID): ")
	d.send("Sysop 72000,2\r")
	d.expect("Subject: ")
	d.send("Testing\r")
	d.expect("Send? (Y or N)! ")
	d.send("Y\r")
	d.expect("Message sent to sysop [72000,2]\r\n")
	d.expect("Mail!")
	d.send("OFF\r")
	d.expect("Thank you for using CompuServe!")

	d = cisLogin(e, "72000,2", "sysoppass1")
	d.expect("") // the banner came before the menu
	d.send("GO MAIL\r")
	d.expect("CompuServe Mail  Main Menu")
	d.expect(" 1 READ mail, 1 message pending")
	d.expect("Enter choice !")
	d.send("SET MODE COMMAND\r")
	d.expect("Mail!")
	d.send("SCAN\r")
	d.expect(" 1 alice/Testing\r\n")
	d.expect("Mail!")
	d.send("read all\r")
	d.expect("Date:  ")
	d.expect("\r\nFrom:  alice [72000,1]\r\nSubj:  Testing\r\n\r\nHello from Mail.\r\n")
	d.expect("Action!")
	d.send("\r")
	d.expect("Mail!")
	d.send("SCAN\r")
	d.expect("No messages pending")
	d.expect("Mail!")
	d.send("DOW SAVED /PROTO:B\r")
	d.expect("1 message and ")
	d.expect(" characters ready for download\r\n")
	term := xfer.NewBLink(d.port())
	if err := term.AnswerENQ(&xfer.BParams{WS: 1, WR: 1, BS: 8, CM: xfer.CheckCRC16}); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := term.Terminal(func(string, bool) (io.Writer, error) { return &got, nil }, nil); err != nil {
		t.Fatal(err)
	}
	d.got = term.Rest()
	if !strings.Contains(got.String(), "Subj:  Testing\r\n\r\nHello from Mail.\r\n") {
		t.Errorf("downloaded %q", got.String())
	}
	d.expect("*** File Transfer Completed! ***")
	d.expect("Mail!")

	// TAPCIS's upload of a reply
	d.send("UPL/PRO:B/TYP:ASCII MAIL%.TMP\r")
	d.expect("MAIL%.TMP\r\n")
	term = xfer.NewBLink(d.port())
	if err := term.AnswerENQ(nil); err != nil {
		t.Fatal(err)
	}
	if err := term.Terminal(nil, func(string) (io.Reader, error) {
		return strings.NewReader("Got it.\r\nThanks.\r\n"), nil
	}); err != nil {
		t.Fatal(err)
	}
	d.got = term.Rest()
	d.expect("Send to (Name or User ID): ")
	d.send(">INTERNET:72000.1@compuserve.com\r")
	d.expect("Subject: ")
	d.send("Re: Testing\r")
	d.expect("Send? (Y or N)! ")
	d.send("y\r")
	d.expect("Message sent to alice [72000,1]")
	d.expect("Mail!")
	d.send("READ SAVED\r")
	d.expect("Hello from Mail.")
	d.expect("Action!")
	d.send("DELETE\r")
	d.expect("Message deleted")
	d.expect("Mail!")
	d.send("EXIT\r")
	d.expect("Enter choice !")
	d.send("OFF\r")
	d.expect("Thank you for using CompuServe!")

	// alice's mode was kept: straight to "Mail!", and the reply is there
	d = cisLogin(e, "72000,1", "alicepass1")
	d.send("GO MAIL\r")
	d.expect("1 message pending")
	d.expect("Mail!")
	d.send("REA\r")
	d.expect("From:  sysop [72000,2]\r\nSubj:  Re: Testing\r\n\r\nGot it.\r\nThanks.\r\n")
	d.expect("Action!")
	d.send("OFF\r")
	d.expect("Thank you for using CompuServe!")
}
