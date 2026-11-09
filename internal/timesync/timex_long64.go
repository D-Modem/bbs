//go:build linux && !(386 || arm || mips || mipsle)

package timesync

// setLong sets a struct timex long (64 bits here).
func setLong(f *int64, v int64) { *f = v }
