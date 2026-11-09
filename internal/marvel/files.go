package marvel

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/D-Modem/bbs/internal/store"
)

// Files: the BBS's file areas as MSN folders, each file a
// Download-and-Run node.  Opening one has the client's File Transfer
// Manager fetch it over an FTM pipe, into a temporary file the client then
// opens as Explorer would.

var keyFiles = nodeKey{1, 0x13}

const (
	libBase = 0x200000   // 1:libBase+area is a file area's folder
	fileTag = 0x454C4946 // "FILE": a file's node is (its id, fileTag)
)

// a file area and the files in it
type library struct {
	area  *store.FileArea
	files []*store.File
}

// libraries are the file areas the caller may download from.
func (c *conn) libraries(u *store.User) []library {
	b := c.s.B
	if b.Files == nil || u == nil {
		return nil
	}
	areas, err := b.Store.FileAreas()
	if err != nil {
		c.logf("file areas: %v", err)
	}
	var out []library
	for _, a := range areas {
		if !b.CanDownload(u, a) {
			continue
		}
		fs, err := b.Store.Files(a.ID)
		if err != nil {
			c.logf("files: %v", err)
		}
		out = append(out, library{a, fs})
	}
	return out
}

// FTM is the File Transfer Manager's service.
type ftm struct {
	c    *conn
	pipe uint16

	mu   sync.Mutex
	stop chan struct{} // closed when the pipe or the call goes
}

const (
	ftmRequest = 0x00 // HrRequestDownload: what the file is, and how it comes
	ftmStart   = 0x01 // HrStartDownload: the file, a block at a time

	cfiSize   = 60 // FtmClientFileId: the service's name, then a file id of its own
	cfiFileAt = 0x20
	ftmReply  = 72

	ftmHasUnpack = 0x01 // reply flags: the unpack method is given (0: none)

	// the most one block of the file may carry: the client's receive
	// state for a block is this big
	ftmBlockMax = 0x4000
)

func newFTM(c *conn, pipe uint16) *ftm { return &ftm{c: c, pipe: pipe, stop: make(chan struct{})} }

func (f *ftm) ifaces() []iface { return []iface{mosIID(0x25, 0x01)} }

func (f *ftm) end() {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.stop:
	default:
		close(f.stop)
	}
}

var errNoFile = errors.New("no such file here")

// file is the file a client file id names: DIRSRV's, a file node's "fi".
func (f *ftm) file(ps []param) (*store.File, *store.FileArea, error) {
	var cfi []byte
	for _, p := range ps {
		if p.Tag == 0x04 && len(p.Data) >= cfiSize {
			cfi = p.Data
			break
		}
	}
	if cfi == nil || asciiz(cfi[:cfiFileAt]) != "DIRSRV" {
		return nil, nil, errNoFile
	}
	// the node's id, its halves the other way round, then the property's name
	k := nodeKey{binary.LittleEndian.Uint32(cfi[cfiFileAt+4:]), binary.LittleEndian.Uint32(cfi[cfiFileAt:])}
	if k.b != fileTag || asciiz(cfi[cfiFileAt+8:cfiFileAt+16]) != "fi" {
		return nil, nil, errNoFile
	}
	u := f.c.signedOn()
	b := f.c.s.B
	if u == nil || b.Files == nil {
		return nil, nil, errNoFile
	}
	file, err := b.Store.FileByID(int64(k.a))
	if err != nil {
		return nil, nil, errNoFile
	}
	area, err := b.Store.FileArea(file.AreaID)
	if err != nil {
		return nil, nil, errNoFile
	}
	if err := b.MayDownload(u, area); err != nil {
		return nil, nil, err
	}
	return file, area, nil
}

func (f *ftm) call(h hostBlock) ([]byte, bool) {
	if h.oneWay() {
		return nil, false
	}
	ps, _ := parseParams(h.Params)
	switch h.Method {
	case ftmRequest:
		rec := make([]byte, ftmReply)
		file, _, err := f.file(ps)
		if err != nil {
			f.c.logf("FTM: %v", err)
			binary.LittleEndian.PutUint32(rec, mapiFail)
			return reply{}.bytes_(rec), true
		}
		size := uint32(min(file.Size, 1<<31-1))
		binary.LittleEndian.PutUint32(rec[0x08:], size) // what to make room for
		binary.LittleEndian.PutUint32(rec[0x0C:], size) // what comes
		// no fast path: the file comes on HrStartDownload, from its start
		binary.LittleEndian.PutUint32(rec[0x10:], ftmHasUnpack)
		return reply{}.bytes_(rec), true
	case ftmStart:
		file, _, err := f.file(ps)
		if err != nil {
			f.c.logf("FTM: %v", err)
			st := binary.LittleEndian.AppendUint32(nil, mapiFail)
			f.c.sendPipe(f.pipe, hostBlock{Iface: h.Iface, Method: h.Method, ID: h.ID,
				Params: append(reply{}.bytes_(append(st, 0, 0, 0, 0)).endStatic(), tagComplete)}.bytes())
			return nil, false
		}
		go f.send(h, file)
		return nil, false
	}
	f.c.logf("FTM: no method %#x", h.Method)
	return nil, false
}

// send streams a file: each block closes a message of its own (0x88), and
// a bare 0x86 ends the request.
func (f *ftm) send(h hostBlock, file *store.File) {
	b := f.c.s.B
	in, err := b.Files.OpenFile(file)
	if err != nil {
		f.c.logf("FTM: %s: %v", file.Name, err)
		in = nil
	}
	var r io.Reader = bytes.NewReader(nil)
	if in != nil {
		defer in.Close()
		r = in
	}
	block := func(params []byte) error {
		return f.c.sendPipe(f.pipe, hostBlock{Iface: h.Iface, Method: h.Method, ID: h.ID, Params: params}.bytes())
	}
	f.c.mu.Lock()
	n := f.c.node
	f.c.mu.Unlock()
	if n != nil {
		n.Doing("MSN: downloading " + file.Name)
		defer n.Doing("MSN")
	}
	head := reply{}.bytes_(make([]byte, 8)).endStatic() // the status, 0
	buf := make([]byte, ftmBlockMax)
	var sent int64
	for first := true; ; first = false {
		k, rerr := io.ReadFull(r, buf)
		if k > 0 || first {
			params := []byte{tagStreamEnd}
			if first {
				params = append(append([]byte(nil), head...), tagStreamEnd)
			}
			if err := block(append(params, buf[:k]...)); err != nil {
				return
			}
			sent += int64(k)
		}
		if rerr != nil {
			if rerr != io.EOF && rerr != io.ErrUnexpectedEOF {
				f.c.logf("FTM: %s: %v", file.Name, rerr)
			}
			break
		}
		if !f.wait() {
			return
		}
	}
	if in != nil && sent == file.Size {
		b.Downloaded(f.c.signedOn(), file)
		f.c.logf("downloaded %q (%d bytes)", file.Name, sent)
	}
	block([]byte{tagComplete})
}

// wait holds the stream back while the link has much still to send, and
// says whether to go on.
func (f *ftm) wait() bool {
	bl, _ := f.c.l.(interface{ backlog() int })
	for {
		select {
		case <-f.stop:
			return false
		default:
		}
		if bl == nil || bl.backlog() < 4*window {
			return true
		}
		select {
		case <-f.stop:
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
}
