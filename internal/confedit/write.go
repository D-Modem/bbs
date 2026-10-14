package confedit

import (
	"os"
	"path/filepath"
	"syscall"
)

// WriteFile puts data in path whole or not at all: a file beside it
// renamed over it, with the old one's permissions and owner; the old one
// kept as path.bak (absent: there was none).
func WriteFile(path string, data, old []byte, absent bool) error {
	mode := os.FileMode(0o640)
	uid, gid := -1, -1
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(st.Uid), int(st.Gid)
		}
	}
	if !absent {
		if err := os.WriteFile(path+".bak", old, mode); err != nil {
			return err
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if uid >= 0 && os.Geteuid() == 0 {
		f.Chown(uid, gid)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
