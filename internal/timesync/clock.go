// Package timesync keeps the BBS's time and serves it.  The clock follows
// time servers (NTP, NTS-secured NTP, PTP) or the system's own; it serves
// NTP (RFC 5905) with NTS (RFC 8915) and its key exchange, and PTP
// (IEEE 1588) as a grandmaster, by multicast and by unicast negotiation.
//
// The protocols' packets are github.com/facebook/time's (Apache-2.0); the
// servers and clients are the BBS's own.
package timesync

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// unsynced is the stratum of a clock that follows nothing.
const unsynced = 16

// Ref is what the clock follows, as the servers tell it.
type Ref struct {
	Synced    bool
	Stratum   uint8 // the BBS's own: 1 beside a reference clock, 16 unsynchronised
	RefID     uint32
	Leap      uint8 // NTP's leap indicator: 0, 1 (61 s), 2 (59 s), 3 (unsynchronised)
	RootDelay time.Duration
	RootDisp  time.Duration
	Updated   time.Time  // when it was last set (NTP's reference time)
	Source    string     // "nts time.example.org", "system"
	TimeSrc   TimeSource // where the time comes from, for PTP
	Offset    time.Duration
	Delay     time.Duration
}

// TimeSource is where the time comes from, for PTP's announcements.
type TimeSource uint8

// PTP's time sources (IEEE 1588 table 6).
const (
	SourceGNSS     TimeSource = 0x20
	SourcePTP      TimeSource = 0x40
	SourceNTP      TimeSource = 0x50
	SourceOther    TimeSource = 0x90
	SourceInternal TimeSource = 0xa0
)

// Local is how the clock tells of itself when it follows no server: the
// system clock, which something else keeps (chrony, ntpd, a GPS).
type Local struct {
	Stratum int    // 0: 3 while the kernel says it is synchronised, else 16
	RefID   string // stratum 1's: GPS, PPS ...
}

// Clock is the BBS's time: the system's, corrected by the offset the
// servers it follows measure, or the system's set by it.
type Clock struct {
	Local    Local
	SetClock bool                  // sets the system clock (else corrects its own reading)
	Log      func(string, ...any) // nil: none

	mu       sync.Mutex
	offset   time.Duration // added to the system clock's reading
	ref      Ref
	followed bool // a server's time has been taken
	setWarn  bool
}

// Now is the time, corrected.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	o := c.offset
	c.mu.Unlock()
	return time.Now().Add(o)
}

// Correct is a reading of the system clock (a packet's timestamp) corrected.
func (c *Clock) Correct(t time.Time) time.Time {
	c.mu.Lock()
	o := c.offset
	c.mu.Unlock()
	return t.Add(o)
}

// maxDrift is how fast an unsynchronised clock's error is taken to grow
// (RFC 5905's PHI).
const maxDrift = 15e-6

// Ref is what the clock follows now: its dispersion grown with its age;
// with no server followed, the system clock's.
func (c *Clock) Ref() Ref {
	c.mu.Lock()
	r, followed := c.ref, c.followed
	c.mu.Unlock()
	if !followed {
		return c.local()
	}
	age := time.Since(r.Updated)
	r.RootDisp += time.Duration(float64(age) * maxDrift)
	if age > 24*time.Hour {
		r.Synced = false
	}
	if !r.Synced {
		r.Stratum, r.Leap = unsynced, 3
	}
	return r
}

// local is the system clock's Ref.
func (c *Clock) local() Ref {
	k := kernelStatus()
	r := Ref{Source: "system", Synced: k.synced, Stratum: 3, RootDisp: k.maxError,
		Updated: time.Now().Truncate(time.Minute), TimeSrc: SourceInternal, RefID: refASCII("LOCL")}
	if c.Local.Stratum > 0 {
		r.Synced, r.Stratum = true, uint8(c.Local.Stratum)
		if r.Stratum == 1 && c.Local.RefID != "" {
			r.RefID = refASCII(c.Local.RefID)
			r.TimeSrc = SourceOther
			switch strings.ToUpper(c.Local.RefID) {
			case "GPS", "GNSS", "GAL", "BDS", "GLO", "PPS":
				r.TimeSrc = SourceGNSS
			}
		}
	}
	if !r.Synced {
		r.Stratum, r.Leap = unsynced, 3
	}
	r.Leap = max(r.Leap, k.leap)
	return r
}

// Sample is one measurement of the system clock against a server.
type Sample struct {
	Source    string
	Offset    time.Duration // to add to the system clock
	Delay     time.Duration // round trip
	Stratum   uint8         // the server's
	RefID     uint32        // what the BBS tells as its own, following it
	Leap      uint8
	RootDelay time.Duration // the server's
	RootDisp  time.Duration
	TimeSrc   TimeSource
	When      time.Time
}

// stepLimit: an offset past it is stepped, under it slewed.
const stepLimit = 128 * time.Millisecond

// Follow takes a server's measurement as the time.
func (c *Clock) Follow(s Sample) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.followed = true
	c.ref = Ref{Synced: true, Stratum: min(s.Stratum+1, unsynced), RefID: s.RefID, Leap: s.Leap,
		RootDelay: s.RootDelay + s.Delay, RootDisp: s.RootDisp + s.Delay/2 + time.Millisecond,
		Updated: s.When, Source: s.Source, TimeSrc: s.TimeSrc, Offset: s.Offset, Delay: s.Delay}
	if !c.SetClock {
		c.offset = s.Offset
		return
	}
	c.offset = 0
	var err error
	if s.Offset > stepLimit || s.Offset < -stepLimit {
		err = stepClock(s.Offset)
		if err == nil && c.Log != nil {
			c.Log("time: system clock stepped %v (%s)", s.Offset.Round(time.Microsecond), s.Source)
		}
	} else {
		err = slewClock(s.Offset, c.ref.RootDelay/2+c.ref.RootDisp)
	}
	if err != nil {
		// not allowed (no CAP_SYS_TIME): the clock corrects its readings
		c.offset = s.Offset
		if !c.setWarn && c.Log != nil {
			c.Log("time: cannot set the system clock (%v): correcting the BBS's own time instead", err)
		}
		c.setWarn = true
	}
}

// refASCII is a reference ID of up to four letters (stratum 1, kiss codes).
func refASCII(s string) uint32 {
	var b [4]byte
	copy(b[:], s)
	return binary.BigEndian.Uint32(b[:])
}

// refIP is the reference ID of a server followed (RFC 5905): its IPv4
// address, or the first four bytes of the MD5 of its IPv6 address.
func refIP(ip net.IP) uint32 {
	if v4 := ip.To4(); v4 != nil {
		return binary.BigEndian.Uint32(v4)
	}
	h := md5.Sum(ip.To16())
	return binary.BigEndian.Uint32(h[:4])
}

// RefIDString is a reference ID as people read it: letters for stratum 1
// (and kiss codes), an address otherwise.
func RefIDString(id uint32, stratum uint8) string {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], id)
	if stratum <= 1 {
		return strings.TrimRight(string(b[:]), "\x00")
	}
	return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
}
