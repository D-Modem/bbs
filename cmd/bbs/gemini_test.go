package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/gemini"
)

// TestGeminiCert: bbs gemini cert asks, shows what it will make and makes
// it only on a yes; asks again after a bad answer; refuses to replace one
// without -force, and with it keeps the old one as .old.
func TestGeminiCert(t *testing.T) {
	dir := t.TempDir()
	g := &gemini.Settings{Host: "bbs.example.org", CertFile: filepath.Join(dir, "c.pem"), KeyFile: filepath.Join(dir, "k.pem")}
	var out bytes.Buffer
	if err := geminiCert(g, "Test BBS", false, strings.NewReader("\n\n\nno\n"), &out); err == nil {
		t.Error("made on a no")
	}
	if _, err := os.Stat(g.CertFile); err == nil {
		t.Error("a file made on a no")
	}
	if !strings.Contains(out.String(), "Host names callers use, by commas [bbs.example.org]: ") ||
		!strings.Contains(out.String(), "Organization (- for none) [Test BBS]: ") || !strings.Contains(out.String(), "For:      bbs.example.org\n") {
		t.Errorf("prompts:\n%s", out.String())
	}
	out.Reset()
	in := "gemini.example.org, 192.0.2.7\n-\n0\nGemini.Example.org\nMy BBS\n3\nyes\n"
	if err := geminiCert(g, "Test BBS", false, strings.NewReader(in), &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "years: 1 to 100") || !strings.Contains(out.String(), "Made: gemini.example.org until ") {
		t.Errorf("output:\n%s", out.String())
	}
	first, _ := os.ReadFile(g.CertFile)
	if err := geminiCert(g, "Test BBS", false, strings.NewReader("\n\n\nyes\n"), &out); err == nil ||
		!strings.Contains(err.Error(), "-force") {
		t.Errorf("replaced without -force: %v", err)
	}
	if err := geminiCert(g, "Test BBS", true, strings.NewReader("\n\n\nyes\n"), &out); err != nil {
		t.Fatal(err)
	}
	if old, _ := os.ReadFile(g.CertFile + ".old"); !bytes.Equal(old, first) {
		t.Error("old certificate not kept")
	}
	if now, _ := os.ReadFile(g.CertFile); bytes.Equal(now, first) {
		t.Error("not replaced")
	}
}
