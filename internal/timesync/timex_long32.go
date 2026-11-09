//go:build linux && (386 || arm || mips || mipsle)

package timesync

// setLong sets a struct timex long (32 bits here).
func setLong(f *int32, v int64) { *f = int32(v) }
