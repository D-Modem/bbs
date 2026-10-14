package confui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/Neo-Desktop/bbs-sec/internal/imageboard"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

func (e *Editor) maintenanceMenu() page {
	return page{title: "Maintenance", items: func() []item {
		out := []item{
			action("Disk space", e.diskSpace),
			heading("Pruning and archiving"),
			menu("Message areas: what they keep", "", func() error { return e.show(e.retentionPage()) }),
		}
		purge := item{Entry: view.Entry{ID: "purge", Kind: view.EntryBool, Label: "Take out old messages at the save",
			Help: "What areas keep no more (the newest N, for N days) taken out now, not at the hourly look."}}
		if d := e.db; d != nil {
			purge.Value, purge.Changed = yesno(d.purge), d.purge
		} else {
			purge.Value = "no"
		}
		purge.set = func(v string) error {
			d, err := e.data()
			if err != nil {
				return err
			}
			d.purge = v == "yes"
			return nil
		}
		out = append(out, purge, e.field(sec("chan"), "archive_days", "Image boards' archive (days)"))
		out = append(out, heading("Backups: the configuration, the database, the files"))
		now := action("Back up now", e.backupNow)
		now.Help = "Made at once (it changes nothing to save), as the saved [backup] says."
		if e.Tools.Backup == nil {
			now.Disabled, now.Note = true, "not here"
		}
		out = append(out, now)
		if e.Tools.Backups != nil {
			v := ""
			if as, err := e.Tools.Backups(); err == nil && len(as) > 0 {
				v = fmt.Sprintf("%d, the last %s", len(as), as[0].Time.Format("2006-01-02 15:04"))
			} else if err == nil {
				v = "none yet"
			}
			out = append(out, menu("Backups made", v, func() error { return e.show(e.backupsPage()) }))
		}
		b := sec("backup")
		out = append(out, e.fields(b, "dir", "at", "keep", "files")...)
		out = append(out, item{Entry: view.Entry{Kind: view.EntryInfo, Label: "Configuration's last",
			Value: filepath.Base(e.Tools.Path) + ".bak, at each save"}})
		return out
	}}
}

// backupNow makes a backup at once.
func (e *Editor) backupNow() error {
	a, err := e.Tools.Backup()
	if err != nil {
		e.say(view.Bad, "No backup: "+err.Error()+".")
		return nil
	}
	e.log("backup by %s: %s", e.who(), a.Path)
	e.say(view.Good, fmt.Sprintf("Backed up: %s, %s.", filepath.Base(a.Path), bytesText(a.Size)))
	return nil
}

// backupsPage is the backups there are, the newest first.
func (e *Editor) backupsPage() page {
	return page{title: "Backups made",
		note: []view.Line{view.T(view.Dim, "To put one back: bbs backup restore ARCHIVE, with the BBS stopped.")},
		items: func() []item {
			as, err := e.Tools.Backups()
			if err != nil {
				return []item{{Entry: view.Entry{Kind: view.EntryInfo, Label: "Backups", Value: err.Error(), Disabled: true}}}
			}
			var out []item
			for _, a := range as {
				out = append(out, item{Entry: view.Entry{Kind: view.EntryInfo, Label: a.Time.Format("2006-01-02 15:04"),
					Value: bytesText(a.Size) + "  " + filepath.Base(a.Path)}})
			}
			if len(out) == 0 {
				out = append(out, item{Entry: view.Entry{Kind: view.EntryInfo, Label: "None yet."}})
			}
			return out
		}}
}

// retentionPage is each message area's keeping: the newest N, N days.
func (e *Editor) retentionPage() page {
	return page{title: "Message areas: what they keep", items: func() []item {
		d, err := e.data()
		if err != nil {
			return noData(err)
		}
		var out []item
		for _, a := range d.areas {
			a := a
			w := d.areasWas[a.ID]
			it := menu(a.Name, keeps(a), func() error { return e.show(e.areaKeepPage(a)) })
			it.Changed = a.MaxMsgs != w.MaxMsgs || a.MaxDays != w.MaxDays
			it.Help = fmt.Sprintf("%d messages now", a.Messages)
			out = append(out, it)
		}
		if len(out) == 0 {
			out = append(out, item{Entry: view.Entry{Kind: view.EntryInfo, Label: "No message areas yet."}})
		}
		return out
	}}
}

func (e *Editor) areaKeepPage(a *store.Area) page {
	return page{title: "Area " + a.Name, items: func() []item {
		w := e.db.areasWas[a.ID]
		num := func(id, label string, p *int, was int, help string) item {
			it := item{Entry: view.Entry{ID: id, Kind: view.EntryText, Label: label, Value: strconv.Itoa(*p),
				Changed: *p != was, Help: help}}
			it.set = func(v string) error {
				n, err := strconv.Atoi(strings.TrimSpace(v))
				if err != nil || n < 0 {
					return fmt.Errorf("%s: a number, 0 or more", label)
				}
				*p = n
				return nil
			}
			return it
		}
		return []item{
			{Entry: view.Entry{Kind: view.EntryInfo, Label: "Messages", Value: strconv.Itoa(a.Messages)}},
			num("msgs", "Keep the newest", &a.MaxMsgs, w.MaxMsgs, "Messages kept (0: all)."),
			num("days", "Keep for days", &a.MaxDays, w.MaxDays, "Days a message is kept (0: for ever)."),
		}
	}}
}

// diskSpace shows what the BBS's files take, and what is free where they
// are.
func (e *Editor) diskSpace() error {
	g := sec("general")
	db := e.value(g, "database", "bbs.db")
	paths := []struct{ label, path string }{
		{"File areas", e.value(g, "files_dir", "files")},
		{"Image boards", e.value(sec("chan"), "dir", imageboard.Defaults().Dir)},
		{"FidoNet", e.value(sec("fido"), "dir", "fido")},
		{"Display files", e.value(g, "screens_dir", "")},
		{"Scripts", e.value(g, "scripts_dir", "")},
	}
	if !strings.Contains(db, "://") {
		paths = append([]struct{ label, path string }{{"Database", strings.TrimPrefix(db, "sqlite:")}}, paths...)
	}
	var lines []view.Line
	seen := map[string]bool{} // file systems told of
	for _, p := range paths {
		if p.path == "" {
			continue
		}
		size, files, err := du(p.path)
		if err != nil {
			lines = append(lines, view.V(p.label+":", errText(err)), view.T(view.Dim, "  "+p.path))
			continue
		}
		v := fmt.Sprintf("%s in %d files", bytesText(size), files)
		if files == 1 {
			v = bytesText(size)
		}
		lines = append(lines, view.V(p.label+":", v), view.T(view.Dim, "  "+p.path))
		var st syscall.Statfs_t
		if syscall.Statfs(p.path, &st) == nil && !seen[fmt.Sprint(st.Fsid)] {
			seen[fmt.Sprint(st.Fsid)] = true
			total, free := st.Blocks*uint64(st.Bsize), st.Bavail*uint64(st.Bsize)
			used := 0.0
			if total > 0 {
				used = 1 - float64(free)/float64(total)
			}
			lines = append(lines, view.Line{Label: "  Disk used:", Text: fmt.Sprintf("%s free of %s", bytesText(int64(free)),
				bytesText(int64(total))), Bar: used})
		}
	}
	if strings.Contains(db, "://") {
		lines = append(lines, view.V("Database:", "on a server ("+db[:strings.Index(db, "://")]+"): its size is the server's to say"))
	}
	if d, err := e.data(); err == nil {
		msgs := 0
		for _, a := range d.areas {
			msgs += a.Messages
		}
		lines = append(lines, view.L(""), view.V("Messages:", fmt.Sprintf("%d in %d areas", msgs, len(d.areas))))
		if fas, err := d.b.Store.FileAreas(); err == nil {
			n, b := 0, int64(0)
			for _, fa := range fas {
				n += fa.Files
				b += fa.Bytes
			}
			lines = append(lines, view.V("Files:", fmt.Sprintf("%d in %d areas, %s", n, len(fas), bytesText(b))))
		}
	}
	return e.UI.Show("Disk space", lines)
}

// du is the bytes and files under path.
func du(path string) (int64, int, error) {
	var size int64
	n := 0
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				size += fi.Size()
				n++
			}
		}
		return nil
	})
	return size, n, err
}

func errText(err error) string {
	if os.IsNotExist(err) {
		return "not there"
	}
	return err.Error()
}

func bytesText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
