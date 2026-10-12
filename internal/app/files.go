package app

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// fileAreas: the list of file areas
func (s *Session) fileAreas() error {
	for {
		s.N.Doing("File areas")
		all, err := s.B.Store.FileAreas()
		if err != nil {
			return err
		}
		var areas []*store.FileArea
		for _, a := range all {
			if s.B.CanDownload(s.User, a) || s.B.CanUpload(s.User, a) {
				areas = append(areas, a)
			}
		}
		t := &view.Table{Title: "File areas", Empty: "There are no file areas yet.", Pick: "Area number",
			Columns: []view.Column{{Title: "Area", Width: 20}, {Title: "Description", Width: 34, Wide: true},
				{Title: "Files", Width: 5, Right: true}, {Title: "Size", Width: 9, Right: true}}}
		if s.B.FTPAddress != "" {
			t.Note = []view.Line{view.T(view.Dim, "Files come and go by FTP: "+s.B.FTPAddress+", your BBS name and password.")}
		}
		for i, a := range areas {
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1),
				Cells: []string{a.Name, a.Description, strconv.Itoa(a.Files), files.Size(a.Bytes)}})
		}
		c, err := s.UI.Table(t)
		if err != nil || c.Action == view.Back {
			return err
		}
		if c.Action != view.Pick {
			continue
		}
		if c.Row < 0 {
			s.UI.Say(view.Bad, "No such area.")
			continue
		}
		if err := s.fileArea(areas[c.Row]); err != nil {
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
				t.Actions = []view.Action{{ID: "upload", Label: "Upload", Key: 'U'}}
			}
		} else {
			t.Command = "File transfer (IND$FILE): GET name downloads, PUT name uploads here."
		}
		if s.B.FTPAddress != "" && up {
			t.Note = append(t.Note, view.T(view.Dim, "Upload by FTP into /"+a.Name+"/."))
		}
		for i, f := range list {
			t.Rows = append(t.Rows, view.Row{Num: strconv.Itoa(i + 1),
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
			if err := s.UI.Upload(a); err != nil {
				return err
			}
		case view.Cmd:
			msg, err := s.UI.FileCommand(a, c.Text)
			if err != nil {
				return err
			}
			if msg != "" {
				s.UI.Say(view.Plain, msg)
			}
		case view.Pick:
			if c.Row < 0 {
				s.UI.Say(view.Bad, "No such file.")
				continue
			}
			if err := s.fileDetail(a, list[c.Row]); err != nil {
				return err
			}
		}
	}
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
		if s.B.FTPAddress != "" {
			p.Lines = append(p.Lines, view.V("By FTP:", s.B.FTPAddress+"  /"+a.Name+"/"+f.Name))
		}
		if s.UI.Transfers() {
			p.Actions = append(p.Actions, view.Action{ID: "download", Label: "Download", Key: 'D'})
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
			if err := s.UI.Download(a, f); err != nil {
				return err
			}
			if fresh, err := s.B.Store.FileByID(f.ID); err == nil {
				f = fresh
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
			s.B.Log.Printf("node %d: %s deleted file %q", s.N.Num, s.User.Name, f.Name)
			s.UI.Say(view.Good, "Deleted.")
			return nil
		}
	}
}
