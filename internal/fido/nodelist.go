package fido

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/D-Modem/bbs/internal/archive"
	"github.com/D-Modem/bbs/internal/ftn"
	"github.com/D-Modem/bbs/internal/store"
)

// maxNodelist is the most of a nodelist read (FidoNet's is some 3 MB).
const maxNodelist = 64 << 20

// Nodelist is the nodelists read (nil: none).
func (f *Fido) Nodelist() *ftn.Nodelist { return f.nl.Load() }

// newestNodelists are the newest file each nodelist pattern matches,
// with their times: what is read, and what tells whether to read again.
func (f *Fido) newestNodelists() ([]string, string) {
	var files []string
	var stamp strings.Builder
	for _, pat := range f.S.Nodelists {
		if name, ok := strings.CutPrefix(pat, "area:"); ok {
			if p, t := f.newestInArea(name); p != "" {
				files = append(files, p)
				fmt.Fprintf(&stamp, "%s@%d;", p, t)
			}
			continue
		}
		ms, _ := filepath.Glob(pat)
		best, bestTime := "", int64(0)
		for _, m := range ms {
			fi, err := os.Stat(m)
			if err != nil || !fi.Mode().IsRegular() {
				continue
			}
			if t := fi.ModTime().UnixNano(); best == "" || t > bestTime {
				best, bestTime = m, t
			}
		}
		if best != "" {
			files = append(files, best)
			fmt.Fprintf(&stamp, "%s@%d;", best, bestTime)
		}
	}
	return files, stamp.String()
}

// newestInArea is the newest file in a file area (a file echo's, as
// NODELIST comes) and when it came.
func (f *Fido) newestInArea(name string) (string, int64) {
	if f.B.Files == nil {
		return "", 0
	}
	a, err := f.B.Store.FileAreaByName(name)
	if err != nil {
		return "", 0
	}
	list, err := f.B.Store.Files(a.ID)
	if err != nil {
		return "", 0
	}
	var best *store.File
	for _, fl := range list {
		if best == nil || fl.ID > best.ID {
			best = fl
		}
	}
	if best == nil {
		return "", 0
	}
	return f.B.Files.Path(best), best.ID
}

// loadNodelists reads the nodelists again if they have changed.
func (f *Fido) loadNodelists() {
	if len(f.S.Nodelists) == 0 {
		return
	}
	files, stamp := f.newestNodelists()
	f.nlMu.Lock()
	defer f.nlMu.Unlock()
	if stamp == f.nlStamp {
		return
	}
	f.nlStamp = stamp
	nl := ftn.NewNodelist()
	for _, p := range files {
		if err := readNodelist(nl, p); err != nil {
			f.B.Log.Printf("fido: nodelist %s: %v", p, err)
			continue
		}
	}
	f.nl.Store(nl)
	f.B.Log.Printf("fido: nodelist: %d systems in zones %v, from %s", nl.Len(), nl.Zones(), strings.Join(files, ", "))
}

// readNodelist reads a nodelist file into nl: the list as it is, or in
// an archive (ZIP, ARC, ARJ, LHA), the one in it.
func readNodelist(nl *ftn.Nodelist, p string) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if len(b) > maxNodelist {
		return fmt.Errorf("bigger than %d MB", maxNodelist>>20)
	}
	if archive.Detect(b) == "" {
		return nl.Read(bytes.NewReader(b))
	}
	var data []byte
	var werr error
	err = archive.WalkBytes(b, func(e archive.Entry, open archive.Opener) bool {
		if e.Dir {
			return true
		}
		data, werr = open(maxNodelist)
		return false // the first file: an archived nodelist has one
	})
	if err != nil {
		return err
	}
	if werr != nil {
		return werr
	}
	return nl.Read(bytes.NewReader(data))
}

// checkNodelist refuses netmail for a node the nodelist lacks or has as
// down, where the nodelist has the zone; links are always reachable.
func (f *Fido) checkNodelist(a ftn.Addr) error {
	nl := f.Nodelist()
	if !nl.Covers(a.Zone) || f.linkByAddr(a) != nil || f.linkByAddr(a.Boss()) != nil {
		return nil
	}
	n, ok := nl.Lookup(a)
	if !ok {
		return fmt.Errorf("%s is not in the nodelist", a.Boss())
	}
	if n.Down() {
		return fmt.Errorf("%s (%s) is down in the nodelist", a.Boss(), n.Name)
	}
	return nil
}

// host is where to call a link: its host, or with host = nodelist what
// its nodelist entry says.
func (f *Fido) host(l *Link) (string, error) {
	if l.Host != NodelistHost {
		return l.Host, nil
	}
	n, ok := f.Nodelist().Lookup(l.Addr)
	if !ok {
		return "", fmt.Errorf("%s is not in the nodelist", l.Addr)
	}
	h, ok := n.Binkp()
	if !ok {
		return "", fmt.Errorf("the nodelist has no binkp host for %s (no IBN with a host or INA)", l.Addr)
	}
	return h, nil
}

// NodelistHost, as a link's host, has it called where the nodelist says.
const NodelistHost = "nodelist"

// NetmailTo checks a netmail's address before it is written (the
// nodelist, a route) and names its system ("": the nodelist does not
// say): the BBS's NetmailTo.
func (f *Fido) NetmailTo(a ftn.Addr) (string, error) {
	if err := f.checkNodelist(a); err != nil {
		return "", err
	}
	if f.route(a) == nil {
		return "", fmt.Errorf("no route to %s: it is not a link, and no link takes netmail on", a)
	}
	n, ok := f.Nodelist().Lookup(a)
	if !ok {
		return "", nil
	}
	desc := n.Name
	if n.Location != "" {
		desc += ", " + n.Location
	}
	return desc, nil
}
