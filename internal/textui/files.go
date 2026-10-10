package textui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// fileAreas: the list of file areas
func (s *session) fileAreas() error {
	t := s.t
	for {
		s.n.Doing("File areas")
		areas, err := s.b.Store.FileAreas()
		if err != nil {
			return err
		}
		t.Println("")
		t.Println(t.C(Cyan, "File areas"))
		if len(areas) == 0 {
			t.Println("There are no file areas yet.")
		}
		var lines []string
		for i, a := range areas {
			lines = append(lines, fmt.Sprintf("%s %s %s %5d %9s", t.C(Yellow, fmt.Sprintf("%3d", i+1)),
				bbs.Fit(a.Name, 20), t.C(Dim, bbs.Fit(bbs.Trunc(a.Description, 34), 34)),
				a.Files, files.Size(a.Bytes)))
		}
		t.Page(lines)
		if s.b.FTPAddress != "" {
			t.Println(t.C(Dim, "Files come and go by FTP: "+s.b.FTPAddress+", your BBS name and password."))
		}
		cmd, err := t.Input("\nArea number, [Q]uit: ", 6)
		if err != nil {
			return err
		}
		if cmd == "" || strings.EqualFold(cmd, "Q") {
			return nil
		}
		i, err := strconv.Atoi(cmd)
		if err != nil || i < 1 || i > len(areas) {
			t.Println(t.C(Red, "No such area."))
			continue
		}
		if err := s.fileArea(areas[i-1]); err != nil {
			return err
		}
	}
}

// fileArea: an area's files
func (s *session) fileArea(a *store.FileArea) error {
	t := s.t
	for {
		s.n.Doing("Files in " + a.Name)
		list, err := s.b.Store.Files(a.ID)
		if err != nil {
			return err
		}
		t.Println("")
		t.Println(t.C(Cyan, a.Name) + t.C(Dim, "  "+a.Description))
		lines := []string{t.C(Dim, fmt.Sprintf("%4s %-28s %9s %-10s %s", "#", "Name", "Size", "Date", "Description"))}
		for i, f := range list {
			lines = append(lines, fmt.Sprintf("%s %s %9s %s %s", t.C(Yellow, fmt.Sprintf("%4d", i+1)),
				bbs.Fit(f.Name, 28), files.Size(f.Size), f.Created.Format("2006-01-02"),
				bbs.Trunc(f.Description, max(10, t.Cols-56))))
		}
		if len(list) == 0 {
			lines = append(lines, "  No files here yet.")
		}
		t.Page(lines)
		if s.b.FTPAddress != "" && files.CanUpload(s.user, a) {
			t.Println(t.C(Dim, "Upload by FTP into /"+a.Name+"/."))
		}
		cmd, err := t.Input("\nNumber for its details, [Q]uit: ", 6)
		if err != nil {
			return err
		}
		if cmd == "" || strings.EqualFold(cmd, "Q") {
			return nil
		}
		i, err := strconv.Atoi(cmd)
		if err != nil || i < 1 || i > len(list) {
			t.Println(t.C(Red, "No such file."))
			continue
		}
		if err := s.fileDetail(a, list[i-1]); err != nil {
			return err
		}
	}
}

// fileDetail: a file's details, and its uploader's (or a sysop's) changes
func (s *session) fileDetail(a *store.FileArea, f *store.File) error {
	t := s.t
	for {
		t.Println("")
		t.Println(t.C(Cyan, "Name:        ") + t.C(White, f.Name))
		t.Println(t.C(Cyan, "Area:        ") + a.Name)
		t.Printf(t.C(Cyan, "Size:        ")+"%s (%d bytes)\n", files.Size(f.Size), f.Size)
		t.Println(t.C(Cyan, "Uploaded:    ") + date(f.Created) + " by " + f.Uploader)
		t.Printf(t.C(Cyan, "Downloads:   ")+"%d\n", f.Downloads)
		t.Println(t.C(Cyan, "SHA-256:     ") + f.SHA256)
		t.Println(t.C(Cyan, "Description: ") + f.Description)
		if s.b.FTPAddress != "" {
			t.Println(t.C(Cyan, "By FTP:      ") + s.b.FTPAddress + "  /" + a.Name + "/" + f.Name)
		}
		if !files.CanDelete(s.user, f) {
			return t.Pause()
		}
		t.Print("\n[E]dit the description, [D]elete, [Q]uit: ")
		k, err := t.Key()
		if err != nil {
			return err
		}
		t.Println("")
		switch k {
		case 'E':
			desc, err := t.Input("Description: ", 60)
			if err != nil {
				return err
			}
			if err := s.b.Store.SetFileDescription(f.ID, bbs.Clean(desc, 60)); err != nil {
				return err
			}
			f.Description = bbs.Clean(desc, 60)
		case 'D':
			ok, err := t.YesNo("Delete "+f.Name+"?", false)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if err := s.b.Files.Delete(s.user, f); err != nil {
				if errors.Is(err, files.ErrDenied) {
					t.Println(t.C(Red, "Only its uploader or the sysop deletes a file."))
					continue
				}
				return err
			}
			s.b.Log.Printf("node %d: %s deleted file %q", s.n.Num, s.user.Name, f.Name)
			t.Println("Deleted.")
			return nil
		default:
			return nil
		}
	}
}
