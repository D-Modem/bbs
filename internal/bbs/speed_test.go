package bbs

import "testing"

// TestLineSpeeds: speeds as callers write them, as they are shown, and
// kept as a setting.
func TestLineSpeeds(t *testing.T) {
	for in, want := range map[string]int{"2400": 2400, "14.4k": 14400, "56K": 56000, "115.2k": 115200,
		"off": 0, "full": 0, "33600 bps": 33600} {
		if got, ok := ParseLineSpeed(in); !ok || got != want {
			t.Errorf("%q: %d %v, want %d", in, got, ok, want)
		}
	}
	for _, bad := range []string{"2401", "fast", "-300"} {
		if _, ok := ParseLineSpeed(bad); ok {
			t.Errorf("%q taken", bad)
		}
	}
	for bps, want := range map[int]string{0: "full", 300: "300", 9600: "9600", 14400: "14.4k", 56000: "56k", 115200: "115.2k"} {
		if got := LineSpeedName(bps); got != want {
			t.Errorf("%d: %q, want %q", bps, got, want)
		}
	}
	if err := CheckPref("baud", "2400"); err != nil {
		t.Error(err)
	}
	if err := CheckPref("baud", "2401"); err == nil {
		t.Error("baud=2401 taken")
	}
	if bps, ok := ParsePrefs("baud=1200 ansi=on").LineSpeed(); !ok || bps != 1200 {
		t.Errorf("kept 1200: %d %v", bps, ok)
	}
	if _, ok := ParsePrefs("ansi=on").LineSpeed(); ok {
		t.Error("a speed where none was chosen")
	}
	if !(&Caller{Via: "modem"}).HasLine() || !(&Caller{Via: "ssh", Rate: "26400/31200"}).HasLine() || (&Caller{Via: "ssh"}).HasLine() {
		t.Error("HasLine")
	}
}
