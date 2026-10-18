package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// fileAreasFor are the file areas the caller downloads from or uploads to.
func (s *Session) fileAreasFor() ([]*store.FileArea, error) {
	all, err := s.B.Store.FileAreas()
	if err != nil {
		return nil, err
	}
	var areas []*store.FileArea
	for _, a := range all {
		if s.B.CanDownload(s.User, a) || s.B.CanUpload(s.User, a) {
			areas = append(areas, a)
		}
	}
	return areas, nil
}

// batchAction is the action that downloads the tagged files, if any are.
func (s *Session) batchAction() []view.Action {
	if len(s.batch) == 0 || !s.UI.Transfers() {
		return nil
	}
	return []view.Action{{ID: "batch", Label: fmt.Sprintf("Batch (%d tagged)", len(s.batch)), Key: 'B'}}
}

// tagged: the file is in the batch
func (s *Session) tagged(f *store.File) bool {
	for _, r := range s.batch {
		if r.File.ID == f.ID {
			return true
		}
	}
	return false
}

// tag puts a file in the batch, or takes it out.
func (s *Session) tag(a *store.FileArea, f *store.File) {
	for i, r := range s.batch {
		if r.File.ID == f.ID {
			s.batch = append(s.batch[:i], s.batch[i+1:]...)
			s.UI.Say(view.Plain, "Untagged "+f.Name+".")
			return
		}
	}
	if !s.B.CanDownload(s.User, a) {
		s.UI.Say(view.Bad, "Your access does not allow downloading here.")
		return
	}
	s.batch = append(s.batch, FileRef{Area: a, File: f})
	s.UI.Say(view.Good, fmt.Sprintf("Tagged %s (%d in the batch).", f.Name, len(s.batch)))
}

// fileAreas: the list of file areas
func (s *Session) fileAreas() error {
	for {
		s.N.Doing("File areas")
		areas, err := s.fileAreasFor()
		if err != nil {
			return err
		}
		t := &view.Table{Title: "File areas", Empty: "There are no file areas yet.", Pick: "Area number",
			Columns: []view.Column{{Title: "Area", Width: 20}, {Title: "Description", Width: 34, Wide: true},
				{Title: "Files", Width: 5, Right: true}, {Title: "Size", Width: 9, Right: true}},
			Actions: append([]view.Action{{ID: "new", Label: "New files", Key: 'N', PF: 5, Short: "New"},
				{ID: "find", Label: "Find", Key: 'F', PF: 10}}, s.batchAction()...)}
		if s.B.FTPAddress != "" {
			t.Note = []view.Line{view.T(view.Dim, "Files come and go by FTP: "+s.B.FTPAddress+", your BBS name and password.")}
		}
		for i, a := range areas {
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1),
				Cells: []string{a.Name, a.Description, strconv.Itoa(a.Files), files.Size(a.Bytes)}})
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case "new":
			err = s.newFiles(areas)
		case "find":
			err = s.findFiles(areas)
		case "batch":
			err = s.batchScreen()
		case view.Pick:
			if c.Row < 0 {
				s.UI.Say(view.Bad, "No such area.")
				continue
			}
			err = s.fileArea(areas[c.Row])
		}
		if err != nil {
			return err
		}
	}
}

// fileArea: an area's files
func (s *Session) fileArea(a *store.FileArea) error {
	for {
		s.N.Doing("Files in " + a.Name)
		list, err := s.B.Store.Files(a.ID)
		if err != nil {
			return err
		}
		t := &view.Table{Title: a.Name, Empty: "No files here yet.", Pick: "Number for its details (and download)",
			Note: []view.Line{view.T(view.Dim, a.Description)},
			Columns: []view.Column{{Title: "Name", Width: 28}, {Title: "Size", Width: 9, Right: true},
				{Title: "Date", Width: 10, Wide: true}, {Title: "Description", Wide: true}}}
		up := s.B.CanUpload(s.User, a)
		if s.UI.Transfers() {
			if up {
				t.Actions = append(t.Actions, view.Action{ID: "upload", Label: "Upload", Key: 'U'})
			}
			if len(list) > 0 && s.B.CanDownload(s.User, a) {
				t.Actions = append(t.Actions, view.Action{ID: "tag", Label: "Tag", Key: 'T'})
			}
			t.Actions = append(t.Actions, s.batchAction()...)
		} else {
			t.Command = "File transfer (IND$FILE): GET name downloads, PUT name uploads here."
		}
		if s.B.FTPAddress != "" && up {
			t.Note = append(t.Note, view.T(view.Dim, "Upload by FTP into /"+a.Name+"/."))
		}
		for i, f := range list {
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Mark: s.tagged(f),
				Cells: []string{f.Name, files.Size(f.Size), f.Created.Format("2006-01-02"), f.Description}})
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case "upload":
			err = s.UI.Upload(a)
		case "tag":
			err = s.tagNumbers(a, list)
		case "batch":
			err = s.batchScreen()
		case view.Cmd:
			var msg string
			if msg, err = s.UI.FileCommand(a, c.Text); msg != "" {
				s.UI.Say(view.Plain, msg)
			}
		case view.Pick:
			if c.Row < 0 {
				s.UI.Say(view.Bad, "No such file.")
				continue
			}
			err = s.fileDetail(a, list[c.Row])
		}
		if err != nil {
			return err
		}
	}
}

// tagNumbers tags (or untags) files by their numbers: "1 3 5-7".
func (s *Session) tagNumbers(a *store.FileArea, list []*store.File) error {
	v, ok, err := s.UI.Form(&view.Form{Title: "Tag files", Submit: "Tag", Fields: []view.Field{
		{Name: "n", Label: "Numbers", Prompt: "Tag which (numbers, as 1 3 5-7)? ", Width: 40}}})
	if err != nil || !ok {
		return err
	}
	for _, f := range strings.FieldsFunc(v["n"], func(r rune) bool { return r == ' ' || r == ',' }) {
		lo, hi, rng := strings.Cut(f, "-")
		i, e1 := strconv.Atoi(lo)
		j, e2 := i, error(nil)
		if rng {
			j, e2 = strconv.Atoi(hi)
		}
		if e1 != nil || e2 != nil || i < 1 || j > len(list) || i > j {
			s.UI.Say(view.Bad, "No such file: "+f+".")
			continue
		}
		for k := i; k <= j; k++ {
			s.tag(a, list[k-1])
		}
	}
	return nil
}

// batchScreen: the files tagged, to download together
func (s *Session) batchScreen() error {
	for {
		if len(s.batch) == 0 {
			return nil
		}
		var size int64
		t := &view.Table{Title: "Your batch", Pick: "Number to untag",
			Columns: []view.Column{{Title: "Area", Width: 16}, {Title: "Name", Width: 28},
				{Title: "Size", Width: 9, Right: true}},
			Actions: []view.Action{{ID: "download", Label: "Download all", Key: 'D'},
				{ID: "clear", Label: "Clear", Key: 'C'}}}
		for i, r := range s.batch {
			size += r.File.Size
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1),
				Cells: []string{r.Area.Name, r.File.Name, files.Size(r.File.Size)}})
		}
		t.Note = []view.Line{view.T(view.Dim, fmt.Sprintf("%d files, %s", len(s.batch), files.Size(size)))}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case "clear":
			s.batch = nil
		case "download":
			left, why := s.B.DownloadsLeft(s.refresh())
			if left >= 0 && len(s.batch) > left {
				s.UI.Say(view.Bad, fmt.Sprintf("You may download %d more just now: %s.", left, why))
				continue
			}
			if err := s.UI.Download(s.batch); err != nil {
				return err
			}
			// what was sent is out of the batch
			var still []FileRef
			for _, r := range s.batch {
				if f, err := s.B.Store.FileByID(r.File.ID); err == nil && f.Downloads == r.File.Downloads {
					still = append(still, r)
				}
			}
			s.batch = still
		case view.Pick:
			if c.Row >= 0 {
				s.batch = append(s.batch[:c.Row], s.batch[c.Row+1:]...)
			}
		}
	}
}

// fileList lists files found across areas.
func (s *Session) fileList(title, empty string, list []*store.File, areas []*store.FileArea) error {
	byID := map[int64]*store.FileArea{}
	for _, a := range areas {
		byID[a.ID] = a
	}
	for {
		t := &view.Table{Title: title, Empty: empty, Pick: "Number for its details",
			Columns: []view.Column{{Title: "Area", Width: 14}, {Title: "Name", Width: 24},
				{Title: "Size", Width: 9, Right: true}, {Title: "Date", Width: 10, Wide: true},
				{Title: "Description", Wide: true}},
			Actions: s.batchAction()}
		for i, f := range list {
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1), Mark: s.tagged(f),
				Cells: []string{byID[f.AreaID].Name, f.Name, files.Size(f.Size), f.Created.Format("2006-01-02"), f.Description}})
		}
		c, err := s.UI.Table(t)
		if err != nil {
			return err
		}
		switch c.Action {
		case view.Back:
			return nil
		case "batch":
			err = s.batchScreen()
		case view.Pick:
			if c.Row < 0 {
				s.UI.Say(view.Bad, "No such file.")
				continue
			}
			err = s.fileDetail(byID[list[c.Row].AreaID], list[c.Row])
		}
		if err != nil {
			return err
		}
	}
}

// ids is the areas' IDs, in order.
func ids(areas []*store.FileArea) []int64 {
	out := make([]int64, len(areas))
	for i, a := range areas {
		out[i] = a.ID
	}
	return out
}

// newFiles: files added since the caller's last call
func (s *Session) newFiles(areas []*store.FileArea) error {
	s.N.Doing("New files")
	list, err := s.B.Store.FilesSince(ids(areas), s.since, most)
	if err != nil {
		return err
	}
	when := s.since.Format("2006-01-02 15:04")
	return s.fileList("New files since "+when, "No files since "+when+".", list, areas)
}

// findFiles: files by their names and descriptions
func (s *Session) findFiles(areas []*store.FileArea) error {
	v, ok, err := s.UI.Form(&view.Form{Title: "Find files", Submit: "Find", Fields: []view.Field{
		{Name: "text", Label: "Find", Prompt: "Find (in names and descriptions): ", Width: 40}}})
	text := strings.TrimSpace(v["text"])
	if err != nil || !ok || text == "" {
		return err
	}
	if len([]rune(text)) < 2 {
		s.UI.Say(view.Bad, "Give two characters or more.")
		return nil
	}
	s.N.Doing("Finding files")
	list, err := s.B.Store.SearchFiles(ids(areas), text, most)
	if err != nil {
		return err
	}
	return s.fileList("Files: "+text, "No file has "+text+" in it.", list, areas)
}

// archive: what is in a file that is an archive
func (s *Session) archive(f *store.File) error {
	es, err := s.B.Files.Listing(f)
	if errors.Is(err, files.ErrNotArchive) {
		s.UI.Say(view.Bad, capital(err.Error())+".")
		return nil
	}
	if err != nil {
		return err
	}
	t := &view.Table{Title: "In " + f.Name, Empty: "The archive is empty.",
		Columns: []view.Column{{Title: "Name", Width: 44}, {Title: "Size", Width: 9, Right: true},
			{Title: "Date", Width: 16, Wide: true}}}
	var total int64
	for _, e := range es {
		size := files.Size(e.Size)
		if e.Dir {
			size = "dir"
		}
		total += e.Size
		when := ""
		if !e.Modified.IsZero() {
			when = e.Modified.Format("2006-01-02 15:04")
		}
		t.Rows = append(t.Rows, view.Row{Cells: []string{e.Name, size, when}})
	}
	t.Note = []view.Line{view.T(view.Dim, fmt.Sprintf("%d entries, %s unpacked", len(es), files.Size(total)))}
	_, err = s.UI.Table(t)
	return err
}

// capital is s with its first byte in upper case: for the errors said to the
// caller as sentences.
func capital(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:] // the first letter, however many bytes
}

// fileDetail: a file's details, and its uploader's (or a sysop's) changes
func (s *Session) fileDetail(a *store.FileArea, f *store.File) error {
	for {
		p := &view.Panel{Title: f.Name, Lines: []view.Line{
			view.V("Name:", f.Name),
			view.V("Area:", a.Name),
			view.V("Size:", fmt.Sprintf("%s (%d bytes)", files.Size(f.Size), f.Size)),
			view.V("Uploaded:", f.Created.Format("2006-01-02 15:04")+" by "+f.Uploader),
			view.V("Downloads:", strconv.Itoa(f.Downloads)),
			view.V("SHA-256:", f.SHA256),
			view.V("Description:", f.Description)}}
		if f.LongDesc != "" {
			p.Lines = append(p.Lines, view.L(""))
			for _, l := range strings.Split(f.LongDesc, "\n") {
				p.Lines = append(p.Lines, view.L(l))
			}
			p.Lines = append(p.Lines, view.L(""))
		}
		if s.B.FTPAddress != "" {
			p.Lines = append(p.Lines, view.V("By FTP:", s.B.FTPAddress+"  /"+a.Name+"/"+f.Name))
		}
		if s.UI.Transfers() {
			tag := "Tag"
			if s.tagged(f) {
				tag = "Untag"
			}
			p.Actions = append(p.Actions, view.Action{ID: "download", Label: "Download", Key: 'D'},
				view.Action{ID: "tag", Label: tag, Key: 'T'})
		}
		if files.IsArchive(f.Name) {
			p.Actions = append(p.Actions, view.Action{ID: "view", Label: "View the archive", Key: 'V', PF: 6, Short: "View"})
		}
		mine := files.CanDelete(s.User, f)
		if mine {
			p.Actions = append(p.Actions,
				view.Action{ID: "edit", Label: "Edit the description", Key: 'E', PF: 5, Short: "Describe"},
				view.Action{ID: "remove", Label: "Remove", Key: 'R', PF: 9})
		}
		act, err := s.UI.Panel(p)
		if err != nil {
			return err
		}
		if (act == "edit" || act == "remove") && !mine {
			continue
		}
		switch act {
		case view.Back:
			return nil
		case "download":
			if err := s.B.MayDownload(s.refresh(), a); err != nil {
				s.UI.Say(view.Bad, capital(err.Error())+".")
				continue
			}
			if err := s.UI.Download([]FileRef{{Area: a, File: f}}); err != nil {
				return err
			}
			if fresh, err := s.B.Store.FileByID(f.ID); err == nil {
				f = fresh
			}
		case "tag":
			s.tag(a, f)
		case "view":
			if err := s.archive(f); err != nil {
				return err
			}
		case "edit":
			v, ok, err := s.UI.Form(&view.Form{Title: f.Name, Submit: "Save", Fields: []view.Field{
				{Name: "desc", Label: "Description", Width: 60, Value: f.Description}}})
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			desc := bbs.Clean(v["desc"], 60)
			if err := s.B.Store.SetFileDescription(f.ID, desc); err != nil {
				return err
			}
			f.Description = desc
		case "remove":
			ok, err := s.UI.Confirm("Delete "+f.Name+"?", false)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if err := s.B.Files.Delete(s.User, f); err != nil {
				if errors.Is(err, files.ErrDenied) {
					s.UI.Say(view.Bad, "Only its uploader or the sysop deletes a file.")
					continue
				}
				return err
			}
			for i, r := range s.batch {
				if r.File.ID == f.ID {
					s.batch = append(s.batch[:i], s.batch[i+1:]...)
					break
				}
			}
			s.B.Log.Printf("node %d: %s deleted file %q", s.N.Num, s.User.Name, f.Name)
			s.UI.Say(view.Good, "Deleted.")
			return nil
		}
	}
}
