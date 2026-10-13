package archive

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
)

func walkZIP(r io.ReaderAt, size int64, fn func(e Entry, open Opener) bool) error {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return ErrFormat
	}
	for _, f := range z.File {
		method := "deflated"
		if f.Method == zip.Store {
			method = "stored"
		}
		e := Entry{Name: f.Name, Size: int64(f.UncompressedSize64), Packed: int64(f.CompressedSize64),
			Method: method, Modified: f.Modified, Dir: strings.HasSuffix(f.Name, "/")}
		open := func(max int64) ([]byte, error) {
			if err := sized(e.Size, max); err != nil {
				return nil, err
			}
			rc, err := f.Open()
			if err != nil {
				return nil, ErrMethod
			}
			defer rc.Close()
			var b bytes.Buffer
			n, err := io.Copy(&b, io.LimitReader(rc, max+1))
			if n > max {
				return nil, ErrTooBig
			}
			if err != nil {
				return nil, ErrChecksum // archive/zip checks the CRC as it ends
			}
			return b.Bytes(), nil
		}
		if !fn(e, open) {
			return nil
		}
	}
	return nil
}
