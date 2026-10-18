package fido

import (
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/ftn"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// ticWait is how long a TIC waits for its file before it goes to bad.
const ticWait = 24 * time.Hour

// ticResult is what became of a TIC.
type ticResult int

const (
	ticDone  ticResult = iota // taken (or a dupe, thrown away)
	ticWaits                  // its file has not come yet
	ticBad                    // to bad, with its file
	ticAside                  // set aside for the sysop (in/LINK/files)
)

// tossTICs takes in the file echoes' files a link sent: each with its
// TIC, checked, put in its file area, passed on to the links that carry
// it.  Files with no TIC that are a day old are set aside.
func (f *Fido) tossTICs(l *Link, dir string, st *TossStats) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := map[string]string{} // lower case: as it is
	for _, e := range ents {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			names[strings.ToLower(e.Name())] = e.Name()
		}
	}
	claimed := map[string]bool{}
	for low, name := range names {
		if !strings.HasSuffix(low, ".tic") {
			continue
		}
		p := filepath.Join(dir, name)
		res, file, why := f.tossTIC(l, p, names)
		if file != "" {
			claimed[strings.ToLower(file)] = true
		}
		switch res {
		case ticDone:
			os.Remove(p)
			if file != "" {
				os.Remove(filepath.Join(dir, file))
			}
			if why == "dupe" {
				st.Dupes++
			} else {
				st.Files++
			}
		case ticWaits:
			if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > ticWait {
				f.B.Log.Printf("fido: %s: %s waited a day for its file: moved to bad", l.Name, name)
				os.Rename(p, filepath.Join(f.S.Dir, "bad", l.Name+"-"+name))
				st.Bad++
			}
		case ticBad, ticAside:
			to := filepath.Join(f.S.Dir, "bad")
			prefix := l.Name + "-"
			if res == ticAside {
				to, prefix = filepath.Join(dir, "files"), ""
				os.MkdirAll(to, 0700)
			} else {
				st.Bad++
			}
			f.B.Log.Printf("fido: %s: %s: %s (moved to %s)", l.Name, name, why, to)
			os.Rename(p, filepath.Join(to, prefix+name))
			if file != "" {
				os.Rename(filepath.Join(dir, file), filepath.Join(to, prefix+file))
			}
		}
	}
	// files with no TIC, a day old: not ours to know
	for low, name := range names {
		if strings.HasSuffix(low, ".tic") || claimed[low] || bundleName.MatchString(name) || strings.HasSuffix(low, ".pkt") {
			continue
		}
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > ticWait {
			files := filepath.Join(dir, "files")
			os.MkdirAll(files, 0700)
			os.Rename(p, filepath.Join(files, name))
			f.B.Log.Printf("fido: %s sent %s, with no TIC: left in %s", l.Name, name, files)
		}
	}
}

// tossTIC takes in one TIC: what became of it, its file's name in the
// inbound (if it is there), and why, when it was not taken.
func (f *Fido) tossTIC(l *Link, p string, names map[string]string) (ticResult, string, string) {
	data, err := os.ReadFile(p)
	if err != nil {
		return ticBad, "", err.Error()
	}
	t, err := ftn.ParseTIC(data)
	if err != nil {
		return ticBad, "", err.Error()
	}
	file := names[strings.ToLower(t.File)]
	if file == "" {
		file = names[strings.ToLower(t.LFile)]
	}
	if file == "" || strings.ContainsAny(file, "/\\") {
		return ticWaits, "", ""
	}
	if f.B.Files == nil || l.FileEchoes == "" {
		return ticAside, file, "a file echo's file, and file echoes are not exchanged with " + l.Name
	}
	if t.From.Zone == 0 {
		t.From.Zone = l.Addr.Zone
	}
	if !t.From.Same(l.Addr) {
		return ticBad, file, fmt.Sprintf("the TIC is from %s, not %s", t.From, l.Addr)
	}
	if l.TicPassword != "" && !strings.EqualFold(t.Pw, l.TicPassword) {
		return ticBad, file, "wrong TIC password"
	}
	fp := filepath.Join(filepath.Dir(p), file)
	size, crc, err := sizeCRC(fp)
	if err != nil {
		return ticBad, file, err.Error()
	}
	if t.Size != 0 && size != t.Size || t.HasCRC && crc != t.CRC {
		return ticBad, file, fmt.Sprintf("%s does not match its TIC (size %d, CRC %08X)", file, size, crc)
	}
	// been here: our address in its path
	for _, pl := range t.Path {
		if fs := strings.Fields(pl); len(fs) > 0 {
			if a, err := ftn.ParseAddr(fs[0]); err == nil && f.ours(a) {
				return ticDone, file, "dupe"
			}
		}
	}
	key := fmt.Sprintf("tic %s %s %08X", t.Area, strings.ToLower(t.Name()), crc)
	if seen, _ := f.B.Store.Seen(key); seen {
		return ticDone, file, "dupe"
	}
	area, err := f.B.Store.FileAreaByEcho(t.Area)
	if errors.Is(err, store.ErrNotFound) && l.Create && l.allows(FileEchos, t.Area) {
		area, err = f.createFileEcho(l, t.Area, t.AreaDesc)
	}
	if err != nil {
		return ticBad, file, fmt.Sprintf("the file echo %s: %v", t.Area, err)
	}
	if !f.carries(l, FileEchos, t.Area) {
		return ticBad, file, "the file echo " + t.Area + " is not exchanged with " + l.Name
	}
	name := t.Name()
	if files.ValidName(name) != "" {
		name = t.File
	}
	if msg := files.ValidName(name); msg != "" {
		return ticBad, file, msg
	}
	var long []string
	for _, ld := range t.LDesc {
		long = append(long, bbs.Clean(ld, 200))
	}
	origin := t.Origin
	if origin.Zone == 0 {
		origin = l.Addr
	}
	fl, err := f.B.Files.Put(area, fp, name, bbs.Clean(t.Desc, 200), strings.Join(long, "\n"), origin.String(), l.origin())
	if err != nil {
		return ticBad, file, err.Error()
	}
	if t.Replaces != "" {
		f.replace(area, fl, t.Replaces)
	}
	f.B.Store.Remember(key)
	f.B.Log.Printf("fido: %s sent %s in the file echo %s", l.Name, name, t.Area)
	f.passOn(l, t, f.B.Files.Path(fl))
	return ticDone, file, ""
}

// sizeCRC is a file's size and CRC-32.
func sizeCRC(p string) (int64, uint32, error) {
	fh, err := os.Open(p)
	if err != nil {
		return 0, 0, err
	}
	defer fh.Close()
	h := crc32.NewIEEE()
	n, err := io.Copy(h, fh)
	return n, h.Sum32(), err
}

// replace takes away the files of an area a new one replaces (its TIC's
// Replaces, a pattern too): only those that came by a file echo.
func (f *Fido) replace(a *store.FileArea, keep *store.File, pattern string) {
	list, err := f.B.Store.Files(a.ID)
	if err != nil {
		return
	}
	for _, old := range list {
		if old.ID == keep.ID || !strings.HasPrefix(old.Origin, "ftn:") {
			continue
		}
		if ok, _ := path.Match(strings.ToLower(pattern), strings.ToLower(old.Name)); ok {
			if err := f.B.Files.Remove(old); err == nil {
				f.B.Log.Printf("fido: %s replaces %s in %s", keep.Name, old.Name, a.Name)
			}
		}
	}
}

// createFileEcho makes a file area for a file echo a link sent that is
// not here (create = yes), named by its tag.
func (f *Fido) createFileEcho(l *Link, tag, desc string) (*store.FileArea, error) {
	tag = strings.ToUpper(tag)
	if !validTag.MatchString(tag) {
		return nil, store.ErrNotFound
	}
	if desc = bbs.Clean(desc, 60); desc == "" {
		desc = "FidoNet file echo " + tag
	}
	a, err := f.B.Store.CreateFileArea(tag, desc, true)
	if errors.Is(err, store.ErrExists) {
		return nil, fmt.Errorf("a file area is called %s but is not its file echo", tag)
	}
	if err != nil {
		return nil, err
	}
	if err := f.B.Store.SetFileEcho(a.ID, tag); err != nil {
		return nil, err
	}
	if l.AreaFix {
		f.B.Store.SetFidoSub(l.Name, FileEchos, tag, true)
	}
	f.B.Log.Printf("fido: made the file area %s for the file echo %s, which %s sent", tag, tag, l.Name)
	return f.B.Store.FileAreaByEcho(tag)
}

// passOn sends a file that came by a file echo on to the other links
// that carry it and have not seen it, each with a TIC of its own.
func (f *Fido) passOn(from *Link, t *ftn.TIC, file string) {
	var to []*Link
	for _, o := range f.S.Links {
		if o != from && f.carries(o, FileEchos, t.Area) && !t.Seen(o.Addr) && !f.ours(o.Addr) {
			to = append(to, o)
		}
	}
	if len(to) == 0 {
		return
	}
	seen := append([]ftn.Addr{}, t.SeenBy...)
	for _, o := range to {
		seen = append(seen, o.Addr)
	}
	for _, o := range to {
		nt := *t
		nt.SeenBy = append(append([]ftn.Addr{}, seen...), f.akaFor(o))
		if err := f.sendTIC(o, &nt, file); err != nil {
			f.B.Log.Printf("fido: passing %s on to %s: %v", t.File, o.Name, err)
		}
	}
}

// sendTIC puts a file and its TIC (from us, to the link, our way on its
// path) in a link's outbound.
func (f *Fido) sendTIC(l *Link, t *ftn.TIC, file string) error {
	me := f.akaFor(l)
	now := time.Now().UTC()
	t.From = me
	t.To = l.Name + ", " + l.Addr.String()
	t.Pw = l.TicPassword
	t.Path = append(append([]string{}, t.Path...), fmt.Sprintf("%s %d %s", me, now.Unix(), now.Format("Mon Jan 02 15:04:05 2006 UTC")))
	if t.Created == "" {
		t.Created = "by " + strings.TrimPrefix(Tearline, "--- ")
	}
	out := f.outbound(l)
	name := t.File
	if name == "" || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		return errors.New("no file name to send it by")
	}
	tmp := filepath.Join(f.S.Dir, "tmp", fmt.Sprintf("tic-%d", time.Now().UnixNano()))
	if err := copyFile(file, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(out, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	base := uint32(time.Now().UnixNano() / int64(time.Millisecond))
	for i := uint32(0); ; i++ {
		tn := fmt.Sprintf("%08x.tic", base+i)
		if exists(filepath.Join(out, tn)) {
			continue
		}
		tmp := filepath.Join(f.S.Dir, "tmp", tn)
		if err := os.WriteFile(tmp, t.Bytes(), 0600); err != nil {
			return err
		}
		return os.Rename(tmp, filepath.Join(out, tn))
	}
}

// copyFile copies a file to a new one, never over one there; a copy that fails
// is removed.
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(to)
		return err
	}
	return out.Close()
}

// hatch sends a link the files added here (not by a file echo) since its
// last scan, in the file echoes it carries.  The first scan only marks
// where to start.
func (f *Fido) hatch(l *Link) (int, error) {
	if f.B.Files == nil || l.FileEchoes == "" {
		return 0, nil
	}
	st := f.B.Store
	key := "tic:" + l.Name
	after, ok, err := st.PeerPushed(key)
	if err != nil {
		return 0, err
	}
	if !ok {
		max, err := st.MaxFileID()
		if err != nil {
			return 0, err
		}
		return 0, st.SetPeerPushed(key, max)
	}
	areas, err := st.FileEchoAreas()
	if err != nil {
		return 0, err
	}
	byID := map[int64]*store.FileArea{}
	var ids []int64
	for _, a := range areas {
		if f.carries(l, FileEchos, a.FileEcho) {
			ids = append(ids, a.ID)
			byID[a.ID] = a
		}
	}
	list, err := st.FilesToHatch(after, ids, 200)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, fl := range list {
		after = fl.ID
		a := byID[fl.AreaID]
		p := f.B.Files.Path(fl)
		size, crc, err := sizeCRC(p)
		if err != nil {
			f.B.Log.Printf("fido: hatching %s: %v", fl.Name, err)
			continue
		}
		me := f.akaFor(l)
		t := &ftn.TIC{Area: strings.ToUpper(a.FileEcho), AreaDesc: a.Description, Origin: me,
			File: fl.Name, Desc: fl.Description, Size: size, CRC: crc, HasCRC: true}
		if fl.LongDesc != "" {
			t.LDesc = strings.Split(fl.LongDesc, "\n")
		}
		// seen by us and every link that carries it
		t.SeenBy = []ftn.Addr{me}
		for _, o := range f.S.Links {
			if f.carries(o, FileEchos, a.FileEcho) {
				t.SeenBy = append(t.SeenBy, o.Addr)
			}
		}
		if err := f.sendTIC(l, t, p); err != nil {
			f.B.Log.Printf("fido: hatching %s for %s: %v", fl.Name, l.Name, err)
			continue
		}
		n++
	}
	if len(list) > 0 {
		if err := st.SetPeerPushed(key, after); err != nil {
			return n, err
		}
	}
	if n > 0 {
		f.B.Log.Printf("fido: hatched %d files for %s", n, l.Name)
	}
	return n, nil
}

// isTIC reports whether a file's name is a TIC's (.tic, any case).
func isTIC(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".tic") }

// ticNames are the files (lower case) the TICs in an inbound name: not
// packets or bundles, whatever they are called.
func ticNames(dir string, ents []os.DirEntry) map[string]bool {
	out := map[string]bool{}
	for _, e := range ents {
		if !e.Type().IsRegular() || !isTIC(e.Name()) {
			continue
		}
		out[strings.ToLower(e.Name())] = true
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || len(data) > ftn.MaxTIC {
			continue
		}
		if t, err := ftn.ParseTIC(data); err == nil {
			out[strings.ToLower(t.File)] = true
			if t.LFile != "" {
				out[strings.ToLower(t.LFile)] = true
			}
		}
	}
	return out
}
