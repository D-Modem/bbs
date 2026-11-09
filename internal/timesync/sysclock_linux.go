package timesync

import (
	"time"

	"golang.org/x/sys/unix"
)

// kernel is what the kernel says of the system clock.
type kernel struct {
	synced   bool
	maxError time.Duration
	leap     uint8
}

// kernelStatus reads it (adjtimex, changing nothing).
func kernelStatus() kernel {
	var tx unix.Timex
	state, err := unix.Adjtimex(&tx)
	if err != nil {
		return kernel{synced: true}
	}
	k := kernel{synced: state != unix.TIME_ERROR && tx.Status&unix.STA_UNSYNC == 0,
		maxError: time.Duration(tx.Maxerror) * time.Microsecond}
	switch {
	case tx.Status&unix.STA_INS != 0:
		k.leap = 1
	case tx.Status&unix.STA_DEL != 0:
		k.leap = 2
	}
	return k
}

// stepClock sets the system clock by d at once.
func stepClock(d time.Duration) error {
	ts := unix.NsecToTimespec(time.Now().Add(d).UnixNano())
	if err := unix.ClockSettime(unix.CLOCK_REALTIME, &ts); err != nil {
		return err
	}
	return markSynced(0)
}

// slewClock moves the system clock by d gradually (as adjtime does, at
// half a millisecond a second), and tells the kernel it is synchronised,
// within errBound.
func slewClock(d, errBound time.Duration) error {
	tx := unix.Timex{Modes: unix.ADJ_OFFSET_SINGLESHOT}
	setLong(&tx.Offset, d.Microseconds())
	if _, err := unix.Adjtimex(&tx); err != nil {
		return err
	}
	return markSynced(errBound)
}

// markSynced tells the kernel the clock is synchronised (for others that
// ask it: chronyc, timedatectl, the RTC's eleven-minute mode).
func markSynced(errBound time.Duration) error {
	us := max(errBound.Microseconds(), 1000)
	var cur unix.Timex
	if _, err := unix.Adjtimex(&cur); err != nil {
		return err
	}
	tx := unix.Timex{Modes: unix.ADJ_STATUS | unix.ADJ_MAXERROR | unix.ADJ_ESTERROR,
		Status: cur.Status &^ unix.STA_UNSYNC}
	setLong(&tx.Maxerror, us)
	setLong(&tx.Esterror, us)
	_, err := unix.Adjtimex(&tx)
	return err
}
