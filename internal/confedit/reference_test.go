package confedit

import (
	"bytes"
	"os"
	"testing"

	bbssec "github.com/D-Modem/bbs"
)

//go:generate go run ./refdoc ../../docs/wiki/Configuration-Reference.md

// TestReferenceUpToDate: the committed reference page is the one the
// sample makes now (go generate ./internal/confedit if not).
func TestReferenceUpToDate(t *testing.T) {
	have, err := os.ReadFile("../../docs/wiki/Configuration-Reference.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := Reference(NewCatalog(bbssec.Sample)); !bytes.Equal(have, want) {
		t.Error("docs/wiki/Configuration-Reference.md is out of date: go generate ./internal/confedit")
	}
}
