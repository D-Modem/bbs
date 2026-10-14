package geoip

import (
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// File is a database file kept up to date: read again when it changes
// (looked at once a minute at most), as the sysop's updates replace it.
type File struct {
	Path string
	// Log hears of a file that would not be read (nil: nothing is said);
	// the one read before stays in use.
	Log func(error)

	mu      sync.Mutex
	reader  atomic.Pointer[Reader]
	stamp   time.Time // the file's, when read
	checked time.Time
}

// Country is an address's country, as Reader's Country, from the file
// as it is now.  With no path, or no file there, nothing is known.
func (f *File) Country(ip net.IP) (code, name string, ok bool) {
	if f == nil || f.Path == "" || ip == nil {
		return "", "", false
	}
	f.mu.Lock()
	if time.Since(f.checked) > time.Minute {
		f.checked = time.Now()
		if fi, err := os.Stat(f.Path); err == nil && !fi.ModTime().Equal(f.stamp) {
			r, err := Open(f.Path)
			if err != nil {
				if f.Log != nil {
					f.Log(err)
				}
			} else {
				f.reader.Store(r)
				f.stamp = fi.ModTime()
			}
		}
	}
	f.mu.Unlock()
	return f.reader.Load().Country(ip)
}
