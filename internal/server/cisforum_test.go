package server

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/ui3270"
	"github.com/D-Modem/bbs/internal/xfer"
)

// cisLogin logs a user on to the CompuServe listener, to the TOP menu.
func cisLogin(e *env, id, pw string) *dialog {
	d := e.dial("cis.sock")
	d.expect("Host Name: ")
	d.send("CIS\r")
	d.expect("User ID: ")
	d.send(id + "\r")
	d.expect("Password: ")
	d.send(pw + "\r")
	d.expect("Enter choice !")
	return d
}

// TestCompuServeForums: the message areas as forums: a post from one
// caller, read, replied to and scanned by another in the period's format.
func TestCompuServeForums(t *testing.T) {
	e := startFull(t, ui3270.Graphics{}, nil, func(b *bbs.BBS, dir string) {
		for _, name := range []string{"Hardware", "Software"} {
			a, err := b.Store.CreateArea(name, "", false)
			if err != nil {
				t.Fatal(err)
			}
			b.Store.SetAreaGroup(a.ID, "Amiga Tech")
		}
	}, cisListener())

	d := cisLogin(e, "72000,1", "alicepass1")
	d.send("GO FORUMS\r")
	d.expect(" 1 Test BBS Forum")
	d.expect(" 2 Amiga Tech Forum")
	d.expect("Enter choice !")
	d.send("2\r")
	d.expect("Welcome to Amiga Tech Forum, V. 4D(1)")
	d.expect("Hello, alice")
	d.expect("Forum !")
	d.send("DES\r")
	d.expect(" 0 Hardware\r\n 1 Software\r\n")
	d.expect("Forum !")
	d.send("POST SEC:1\r")
	d.expect("To: ")
	d.send("sysop\r")
	d.expect("Subject: ")
	d.send("Kickstart 1.3\r")
	d.expect("Enter /EX to exit EDIT\r\n")
	d.send("Where do I find it?\r/ex\r")
	d.expect("Post action !")
	d.send("S\r")
	d.expect("Message # ")
	d.expect(" Stored")
	d.expect("Forum !")
	d.send("EXIT\r")
	d.expect("Thank you for visiting Amiga Tech Forum")
	d.expect("Enter choice !")
	d.send("OFF\r")
	d.expect("Thank you for using CompuServe!")

	d = cisLogin(e, "72000,2", "sysoppass1")
	d.send("GO AMIGATECH\r")
	d.expect("Forum messages:")
	d.expect("You have 1 message(s) waiting.")
	d.expect("Forum !")
	d.send("OPT;TYP N;SKI N;MOD COM\r")
	d.expect("Options set")
	d.expect("Forum !")
	d.send("REA NEW\r")
	d.expect("#: ")
	d.expect(" S1/Software\r\n    ")
	d.expect("\r\nSb: Kickstart 1.3\r\nFm: alice 72000,1\r\nTo: sysop 72000,2\r\n\r\nWhere do I find it?\r\n")
	d.expect("Forum !")
	d.send("OPT;TYP P\r")
	d.expect("Forum !")
	d.send("REA ALL\r")
	d.expect("Where do I find it?")
	d.expect("Read action !")
	d.send("RE\r")
	d.expect("Enter /EX to exit EDIT")
	d.send("In ROM.\r/post\r")
	d.expect(" Stored")
	d.expect("Read action !")
	d.send("\r")
	d.expect("Forum !")
	d.send("MESS;SCA ALL;M\r")
	d.expect("Sb: #Kickstart 1.3\r\n")
	d.expect("-Kickstart 1.3\r\nFm: sysop 72000,2\r\nTo: alice 72000,1\r\n")
	d.expect("Forum !")
	d.send("SCA QUI ALL\r")
	d.expect(" S1  Kickstart 1.3\r\n")
	d.send("REA ALL FROM:sysop\r")
	d.expect("In ROM.")
	d.expect("Read action !")
	d.send("P\r") // the parent
	d.expect("Where do I find it?\r\n\r\n* Reply:  ")
	d.expect("Read action !")
	d.send("STOP\r")
	d.expect("Forum !")
	// what TAPCIS sends (its scripts wait for "um !" and "es !")
	for _, c := range []string{"OPT;TYP N;SKI N;MOD COM;INI;1;CHA;P", "MESS;SCA QUI NEW;M",
		"REA NEW", "REA WAI", "MESS;SCA WAI", "REA THR NUM:U", "RS"} {
		d.send(c + "\r")
		d.expect(" !")
		if b := string(d.before); !strings.HasSuffix(b, "Forum") && !strings.HasSuffix(b, "Messages") ||
			strings.Contains(b, "not a forum command") {
			t.Errorf("%s: %s", c, d.before)
		}
	}
	d.send("OFF\r")
	d.expect("Exiting at ")
	d.expect("Thank you for using CompuServe!")
}

// TestCompuServeLibraries: a forum's library, its files listed the
// period's way, one downloaded by B Plus and one uploaded by Classic B.
func TestCompuServeLibraries(t *testing.T) {
	data := bytes.Repeat([]byte("CompuServe B Plus test data\r\n"), 300)
	e := startFull(t, ui3270.Graphics{}, nil, func(b *bbs.BBS, dir string) {
		a, err := b.Store.FileAreaByName("Uploads")
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "bplus.zip")
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Files.Put(a, p, "BPLUS.ZIP", "B Plus for everyone", "Keywords: BPLUS PROTOCOL\nThe protocol, in Go.", "sysop", ""); err != nil {
			t.Fatal(err)
		}
	}, cisListener())

	d := cisLogin(e, "72000,1", "alicepass1")
	d.send("GO TESTBBS\r")
	d.expect("Forum !")
	d.send("LIB 0;DIR /DES\r")
	d.expect("BPLUS.ZIP/binary ")
	d.expect("Accesses: 0\r\n")
	d.expect("    Title   : B Plus for everyone\r\n    Keywords: BPLUS PROTOCOL\r\n\r\n    The protocol, in Go.\r\n")
	d.expect("LIB !")
	d.send("SCA\r")
	d.expect("BPLUS.ZIP/Bin  Bytes: 8700\r\n  Title   : B Plus for everyone\r\n")
	d.expect("LIB !")
	d.send("DOW BPLUS.ZIP /PROTO:B\r")
	d.expect("DOW BPLUS.ZIP /PROTO:B\r\n")
	term := xfer.NewBLink(d.port())
	if err := term.AnswerENQ(&xfer.BParams{WS: 1, WR: 1, BS: 8, CM: xfer.CheckCRC16}); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	var name string
	if err := term.Terminal(func(n string, bin bool) (io.Writer, error) { name = n; return &got, nil }, nil); err != nil {
		t.Fatal(err)
	}
	d.got = term.Rest()
	if name != "BPLUS.ZIP" || !bytes.Equal(got.Bytes(), data) {
		t.Errorf("downloaded %q, %d bytes", name, got.Len())
	}
	d.expect("LIB !")
	d.send("DIR BPLUS.ZIP\r")
	d.expect("Accesses: 1\r\n")
	d.expect("LIB !")

	d.send("UPL NOTES.TXT /PROTO:B /TYPE:ASC\r")
	d.expect("/TYPE:ASC\r\n")
	term = xfer.NewBLink(d.port())
	if err := term.AnswerENQ(nil); err != nil {
		t.Fatal(err)
	}
	if err := term.Terminal(nil, func(n string) (io.Reader, error) {
		if n != "NOTES.TXT" {
			t.Errorf("asked for %q", n)
		}
		return strings.NewReader("my notes\r\n"), nil
	}); err != nil {
		t.Fatal(err)
	}
	d.got = term.Rest()
	d.expect("Title: ")
	d.send("My notes\r")
	d.expect("Keywords: ")
	d.send("notes test\r")
	d.expect("Enter /EX to exit EDIT")
	d.send("Notes I took.\r/ex\r")
	d.expect("Is this okay (Y or N)? ")
	d.send("y\r")
	d.expect("File NOTES.TXT stored in library")
	d.expect("LIB !")
	d.send("SCA /DES\r")
	d.expect("NOTES.TXT  Bytes: 10\r\n  Title   : My notes\r\n  Keywords: NOTES TEST\r\n")
	d.send("M\r")
	d.expect("Forum !")
	d.send("OFF\r")
	d.expect("Thank you for using CompuServe!")
}

// port is the dialog's connection for a transfer protocol, starting with
// what the dialog has read and not yet looked at.
func (d *dialog) port() xfer.Port {
	p := &prefixed{Conn: d.c, pre: d.got}
	d.got = nil
	return p
}

type prefixed struct {
	net.Conn
	pre []byte
}

func (p *prefixed) Read(b []byte) (int, error) {
	if len(p.pre) > 0 {
		n := copy(b, p.pre)
		p.pre = p.pre[n:]
		return n, nil
	}
	return p.Conn.Read(b)
}
