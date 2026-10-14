package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// [chan]'s helpers for videos' and PDFs' thumbnails, found on the PATH,
// and a video's longest length.
func TestChanMediaConfig(t *testing.T) {
	load := func(body string) (*Config, error) {
		p := filepath.Join(t.TempDir(), "bbs.conf")
		os.WriteFile(p, []byte("[listen telnet]\ntype = telnet\naddress = :2300\n"+body), 0600)
		return Load(p)
	}
	c, err := load("[chan]\n")
	if err != nil || c.Chan.FFmpeg != "" || c.Chan.MaxVideo != 120*time.Second {
		t.Fatalf("defaults: %+v %v", c.Chan, err)
	}
	c, err = load("[chan]\nffmpeg = sh\nmax_video = 300\n")
	if err != nil || !filepath.IsAbs(c.Chan.FFmpeg) || c.Chan.MaxVideo != 300*time.Second {
		t.Fatalf("set: %q %v %v", c.Chan.FFmpeg, c.Chan.MaxVideo, err)
	}
	if _, err := load("[chan]\npdftoppm = no-such-helper-here\n"); err == nil || !strings.Contains(err.Error(), "pdftoppm") {
		t.Errorf("a helper not there: %v", err)
	}
	dir := t.TempDir()
	if _, err := load("[chan]\nruffle = " + dir + "\n"); err == nil || !strings.Contains(err.Error(), "no ruffle.js") {
		t.Errorf("ruffle without its files: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "ruffle.js"), []byte("//"), 0644)
	if c, err := load("[chan]\nruffle = " + dir + "\n"); err != nil || c.Chan.Ruffle != dir {
		t.Errorf("ruffle: %v", err)
	}
}
