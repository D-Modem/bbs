package ui3270

import (
	"bytes"
	"errors"
	"strings"
	"time"

	"github.com/racingmars/go3270"

	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// dftTimeout is how long an emulator may take to answer in a transfer.
const dftTimeout = 60 * time.Second

// IND$FILE's messages
const (
	transOK      = "TRANS03   File transfer complete"
	transNoFile  = "TRANS14 File not found: file transfer canceled"
	transBadName = "TRANS17 Incorrect file name: file transfer canceled"
	transBadParm = "TRANS18 Incorrect parameter: file transfer canceled"
	transWrite   = "TRANS13 Error writing file to host: file transfer canceled"
	transRead    = "TRANS16 Error reading file from host: file transfer canceled"
	transDenied  = "TRANS15 Not permitted: file transfer canceled"
)

// indFile runs an IND$FILE transfer the emulator started: GET sends a file
// of the area, PUT receives one into it.  The message is for the screen
// after.
func (u *ui) indFile(a *store.FileArea, ind *go3270.IndFile) (string, error) {
	conn := u.g.Conn // the transfer's records are not screens: no guard
	if go3270.DFTBufferSize(u.dev) == 0 {
		// the emulator waits for the transfer to start, and gives up
		return "Your emulator does not do DFT file transfers.", nil
	}
	name := strings.Trim(ind.Name, `'"`)
	if i := strings.LastIndexAny(name, `/\:`); i >= 0 {
		name = name[i+1:]
	}
	end := func(msg string) (string, error) {
		if err := go3270.DFTMessage(conn, dftTimeout, msg); err != nil {
			u.b.Log.Printf("node %d: IND$FILE: %v", u.n.Num, err)
		}
		if msg == transOK {
			return "Transfer complete.", nil
		}
		return msg[8:], nil
	}
	if ind.Get {
		f, err := u.b.Store.FileByName(a.ID, name)
		if err != nil {
			return end(transNoFile)
		}
		if err := u.b.MayDownload(u.user, a); err != nil {
			end(transDenied)
			return capital(err.Error()) + ".", nil
		}
		r, err := u.b.Files.OpenFile(f)
		if err != nil {
			return end(transRead)
		}
		defer r.Close()
		u.n.Doing("Downloading " + f.Name)
		start := time.Now()
		n, err := go3270.SendFileDFT(conn, u.dev, r, dftTimeout)
		if err != nil {
			u.b.Log.Printf("node %d: %s: IND$FILE GET %q failed: %v", u.n.Num, u.user.Name, f.Name, err)
			if errors.Is(err, go3270.ErrDFTAborted) {
				return "The transfer was cancelled.", nil
			}
			return end(transRead)
		}
		u.b.Downloaded(u.user, f)
		u.b.Log.Printf("node %d: %s downloaded %q by IND$FILE (%d bytes, %v)", u.n.Num, u.user.Name,
			f.Name, n, time.Since(start).Round(time.Second))
		return end(transOK)
	}

	if ind.Append {
		return end(transBadParm)
	}
	if files.ValidName(name) != "" {
		return end(transBadName)
	}
	if !u.b.CanUpload(u.user, a) {
		end(transDenied)
		return "Your access does not allow uploading here.", nil
	}
	up, err := u.b.Files.Upload(u.user, a, name, "")
	switch {
	case errors.Is(err, files.ErrDenied), errors.Is(err, files.ErrQuota):
		return end(transDenied)
	case errors.Is(err, files.ErrExists):
		return end("TRANS17 A file of that name is there already: file transfer canceled")
	case err != nil:
		return end(transWrite)
	}
	u.n.Doing("Uploading to " + a.Name)
	n, err := go3270.ReceiveFileDFT(conn, u.dev, up, u.b.Files.MaxSize, dftTimeout)
	if err != nil {
		up.Abort()
		u.b.Log.Printf("node %d: %s: IND$FILE PUT %q failed after %d bytes: %v", u.n.Num, u.user.Name, name, n, err)
		if errors.Is(err, go3270.ErrDFTAborted) {
			return "The transfer was cancelled.", nil
		}
		return end(transWrite)
	}
	f, err := up.Commit()
	if err != nil {
		return end(transWrite)
	}
	u.b.Log.Printf("node %d: %s uploaded %q to %s by IND$FILE (%d bytes, sha256 %s)", u.n.Num,
		u.user.Name, f.Name, a.Name, f.Size, f.SHA256)
	return end(transOK)
}

// maxData is the most an IND$FILE PUT of data brings.
const maxData = 16 << 20

// indData runs an IND$FILE transfer of a file made for the caller: GET
// sends what get makes of the name, PUT gives what came to put.
func (u *ui) indData(ind *go3270.IndFile, get func(string) ([]byte, func(), error),
	put func(string, []byte) string) (string, error) {
	conn := u.g.Conn
	if go3270.DFTBufferSize(u.dev) == 0 {
		return "Your emulator does not do DFT file transfers.", nil
	}
	name := strings.Trim(ind.Name, `'"`)
	if i := strings.LastIndexAny(name, `/\:`); i >= 0 {
		name = name[i+1:]
	}
	end := func(msg string) {
		if err := go3270.DFTMessage(conn, dftTimeout, msg); err != nil {
			u.b.Log.Printf("node %d: IND$FILE: %v", u.n.Num, err)
		}
	}
	if ind.Get {
		data, sent, err := get(name)
		if err != nil {
			end(transNoFile)
			return capital(err.Error()) + ".", nil
		}
		u.n.Doing("Downloading " + name)
		if _, err := go3270.SendFileDFT(conn, u.dev, bytes.NewReader(data), dftTimeout); err != nil {
			u.b.Log.Printf("node %d: %s: IND$FILE GET %q failed: %v", u.n.Num, u.user.Name, name, err)
			if errors.Is(err, go3270.ErrDFTAborted) {
				return "The transfer was cancelled.", nil
			}
			end(transRead)
			return "The transfer failed.", nil
		}
		sent()
		end(transOK)
		return "Sent " + name + ".", nil
	}
	if ind.Append {
		end(transBadParm)
		return "", nil
	}
	var buf bytes.Buffer
	u.n.Doing("Uploading " + name)
	if _, err := go3270.ReceiveFileDFT(conn, u.dev, &buf, maxData, dftTimeout); err != nil {
		u.b.Log.Printf("node %d: %s: IND$FILE PUT %q failed: %v", u.n.Num, u.user.Name, name, err)
		if errors.Is(err, go3270.ErrDFTAborted) {
			return "The transfer was cancelled.", nil
		}
		end(transWrite)
		return "The transfer failed.", nil
	}
	end(transOK)
	return put(name, buf.Bytes()), nil
}

// capital is s with its first letter upper case (an error as a sentence).
func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
