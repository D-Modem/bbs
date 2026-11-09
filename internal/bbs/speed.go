package bbs

import (
	"strconv"
	"strings"
)

// LineSpeeds are the speeds, in bit/s, a caller without a modem may have
// the BBS send at, as modems of the time did: 300 baud to a fast serial
// port.
var LineSpeeds = []int{300, 1200, 2400, 4800, 9600, 14400, 19200, 28800, 33600, 56000, 57600, 115200}

// ValidLineSpeed is whether bps is one of LineSpeeds.
func ValidLineSpeed(bps int) bool {
	for _, s := range LineSpeeds {
		if s == bps {
			return true
		}
	}
	return false
}

// ParseLineSpeed reads a line speed as a caller or the sysop writes it:
// "2400", "14.4k", "56k", or "off" (or "full", "0"): full speed.
func ParseLineSpeed(v string) (int, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimSuffix(strings.TrimSuffix(v, "bps"), "bit/s")
	v = strings.TrimSpace(v)
	switch v {
	case "off", "full", "0", "none", "":
		return 0, true
	}
	mul := 1.0
	if strings.HasSuffix(v, "k") {
		v, mul = strings.TrimSuffix(v, "k"), 1000
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	bps := int(f*mul + 0.5)
	// 56k is 56000, 14.4k 14400, 115.2k 115200
	if ValidLineSpeed(bps) {
		return bps, true
	}
	return 0, false
}

// LineSpeedName is a speed as callers know it: "2400", "14.4k", "full".
func LineSpeedName(bps int) string {
	switch {
	case bps <= 0:
		return "full"
	case bps < 10000:
		return strconv.Itoa(bps)
	case bps%1000 == 0:
		return strconv.Itoa(bps/1000) + "k"
	}
	return strconv.FormatFloat(float64(bps)/1000, 'f', 1, 64) + "k"
}

// HasLine is whether the caller came by a line of its own speed (a modem,
// or what a modem server said of the call): such a caller's speed is the
// line's, and the BBS does not slow it again.
func (c *Caller) HasLine() bool {
	return c.Via == "modem" || c.Rate != ""
}

// LineSpeed is the speed the user chose (0: full speed), and whether they
// chose one at all.
func (p Prefs) LineSpeed() (int, bool) {
	v, ok := p["baud"]
	if !ok {
		return 0, false
	}
	bps, _ := ParseLineSpeed(v)
	return bps, true
}
