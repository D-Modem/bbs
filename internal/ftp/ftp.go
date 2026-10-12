// Package ftp serves the BBS's file areas by FTP (and FTPS): users log in
// with their BBS name and password, each file area is a directory, and they
// download, upload (where they may) and delete their own files.
//
// The protocol is fclairamb/ftpserverlib's; this package is its driver: a
// file system of the database's areas and files, with nothing of the
// server's own file system reachable.
package ftp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/files"
	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// TLS modes.
const (
	TLSNo       = "no"       // plain FTP: passwords in the clear
	TLSOptional = "optional" // AUTH TLS offered
	TLSRequired = "required" // AUTH TLS before USER (explicit FTPS)
	TLSImplicit = "implicit" // TLS from the first byte (port 990 style)
)

// Settings are the [ftp] section's.
type Settings struct {
	Address     string // host:port
	PublicHost  string // the address passive replies give, behind NAT ("": the local one)
	PassiveMin  int    // passive data ports (0: any)
	PassiveMax  int
	TLS         string
	CertFile    string
	KeyFile     string
	Anonymous   bool // "anonymous" and "ftp" may log in, to download only
	MaxClients  int
	MaxPerIP    int
	IdleTimeout time.Duration
	Banner      string
}

// Server is the FTP server.
type Server struct {
	B   *bbs.BBS
	Set Settings

	tlsConfig *tls.Config
	mu        sync.Mutex
	clients   int
	perIP     map[string]int
}

// New makes the server, loading the certificate if TLS is on.
func New(b *bbs.BBS, set Settings) (*Server, error) {
	if b.Files == nil {
		return nil, errors.New("ftp: no file store")
	}
	s := &Server{B: b, Set: set, perIP: map[string]int{}}
	if set.TLS != TLSNo {
		cert, err := tls.LoadX509KeyPair(set.CertFile, set.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("ftp: certificate: %w", err)
		}
		s.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}
	return s, nil
}

// Run serves until ctx is done.
func (s *Server) Run(ctx context.Context) error {
	srv := ftpserver.NewFtpServer(s)
	srv.Logger = slog.New(slog.NewTextHandler(s.B.Log.Writer(), &slog.HandlerOptions{Level: slog.LevelError}))
	if err := srv.Listen(); err != nil {
		return fmt.Errorf("ftp: %w", err)
	}
	s.B.Log.Printf("ftp: listening on %s (tls %s)", s.Set.Address, s.Set.TLS)
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	select {
	case <-ctx.Done():
		srv.Stop()
		<-done
		return nil
	case err := <-done:
		return err
	}
}

// GetSettings is the library's settings: no active mode (FTP bounce), data
// connections only from the control connection's address, binary by
// default, no SITE, no compression.
func (s *Server) GetSettings() (*ftpserver.Settings, error) {
	set := &ftpserver.Settings{
		ListenAddr:             s.Set.Address,
		PublicHost:             s.Set.PublicHost,
		Banner:                 s.Set.Banner,
		IdleTimeout:            int(s.Set.IdleTimeout / time.Second),
		ConnectionTimeout:      30,
		DisableActiveMode:      true,
		DisableSite:            true,
		DisableMFMT:            true,
		DefaultTransferType:    ftpserver.TransferTypeBinary,
		ActiveConnectionsCheck: ftpserver.IPMatchRequired,
		PasvConnectionsCheck:   ftpserver.IPMatchRequired,
	}
	if s.Set.PassiveMin > 0 {
		set.PassiveTransferPortRange = ftpserver.PortRange{Start: s.Set.PassiveMin, End: s.Set.PassiveMax}
	}
	switch s.Set.TLS {
	case TLSRequired:
		set.TLSRequired = ftpserver.MandatoryEncryption
	case TLSImplicit:
		set.TLSRequired = ftpserver.ImplicitEncryption
	default:
		set.TLSRequired = ftpserver.ClearOrEncrypted
	}
	return set, nil
}

// GetTLSConfig is the certificate's.
func (s *Server) GetTLSConfig() (*tls.Config, error) {
	if s.tlsConfig == nil {
		return nil, errors.New("TLS is not set up")
	}
	return s.tlsConfig, nil
}

func host(a net.Addr) string {
	if t, ok := a.(*net.TCPAddr); ok {
		return t.IP.String()
	}
	return ""
}

// ClientConnected greets a client, or turns it away over the limits.
func (s *Server) ClientConnected(cc ftpserver.ClientContext) (string, error) {
	if s.B.Refuse("ftp", cc.RemoteAddr()) {
		return "Not here.", errors.New("refused")
	}
	h := host(cc.RemoteAddr())
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Set.MaxClients > 0 && s.clients >= s.Set.MaxClients {
		return "Too many users; try again later.", errors.New("too many clients")
	}
	if s.Set.MaxPerIP > 0 && s.perIP[h] >= s.Set.MaxPerIP {
		return "Too many connections from your address.", errors.New("too many from one address")
	}
	s.clients++
	s.perIP[h]++
	cc.SetExtra(h)
	return s.B.Name + " file areas.  Log in with your BBS name and password.", nil
}

// ClientDisconnected counts the client out.
func (s *Server) ClientDisconnected(cc ftpserver.ClientContext) {
	h, ok := cc.Extra().(string)
	if !ok {
		// a client turned away by ClientConnected was never counted;
		// one that logged in carries its fs as extra (see AuthUser)
		if fs, isFS := cc.Extra().(*fileSystem); isFS {
			h = fs.host
		} else {
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients--
	if s.perIP[h]--; s.perIP[h] <= 0 {
		delete(s.perIP, h)
	}
}

// AuthUser checks the BBS name and password.
func (s *Server) AuthUser(cc ftpserver.ClientContext, name, pass string) (ftpserver.ClientDriver, error) {
	h := host(cc.RemoteAddr())
	source := ""
	if a, ok := cc.RemoteAddr().(*net.TCPAddr); ok && !a.IP.IsLoopback() {
		source = "ip:" + h
	}
	who := fmt.Sprintf("ftp %d", cc.ID())
	var u *store.User
	if s.Set.Anonymous && (strings.EqualFold(name, "anonymous") || strings.EqualFold(name, "ftp")) {
		u = &store.User{Name: "anonymous"} // no ID: may not upload or delete
	} else {
		var err error
		u, err = s.B.Authenticate(who, source, name, pass)
		if err != nil {
			return nil, err
		}
	}
	tls := ""
	if cc.HasTLSForControl() {
		tls = ", TLS"
	}
	s.B.Log.Printf("%s: %s logged in from %s%s", who, u.Name, h, tls)
	fs := &fileSystem{s: s, user: u, host: h, who: who}
	cc.SetExtra(fs)
	return fs, nil
}

// fileSystem is a user's view of the file areas: / has a directory for
// each area, each area its files.  It is all there is.
type fileSystem struct {
	s    *Server
	user *store.User
	host string
	who  string
}

var (
	_ ftpserver.ClientDriver                      = (*fileSystem)(nil)
	_ ftpserver.ClientDriverExtensionFileList     = (*fileSystem)(nil)
	_ ftpserver.ClientDriverExtentionFileTransfer = (*fileSystem)(nil)
	_ ftpserver.ClientDriverExtensionRemoveDir    = (*fileSystem)(nil)
)

func (fs *fileSystem) anonymous() bool { return fs.user.ID == 0 }

// sees: an area the user may download from or upload into
func (fs *fileSystem) sees(a *store.FileArea) bool {
	return fs.s.B.CanDownload(fs.user, a) || !fs.anonymous() && fs.s.B.CanUpload(fs.user, a)
}

// resolve splits a path into its area and file (nil for /, file nil for an
// area's directory).
func (fs *fileSystem) resolve(name string) (*store.FileArea, *store.File, error) {
	p := strings.Trim(path.Clean("/"+name), "/")
	if p == "" {
		return nil, nil, nil
	}
	parts := strings.Split(p, "/")
	if len(parts) > 2 {
		return nil, nil, os.ErrNotExist
	}
	a, err := fs.s.B.Store.FileAreaByName(parts[0])
	if errors.Is(err, store.ErrNotFound) || err == nil && !fs.sees(a) {
		return nil, nil, os.ErrNotExist
	} else if err != nil {
		return nil, nil, err
	}
	if len(parts) == 1 {
		return a, nil, nil
	}
	f, err := fs.s.B.Store.FileByName(a.ID, parts[1])
	if errors.Is(err, store.ErrNotFound) {
		return a, nil, os.ErrNotExist
	} else if err != nil {
		return nil, nil, err
	}
	return a, f, nil
}

// info is what a listing says of an entry.
type info struct {
	name  string
	size  int64
	dir   bool
	mtime time.Time
}

func (i info) Name() string       { return i.name }
func (i info) Size() int64        { return i.size }
func (i info) ModTime() time.Time { return i.mtime }
func (i info) IsDir() bool        { return i.dir }
func (i info) Sys() any           { return nil }
func (i info) Mode() os.FileMode {
	if i.dir {
		return os.ModeDir | 0o555
	}
	return 0o444
}

func areaInfo(a *store.FileArea) info { return info{name: a.Name, dir: true, mtime: time.Unix(0, 0)} }
func fileInfo(f *store.File) info     { return info{name: f.Name, size: f.Size, mtime: f.Created} }

// Stat describes an entry.
func (fs *fileSystem) Stat(name string) (os.FileInfo, error) {
	a, f, err := fs.resolve(name)
	switch {
	case err != nil:
		return nil, err
	case a == nil:
		return info{name: "/", dir: true, mtime: time.Unix(0, 0)}, nil
	case f == nil:
		return areaInfo(a), nil
	}
	return fileInfo(f), nil
}

// ReadDir lists / (the areas) or an area (its files).
func (fs *fileSystem) ReadDir(name string) ([]os.FileInfo, error) {
	a, f, err := fs.resolve(name)
	if err != nil {
		return nil, err
	}
	if f != nil {
		return nil, errors.New("not a directory")
	}
	var out []os.FileInfo
	if a == nil {
		areas, err := fs.s.B.Store.FileAreas()
		if err != nil {
			return nil, err
		}
		for _, a := range areas {
			if fs.sees(a) {
				out = append(out, areaInfo(a))
			}
		}
		return out, nil
	}
	list, err := fs.s.B.Store.Files(a.ID)
	if err != nil {
		return nil, err
	}
	for _, f := range list {
		out = append(out, fileInfo(f))
	}
	return out, nil
}

// GetHandle opens a file to download, or a new one to upload.
func (fs *fileSystem) GetHandle(name string, flags int, offset int64) (ftpserver.FileTransfer, error) {
	a, f, err := fs.resolve(name)
	write := flags&(os.O_WRONLY|os.O_RDWR) != 0
	if !write {
		if err != nil {
			return nil, err
		}
		if f == nil {
			return nil, errors.New("not a file")
		}
		if err := fs.s.B.MayDownload(fs.user, a); err != nil {
			return nil, fmt.Errorf("%w: %v", os.ErrPermission, err)
		}
		r, err := fs.s.B.Files.OpenFile(f)
		if err != nil {
			return nil, err
		}
		return &download{file: r, fs: fs, f: f}, nil
	}
	// an upload: a new file in an area, from its start
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if a == nil || (f == nil && err == nil) {
		// the root, an area that is not there, or an area's directory
		return nil, os.ErrPermission
	}
	if f != nil {
		return nil, fmt.Errorf("%w: %s", os.ErrExist, files.ErrExists)
	}
	if offset != 0 || flags&os.O_APPEND != 0 {
		return nil, fmt.Errorf("%w: uploads cannot be resumed or appended to", os.ErrPermission)
	}
	if fs.anonymous() || !fs.s.B.CanUpload(fs.user, a) {
		return nil, os.ErrPermission
	}
	up, err := fs.s.B.Files.Upload(fs.user, a, path.Base(path.Clean("/"+name)), "")
	if errors.Is(err, files.ErrDenied) {
		return nil, os.ErrPermission
	}
	if err != nil {
		return nil, err
	}
	return &upload{Upload: up, fs: fs, area: a}, nil
}

// download is a file being downloaded.  The file is not embedded: its
// WriteTo would let the copy go around Read, and the download uncounted.
type download struct {
	file *os.File
	fs   *fileSystem
	f    *store.File
	read int64
}

func (d *download) Read(p []byte) (int, error) {
	n, err := d.file.Read(p)
	d.read += int64(n)
	return n, err
}

func (d *download) Seek(offset int64, whence int) (int64, error) {
	return d.file.Seek(offset, whence)
}

func (d *download) Write([]byte) (int, error) { return 0, os.ErrPermission }

func (d *download) Close() error {
	if d.read > 0 {
		d.fs.s.B.Downloaded(d.fs.user, d.f)
		d.fs.s.B.Log.Printf("%s: %s downloaded %q (%d bytes)", d.fs.who, d.fs.user.Name, d.f.Name, d.read)
	}
	return d.file.Close()
}

// upload is a file being uploaded: kept only if it all came.
type upload struct {
	*files.Upload
	fs     *fileSystem
	area   *store.FileArea
	failed error
}

func (u *upload) Read([]byte) (int, error)                     { return 0, os.ErrPermission }
func (u *upload) Seek(offset int64, whence int) (int64, error) { return 0, os.ErrPermission }

// TransferError is told of a transfer that failed.
func (u *upload) TransferError(err error) { u.failed = err }

func (u *upload) Close() error {
	if u.failed != nil {
		u.Abort()
		return u.failed
	}
	f, err := u.Commit()
	if err != nil {
		return err
	}
	u.fs.s.B.Log.Printf("%s: %s uploaded %q to %s (%d bytes, sha256 %s)", u.fs.who, u.fs.user.Name,
		f.Name, u.area.Name, f.Size, f.SHA256)
	return nil
}

// Remove deletes a file, if the user uploaded it (or is a sysop).
func (fs *fileSystem) Remove(name string) error {
	_, f, err := fs.resolve(name)
	if err != nil {
		return err
	}
	if f == nil {
		return os.ErrPermission
	}
	if err := fs.s.B.Files.Delete(fs.user, f); err != nil {
		if errors.Is(err, files.ErrDenied) {
			return os.ErrPermission
		}
		return err
	}
	fs.s.B.Log.Printf("%s: %s deleted %q", fs.who, fs.user.Name, f.Name)
	return nil
}

// What the file areas do not do over FTP: areas are made with the bbs
// command, files are not renamed, opened otherwise or changed.
func (fs *fileSystem) RemoveDir(string) error                     { return os.ErrPermission }
func (fs *fileSystem) RemoveAll(string) error                     { return os.ErrPermission }
func (fs *fileSystem) Mkdir(string, os.FileMode) error            { return os.ErrPermission }
func (fs *fileSystem) MkdirAll(string, os.FileMode) error         { return os.ErrPermission }
func (fs *fileSystem) Rename(string, string) error                { return os.ErrPermission }
func (fs *fileSystem) Chmod(string, os.FileMode) error            { return os.ErrPermission }
func (fs *fileSystem) Chown(string, int, int) error               { return os.ErrPermission }
func (fs *fileSystem) Chtimes(string, time.Time, time.Time) error { return os.ErrPermission }
func (fs *fileSystem) Create(string) (afero.File, error)          { return nil, os.ErrPermission }
func (fs *fileSystem) Open(string) (afero.File, error)            { return nil, os.ErrPermission }
func (fs *fileSystem) Name() string                               { return "bbs" }
func (fs *fileSystem) OpenFile(string, int, os.FileMode) (afero.File, error) {
	return nil, os.ErrPermission
}
