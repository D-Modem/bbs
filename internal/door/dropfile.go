package door

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

// Drop files a door may want, by name.
const (
	DoorSys     = "door.sys"     // GAP, PCBoard and most doors since
	DorInfo     = "dorinfo1.def" // RBBS, QuickBBS
	Door32Sys   = "door32.sys"   // Door32: newer doors, socket or FOSSIL
	ChainTxt    = "chain.txt"    // WWIV
	DropDefault = "door.sys dorinfo1.def door32.sys chain.txt"
)

// DropFiles are those the BBS writes.
var DropFiles = []string{DoorSys, DorInfo, Door32Sys, ChainTxt}

// Info is what drop files say of the caller and the system.
type Info struct {
	Node       int
	UserID     int64
	UserName   string
	Location   string
	Sysop      bool
	Logins     int
	Posts      int
	LastLogin  time.Time
	ANSI       bool
	Rows, Cols int
	Baud       int // the caller's line; 38400 for a network caller
	TimeLeft   time.Duration
	BBSName    string
	SysopName  string
	ComPort    int // the door's COM port: 1
	CommType   int // DOOR32.SYS: 0 local, 1 serial (FOSSIL), 2 telnet
	Now        time.Time
}

// security is the level drop files give: 255 for sysops, 30 for others.
func (in *Info) security() int {
	if in.Sysop {
		return 255
	}
	return 30
}

// names splits a name into first and last, as DORINFO wants.
func names(n string) (string, string) {
	f := strings.Fields(n)
	switch len(f) {
	case 0:
		return "Unknown", "User"
	case 1:
		return f[0], ""
	}
	return f[0], strings.Join(f[1:], " ")
}

func yn(b bool) string {
	if b {
		return "Y"
	}
	return "N"
}

func onezero(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// cp437 is a line as DOS doors read it: CP437, no line breaks.
func cp437(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		if c, ok := charmap.CodePage437.EncodeRune(r); ok {
			b.WriteByte(c)
		} else {
			b.WriteByte('?')
		}
	}
	return b.String()
}

func (in *Info) doorSys() []string {
	ansi := "NG"
	if in.ANSI {
		ansi = "GR"
	}
	secs := int(in.TimeLeft / time.Second)
	last := in.LastLogin
	if last.IsZero() {
		last = in.Now
	}
	return []string{
		fmt.Sprintf("COM%d:", in.ComPort), // 1 the port
		fmt.Sprint(in.Baud),               // 2 the caller's rate
		"8",                               // 3 data bits
		fmt.Sprint(in.Node),               // 4 node
		fmt.Sprint(in.Baud),               // 5 the port's rate
		"Y", "N", "Y", "Y",                // 6-9 screen, printer, page bell, caller alarm
		in.UserName,                    // 10 name
		in.Location,                    // 11 calling from
		"000-000-0000", "000-000-0000", // 12-13 phones
		"",                        // 14 password: not given
		fmt.Sprint(in.security()), // 15 security level
		fmt.Sprint(in.Logins),     // 16 times on
		last.Format("01/02/06"),   // 17 last call
		fmt.Sprint(secs),          // 18 seconds left
		fmt.Sprint(secs / 60),     // 19 minutes left
		ansi,                      // 20 graphics
		fmt.Sprint(in.Rows),       // 21 screen length
		"Y",                       // 22 expert
		"", "1",                   // 23-24 conferences
		"12/31/99",            // 25 expiration
		fmt.Sprint(in.UserID), // 26 record number
		"Z",                   // 27 protocol
		"0", "0", "0", "9999", // 28-31 uploads, downloads, K today, K a day
		"01/01/80",   // 32 birthday
		`C:\`, `C:\`, // 33-34 main and gen dirs
		in.SysopName,  // 35 sysop
		in.UserName,   // 36 alias
		"00:00",       // 37 event
		"Y", "N", "Y", // 38-40 error correcting, ANSI in NG, record locking
		"7", "0", // 41-42 color, time credits
		last.Format("01/02/06"), // 43 last new files scan
		in.Now.Format("15:04"),  // 44 this call
		last.Format("15:04"),    // 45 last call
		"9999", "0", "0", "0",   // 46-49 files a day, today, K up, K down
		"",                   // 50 comment
		"0",                  // 51 doors opened
		fmt.Sprint(in.Posts), // 52 messages left
	}
}

func (in *Info) dorInfo() []string {
	sf, sl := names(in.SysopName)
	uf, ul := names(in.UserName)
	return []string{
		in.BBSName, sf, sl,
		fmt.Sprintf("COM%d", in.ComPort),
		fmt.Sprintf("%d BAUD,N,8,1", in.Baud),
		"0", uf, ul, in.Location,
		onezero(in.ANSI),
		fmt.Sprint(in.security()),
		fmt.Sprint(int(in.TimeLeft / time.Minute)),
		"-1", // a FOSSIL
	}
}

func (in *Info) door32Sys() []string {
	emu := "0"
	if in.ANSI {
		emu = "1"
	}
	return []string{
		fmt.Sprint(in.CommType),
		fmt.Sprint(in.ComPort), // the handle: for a FOSSIL, the port
		fmt.Sprint(in.Baud),
		"bbs-sec",
		fmt.Sprint(in.UserID),
		in.UserName, in.UserName,
		fmt.Sprint(in.security()),
		fmt.Sprint(int(in.TimeLeft / time.Minute)),
		emu,
		fmt.Sprint(in.Node),
	}
}

func (in *Info) chainTxt() []string {
	secs := int(in.TimeLeft / time.Second)
	midnight := time.Date(in.Now.Year(), in.Now.Month(), in.Now.Day(), 0, 0, 0, 0, in.Now.Location())
	return []string{
		fmt.Sprint(in.UserID), in.UserName, in.UserName, "", "21", "M", "0",
		in.LastLogin.Format("01/02/06"),
		fmt.Sprint(in.Cols), fmt.Sprint(in.Rows),
		fmt.Sprint(in.security()),
		onezero(in.Sysop), onezero(in.Sysop), onezero(in.ANSI), "1",
		fmt.Sprintf("%d.00", secs),
		`C:\`, `C:\`, "BBS.LOG",
		fmt.Sprint(in.Baud), fmt.Sprint(in.ComPort),
		in.BBSName, in.SysopName,
		fmt.Sprint(int(in.Now.Sub(midnight) / time.Second)),
		"0", "0", "0", "0", "0", "8N1",
		fmt.Sprint(in.Baud),
	}
}

// WriteDropFiles writes the drop files named (DOS style: upper case
// names, CP437, CR LF) into dir.
func WriteDropFiles(dir string, in *Info, which []string) error {
	for _, name := range which {
		var lines []string
		switch strings.ToLower(name) {
		case DoorSys:
			lines = in.doorSys()
		case DorInfo:
			lines = in.dorInfo()
		case Door32Sys:
			lines = in.door32Sys()
		case ChainTxt:
			lines = in.chainTxt()
		default:
			return fmt.Errorf("no such drop file: %s", name)
		}
		var b bytes.Buffer
		for _, l := range lines {
			b.WriteString(cp437(l) + "\r\n")
		}
		if err := os.WriteFile(filepath.Join(dir, strings.ToUpper(name)), b.Bytes(), 0644); err != nil {
			return err
		}
	}
	return nil
}
