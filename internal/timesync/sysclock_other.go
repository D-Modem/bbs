//go:build !linux

package timesync

import (
	"errors"
	"time"
)

type kernel struct {
	synced   bool
	maxError time.Duration
	leap     uint8
}

// kernelStatus: the system clock is taken to be kept (no adjtimex here).
func kernelStatus() kernel { return kernel{synced: true} }

var errNoSet = errors.New("setting the clock is done on Linux only")

func stepClock(time.Duration) error      { return errNoSet }
func slewClock(_, _ time.Duration) error { return errNoSet }
