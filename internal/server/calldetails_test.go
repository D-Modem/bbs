package server

import (
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
)

// TestCallDetails: a call's log line has its rate, caller ID, the number
// as the world writes it, and where it is from (or what kind of number).
func TestCallDetails(t *testing.T) {
	b := bbs.New(bbs.Settings{PhoneCountry: "GB"}, nil, nil)
	for _, c := range []struct {
		caller bbs.Caller
		want   string
	}{
		{bbs.Caller{Rate: "33600/31200", CallerID: "02071234567"},
			": rate 33600/31200, from 02071234567 (+44 20 7123 4567), in London, United Kingdom"},
		{bbs.Caller{CallerID: "08005550123"}, ": from 08005550123 (+44 800 555 0123), in United Kingdom (toll-free)"},
		{bbs.Caller{CallerID: "+80012345678"}, ": from +80012345678 (+800 1234 5678), an international freephone number"},
		{bbs.Caller{CallerID: "anonymous"}, ": from anonymous"},
	} {
		if got := callDetails(&c.caller, b.Locate(&c.caller)); got != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.caller.CallerID, got, c.want)
		}
	}
}
