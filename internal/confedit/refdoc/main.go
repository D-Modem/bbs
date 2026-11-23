// Command refdoc writes docs/wiki/Configuration-Reference.md from
// bbs.conf.sample (confedit.Reference): go generate ./internal/confedit.
package main

import (
	"log"
	"os"
	"strings"

	bbssec "github.com/D-Modem/bbs"
	"github.com/D-Modem/bbs/internal/confedit"
)

func main() {
	out := "docs/wiki/Configuration-Reference.md"
	if len(os.Args) > 2 || len(os.Args) == 2 && strings.HasPrefix(os.Args[1], "-") {
		log.Fatal("usage: refdoc [output.md]")
	}
	if len(os.Args) == 2 {
		out = os.Args[1]
	}
	if err := os.WriteFile(out, confedit.Reference(confedit.NewCatalog(bbssec.Sample)), 0o644); err != nil {
		log.Fatal(err)
	}
}
