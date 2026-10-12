package textui

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

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

// download sends a file.
func (s *session) download(a *store.FileArea, f *store.File) error {
	t := s.t
	if err := s.b.MayDownload(s.user, a); err != nil {
		t.Println(t.C(Red, capital(err.Error())+"."))
		return nil
	}
	p, err := s.chooseProtocol()
	if err != nil || p == nil {
		return err
	}
	r, err := s.b.Files.OpenFile(f)
	if err != nil {
		return err
	}
	defer r.Close()
	t.Println(t.C(Cyan, startHint(p, false)))
	port, err := t.Port()
	if err != nil {
		return err
	}
	s.n.Doing("Downloading " + f.Name)
	start := time.Now()
	err = p.Send(port, []xfer.Outgoing{{Name: f.Name, Size: f.Size, ModTime: f.Created, Data: r}})
	t.Settle(time.Second)
	t.Println("")
	if err != nil {
		s.b.Log.Printf("node %d: %s: %s download of %q failed: %v", s.n.Num, s.user.Name, p.Name, f.Name, err)
		t.Println(t.C(Red, "The transfer failed: "+err.Error()+"."))
		return nil
	}
	s.b.Downloaded(s.user, f)
	s.b.Log.Printf("node %d: %s downloaded %q by %s (%d bytes, %v)", s.n.Num, s.user.Name, f.Name,
		p.Name, f.Size, time.Since(start).Round(time.Second))
	t.Println(t.C(Green, "Sent "+f.Name+"."))
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

// upload receives files into an area.
func (s *session) upload(a *store.FileArea) error {
	t := s.t
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

// capital is s with its first letter upper case (an error as a sentence).
func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
