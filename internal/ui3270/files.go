package ui3270

import (
	"fmt"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

func (u *ui) fileAreas() error {
	top := 0
	for {
		u.n.Doing("File areas")
		areas, err := u.b.Store.FileAreas()
		if err != nil {
			return err
		}
		tb := &table{title: "File areas", top: top,
			heading: fmt.Sprintf("%-20s %-38s %5s %9s", "Area", "Description", "Files", "Size"),
			keys:    "Type S by an area, Enter  PF3=Back  PF7=Up  PF8=Down"}
		for _, a := range areas {
			tb.lines = append(tb.lines, fmt.Sprintf("%s %s %5d %9s", bbs.Fit(a.Name, 20),
				bbs.Fit(a.Description, 38), a.Files, files.Size(a.Bytes)))
		}
		msg := ""
		if u.b.FTPAddress != "" {
			msg = "Files come and go by FTP: " + u.b.FTPAddress
		}
		i, aid, err := u.pickFrom(tb, msg)
		top = tb.top
		if err != nil {
			return err
		}
		if aid == go3270.AIDPF3 {
			return nil
		}
		if i >= 0 {
			if err := u.fileArea(areas[i]); err != nil {
				return err
			}
		}
	}
}

func (u *ui) fileArea(a *store.FileArea) error {
	top := 0
	for {
		u.n.Doing("Files in " + a.Name)
		list, err := u.b.Store.Files(a.ID)
		if err != nil {
			return err
		}
		tb := &table{title: a.Name, top: top,
			heading: fmt.Sprintf("%-28s %9s %-10s %s", "Name", "Size", "Date", "Description"),
			keys:    "Type S by a file, Enter  PF3=Back  PF7=Up  PF8=Down"}
		for _, f := range list {
			tb.lines = append(tb.lines, fmt.Sprintf("%s %9s %s %s", bbs.Fit(f.Name, 28),
				files.Size(f.Size), f.Created.Format("2006-01-02"), f.Description))
		}
		msg := ""
		if u.b.FTPAddress != "" && files.CanUpload(u.user, a) {
			msg = "Upload by FTP into /" + a.Name + "/"
		}
		i, aid, err := u.pickFrom(tb, msg)
		top = tb.top
		if err != nil {
			return err
		}
		if aid == go3270.AIDPF3 {
			return nil
		}
		if i >= 0 {
			if err := u.fileDetail(a, list[i]); err != nil {
				return err
			}
		}
	}
}

func (u *ui) fileDetail(a *store.FileArea, f *store.File) error {
	lines := []string{
		"Name:         " + f.Name,
		"Area:         " + a.Name,
		fmt.Sprintf("Size:         %s (%d bytes)", files.Size(f.Size), f.Size),
		"Uploaded:     " + f.Created.Format("2006-01-02 15:04") + " by " + f.Uploader,
		fmt.Sprintf("Downloads:    %d", f.Downloads),
		"SHA-256:      " + f.SHA256[:32],
		"              " + f.SHA256[32:],
		"Description:  " + f.Description,
	}
	if u.b.FTPAddress != "" {
		lines = append(lines, "", "By FTP:       "+u.b.FTPAddress, "              /"+a.Name+"/"+f.Name)
	}
	return u.list(f.Name, "", lines)
}
