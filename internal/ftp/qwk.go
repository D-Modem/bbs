package ftp

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
)

// /qwk is a user's offline mail: BBSID.QWK, their packet, made as it is
// asked for (its messages count as read once it has all been sent), and
// a REP put there brings their replies in.  It shadows a file area of
// that name.

const qwkDir = "qwk"

// qwkPath: the path is /qwk (q "") or a file in it, for a user logged in.
func (fs *fileSystem) qwkPath(name string) (q string, ok bool) {
	if fs.anonymous() {
		return "", false
	}
	p := strings.Trim(path.Clean("/"+name), "/")
	dir, file, _ := strings.Cut(p, "/")
	if !strings.EqualFold(dir, qwkDir) || strings.Contains(file, "/") {
		return "", false
	}
	return file, true
}

// qwkPacket is the user's packet: made again after a minute, or once it
// has been taken.
func (fs *fileSystem) qwkPacket() (*bbs.QWKOut, error) {
	if fs.packet != nil && time.Since(fs.made) < time.Minute {
		return fs.packet, nil
	}
	out, err := fs.s.B.Agent(fs.user, "ftp").QWKPacket()
	if err != nil {
		return nil, err
	}
	fs.packet, fs.made = out, time.Now()
	return out, nil
}

// qwkStat describes /qwk (q "") or the user's packet, BBSID.QWK (any case),
// making the packet if it is due; anything else there is not.
func (fs *fileSystem) qwkStat(q string) (os.FileInfo, error) {
	if q == "" {
		return info{name: qwkDir, dir: true, mtime: time.Unix(0, 0)}, nil
	}
	if !strings.EqualFold(q, fs.s.B.BBSID()+".QWK") {
		return nil, os.ErrNotExist
	}
	out, err := fs.qwkPacket()
	if err != nil {
		return nil, err
	}
	return info{name: out.Name, size: int64(len(out.Data)), mtime: fs.made}, nil
}

// qwkHandle opens the user's packet to download, or takes a .REP upload (from
// its start); nothing else in /qwk can be opened.
func (fs *fileSystem) qwkHandle(q string, flags int, offset int64) (ftpserver.FileTransfer, error) {
	if flags&(os.O_WRONLY|os.O_RDWR) == 0 {
		if _, err := fs.qwkStat(q); err != nil || q == "" {
			return nil, os.ErrNotExist
		}
		out, err := fs.qwkPacket()
		if err != nil {
			return nil, err
		}
		return &qwkDownload{r: bytes.NewReader(out.Data), fs: fs, out: out}, nil
	}
	if !strings.HasSuffix(strings.ToUpper(q), ".REP") || offset != 0 || flags&os.O_APPEND != 0 {
		return nil, fmt.Errorf("%w: put your replies here as %s.REP", os.ErrPermission, fs.s.B.BBSID())
	}
	return &repUpload{fs: fs}, nil
}

// qwkDownload is the packet being sent: once all of it has gone, its
// messages count as read.  The reader is not embedded: its WriteTo would
// let the copy go around Read.
type qwkDownload struct {
	r    *bytes.Reader
	fs   *fileSystem
	out  *bbs.QWKOut
	read bool
}

// Read reads the packet, noting that some has gone.
func (d *qwkDownload) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	d.read = d.read || n > 0
	return n, err
}

// Seek moves in the packet (for a resumed download).
func (d *qwkDownload) Seek(offset int64, whence int) (int64, error) { return d.r.Seek(offset, whence) }

// Write is refused: the packet is read only.
func (d *qwkDownload) Write([]byte) (int, error) { return 0, os.ErrPermission }

// Close, once the packet has all been read, marks its messages read and has
// the next one made afresh; else it changes nothing.
func (d *qwkDownload) Close() error {
	if d.read && d.r.Len() == 0 {
		d.fs.packet = nil
		d.fs.s.B.Log.Printf("%s: %s took %s", d.fs.who, d.fs.user.Name, d.out.Name)
		return d.out.Done()
	}
	return nil
}

// repUpload is a REP coming: its replies go in once it has all come.
type repUpload struct {
	fs     *fileSystem
	buf    bytes.Buffer
	failed error
}

// Read is refused: a REP is written only.
func (u *repUpload) Read([]byte) (int, error) { return 0, os.ErrPermission }

// Seek is refused: a REP comes from its start.
func (u *repUpload) Seek(int64, int) (int64, error) { return 0, os.ErrPermission }

// TransferError is told of a transfer that failed, so Close imports nothing.
func (u *repUpload) TransferError(err error) { u.failed = err }

// Write keeps what comes, refusing a REP larger than qwk.MaxREP.
func (u *repUpload) Write(p []byte) (int, error) {
	if u.buf.Len()+len(p) > qwk.MaxREP {
		return 0, errors.New("a REP packet is at most 16 MB")
	}
	return u.buf.Write(p)
}

// Close imports the REP's replies, if it all came, logging those refused,
// and has the next packet made afresh.
func (u *repUpload) Close() error {
	if u.failed != nil {
		return u.failed
	}
	res, err := u.fs.s.B.Agent(u.fs.user, "ftp").ImportREP(u.buf.Bytes())
	if err != nil {
		return err
	}
	u.fs.packet = nil // what was posted may change what is new
	for _, e := range res.Errors {
		u.fs.s.B.Log.Printf("%s: %s's REP: %s", u.fs.who, u.fs.user.Name, e)
	}
	return nil
}
