package ftn

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// TIC is a file echo's control file (FTS-5006): what file it goes with,
// in which file echo, from where, by which way.
type TIC struct {
	Area     string // the file echo's tag
	AreaDesc string
	Origin   Addr // the system that hatched it
	From     Addr // the system that sent it here
	To       string
	File     string // its name (8.3 by old systems)
	LFile    string // its long name, if it has one
	Replaces string // a file it takes the place of
	Desc     string
	LDesc    []string
	Size     int64
	CRC      uint32 // CRC-32 of the file
	HasCRC   bool
	Magic    string
	Created  string   // by what
	Path     []string // "address unixtime date", a line a system
	SeenBy   []Addr
	Pw       string
}

// MaxTIC is the largest TIC read.
const MaxTIC = 64 << 10

// ParseTIC reads a TIC; keywords it does not know are skipped.
func ParseTIC(b []byte) (*TIC, error) {
	if len(b) > MaxTIC {
		return nil, errors.New("the TIC is too big")
	}
	t := &TIC{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 1024), MaxTIC)
	sc.Split(scanLinesCR)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t\x1a")
		key, val, _ := strings.Cut(strings.TrimLeft(line, " \t"), " ")
		val = strings.TrimSpace(val)
		addr := func() Addr {
			f := strings.Fields(val)
			if len(f) == 0 {
				return Addr{}
			}
			a, _ := ParseAddr(f[0])
			return a
		}
		switch strings.ToLower(key) {
		case "area":
			t.Area = strings.ToUpper(val)
		case "areadesc":
			t.AreaDesc = val
		case "origin":
			t.Origin = addr()
		case "from":
			t.From = addr()
		case "to":
			t.To = val
		case "file":
			t.File = val
		case "lfile", "fullname":
			t.LFile = val
		case "replaces":
			t.Replaces = val
		case "desc":
			if t.Desc == "" {
				t.Desc = val
			} else {
				t.LDesc = append(t.LDesc, val)
			}
		case "ldesc":
			t.LDesc = append(t.LDesc, val)
		case "size":
			t.Size, _ = strconv.ParseInt(val, 10, 64)
		case "crc":
			if c, err := strconv.ParseUint(val, 16, 32); err == nil {
				t.CRC, t.HasCRC = uint32(c), true
			}
		case "magic":
			t.Magic = val
		case "created":
			t.Created = val
		case "path":
			t.Path = append(t.Path, val)
		case "seenby":
			if a := addr(); a.Zone != 0 {
				t.SeenBy = append(t.SeenBy, a)
			}
		case "pw":
			t.Pw = val
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if t.Area == "" || t.File == "" {
		return nil, errors.New("a TIC needs an Area and a File")
	}
	return t, nil
}

// Name is the file's name: its long one where it has one.
func (t *TIC) Name() string {
	if t.LFile != "" {
		return t.LFile
	}
	return t.File
}

// Seen reports whether an address is in its SEEN-BY.
func (t *TIC) Seen(a Addr) bool {
	for _, s := range t.SeenBy {
		if s.Same(a) {
			return true
		}
	}
	return false
}

// Bytes writes the TIC, CR LF ended.
func (t *TIC) Bytes() []byte {
	var b bytes.Buffer
	w := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s %s\r\n", k, oneLine(v))
		}
	}
	w("Area", t.Area)
	w("Areadesc", t.AreaDesc)
	w("Origin", t.Origin.String())
	w("From", t.From.String())
	w("To", t.To)
	w("File", t.File)
	w("Lfile", t.LFile)
	w("Replaces", t.Replaces)
	w("Desc", t.Desc)
	for _, l := range t.LDesc {
		fmt.Fprintf(&b, "Ldesc %s\r\n", oneLine(l))
	}
	fmt.Fprintf(&b, "Size %d\r\n", t.Size)
	fmt.Fprintf(&b, "Crc %08X\r\n", t.CRC)
	w("Magic", t.Magic)
	w("Created", t.Created)
	for _, p := range t.Path {
		w("Path", p)
	}
	for _, s := range t.SeenBy {
		w("Seenby", s.String())
	}
	w("Pw", t.Pw)
	return b.Bytes()
}

// oneLine is a value made safe for a TIC's line.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}
