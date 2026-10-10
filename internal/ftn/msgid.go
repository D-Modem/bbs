package ftn

import (
	"crypto/sha1"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
	"sync"
	"time"
)

var serial struct {
	sync.Mutex
	n uint32
}

// NewMSGID is a MSGID of a message written at a: its address and a serial
// number, from the clock and counting up (FTS-0009 asks that it not repeat
// for three years: one a second would take 136).
func NewMSGID(a Addr) string {
	serial.Lock()
	defer serial.Unlock()
	if t := uint32(time.Now().Unix()); serial.n < t {
		serial.n = t
	} else {
		serial.n++
	}
	return fmt.Sprintf("%s %08x", a.String(), serial.n)
}

// gateDomain is the right side of Message-IDs made of MSGIDs: as FidoGate
// makes them, so that the same message gated twice is seen to be one.
const gateDomain = "fidonet.org"

// MessageID is the RFC 5322 Message-ID of a message with that MSGID, as
// gateways write it: "2:5020/1 1234abcd" is
// <MSGID_2=3A5020=2F1_1234abcd@fidonet.org>.
func MessageID(msgid string) string {
	var b strings.Builder
	b.WriteString("<MSGID_")
	for i := 0; i < len(msgid); i++ {
		c := msgid[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('_')
		default:
			fmt.Fprintf(&b, "=%02X", c)
		}
	}
	b.WriteString("@" + gateDomain + ">")
	return b.String()
}

// NoMSGIDMessageID is a Message-ID for a message without a MSGID, made of
// what it says: the same message twice gets the same one.
func NoMSGIDMessageID(m *Message) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00", m.Area, m.From, m.To, m.Subject, m.DateTime)
	for _, l := range m.Lines {
		h.Write([]byte(l + "\r"))
	}
	return fmt.Sprintf("<NOMSGID_%x@%s>", h.Sum(nil)[:12], gateDomain)
}

// MSGIDOf is the MSGID to give, written at a, a message whose Message-ID
// is id: the one it was made of, if it was, else a's address and a CRC of
// id (as gateways do).
func MSGIDOf(id string, a Addr) string {
	if strings.HasPrefix(id, "<MSGID_") && strings.HasSuffix(id, "@"+gateDomain+">") {
		enc := id[len("<MSGID_") : len(id)-len("@"+gateDomain+">")]
		var b strings.Builder
		for i := 0; i < len(enc); i++ {
			switch c := enc[i]; {
			case c == '_':
				b.WriteByte(' ')
			case c == '=' && i+2 < len(enc):
				v, err := strconv.ParseUint(enc[i+1:i+3], 16, 8)
				if err != nil {
					return fmt.Sprintf("%s %08x", a, crc32.ChecksumIEEE([]byte(id)))
				}
				b.WriteByte(byte(v))
				i += 2
			default:
				b.WriteByte(c)
			}
		}
		if s := b.String(); strings.Count(s, " ") == 1 {
			return s
		}
	}
	return fmt.Sprintf("%s %08x", a, crc32.ChecksumIEEE([]byte(id)))
}
