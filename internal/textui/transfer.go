package textui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/Neo-Desktop/bbs-sec/internal/app"
	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
	"github.com/Neo-Desktop/bbs-sec/internal/xfer"
)

// chooseProtocol asks for a transfer protocol; nil if the caller gave up.
func (s *session) chooseProtocol() (*xfer.Protocol, error) {
	t := s.t
	t.Println("")
	if !t.Clean8 {
		t.Println(t.C(Red, "Your telnet client is not in binary mode: X/Y/ZMODEM will likely fail; Kermit may work."))
	}
	for _, p := range xfer.Protocols {
		t.Println("  " + t.C(Yellow, "["+p.Key+"]") + " " + p.Name)
	}
	t.Print("Protocol, [Q]uit: ")
	for {
		k, err := t.Key()
		if err != nil {
			return nil, err
		}
		if k == 'Q' || k == '\r' {
			t.Println("")
			return nil, nil
		}
		if p := xfer.Lookup(string(k)); p != nil {
			t.Println(p.Name)
			return p, nil
		}
	}
}

func startHint(p *xfer.Protocol, upload bool) string {
	dir := "download"
	if upload {
		dir = "upload"
	}
	return fmt.Sprintf("Start your terminal program's %s %s now; Ctrl-X five times cancels.", p.Name, dir)
}

// Transfers: files go by transfer protocols.
func (s *session) Transfers() bool { return true }

// FileCommand: there is no file area command line.
func (s *session) FileCommand(*store.FileArea, string) (string, error) { return "", nil }

// started marks when a protocol first reads a file.
type started struct {
	r  io.Reader
	on *bool
}

func (s started) Read(p []byte) (int, error) {
	*s.on = true
	return s.r.Read(p)
}

// Download sends files: many at once by a batch protocol, one by XMODEM.
func (s *session) Download(fs []app.FileRef) error {
	t := s.t
	s.user = s.n.User()
	for _, r := range fs {
		if !s.b.CanDownload(s.user, r.Area) {
			t.Println(t.C(Red, "Your access does not allow downloading from "+r.Area.Name+"."))
			return nil
		}
	}
	if left, why := s.b.DownloadsLeft(s.user); left >= 0 && len(fs) > left {
		t.Println(t.C(Red, capital(why.Error())+"."))
		return nil
	}
	p, err := s.chooseProtocol()
	if err != nil || p == nil {
		return err
	}
	if !p.Batch && len(fs) > 1 {
		t.Println(t.C(Red, p.Name+" sends one file at a time: choose a batch protocol (YMODEM, ZMODEM, Kermit)."))
		return nil
	}
	out := make([]xfer.Outgoing, len(fs))
	begun := make([]bool, len(fs))
	var size int64
	for i, r := range fs {
		f := r.File
		in, err := s.b.Files.OpenFile(f)
		if err != nil {
			return err
		}
		defer in.Close()
		out[i] = xfer.Outgoing{Name: f.Name, Size: f.Size, ModTime: f.Created, Data: started{in, &begun[i]}}
		size += f.Size
	}
	t.Println(t.C(Cyan, startHint(p, false)))
	port, err := t.Port()
	if err != nil {
		return err
	}
	what := fs[0].File.Name
	if len(fs) > 1 {
		what = fmt.Sprintf("%d files", len(fs))
	}
	s.n.Doing("Downloading " + what)
	start := time.Now()
	err = p.Send(port, out)
	t.Settle(time.Second)
	t.Println("")
	// a file was sent when the transfer ended well, or went on to the next
	for i, r := range fs {
		if err == nil || i+1 < len(fs) && begun[i+1] {
			s.b.Downloaded(s.user, r.File)
			s.b.Log.Printf("node %d: %s downloaded %q by %s (%d bytes)", s.n.Num, s.user.Name, r.File.Name,
				p.Name, r.File.Size)
			t.Println(t.C(Green, "Sent "+r.File.Name+"."))
		}
	}
	if err != nil {
		s.b.Log.Printf("node %d: %s: %s download of %s failed: %v", s.n.Num, s.user.Name, p.Name, what, err)
		t.Println(t.C(Red, "The transfer failed: "+err.Error()+"."))
		return nil
	}
	s.b.Log.Printf("node %d: %s: %s sent by %s, %d bytes in %v", s.n.Num, s.user.Name, what, p.Name,
		size, time.Since(start).Round(time.Second))
	return nil
}

// incoming is an upload, for a transfer protocol.
type incoming struct {
	up   *files.Upload
	done func(*store.File)
}

func (in *incoming) Write(p []byte) (int, error) { return in.up.Write(p) }
func (in *incoming) Abort()                      { in.up.Abort() }
func (in *incoming) Commit() error {
	f, err := in.up.Commit()
	if err == nil {
		in.done(f)
	}
	return err
}

// safeName makes a name a terminal program sent into one the file areas
// take: its last path element, characters they do not take as _, no
// leading dots or dashes, at most 64 long.  "" if nothing is left.
func safeName(name string) string {
	if i := strings.LastIndexAny(name, `/\:`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			return r
		case strings.ContainsRune(" ._+()[]!,#&'@~=-", r):
			return r
		}
		return '_'
	}, name)
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", ".")
	}
	name = strings.TrimLeft(name, ".- _")
	if len(name) > 64 {
		name = name[:64]
	}
	name = strings.TrimRight(name, ". ")
	if files.ValidName(name) != "" {
		return ""
	}
	return name
}

// Upload receives files into an area.
func (s *session) Upload(a *store.FileArea) error {
	t := s.t
	s.user = s.n.User()
	if !s.b.CanUpload(s.user, a) {
		t.Println(t.C(Red, "Your access does not allow uploading here."))
		return nil
	}
	p, err := s.chooseProtocol()
	if err != nil || p == nil {
		return err
	}
	given := ""
	if !p.Batch {
		// XMODEM sends no name
		for {
			given, err = t.Input("Name of the file: ", 64)
			if err != nil || given == "" {
				return err
			}
			if msg := files.ValidName(given); msg != "" {
				t.Println(t.C(Red, msg))
				continue
			}
			break
		}
	}
	var got []*store.File
	var refused []string
	sink := func(name string, size int64) (xfer.Incoming, error) {
		if given != "" {
			name = given
		} else if clean := safeName(name); clean != "" {
			name = clean
		} else {
			refused = append(refused, bbs.Clean(name, 64))
			return nil, errors.New("a name the file areas do not take")
		}
		if size > s.b.Files.MaxSize {
			refused = append(refused, name)
			return nil, files.ErrTooLarge
		}
		up, err := s.b.Files.Upload(s.user, a, name, "")
		if err != nil {
			refused = append(refused, name+" ("+err.Error()+")")
			return nil, err
		}
		return &incoming{up: up, done: func(f *store.File) { got = append(got, f) }}, nil
	}
	t.Println(t.C(Cyan, startHint(p, true)))
	port, err := t.Port()
	if err != nil {
		return err
	}
	s.n.Doing("Uploading to " + a.Name)
	err = p.Receive(port, sink)
	t.Settle(time.Second)
	t.Println("")
	for _, f := range got {
		s.b.Log.Printf("node %d: %s uploaded %q to %s by %s (%d bytes, sha256 %s)", s.n.Num,
			s.user.Name, f.Name, a.Name, p.Name, f.Size, f.SHA256)
	}
	if err != nil {
		s.b.Log.Printf("node %d: %s: %s upload failed: %v", s.n.Num, s.user.Name, p.Name, err)
		t.Println(t.C(Red, "The transfer failed: "+err.Error()+"."))
	}
	for _, r := range refused {
		t.Println(t.C(Red, "Not taken: "+r))
	}
	for _, f := range got {
		t.Println(t.C(Green, fmt.Sprintf("Received %s, %s.", f.Name, files.Size(f.Size))))
		desc, err := t.Input("Describe it: ", 60)
		if err != nil {
			return err
		}
		if desc = bbs.Clean(desc, 60); desc != "" {
			s.b.Store.SetFileDescription(f.ID, desc)
		}
	}
	return nil
}

// SendData sends a file made for the caller.
func (s *session) SendData(name string, data []byte) (bool, error) {
	t := s.t
	p, err := s.chooseProtocol()
	if err != nil || p == nil {
		return false, err
	}
	t.Println(t.C(Cyan, startHint(p, false)))
	port, err := t.Port()
	if err != nil {
		return false, err
	}
	s.n.Doing("Downloading " + name)
	err = p.Send(port, []xfer.Outgoing{{Name: name, Size: int64(len(data)), ModTime: time.Now(),
		Data: bytes.NewReader(data)}})
	t.Settle(time.Second)
	t.Println("")
	if err != nil {
		s.b.Log.Printf("node %d: %s: %s download of %s failed: %v", s.n.Num, s.n.User().Name, p.Name, name, err)
		t.Println(t.C(Red, "The transfer failed: "+err.Error()+"."))
		return false, nil
	}
	t.Println(t.C(Green, "Sent "+name+"."))
	return true, nil
}

// buffer takes an upload into memory, up to max bytes.
type buffer struct {
	b    bytes.Buffer
	max  int64
	done *[]byte
}

func (in *buffer) Write(p []byte) (int, error) {
	if int64(in.b.Len()+len(p)) > in.max {
		return 0, files.ErrTooLarge
	}
	return in.b.Write(p)
}
func (in *buffer) Abort()        {}
func (in *buffer) Commit() error { *in.done = in.b.Bytes(); return nil }

// ReceiveData receives one file from the caller.
func (s *session) ReceiveData(max int64) ([]byte, error) {
	t := s.t
	p, err := s.chooseProtocol()
	if err != nil || p == nil {
		return nil, err
	}
	var got []byte
	taken := false
	sink := func(name string, size int64) (xfer.Incoming, error) {
		if taken {
			return nil, errors.New("one file at a time")
		}
		if size > max {
			return nil, files.ErrTooLarge
		}
		taken = true
		return &buffer{max: max, done: &got}, nil
	}
	t.Println(t.C(Cyan, startHint(p, true)))
	port, err := t.Port()
	if err != nil {
		return nil, err
	}
	s.n.Doing("Uploading")
	err = p.Receive(port, sink)
	t.Settle(time.Second)
	t.Println("")
	if err != nil && got == nil {
		s.b.Log.Printf("node %d: %s: %s upload failed: %v", s.n.Num, s.n.User().Name, p.Name, err)
		t.Println(t.C(Red, "The transfer failed: "+err.Error()+"."))
		return nil, nil
	}
	return got, nil
}

// DataCommand: there is no command line.
func (s *session) DataCommand(string, func(string) ([]byte, func(), error), func(string, []byte) string) (string, error) {
	return "", nil
}

// capital is s with its first letter upper case (an error as a sentence).
func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
