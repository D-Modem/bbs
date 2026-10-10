package ui3270

import (
	"fmt"
	"strings"

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

// fileArea is an area's files, and the command line an emulator's file
// transfer types IND$FILE into.
func (u *ui) fileArea(a *store.FileArea) error {
	top := 0
	msg := ""
	if u.b.FTPAddress != "" && files.CanUpload(u.user, a) {
		msg = "Upload by FTP into /" + a.Name + "/, or with your emulator's file transfer"
	}
	for {
		u.n.Doing("Files in " + a.Name)
		list, err := u.b.Store.Files(a.ID)
		if err != nil {
			return err
		}
		per := u.rows - 8
		scr := u.frame(a.Name, "S+Enter=Details  PF3=Back  PF7=Up  PF8=Down", msg)
		msg = ""
		// the command line first: the cursor goes to it, where an
		// emulator types IND$FILE
		scr = append(scr, u.text(u.rows-3, 0, "Command ===>", go3270.Turquoise, false))
		scr = append(scr, u.input(u.rows-3, 13, u.cols-15, "cmd", false)...)
		scr = append(scr, u.text(u.rows-4, 0,
			"File transfer (IND$FILE): GET name downloads, PUT name uploads here.", go3270.Blue, false))
		scr = append(scr, u.text(2, 0, fmt.Sprintf("   %-28s %9s %-10s %s", "Name", "Size", "Date", "Description"),
			go3270.Turquoise, false))
		for i := 0; i < per && top+i < len(list); i++ {
			f := list[top+i]
			scr = append(scr, go3270.Field{Row: 3 + i, Col: 0, Write: true, Name: fmt.Sprintf("s%d", i),
				Color: go3270.Green, Highlighting: go3270.Underscore})
			scr = append(scr, u.text(3+i, 2, fmt.Sprintf("%s %9s %s %s", bbs.Fit(f.Name, 28),
				files.Size(f.Size), f.Created.Format("2006-01-02"), f.Description), go3270.Green, false))
		}
		if len(list) == 0 {
			scr = append(scr, u.text(4, 2, "No files here yet.", go3270.White, false))
		}
		resp, v, err := u.show(scr, nil)
		if err != nil {
			return err
		}
		switch resp.AID {
		case go3270.AIDPF3:
			return nil
		case go3270.AIDPF7:
			top = max(0, top-per)
			continue
		case go3270.AIDPF8:
			if top+per < len(list) {
				top += per
			}
			continue
		case go3270.AIDEnter:
		default:
			continue
		}
		if cmd := strings.TrimSpace(v["cmd"]); cmd != "" {
			if ind, ok := go3270.ParseIndFile(cmd); ok {
				if msg, err = u.indFile(a, ind); err != nil {
					return err
				}
			} else {
				msg = "Commands here are IND$FILE GET and PUT, from your emulator's file transfer."
			}
			continue
		}
		for i := 0; i < per && top+i < len(list); i++ {
			if v[fmt.Sprintf("s%d", i)] != "" {
				if err := u.fileDetail(a, list[top+i]); err != nil {
					return err
				}
				break
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
