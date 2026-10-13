package archive

import (
	"archive/zip"
	"bytes"
	"io"
	"time"
)

// File is a file to put in an archive.
type File struct {
	Name     string
	Data     []byte
	Modified time.Time
}

// Write writes files as an archive of the kind (called name, which ARJ
// keeps): ZIP deflated, the others stored.
func Write(w io.Writer, kind Kind, name string, files []File) error {
	switch kind {
	case ZIP:
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		for _, f := range files {
			fw, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: f.Modified})
			if err != nil {
				return err
			}
			if _, err := fw.Write(f.Data); err != nil {
				return err
			}
		}
		if err := zw.Close(); err != nil {
			return err
		}
		_, err := w.Write(b.Bytes())
		return err
	case ARC:
		return writeARC(w, files)
	case ARJ:
		return writeARJ(w, name, files)
	case LHA:
		return writeLHA(w, files)
	}
	return ErrFormat
}
