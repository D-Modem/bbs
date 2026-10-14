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
// The first reading is waited for; later ones are made in the background
// (a city list takes seconds), the one read before answering meanwhile.
type File struct {
	Path string
	// Log hears of a file that would not be read (nil: nothing is said);
	// the one read before stays in use.
	Log func(error)

	mu      sync.Mutex
	reader  atomic.Pointer[Reader]
	stamp   time.Time // the file's, when read (or being read)
	checked time.Time
	loading bool
}

// Country is an address's country, as Reader's Country, from the file
// as it is now.  With no path, or no file there, nothing is known.
func (f *File) Country(ip net.IP) (code, name string, ok bool) {
	l, ok := f.Lookup(ip)
	return l.Code, l.Name, ok
}

// Lookup is where an address is, as Reader's Lookup, from the file as it
// is now.
func (f *File) Lookup(ip net.IP) (Location, bool) {
	if f == nil || f.Path == "" || ip == nil {
		return Location{}, false
	}
	f.Refresh()
	return f.reader.Load().Lookup(ip)
}

// Refresh reads the file if it has changed since it was last read (not
// within a minute of looking): the first time now, then in the
// background.
func (f *File) Refresh() {
	if f == nil || f.Path == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loading || time.Since(f.checked) < time.Minute {
		return
	}
	f.checked = time.Now()
	fi, err := os.Stat(f.Path)
	if err != nil || fi.ModTime().Equal(f.stamp) {
		return
	}
	f.stamp = fi.ModTime()
	load := func() {
		r, err := Open(f.Path)
		if err != nil {
			if f.Log != nil {
				f.Log(err)
			}
			return
		}
		f.reader.Store(r)
	}
	if f.reader.Load() == nil {
		load()
		return
	}
	f.loading = true
	go func() {
		load()
		f.mu.Lock()
		f.loading = false
		f.mu.Unlock()
	}()
}
